package app

import (
	"context"
	"testing"
	"time"
)

func TestMaintenanceAcceptanceSnapshotsAndDecisions(t *testing.T) {
	t.Setenv("DRK_CLAMD_ADDRESS", fakeClamd(t, time.Now(), "stream: OK"))
	f, id, document := documentFixture(t)
	reviewPost(t, f, id, document.ID, "maintenance-acceptance-scan", reviewProof(document, "scan", 0), 200)
	reviewPost(t, f, id, document.ID, "maintenance-acceptance-release", reviewProof(document, "release", 1), 200)
	m := maintenanceInput(0, "completed")
	m.MaintenanceKind = "maintenance"
	m.DocumentID = document.ID
	requireStatus(t, packHTTP(f, "POST", "devices/"+id+"/maintenance", "maintenance-acceptance-event", m), 200)
	in := maintenanceDuty(dutyProof(document), 1)
	in.ExpectedDeviceVersion = 1
	var rule *productDutyRule
	for i := range in.Rules {
		if in.Rules[i].Kind == "manufacturer_maintenance" {
			rule = &in.Rules[i]
		}
	}
	a := &dutyMaintenanceAcceptance{Reviewed: true, Decision: "accepted", PerformedOn: time.Now().UTC().Format("2006-01-02"), NextDueOn: time.Now().AddDate(0, 1, 0).UTC().Format("2006-01-02"), Qualification: "Synthetic competence", Assignment: "Synthetic assignment", Evidence: "Synthetic approval", Note: "Synthetic decision", IntervalEvidence: "Synthetic manufacturer interval", ScopeEvidence: "Synthetic scope", ExecutorEvidence: "Synthetic executor competence"}
	rule.MaintenanceAcceptance = a
	a.Reviewed = false
	dutyPost(t, f, id, "maintenance-acceptance-unchecked", in, 400)
	a.Reviewed = true
	a.ScopeEvidence = ""
	dutyPost(t, f, id, "maintenance-acceptance-incomplete", in, 400)
	a.ScopeEvidence = "Synthetic scope"
	dutyPost(t, f, id, "maintenance-acceptance-positive", in, 200)
	if maintenanceDutyStatus(t, f, id) != "accepted" || dutyRead(t, f, id).Overall != "not_assessed" {
		t.Fatal("missing scoped approval")
	}
	book, _ := bookArchive(t, f, id)
	if book.ProductDutyReview.Checks == nil {
		t.Fatal("missing archive assessment")
	}
	reviewPost(t, f, id, document.ID, "maintenance-acceptance-quarantine", reviewProof(document, "quarantine", 2), 200)
	if dutyRead(t, f, id).Status != "unknown" {
		t.Fatal("withdrawal ignored")
	}
	reviewPost(t, f, id, document.ID, "maintenance-acceptance-rescan", reviewProof(document, "scan", 3), 200)
	reviewPost(t, f, id, document.ID, "maintenance-acceptance-rerelease", reviewProof(document, "release", 4), 200)
	if maintenanceDutyStatus(t, f, id) != "unknown" {
		t.Fatal("rerelease restored stale approval")
	}
	in.ExpectedVersion = 1
	dutyPost(t, f, id, "maintenance-acceptance-renew", in, 200)
	if maintenanceDutyStatus(t, f, id) != "accepted" {
		t.Fatal("renewal missing")
	}
	basis := dutyRead(t, f, id).Events[0].MaintenanceBasis
	tx, _ := f.db.Begin()
	record, err := readDevice(context.Background(), tx, f.user, id, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	// A passed deadline blocks an otherwise current approved proof.
	a.NextDueOn = "2000-01-01"
	checks, err := checkProductDutyProofs(context.Background(), tx, record, []productDutyRule{*rule}, "", nil, basis)
	if err != nil || checks[0].Status != "blocked" {
		t.Fatal("deadline ignored", checks, err)
	}
	a.NextDueOn = time.Now().AddDate(0, 1, 0).UTC().Format("2006-01-02")
	// Failed outcomes remain blocked even after a correction.
	record.Maintenances[0].Result = "failed"
	record.Corrections = append(record.Corrections, correctionProof{})
	record.Corrections[len(record.Corrections)-1].Target.Version = 1
	checks, err = checkProductDutyProofs(context.Background(), tx, record, []productDutyRule{*rule}, "", nil, basis)
	if err != nil || checks[0].Status != "blocked" {
		t.Fatal("failed proof downgraded", checks, err)
	}
	tx.Rollback()
	in.ExpectedVersion = 2
	a.Decision = "rejected"
	dutyPost(t, f, id, "maintenance-acceptance-negative", in, 200)
	if maintenanceDutyStatus(t, f, id) != "blocked" {
		t.Fatal("rejection ignored")
	}
	in.ExpectedVersion = 3
	a.Decision = "accepted"
	a.NextDueOn = "2000-01-01"
	dutyPost(t, f, id, "maintenance-acceptance-old-due", in, 400)
}
