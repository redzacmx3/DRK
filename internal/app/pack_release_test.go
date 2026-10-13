package app

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

func releaseRead(t *testing.T, f stockFixture, id, subject string) packReleaseAssessment {
	t.Helper()
	path := "packlists/" + id + "/release"
	if subject != "" {
		path += "?user_id=" + subject
	}
	w := packHTTP(f, "GET", path, "", nil)
	requireStatus(t, w, 200)
	var out struct {
		Assessment packReleaseAssessment `json:"assessment"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	return out.Assessment
}
func releasePost(t *testing.T, f stockFixture, id, key string, in packReleaseInput, status int) packReleaseEvent {
	t.Helper()
	w := packHTTP(f, "POST", "packlists/"+id+"/release", key, in)
	requireStatus(t, w, status)
	var out packReleaseEvent
	if status == 200 {
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
	}
	return out
}
func releaseProof(t *testing.T, f stockFixture, id, subject string) packReleaseInput {
	t.Helper()
	current := releaseRead(t, f, id, subject)
	yes := true
	return packReleaseInput{Action: "release", UserID: subject, ExpectedVersion: current.Version, ExpectedBasis: current.Basis, Reviewed: &yes, PerformedOn: time.Now().UTC().Format("2006-01-02"), ValidUntil: time.Now().Add(30 * time.Minute).UTC().Format(time.RFC3339), MissionReference: "Synthetic mission reference", OperatingScope: "Synthetic explicitly checked operational scope", ConditionsEvidence: "Synthetic current operational conditions", HandoverEvidence: "Synthetic qualified handover", Evidence: "Synthetic operational approval <img>", Qualification: "Synthetic release competence", Assignment: "Synthetic operator mandate", Note: "Synthetic scoped release decision"}
}
func releaseFixture(t *testing.T, other bool) (stockFixture, string, string, string) {
	t.Helper()
	f, device, doc := fulfilFixture(t)
	subject := f.user
	if _, err := f.db.Exec(`INSERT INTO role_assignments VALUES(?,?,'material_manager')`, f.user, demoSite); err != nil {
		t.Fatal(err)
	}
	if other {
		if err := CreateUser(f.db, "mission-user", "SyntheticPassword123", demoSite, "viewer"); err != nil {
			t.Fatal(err)
		}
		if err := f.db.QueryRow(`SELECT id FROM users WHERE username='mission-user'`).Scan(&subject); err != nil {
			t.Fatal(err)
		}
	}
	commission := controlProof()
	commission.PerformedOn = time.Now().UTC().Format("2006-01-02")
	deviceResponse(t, f, device, "commission", "release-device-commission", commission)
	rules := controlRules(1)
	for i := range rules.Rules {
		rules.Rules[i].Applicability = "exempt"
		rules.Rules[i].Mode = ""
		rules.Rules[i].IntervalMonths = 0
	}
	deviceResponse(t, f, device, "rules", "release-control-assess", rules)
	policy := usageProof("policy", 0)
	policy.ExpectedDeviceVersion = 2
	policy.InstructionMode = "exempt"
	policy.Codes = []string{"basic_competence"}
	usagePost(t, f, device, "release-usage-policy", policy)
	qual := usageProof("qualify", 1)
	qual.ExpectedDeviceVersion = 2
	qual.UserID = subject
	qual.Code = "basic_competence"
	qual.ValidFrom = time.Now().Add(-24 * time.Hour).UTC().Format("2006-01-02")
	qual.ValidUntil = time.Now().Add(48 * time.Hour).UTC().Format("2006-01-02")
	usagePost(t, f, device, "release-usage-qualify", qual)
	access := accessProof(doc, subject, 0)
	access.ExpectedDeviceVersion = 2
	accessPost(t, f, device, "release-manual-access", access, 200)
	w := packHTTP(f, "GET", "devices/"+device+"/end-user-acceptance?user_id="+subject, "", nil)
	requireStatus(t, w, 200)
	var current struct {
		Assessment endUserAssessment `json:"assessment"`
	}
	json.Unmarshal(w.Body.Bytes(), &current)
	end := endUserProof(t, f, device)
	end.UserID = subject
	end.ExpectedBasis = current.Assessment.Basis
	end.ExpectedVersion = current.Assessment.Version
	end.ExpectedDeviceVersion = 2
	endUserPost(t, f, device, "release-end-user", end, 200)
	source := dutyProof(doc)
	source.ExpectedDeviceVersion = 2
	dutyPost(t, f, device, "release-product-duty-source", source, 200)
	fulfilPost(t, f, device, "release-product-duty-accept", fulfilProof(t, f, device, doc.ID), 200)
	requireStatus(t, stockHTTP(f, "receipts", "release-material-receipt", stockRequest{BatchID: demoBatch, To: warehouse, Quantity: 5}), 201)
	input := packInput(warehouse)
	input.Devices = []packDevice{{DeviceID: device}}
	pack := published(t, f, input, "release-pack-publish")
	material := readiness(t, f, pack.ID)
	requireStatus(t, materialCheck(t, f, pack.ID, "release-material-physical", material.Basis, true), 201)
	currentPack := assessment(t, f, pack.ID)
	requireStatus(t, packHTTP(f, "POST", "packlists/"+pack.ID+"/device-checks", "release-device-physical", physicalInput(currentPack, device, true)), 201)
	return f, pack.ID, device, subject
}
func TestPackReleaseExplicitQualificationAtomicityAndNoAutomaticGrant(t *testing.T) {
	f, id, _, subject := releaseFixture(t, false)
	prepared := assessment(t, f, id)
	if prepared.Evidence.Status != "recorded_prerequisites_met" || prepared.Overall != "not_assessed" {
		t.Fatal("prerequisites inferred release or fixture incomplete", prepared.Evidence)
	}
	in := releaseProof(t, f, id, subject)
	releasePost(t, f, id, "release-material-physical", in, 409)
	for _, variant := range []string{"implicit", "empty-scope", "future", "backdate", "too-long", "foreign", "stale"} {
		bad := in
		status := 400
		switch variant {
		case "implicit":
			bad.Reviewed = nil
		case "empty-scope":
			bad.OperatingScope = ""
		case "future":
			bad.PerformedOn = time.Now().Add(48 * time.Hour).UTC().Format("2006-01-02")
		case "backdate":
			bad.PerformedOn = time.Now().Add(-48 * time.Hour).UTC().Format("2006-01-02")
		case "too-long":
			bad.ValidUntil = time.Now().Add(48 * time.Hour).UTC().Format(time.RFC3339)
		case "foreign":
			bad.UserID = "00000000-0000-4000-8000-000000000099"
			status = 404
		case "stale":
			bad.ExpectedBasis = "old-basis"
			status = 409
		}
		releasePost(t, f, id, "release-invalid-"+variant, bad, status)
	}
	if _, err := f.db.Exec(`CREATE TRIGGER release_fail BEFORE INSERT ON pack_release_requests BEGIN SELECT RAISE(ABORT,'synthetic rollback'); END`); err != nil {
		t.Fatal(err)
	}
	releasePost(t, f, id, "release-positive", in, 503)
	for _, table := range []string{"pack_release_events", "pack_release_outbox", "pack_release_requests"} {
		var count int
		if err := f.db.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&count); err != nil || count != 0 {
			t.Fatal("partial release", table, count, err)
		}
	}
	if _, err := f.db.Exec(`DROP TRIGGER release_fail`); err != nil {
		t.Fatal(err)
	}
	saved := releasePost(t, f, id, "release-positive", in, 200)
	if len(saved.Components) != 7 || releaseRead(t, f, id, subject).Status != "released" || assessment(t, f, id).Overall != "released_for_user" {
		t.Fatal("explicit qualified release missing")
	}
	if _, err := f.db.Exec(`UPDATE pack_release_events SET action='revoke'`); err == nil {
		t.Fatal("mutable event")
	}
	w := packHTTP(f, "GET", "packlists/"+id+"/release/export.json", "", nil)
	requireStatus(t, w, 200)
	var archive struct {
		Events []packReleaseEvent `json:"events"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &archive); err != nil || len(archive.Events) != 1 {
		t.Fatal("missing full export", err)
	}
	tx, err := f.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	p, err := loadPack(context.Background(), tx, f.user, id, "")
	if err != nil {
		t.Fatal(err)
	}
	base, err := assessPackFor(context.Background(), tx, f.user, subject, p, time.Now().Add(40*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	expired, err := assessPackReleaseWith(context.Background(), tx, subject, p, base, time.Now().Add(40*time.Minute))
	tx.Rollback()
	if err != nil || expired.Status == "released" || expired.Overall != "not_released" {
		t.Fatal("expiry ignored", expired, err)
	}
	revoke := packReleaseInput{ExpectedVersion: 1, UserID: subject, Action: "revoke", Evidence: in.Evidence, Qualification: in.Qualification, Assignment: in.Assignment, Note: in.Note}
	releasePost(t, f, id, "release-revoke", revoke, 200)
	releasePost(t, f, id, "release-positive", saved.Input, 200)
	if releaseRead(t, f, id, subject).Status != "blocked" || assessment(t, f, id).Overall != "not_released" {
		t.Fatal("historical response revived revoked release")
	}
}
func TestPackReleaseOtherPersonAuthorityAndRoleEpoch(t *testing.T) {
	f, id, _, subject := releaseFixture(t, true)
	in := releaseProof(t, f, id, subject)
	saved := releasePost(t, f, id, "release-other-positive", in, 200)
	if releaseRead(t, f, id, subject).Status != "released" || assessment(t, f, id).Overall != "not_assessed" {
		t.Fatal("grant applied to wrong person")
	}
	if _, err := f.db.Exec(`DELETE FROM role_assignments WHERE user_id=? AND role='device_manager'`, f.user); err != nil {
		t.Fatal(err)
	}
	releasePost(t, f, id, "release-other-positive", saved.Input, 404)
	requireStatus(t, packHTTP(f, "GET", "packlists/"+id+"/release?user_id="+subject, "", nil), 404)
	if _, err := f.db.Exec(`INSERT INTO role_assignments VALUES(?,?,'device_manager')`, f.user, demoSite); err != nil {
		t.Fatal(err)
	}
	if releaseRead(t, f, id, subject).Status == "released" {
		t.Fatal("restored reviewer roles revived release")
	}
	in = releaseProof(t, f, id, subject)
	releasePost(t, f, id, "release-other-renewed", in, 200)
	if _, err := f.db.Exec(`UPDATE users SET active=0 WHERE id=?`, subject); err != nil {
		t.Fatal(err)
	}
	if releaseRead(t, f, id, subject).Status != "blocked" {
		t.Fatal("inactive subject accepted")
	}
	if _, err := f.db.Exec(`UPDATE users SET active=1 WHERE id=?`, subject); err != nil {
		t.Fatal(err)
	}
	if releaseRead(t, f, id, subject).Status == "released" {
		t.Fatal("restored subject revived release")
	}
	in = releaseProof(t, f, id, subject)
	releasePost(t, f, id, "release-other-new-epoch", in, 200)
	if _, err := f.db.Exec(`DELETE FROM role_assignments WHERE user_id=? AND role='device_manager'`, f.user); err != nil {
		t.Fatal(err)
	}
	own := releaseRead(t, f, id, f.user)
	if own.Basis != "" {
		t.Fatal("read-only viewer exposed prep basis")
	}
	requireStatus(t, packHTTP(f, "GET", "packlists/"+id+"/release/export.json?user_id="+subject, "", nil), 404)
}
func TestPackReleaseChangedMaterialAndNewPackVersion(t *testing.T) {
	f, id, _, subject := releaseFixture(t, false)
	in := releaseProof(t, f, id, subject)
	releasePost(t, f, id, "release-stock-positive", in, 200)
	requireStatus(t, stockHTTP(f, "consumptions", "release-stock-consume", stockRequest{BatchID: demoBatch, From: warehouse, Quantity: 1, ExpectedVersion: 1}), 201)
	if releaseRead(t, f, id, subject).Status != "blocked" {
		t.Fatal("material shortage ignored")
	}
	in = releaseProof(t, f, id, subject)
	releasePost(t, f, id, "release-stock-blocked", in, 409)
	in.Action = "reject"
	releasePost(t, f, id, "release-stock-rejected", in, 200)
	input := packInput(warehouse)
	input.ExpectedVersion = 1
	published(t, f, input, "release-new-pack-version")
	if releaseRead(t, f, id, subject).Status == "released" || assessment(t, f, id).Overall == "released_for_user" {
		t.Fatal("obsolete version released")
	}
}

func TestPackReleaseNewPhysicalCheckAndDocumentWithdrawal(t *testing.T) {
	f, id, device, subject := releaseFixture(t, false)
	in := releaseProof(t, f, id, subject)
	saved := releasePost(t, f, id, "release-source-positive", in, 200)
	material := readiness(t, f, id)
	requireStatus(t, materialCheck(t, f, id, "release-new-material-proof", material.Basis, true), 201)
	result := releaseRead(t, f, id, subject)
	if result.Status == "released" || result.Prerequisites.Status != "recorded_prerequisites_met" {
		t.Fatal("fresh independent check revived approval or incorrectly broke prerequisites", result)
	}
	in = releaseProof(t, f, id, subject)
	releasePost(t, f, id, "release-source-new-proof", in, 200)
	var doc documentRecord
	if err := f.db.QueryRow(`SELECT id,sha256 FROM effective_documents WHERE device_id=?`, device).Scan(&doc.ID, &doc.SHA256); err != nil {
		t.Fatal(err)
	}
	reviewPost(t, f, device, doc.ID, "release-source-quarantine", reviewProof(doc, "quarantine", 2), 200)
	reviewPost(t, f, device, doc.ID, "release-source-rescan", reviewProof(doc, "scan", 3), 200)
	reviewPost(t, f, device, doc.ID, "release-source-rerelease", reviewProof(doc, "release", 4), 200)
	if releaseRead(t, f, id, subject).Status == "released" {
		t.Fatal("document rerelease revived operating release")
	}
	releasePost(t, f, id, "release-source-positive", saved.Input, 200)
	if releaseRead(t, f, id, subject).Status == "released" {
		t.Fatal("exact reply reactivated changed sources")
	}
}
