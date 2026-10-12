package app

import (
	"encoding/json"
	"testing"
	"time"
)

func bookDutyStatus(t *testing.T, f stockFixture, id string) string {
	t.Helper()
	for _, c := range dutyRead(t, f, id).Checks {
		if c.Kind == "device_book" {
			return c.Status
		}
	}
	t.Fatal("missing book check")
	return ""
}
func bookSnapshot(t *testing.T, f stockFixture, id string) string {
	t.Helper()
	w := packHTTP(f, "GET", "devices/"+id+"/product-duties", "", nil)
	requireStatus(t, w, 200)
	var r struct {
		Snapshot string `json:"book_snapshot"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &r); err != nil {
		t.Fatal(err)
	}
	return r.Snapshot
}
func TestProductDutyBookAcceptanceSnapshotAndRevocation(t *testing.T) {
	t.Setenv("DRK_CLAMD_ADDRESS", fakeClamd(t, time.Now(), "stream: OK"))
	f, id, doc := documentFixture(t)
	reviewPost(t, f, id, doc.ID, "book-acceptance-scan", reviewProof(doc, "scan", 0), 200)
	reviewPost(t, f, id, doc.ID, "book-acceptance-release", reviewProof(doc, "release", 1), 200)
	in := dutyProof(doc)
	var rule *productDutyRule
	for i := range in.Rules {
		if in.Rules[i].Kind == "device_book" {
			rule = &in.Rules[i]
		}
	}
	rule.Applicability = "required"
	rule.Evidence = "Synthetic book obligation"
	dutyPost(t, f, id, "book-acceptance-no-attestation", in, 200)
	if bookDutyStatus(t, f, id) != "unknown" {
		t.Fatal("archive presence inferred completeness")
	}
	a := &dutyBookAcceptance{Reviewed: true, Decision: "accepted", PerformedOn: time.Now().UTC().Format("2006-01-02"), DocumentID: doc.ID, Qualification: "Synthetic reviewer", Assignment: "Synthetic assignment", Evidence: "Synthetic book proof", Note: "Synthetic completeness decision", CompletenessEvidence: "Synthetic contents review", AccessEvidence: "Synthetic access arrangements", RetentionEvidence: "Synthetic retention responsibility"}
	a.ExpectedSnapshot = bookSnapshot(t, f, id)
	rule.BookAcceptance = a
	in.ExpectedVersion = 1
	a.Reviewed = false
	dutyPost(t, f, id, "book-acceptance-unconfirmed", in, 400)
	a.Reviewed = true
	a.DocumentID = "00000000-0000-4000-8000-000000000099"
	dutyPost(t, f, id, "book-acceptance-foreign-document", in, 404)
	a.DocumentID = doc.ID
	a.RetentionEvidence = ""
	dutyPost(t, f, id, "book-acceptance-missing-retention", in, 400)
	a.RetentionEvidence = "Synthetic retention responsibility"
	saved := dutyPost(t, f, id, "book-acceptance-positive", in, 200)
	if saved.BookBasis == nil || saved.BookBasis.Snapshot == "" || bookDutyStatus(t, f, id) != "accepted" || dutyRead(t, f, id).Overall != "not_assessed" {
		t.Fatal("missing scoped attestation")
	}
	book, _ := bookArchive(t, f, id)
	found := false
	for _, c := range book.ProductDutyReview.Checks {
		if c.Kind == "device_book" && c.Status == "accepted" {
			found = true
		}
	}
	if !found {
		t.Fatal("book archive missing acceptance")
	}
	reviewPost(t, f, id, doc.ID, "book-acceptance-quarantine", reviewProof(doc, "quarantine", 2), 200)
	reviewPost(t, f, id, doc.ID, "book-acceptance-rescan", reviewProof(doc, "scan", 3), 200)
	reviewPost(t, f, id, doc.ID, "book-acceptance-rerelease", reviewProof(doc, "release", 4), 200)
	if bookDutyStatus(t, f, id) != "unknown" {
		t.Fatal("document rerelease revived stale book attestation")
	}
	in.ExpectedVersion = 2
	dutyPost(t, f, id, "book-acceptance-stale-snapshot", in, 409)
	a.ExpectedSnapshot = bookSnapshot(t, f, id)
	dutyPost(t, f, id, "book-acceptance-renewed", in, 200)
	requireStatus(t, packHTTP(f, "POST", "devices/"+id+"/maintenance", "book-acceptance-new-control", maintenanceInput(0, "completed")), 200)
	if bookDutyStatus(t, f, id) != "unknown" {
		t.Fatal("changed device book not invalidated")
	}
	a.ExpectedSnapshot = bookSnapshot(t, f, id)
	in.ExpectedDeviceVersion = 1
	in.ExpectedVersion = 3
	dutyPost(t, f, id, "book-acceptance-updated-book", in, 200)
	if bookDutyStatus(t, f, id) != "accepted" {
		t.Fatal("fresh book review missing")
	}
	policy := usageProof("policy", 0)
	policy.InstructionMode = "exempt"
	policy.Codes = []string{"basic_competence"}
	policy.ExpectedDeviceVersion = 1
	usagePost(t, f, id, "book-acceptance-new-person-policy", policy)
	if bookDutyStatus(t, f, id) != "unknown" {
		t.Fatal("person proof change not invalidated")
	}
	a.ExpectedSnapshot = bookSnapshot(t, f, id)
	in.ExpectedVersion = 4
	a.Decision = "rejected"
	dutyPost(t, f, id, "book-acceptance-rejected", in, 200)
	if bookDutyStatus(t, f, id) != "blocked" {
		t.Fatal("rejected book not blocked")
	}
	dutyPost(t, f, id, "book-acceptance-positive", saved.Input, 200)
}
