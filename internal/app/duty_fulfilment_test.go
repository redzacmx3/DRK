package app

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

func fulfilRead(t *testing.T, f stockFixture, id string) dutyFulfilmentAssessment {
	t.Helper()
	w := packHTTP(f, "GET", "devices/"+id+"/duty-fulfilment", "", nil)
	requireStatus(t, w, 200)
	var out struct {
		Assessment dutyFulfilmentAssessment `json:"assessment"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	return out.Assessment
}
func fulfilPost(t *testing.T, f stockFixture, id, key string, in dutyFulfilmentInput, status int) dutyFulfilmentEvent {
	t.Helper()
	w := packHTTP(f, "POST", "devices/"+id+"/duty-fulfilment", key, in)
	requireStatus(t, w, status)
	var event dutyFulfilmentEvent
	if status == 200 {
		if err := json.Unmarshal(w.Body.Bytes(), &event); err != nil {
			t.Fatal(err)
		}
	}
	return event
}
func fulfilProof(t *testing.T, f stockFixture, id, document string) dutyFulfilmentInput {
	t.Helper()
	current := fulfilRead(t, f, id)
	yes := true
	in := dutyFulfilmentInput{ExpectedVersion: current.Version, ExpectedDeviceVersion: current.DeviceVersion, ExpectedBasis: current.Basis, Action: "accept", Reviewed: &yes, PerformedOn: time.Now().UTC().Format("2006-01-02"), ValidUntil: time.Now().Add(time.Hour).UTC().Format(time.RFC3339), DocumentID: document, Evidence: "Synthetic overall duties proof <img>", Qualification: "Synthetic authorized reviewer", Assignment: "Synthetic operator appointment", Note: "Synthetic complete product scope"}
	for _, kind := range productDutyKinds {
		in.Areas = append(in.Areas, dutyFulfilmentArea{Kind: kind, ScopeComplete: &yes, Evidence: "Synthetic assessed full scope " + kind, Reason: "Synthetic qualified fulfilment / exception review"})
	}
	return in
}
func fulfilFixture(t *testing.T) (stockFixture, string, documentRecord) {
	t.Helper()
	t.Setenv("DRK_CLAMD_ADDRESS", fakeClamd(t, time.Now(), "stream: OK"))
	f, id, doc := documentFixture(t)
	reviewPost(t, f, id, doc.ID, "fulfil-proof-scan", reviewProof(doc, "scan", 0), 200)
	reviewPost(t, f, id, doc.ID, "fulfil-proof-release", reviewProof(doc, "release", 1), 200)
	return f, id, doc
}
func TestDutyFulfilmentExplicitValidationAtomicityAndRevocation(t *testing.T) {
	f, id, doc := fulfilFixture(t)
	in := fulfilProof(t, f, id, doc.ID)
	fulfilPost(t, f, id, "fulfil-no-applicability", in, 409)
	dutyPost(t, f, id, "fulfil-source-review", dutyProof(doc), 200)
	if fulfilRead(t, f, id).Status != "unknown" {
		t.Fatal("eight exceptions inferred acceptance")
	}
	in = fulfilProof(t, f, id, doc.ID)
	for _, variant := range []string{"implicit", "missing", "duplicate", "incomplete", "missing-ref", "future", "backdate", "expired", "foreign", "stale"} {
		raw, _ := json.Marshal(in)
		var bad dutyFulfilmentInput
		json.Unmarshal(raw, &bad)
		no := false
		status := 400
		switch variant {
		case "implicit":
			bad.Reviewed = nil
		case "missing":
			bad.Areas = bad.Areas[:7]
		case "duplicate":
			bad.Areas[1].Kind = bad.Areas[0].Kind
		case "incomplete":
			bad.Areas[0].ScopeComplete = &no
		case "missing-ref":
			bad.Areas[0].Evidence = ""
		case "future":
			bad.PerformedOn = time.Now().Add(48 * time.Hour).UTC().Format("2006-01-02")
		case "backdate":
			bad.PerformedOn = time.Now().Add(-48 * time.Hour).UTC().Format("2006-01-02")
		case "expired":
			bad.ValidUntil = time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)
		case "foreign":
			bad.DocumentID = "00000000-0000-4000-8000-000000000099"
			status = 404
		case "stale":
			bad.ExpectedBasis = "synthetic-stale"
			status = 409
		}
		fulfilPost(t, f, id, "fulfil-invalid-"+variant, bad, status)
	}
	fulfilPost(t, f, id, "fulfil-source-review", in, 409)
	if _, err := f.db.Exec(`CREATE TRIGGER fulfil_fail BEFORE INSERT ON duty_fulfilment_requests BEGIN SELECT RAISE(ABORT,'synthetic rollback'); END`); err != nil {
		t.Fatal(err)
	}
	fulfilPost(t, f, id, "fulfil-positive", in, 503)
	for _, table := range []string{"duty_fulfilment_events", "duty_fulfilment_outbox", "duty_fulfilment_requests"} {
		var count int
		if err := f.db.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&count); err != nil || count != 0 {
			t.Fatal("partial acceptance", table, count, err)
		}
	}
	if _, err := f.db.Exec(`DROP TRIGGER fulfil_fail`); err != nil {
		t.Fatal(err)
	}
	saved := fulfilPost(t, f, id, "fulfil-positive", in, 200)
	if saved.ProductDutyVersion != 1 || len(saved.Sources) != 8 || saved.DocumentReviewVersion != 2 || fulfilRead(t, f, id).Status != "accepted" || fulfilRead(t, f, id).Overall != "not_assessed" {
		t.Fatal("missing professional product acceptance or false overall release")
	}
	if usageRead(t, f, id).Status != "unknown" {
		t.Fatal("product acceptance granted individual eligibility")
	}
	book, _ := bookArchive(t, f, id)
	if book.DutyFulfilment == nil || book.DutyFulfilment.Status != "accepted" || len(book.DutyFulfilmentEvents) != 1 {
		t.Fatal("archive missing acceptance")
	}
	if _, err := f.db.Exec(`UPDATE duty_fulfilment_events SET action='revoke'`); err == nil {
		t.Fatal("mutable acceptance")
	}
	if _, err := f.db.Exec(`DELETE FROM duty_fulfilment_requests`); err == nil {
		t.Fatal("mutable response")
	}
	tx, err := f.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	record, err := readDevice(context.Background(), tx, f.user, id, time.Now().Add(2*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	expired, err := assessDutyFulfilment(context.Background(), tx, f.user, record, time.Now().Add(2*time.Hour))
	tx.Rollback()
	if err != nil || expired.Status != "unknown" {
		t.Fatal("expired acceptance remained current", expired, err)
	}
	revoke := dutyFulfilmentInput{ExpectedVersion: 1, ExpectedDeviceVersion: 0, Action: "revoke", Evidence: in.Evidence, Qualification: in.Qualification, Assignment: in.Assignment, Note: in.Note}
	fulfilPost(t, f, id, "fulfil-revoke", revoke, 200)
	fulfilPost(t, f, id, "fulfil-positive", saved.Input, 200)
	if fulfilRead(t, f, id).Status != "blocked" || fulfilRead(t, f, id).Version != 2 {
		t.Fatal("historical reply revived revoked acceptance")
	}
	if _, err := f.db.Exec(`DELETE FROM role_assignments WHERE user_id=?`, f.user); err != nil {
		t.Fatal(err)
	}
	fulfilPost(t, f, id, "fulfil-positive", saved.Input, 404)
}
func TestDutyFulfilmentDocumentEpochChangesAndIndependentDefect(t *testing.T) {
	f, id, doc := fulfilFixture(t)
	source := dutyProof(doc)
	dutyPost(t, f, id, "fulfil-binding-source", source, 200)
	in := fulfilProof(t, f, id, doc.ID)
	fulfilPost(t, f, id, "fulfil-binding-positive", in, 200)
	reviewPost(t, f, id, doc.ID, "fulfil-binding-quarantine", reviewProof(doc, "quarantine", 2), 200)
	reviewPost(t, f, id, doc.ID, "fulfil-binding-rescan", reviewProof(doc, "scan", 3), 200)
	reviewPost(t, f, id, doc.ID, "fulfil-binding-rerelease", reviewProof(doc, "release", 4), 200)
	if fulfilRead(t, f, id).Status != "unknown" {
		t.Fatal("rerelease revived old product acceptance")
	}
	in.ExpectedVersion = 1
	fulfilPost(t, f, id, "fulfil-binding-stale", in, 409)
	in = fulfilProof(t, f, id, doc.ID)
	fulfilPost(t, f, id, "fulfil-binding-renewed", in, 200)
	source.ExpectedVersion = 1
	dutyPost(t, f, id, "fulfil-binding-new-source", source, 200)
	if fulfilRead(t, f, id).Status != "unknown" {
		t.Fatal("new applicability version did not invalidate fulfilment")
	}
	in = fulfilProof(t, f, id, doc.ID)
	fulfilPost(t, f, id, "fulfil-binding-new-review", in, 200)
	proof := controlProof()
	proof.BlockReason = "defect"
	deviceResponse(t, f, id, "defects", "fulfil-binding-defect", proof)
	if fulfilRead(t, f, id).Status != "blocked" {
		t.Fatal("defect ignored")
	}
	in = fulfilProof(t, f, id, doc.ID)
	fulfilPost(t, f, id, "fulfil-binding-defect-positive", in, 409)
	in.Action = "reject"
	no := false
	in.Areas[0].ScopeComplete = &no
	fulfilPost(t, f, id, "fulfil-binding-rejected", in, 200)
	if fulfilRead(t, f, id).Status != "blocked" {
		t.Fatal("rejection ignored")
	}
}
func TestDutyFulfilmentAggregationRequiresQualifiedSubproofs(t *testing.T) {
	record := deviceRecord{Controls: map[string]Deadline{"STK": {State: "CURRENT"}, "MTK": {State: "CURRENT"}, "electrical": {State: "CURRENT"}}}
	base := productDutyAssessment{Status: "reviewed"}
	for _, kind := range productDutyKinds {
		base.Checks = append(base.Checks, productDutyCheck{Kind: kind, Status: "not_applicable"})
	}
	for _, parent := range []string{"annex_1", "annex_2", "other"} {
		for i := range base.Checks {
			if base.Checks[i].Kind == parent {
				base.Checks[i].Status = "unknown"
			}
		}
	}
	for _, kind := range []string{"annex_1_function", "annex_1_instruction", "annex_1_stk", "annex_2_mtk", "other_requirements", "electrical_safety"} {
		base.Checks = append(base.Checks, productDutyCheck{Kind: kind, Status: "accepted"})
	}
	base.Checks = append(base.Checks, productDutyCheck{Kind: "annex_1_commission", Status: "record_found"})
	state, _ := dutyFulfilmentPrerequisites(record, base)
	if state != "met" {
		t.Fatal("current professional subproofs not aggregated", state)
	}
	for _, kind := range []string{"annex_1_function", "annex_1_instruction", "annex_1_stk", "annex_2_mtk", "other_requirements", "electrical_safety"} {
		bad := base
		bad.Checks = append([]productDutyCheck{}, base.Checks...)
		for i := range bad.Checks {
			if bad.Checks[i].Kind == kind {
				bad.Checks[i].Status = "record_found"
			}
		}
		state, _ = dutyFulfilmentPrerequisites(record, bad)
		if state != "unknown" {
			t.Fatal("unqualified record inferred acceptance", kind, state)
		}
	}
	record.Controls["MTK"] = Deadline{State: "OVERDUE"}
	state, _ = dutyFulfilmentPrerequisites(record, base)
	if state != "blocked" {
		t.Fatal("overdue MTK ignored")
	}
}

func TestDutyFulfilmentRequiredOtherAndPackComponentRemainSeparate(t *testing.T) {
	f, id, doc, source := otherDutyFixture(t)
	dutyPost(t, f, id, "fulfil-required-other-source", source, 200)
	in := fulfilProof(t, f, id, doc.ID)
	fulfilPost(t, f, id, "fulfil-required-unknown-electrical", in, 409)
	commission := controlProof()
	commission.PerformedOn = time.Now().UTC().Format("2006-01-02")
	deviceResponse(t, f, id, "commission", "fulfil-required-commission", commission)
	rules := controlRules(1)
	deviceResponse(t, f, id, "rules", "fulfil-required-control-assessment", rules)
	source.ExpectedVersion = 1
	source.ExpectedDeviceVersion = 2
	otherInput(&source).ExpectedSnapshot = bookSnapshot(t, f, id)
	dutyPost(t, f, id, "fulfil-required-current-other", source, 200)
	in = fulfilProof(t, f, id, doc.ID)
	fulfilPost(t, f, id, "fulfil-required-positive", in, 200)
	if fulfilRead(t, f, id).Status != "accepted" {
		t.Fatal("explicit required obligations and electrical exception not accepted")
	}
	if _, err := f.db.Exec(`INSERT INTO role_assignments VALUES(?,?,'material_manager')`, f.user, demoSite); err != nil {
		t.Fatal(err)
	}
	input := packInput(warehouse)
	input.Devices = []packDevice{{DeviceID: id}}
	pack := published(t, f, input, "fulfil-pack-create")
	assessed := assessment(t, f, pack.ID)
	met := false
	for _, c := range assessed.Evidence.Components {
		if c.Kind == "product_duties" && c.Status == "met" {
			met = true
		}
	}
	if !met || assessed.Overall != "not_assessed" || assessed.Evidence.Status != "blocked" {
		t.Fatal("product duties not aggregated or material/user gap overridden", assessed.Evidence)
	}
	revoke := dutyFulfilmentInput{ExpectedVersion: 1, ExpectedDeviceVersion: 2, Action: "revoke", Evidence: in.Evidence, Qualification: in.Qualification, Assignment: in.Assignment, Note: in.Note}
	fulfilPost(t, f, id, "fulfil-required-revoke", revoke, 200)
	for _, c := range assessment(t, f, pack.ID).Evidence.Components {
		if c.Kind == "product_duties" && c.Status != "blocked" {
			t.Fatal("pack ignored revoked fulfilment")
		}
	}
	if err := SetSiteRole(f.db, "viewer", []string{demoSite}, "viewer"); err != nil {
		t.Fatal(err)
	}
	f.cookie, f.csrf = loginTest(t, f.handler, nil)
	if fulfilRead(t, f, id).Basis != "" {
		t.Fatal("read-only view exposed preparation basis")
	}
	fulfilPost(t, f, id, "fulfil-required-positive", in, 404)
}
