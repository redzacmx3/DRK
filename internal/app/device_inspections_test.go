package app

import (
	"encoding/json"
	"testing"
	"time"
)

func inspectedFixture(t *testing.T) (stockFixture, string) {
	f, id := controlFixture(t)
	proof := controlProof()
	proof.PerformedOn = "2020-03-15"
	requireStatus(t, packHTTP(f, "POST", "devices/"+id+"/commission", "inspection-commission", proof), 200)
	requireStatus(t, packHTTP(f, "POST", "devices/"+id+"/rules", "inspection-rule-basis", controlRules(1)), 200)
	return f, id
}
func deviceResponse(t *testing.T, f stockFixture, id, action, key string, input deviceControlInput) deviceRecord {
	t.Helper()
	w := packHTTP(f, "POST", "devices/"+id+"/"+action, key, input)
	requireStatus(t, w, 200)
	var record deviceRecord
	if err := json.Unmarshal(w.Body.Bytes(), &record); err != nil {
		t.Fatal(err)
	}
	return record
}
func TestInspectionAndIndependentBlocks(t *testing.T) {
	f, id := inspectedFixture(t)
	proof := controlProof()
	proof.ExpectedVersion = 2
	proof.BlockReason = "recall"
	record := deviceResponse(t, f, id, "defects", "recall-device-proof", proof)
	recall := record.ActiveBlocks[0].ID
	proof = controlProof()
	proof.ExpectedVersion = 3
	proof.ControlType = "STK"
	proof.PerformedOn = "2020-03-16"
	proof.Examiner = "Synthetic examiner/company"
	proof.Method = "Synthetic method and protocol reference"
	proof.Result = "failed"
	record = deviceResponse(t, f, id, "inspections", "failed-inspection-proof", proof)
	failed := record.ActiveBlocks[1].ID
	if record.Controls["STK"].BasisDate != "2020-03-15" || len(record.ActiveBlocks) != 2 {
		t.Fatal(record)
	}
	release := controlProof()
	release.ExpectedVersion = 4
	release.BlockID = failed
	requireStatus(t, packHTTP(f, "POST", "devices/"+id+"/releases", "cannot-release-failure", release), 409)
	proof.ExpectedVersion = 4
	proof.Result = "passed"
	tz, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Fatal(err)
	}
	proof.PerformedOn = time.Now().In(tz).Format("2006-01-02")
	w := packHTTP(f, "POST", "devices/"+id+"/inspections", "passed-inspection-proof", proof)
	requireStatus(t, w, 200)
	json.Unmarshal(w.Body.Bytes(), &record)
	if record.Controls["STK"].BasisDate != proof.PerformedOn || len(record.ActiveBlocks) != 2 || record.Readiness != "not_assessed" {
		t.Fatal(record)
	}
	release.ExpectedVersion = 5
	record = deviceResponse(t, f, id, "releases", "release-only-failure", release)
	if len(record.ActiveBlocks) != 1 || record.ActiveBlocks[0].ID != recall {
		t.Fatal(record)
	}
	proofReplay := packHTTP(f, "POST", "devices/"+id+"/inspections", "passed-inspection-proof", proof)
	requireStatus(t, proofReplay, 200)
	if proofReplay.Body.String() != w.Body.String() {
		t.Fatal("changed replay")
	}
	rules := controlRules(6)
	record = deviceResponse(t, f, id, "rules", "new-rule-after-inspection", rules)
	if record.Controls["STK"].BasisDate != proof.PerformedOn {
		t.Fatal("rule revision lost latest passed inspection", record)
	}
	release.ExpectedVersion = 7
	release.BlockID = recall
	record = deviceResponse(t, f, id, "releases", "release-only-recall", release)
	if len(record.ActiveBlocks) != 0 || record.Readiness != "not_assessed" {
		t.Fatal(record)
	}
	requireStatus(t, packHTTP(f, "POST", "devices/"+id+"/releases", "duplicate-block-release", release), 409)
}
func TestHelperDefectRightsAndInspectionValidation(t *testing.T) {
	f, id := inspectedFixture(t)
	proof := controlProof()
	proof.ExpectedVersion = 2
	proof.ControlType = "STK"
	proof.PerformedOn = "2020-03-16"
	proof.Result = "passed"
	proof.Examiner = "Examiner"
	proof.Method = "Method"
	for _, mutate := range []func(*deviceControlInput){
		func(p *deviceControlInput) { p.PerformedOn = "9999-01-01" }, func(p *deviceControlInput) { p.PerformedOn = "2020-03-14" }, func(p *deviceControlInput) { p.Examiner = "" }, func(p *deviceControlInput) { p.Method = "" }, func(p *deviceControlInput) { p.Result = "" }, func(p *deviceControlInput) { p.ControlType = "electrical" }, func(p *deviceControlInput) { p.NextDueOn = "2030-01-01" },
	} {
		bad := proof
		mutate(&bad)
		w := packHTTP(f, "POST", "devices/"+id+"/inspections", "invalid-inspection-proof", bad)
		if w.Code != 400 && w.Code != 409 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	if _, err := f.db.Exec(`CREATE TRIGGER reject_inspection_outbox BEFORE INSERT ON device_control_outbox BEGIN SELECT RAISE(ABORT,'test rollback'); END`); err != nil {
		t.Fatal(err)
	}
	requireStatus(t, packHTTP(f, "POST", "devices/"+id+"/inspections", "rolled-back-inspection", proof), 503)
	var count int
	f.db.QueryRow(`SELECT COUNT(*) FROM device_control_events`).Scan(&count)
	if count != 2 {
		t.Fatal(count)
	}
	f.db.Exec(`DROP TRIGGER reject_inspection_outbox`)
	if err := SetSiteRole(f.db, "viewer", []string{demoSite}, "helper"); err != nil {
		t.Fatal(err)
	}
	f.cookie, f.csrf = loginTest(t, f.handler, nil)
	requireStatus(t, packHTTP(f, "POST", "devices/"+id+"/inspections", "helper-cannot-inspect", proof), 404)
	defect := deviceControlInput{ExpectedVersion: 2, BlockReason: "defect", Evidence: "Synthetic observed defect", Note: "Synthetic malfunction report"}
	record := deviceResponse(t, f, id, "defects", "helper-defect-report", defect)
	if len(record.ActiveBlocks) != 1 {
		t.Fatal(record)
	}
	defect.ExpectedVersion = 3
	defect.BlockReason = "recall"
	requireStatus(t, packHTTP(f, "POST", "devices/"+id+"/defects", "helper-cannot-recall", defect), 404)
	release := controlProof()
	release.ExpectedVersion = 3
	release.BlockID = record.ActiveBlocks[0].ID
	requireStatus(t, packHTTP(f, "POST", "devices/"+id+"/releases", "helper-cannot-release", release), 404)
}

func TestInspectionCannotEraseEarlierDeadlineOrBackdate(t *testing.T) {
	f, id := inspectedFixture(t)
	rules := controlRules(2)
	rules.Rules[0].EarlierDate = "2020-03-31"
	rules.Rules[0].EarlierEvidence = "Documented occasion deadline"
	deviceResponse(t, f, id, "rules", "shorter-inspection-rule", rules)
	proof := controlProof()
	proof.ExpectedVersion = 3
	proof.ControlType = "STK"
	proof.PerformedOn = "2020-04-01"
	proof.Examiner = "Examiner"
	proof.Method = "Method"
	proof.Result = "passed"
	record := deviceResponse(t, f, id, "inspections", "passed-shorter-rule", proof)
	if record.Controls["STK"].DueOn != "2020-03-31" {
		t.Fatal("occasion deadline erased", record)
	}
	proof.ExpectedVersion = 4
	proof.PerformedOn = "2021-04-01"
	proof.Result = "failed"
	record = deviceResponse(t, f, id, "inspections", "later-failed-inspection", proof)
	proof.ExpectedVersion = 5
	proof.PerformedOn = "2020-04-02"
	proof.Result = "passed"
	requireStatus(t, packHTTP(f, "POST", "devices/"+id+"/inspections", "backdated-after-failure", proof), 400)
	release := controlProof()
	release.ExpectedVersion = 5
	release.BlockID = record.ActiveBlocks[0].ID
	requireStatus(t, packHTTP(f, "POST", "devices/"+id+"/releases", "old-passed-proof-release", release), 409)
}

func TestExactInspectionRequiresExplicitNextDate(t *testing.T) {
	f, id := inspectedFixture(t)
	rules := controlRules(2)
	rules.Rules[2] = DeadlineRule{ControlType: "electrical", Applicability: "required", Mode: "exact_date", ExactDate: "2030-01-01", Evidence: "Explicit product electrical schedule"}
	deviceResponse(t, f, id, "rules", "exact-electrical-rule", rules)
	proof := controlProof()
	proof.ExpectedVersion = 3
	proof.ControlType = "electrical"
	proof.PerformedOn = "2021-01-01"
	proof.Examiner = "Examiner"
	proof.Method = "Method"
	proof.Result = "passed"
	requireStatus(t, packHTTP(f, "POST", "devices/"+id+"/inspections", "missing-exact-next-date", proof), 400)
	proof.NextDueOn = "2020-12-31"
	requireStatus(t, packHTTP(f, "POST", "devices/"+id+"/inspections", "invalid-exact-next-date", proof), 400)
	proof.NextDueOn = "2031-01-01"
	record := deviceResponse(t, f, id, "inspections", "explicit-exact-next-date", proof)
	if record.Controls["electrical"].DueOn != "2031-01-01" || record.Controls["electrical"].BasisDate != "2021-01-01" {
		t.Fatal(record)
	}
}
