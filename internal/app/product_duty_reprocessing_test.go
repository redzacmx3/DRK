package app

import (
	"testing"
	"time"
)

func reprocessingDutyStatus(t *testing.T, f stockFixture, id string) string {
	t.Helper()
	for _, c := range dutyRead(t, f, id).Checks {
		if c.Kind == "reprocessing_process" {
			return c.Status
		}
	}
	t.Fatal("missing group check")
	return ""
}
func TestProductDutyReprocessingAcceptanceSnapshotAndRevocation(t *testing.T) {
	t.Setenv("DRK_CLAMD_ADDRESS", fakeClamd(t, time.Now(), "stream: OK"))
	f, id, doc := documentFixture(t)
	reviewPost(t, f, id, doc.ID, "reprocessing-acceptance-scan", reviewProof(doc, "scan", 0), 200)
	reviewPost(t, f, id, doc.ID, "reprocessing-acceptance-release", reviewProof(doc, "release", 1), 200)
	in := dutyProof(doc)
	var rule *productDutyRule
	for i := range in.Rules {
		if in.Rules[i].Kind == "reprocessing" {
			rule = &in.Rules[i]
		}
	}
	rule.Applicability = "required"
	rule.Evidence = "Synthetic book obligation"
	dutyPost(t, f, id, "reprocessing-acceptance-no-attestation", in, 200)
	if reprocessingDutyStatus(t, f, id) != "unknown" {
		t.Fatal("archive presence inferred completeness")
	}
	yes, no := true, false
	a := &dutyReprocessingAcceptance{dutyFunctionAcceptance: dutyFunctionAcceptance{Reviewed: true, Decision: "accepted", PerformedOn: time.Now().UTC().Format("2006-01-02"), Qualification: "Synthetic reviewer", Assignment: "Synthetic assignment", Evidence: "Synthetic group proof", Note: "Synthetic scope decision"}, DocumentID: doc.ID, ProcessDescription: "Synthetic designated group", ProcessVersion: "Synthetic restricted use", ManufacturerEvidence: "Synthetic met product conditions", ClassificationEvidence: "Synthetic explicit no special exception", ValidationEvidence: "Synthetic validated process", ExecutorEvidence: "Synthetic competence", ReleaseProcedureEvidence: "Synthetic controlled result release", StorageEvidence: "Synthetic storage rules", RequirementsMet: &yes, ProcessValidated: &yes}
	a.ExpectedSnapshot = bookSnapshot(t, f, id)
	rule.ReprocessingAcceptance = a
	in.ExpectedVersion = 1
	a.Reviewed = false
	dutyPost(t, f, id, "reprocessing-acceptance-unconfirmed", in, 400)
	a.Reviewed = true
	a.DocumentID = "00000000-0000-4000-8000-000000000099"
	dutyPost(t, f, id, "reprocessing-acceptance-foreign-document", in, 404)
	a.DocumentID = doc.ID
	a.ClassificationEvidence = ""
	dutyPost(t, f, id, "reprocessing-acceptance-missing-retention", in, 400)
	a.ClassificationEvidence = "Synthetic retention responsibility"
	a.RequirementsMet = nil
	dutyPost(t, f, id, "reprocessing-acceptance-implicit-conditions", in, 400)
	a.RequirementsMet = &no
	dutyPost(t, f, id, "reprocessing-acceptance-unmet-conditions", in, 400)
	a.RequirementsMet = &yes
	a.ProcessValidated = &no
	dutyPost(t, f, id, "reprocessing-acceptance-unvalidated", in, 400)
	a.ProcessValidated = nil
	dutyPost(t, f, id, "reprocessing-acceptance-implicit-exception", in, 400)
	a.ProcessValidated = &yes
	saved := dutyPost(t, f, id, "reprocessing-acceptance-positive", in, 200)
	if saved.ReprocessingBasis == nil || saved.ReprocessingBasis.Snapshot == "" || reprocessingDutyStatus(t, f, id) != "accepted" || dutyRead(t, f, id).Overall != "not_assessed" {
		t.Fatal("missing scoped attestation")
	}
	for _, c := range dutyRead(t, f, id).Checks {
		if c.Kind == "reprocessing" && c.Status != "unknown" {
			t.Fatal("process review granted cycle release")
		}
	}
	if usageRead(t, f, id).Status != "unknown" {
		t.Fatal("group review granted individual eligibility")
	}
	book, _ := bookArchive(t, f, id)
	found := false
	for _, c := range book.ProductDutyReview.Checks {
		if c.Kind == "reprocessing_process" && c.Status == "accepted" {
			found = true
		}
	}
	if !found {
		t.Fatal("book archive missing acceptance")
	}
	reviewPost(t, f, id, doc.ID, "reprocessing-acceptance-quarantine", reviewProof(doc, "quarantine", 2), 200)
	reviewPost(t, f, id, doc.ID, "reprocessing-acceptance-rescan", reviewProof(doc, "scan", 3), 200)
	reviewPost(t, f, id, doc.ID, "reprocessing-acceptance-rerelease", reviewProof(doc, "release", 4), 200)
	if reprocessingDutyStatus(t, f, id) != "unknown" {
		t.Fatal("document rerelease revived stale book attestation")
	}
	in.ExpectedVersion = 2
	dutyPost(t, f, id, "reprocessing-acceptance-stale-snapshot", in, 409)
	a.ExpectedSnapshot = bookSnapshot(t, f, id)
	dutyPost(t, f, id, "reprocessing-acceptance-renewed", in, 200)
	requireStatus(t, packHTTP(f, "POST", "devices/"+id+"/maintenance", "reprocessing-acceptance-new-control", maintenanceInput(0, "completed")), 200)
	if reprocessingDutyStatus(t, f, id) != "unknown" {
		t.Fatal("changed device book not invalidated")
	}
	a.ExpectedSnapshot = bookSnapshot(t, f, id)
	in.ExpectedDeviceVersion = 1
	in.ExpectedVersion = 3
	dutyPost(t, f, id, "reprocessing-acceptance-updated-book", in, 200)
	if reprocessingDutyStatus(t, f, id) != "accepted" {
		t.Fatal("fresh book review missing")
	}
	policy := usageProof("policy", 0)
	policy.InstructionMode = "exempt"
	policy.Codes = []string{"basic_competence"}
	policy.ExpectedDeviceVersion = 1
	usagePost(t, f, id, "reprocessing-acceptance-new-person-policy", policy)
	if reprocessingDutyStatus(t, f, id) != "unknown" {
		t.Fatal("person proof change not invalidated")
	}
	a.ExpectedSnapshot = bookSnapshot(t, f, id)
	in.ExpectedVersion = 4
	a.Decision = "rejected"
	a.RequirementsMet = &no
	dutyPost(t, f, id, "reprocessing-acceptance-rejected", in, 200)
	if reprocessingDutyStatus(t, f, id) != "blocked" {
		t.Fatal("rejected book not blocked")
	}
	dutyPost(t, f, id, "reprocessing-acceptance-positive", saved.Input, 200)
}
