package app

import (
	"testing"
	"time"
)

func groupDutyStatus(t *testing.T, f stockFixture, id string) string {
	t.Helper()
	for _, c := range dutyRead(t, f, id).Checks {
		if c.Kind == "special_user_group" {
			return c.Status
		}
	}
	t.Fatal("missing group check")
	return ""
}
func TestProductDutyUserGroupAcceptanceSnapshotAndRevocation(t *testing.T) {
	t.Setenv("DRK_CLAMD_ADDRESS", fakeClamd(t, time.Now(), "stream: OK"))
	f, id, doc := documentFixture(t)
	reviewPost(t, f, id, doc.ID, "group-acceptance-scan", reviewProof(doc, "scan", 0), 200)
	reviewPost(t, f, id, doc.ID, "group-acceptance-release", reviewProof(doc, "release", 1), 200)
	in := dutyProof(doc)
	var rule *productDutyRule
	for i := range in.Rules {
		if in.Rules[i].Kind == "special_user_group" {
			rule = &in.Rules[i]
		}
	}
	rule.Applicability = "required"
	rule.Evidence = "Synthetic book obligation"
	dutyPost(t, f, id, "group-acceptance-no-attestation", in, 200)
	if groupDutyStatus(t, f, id) != "unknown" {
		t.Fatal("archive presence inferred completeness")
	}
	yes, no := true, false
	a := &dutyUserGroupAcceptance{dutyFunctionAcceptance: dutyFunctionAcceptance{Reviewed: true, Decision: "accepted", PerformedOn: time.Now().UTC().Format("2006-01-02"), Qualification: "Synthetic reviewer", Assignment: "Synthetic assignment", Evidence: "Synthetic group proof", Note: "Synthetic scope decision"}, DocumentID: doc.ID, GroupDescription: "Synthetic designated group", UseScope: "Synthetic restricted use", ConditionsEvidence: "Synthetic met product conditions", ExceptionEvidence: "Synthetic explicit no special exception", RequirementsMet: &yes, ExceptionUsed: &no}
	a.ExpectedSnapshot = bookSnapshot(t, f, id)
	rule.UserGroupAcceptance = a
	in.ExpectedVersion = 1
	a.Reviewed = false
	dutyPost(t, f, id, "group-acceptance-unconfirmed", in, 400)
	a.Reviewed = true
	a.DocumentID = "00000000-0000-4000-8000-000000000099"
	dutyPost(t, f, id, "group-acceptance-foreign-document", in, 404)
	a.DocumentID = doc.ID
	a.ExceptionEvidence = ""
	dutyPost(t, f, id, "group-acceptance-missing-retention", in, 400)
	a.ExceptionEvidence = "Synthetic retention responsibility"
	a.RequirementsMet = nil
	dutyPost(t, f, id, "group-acceptance-implicit-conditions", in, 400)
	a.RequirementsMet = &no
	dutyPost(t, f, id, "group-acceptance-unmet-conditions", in, 400)
	a.RequirementsMet = &yes
	a.ExceptionUsed = nil
	dutyPost(t, f, id, "group-acceptance-implicit-exception", in, 400)
	a.ExceptionUsed = &no
	saved := dutyPost(t, f, id, "group-acceptance-positive", in, 200)
	if saved.UserGroupBasis == nil || saved.UserGroupBasis.Snapshot == "" || groupDutyStatus(t, f, id) != "accepted" || dutyRead(t, f, id).Overall != "not_assessed" {
		t.Fatal("missing scoped attestation")
	}
	if usageRead(t, f, id).Status != "unknown" {
		t.Fatal("group review granted individual eligibility")
	}
	book, _ := bookArchive(t, f, id)
	found := false
	for _, c := range book.ProductDutyReview.Checks {
		if c.Kind == "special_user_group" && c.Status == "accepted" {
			found = true
		}
	}
	if !found {
		t.Fatal("book archive missing acceptance")
	}
	reviewPost(t, f, id, doc.ID, "group-acceptance-quarantine", reviewProof(doc, "quarantine", 2), 200)
	reviewPost(t, f, id, doc.ID, "group-acceptance-rescan", reviewProof(doc, "scan", 3), 200)
	reviewPost(t, f, id, doc.ID, "group-acceptance-rerelease", reviewProof(doc, "release", 4), 200)
	if groupDutyStatus(t, f, id) != "unknown" {
		t.Fatal("document rerelease revived stale book attestation")
	}
	in.ExpectedVersion = 2
	dutyPost(t, f, id, "group-acceptance-stale-snapshot", in, 409)
	a.ExpectedSnapshot = bookSnapshot(t, f, id)
	dutyPost(t, f, id, "group-acceptance-renewed", in, 200)
	requireStatus(t, packHTTP(f, "POST", "devices/"+id+"/maintenance", "group-acceptance-new-control", maintenanceInput(0, "completed")), 200)
	if groupDutyStatus(t, f, id) != "unknown" {
		t.Fatal("changed device book not invalidated")
	}
	a.ExpectedSnapshot = bookSnapshot(t, f, id)
	in.ExpectedDeviceVersion = 1
	in.ExpectedVersion = 3
	dutyPost(t, f, id, "group-acceptance-updated-book", in, 200)
	if groupDutyStatus(t, f, id) != "accepted" {
		t.Fatal("fresh book review missing")
	}
	policy := usageProof("policy", 0)
	policy.InstructionMode = "exempt"
	policy.Codes = []string{"basic_competence"}
	policy.ExpectedDeviceVersion = 1
	usagePost(t, f, id, "group-acceptance-new-person-policy", policy)
	if groupDutyStatus(t, f, id) != "unknown" {
		t.Fatal("person proof change not invalidated")
	}
	a.ExpectedSnapshot = bookSnapshot(t, f, id)
	in.ExpectedVersion = 4
	a.Decision = "rejected"
	a.RequirementsMet = &no
	dutyPost(t, f, id, "group-acceptance-rejected", in, 200)
	if groupDutyStatus(t, f, id) != "blocked" {
		t.Fatal("rejected book not blocked")
	}
	dutyPost(t, f, id, "group-acceptance-positive", saved.Input, 200)
}
