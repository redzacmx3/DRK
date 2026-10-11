package app

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"
)

func TestDeviceAttentionCivilDayWindowsAndRetirement(t *testing.T) {
	today, _ := civilDate("2026-03-29") // DST start does not turn civil days into 23-hour intervals.
	record := registeredDevice(deviceInput{InventoryNumber: "SYNTH-1"}, "id", "actor", "time")
	record.CommissionedOn = "2020-01-01"
	record.Controls["STK"] = Deadline{State: "CURRENT", DueOn: "2026-05-28"} // inclusive60
	record.Controls["MTK"] = Deadline{State: "CURRENT", DueOn: "2026-05-29"} //61
	record.Controls["electrical"] = Deadline{State: "OVERDUE", DueOn: "2026-03-28"}
	item := deviceAttention(record, today, 60)
	if len(item.Reasons) != 2 || item.Reasons[0].Kind != "due" || *item.Reasons[0].DaysRemaining != 60 || item.Reasons[1].Kind != "overdue" || *item.Reasons[1].DaysRemaining != -1 {
		t.Fatal(item)
	}
	record.Controls["STK"] = Deadline{State: "CURRENT", DueOn: "2026-03-29"}
	item = deviceAttention(record, today, 0)
	if len(item.Reasons) != 2 || *item.Reasons[0].DaysRemaining != 0 || item.Reasons[0].Kind != "due" {
		t.Fatal(item)
	}
	record.Decommission = &decommissionProof{PerformedOn: "2026-03-01"}
	record.ActiveBlocks = []deviceBlock{{ID: "block", Reason: "quarantine", Version: 3, Note: "Synthetic review"}}
	record.Incidents = []incidentProof{{Version: 4, deviceIncident: deviceIncident{Description: "Synthetic incident"}, Report: &incidentReport{Status: "REPORT_PENDING"}}}
	item = deviceAttention(record, today, 365)
	if len(item.Reasons) != 2 || item.Reasons[0].Kind != "block" || item.Reasons[1].Kind != "report" {
		t.Fatal("retired reminders or missing open tasks", item)
	}
	record.ActiveBlocks = nil
	record.Incidents[0].Report.Status = "CLOSED"
	if len(deviceAttention(record, today, 365).Reasons) != 0 {
		t.Fatal("closed retired device still schedules controls")
	}
}
func TestDeviceAttentionScopedCurrentReadOnlyAndInvalidQueries(t *testing.T) {
	f, id := controlFixture(t)
	var before int
	f.db.QueryRow(`SELECT count(*) FROM device_control_events`).Scan(&before)
	w := packHTTP(f, "GET", "devices/attention?days=7", "", nil)
	requireStatus(t, w, 200)
	var board attentionBoard
	if err := json.Unmarshal(w.Body.Bytes(), &board); err != nil {
		t.Fatal(err)
	}
	if board.HorizonDays != 7 || board.ScannedDevices != 1 || len(board.Devices) != 1 || board.Devices[0].ID != id || board.Counts["unknown"] != 3 || board.Counts["not_commissioned"] != 1 || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal(board)
	}
	for _, query := range []string{"days=-1", "days=366", "days=bad", "days=7&days=30", "days=7&other=x", "other=x", "days="} {
		requireStatus(t, packHTTP(f, "GET", "devices/attention?"+query, "", nil), 400)
	}
	requireStatus(t, packHTTP(f, "POST", "devices/"+id+"/incidents", "attention-incident1", incidentInput(0)), 200)
	w = packHTTP(f, "GET", "devices/attention", "", nil)
	requireStatus(t, w, 200)
	json.Unmarshal(w.Body.Bytes(), &board)
	if board.Counts["report"] != 1 || board.Counts["block"] != 1 || board.Devices[0].Version != 1 {
		t.Fatal(board)
	}
	var after int
	f.db.QueryRow(`SELECT count(*) FROM device_control_events`).Scan(&after)
	if after != before+1 {
		t.Fatal("read created events")
	}
	if err := SetViewerSites(f.db, "viewer", []string{}); err != nil {
		t.Fatal(err)
	}
	f.cookie, f.csrf = loginTest(t, f.handler, nil)
	w = packHTTP(f, "GET", "devices/attention", "", nil)
	requireStatus(t, w, 200)
	json.Unmarshal(w.Body.Bytes(), &board)
	if board.ScannedDevices != 0 || len(board.Devices) != 0 || board.Counts["report"] != 0 {
		t.Fatal("scope leaked", board)
	}
}
func TestDeviceAttentionPriorityAndBerlinBoundary(t *testing.T) {
	now := time.Date(2026, 10, 24, 22, 30, 0, 0, time.UTC)
	zone, _ := time.LoadLocation("Europe/Berlin")
	today, _ := civilDate(now.In(zone).Format("2006-01-02"))
	record := registeredDevice(deviceInput{}, "id", "actor", "time")
	record.CommissionedOn = "2020-01-01"
	record.Controls = map[string]Deadline{"STK": {State: "CURRENT", DueOn: "2026-10-24"}, "MTK": {State: "NOT_APPLICABLE"}, "electrical": {State: "NOT_APPLICABLE"}}
	item := deviceAttention(record, today, 0)
	if len(item.Reasons) != 1 || *item.Reasons[0].DaysRemaining != -1 {
		t.Fatal(item)
	}
	if attentionPriority(attentionDevice{Reasons: []attentionReason{{Kind: "report"}}}) >= attentionPriority(item) {
		t.Fatal("pending reporting not prioritized")
	}
}

func TestDeviceAttentionRejectsPartialDataset(t *testing.T) {
	f, _ := controlFixture(t)
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
		if _, err = statement.Exec(fmt.Sprintf("attention-id-%d", n), warehouse, fmt.Sprintf("attention-inv-%d", n), "Synthetic", "Maker", "Model", "Serial", f.user, "2026-01-01T00:00:00Z", "Synthetic fixture"); err != nil {
			t.Fatal(err)
		}
	}
	statement.Close()
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	w := packHTTP(f, "GET", "devices/attention", "", nil)
	requireStatus(t, w, 413)
	var failure struct {
		Code string `json:"code"`
	}
	if err = json.Unmarshal(w.Body.Bytes(), &failure); err != nil {
		t.Fatal(err)
	}
	if failure.Code != "attention_too_large" {
		t.Fatal(w.Body.String())
	}
}
