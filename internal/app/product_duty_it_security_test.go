package app

import (
	"context"
	"testing"
	"time"
)

func securityDutyStatus(t *testing.T, f stockFixture, id string) string {
	t.Helper()
	for _, c := range dutyRead(t, f, id).Checks {
		if c.Kind == "it_security" {
			return c.Status
		}
	}
	t.Fatal("missing security check")
	return ""
}
func TestProductDutySecurityAcceptanceSnapshotAndRevocation(t *testing.T) {
	t.Setenv("DRK_CLAMD_ADDRESS", fakeClamd(t, time.Now(), "stream: OK"))
	f, id, doc := documentFixture(t)
	reviewPost(t, f, id, doc.ID, "security-acceptance-scan", reviewProof(doc, "scan", 0), 200)
	reviewPost(t, f, id, doc.ID, "security-acceptance-release", reviewProof(doc, "release", 1), 200)
	in := dutyProof(doc)
	var rule *productDutyRule
	for i := range in.Rules {
		if in.Rules[i].Kind == "it_security" {
			rule = &in.Rules[i]
		}
	}
	rule.Applicability = "required"
	rule.Evidence = "Synthetic book obligation"
	dutyPost(t, f, id, "security-acceptance-no-attestation", in, 200)
	if securityDutyStatus(t, f, id) != "unknown" {
		t.Fatal("archive presence inferred completeness")
	}
	yes := true
	a := &dutySecurityAcceptance{dutyFunctionAcceptance: dutyFunctionAcceptance{Reviewed: true, Decision: "accepted", PerformedOn: time.Now().UTC().Format("2006-01-02"), Qualification: "Synthetic reviewer", Assignment: "Synthetic assignment", Evidence: "Synthetic security review", Note: "Synthetic findings assessment"}, DocumentID: doc.ID, CheckPerformedOn: time.Now().UTC().Format("2006-01-02"), NextDueOn: time.Now().AddDate(0, 1, 0).UTC().Format("2006-01-02"), SoftwareVersion: "Synthetic software 1.0", SystemScope: "Synthetic device configuration", Method: "Synthetic external inspection", Examiner: "Synthetic examiner", ExaminerEvidence: "Synthetic examiner competence", FindingsEvidence: "Synthetic no remaining findings", MeasuresEvidence: "Synthetic measures fulfilled", MeasuresCompleted: &yes}
	a.ExpectedSnapshot = bookSnapshot(t, f, id)
	a.Result = "passed"
	rule.SecurityAcceptance = a
	in.ExpectedVersion = 1
	a.Reviewed = false
	dutyPost(t, f, id, "security-acceptance-unconfirmed", in, 400)
	a.Reviewed = true
	a.DocumentID = "00000000-0000-4000-8000-000000000099"
	dutyPost(t, f, id, "security-acceptance-foreign-document", in, 404)
	a.DocumentID = doc.ID
	a.FindingsEvidence = ""
	dutyPost(t, f, id, "security-acceptance-missing-retention", in, 400)
	a.FindingsEvidence = "Synthetic retention responsibility"
	a.MeasuresCompleted = nil
	dutyPost(t, f, id, "security-acceptance-no-measures-decision", in, 400)
	no := false
	a.MeasuresCompleted = &no
	dutyPost(t, f, id, "security-acceptance-open-measures", in, 400)
	a.MeasuresCompleted = &yes
	a.Result = "failed"
	dutyPost(t, f, id, "security-acceptance-failed-positive", in, 400)
	a.Result = "passed"
	saved := dutyPost(t, f, id, "security-acceptance-positive", in, 200)
	if saved.SecurityBasis == nil || saved.SecurityBasis.Snapshot == "" || securityDutyStatus(t, f, id) != "accepted" || dutyRead(t, f, id).Overall != "not_assessed" {
		t.Fatal("missing scoped attestation")
	}
	tx, _ := f.db.Begin()
	record, err := readDevice(context.Background(), tx, f.user, id, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	expired := productDutyCheck{Kind: "it_security", Status: "unknown"}
	err = checkDutySecurity(context.Background(), tx, record, *rule, doc.ID, saved.SecurityBasis, time.Now().AddDate(2, 0, 0), &expired)
	if err != nil || expired.Status != "blocked" {
		t.Fatal("IT deadline ignored", expired, err)
	}
	tx.Rollback()
	book, _ := bookArchive(t, f, id)
	found := false
	for _, c := range book.ProductDutyReview.Checks {
		if c.Kind == "it_security" && c.Status == "accepted" {
			found = true
		}
	}
	if !found {
		t.Fatal("book archive missing acceptance")
	}
	reviewPost(t, f, id, doc.ID, "security-acceptance-quarantine", reviewProof(doc, "quarantine", 2), 200)
	reviewPost(t, f, id, doc.ID, "security-acceptance-rescan", reviewProof(doc, "scan", 3), 200)
	reviewPost(t, f, id, doc.ID, "security-acceptance-rerelease", reviewProof(doc, "release", 4), 200)
	if securityDutyStatus(t, f, id) != "unknown" {
		t.Fatal("document rerelease revived stale book attestation")
	}
	in.ExpectedVersion = 2
	dutyPost(t, f, id, "security-acceptance-stale-snapshot", in, 409)
	a.ExpectedSnapshot = bookSnapshot(t, f, id)
	dutyPost(t, f, id, "security-acceptance-renewed", in, 200)
	requireStatus(t, packHTTP(f, "POST", "devices/"+id+"/maintenance", "security-acceptance-new-control", maintenanceInput(0, "completed")), 200)
	if securityDutyStatus(t, f, id) != "unknown" {
		t.Fatal("changed device book not invalidated")
	}
	a.ExpectedSnapshot = bookSnapshot(t, f, id)
	in.ExpectedDeviceVersion = 1
	in.ExpectedVersion = 3
	dutyPost(t, f, id, "security-acceptance-updated-book", in, 200)
	if securityDutyStatus(t, f, id) != "accepted" {
		t.Fatal("fresh book review missing")
	}
	policy := usageProof("policy", 0)
	policy.InstructionMode = "exempt"
	policy.Codes = []string{"basic_competence"}
	policy.ExpectedDeviceVersion = 1
	usagePost(t, f, id, "security-acceptance-new-person-policy", policy)
	if securityDutyStatus(t, f, id) != "unknown" {
		t.Fatal("person proof change not invalidated")
	}
	a.ExpectedSnapshot = bookSnapshot(t, f, id)
	in.ExpectedVersion = 4
	a.Decision = "rejected"
	dutyPost(t, f, id, "security-acceptance-rejected", in, 200)
	if securityDutyStatus(t, f, id) != "blocked" {
		t.Fatal("rejected book not blocked")
	}
	dutyPost(t, f, id, "security-acceptance-positive", saved.Input, 200)
}
