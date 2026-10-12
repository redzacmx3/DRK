package app

import (
	"context"
	"testing"
	"time"
)

func mtkStatus(t *testing.T, f stockFixture, id string) string {
	t.Helper()
	for _, c := range dutyRead(t, f, id).Checks {
		if c.Kind == "annex_2_mtk" {
			return c.Status
		}
	}
	t.Fatal("missing MTK check")
	return ""
}
func TestProductDutyMTKQualifiedAcceptanceAndInvalidation(t *testing.T) {
	t.Setenv("DRK_CLAMD_ADDRESS", fakeClamd(t, time.Now(), "stream: OK"))
	f, id, doc := documentFixture(t)
	reviewPost(t, f, id, doc.ID, "mtk-acceptance-scan", reviewProof(doc, "scan", 0), 200)
	reviewPost(t, f, id, doc.ID, "mtk-acceptance-release", reviewProof(doc, "release", 1), 200)
	commission := controlProof()
	commission.PerformedOn = "2024-01-01"
	requireStatus(t, packHTTP(f, "POST", "devices/"+id+"/commission", "mtk-acceptance-commission", commission), 200)
	rules := controlRules(1)
	requireStatus(t, packHTTP(f, "POST", "devices/"+id+"/rules", "mtk-acceptance-rules", rules), 200)
	inspection := controlProof()
	inspection.ExpectedVersion = 2
	inspection.PerformedOn = time.Now().UTC().Format("2006-01-02")
	inspection.ControlType = "MTK"
	inspection.Result = "passed"
	inspection.Examiner = "Synthetic MTK examiner"
	inspection.Method = "Synthetic MTK procedure"
	requireStatus(t, packHTTP(f, "POST", "devices/"+id+"/inspections", "mtk-acceptance-inspection", inspection), 200)
	in := dutyProof(doc)
	in.ExpectedDeviceVersion = 3
	var rule *productDutyRule
	for i := range in.Rules {
		if in.Rules[i].Kind == "annex_2" {
			rule = &in.Rules[i]
		}
	}
	rule.Applicability = "required"
	rule.Evidence = "Synthetic MTK obligation"
	dutyPost(t, f, id, "mtk-acceptance-without-attestation", in, 200)
	if mtkStatus(t, f, id) != "unknown" {
		t.Fatal("inspection alone accepted")
	}
	a := &dutyMTKAcceptance{dutyFunctionAcceptance: dutyFunctionAcceptance{Reviewed: true, Decision: "accepted", PerformedOn: inspection.PerformedOn, Qualification: "Synthetic reviewer", Assignment: "Synthetic assignment", Evidence: "Synthetic MTK acceptance", Note: "Synthetic qualified review"}, InspectionVersion: 3, DocumentID: doc.ID, ExpectedSnapshot: bookSnapshot(t, f, id), ScopeEvidence: "Synthetic MTK scope", ExecutorEvidence: "Synthetic examiner competence", MarkingEvidence: "Synthetic marking and deadline"}
	rule.MTKAcceptance = a
	in.ExpectedVersion = 1
	a.Reviewed = false
	dutyPost(t, f, id, "mtk-acceptance-unconfirmed", in, 400)
	a.Reviewed = true
	a.InspectionVersion = 2
	dutyPost(t, f, id, "mtk-acceptance-not-inspection", in, 400)
	a.InspectionVersion = 3
	a.MarkingEvidence = ""
	dutyPost(t, f, id, "mtk-acceptance-missing-marking", in, 400)
	a.MarkingEvidence = "Synthetic marking and deadline"
	saved := dutyPost(t, f, id, "mtk-acceptance-positive", in, 200)
	if saved.MTKBasis == nil || mtkStatus(t, f, id) != "accepted" || dutyRead(t, f, id).Overall != "not_assessed" {
		t.Fatal("missing qualified MTK acceptance")
	}
	book, _ := bookArchive(t, f, id)
	found := false
	for _, c := range book.ProductDutyReview.Checks {
		if c.Kind == "annex_2_mtk" && c.Status == "accepted" {
			found = true
		}
	}
	if !found {
		t.Fatal("missing archive MTK status")
	}
	tx, _ := f.db.Begin()
	record, err := readDevice(context.Background(), tx, f.user, id, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	deadline := record.Controls["MTK"]
	deadline.State = "OVERDUE"
	record.Controls["MTK"] = deadline
	check, err := checkDutyMTK(context.Background(), tx, record, *rule, doc.ID, saved.MTKBasis)
	if err != nil || check.Status != "blocked" {
		t.Fatal("overdue MTK accepted", check, err)
	}
	tx.Rollback()
	reviewPost(t, f, id, doc.ID, "mtk-acceptance-quarantine", reviewProof(doc, "quarantine", 2), 200)
	reviewPost(t, f, id, doc.ID, "mtk-acceptance-rescan", reviewProof(doc, "scan", 3), 200)
	reviewPost(t, f, id, doc.ID, "mtk-acceptance-rerelease", reviewProof(doc, "release", 4), 200)
	if mtkStatus(t, f, id) != "unknown" {
		t.Fatal("rerelease reactivated MTK")
	}
	in.ExpectedVersion = 2
	dutyPost(t, f, id, "mtk-acceptance-stale-snapshot", in, 409)
	a.ExpectedSnapshot = bookSnapshot(t, f, id)
	dutyPost(t, f, id, "mtk-acceptance-renewed", in, 200)
	inspection.ExpectedVersion = 3
	inspection.Result = "failed"
	inspection.NextDueOn = ""
	requireStatus(t, packHTTP(f, "POST", "devices/"+id+"/inspections", "mtk-acceptance-new-failure", inspection), 200)
	if mtkStatus(t, f, id) != "blocked" {
		t.Fatal("new failed inspection block ignored")
	}
	in.ExpectedVersion = 3
	in.ExpectedDeviceVersion = 4
	a.ExpectedSnapshot = bookSnapshot(t, f, id)
	dutyPost(t, f, id, "mtk-acceptance-blocked-positive", in, 409)
	a.Decision = "rejected"
	a.InspectionVersion = 4
	dutyPost(t, f, id, "mtk-acceptance-rejected", in, 200)
	if mtkStatus(t, f, id) != "blocked" {
		t.Fatal("explicit rejection ignored")
	}
	dutyPost(t, f, id, "mtk-acceptance-positive", saved.Input, 200)
}
