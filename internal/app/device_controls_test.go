package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"testing"
	"time"
)

func controlFixture(t *testing.T) (stockFixture, string) {
	t.Helper()
	f := stockDB(t)
	if err := SetSiteRole(f.db, "viewer", []string{demoSite}, "device_manager"); err != nil {
		t.Fatal(err)
	}
	f.cookie, f.csrf = loginTest(t, f.handler, nil)
	w := packHTTP(f, "POST", "devices", "control-device-registration", deviceInput{LocationID: warehouse, InventoryNumber: "CTRL-1", Name: "Demo", Manufacturer: "Demo", Model: "Demo", SerialOrLot: "Demo", Note: "synthetic fixture"})
	requireStatus(t, w, 200)
	var record deviceRecord
	if err := json.Unmarshal(w.Body.Bytes(), &record); err != nil {
		t.Fatal(err)
	}
	return f, record.ID
}
func controlProof() deviceControlInput {
	return deviceControlInput{Evidence: "protocol reference", Qualification: "qualification reference", Assignment: "formal assignment reference", Note: "synthetic documented assessment"}
}
func controlRules(version int) deviceControlInput {
	input := controlProof()
	input.ExpectedVersion = version
	input.Rules = []DeadlineRule{
		{ControlType: "STK", Applicability: "required", Mode: "month_end", IntervalMonths: 24, Evidence: "product STK assessment"},
		{ControlType: "MTK", Applicability: "required", Mode: "year_end", IntervalMonths: 24, Evidence: "product MTK assessment"},
		{ControlType: "electrical", Applicability: "exempt", Evidence: "explicit electrical exemption"},
	}
	return input
}
func TestDeviceCommissionRulesHistoryAndReplay(t *testing.T) {
	f, id := controlFixture(t)
	path := "devices/" + id
	input := controlProof()
	input.PerformedOn = "2020-03-15"
	requireStatus(t, packHTTP(f, "POST", path+"/rules", "no-commission-rules", controlRules(0)), 409)
	first := packHTTP(f, "POST", path+"/commission", "commission-proof-key", input)
	requireStatus(t, first, 200)
	var record deviceRecord
	json.Unmarshal(first.Body.Bytes(), &record)
	if record.Version != 1 || record.Lifecycle != "IN_SERVICE" || record.Readiness != "not_assessed" || record.Controls["STK"].State != "UNKNOWN" {
		t.Fatal(record)
	}
	requireStatus(t, packHTTP(f, "POST", path+"/commission", "commission-again-key", input), 409)
	rules := controlRules(1)
	w := packHTTP(f, "POST", path+"/rules", "control-rules-key", rules)
	requireStatus(t, w, 200)
	json.Unmarshal(w.Body.Bytes(), &record)
	if record.Version != 2 || record.Controls["STK"].DueOn != "2022-03-31" || record.Controls["MTK"].DueOn != "2022-12-31" || record.Controls["electrical"].State != "NOT_APPLICABLE" || record.Readiness != "not_assessed" {
		t.Fatal(record)
	}
	requireStatus(t, packHTTP(f, "POST", path+"/rules", "stale-rules-key", rules), 409)
	rules.ExpectedVersion = 2
	rules.Rules[0].IntervalMonths = 12
	requireStatus(t, packHTTP(f, "POST", path+"/rules", "new-control-rules", rules), 200)
	rules = controlRules(1)
	replay := packHTTP(f, "POST", path+"/rules", "control-rules-key", rules)
	requireStatus(t, replay, 200)
	if replay.Body.String() != w.Body.String() {
		t.Fatal("historical response changed")
	}
	replay = packHTTP(f, "POST", path+"/commission", "commission-proof-key", input)
	requireStatus(t, replay, 200)
	if replay.Body.String() != first.Body.String() {
		t.Fatal("commission response changed")
	}
	history := packHTTP(f, "GET", path+"/events", "", nil)
	requireStatus(t, history, 200)
	var events []map[string]any
	json.Unmarshal(history.Body.Bytes(), &events)
	if len(events) != 3 {
		t.Fatal(history.Body.String())
	}
	tx, err := f.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	projected, err := readDevice(context.Background(), tx, f.user, id, time.Date(2035, 1, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if projected.Controls["STK"].State != "OVERDUE" {
		t.Fatal(projected)
	}
}
func TestDeviceControlValidationAndRollback(t *testing.T) {
	f, id := controlFixture(t)
	path := "devices/" + id
	input := controlProof()
	input.PerformedOn = "9999-01-01"
	requireStatus(t, packHTTP(f, "POST", path+"/commission", "future-commission", input), 400)
	input.PerformedOn = "2020-02-30"
	requireStatus(t, packHTTP(f, "POST", path+"/commission", "bad-commission-date", input), 400)
	input.PerformedOn = "2020-03-15"
	input.Qualification = ""
	requireStatus(t, packHTTP(f, "POST", path+"/commission", "no-qualification", input), 400)
	input = controlProof()
	input.PerformedOn = "2020-03-15"
	requireStatus(t, packHTTP(f, "POST", path+"/commission", "valid-commission", input), 200)
	for _, mutate := range []func(*deviceControlInput){
		func(in *deviceControlInput) { in.Rules[0].IntervalMonths = 25 },
		func(in *deviceControlInput) { in.Rules[0].BasisDate = "2025-01-01" },
		func(in *deviceControlInput) { in.Rules[0].Version = "999" },
		func(in *deviceControlInput) {
			in.Rules[0].EarlierDate = "2030-01-01"
			in.Rules[0].EarlierEvidence = "extension"
		},
		func(in *deviceControlInput) { in.Rules[1].Mode = "month_end" },
		func(in *deviceControlInput) { in.Rules[2].Evidence = "" },
		func(in *deviceControlInput) { in.Rules[2].ControlType = "STK" },
		func(in *deviceControlInput) { in.Rules[2].Applicability = "unknown"; in.Rules[2].Mode = "exact_date" },
	} {
		rules := controlRules(1)
		mutate(&rules)
		requireStatus(t, packHTTP(f, "POST", path+"/rules", "invalid-rule-proof", rules), 400)
	}
	if _, err := f.db.Exec(`CREATE TRIGGER reject_control_outbox BEFORE INSERT ON device_control_outbox BEGIN SELECT RAISE(ABORT,'test rollback'); END`); err != nil {
		t.Fatal(err)
	}
	requireStatus(t, packHTTP(f, "POST", path+"/rules", "atomic-control-proof", controlRules(1)), 503)
	var count int
	f.db.QueryRow(`SELECT COUNT(*) FROM device_control_events`).Scan(&count)
	if count != 1 {
		t.Fatal(count)
	}
	if _, err := f.db.Exec(`UPDATE device_control_events SET action='rules'`); err == nil {
		t.Fatal("mutable control history")
	}
	if err := SetSiteRole(f.db, "viewer", []string{demoSite}, "material_manager"); err != nil {
		t.Fatal(err)
	}
	f.cookie, f.csrf = loginTest(t, f.handler, nil)
	requireStatus(t, packHTTP(f, "POST", path+"/rules", "material-cannot-control", controlRules(1)), 404)
	requireStatus(t, packHTTP(f, "GET", path+"/book", "", nil), 200)
	if err := SetSiteRole(f.db, "viewer", nil, "viewer"); err != nil {
		t.Fatal(err)
	}
	f.cookie, f.csrf = loginTest(t, f.handler, nil)
	requireStatus(t, packHTTP(f, "GET", path+"/events", "", nil), 404)
}

func TestConcurrentControlVersions(t *testing.T) {
	f, id := controlFixture(t)
	input := controlProof()
	input.PerformedOn = "2020-03-15"
	requireStatus(t, packHTTP(f, "POST", "devices/"+id+"/commission", "concurrent-commission", input), 200)
	other := secondDB(t, f.db)
	results := make(chan error, 2)
	for i, db := range []*sql.DB{f.db, other} {
		go func(db *sql.DB, i int) {
			_, err := controlDevice(context.Background(), db, f.user, digest(f.cookie.Value), fmt.Sprintf("concurrent-control-%d", i), id, "rules", controlRules(1), time.Now())
			results <- err
		}(db, i)
	}
	success, conflicts := 0, 0
	for range 2 {
		err := <-results
		if err == nil {
			success++
		} else if e, ok := err.(*stockError); ok && e.Code == "device_version_conflict" {
			conflicts++
		} else {
			t.Fatal(err)
		}
	}
	if success != 1 || conflicts != 1 {
		t.Fatal(success, conflicts)
	}
	requireStatus(t, packHTTP(f, "POST", "devices/"+id+"/rules", "control-device-registration", controlRules(2)), 409)
}
