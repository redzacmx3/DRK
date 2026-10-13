package app

import (
	"encoding/json"
	"testing"
	"time"
)

func otherDutyStatus(t *testing.T, f stockFixture, id string) string {
	t.Helper()
	for _, c := range dutyRead(t, f, id).Checks {
		if c.Kind == "other_requirements" {
			return c.Status
		}
	}
	t.Fatal("missing other requirements check")
	return ""
}
func otherDutyFixture(t *testing.T) (stockFixture, string, documentRecord, productDutyInput) {
	t.Helper()
	t.Setenv("DRK_CLAMD_ADDRESS", fakeClamd(t, time.Now(), "stream: OK"))
	f, id, doc := documentFixture(t)
	reviewPost(t, f, id, doc.ID, "other-proof-scan", reviewProof(doc, "scan", 0), 200)
	reviewPost(t, f, id, doc.ID, "other-proof-release", reviewProof(doc, "release", 1), 200)
	in := dutyProof(doc)
	yes := true
	for i := range in.Rules {
		if in.Rules[i].Kind == "other" {
			in.Rules[i].Applicability = "required"
			in.Rules[i].Evidence = "Synthetic product obligation"
			in.Rules[i].OtherAcceptance = &dutyOtherAcceptance{
				dutyFunctionAcceptance: dutyFunctionAcceptance{Reviewed: true, Decision: "accepted", PerformedOn: time.Now().UTC().Format("2006-01-02"), Qualification: "Synthetic authorized reviewer", Assignment: "Synthetic assignment", Evidence: "Synthetic acceptance", Note: "Synthetic complete scope decision"},
				ExpectedSnapshot:       bookSnapshot(t, f, id), DocumentID: doc.ID, Scope: "Synthetic additional product obligations", CompletenessEvidence: "Synthetic manufacturer scope assessment", ScopeComplete: &yes,
				Requirements: []dutyOtherRequirement{{Code: "storage_limits", Description: "Synthetic storage conditions", Status: "met", Evidence: "Synthetic current storage proof", Reason: "Synthetic compliant conditions"}, {Code: "transport_limits", Description: "Synthetic transport conditions", Status: "not_applicable", Evidence: "Synthetic documented non-transport use", Reason: "Synthetic externally assessed exception"}},
			}
		}
	}
	return f, id, doc, in
}
func otherInput(in *productDutyInput) *dutyOtherAcceptance {
	for i := range in.Rules {
		if in.Rules[i].Kind == "other" {
			return in.Rules[i].OtherAcceptance
		}
	}
	panic("missing other")
}
func TestProductDutyOtherValidationAndScopedAcceptance(t *testing.T) {
	f, id, _, in := otherDutyFixture(t)
	variants := []string{"unconfirmed", "implicit-scope", "incomplete", "empty-scope", "empty-completeness", "missing-requirements", "too-many", "duplicate", "bad-code", "empty-description", "empty-evidence", "empty-reason", "implicit-status", "unknown", "not-met", "foreign-document", "wrong-kind", "exempt", "future", "stale"}
	for _, variant := range variants {
		raw, _ := json.Marshal(in)
		var bad productDutyInput
		json.Unmarshal(raw, &bad)
		a := otherInput(&bad)
		no := false
		switch variant {
		case "unconfirmed":
			a.Reviewed = false
		case "implicit-scope":
			a.ScopeComplete = nil
		case "incomplete":
			a.ScopeComplete = &no
		case "empty-scope":
			a.Scope = ""
		case "empty-completeness":
			a.CompletenessEvidence = ""
		case "missing-requirements":
			a.Requirements = nil
		case "too-many":
			for len(a.Requirements) < 13 {
				a.Requirements = append(a.Requirements, a.Requirements[0])
			}
		case "duplicate":
			a.Requirements[1].Code = a.Requirements[0].Code
		case "bad-code":
			a.Requirements[0].Code = "Invalid Code"
		case "empty-description":
			a.Requirements[0].Description = ""
		case "empty-evidence":
			a.Requirements[1].Evidence = ""
		case "empty-reason":
			a.Requirements[1].Reason = ""
		case "implicit-status":
			a.Requirements[0].Status = ""
		case "unknown":
			a.Requirements[0].Status = "unknown"
		case "not-met":
			a.Requirements[0].Status = "not_met"
		case "foreign-document":
			a.DocumentID = "00000000-0000-4000-8000-000000000099"
		case "wrong-kind":
			for i := range bad.Rules {
				if bad.Rules[i].Kind == "other" {
					bad.Rules[i].OtherAcceptance = nil
				}
				if bad.Rules[i].Kind == "it_security" {
					bad.Rules[i].Applicability = "required"
					bad.Rules[i].Evidence = "Synthetic required duty"
					bad.Rules[i].OtherAcceptance = a
				}
			}
		case "exempt":
			for i := range bad.Rules {
				if bad.Rules[i].Kind == "other" {
					bad.Rules[i].Applicability = "exempt"
				}
			}
		case "future":
			a.PerformedOn = time.Now().Add(48 * time.Hour).UTC().Format("2006-01-02")
		case "stale":
			a.ExpectedSnapshot = "synthetic-stale"
		}
		status := 400
		if variant == "foreign-document" {
			status = 404
		}
		if variant == "stale" {
			status = 409
		}
		dutyPost(t, f, id, "other-invalid-"+variant, bad, status)
	}
	if dutyRead(t, f, id).Version != 0 {
		t.Fatal("invalid acceptance persisted")
	}
	if _, err := f.db.Exec(`CREATE TRIGGER other_fail BEFORE INSERT ON product_duty_requests BEGIN SELECT RAISE(ABORT,'synthetic rollback'); END`); err != nil {
		t.Fatal(err)
	}
	dutyPost(t, f, id, "other-positive", in, 503)
	if dutyRead(t, f, id).Version != 0 {
		t.Fatal("partial acceptance after failed request persistence")
	}
	if _, err := f.db.Exec(`DROP TRIGGER other_fail`); err != nil {
		t.Fatal(err)
	}
	saved := dutyPost(t, f, id, "other-positive", in, 200)
	if saved.OtherBasis == nil || saved.OtherBasis.Snapshot == "" || otherDutyStatus(t, f, id) != "accepted" {
		t.Fatal("missing scoped acceptance")
	}
	assessment := dutyRead(t, f, id)
	if assessment.Overall != "not_assessed" {
		t.Fatal("granted overall permission")
	}
	for _, c := range assessment.Checks {
		if c.Kind == "other" && c.Status == "accepted" {
			t.Fatal("individual requirements bypassed electrical/remaining total duties")
		}
	}
	book, _ := bookArchive(t, f, id)
	found := false
	for _, c := range book.ProductDutyReview.Checks {
		if c.Kind == "other_requirements" && c.Status == "accepted" {
			found = true
		}
	}
	if !found {
		t.Fatal("archive omitted acceptance")
	}
	a := otherInput(&in)
	in.ExpectedVersion = 1
	a.Decision = "rejected"
	a.ScopeComplete = new(bool)
	a.Requirements[0].Status = "unknown"
	rejected := dutyPost(t, f, id, "other-rejected", in, 200)
	if otherDutyStatus(t, f, id) != "blocked" {
		t.Fatal("rejection not blocked")
	}
	dutyPost(t, f, id, "other-positive", saved.Input, 200)
	if dutyRead(t, f, id).Version != rejected.Version || otherDutyStatus(t, f, id) != "blocked" {
		t.Fatal("historical reply reactivated acceptance")
	}
}
func TestProductDutyOtherDocumentEpochAndIndependentBlock(t *testing.T) {
	f, id, doc, in := otherDutyFixture(t)
	saved := dutyPost(t, f, id, "other-binding-positive", in, 200)
	reviewPost(t, f, id, doc.ID, "other-binding-quarantine", reviewProof(doc, "quarantine", 2), 200)
	reviewPost(t, f, id, doc.ID, "other-binding-rescan", reviewProof(doc, "scan", 3), 200)
	reviewPost(t, f, id, doc.ID, "other-binding-rerelease", reviewProof(doc, "release", 4), 200)
	if otherDutyStatus(t, f, id) != "unknown" {
		t.Fatal("rerelease revived stale acceptance")
	}
	in.ExpectedVersion = 1
	dutyPost(t, f, id, "other-binding-stale", in, 409)
	otherInput(&in).ExpectedSnapshot = bookSnapshot(t, f, id)
	dutyPost(t, f, id, "other-binding-fresh", in, 200)
	requireStatus(t, packHTTP(f, "POST", "devices/"+id+"/maintenance", "other-binding-independent-defect", maintenanceInput(0, "failed")), 200)
	if otherDutyStatus(t, f, id) != "blocked" {
		t.Fatal("independent defect ignored")
	}
	in.ExpectedVersion = 2
	in.ExpectedDeviceVersion = 1
	otherInput(&in).ExpectedSnapshot = bookSnapshot(t, f, id)
	dutyPost(t, f, id, "other-binding-blocked-positive", in, 409)
	dutyPost(t, f, id, "other-binding-positive", saved.Input, 200)
	if otherDutyStatus(t, f, id) != "blocked" {
		t.Fatal("historical success lifted independent block")
	}
	if _, err := f.db.Exec(`DELETE FROM role_assignments WHERE user_id=?`, f.user); err != nil {
		t.Fatal(err)
	}
	dutyPost(t, f, id, "other-binding-positive", saved.Input, 404)

}
