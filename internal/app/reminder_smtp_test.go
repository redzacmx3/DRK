package app

import (
	"bufio"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"mime"
	"mime/quotedprintable"
	"net"
	"net/http"
	"net/http/httptest"
	"net/mail"
	"net/textproto"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

type smtpTestOptions struct {
	mode                      string
	noTLS, dropDATA, dropQUIT bool
	reject                    int
}

func smtpTestServer(t *testing.T, options smtpTestOptions) (*SMTPConfig, <-chan string) {
	t.Helper()
	sample := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	certificate := sample.TLS.Certificates[0]
	root := sample.Certificate()
	sample.Close()
	roots := x509.NewCertPool()
	roots.AddCert(root)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	tlsConfig := &tls.Config{Certificates: []tls.Certificate{certificate}, MinVersion: tls.VersionTLS12}
	captured := make(chan string, 20)
	var wg sync.WaitGroup
	acceptDone := make(chan struct{})
	go func() {
		defer close(acceptDone)
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			wg.Add(1)
			go func() {
				defer wg.Done()
				defer conn.Close()
				conn.SetDeadline(time.Now().Add(5 * time.Second))
				encrypted := false
				if options.mode == "tls" {
					secured := tls.Server(conn, tlsConfig)
					if secured.Handshake() != nil {
						return
					}
					conn = secured
					encrypted = true
				}
				reader := bufio.NewReader(conn)
				writer := bufio.NewWriter(conn)
				reply := func(line string) { fmt.Fprint(writer, line+"\r\n"); writer.Flush() }
				reply("220 synthetic SMTP")
				for {
					line, err := reader.ReadString('\n')
					if err != nil {
						return
					}
					command := strings.TrimSpace(line)
					switch {
					case strings.HasPrefix(command, "EHLO"):
						reply("250-synthetic")
						if !encrypted && !options.noTLS {
							reply("250-STARTTLS")
						}
						reply("250 AUTH PLAIN")
					case command == "STARTTLS":
						reply("220 upgrade")
						secured := tls.Server(conn, tlsConfig)
						if secured.Handshake() != nil {
							return
						}
						conn = secured
						encrypted = true
						reader = bufio.NewReader(conn)
						writer = bufio.NewWriter(conn)
					case strings.HasPrefix(command, "AUTH"):
						if !encrypted {
							reply("530 TLS required")
						} else {
							reply("235 authenticated")
						}
					case strings.HasPrefix(command, "MAIL FROM"):
						reply("250 sender")
					case strings.HasPrefix(command, "RCPT TO"):
						if options.reject != 0 {
							reply(fmt.Sprintf("%d synthetic rejection", options.reject))
						} else {
							reply("250 recipient")
						}
					case command == "DATA":
						reply("354 data")
						bytes, err := textproto.NewReader(reader).ReadDotBytes()
						if err != nil {
							return
						}
						captured <- string(bytes)
						if options.dropDATA {
							return
						}
						reply("250 accepted")
					case command == "QUIT":
						if !options.dropQUIT {
							reply("221 goodbye")
						}
						return
					default:
						reply("250 synthetic")
					}
				}
			}()
		}
	}()
	t.Cleanup(func() { listener.Close(); <-acceptDone; wg.Wait() })
	_, port, _ := net.SplitHostPort(listener.Addr().String())
	n, _ := strconv.Atoi(port)
	return &SMTPConfig{Host: "127.0.0.1", Port: n, Mode: options.mode, From: "inventory@example.test", Recipients: []SMTPRecipient{{demoSite, "manager@example.test"}}, roots: roots}, captured
}
func TestSMTPVerifiedTLSModesAndAcceptance(t *testing.T) {
	for _, mode := range []string{"tls", "starttls"} {
		t.Run(mode, func(t *testing.T) {
			config, captured := smtpTestServer(t, smtpTestOptions{mode: mode, dropQUIT: true})
			config.Username = "synthetic"
			config.PasswordFile = "unused-test-path"
			config.password = "synthetic-password"
			message := ReminderMail{Recipient: "manager@example.test", MessageID: "<synthetic@drk-inventory.invalid>", Subject: "DRK: Überfällige Kontrolle", Body: "Gerät: Ä <svg>synthetic</svg>\n.keine HTML-Mail\n", CreatedAt: reminderTime("2026-10-11")}
			result := config.Send(context.Background(), message)
			if result.State != "accepted" {
				t.Fatal(result)
			}
			data := <-captured
			parsed, err := mail.ReadMessage(strings.NewReader(data))
			if err != nil {
				t.Fatal(err)
			}
			subject, err := new(mime.WordDecoder).DecodeHeader(parsed.Header.Get("Subject"))
			if err != nil || subject != message.Subject {
				t.Fatal(subject, err)
			}
			body, err := io.ReadAll(quotedprintable.NewReader(parsed.Body))
			if err != nil || !strings.Contains(string(body), "Ä <svg>synthetic</svg>") || parsed.Header.Get("Message-ID") != message.MessageID || parsed.Header.Get("Content-Type") != "text/plain; charset=UTF-8" {
				t.Fatal("invalid MIME message")
			}
		})
	}
}
func TestSMTPTLSRequiredAndAmbiguousDATA(t *testing.T) {
	tests := []struct {
		name        string
		options     smtpTestOptions
		untrust     bool
		state, code string
	}{{"missing STARTTLS", smtpTestOptions{mode: "starttls", noTLS: true}, false, "failed", "tls_required"}, {"untrusted certificate", smtpTestOptions{mode: "tls"}, true, "failed", "tls"}, {"lost DATA response", smtpTestOptions{mode: "tls", dropDATA: true}, false, "uncertain", "data_response_lost"}, {"temporary recipient rejection", smtpTestOptions{mode: "tls", reject: 450}, false, "retry", "smtp_4xx"}, {"permanent recipient rejection", smtpTestOptions{mode: "tls", reject: 550}, false, "failed", "smtp_5xx"}}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			config, _ := smtpTestServer(t, test.options)
			if test.untrust {
				config.roots = x509.NewCertPool()
			}
			result := config.Send(context.Background(), ReminderMail{Recipient: "manager@example.test", MessageID: "<test@drk-inventory.invalid>", Subject: "Synthetic", Body: "Synthetic", CreatedAt: time.Now()})
			if result.State != test.state || result.Code != test.code {
				t.Fatal(result)
			}
		})
	}
}
func TestSMTPConfigFilesAndHeaderInjection(t *testing.T) {
	config, _ := smtpTestServer(t, smtpTestOptions{mode: "tls"})
	dir := t.TempDir()
	config.PasswordFile = filepath.Join(dir, "password")
	config.Username = "synthetic"
	if err := os.WriteFile(config.PasswordFile, []byte("synthetic-secret\n"), 0600); err != nil {
		t.Fatal(err)
	}
	// Write the test root certificate so the actual config loader verifies it.
	encoded, _ := json.Marshal(config)
	path := filepath.Join(dir, "config.json")
	os.WriteFile(path, encoded, 0600)
	loaded, err := LoadSMTPConfig(path)
	if err != nil || loaded.password != "synthetic-secret" {
		t.Fatal(err)
	}
	loaded.CAFile = filepath.Join(dir, "ca.pem")
	sample := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	certificate := sample.Certificate()
	sample.Close()
	os.WriteFile(loaded.CAFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certificate.Raw}), 0600)
	encoded, _ = json.Marshal(loaded)
	os.WriteFile(path, encoded, 0600)
	if _, err = LoadSMTPConfig(path); err != nil {
		t.Fatal(err)
	}
	for _, invalid := range []string{`{"host":"synthetic-secret","password":"synthetic-secret"}`, string(encoded) + ` {}`, strings.Replace(string(encoded), `"mode":"tls"`, `"mode":"plain"`, 1)} {
		os.WriteFile(path, []byte(invalid), 0600)
		_, err = LoadSMTPConfig(path)
		if err == nil || strings.Contains(err.Error(), "synthetic-secret") {
			t.Fatal("invalid config or secret leak", err)
		}
	}
	for _, address := range []string{"Name <manager@example.test>", "manager@example.test\r\nBcc: leak@example.test", "mär@example.test"} {
		if strictMailbox(address) {
			t.Fatal("unsafe envelope", address)
		}
	}
	result := config.Send(context.Background(), ReminderMail{Recipient: "manager@example.test", MessageID: "<id>\r\nBcc: injected", Subject: "Synthetic"})
	if result.State != "failed" || result.Code != "invalid_message" {
		t.Fatal(result)
	}
	disabled, err := LoadSMTPConfig("")
	if disabled != nil || err != nil {
		t.Fatal("default SMTP enabled")
	}
}
