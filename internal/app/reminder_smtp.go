package app

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/quotedprintable"
	"net"
	"net/mail"
	"net/smtp"
	"net/textproto"
	"os"
	"strconv"
	"strings"
	"time"
)

type SMTPRecipient struct {
	SiteID  string `json:"site_id"`
	Address string `json:"address"`
}
type SMTPConfig struct {
	Host         string          `json:"host"`
	Port         int             `json:"port"`
	Mode         string          `json:"mode"`
	From         string          `json:"from"`
	Username     string          `json:"username"`
	PasswordFile string          `json:"password_file"`
	CAFile       string          `json:"ca_file"`
	Recipients   []SMTPRecipient `json:"recipients"`
	password     string
	roots        *x509.CertPool
}

func strictMailbox(value string) bool {
	for _, c := range []byte(value) {
		if c < 33 || c > 126 {
			return false
		}
	}
	address, err := mail.ParseAddress(value)
	return err == nil && address.Address == value && address.Name == "" && !strings.ContainsAny(value, "\r\n\x00") && len(value) <= 254 && strings.Contains(value, "@")
}
func validateSMTPConfig(config SMTPConfig) error {
	if config.Host == "" || len(config.Host) > 253 || strings.ContainsAny(config.Host, " /\\\t\r\n@:") && net.ParseIP(config.Host) == nil || config.Port < 1 || config.Port > 65535 {
		return fmt.Errorf("invalid SMTP host/port")
	}
	if config.Mode != "starttls" && config.Mode != "tls" {
		return fmt.Errorf("SMTP requires starttls or tls with certificate verification")
	}
	if !strictMailbox(config.From) || len(config.Username) > 254 || strings.ContainsAny(config.Username, "\r\n\x00") {
		return fmt.Errorf("invalid SMTP sender/account")
	}
	if (config.Username == "") != (config.PasswordFile == "") {
		return fmt.Errorf("SMTP username and password_file must be supplied together")
	}
	if len(config.Recipients) == 0 || len(config.Recipients) > 100 {
		return fmt.Errorf("SMTP requires 1–100 explicit site recipients")
	}
	seen := map[string]bool{}
	for _, recipient := range config.Recipients {
		key := recipient.SiteID + "\x00" + recipient.Address
		if recipient.SiteID == "" || len(recipient.SiteID) > 100 || strings.TrimSpace(recipient.SiteID) != recipient.SiteID || !strictMailbox(recipient.Address) || seen[key] {
			return fmt.Errorf("invalid or duplicate SMTP site recipient")
		}
		seen[key] = true
	}
	return nil
}
func LoadSMTPConfig(path string) (*SMTPConfig, error) {
	if path == "" {
		return nil, nil
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("SMTP configuration file unavailable")
	}
	defer file.Close()
	bytes, err := io.ReadAll(io.LimitReader(file, 65537))
	if err != nil || len(bytes) > 65536 {
		return nil, fmt.Errorf("SMTP configuration file invalid or too large")
	}
	decoder := json.NewDecoder(strings.NewReader(string(bytes)))
	decoder.DisallowUnknownFields()
	var config SMTPConfig
	if decoder.Decode(&config) != nil {
		return nil, fmt.Errorf("invalid SMTP configuration JSON")
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return nil, fmt.Errorf("SMTP configuration must contain exactly one JSON object")
	}
	if err = validateSMTPConfig(config); err != nil {
		return nil, err
	}
	if config.PasswordFile != "" {
		file, err := os.Open(config.PasswordFile)
		if err != nil {
			return nil, fmt.Errorf("SMTP password file unavailable")
		}
		bytes, err := io.ReadAll(io.LimitReader(file, 4097))
		file.Close()
		if err != nil || len(bytes) > 4096 {
			return nil, fmt.Errorf("SMTP password file invalid or too large")
		}
		config.password = strings.TrimSuffix(strings.TrimSuffix(string(bytes), "\n"), "\r")
		if config.password == "" || strings.ContainsAny(config.password, "\r\n\x00") {
			return nil, fmt.Errorf("SMTP password file empty or invalid")
		}
	}
	if config.CAFile != "" {
		roots, err := x509.SystemCertPool()
		if err != nil {
			return nil, fmt.Errorf("SMTP system trust unavailable")
		}
		file, err := os.Open(config.CAFile)
		if err != nil {
			return nil, fmt.Errorf("SMTP CA file unavailable")
		}
		bytes, err := io.ReadAll(io.LimitReader(file, 1<<20+1))
		file.Close()
		if err != nil || len(bytes) > 1<<20 || !roots.AppendCertsFromPEM(bytes) {
			return nil, fmt.Errorf("SMTP CA file invalid")
		}
		config.roots = roots
	}
	return &config, nil
}

type ReminderMail struct {
	Recipient, MessageID, Subject, Body string
	CreatedAt                           time.Time
}
type MailOutcome struct{ State, Code string }
type ReminderMailSender interface {
	Send(context.Context, ReminderMail) MailOutcome
}

// SMTP acceptance is not proof of mailbox delivery. A lost DATA response is
// uncertain and must never trigger an automatic duplicate.
func (config *SMTPConfig) Send(ctx context.Context, message ReminderMail) MailOutcome {
	if err := validateSMTPConfig(*config); err != nil {
		return MailOutcome{"failed", "invalid_config"}
	}
	if !strictMailbox(message.Recipient) || strings.ContainsAny(message.MessageID+message.Subject, "\r\n\x00") {
		return MailOutcome{"failed", "invalid_message"}
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	dialer := net.Dialer{}
	raw, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(config.Host, strconv.Itoa(config.Port)))
	if err != nil {
		return MailOutcome{"retry", "connect"}
	}
	defer raw.Close()
	deadline, _ := ctx.Deadline()
	raw.SetDeadline(deadline)
	stop := context.AfterFunc(ctx, func() { raw.Close() })
	defer stop()
	tlsConfig := &tls.Config{ServerName: config.Host, RootCAs: config.roots, MinVersion: tls.VersionTLS12}
	connection := raw
	if config.Mode == "tls" {
		secured := tls.Client(raw, tlsConfig)
		if err = secured.HandshakeContext(ctx); err != nil {
			return MailOutcome{"failed", "tls"}
		}
		connection = secured
	}
	client, err := smtp.NewClient(connection, config.Host)
	if err != nil {
		return smtpFailure(err, false)
	}
	defer client.Close()
	if config.Mode == "starttls" {
		if ok, _ := client.Extension("STARTTLS"); !ok {
			return MailOutcome{"failed", "tls_required"}
		}
		if err = client.StartTLS(tlsConfig); err != nil {
			return MailOutcome{"failed", "tls"}
		}
	}
	if config.Username != "" {
		if err = client.Auth(smtp.PlainAuth("", config.Username, config.password, config.Host)); err != nil {
			return smtpFailure(err, false)
		}
	}
	if err = client.Mail(config.From); err != nil {
		return smtpFailure(err, false)
	}
	if err = client.Rcpt(message.Recipient); err != nil {
		return smtpFailure(err, false)
	}
	writer, err := client.Data()
	if err != nil {
		return smtpFailure(err, false)
	}
	// Text-only UTF-8; quoted-printable ensures compatibility without SMTPUTF8.
	body := encodeReminderMail(config.From, message)
	if _, err = io.WriteString(writer, body); err != nil {
		return smtpFailure(err, true)
	}
	if err = writer.Close(); err != nil {
		return smtpFailure(err, true)
	}
	client.Quit() // a QUIT error after DATA 250 does not undo server acceptance
	return MailOutcome{"accepted", "smtp_accepted"}
}
func smtpFailure(err error, afterData bool) MailOutcome {
	var response *textproto.Error
	if errors.As(err, &response) {
		if response.Code >= 500 && response.Code < 600 {
			return MailOutcome{"failed", "smtp_5xx"}
		}
		if response.Code >= 400 && response.Code < 500 {
			return MailOutcome{"retry", "smtp_4xx"}
		}
	}
	if afterData {
		return MailOutcome{"uncertain", "data_response_lost"}
	}
	return MailOutcome{"retry", "transport"}
}

func encodeReminderMail(from string, message ReminderMail) string {
	var body strings.Builder
	writer := quotedprintable.NewWriter(&body)
	writer.Write([]byte(message.Body))
	writer.Close()
	return "From: " + from + "\r\nTo: " + message.Recipient + "\r\nDate: " + message.CreatedAt.Format(time.RFC1123Z) + "\r\nMessage-ID: " + message.MessageID + "\r\nSubject: " + mime.QEncoding.Encode("UTF-8", message.Subject) + "\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\nContent-Transfer-Encoding: quoted-printable\r\n\r\n" + body.String() + "\r\n"
}
