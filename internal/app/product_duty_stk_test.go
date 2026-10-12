package app

import (
	"context"
	"testing"
	"time"
)

func stkStatus(t *testing.T, f stockFixture, id string) string {
	t.Helper()
	for _, c := range dutyRead(t, f, id).Checks {
		if c.Kind == "annex_1_stk" {
			return c.Status
		}
	}
	t.Fatal("missing STK check")
	return ""
}
func TestProductDutySTKQualifiedAcceptanceAndInvalidation(t *testing.T) {
	t.Setenv("DRK_CLAMD_ADDRESS", fakeClamd(t, time.Now(), "stream: OK"))
	f, id, doc := documentFixture(t)
	reviewPost(t, f, id, doc.ID, "stk-acceptance-scan", reviewProof(doc, "scan", 0), 200)
	reviewPost(t, f, id, doc.ID, "stk-acceptance-release", reviewProof(doc, "release", 1), 200)
	commission := controlProof()
	commission.PerformedOn = "2024-01-01"
	requireStatus(t, packHTTP(f, "POST", "devices/"+id+"/commission", "stk-acceptance-commission", commission), 200)
	rules := controlRules(1)
	requireStatus(t, packHTTP(f, "POST", "devices/"+id+"/rules", "stk-acceptance-rules", rules), 200)
	inspection := controlProof()
	inspection.ExpectedVersion = 2
	inspection.PerformedOn = time.Now().UTC().Format("2006-01-02")
	inspection.ControlType = "STK"
	inspection.Result = "passed"
	inspection.Examiner = "Synthetic STK examiner"
	inspection.Method = "Synthetic STK procedure"
	requireStatus(t, packHTTP(f, "POST", "devices/"+id+"/inspections", "stk-acceptance-inspection", inspection), 200)
	in := dutyProof(doc)
	in.ExpectedDeviceVersion = 3
	var rule *productDutyRule
	for i := range in.Rules {
		if in.Rules[i].Kind == "annex_1" {
			rule = &in.Rules[i]
		}
	}
	rule.Applicability = "required"
	rule.Evidence = "Synthetic STK obligation"
	dutyPost(t, f, id, "stk-acceptance-without-attestation", in, 200)
	if stkStatus(t, f, id) != "unknown" {
		t.Fatal("inspection alone accepted")
	}
	a := &dutySTKAcceptance{dutyFunctionAcceptance: dutyFunctionAcceptance{Reviewed: true, Decision: "accepted", PerformedOn: inspection.PerformedOn, Qualification: "Synthetic reviewer", Assignment: "Synthetic assignment", Evidence: "Synthetic STK acceptance", Note: "Synthetic qualified review"}, InspectionVersion: 3, DocumentID: doc.ID, ExpectedSnapshot: bookSnapshot(t, f, id), ScopeEvidence: "Synthetic STK scope", ExecutorEvidence: "Synthetic examiner competence", MarkingEvidence: "Synthetic marking and deadline"}
	rule.STKAcceptance = a
	in.ExpectedVersion = 1
	a.Reviewed = false
	dutyPost(t, f, id, "stk-acceptance-unconfirmed", in, 400)
	a.Reviewed = true
	a.InspectionVersion = 2
	dutyPost(t, f, id, "stk-acceptance-not-inspection", in, 400)
	a.InspectionVersion = 3
	a.MarkingEvidence = ""
	dutyPost(t, f, id, "stk-acceptance-missing-marking", in, 400)
	a.MarkingEvidence = "Synthetic marking and deadline"
	saved := dutyPost(t, f, id, "stk-acceptance-positive", in, 200)
	if saved.STKBasis == nil || stkStatus(t, f, id) != "accepted" || dutyRead(t, f, id).Overall != "not_assessed" {
		t.Fatal("missing qualified STK acceptance")
	}
	book, _ := bookArchive(t, f, id)
	found := false
	for _, c := range book.ProductDutyReview.Checks {
		if c.Kind == "annex_1_stk" && c.Status == "accepted" {
			found = true
		}
	}
	if !found {
		t.Fatal("missing archive STK status")
	}
	tx, _ := f.db.Begin()
	record, err := readDevice(context.Background(), tx, f.user, id, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	deadline := record.Controls["STK"]
	deadline.State = "OVERDUE"
	record.Controls["STK"] = deadline
	check, err := checkDutySTK(context.Background(), tx, record, *rule, doc.ID, saved.STKBasis)
	if err != nil || check.Status != "blocked" {
		t.Fatal("overdue STK accepted", check, err)
	}
	tx.Rollback()
	reviewPost(t, f, id, doc.ID, "stk-acceptance-quarantine", reviewProof(doc, "quarantine", 2), 200)
	reviewPost(t, f, id, doc.ID, "stk-acceptance-rescan", reviewProof(doc, "scan", 3), 200)
	reviewPost(t, f, id, doc.ID, "stk-acceptance-rerelease", reviewProof(doc, "release", 4), 200)
	if stkStatus(t, f, id) != "unknown" {
		t.Fatal("rerelease reactivated STK")
	}
	in.ExpectedVersion = 2
	dutyPost(t, f, id, "stk-acceptance-stale-snapshot", in, 409)
	a.ExpectedSnapshot = bookSnapshot(t, f, id)
	dutyPost(t, f, id, "stk-acceptance-renewed", in, 200)
	inspection.ExpectedVersion = 3
	inspection.Result = "failed"
	inspection.NextDueOn = ""
	requireStatus(t, packHTTP(f, "POST", "devices/"+id+"/inspections", "stk-acceptance-new-failure", inspection), 200)
	if stkStatus(t, f, id) != "blocked" {
		t.Fatal("new failed inspection block ignored")
	}
	in.ExpectedVersion = 3
	in.ExpectedDeviceVersion = 4
	a.ExpectedSnapshot = bookSnapshot(t, f, id)
	dutyPost(t, f, id, "stk-acceptance-blocked-positive", in, 409)
	a.Decision = "rejected"
	a.InspectionVersion = 4
	dutyPost(t, f, id, "stk-acceptance-rejected", in, 200)
	if stkStatus(t, f, id) != "blocked" {
		t.Fatal("explicit rejection ignored")
	}
	dutyPost(t, f, id, "stk-acceptance-positive", saved.Input, 200)
}
