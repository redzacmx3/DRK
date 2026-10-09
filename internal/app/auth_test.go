package app

import (
	"bytes"
	"crypto/tls"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"
)

const testOrigin = "https://localhost:8080"
const demoSite = "00000000-0000-4000-8000-000000000002"
const testPassword = "synthetic-test-password-2026"

func authDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := Open(filepath.Join(t.TempDir(), "auth.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err = Migrate(db); err != nil {
		t.Fatal(err)
	}
	if err = SeedDemo(db); err != nil {
		t.Fatal(err)
	}
	if err = CreateViewer(db, "viewer", testPassword, demoSite); err != nil {
		t.Fatal(err)
	}
	return db
}
func authRequest(h http.Handler, method, path string, body any, cookie *http.Cookie, csrf, origin string) *httptest.ResponseRecorder {
	var b bytes.Buffer
	if body != nil {
		json.NewEncoder(&b).Encode(body)
	}
	r := httptest.NewRequest(method, testOrigin+path, &b)
	r.TLS = &tls.ConnectionState{}
	r.RemoteAddr = "127.0.0.1:12345"
	r.Header.Set("Content-Type", "application/json")
	if origin != "" {
		r.Header.Set("Origin", origin)
	}
	if cookie != nil {
		r.AddCookie(cookie)
	}
	if csrf != "" {
		r.Header.Set("X-CSRF-Token", csrf)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}
func loginTest(t *testing.T, h http.Handler, old *http.Cookie) (*http.Cookie, string) {
	t.Helper()
	w := authRequest(h, "POST", "/api/v1/auth/login", map[string]string{"username": "viewer", "password": testPassword}, old, "", testOrigin)
	if w.Code != 200 {
		t.Fatalf("login: %d %s", w.Code, w.Body.String())
	}
	cookies := w.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatal("missing session cookie")
	}
	c := cookies[0]
	if c.Name != cookieName || !c.Secure || !c.HttpOnly || c.Path != "/" || c.Domain != "" || c.SameSite != http.SameSiteLaxMode {
		t.Fatalf("cookie policy: %+v", c)
	}
	var result map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result["csrf_token"] == "" {
		t.Fatal("missing csrf")
	}
	return c, result["csrf_token"]
}
func requireStatus(t *testing.T, w *httptest.ResponseRecorder, want int) {
	t.Helper()
	if w.Code != want {
		t.Fatalf("status %d want %d: %s", w.Code, want, w.Body.String())
	}
}
func TestSessionsAndObjectScopes(t *testing.T) {
	db := authDB(t)
	h := Handler(db)
	requireStatus(t, authRequest(h, "GET", "/api/v1/locations", nil, nil, "", ""), 401)
	cookie, csrf := loginTest(t, h, nil)
	var tokenHash, csrfHash string
	if err := db.QueryRow(`SELECT token_hash,csrf_hash FROM sessions`).Scan(&tokenHash, &csrfHash); err != nil {
		t.Fatal(err)
	}
	if tokenHash == cookie.Value || tokenHash != digest(cookie.Value) || csrfHash != digest(csrf) {
		t.Fatal("session/CSRF hash storage")
	}
	var locations []Location
	w := authRequest(h, "GET", "/api/v1/locations", nil, cookie, "", "")
	requireStatus(t, w, 200)
	json.Unmarshal(w.Body.Bytes(), &locations)
	if len(locations) != 2 {
		t.Fatalf("locations %d", len(locations))
	}
	_, err := db.Exec(`INSERT INTO sites VALUES('other-site','00000000-0000-4000-8000-000000000001','Other'); INSERT INTO locations VALUES('hidden-location','other-site','Hidden')`)
	if err != nil {
		t.Fatal(err)
	}
	requireStatus(t, authRequest(h, "GET", "/api/v1/locations/hidden-location", nil, cookie, "", ""), 404)
	requireStatus(t, authRequest(h, "GET", "/api/v1/locations/00000000-0000-4000-8000-000000000003", nil, cookie, "", ""), 200)
	w = authRequest(h, "GET", "/api/v1/locations", nil, cookie, "", "")
	json.Unmarshal(w.Body.Bytes(), &locations)
	if len(locations) != 2 {
		t.Fatal("scope leaked")
	}
	w = authRequest(h, "GET", "/api/v1/me", nil, cookie, "", "")
	requireStatus(t, w, 200)
	var me struct {
		CSRF   string `json:"csrf_token"`
		Rights []struct {
			SiteID string `json:"site_id"`
		} `json:"rights"`
	}
	json.Unmarshal(w.Body.Bytes(), &me)
	if len(me.Rights) != 1 || me.Rights[0].SiteID != demoSite {
		t.Fatal("incorrect rights in identity response")
	}
	if me.CSRF != csrf {
		t.Fatal("csrf unavailable after refresh")
	}
	requireStatus(t, authRequest(h, "POST", "/api/v1/auth/logout", nil, cookie, csrf, "https://foreign.example"), 403)
	requireStatus(t, authRequest(h, "POST", "/api/v1/auth/logout", nil, cookie, "", testOrigin), 403)
	requireStatus(t, authRequest(h, "POST", "/api/v1/auth/logout", nil, cookie, csrf, testOrigin), 204)
	requireStatus(t, authRequest(h, "GET", "/api/v1/me", nil, cookie, "", ""), 401)
	cookie, _ = loginTest(t, h, nil)
	replacement, _ := loginTest(t, h, cookie)
	if replacement.Value == cookie.Value {
		t.Fatal("session not rotated")
	}
	requireStatus(t, authRequest(h, "GET", "/api/v1/me", nil, cookie, "", ""), 401)
	if err = SetViewerSites(db, "viewer", []string{"other-site"}); err != nil {
		t.Fatal(err)
	}
	requireStatus(t, authRequest(h, "GET", "/api/v1/me", nil, replacement, "", ""), 401)
	cookie, _ = loginTest(t, h, nil)
	requireStatus(t, authRequest(h, "GET", "/api/v1/locations/hidden-location", nil, cookie, "", ""), 200)
	requireStatus(t, authRequest(h, "GET", "/api/v1/locations/00000000-0000-4000-8000-000000000003", nil, cookie, "", ""), 404)
	if err = DisableUser(db, "viewer"); err != nil {
		t.Fatal(err)
	}
	requireStatus(t, authRequest(h, "GET", "/api/v1/me", nil, cookie, "", ""), 401)
	requireStatus(t, authRequest(h, "POST", "/api/v1/auth/login", map[string]string{"username": "viewer", "password": testPassword}, nil, "", testOrigin), 401)
}
func TestExpiryAndLoginGuards(t *testing.T) {
	db := authDB(t)
	h := Handler(db)
	input := map[string]string{"username": "viewer", "password": testPassword}
	requireStatus(t, authRequest(h, "POST", "/api/v1/auth/login", input, nil, "", ""), 403)
	r := httptest.NewRequest("POST", "http://localhost:8080/api/v1/auth/login", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	requireStatus(t, w, 400)
	requireStatus(t, authRequest(h, "POST", "/api/v1/auth/login", map[string]string{"username": "viewer", "password": "incorrect"}, nil, "", testOrigin), 401)
	requireStatus(t, authRequest(h, "POST", "/api/v1/auth/login", map[string]string{"username": "unknown", "password": testPassword}, nil, "", testOrigin), 401)
	cookie, _ := loginTest(t, h, nil)
	if _, err := db.Exec(`UPDATE sessions SET last_seen=?`, time.Now().Add(-31*time.Minute).Unix()); err != nil {
		t.Fatal(err)
	}
	requireStatus(t, authRequest(h, "GET", "/api/v1/me", nil, cookie, "", ""), 401)
	cookie, _ = loginTest(t, h, nil)
	if _, err := db.Exec(`UPDATE sessions SET created_at=?`, time.Now().Add(-9*time.Hour).Unix()); err != nil {
		t.Fatal(err)
	}
	requireStatus(t, authRequest(h, "GET", "/api/v1/me", nil, cookie, "", ""), 401)
	// A new handler has an empty rate-limit window.
	a := newAuth(db, testOrigin)
	mux := http.NewServeMux()
	a.routes(mux)
	for range 10 {
		r := httptest.NewRequest("POST", "/", nil)
		r.RemoteAddr = "127.0.0.1:12345"
		if !a.allowLogin(r) {
			t.Fatal("unexpected limit")
		}
	}
	r2 := httptest.NewRequest("POST", "/", nil)
	r2.RemoteAddr = "127.0.0.1:12345"
	if a.allowLogin(r2) {
		t.Fatal("rate limit missing")
	}
	requireStatus(t, authRequest(mux, "POST", "/api/v1/auth/login", input, nil, "", testOrigin), 429)
}
func TestScopeChangeRollback(t *testing.T) {
	db := authDB(t)
	if SetViewerSites(db, "viewer", []string{"missing-site"}) == nil {
		t.Fatal("invalid scope accepted")
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM role_assignments`).Scan(&count); err != nil || count != 1 {
		t.Fatal("failed scope change did not roll back")
	}
}
