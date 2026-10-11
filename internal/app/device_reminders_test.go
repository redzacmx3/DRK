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

func reminderFixture(t *testing.T) (stockFixture, string) {
	t.Helper()
	f, id := controlFixture(t)
	input := controlProof()
	input.PerformedOn = "2020-03-15"
	requireStatus(t, packHTTP(f, "POST", "devices/"+id+"/commission", "reminder-commission", input), 200)
	requireStatus(t, packHTTP(f, "POST", "devices/"+id+"/rules", "reminder-rules", controlRules(1)), 200)
	return f, id
}
func reminderTime(day string) time.Time { t, _ := time.Parse(time.RFC3339, day+"T12:00:00Z"); return t }
func reminderCount(t *testing.T, f stockFixture, table string) int {
	t.Helper()
	var n int
	if err := f.db.QueryRow("SELECT count(*) FROM " + table).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}
func reconcileTest(t *testing.T, f stockFixture, day string) {
	t.Helper()
	if err := ReconcileDeviceReminders(context.Background(), f.db, reminderTime(day), []int{60, 30, 7}); err != nil {
		t.Fatal(err)
	}
}
func TestReminderThresholdsAndCivilStages(t *testing.T) {
	thresholds, err := ParseReminderThresholds("7,60,30")
	if err != nil || fmt.Sprint(thresholds) != "[60 30 7]" {
		t.Fatal(thresholds, err)
	}
	for _, invalid := range []string{"", "0", "-1", "366", "7,7", "30,", "7, x", "1,2,3,4,5,6,7,8,9,10,11,12,13"} {
		if _, err := ParseReminderThresholds(invalid); err == nil {
			t.Fatal(invalid)
		}
	}
	for _, test := range []struct {
		days, stage int
		want        bool
	}{{61, 366, false}, {60, 60, true}, {31, 60, true}, {30, 30, true}, {8, 30, true}, {7, 7, true}, {0, 0, true}, {-1, -1, true}, {-500, -1, true}} {
		got, ok := reminderStage(test.days, thresholds)
		if got != test.stage || ok != test.want {
			t.Fatal(test, got, ok)
		}
	}
	// Berlin has crossed midnight while UTC is still on the previous date.
	today, err := reminderDay(time.Date(2026, 10, 24, 22, 30, 0, 0, time.UTC))
	if err != nil || today.Format("2006-01-02") != "2026-10-25" {
		t.Fatal(today, err)
	}
}
func TestReminderCatchupDedupAndAtomicRollback(t *testing.T) {
	f, _ := reminderFixture(t)
	reconcileTest(t, f, "2022-01-29") //61 before March31: no reminders
	if reminderCount(t, f, "device_reminders") != 0 {
		t.Fatal("early reminder")
	}
	reconcileTest(t, f, "2022-01-30")
	reconcileTest(t, f, "2022-01-31")
	if reminderCount(t, f, "device_reminders") != 1 || reminderCount(t, f, "device_reminder_outbox") != 1 {
		t.Fatal("duplicate reminder")
	}
	var oldStamp string
	f.db.QueryRow(`SELECT last_success_at FROM device_reminder_worker`).Scan(&oldStamp)
	if _, err := f.db.Exec(`CREATE TRIGGER fail_reminder_outbox BEFORE INSERT ON device_reminder_outbox BEGIN SELECT RAISE(ABORT,'rollback'); END`); err != nil {
		t.Fatal(err)
	}
	if err := ReconcileDeviceReminders(context.Background(), f.db, reminderTime("2022-03-01"), []int{60, 30, 7}); err == nil {
		t.Fatal("failed outbox committed")
	}
	var stamp string
	f.db.QueryRow(`SELECT last_success_at FROM device_reminder_worker`).Scan(&stamp)
	var active int
	f.db.QueryRow(`SELECT count(*) FROM device_reminders WHERE active=1`).Scan(&active)
	if stamp != oldStamp || active != 1 || reminderCount(t, f, "device_reminders") != 1 {
		t.Fatal("partial reconciliation")
	}
	f.db.Exec(`DROP TRIGGER fail_reminder_outbox`)
	reconcileTest(t, f, "2022-03-25") // missed30day stage: catch up once at7, no burst
	reconcileTest(t, f, "2022-03-31")
	reconcileTest(t, f, "2022-04-01")
	reconcileTest(t, f, "2022-04-02")
	if reminderCount(t, f, "device_reminders") != 4 || reminderCount(t, f, "device_reminder_outbox") != 4 {
		t.Fatal("wrong stage or repeat")
	}
	f.db.QueryRow(`SELECT count(*) FROM device_reminders WHERE active=1 AND threshold_days=-1`).Scan(&active)
	if active != 1 {
		t.Fatal("overdue state", active)
	}
	for _, query := range []string{`UPDATE device_reminders SET due_on='2099-01-01'`, `DELETE FROM device_reminders`, `DELETE FROM device_reminder_outbox`} {
		if _, err := f.db.Exec(query); err == nil {
			t.Fatal("historical evidence mutable", query)
		}
	}
}
func TestReminderCurrentScopeCyclesAndRetirement(t *testing.T) {
	f, id := reminderFixture(t)
	reconcileTest(t, f, "2026-10-11")
	// Both required controls are overdue. No electrical exemption reminders.
	w := packHTTP(f, "GET", "devices/reminders", "", nil)
	requireStatus(t, w, 200)
	var board deviceReminderBoard
	if err := json.Unmarshal(w.Body.Bytes(), &board); err != nil {
		t.Fatal(err)
	}
	if len(board.Reminders) != 2 || board.LastWorkerSuccess == "" {
		t.Fatal(board)
	}
	if err := SetViewerSites(f.db, "viewer", nil); err != nil {
		t.Fatal(err)
	}
	f.cookie, f.csrf = loginTest(t, f.handler, nil)
	scoped := packHTTP(f, "GET", "devices/reminders", "", nil)
	requireStatus(t, scoped, 200)
	if strings.Contains(scoped.Body.String(), id) {
		t.Fatal("active reminder leaked")
	}
	if err := SetSiteRole(f.db, "viewer", []string{demoSite}, "device_manager"); err != nil {
		t.Fatal(err)
	}
	f.cookie, f.csrf = loginTest(t, f.handler, nil)
	before := reminderCount(t, f, "device_control_events")
	details := deviceControlInput{Evidence: "Synthetic edit", Note: "Synthetic edit"}
	details.ExpectedVersion = 2
	details.Details = &deviceDetails{Name: "Updated", Manufacturer: "Maker", Model: "Model", SerialOrLot: "Serial"}
	requireStatus(t, packHTTP(f, "POST", "devices/"+id+"/details", "reminder-detail", details), 200)
	reconcileTest(t, f, "2026-10-11")
	if reminderCount(t, f, "device_reminders") != 2 {
		t.Fatal("administrative change spam")
	}
	rules := controlRules(3)
	rules.Rules[0].IntervalMonths = 12
	requireStatus(t, packHTTP(f, "POST", "devices/"+id+"/rules", "reminder-new-rule", rules), 200)
	w = packHTTP(f, "GET", "devices/reminders", "", nil)
	requireStatus(t, w, 200)
	json.Unmarshal(w.Body.Bytes(), &board)
	if len(board.Reminders) != 0 {
		t.Fatal("old cycle visible before worker", board)
	}
	reconcileTest(t, f, "2026-10-11")
	if reminderCount(t, f, "device_reminders") != 4 {
		t.Fatal("new cycle missing")
	}
	if reminderCount(t, f, "device_control_events") != before+2 {
		t.Fatal("worker changed device history")
	}
	retired := deviceControlInput{Evidence: "Synthetic retirement", Note: "Synthetic retirement"}
	retired.ExpectedVersion = 4
	retired.PerformedOn = "2024-01-01"
	requireStatus(t, packHTTP(f, "POST", "devices/"+id+"/decommission", "reminder-retire", retired), 200)
	w = packHTTP(f, "GET", "devices/reminders", "", nil)
	requireStatus(t, w, 200)
	json.Unmarshal(w.Body.Bytes(), &board)
	if len(board.Reminders) != 0 {
		t.Fatal("retired device before worker", board)
	}
	reconcileTest(t, f, "2026-10-11")
	var active int
	f.db.QueryRow(`SELECT count(*) FROM device_reminders WHERE active=1`).Scan(&active)
	if active != 0 {
		t.Fatal(active)
	}
	if err := SetViewerSites(f.db, "viewer", nil); err != nil {
		t.Fatal(err)
	}
	f.cookie, f.csrf = loginTest(t, f.handler, nil)
	w = packHTTP(f, "GET", "devices/reminders", "", nil)
	requireStatus(t, w, 200)
	if strings.Contains(w.Body.String(), id) {
		t.Fatal("scope leak")
	}
	requireStatus(t, packHTTP(f, "GET", "devices/reminders?history=true", "", nil), 400)
}
func TestReminderConcurrentJobsAndCancellation(t *testing.T) {
	f, _ := reminderFixture(t)
	var wg sync.WaitGroup
	failures := make(chan error, 2)
	for n := 0; n < 2; n++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			failures <- ReconcileDeviceReminders(context.Background(), f.db, reminderTime("2026-10-11"), []int{60, 30, 7})
		}()
	}
	wg.Wait()
	close(failures)
	for err := range failures {
		if err != nil {
			t.Fatal(err)
		}
	}
	if reminderCount(t, f, "device_reminders") != 2 || reminderCount(t, f, "device_reminder_outbox") != 2 {
		t.Fatal("concurrent duplicates")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := ReconcileDeviceReminders(ctx, f.db, reminderTime("2026-10-12"), []int{60, 30, 7}); err == nil {
		t.Fatal("cancelled job committed")
	}
	done := make(chan struct{})
	go func() { RunDeviceReminderWorker(ctx, f.db, []int{60}, nil); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("worker did not stop")
	}
}

func TestReminderWorkerRefusesPartialDataset(t *testing.T) {
	f, _ := reminderFixture(t)
	reconcileTest(t, f, "2026-10-11")
	tx, err := f.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	statement, err := tx.Prepare(`INSERT INTO devices VALUES(?,?,?,?,?,?,?,?,?,?)`)
	if err != nil {
		t.Fatal(err)
	}
	for n := 0; n < 1000; n++ {
		if _, err = statement.Exec(fmt.Sprintf("reminder-large-%d", n), warehouse, fmt.Sprintf("REM-%d", n), "Synthetic", "Maker", "Model", "Serial", f.user, "2026-01-01T00:00:00Z", "Synthetic fixture"); err != nil {
			t.Fatal(err)
		}
	}
	statement.Close()
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err = ReconcileDeviceReminders(context.Background(), f.db, reminderTime("2026-10-12"), []int{60, 30, 7}); err == nil {
		t.Fatal("partial worker accepted")
	}
	var active int
	f.db.QueryRow(`SELECT count(*) FROM device_reminders WHERE active=1`).Scan(&active)
	if active != 2 || reminderCount(t, f, "device_reminder_outbox") != 2 {
		t.Fatal("failed job lost old queue")
	}
	var success string
	f.db.QueryRow(`SELECT last_success_at FROM device_reminder_worker`).Scan(&success)
	if success != reminderTime("2026-10-11").Format(time.RFC3339Nano) {
		t.Fatal("failed job claimed success")
	}
}
func TestReminderWorkerStartsAndStops(t *testing.T) {
	f, _ := reminderFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { defer close(done); RunDeviceReminderWorker(ctx, f.db, []int{60, 30, 7}, nil) }()
	deadline := time.Now().Add(3 * time.Second)
	for {
		var n int
		err := f.db.QueryRow(`SELECT count(*) FROM device_reminder_worker`).Scan(&n)
		if err != nil {
			t.Fatal(err)
		}
		if n == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("no startup job")
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("worker did not stop")
	}
	if reminderCount(t, f, "device_reminder_outbox") != 2 {
		t.Fatal("startup reminders missing")
	}
}
