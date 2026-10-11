package app

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func usagePost(t *testing.T, f stockFixture, id, key string, in usageInput) int {
	t.Helper()
	w := packHTTP(f, "POST", "devices/"+id+"/usage", key, in)
	requireStatus(t, w, 200)
	var result struct {
		Version int `json:"version"`
	}
	json.Unmarshal(w.Body.Bytes(), &result)
	return result.Version
}
func usageRead(t *testing.T, f stockFixture, id string) usageAssessment {
	t.Helper()
	w := packHTTP(f, "GET", "devices/"+id+"/usage", "", nil)
	requireStatus(t, w, 200)
	var result struct {
		Assessment usageAssessment `json:"assessment"`
	}
	json.Unmarshal(w.Body.Bytes(), &result)
	return result.Assessment
}
func usageProof(action string, version int) usageInput {
	return usageInput{Action: action, ExpectedVersion: version, Evidence: "Reviewed evidence reference", Qualification: "Reviewer qualification reference", Assignment: "Operator assignment reference", Note: "Synthetic documented review"}
}
func TestUsagePolicyQualificationRevocationAndAtomicity(t *testing.T) {
	f, id := controlFixture(t)
	initial := usageRead(t, f, id)
	if initial.Status != "unknown" || initial.Overall != "not_assessed" {
		t.Fatal(initial)
	}
	policy := usageProof("policy", 0)
	policy.InstructionMode = "exempt"
	policy.Codes = []string{"basic_competence"}
	version := usagePost(t, f, id, "usage-policy-first", policy)
	qualify := usageProof("qualify", version)
	qualify.UserID = f.user
	qualify.Code = "basic_competence"
	qualify.ValidFrom = localDay(time.Now())
	qualify.ValidUntil = "2099-12-31"
	if _, err := f.db.Exec(`CREATE TRIGGER fail_usage BEFORE INSERT ON usage_requests BEGIN SELECT RAISE(ABORT,'synthetic failure'); END`); err != nil {
		t.Fatal(err)
	}
	requireStatus(t, packHTTP(f, "POST", "devices/"+id+"/usage", "usage-atomic-proof", qualify), 503)
	if got := usageRead(t, f, id); got.Version != 1 || got.Status != "unknown" {
		t.Fatal("partial commit", got)
	}
	f.db.Exec(`DROP TRIGGER fail_usage`)
	version = usagePost(t, f, id, "usage-atomic-proof", qualify)
	if got := usageRead(t, f, id); got.Status != "recorded_requirements_met" || got.Overall != "not_assessed" {
		t.Fatal(got)
	}
	revoke := usageProof("revoke", version)
	revoke.UserID = f.user
	revoke.Code = "basic_competence"
	usagePost(t, f, id, "usage-revoke-proof", revoke)
	if usageRead(t, f, id).Status != "blocked" {
		t.Fatal("revocation ignored")
	}
	if replay := usagePost(t, f, id, "usage-atomic-proof", qualify); replay != 2 {
		t.Fatal("replay changed")
	}
	requireStatus(t, packHTTP(f, "POST", "devices/"+id+"/usage", "usage-stale-proof", qualify), 409)
	if _, err := f.db.Exec(`UPDATE usage_events SET action='qualify'`); err == nil {
		t.Fatal("mutable evidence")
	}
	if err := SetSiteRole(f.db, "viewer", []string{demoSite}, "viewer"); err != nil {
		t.Fatal(err)
	}
	f.cookie, f.csrf = loginTest(t, f.handler, nil)
	requireStatus(t, packHTTP(f, "POST", "devices/"+id+"/usage", "usage-atomic-proof", qualify), 404)
}
func TestUsageConcreteInstructionMappingQuarantineAndExpiry(t *testing.T) {
	f, id, document := documentFixture(t)
	instruction := controlProof()
	instruction.Instructor = "Synthetic trainer"
	instruction.Trainee = "Free text person"
	instruction.PerformedOn = "2024-01-01"
	instruction.DocumentVersion = "Manual 1"
	instruction.DocumentID = document.ID
	instruction.InstructionScope = "device"
	requireStatus(t, packHTTP(f, "POST", "devices/"+id+"/instructions", "usage-instruction-proof", instruction), 200)
	policy := usageProof("policy", 0)
	policy.ExpectedDeviceVersion = 1
	policy.InstructionMode = "required"
	policy.Codes = []string{"basic_competence"}
	version := usagePost(t, f, id, "usage-required-policy", policy)
	qualify := usageProof("qualify", version)
	qualify.ExpectedDeviceVersion = 1
	qualify.UserID = f.user
	qualify.Code = "basic_competence"
	qualify.ValidFrom = "2020-01-01"
	qualify.ValidUntil = "2099-12-31"
	version = usagePost(t, f, id, "usage-required-qualify", qualify)
	if got := usageRead(t, f, id); !strings.Contains(strings.Join(got.Reasons, " "), "Benutzerkonto") {
		t.Fatal("free text inferred as account", got)
	}
	bind := usageProof("bind", version)
	bind.ExpectedDeviceVersion = 1
	bind.UserID = f.user
	bind.InstructionVersion = 1
	version = usagePost(t, f, id, "usage-bind-instruction", bind)
	got := usageRead(t, f, id)
	if got.Status != "unknown" || !strings.Contains(strings.Join(got.Reasons, " "), "Quarantäne") {
		t.Fatal("quarantined proof approved", got)
	}
	unbind := usageProof("unbind", version)
	unbind.ExpectedDeviceVersion = 1
	unbind.UserID = f.user
	usagePost(t, f, id, "usage-unbind-instruction", unbind)
	// Civil-day expiry is inclusive in Berlin and recomputed without a worker.
	qualify.ExpectedVersion = version + 1
	qualify.ValidUntil = localDay(time.Now().AddDate(0, 0, -1))
	usagePost(t, f, id, "usage-expired-qualify", qualify)
	if usageRead(t, f, id).Status != "blocked" {
		t.Fatal("expired proof accepted")
	}
}

func TestUsageScopeIdentityAndDisabledAccountRevocation(t *testing.T) {
	f, id := controlFixture(t)
	policy := usageProof("policy", 0)
	policy.InstructionMode = "exempt"
	policy.Codes = []string{"basic_competence"}
	usagePost(t, f, id, "usage-identity-policy", policy)
	qualify := usageProof("qualify", 1)
	qualify.UserID = f.user
	qualify.Code = "basic_competence"
	qualify.ValidFrom = "2020-01-01"
	qualify.ValidUntil = "2099-12-31"
	usagePost(t, f, id, "usage-identity-proof", qualify)
	details := detailsInput(0)
	requireStatus(t, packHTTP(f, "POST", "devices/"+id+"/details", "usage-identity-change", details), 200)
	if usageRead(t, f, id).Status == "recorded_requirements_met" {
		t.Fatal("identity change retained personnel qualification")
	}
	qualify.ExpectedVersion = 2
	requireStatus(t, packHTTP(f, "POST", "devices/"+id+"/usage", "usage-stale-device", qualify), 409)
	// A different ordinary reader cannot obtain this account's qualification references.
	if err := CreateUser(f.db, "other-person", testPassword, demoSite, "viewer"); err != nil {
		t.Fatal(err)
	}
	var other string
	f.db.QueryRow(`SELECT id FROM users WHERE username='other-person'`).Scan(&other)
	if _, err := f.db.Exec(`UPDATE sessions SET user_id=? WHERE token_hash=?`, other, digest(f.cookie.Value)); err != nil {
		t.Fatal(err)
	}
	requireStatus(t, packHTTP(f, "GET", "devices/"+id+"/usage?user_id="+f.user, "", nil), 404)
	f.cookie, f.csrf = loginTest(t, f.handler, nil)
	if err := SetSiteRole(f.db, "other-person", []string{demoSite}, "device_manager"); err != nil {
		t.Fatal(err)
	}
	// Local deactivation must remain assessable as blocked; evidence can still be revoked by another responsible manager.
	if err := DisableUser(f.db, "viewer"); err != nil {
		t.Fatal(err)
	}
	w := authRequest(f.handler, "POST", "/api/v1/auth/login", map[string]string{"username": "other-person", "password": testPassword}, nil, "", testOrigin)
	requireStatus(t, w, 200)
	f.cookie = w.Result().Cookies()[0]
	var session map[string]string
	json.Unmarshal(w.Body.Bytes(), &session)
	f.csrf = session["csrf_token"]
	revoke := usageProof("revoke", 2)
	revoke.ExpectedDeviceVersion = 1
	revoke.UserID = f.user
	revoke.Code = "basic_competence"
	usagePost(t, f, id, "usage-disabled-revoke", revoke)
}
