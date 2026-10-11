package app

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"testing"
	"time"
)

func accessProof(document documentRecord, user string, version int) manualAccessInput {
	yes := true
	return manualAccessInput{ExpectedVersion: version, UserID: user, Action: "confirm", DocumentID: document.ID, ManualVersion: "Manual 1", Language: "Deutsch", Accessible: &yes, ValidUntil: time.Now().Add(time.Hour).UTC().Format(time.RFC3339), Evidence: "Synthetic access reference", Qualification: "Synthetic reviewer qualification", Assignment: "Synthetic operator assignment", Note: "Synthetic access review"}
}
func accessPost(t *testing.T, f stockFixture, id, key string, in manualAccessInput, status int) manualAccessEvent {
	t.Helper()
	w := packHTTP(f, "POST", "devices/"+id+"/manual-access", key, in)
	requireStatus(t, w, status)
	var event manualAccessEvent
	if status == 200 {
		if err := json.Unmarshal(w.Body.Bytes(), &event); err != nil {
			t.Fatal(err)
		}
	}
	return event
}
func accessRead(t *testing.T, f stockFixture, id string) manualAccessAssessment {
	t.Helper()
	w := packHTTP(f, "GET", "devices/"+id+"/manual-access", "", nil)
	requireStatus(t, w, 200)
	var out struct {
		Assessment manualAccessAssessment `json:"assessment"`
	}
	json.Unmarshal(w.Body.Bytes(), &out)
	return out.Assessment
}
func TestManualAccessReleasedDocumentBefristungRevocationAndRollback(t *testing.T) {
	t.Setenv("DRK_CLAMD_ADDRESS", fakeClamd(t, time.Now(), "stream: OK"))
	f, id, document := documentFixture(t)
	in := accessProof(document, f.user, 0)
	if accessRead(t, f, id).Status != "unknown" {
		t.Fatal("missing proof accepted")
	}
	accessPost(t, f, id, "quarantined-manual-access", in, 409)
	reviewPost(t, f, id, document.ID, "access-document-scan", reviewProof(document, "scan", 0), 200)
	reviewPost(t, f, id, document.ID, "access-document-release", reviewProof(document, "release", 1), 200)
	invalid := in
	invalid.Accessible = nil
	accessPost(t, f, id, "implicit-manual-access", invalid, 400)
	invalid = in
	invalid.ValidUntil = time.Now().Add(25 * time.Hour).Format(time.RFC3339)
	accessPost(t, f, id, "long-manual-access", invalid, 400)
	f.db.Exec(`CREATE TRIGGER fail_access BEFORE INSERT ON manual_access_requests BEGIN SELECT RAISE(ABORT,'synthetic failure'); END`)
	accessPost(t, f, id, "atomic-manual-access", in, 503)
	if accessRead(t, f, id).Version != 0 {
		t.Fatal("partial access commit")
	}
	f.db.Exec(`DROP TRIGGER fail_access`)
	initial := accessPost(t, f, id, "atomic-manual-access", in, 200)
	if got := accessRead(t, f, id); got.Status != "confirmed" || got.Overall != "not_assessed" {
		t.Fatal(got)
	}
	tx, _ := f.db.Begin()
	record, err := readDevice(context.Background(), tx, f.user, id, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	expired, err := assessManualAccess(context.Background(), tx, f.user, f.user, record, time.Now().Add(2*time.Hour))
	tx.Rollback()
	if err != nil || expired.Status != "unknown" {
		t.Fatal(expired, err)
	}
	accessPost(t, f, id, "stale-manual-access", in, 409)
	revoke := in
	revoke.Action = "revoke"
	revoke.ExpectedVersion = 1
	revoke.DocumentID = ""
	revoke.ManualVersion = ""
	revoke.Language = ""
	revoke.ValidUntil = ""
	revoke.Accessible = nil
	accessPost(t, f, id, "revoke-manual-access", revoke, 200)
	if accessRead(t, f, id).Status != "blocked" {
		t.Fatal("revocation ignored")
	}
	if replay := accessPost(t, f, id, "atomic-manual-access", in, 200); replay.ID != initial.ID {
		t.Fatal("replay changed")
	}
	in.ExpectedVersion = 2
	accessPost(t, f, id, "renew-manual-access", in, 200)
	reviewPost(t, f, id, document.ID, "access-document-quarantine", reviewProof(document, "quarantine", 2), 200)
	if accessRead(t, f, id).Status != "unknown" {
		t.Fatal("document revocation ignored")
	}
	if _, err = f.db.Exec(`UPDATE manual_access_events SET action='confirm'`); err == nil {
		t.Fatal("mutable access proof")
	}
	SetSiteRole(f.db, "viewer", []string{demoSite}, "viewer")
	f.cookie, f.csrf = loginTest(t, f.handler, nil)
	accessPost(t, f, id, "atomic-manual-access", accessProof(document, f.user, 0), 404)
	requireStatus(t, packHTTP(f, "GET", "devices/"+id+"/manual-access?user_id=another-user", "", nil), 404)
}
func TestManualAccessIdentityChangesAndExport(t *testing.T) {
	t.Setenv("DRK_CLAMD_ADDRESS", fakeClamd(t, time.Now(), "stream: OK"))
	f, id, document := documentFixture(t)
	reviewPost(t, f, id, document.ID, "identity-manual-scan", reviewProof(document, "scan", 0), 200)
	reviewPost(t, f, id, document.ID, "identity-manual-release", reviewProof(document, "release", 1), 200)
	in := accessProof(document, f.user, 0)
	accessPost(t, f, id, "identity-manual-access", in, 200)
	details := deviceControlInput{ExpectedVersion: 0, Details: &deviceDetails{Name: "New identity", Manufacturer: "Different maker", Model: "Different model", SerialOrLot: "OTHER"}, Evidence: "Synthetic correction reference", Note: "Synthetic device identity update"}
	requireStatus(t, packHTTP(f, "POST", "devices/"+id+"/details", "manual-details-change", details), 200)
	if accessRead(t, f, id).Status != "unknown" {
		t.Fatal("identity change ignored")
	}
	in.ExpectedVersion = 1
	accessPost(t, f, id, "stale-device-access", in, 409)
	requireStatus(t, packHTTP(f, "GET", "devices/"+id+"/export.zip", "", nil), 200)
}
func TestOperatingEvidenceSeparatesFailuresFromUnknownProductDuties(t *testing.T) {
	f, id := packDeviceFixture(t)
	input := packInput(warehouse)
	input.Devices = []packDevice{{DeviceID: id}}
	pack := published(t, f, input, "evidence-pack-version")
	before := assessment(t, f, pack.ID)
	if before.Evidence.Status != "blocked" || before.Overall != "not_assessed" {
		t.Fatal(before)
	}
	kinds := map[string]string{}
	for _, component := range before.Evidence.Components {
		kinds[component.Kind] = component.Status
	}
	if kinds["product_duties"] != "unknown" || kinds["manual_access"] != "unknown" || kinds["device_function"] != "needs_check" {
		t.Fatal(kinds)
	}
	// Consolidation never turns all recorded prerequisites into an unimplemented release.
	prepared := packAssessment{Material: materialReadiness{Status: "ready", CurrentVersion: true}, Check: &deviceCheckProof{Current: true, Passed: true}, Devices: []deviceAssessment{{Device: deviceRecord{CommissionedOn: "2024-01-01", Controls: map[string]Deadline{"STK": {State: "NOT_APPLICABLE"}, "MTK": {State: "NOT_APPLICABLE"}, "electrical": {State: "NOT_APPLICABLE"}}}, UserUsage: &usageSummary{Status: "recorded_requirements_met"}, ManualAccess: &manualAccessSummary{Status: "confirmed"}}}}
	got := consolidateEvidence(prepared)
	if got.Status != "incomplete" {
		t.Fatal(got)
	}
	for _, component := range got.Components {
		if component.Kind == "product_duties" && component.Status != "unknown" {
			t.Fatal(component)
		}
	}
	prepared.Check.Passed = false
	got = consolidateEvidence(prepared)
	for _, component := range got.Components {
		if component.Kind == "device_function" && component.Status != "blocked" {
			t.Fatal(component)
		}
		if component.Kind == "device_controls" && component.Status == "blocked" {
			t.Fatal("physical failure confused with technical control failure")
		}
	}
}

func TestManualAccessFormerUsersAndScopedArchive(t *testing.T) {
	t.Setenv("DRK_CLAMD_ADDRESS", fakeClamd(t, time.Now(), "stream: OK"))
	f, id, document := documentFixture(t)
	reviewPost(t, f, id, document.ID, "private-manual-scan", reviewProof(document, "scan", 0), 200)
	reviewPost(t, f, id, document.ID, "private-manual-release", reviewProof(document, "release", 1), 200)
	own := accessProof(document, f.user, 0)
	accessPost(t, f, id, "private-own-access", own, 200)
	if err := CreateUser(f.db, "other-person", "SyntheticPassword123", demoSite, "viewer"); err != nil {
		t.Fatal(err)
	}
	var other string
	f.db.QueryRow(`SELECT id FROM users WHERE username='other-person'`).Scan(&other)
	foreign := accessProof(document, other, 1)
	accessPost(t, f, id, "private-other-access", foreign, 200)
	f.db.Exec(`UPDATE users SET active=0 WHERE id=?`, other)
	foreign.ExpectedVersion = 2
	accessPost(t, f, id, "inactive-manual-confirm", foreign, 404)
	revoke := manualAccessInput{ExpectedVersion: 2, UserID: other, Action: "revoke", Evidence: "Synthetic former-person revocation", Qualification: "Reviewer qualification", Assignment: "Operator assignment", Note: "Synthetic revocation of departed person"}
	accessPost(t, f, id, "inactive-manual-revoke", revoke, 200)
	readBook := func() deviceBookExport {
		w := packHTTP(f, "GET", "devices/"+id+"/export.zip", "", nil)
		requireStatus(t, w, 200)
		z, err := zip.NewReader(bytes.NewReader(w.Body.Bytes()), int64(w.Body.Len()))
		if err != nil {
			t.Fatal(err)
		}
		for _, file := range z.File {
			if file.Name == "akte.json" {
				reader, err := file.Open()
				if err != nil {
					t.Fatal(err)
				}
				data, err := io.ReadAll(reader)
				reader.Close()
				if err != nil {
					t.Fatal(err)
				}
				var book deviceBookExport
				if err = json.Unmarshal(data, &book); err != nil {
					t.Fatal(err)
				}
				return book
			}
		}
		t.Fatal("missing archive record")
		return deviceBookExport{}
	}
	if len(readBook().ManualAccess) != 3 {
		t.Fatal("manager history incomplete")
	}
	SetSiteRole(f.db, "viewer", []string{demoSite}, "viewer")
	f.cookie, f.csrf = loginTest(t, f.handler, nil)
	book := readBook()
	if len(book.ManualAccess) != 1 {
		t.Fatal("foreign private evidence leaked", book.ManualAccess)
	}
	var proof manualAccessEvent
	json.Unmarshal(book.ManualAccess[0].Proof, &proof)
	if proof.Input.UserID != f.user {
		t.Fatal("wrong person exported")
	}
	requireStatus(t, packHTTP(f, "GET", "devices/"+id+"/manual-access?user_id="+other, "", nil), 404)
}
