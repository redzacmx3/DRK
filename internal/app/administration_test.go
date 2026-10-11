package app

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func adminHTTP(h http.Handler, cookie *http.Cookie, csrf, key string, input any) *httptest.ResponseRecorder {
	var body bytes.Buffer
	json.NewEncoder(&body).Encode(input)
	r := httptest.NewRequest("POST", testOrigin+"/api/v1/admin/changes", &body)
	r.AddCookie(cookie)
	r.Header.Set("Origin", testOrigin)
	r.Header.Set("X-CSRF-Token", csrf)
	r.Header.Set("Idempotency-Key", key+"-test")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}
func TestAdministrationWorkflowAndGuards(t *testing.T) {
	db := authDB(t)
	h := Handler(db)
	cookie, csrf := loginTest(t, h, nil)
	requireStatus(t, authRequest(h, "GET", "/api/v1/admin", nil, cookie, "", ""), 403)
	requireStatus(t, adminHTTP(h, cookie, csrf, "admin-denied-0001", map[string]any{"action": "create_operator", "name": "Denied", "note": "Test"}), 403)
	if err := SetAdministrator(db, "viewer", true); err != nil {
		t.Fatal(err)
	}
	cookie, csrf = loginTest(t, h, nil)
	// Administrator status does not imply material or device rights.
	requireStatus(t, stockHTTP(stockFixture{db: db, handler: h, cookie: cookie, csrf: csrf}, "receipts", "admin-no-stock-01", stockRequest{BatchID: demoBatch, To: warehouse, Quantity: 1, State: "available"}), 404)
	version := int64(0)
	active := true
	send := func(key string, c adminChange) adminResult {
		t.Helper()
		w := adminHTTP(h, cookie, csrf, key, c)
		requireStatus(t, w, 200)
		var result adminResult
		if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		return result
	}
	operator := send("admin-operator-01", adminChange{Action: "create_operator", Name: "<img src=x>", Note: "Erster Betreiber"})
	site := send("admin-site-00001", adminChange{Action: "create_site", Name: "Neue Betriebsstätte", ParentID: operator.ID, Note: "Neue Zuordnung"})
	location := send("admin-location01", adminChange{Action: "create_location", Name: "Lager", ParentID: site.ID, Note: "Neuer Lagerort"})
	rename := adminChange{Action: "update_location", ID: location.ID, ExpectedVersion: &location.Version, Name: "Lager 2", Note: "Umbenennung"}
	updated := send("admin-rename-001", rename)
	if updated.Version <= location.Version {
		t.Fatal("version did not advance")
	}
	replay := send("admin-rename-001", rename)
	if replay != updated {
		t.Fatal("replay changed response")
	}
	requireStatus(t, adminHTTP(h, cookie, csrf, "admin-stale-0001", rename), 409)
	rename.ParentID = demoSite
	rename.ExpectedVersion = &updated.Version
	requireStatus(t, adminHTTP(h, cookie, csrf, "admin-move-00001", rename), 400)
	rights := []adminRight{{site.ID, "viewer"}, {site.ID, "helper"}, {demoSite, "device_manager"}}
	account := send("admin-user-00001", adminChange{Action: "create_user", Username: "new-user", Password: testPassword, Active: &active, Rights: rights, Note: "Neues Konto"})
	if account.Kind != "user" {
		t.Fatal("wrong kind")
	}
	// CLI role changes advance the same user revision, rejecting stale browser edits.
	if err := SetSiteRole(db, "new-user", []string{site.ID}, "viewer"); err != nil {
		t.Fatal(err)
	}
	requireStatus(t, adminHTTP(h, cookie, csrf, "admin-user-stale", adminChange{Action: "update_user", ID: account.ID, ExpectedVersion: &account.Version, Active: &active, Rights: rights, Note: "Veralteter Stand"}), 409)
	snapshot := authRequest(h, "GET", "/api/v1/admin", nil, cookie, "", "")
	requireStatus(t, snapshot, 200)
	if strings.Contains(snapshot.Body.String(), testPassword) || strings.Contains(snapshot.Body.String(), "argon2") {
		t.Fatal("secret in snapshot")
	}
	var data struct {
		Objects []adminObject `json:"objects"`
	}
	json.Unmarshal(snapshot.Body.Bytes(), &data)
	for _, o := range data.Objects {
		if o.ID == account.ID {
			version = o.Version
		}
	}
	reset := send("admin-password01", adminChange{Action: "reset_password", ID: account.ID, ExpectedVersion: &version, Password: "new-secret-password", Note: "Passwortwechsel"})
	inactive := false
	send("admin-disable001", adminChange{Action: "update_user", ID: account.ID, ExpectedVersion: &reset.Version, Active: &inactive, Rights: []adminRight{}, Note: "Konto deaktivieren"})
	var ownID string
	db.QueryRow(`SELECT id FROM users WHERE username='viewer'`).Scan(&ownID)
	for _, o := range data.Objects {
		if o.ID == ownID {
			version = o.Version
		}
	}
	requireStatus(t, adminHTTP(h, cookie, csrf, "admin-self-off01", adminChange{Action: "update_user", ID: ownID, ExpectedVersion: &version, Active: &inactive, Rights: []adminRight{}, Note: "Selbst abschalten"}), 409)
	if err := DisableUser(db, "viewer"); err == nil {
		t.Fatal("last administrator disabled locally")
	}
	// Shared namespace and immutable audit; no password or hash in persistent request/audit payloads.
	var count int
	db.QueryRow(`SELECT COUNT(*) FROM administrative_events WHERE payload_json LIKE '%secret-password%' OR payload_json LIKE '%argon2%'`).Scan(&count)
	if count != 0 {
		t.Fatal("secret persisted in audit")
	}
	if _, err := db.Exec(`UPDATE administrative_events SET action='tampered'`); err == nil {
		t.Fatal("mutable audit")
	}
	if _, err := db.Exec(`DELETE FROM administrative_requests`); err == nil {
		t.Fatal("mutable replay")
	}
	tx, _ := db.BeginTx(context.Background(), nil)
	if err := rejectOtherKey(context.Background(), tx, ownID, "admin-operator-01-test", "idempotency_keys"); err == nil {
		t.Fatal("cross-module key accepted")
	}
	tx.Rollback()
	if err := SetAdministrator(db, "viewer", false); err != nil {
		t.Fatal(err)
	}
	cookie, csrf = loginTest(t, h, nil)
	requireStatus(t, adminHTTP(h, cookie, csrf, "admin-rename-001", rename), 403)
}
func TestAdministrationFreshBootstrapAndRevocation(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "fresh.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = Migrate(db); err != nil {
		t.Fatal(err)
	}
	if err = CreateAdministrator(db, "bootstrap", testPassword); err != nil {
		t.Fatal(err)
	}
	var count int
	db.QueryRow(`SELECT COUNT(*) FROM role_assignments`).Scan(&count)
	if count != 0 {
		t.Fatal("automatic site rights")
	}
	h := Handler(db)
	w := authRequest(h, "POST", "/api/v1/auth/login", map[string]string{"username": "bootstrap", "password": testPassword}, nil, "", testOrigin)
	requireStatus(t, w, 200)
	cookie := w.Result().Cookies()[0]
	var login map[string]string
	json.Unmarshal(w.Body.Bytes(), &login)
	csrf := login["csrf_token"]
	requireStatus(t, authRequest(h, "GET", "/api/v1/locations", nil, cookie, "", ""), 200)
	requireStatus(t, adminHTTP(h, cookie, csrf, "fresh-admin-0001", adminChange{Action: "create_operator", Name: "Erster Betreiber", Note: "Einrichtung"}), 200)
	var id string
	var version int64
	db.QueryRow(`SELECT id FROM users WHERE username='bootstrap'`).Scan(&id)
	db.QueryRow(`SELECT version FROM administrative_versions WHERE kind='user' AND object_id=?`, id).Scan(&version)
	active := true
	c := adminChange{Action: "update_user", ID: id, ExpectedVersion: &version, Active: &active, Rights: []adminRight{}, Note: "Rechte bestätigen"}
	requireStatus(t, adminHTTP(h, cookie, csrf, "fresh-admin-0002", c), 200)
	requireStatus(t, authRequest(h, "GET", "/api/v1/me", nil, cookie, "", ""), 401)
	if err = SetAdministrator(db, "bootstrap", false); err != nil {
		t.Fatal(err)
	}
	if err = DisableUser(db, "bootstrap"); err != nil {
		t.Fatal(err)
	}
}

func TestAdministrationAtomicSessionRevocation(t *testing.T) {
	db := authDB(t)
	if err := CreateAdministrator(db, "session-admin", testPassword); err != nil {
		t.Fatal(err)
	}
	h := Handler(db)
	targetCookie, _ := loginTest(t, h, nil)
	login := authRequest(h, "POST", "/api/v1/auth/login", map[string]string{"username": "session-admin", "password": testPassword}, nil, "", testOrigin)
	requireStatus(t, login, 200)
	adminCookie := login.Result().Cookies()[0]
	var session map[string]string
	json.Unmarshal(login.Body.Bytes(), &session)
	var id string
	var version int64
	db.QueryRow(`SELECT id FROM users WHERE username='viewer'`).Scan(&id)
	db.QueryRow(`SELECT version FROM administrative_versions WHERE kind='user' AND object_id=?`, id).Scan(&version)
	change := adminChange{Action: "reset_password", ID: id, ExpectedVersion: &version, Password: "replaced-test-secret", Note: "Passwortwechsel prüfen"}
	// A failure persisting the replay record must roll back password, revision, event and session revocation.
	if _, err := db.Exec(`CREATE TRIGGER fail_admin_request BEFORE INSERT ON administrative_requests BEGIN SELECT RAISE(ABORT,'synthetic persistence failure'); END`); err != nil {
		t.Fatal(err)
	}
	requireStatus(t, adminHTTP(h, adminCookie, session["csrf_token"], "atomic-admin-password", change), 503)
	requireStatus(t, authRequest(h, "GET", "/api/v1/me", nil, targetCookie, "", ""), 200)
	var unchanged int64
	db.QueryRow(`SELECT version FROM administrative_versions WHERE kind='user' AND object_id=?`, id).Scan(&unchanged)
	if unchanged != version {
		t.Fatal("revision committed during failure")
	}
	if _, err := db.Exec(`DROP TRIGGER fail_admin_request`); err != nil {
		t.Fatal(err)
	}
	result := adminHTTP(h, adminCookie, session["csrf_token"], "atomic-admin-password", change)
	requireStatus(t, result, 200)
	requireStatus(t, authRequest(h, "GET", "/api/v1/me", nil, targetCookie, "", ""), 401)
	requireStatus(t, authRequest(h, "POST", "/api/v1/auth/login", map[string]string{"username": "viewer", "password": testPassword}, nil, "", testOrigin), 401)
	requireStatus(t, authRequest(h, "POST", "/api/v1/auth/login", map[string]string{"username": "viewer", "password": "replaced-test-secret"}, nil, "", testOrigin), 200)
	replay := adminHTTP(h, adminCookie, session["csrf_token"], "atomic-admin-password", change)
	requireStatus(t, replay, 200)
	if result.Body.String() != replay.Body.String() {
		t.Fatal("password replay changed")
	}
}
