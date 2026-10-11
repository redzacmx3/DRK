package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

func ValidateSMTPRecipients(ctx context.Context, db *sql.DB, config *SMTPConfig) error {
	if config == nil {
		return nil
	}
	if err := validateSMTPConfig(*config); err != nil {
		return err
	}
	for _, recipient := range config.Recipients {
		var exists bool
		if err := db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM sites WHERE id=?)`, recipient.SiteID).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return fmt.Errorf("SMTP recipient has an unknown site")
		}
	}
	return nil
}
func smtpTarget(config *SMTPConfig, site, address string) bool {
	for _, recipient := range config.Recipients {
		if recipient.SiteID == site && recipient.Address == address {
			return true
		}
	}
	return false
}

// Enqueueing and leasing are short database transactions. SMTP network work is
// never performed with a SQLite transaction open.
func enqueueReminderMail(ctx context.Context, db *sql.DB, config *SMTPConfig, now time.Time) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	type target struct{ id, site string }
	targets := []target{}
	rows, err := tx.QueryContext(ctx, `SELECT m.id,l.site_id FROM device_reminders m JOIN devices d ON d.id=m.device_id JOIN locations l ON l.id=d.location_id WHERE m.active=1 ORDER BY m.created_at,m.id LIMIT 3001`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var item target
		if err = rows.Scan(&item.id, &item.site); err != nil {
			rows.Close()
			return err
		}
		targets = append(targets, item)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if len(targets) > 3000 {
		return fmt.Errorf("mail queue exceeds reminder limit")
	}
	total := 0
	for _, item := range targets {
		for _, recipient := range config.Recipients {
			if recipient.SiteID == item.site {
				total++
			}
		}
	}
	if total > 10000 {
		return fmt.Errorf("mail targets exceed 10000; no partial enqueue")
	}
	stamp := now.UTC().Format(time.RFC3339Nano)
	for _, item := range targets {
		for _, recipient := range config.Recipients {
			if recipient.SiteID != item.site {
				continue
			}
			id := newID()
			if _, err = tx.ExecContext(ctx, `INSERT INTO device_reminder_deliveries(id,reminder_id,site_id,recipient,message_id,created_at,state,next_attempt_at) VALUES(?,?,?,?,?,?,'pending',?) ON CONFLICT(reminder_id,recipient) DO NOTHING`, id, item.id, item.site, recipient.Address, "<"+id+"@drk-inventory.invalid>", stamp, stamp); err != nil {
				return err
			}
		}
	}
	// A process that died after SMTP DATA may already have sent its message.
	rows, err = tx.QueryContext(ctx, `SELECT id FROM device_reminder_deliveries WHERE state='sending' AND lease_until<=?`, stamp)
	if err != nil {
		return err
	}
	expired := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		expired = append(expired, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, id := range expired {
		if _, err = tx.ExecContext(ctx, `UPDATE device_reminder_deliveries SET state='uncertain',lease_token=NULL,lease_until=NULL,last_code='lease_expired' WHERE id=?`, id); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO device_reminder_mail_attempts VALUES(?,?,?,'uncertain','lease_expired')`, newID(), id, stamp); err != nil {
			return err
		}
	}
	return tx.Commit()
}

type leasedReminderMail struct {
	id, token, reminder, site, recipient, messageID string
	attempts                                        int
	created                                         time.Time
}

func claimReminderMail(ctx context.Context, db *sql.DB, now time.Time) (*leasedReminderMail, error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var item leasedReminderMail
	var created string
	stamp := now.UTC().Format(time.RFC3339Nano)
	err = tx.QueryRowContext(ctx, `SELECT id,reminder_id,site_id,recipient,message_id,attempts,created_at FROM device_reminder_deliveries WHERE state IN ('pending','retry') AND next_attempt_at<=? ORDER BY next_attempt_at,id LIMIT 1`, stamp).Scan(&item.id, &item.reminder, &item.site, &item.recipient, &item.messageID, &item.attempts, &created)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	item.created, err = time.Parse(time.RFC3339Nano, created)
	if err != nil {
		return nil, err
	}
	item.token = randomToken()
	item.attempts++
	if _, err = tx.ExecContext(ctx, `UPDATE device_reminder_deliveries SET state='sending',attempts=attempts+1,last_attempt_at=?,lease_token=?,lease_until=? WHERE id=?`, stamp, item.token, now.Add(2*time.Minute).UTC().Format(time.RFC3339Nano), item.id); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return &item, nil
}
func currentReminderMail(ctx context.Context, db *sql.DB, config *SMTPConfig, item leasedReminderMail, now time.Time) (*ReminderMail, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if !smtpTarget(config, item.site, item.recipient) {
		return nil, nil
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var id, kind, due, basis, site, actor, created string
	var threshold int
	var input deviceInput
	err = tx.QueryRowContext(ctx, `SELECT d.id,m.control_type,m.due_on,m.basis_hash,m.threshold_days,l.site_id,d.location_id,d.inventory_number,d.name,d.manufacturer,d.model,d.serial_or_lot,d.created_by,d.created_at,d.note FROM device_reminders m JOIN devices d ON d.id=m.device_id JOIN locations l ON l.id=d.location_id WHERE m.id=? AND m.active=1`, item.reminder).Scan(&id, &kind, &due, &basis, &threshold, &site, &input.LocationID, &input.InventoryNumber, &input.Name, &input.Manufacturer, &input.Model, &input.SerialOrLot, &actor, &created, &input.Note)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if site != item.site {
		return nil, nil
	}
	record := registeredDevice(input, id, actor, created)
	if err = projectDevice(ctx, tx, &record, now); err != nil {
		return nil, err
	}
	if record.Decommission != nil || reminderBasis(record, kind) != basis || record.Controls[kind].State == "UNKNOWN" || record.Controls[kind].State == "NOT_APPLICABLE" {
		return nil, nil
	}
	var configJSON string
	if err = tx.QueryRowContext(ctx, `SELECT thresholds_json FROM device_reminder_worker WHERE id=1`).Scan(&configJSON); err != nil {
		return nil, err
	}
	var thresholds []int
	if err = json.Unmarshal([]byte(configJSON), &thresholds); err != nil {
		return nil, err
	}
	today, err := reminderDay(now)
	if err != nil {
		return nil, err
	}
	date, err := civilDate(due)
	if err != nil {
		return nil, err
	}
	days := int(date.Sub(today).Hours() / 24)
	stage, ok := reminderStage(days, thresholds)
	if !ok || stage != threshold {
		return nil, nil
	} // do not mail a superseded warning after an outage
	label := kind
	if kind == "electrical" {
		label = "Elektrische Kontrolle"
	}
	condition := fmt.Sprintf("in %d Tagen", days)
	if days == 0 {
		condition = "heute fällig"
	}
	if days < 0 {
		condition = fmt.Sprintf("%d Tage überfällig", -days)
	}
	return &ReminderMail{Recipient: item.recipient, MessageID: item.messageID, CreatedAt: item.created, Subject: "DRK: Kontrollerinnerung " + record.InventoryNumber, Body: fmt.Sprintf("Kontrollerinnerung\n\nGerät: %s – %s\nKontrolle: %s\nFällig: %s (%s)\n\nBitte die aktuelle Geräteakte und Arbeitsübersicht prüfen. Dieser Hinweis bestätigt keine Einsatzfreigabe. Eine E-Mail ersetzt keine erforderliche Behördenmeldung.\n", record.InventoryNumber, record.Name, label, due, condition)}, nil
}
func finishReminderMail(ctx context.Context, db *sql.DB, item leasedReminderMail, now time.Time, outcome MailOutcome) error {
	switch outcome.State {
	case "accepted", "retry", "failed", "uncertain", "skipped":
	default:
		outcome = MailOutcome{"uncertain", "invalid_sender_outcome"}
	}
	if outcome.State == "retry" && item.attempts >= 5 {
		outcome = MailOutcome{"failed", "retry_exhausted"}
	}
	delays := []time.Duration{time.Minute, 5 * time.Minute, 15 * time.Minute, time.Hour, time.Hour}
	index := item.attempts - 1
	if index < 0 {
		index = 0
	}
	if index >= len(delays) {
		index = len(delays) - 1
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `UPDATE device_reminder_deliveries SET state=?,next_attempt_at=?,lease_token=NULL,lease_until=NULL,last_code=? WHERE id=? AND state='sending' AND lease_token=?`, outcome.State, now.Add(delays[index]).UTC().Format(time.RFC3339Nano), outcome.Code, item.id, item.token)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return fmt.Errorf("mail delivery lease lost")
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO device_reminder_mail_attempts VALUES(?,?,?,?,?)`, newID(), item.id, now.UTC().Format(time.RFC3339Nano), outcome.State, outcome.Code); err != nil {
		return err
	}
	return tx.Commit()
}

// DispatchReminderMail is disabled unless an explicit validated config is given.
// Each pass processes at most20 messages and60 seconds; jobs can run concurrently.
func DispatchReminderMail(ctx context.Context, db *sql.DB, config *SMTPConfig, sender ReminderMailSender, now func() time.Time) error {
	if config == nil {
		return nil
	}
	if sender == nil {
		return fmt.Errorf("SMTP sender unavailable")
	}
	if err := ValidateSMTPRecipients(ctx, db, config); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	if err := enqueueReminderMail(ctx, db, config, now()); err != nil {
		return err
	}
	for n := 0; n < 20; n++ {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		item, err := claimReminderMail(ctx, db, now())
		if err != nil {
			return err
		}
		if item == nil {
			return nil
		}
		message, err := currentReminderMail(ctx, db, config, *item, now())
		outcome := MailOutcome{"skipped", "obsolete_or_recipient_removed"}
		if err != nil {
			outcome = MailOutcome{"retry", "record_unavailable"}
		} else if message != nil {
			outcome = sender.Send(ctx, *message)
		}
		// Persist even if shutdown cancelled the network request. If this fails,
		// lease recovery conservatively marks the send as uncertain.
		saveCtx, saveCancel := context.WithTimeout(context.Background(), 5*time.Second)
		err = finishReminderMail(saveCtx, db, *item, now(), outcome)
		saveCancel()
		if err != nil {
			return err
		}
	}
	return nil
}
func RunReminderMailWorker(ctx context.Context, db *sql.DB, config *SMTPConfig, report func(error)) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		if ctx.Err() != nil {
			return
		}
		if err := DispatchReminderMail(ctx, db, config, config, time.Now); err != nil && ctx.Err() == nil && report != nil {
			report(err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// Explicit local operation; uncertain sends require an acknowledged duplicate risk.
func RetryReminderMail(ctx context.Context, db *sql.DB, id string, allowUncertain bool, now time.Time) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var state string
	if err = tx.QueryRowContext(ctx, `SELECT state FROM device_reminder_deliveries WHERE id=?`, id).Scan(&state); err != nil {
		return err
	}
	if state != "failed" && state != "uncertain" {
		return fmt.Errorf("only failed or uncertain mail can be retried")
	}
	if state == "uncertain" && !allowUncertain {
		return fmt.Errorf("uncertain SMTP acceptance requires --accept-duplicate-risk after external checking")
	}
	stamp := now.UTC().Format(time.RFC3339Nano)
	if _, err = tx.ExecContext(ctx, `UPDATE device_reminder_deliveries SET state='retry',attempts=0,next_attempt_at=?,last_code='manual_retry' WHERE id=?`, stamp, id); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO device_reminder_mail_attempts VALUES(?,?,?,'manual_retry',?)`, newID(), id, stamp, state); err != nil {
		return err
	}
	return tx.Commit()
}
