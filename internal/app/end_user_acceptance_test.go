package app

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func endUserRead(t *testing.T, f stockFixture, id string) endUserAssessment {
	t.Helper()
	w := packHTTP(f, "GET", "devices/"+id+"/end-user-acceptance", "", nil)
	requireStatus(t, w, 200)
	var out struct {
		Assessment endUserAssessment `json:"assessment"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	return out.Assessment
}
func endUserPost(t *testing.T, f stockFixture, id, key string, in endUserInput, status int) endUserEvent {
	t.Helper()
	w := packHTTP(f, "POST", "devices/"+id+"/end-user-acceptance", "synthetic-"+key, in)
	requireStatus(t, w, status)
	var event endUserEvent
	if status == 200 {
		if err := json.Unmarshal(w.Body.Bytes(), &event); err != nil {
			t.Fatal(err)
		}
	}
	return event
}
func endUserProof(t *testing.T, f stockFixture, id string) endUserInput {
	t.Helper()
	current := endUserRead(t, f, id)
	yes := true
	return endUserInput{ExpectedVersion: current.Version, ExpectedDeviceVersion: current.DeviceVersion, ExpectedBasis: current.Basis, UserID: f.user, Action: "accept", Reviewed: &yes, PerformedOn: time.Now().UTC().Format("2006-01-02"), ValidUntil: time.Now().Add(48 * time.Hour).UTC().Format(time.RFC3339), CompetenceEvidence: "Synthetic user competence", InstructionEvidence: "Synthetic suitable instruction", ManualEvidence: "Synthetic comprehensible manual", Evidence: "Synthetic end user acceptance <img>", Qualification: "Synthetic reviewer competence", Assignment: "Synthetic operator assignment", Note: "Synthetic suitability decision"}
}
func endUserFixture(t *testing.T) (stockFixture, string, documentRecord) {
	t.Helper()
	f, id, doc := annex1Fixture(t)
	policy := usageProof("policy", 0)
	policy.ExpectedDeviceVersion = 2
	policy.InstructionMode = "required"
	policy.Codes = []string{"basic_competence"}
	usagePost(t, f, id, "synthetic-end-policy", policy)
	qual := usageProof("qualify", 1)
	qual.ExpectedDeviceVersion = 2
	qual.UserID = f.user
	qual.Code = "basic_competence"
	qual.ValidFrom = time.Now().Add(-24 * time.Hour).UTC().Format("2006-01-02")
	qual.ValidUntil = time.Now().Add(48 * time.Hour).UTC().Format("2006-01-02")
	usagePost(t, f, id, "synthetic-end-qual", qual)
	annex1Bind(t, f, id, 2, 2, 2, f.user)
	access := accessProof(doc, f.user, 0)
	access.ExpectedDeviceVersion = 2
	accessPost(t, f, id, "synthetic-end-access", access, 200)
	return f, id, doc
}
func TestEndUserAcceptanceRenewalRevocationAndEvidenceBinding(t *testing.T) {
	f, id, doc := endUserFixture(t)
	in := endUserProof(t, f, id)
	first := endUserPost(t, f, id, "end-accept", in, 200)
	if len(first.Sources) < 4 || first.Basis != in.ExpectedBasis || endUserRead(t, f, id).Status != "accepted" || endUserRead(t, f, id).Overall != "not_assessed" {
		t.Fatal("missing partial acceptance or false release")
	}
	// Unchanged access renewal and another person's records must not invalidate this approval.
	access := accessProof(doc, f.user, 1)
	access.ExpectedDeviceVersion = 2
	accessPost(t, f, id, "synthetic-end-access-renew", access, 200)
	if endUserRead(t, f, id).Status != "accepted" {
		t.Fatal("unchanged daily access renewal invalidated competence approval")
	}
	if err := CreateUser(f.db, "end-other", "SyntheticPassword123", demoSite, "viewer"); err != nil {
		t.Fatal(err)
	}
	var other string
	if err := f.db.QueryRow(`SELECT id FROM users WHERE username='end-other'`).Scan(&other); err != nil {
		t.Fatal(err)
	}
	qual := usageProof("qualify", 3)
	qual.ExpectedDeviceVersion = 2
	qual.UserID = other
	qual.Code = "basic_competence"
	qual.ValidFrom = time.Now().Add(-24 * time.Hour).UTC().Format("2006-01-02")
	qual.ValidUntil = time.Now().Add(48 * time.Hour).UTC().Format("2006-01-02")
	usagePost(t, f, id, "synthetic-end-other-qualification", qual)
	if endUserRead(t, f, id).Status != "accepted" {
		t.Fatal("another person's private evidence affected approval")
	}
	access.Action = "revoke"
	access.ExpectedVersion = 2
	access.DocumentID = ""
	access.ManualVersion = ""
	access.Language = ""
	access.Accessible = nil
	access.ValidUntil = ""
	accessPost(t, f, id, "synthetic-end-access-revoke", access, 200)
	if endUserRead(t, f, id).Status != "blocked" {
		t.Fatal("access revocation ignored")
	}
	access = accessProof(doc, f.user, 3)
	access.ExpectedDeviceVersion = 2
	accessPost(t, f, id, "synthetic-end-access-after-revoke", access, 200)
	if endUserRead(t, f, id).Status != "unknown" {
		t.Fatal("renewal after revoke revived old approval")
	}
	in = endUserProof(t, f, id)
	endUserPost(t, f, id, "end-reaccept-access", in, 200)
	reviewPost(t, f, id, doc.ID, "end-document-withdraw", reviewProof(doc, "quarantine", 2), 200)
	if endUserRead(t, f, id).Status == "accepted" {
		t.Fatal("document withdrawal ignored")
	}
	reviewPost(t, f, id, doc.ID, "end-document-rescan", reviewProof(doc, "scan", 3), 200)
	reviewPost(t, f, id, doc.ID, "end-document-rerelease", reviewProof(doc, "release", 4), 200)
	if endUserRead(t, f, id).Status != "unknown" {
		t.Fatal("rerelease revived old acceptance")
	}
	in = endUserProof(t, f, id)
	endUserPost(t, f, id, "end-reaccept-document", in, 200)
	unbind := usageProof("unbind", 4)
	unbind.ExpectedDeviceVersion = 2
	unbind.UserID = f.user
	usagePost(t, f, id, "synthetic-end-unbind", unbind)
	if endUserRead(t, f, id).Status != "blocked" {
		t.Fatal("instruction revocation ignored")
	}
	annex1Bind(t, f, id, 5, 2, 2, f.user)
	if endUserRead(t, f, id).Status != "unknown" {
		t.Fatal("rebind revived old acceptance")
	}
	in = endUserProof(t, f, id)
	endUserPost(t, f, id, "end-reaccept-binding", in, 200)
	book, files := bookArchive(t, f, id)
	if len(book.EndUser) != 4 || book.EndUserReview.Status != "accepted" || !strings.Contains(string(files["geraeteakte.html"]), "Endanwenderabnahmen") || strings.Contains(string(files["geraeteakte.html"]), "Synthetic end user acceptance <img>") {
		t.Fatal("missing or unsafe scoped archive")
	}
	revoke := endUserProof(t, f, id)
	revoke.Action = "revoke"
	revoke.ExpectedBasis = ""
	revoke.Reviewed = nil
	revoke.PerformedOn = ""
	revoke.ValidUntil = ""
	revoke.CompetenceEvidence = ""
	revoke.InstructionEvidence = ""
	revoke.ManualEvidence = ""
	endUserPost(t, f, id, "end-revoke", revoke, 200)
	if endUserRead(t, f, id).Status != "blocked" {
		t.Fatal("explicit revocation ignored")
	}
	if replay := endUserPost(t, f, id, "end-accept", first.Input, 200); replay.ID != first.ID {
		t.Fatal("historical retry changed")
	}
	if _, err := f.db.Exec(`UPDATE end_user_acceptance_events SET action='accept'`); err == nil {
		t.Fatal("mutable approval history")
	}
	if err := SetSiteRole(f.db, "viewer", []string{demoSite}, "viewer"); err != nil {
		t.Fatal(err)
	}
	f.cookie, f.csrf = loginTest(t, f.handler, nil)
	endUserPost(t, f, id, "end-accept", first.Input, 404)
	requireStatus(t, packHTTP(f, "GET", "devices/"+id+"/end-user-acceptance?user_id="+other, "", nil), 404)
}

func TestEndUserAcceptanceValidationAtomicityExpiryAndBlocking(t *testing.T) {
	f, id, _ := endUserFixture(t)
	in := endUserProof(t, f, id)
	for _, variant := range []string{"unchecked", "missing-ref", "future", "early", "long", "stale", "foreign-person"} {
		bad := in
		status := 400
		switch variant {
		case "unchecked":
			bad.Reviewed = nil
		case "missing-ref":
			bad.CompetenceEvidence = ""
		case "future":
			bad.PerformedOn = "2099-01-01"
		case "early":
			bad.PerformedOn = "2024-01-01"
		case "long":
			bad.ValidUntil = time.Now().Add(367 * 24 * time.Hour).Format(time.RFC3339)
		case "stale":
			bad.ExpectedBasis = "wrong"
			status = 409
		case "foreign-person":
			bad.UserID = newID()
			status = 404
		}
		endUserPost(t, f, id, "end-invalid-"+variant, bad, status)
	}
	if _, err := f.db.Exec(`CREATE TRIGGER end_fail BEFORE INSERT ON end_user_acceptance_requests BEGIN SELECT RAISE(ABORT,'synthetic rollback'); END`); err != nil {
		t.Fatal(err)
	}
	endUserPost(t, f, id, "end-atomic", in, 503)
	if endUserRead(t, f, id).Version != 0 {
		t.Fatal("partial approval transaction")
	}
	f.db.Exec(`DROP TRIGGER end_fail`)
	endUserPost(t, f, id, "end-good", in, 200)
	tx, err := f.db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	record, err := readDevice(context.Background(), tx, f.user, id, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	expired, err := assessEndUser(context.Background(), tx, f.user, f.user, record, time.Now().Add(3*time.Hour))
	tx.Rollback()
	if err != nil || expired.Status == "accepted" {
		t.Fatal("expired access accepted", expired, err)
	}
	in = endUserProof(t, f, id)
	in.Action = "reject"
	endUserPost(t, f, id, "end-reject", in, 200)
	if endUserRead(t, f, id).Status != "blocked" {
		t.Fatal("rejection ignored")
	}
	in = endUserProof(t, f, id)
	in.Action = "accept"
	endUserPost(t, f, id, "end-good-again", in, 200)
	defect := controlProof()
	defect.ExpectedVersion = 2
	defect.BlockReason = "defect"
	requireStatus(t, packHTTP(f, "POST", "devices/"+id+"/defects", "end-device-defect", defect), 200)
	if endUserRead(t, f, id).Status != "blocked" {
		t.Fatal("approval released device block")
	}
	in = endUserProof(t, f, id)
	endUserPost(t, f, id, "end-blocked-accept", in, 409)
	prepared := packAssessment{Material: materialReadiness{CurrentVersion: true}, Devices: []deviceAssessment{{EndUser: &endUserAssessment{Status: "blocked"}}}}
	found := false
	for _, c := range consolidateEvidence(prepared).Components {
		if c.Kind == "end_user_acceptance" && c.Status == "blocked" {
			found = true
		}
	}
	if !found {
		t.Fatal("missing combined block")
	}
}

func TestEndUserPrivateArchiveAndFormerSubjectScope(t *testing.T) {
	f, id, doc := endUserFixture(t)
	endUserPost(t, f, id, "own-end-acceptance", endUserProof(t, f, id), 200)
	if err := CreateUser(f.db, "private-end-user", "SyntheticPassword123", demoSite, "viewer"); err != nil {
		t.Fatal(err)
	}
	var person string
	if err := f.db.QueryRow(`SELECT id FROM users WHERE username='private-end-user'`).Scan(&person); err != nil {
		t.Fatal(err)
	}
	qual := usageProof("qualify", 3)
	qual.ExpectedDeviceVersion = 2
	qual.UserID = person
	qual.Code = "basic_competence"
	qual.ValidFrom = time.Now().Add(-24 * time.Hour).UTC().Format("2006-01-02")
	qual.ValidUntil = time.Now().Add(48 * time.Hour).UTC().Format("2006-01-02")
	usagePost(t, f, id, "private-user-qualification", qual)
	annex1Bind(t, f, id, 4, 2, 2, person)
	access := accessProof(doc, person, 1)
	access.ExpectedDeviceVersion = 2
	accessPost(t, f, id, "private-user-access", access, 200)
	w := packHTTP(f, "GET", "devices/"+id+"/end-user-acceptance?user_id="+person, "", nil)
	requireStatus(t, w, 200)
	var other struct {
		Assessment endUserAssessment `json:"assessment"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &other); err != nil {
		t.Fatal(err)
	}
	in := endUserProof(t, f, id)
	in.UserID = person
	in.ExpectedBasis = other.Assessment.Basis
	in.Note = "PRIVATE_END_USER_SENTINEL"
	endUserPost(t, f, id, "private-user-approval", in, 200)
	book, _ := bookArchive(t, f, id)
	if len(book.EndUser) != 2 {
		t.Fatal("current manager archive incomplete")
	}
	if err := SetSiteRole(f.db, "viewer", []string{demoSite}, "viewer"); err != nil {
		t.Fatal(err)
	}
	f.cookie, f.csrf = loginTest(t, f.handler, nil)
	book, files := bookArchive(t, f, id)
	if len(book.EndUser) != 1 {
		t.Fatal("foreign end user evidence leaked")
	}
	for _, data := range files {
		if strings.Contains(string(data), "PRIVATE_END_USER_SENTINEL") {
			t.Fatal("private end user archive leaked")
		}
	}
	requireStatus(t, packHTTP(f, "GET", "devices/"+id+"/end-user-acceptance?user_id="+person, "", nil), 404)
}
