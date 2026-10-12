package app

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"
	"time"
)

func maintenanceDuty(in productDutyInput, version int) productDutyInput {
	for i := range in.Rules {
		if in.Rules[i].Kind == "manufacturer_maintenance" {
			in.Rules[i].Applicability = "required"
			in.Rules[i].Evidence = "Synthetic manufacturer maintenance requirement"
			in.Rules[i].EventVersion = version
		}
	}
	return in
}
func maintenanceDutyStatus(t *testing.T, f stockFixture, id string) string {
	t.Helper()
	assessment := dutyRead(t, f, id)
	for _, check := range assessment.Checks {
		if check.Kind == "manufacturer_maintenance" {
			return check.Status
		}
	}
	t.Fatal("missing maintenance check", assessment)
	return ""
}
func TestProductDutyMaintenanceReferenceResolutionAndInvalidation(t *testing.T) {
	t.Setenv("DRK_CLAMD_ADDRESS", fakeClamd(t, time.Now(), "stream: OK"))
	f, id, document := documentFixture(t)
	reviewPost(t, f, id, document.ID, "linked-maintenance-scan", reviewProof(document, "scan", 0), 200)
	reviewPost(t, f, id, document.ID, "linked-maintenance-release", reviewProof(document, "release", 1), 200)
	in := maintenanceDuty(dutyProof(document), 0)
	dutyPost(t, f, id, "maintenance-freetext-only", in, 200)
	if maintenanceDutyStatus(t, f, id) != "unknown" {
		t.Fatal("free text resolved")
	}
	maintenance := maintenanceInput(0, "completed")
	maintenance.MaintenanceKind = "maintenance"
	maintenance.DocumentID = document.ID
	w := packHTTP(f, "POST", "devices/"+id+"/maintenance", "linked-maintenance-event", maintenance)
	requireStatus(t, w, 200)
	var record deviceRecord
	json.Unmarshal(w.Body.Bytes(), &record)
	in = maintenanceDuty(in, 1)
	in.ExpectedVersion = 1
	in.ExpectedDeviceVersion = record.Version
	dutyPost(t, f, id, "linked-maintenance-review", in, 200)
	if maintenanceDutyStatus(t, f, id) != "record_found" || dutyRead(t, f, id).Overall != "not_assessed" {
		t.Fatal("record not resolved or false release")
	}
	book, files := bookArchive(t, f, id)
	if book.ProductDutyReview == nil || book.ProductDutyReview.Overall != "not_assessed" {
		t.Fatal("missing scoped prerequisite report")
	}
	found := false
	for _, check := range book.ProductDutyReview.Checks {
		if check.Kind == "manufacturer_maintenance" && check.Status == "record_found" && check.EventVersion == 1 {
			found = true
		}
	}
	if !found || !bytes.Contains(files["geraeteakte.html"], []byte("Aktuelle Nachweisvorprüfung")) {
		t.Fatal("missing current JSON/HTML proof report")
	}
	tx, _ := f.db.Begin()
	record, err := readDevice(context.Background(), tx, f.user, id, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	// A bound hash is checked against the currently released immutable document.
	changed := record
	changed.Maintenances = append([]maintenanceProof{}, record.Maintenances...)
	changed.Maintenances[0].DocumentSHA256 = "changed"
	checks, err := checkProductDutyProofs(context.Background(), tx, changed, in.Rules, "", nil)
	tx.Rollback()
	if err != nil {
		t.Fatal(err)
	}
	for _, check := range checks {
		if check.Kind == "manufacturer_maintenance" && check.Status != "unknown" {
			t.Fatal("hash drift accepted")
		}
	}
	reviewPost(t, f, id, document.ID, "linked-maintenance-quarantine", reviewProof(document, "quarantine", 2), 200)
	// A withdrawn product basis prevents positive checks altogether.
	if got := dutyRead(t, f, id); got.Status != "unknown" || len(got.Checks) != 0 {
		t.Fatal(got)
	}
	reviewPost(t, f, id, document.ID, "linked-maintenance-rescan", reviewProof(document, "scan", 3), 200)
	reviewPost(t, f, id, document.ID, "linked-maintenance-rerelease", reviewProof(document, "release", 4), 200)
	newer := maintenanceInput(1, "follow_up_required")
	newer.MaintenanceKind = "maintenance"
	newer.DocumentID = document.ID
	requireStatus(t, packHTTP(f, "POST", "devices/"+id+"/maintenance", "newer-maintenance-event", newer), 200)
	if maintenanceDutyStatus(t, f, id) != "unknown" {
		t.Fatal("superseded proof accepted")
	}
	in = maintenanceDuty(in, 2)
	in.ExpectedVersion = 2
	in.ExpectedDeviceVersion = 2
	dutyPost(t, f, id, "linked-failed-maintenance", in, 200)
	if maintenanceDutyStatus(t, f, id) != "blocked" {
		t.Fatal("follow-up accepted")
	}
	prepared := packAssessment{Material: materialReadiness{CurrentVersion: true}, Devices: []deviceAssessment{{ProductDuties: &productDutyAssessment{Status: "reviewed", Checks: []productDutyCheck{{Kind: "manufacturer_maintenance", Status: "blocked"}}}}}}
	for _, check := range consolidateEvidence(prepared).Components {
		if check.Kind == "product_duties" && check.Status != "blocked" {
			t.Fatal("missing consolidated block")
		}
	}
	// A later completed event does not silently release the previous device block.
	newer = maintenanceInput(2, "completed")
	newer.MaintenanceKind = "maintenance"
	newer.DocumentID = document.ID
	requireStatus(t, packHTTP(f, "POST", "devices/"+id+"/maintenance", "completed-with-block", newer), 200)
	in = maintenanceDuty(in, 3)
	in.ExpectedVersion = 3
	in.ExpectedDeviceVersion = 3
	dutyPost(t, f, id, "linked-blocked-device", in, 200)
	if maintenanceDutyStatus(t, f, id) != "blocked" {
		t.Fatal("open independent block ignored")
	}
}
func TestProductDutyMaintenanceBindingRejectsWrongEventTypeAndCorrection(t *testing.T) {
	t.Setenv("DRK_CLAMD_ADDRESS", fakeClamd(t, time.Now(), "stream: OK"))
	f, id, document := documentFixture(t)
	reviewPost(t, f, id, document.ID, "binding-scan", reviewProof(document, "scan", 0), 200)
	reviewPost(t, f, id, document.ID, "binding-release", reviewProof(document, "release", 1), 200)
	repair := maintenanceInput(0, "completed")
	repair.DocumentID = document.ID
	requireStatus(t, packHTTP(f, "POST", "devices/"+id+"/maintenance", "binding-repair", repair), 200)
	in := maintenanceDuty(dutyProof(document), 1)
	in.ExpectedDeviceVersion = 1
	dutyPost(t, f, id, "repair-is-not-maintenance", in, 400)
	in = maintenanceDuty(in, 99)
	dutyPost(t, f, id, "foreign-event-not-resolved", in, 400)
	in = maintenanceDuty(in, -1)
	dutyPost(t, f, id, "negative-event-version", in, 400)
	in = maintenanceDuty(in, 0)
	in.Rules[0].EventVersion = 1
	dutyPost(t, f, id, "unsupported-duty-binding", in, 400)
	in.Rules[0].EventVersion = 0
	maintained := maintenanceInput(1, "completed")
	maintained.MaintenanceKind = "maintenance"
	requireStatus(t, packHTTP(f, "POST", "devices/"+id+"/maintenance", "binding-maintenance-no-doc", maintained), 200)
	in = maintenanceDuty(in, 2)
	in.ExpectedDeviceVersion = 2
	dutyPost(t, f, id, "binding-no-document", in, 200)
	if maintenanceDutyStatus(t, f, id) != "unknown" {
		t.Fatal("missing document accepted")
	}
	maintained.ExpectedVersion = 2
	maintained.DocumentID = document.ID
	requireStatus(t, packHTTP(f, "POST", "devices/"+id+"/maintenance", "binding-maintenance-doc", maintained), 200)
	in = maintenanceDuty(in, 3)
	in.ExpectedDeviceVersion = 3
	in.ExpectedVersion = 1
	dutyPost(t, f, id, "binding-with-document", in, 200)
	if maintenanceDutyStatus(t, f, id) != "record_found" {
		t.Fatal("complete record not resolved")
	}
	correction := controlProof()
	correction.ExpectedVersion = 3
	correction.Correction = &deviceCorrection{TargetVersion: 3, Statement: "Synthetic material correction to maintenance scope"}
	requireStatus(t, packHTTP(f, "POST", "devices/"+id+"/corrections", "correct-linked-maintenance", correction), 200)
	if maintenanceDutyStatus(t, f, id) != "unknown" {
		t.Fatal("corrected reference accepted")
	}
	// Existing schema28 records lacking event_version are valid but cannot resolve a proof.
	in = maintenanceDuty(in, 0)
	in.ExpectedDeviceVersion = 4
	in.ExpectedVersion = 2
	dutyPost(t, f, id, "legacy-unlinked-reference", in, 200)
	if maintenanceDutyStatus(t, f, id) != "unknown" {
		t.Fatal("legacy reference auto-bound")
	}
}
