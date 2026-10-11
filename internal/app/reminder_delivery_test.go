package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeReminderSender struct {
	mu       sync.Mutex
	messages []ReminderMail
	outcomes []MailOutcome
	before   func()
}

func (sender *fakeReminderSender) Send(ctx context.Context, message ReminderMail) MailOutcome {
	if sender.before != nil {
		sender.before()
	}
	sender.mu.Lock()
	defer sender.mu.Unlock()
	sender.messages = append(sender.messages, message)
	if len(sender.outcomes) > 0 {
		result := sender.outcomes[0]
		sender.outcomes = sender.outcomes[1:]
		return result
	}
	return MailOutcome{"accepted", "smtp_accepted"}
}
func mailConfig() *SMTPConfig {
	return &SMTPConfig{Host: "smtp.example.test", Port: 587, Mode: "starttls", From: "inventory@example.test", Recipients: []SMTPRecipient{{demoSite, "manager@example.test"}}}
}
func dispatchTest(t *testing.T, f stockFixture, config *SMTPConfig, sender ReminderMailSender, now time.Time) {
	t.Helper()
	if err := DispatchReminderMail(context.Background(), f.db, config, sender, func() time.Time { return now }); err != nil {
		t.Fatal(err)
	}
}
func mailStates(t *testing.T, f stockFixture) map[string]int {
	t.Helper()
	rows, err := f.db.Query(`SELECT state,count(*) FROM device_reminder_deliveries GROUP BY state`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	states := map[string]int{}
	for rows.Next() {
		var state string
		var n int
		if err = rows.Scan(&state, &n); err != nil {
			t.Fatal(err)
		}
		states[state] = n
	}
	if rows.Err() != nil {
		t.Fatal(rows.Err())
	}
	return states
}
func TestReminderMailDedupScopeAndNetworkOutsideTransaction(t *testing.T) {
	f, id := reminderFixture(t)
	reconcileTest(t, f, "2026-10-11")
	before := reminderCount(t, f, "device_control_events")
	sender := &fakeReminderSender{}
	sender.before = func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		var count int
		if err := f.db.QueryRowContext(ctx, `SELECT count(*) FROM device_control_events`).Scan(&count); err != nil {
			t.Error("SMTP holds SQLite transaction", err)
		}
	}
	config := mailConfig()
	now := reminderTime("2026-10-11")
	dispatchTest(t, f, config, sender, now)
	dispatchTest(t, f, config, sender, now)
	if len(sender.messages) != 2 || mailStates(t, f)["accepted"] != 2 || reminderCount(t, f, "device_reminder_mail_attempts") != 2 {
		t.Fatal("duplicate send")
	}
	if sender.messages[0].MessageID == sender.messages[1].MessageID || !strings.Contains(sender.messages[0].Body, "keine Einsatzfreigabe") {
		t.Fatal("mail identity or disclaimer")
	}
	if reminderCount(t, f, "device_control_events") != before {
		t.Fatal("mail changed technical history")
	}
	w := packHTTP(f, "GET", "devices/reminder-mail", "", nil)
	requireStatus(t, w, 200)
	var statuses []reminderMailStatus
	if err := json.Unmarshal(w.Body.Bytes(), &statuses); err != nil {
		t.Fatal(err)
	}
	if len(statuses) != 2 || statuses[0].DeviceID != id || strings.Contains(w.Body.String(), config.Recipients[0].Address) {
		t.Fatal("wrong scope or recipient exposed")
	}
	if err := RetryReminderMail(context.Background(), f.db, statuses[0].ID, true, now); err == nil {
		t.Fatal("accepted mail retried")
	}
	if err := SetViewerSites(f.db, "viewer", nil); err != nil {
		t.Fatal(err)
	}
	f.cookie, f.csrf = loginTest(t, f.handler, nil)
	w = packHTTP(f, "GET", "devices/reminder-mail", "", nil)
	requireStatus(t, w, 200)
	if strings.Contains(w.Body.String(), id) {
		t.Fatal("mail scope leak")
	}
	for _, query := range []string{`UPDATE device_reminder_deliveries SET recipient='leak@example.test'`, `DELETE FROM device_reminder_deliveries`, `UPDATE device_reminder_mail_attempts SET code='changed'`, `DELETE FROM device_reminder_mail_attempts`} {
		if _, err := f.db.Exec(query); err == nil {
			t.Fatal("mutable delivery evidence")
		}
	}
}
func TestReminderMailRetryAndUncertainManualRecovery(t *testing.T) {
	f, _ := reminderFixture(t)
	reconcileTest(t, f, "2026-10-11")
	config := mailConfig()
	now := reminderTime("2026-10-11")
	sender := &fakeReminderSender{outcomes: []MailOutcome{{"retry", "smtp_4xx"}, {"uncertain", "data_response_lost"}}}
	dispatchTest(t, f, config, sender, now)
	if mailStates(t, f)["retry"] != 1 || mailStates(t, f)["uncertain"] != 1 {
		t.Fatal(mailStates(t, f))
	}
	dispatchTest(t, f, config, sender, now.Add(30*time.Second))
	if len(sender.messages) != 2 {
		t.Fatal("retry too early or uncertain repeated")
	}
	firstIDs := map[string]bool{}
	for _, message := range sender.messages {
		firstIDs[message.MessageID] = true
	}
	dispatchTest(t, f, config, sender, now.Add(time.Minute))
	if len(sender.messages) != 3 || !firstIDs[sender.messages[2].MessageID] || mailStates(t, f)["accepted"] != 1 {
		t.Fatal("retry identity/state")
	}
	var id string
	if err := f.db.QueryRow(`SELECT id FROM device_reminder_deliveries WHERE state='uncertain'`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	if err := RetryReminderMail(context.Background(), f.db, id, false, now); err == nil {
		t.Fatal("uncertain retry lacked acknowledgement")
	}
	if err := RetryReminderMail(context.Background(), f.db, id, true, now); err != nil {
		t.Fatal(err)
	}
	dispatchTest(t, f, config, sender, now)
	if len(sender.messages) != 4 || mailStates(t, f)["accepted"] != 2 {
		t.Fatal("explicit recovery failed")
	}
}
func TestReminderMailRejectsObsoleteAndRemovedRecipients(t *testing.T) {
	f, id := reminderFixture(t)
	reconcileTest(t, f, "2022-01-30")
	config := mailConfig()
	sender := &fakeReminderSender{}
	dispatchTest(t, f, config, sender, reminderTime("2022-03-25"))
	if len(sender.messages) != 0 || mailStates(t, f)["skipped"] != 1 {
		t.Fatal("superseded warning sent")
	}
	reconcileTest(t, f, "2022-03-25")
	dispatchTest(t, f, config, sender, reminderTime("2022-03-25"))
	if len(sender.messages) != 1 {
		t.Fatal("new stage not sent")
	}
	reconcileTest(t, f, "2026-10-11")
	if err := enqueueReminderMail(context.Background(), f.db, config, reminderTime("2026-10-11")); err != nil {
		t.Fatal(err)
	}
	retired := deviceControlInput{ExpectedVersion: 2, PerformedOn: "2024-01-01", Evidence: "Synthetic retire", Note: "Synthetic retire"}
	requireStatus(t, packHTTP(f, "POST", "devices/"+id+"/decommission", "mail-retire", retired), 200)
	config.Recipients[0].Address = "new-manager@example.test"
	dispatchTest(t, f, config, sender, reminderTime("2026-10-11"))
	if len(sender.messages) != 1 {
		t.Fatal("retired/removed recipient sent")
	}
}
func TestReminderMailLeaseRecoveryAndConcurrentDispatch(t *testing.T) {
	f, _ := reminderFixture(t)
	reconcileTest(t, f, "2026-10-11")
	config := mailConfig()
	now := reminderTime("2026-10-11")
	if err := enqueueReminderMail(context.Background(), f.db, config, now); err != nil {
		t.Fatal(err)
	}
	item, err := claimReminderMail(context.Background(), f.db, now)
	if err != nil || item == nil {
		t.Fatal(err)
	}
	if err := enqueueReminderMail(context.Background(), f.db, config, now.Add(3*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if mailStates(t, f)["uncertain"] != 1 {
		t.Fatal("expired lease blindly retried")
	}
	if err := finishReminderMail(context.Background(), f.db, *item, now, MailOutcome{"accepted", "smtp_accepted"}); err == nil {
		t.Fatal("expired lease finished")
	}
	sender := &fakeReminderSender{}
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for n := 0; n < 2; n++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- DispatchReminderMail(context.Background(), f.db, config, sender, func() time.Time { return now.Add(3 * time.Minute) })
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if len(sender.messages) != 1 || mailStates(t, f)["uncertain"] != 1 {
		t.Fatal("concurrent duplicate or uncertainty lost")
	}
}
func TestReminderMailAtomicEnqueueAndBoundedRetries(t *testing.T) {
	f, _ := reminderFixture(t)
	reconcileTest(t, f, "2026-10-11")
	config := mailConfig()
	now := reminderTime("2026-10-11")
	if _, err := f.db.Exec(`CREATE TRIGGER fail_delivery BEFORE INSERT ON device_reminder_deliveries WHEN (SELECT count(*) FROM device_reminder_deliveries)>0 BEGIN SELECT RAISE(ABORT,'rollback'); END`); err != nil {
		t.Fatal(err)
	}
	if err := enqueueReminderMail(context.Background(), f.db, config, now); err == nil {
		t.Fatal("failed enqueue committed")
	}
	if reminderCount(t, f, "device_reminder_deliveries") != 0 {
		t.Fatal("partial queue")
	}
	f.db.Exec(`DROP TRIGGER fail_delivery`)
	outcomes := []MailOutcome{}
	for n := 0; n < 10; n++ {
		outcomes = append(outcomes, MailOutcome{"retry", "smtp_4xx"})
	}
	sender := &fakeReminderSender{outcomes: outcomes}
	for n := 0; n < 5; n++ {
		dispatchTest(t, f, config, sender, now.Add(time.Duration(n)*2*time.Hour))
	}
	dispatchTest(t, f, config, sender, now.Add(20*time.Hour))
	if len(sender.messages) != 10 || mailStates(t, f)["failed"] != 2 {
		t.Fatal("retry limit failed", fmt.Sprint(mailStates(t, f)))
	}
}
