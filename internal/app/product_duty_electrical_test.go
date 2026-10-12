package app

import (
	"context"
	"testing"
	"time"
)

func electricalStatus(t *testing.T, f stockFixture, id string) string {
	t.Helper()
	for _, c := range dutyRead(t, f, id).Checks {
		if c.Kind == "electrical_safety" {
			return c.Status
		}
	}
	t.Fatal("missing Electrical check")
	return ""
}
func TestProductDutyElectricalQualifiedAcceptanceAndInvalidation(t *testing.T) {
	t.Setenv("DRK_CLAMD_ADDRESS", fakeClamd(t, time.Now(), "stream: OK"))
	f, id, doc := documentFixture(t)
	reviewPost(t, f, id, doc.ID, "electrical-acceptance-scan", reviewProof(doc, "scan", 0), 200)
	reviewPost(t, f, id, doc.ID, "electrical-acceptance-release", reviewProof(doc, "release", 1), 200)
	commission := controlProof()
	commission.PerformedOn = "2024-01-01"
	requireStatus(t, packHTTP(f, "POST", "devices/"+id+"/commission", "electrical-acceptance-commission", commission), 200)
	rules := controlRules(1)
	rules.Rules[2] = DeadlineRule{ControlType: "electrical", Applicability: "required", Mode: "exact_date", ExactDate: time.Now().AddDate(1, 0, 0).UTC().Format("2006-01-02"), Evidence: "Synthetic confirmed electrical deadline"}
	requireStatus(t, packHTTP(f, "POST", "devices/"+id+"/rules", "electrical-acceptance-rules", rules), 200)
	inspection := controlProof()
	inspection.ExpectedVersion = 2
	inspection.PerformedOn = time.Now().UTC().Format("2006-01-02")
	inspection.ControlType = "electrical"
	inspection.Result = "passed"
	inspection.NextDueOn = rules.Rules[2].ExactDate
	inspection.Examiner = "Synthetic Electrical examiner"
	inspection.Method = "Synthetic Electrical procedure"
	requireStatus(t, packHTTP(f, "POST", "devices/"+id+"/inspections", "electrical-acceptance-inspection", inspection), 200)
	in := dutyProof(doc)
	in.ExpectedDeviceVersion = 3
	var rule *productDutyRule
	for i := range in.Rules {
		if in.Rules[i].Kind == "other" {
			rule = &in.Rules[i]
		}
	}
	rule.Applicability = "required"
	rule.Evidence = "Synthetic Electrical obligation"
	dutyPost(t, f, id, "electrical-acceptance-without-attestation", in, 200)
	if electricalStatus(t, f, id) != "unknown" {
		t.Fatal("inspection alone accepted")
	}
	a := &dutyElectricalAcceptance{dutyFunctionAcceptance: dutyFunctionAcceptance{Reviewed: true, Decision: "accepted", PerformedOn: inspection.PerformedOn, Qualification: "Synthetic reviewer", Assignment: "Synthetic assignment", Evidence: "Synthetic Electrical acceptance", Note: "Synthetic qualified review"}, InspectionVersion: 3, DocumentID: doc.ID, ExpectedSnapshot: bookSnapshot(t, f, id), ScopeEvidence: "Synthetic Electrical scope", ExecutorEvidence: "Synthetic examiner competence", MarkingEvidence: "Synthetic marking and deadline"}
	rule.ElectricalAcceptance = a
	in.ExpectedVersion = 1
	a.Reviewed = false
	dutyPost(t, f, id, "electrical-acceptance-unconfirmed", in, 400)
	a.Reviewed = true
	a.InspectionVersion = 2
	dutyPost(t, f, id, "electrical-acceptance-not-inspection", in, 400)
	a.InspectionVersion = 3
	a.MarkingEvidence = ""
	dutyPost(t, f, id, "electrical-acceptance-missing-marking", in, 400)
	a.MarkingEvidence = "Synthetic marking and deadline"
	saved := dutyPost(t, f, id, "electrical-acceptance-positive", in, 200)
	if saved.ElectricalBasis == nil || electricalStatus(t, f, id) != "accepted" || dutyRead(t, f, id).Overall != "not_assessed" {
		t.Fatal("missing qualified Electrical acceptance")
	}
	book, _ := bookArchive(t, f, id)
	found := false
	for _, c := range book.ProductDutyReview.Checks {
		if c.Kind == "electrical_safety" && c.Status == "accepted" {
			found = true
		}
	}
	if !found {
		t.Fatal("missing archive Electrical status")
	}
	tx, _ := f.db.Begin()
	record, err := readDevice(context.Background(), tx, f.user, id, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	deadline := record.Controls["electrical"]
	deadline.State = "OVERDUE"
	record.Controls["electrical"] = deadline
	check, err := checkDutyElectrical(context.Background(), tx, record, *rule, doc.ID, saved.ElectricalBasis)
	if err != nil || check.Status != "blocked" {
		t.Fatal("overdue Electrical accepted", check, err)
	}
	tx.Rollback()
	reviewPost(t, f, id, doc.ID, "electrical-acceptance-quarantine", reviewProof(doc, "quarantine", 2), 200)
	reviewPost(t, f, id, doc.ID, "electrical-acceptance-rescan", reviewProof(doc, "scan", 3), 200)
	reviewPost(t, f, id, doc.ID, "electrical-acceptance-rerelease", reviewProof(doc, "release", 4), 200)
	if electricalStatus(t, f, id) != "unknown" {
		t.Fatal("rerelease reactivated Electrical")
	}
	in.ExpectedVersion = 2
	dutyPost(t, f, id, "electrical-acceptance-stale-snapshot", in, 409)
	a.ExpectedSnapshot = bookSnapshot(t, f, id)
	dutyPost(t, f, id, "electrical-acceptance-renewed", in, 200)
	inspection.ExpectedVersion = 3
	inspection.Result = "failed"
	inspection.NextDueOn = ""
	requireStatus(t, packHTTP(f, "POST", "devices/"+id+"/inspections", "electrical-acceptance-new-failure", inspection), 200)
	if electricalStatus(t, f, id) != "blocked" {
		t.Fatal("new failed inspection block ignored")
	}
	in.ExpectedVersion = 3
	in.ExpectedDeviceVersion = 4
	a.ExpectedSnapshot = bookSnapshot(t, f, id)
	dutyPost(t, f, id, "electrical-acceptance-blocked-positive", in, 409)
	a.Decision = "rejected"
	a.InspectionVersion = 4
	dutyPost(t, f, id, "electrical-acceptance-rejected", in, 200)
	if electricalStatus(t, f, id) != "blocked" {
		t.Fatal("explicit rejection ignored")
	}
	dutyPost(t, f, id, "electrical-acceptance-positive", saved.Input, 200)
}
