package app

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"database/sql"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const setupTestOrigin = "http://localhost:8080"

var setupTestRequestNumber atomic.Uint64

type setupTestFixture struct {
	db       *sql.DB
	dir      string
	codeFile string
	code     string
	handler  http.Handler
	cookie   *http.Cookie
	csrf     string
	revision int
}

func freshSetupDB(t *testing.T) (*sql.DB, string) {
	t.Helper()
	dir := t.TempDir()
	db, err := Open(filepath.Join(dir, "setup.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}
	if _, _, err := InitializeModules(context.Background(), db, allModules()); err != nil {
		t.Fatal(err)
	}
	return db, dir
}

func webSetupFixture(t *testing.T) setupTestFixture {
	t.Helper()
	db, dir := freshSetupDB(t)
	f := setupTestFixture{db: db, dir: dir, codeFile: filepath.Join(dir, "first-setup-code")}
	pending, err := PrepareWebSetup(context.Background(), db, f.codeFile)
	if err != nil || !pending {
		t.Fatalf("fresh setup not prepared: %v %v", pending, err)
	}
	code, err := os.ReadFile(f.codeFile)
	if err != nil {
		t.Fatal(err)
	}
	f.code = strings.TrimSpace(string(code))
	f.handler = HandlerWithRuntime(db, setupTestOrigin, true, RuntimeOptions{Modules: allModules(), DataDir: dir, SetupCodeFile: f.codeFile})
	f.refreshStatus(t)
	return f
}

func setupRequest(handler http.Handler, method, path string, input any, cookie *http.Cookie, csrf, origin string) *httptest.ResponseRecorder {
	var body bytes.Buffer
	if input != nil {
		if err := json.NewEncoder(&body).Encode(input); err != nil {
			panic(err)
		}
	}
	r := httptest.NewRequest(method, setupTestOrigin+"/api/v1/setup/"+path, &body)
	// Distinct synthetic clients let validation tests stay independent of the
	// separately tested per-address public bootstrap attempt limiter.
	r.RemoteAddr = "192.0.2." + strconv.FormatUint(setupTestRequestNumber.Add(1)%250+1, 10) + ":23456"
	r.Header.Set("Content-Type", "application/json")
	if origin != "" {
		r.Header.Set("Origin", origin)
	}
	if csrf != "" {
		r.Header.Set("X-CSRF-Token", csrf)
	}
	if cookie != nil {
		r.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	return w
}

func (f *setupTestFixture) refreshStatus(t *testing.T) {
	t.Helper()
	w := setupRequest(f.handler, "GET", "status", nil, nil, "", "")
	requireStatus(t, w, 200)
	var status struct {
		Pending  bool               `json:"pending"`
		CSRF     string             `json:"csrf_token"`
		Revision int                `json:"revision"`
		Modules  []moduleDefinition `json:"modules"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &status); err != nil || !status.Pending || status.CSRF == "" || len(status.Modules) != len(moduleDefinitions) {
		t.Fatalf("incomplete setup metadata: %s (%v)", w.Body.String(), err)
	}
	cookies := w.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatal("setup CSRF cookie missing")
	}
	cookie := cookies[0]
	if cookie.Name != "drk-local-setup" || cookie.Secure || !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode || cookie.Path != "/" || cookie.Domain != "" {
		t.Fatalf("unsafe setup cookie: %+v", cookie)
	}
	if strings.Contains(w.Body.String(), f.code) || strings.Contains(w.Body.String(), f.dir) {
		t.Fatal("public setup status exposes code or filesystem")
	}
	f.cookie, f.csrf, f.revision = cookie, status.CSRF, status.Revision
}

func (f setupTestFixture) input() map[string]any {
	return map[string]any{
		"setup_code": f.code, "expected_revision": f.revision,
		"username": "setup-admin", "password": testPassword,
		"operator_name": "Test operator", "site_name": "Test site", "location_name": "Test equipment location",
		"enabled_modules": []string{"inventory"}, "material_manager": false, "device_manager": false,
	}
}

func (f setupTestFixture) request(path string, input any) *httptest.ResponseRecorder {
	return setupRequest(f.handler, "POST", path, input, f.cookie, f.csrf, setupTestOrigin)
}

func setupCount(t *testing.T, db *sql.DB, table string) int {
	t.Helper()
	var n int
	if err := db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestWebSetupSecretProofAndEnvironmentChecks(t *testing.T) {
	f := webSetupFixture(t)
	if len(f.code) != 43 {
		t.Fatal("setup code lacks random token length")
	}
	info, err := os.Stat(f.codeFile)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("setup code is not private", info, err)
	}
	var storedHash string
	if err := f.db.QueryRow(`SELECT token_hash FROM web_setup_state WHERE id=1`).Scan(&storedHash); err != nil || storedHash != digest(f.code) || storedHash == f.code {
		t.Fatal("plaintext setup code in database", err)
	}
	if pending, err := PrepareWebSetup(context.Background(), f.db, f.codeFile); err != nil || !pending {
		t.Fatal("restart did not preserve setup state", pending, err)
	}
	code, err := os.ReadFile(f.codeFile)
	if err != nil || strings.TrimSpace(string(code)) != f.code {
		t.Fatal("restart rotated one-time code", err)
	}
	checks := map[string]any{"setup_code": f.code, "enabled_modules": []string{"inventory"}}
	for _, proof := range []struct {
		cookie *http.Cookie
		csrf   string
		origin string
	}{
		{nil, f.csrf, setupTestOrigin},
		{f.cookie, "", setupTestOrigin},
		{f.cookie, randomToken(), setupTestOrigin},
		{f.cookie, f.csrf, ""},
		{f.cookie, f.csrf, "https://foreign.example"},
	} {
		requireStatus(t, setupRequest(f.handler, "POST", "checks", checks, proof.cookie, proof.csrf, proof.origin), 403)
		requireStatus(t, setupRequest(f.handler, "POST", "complete", f.input(), proof.cookie, proof.csrf, proof.origin), 403)
	}
	checks["setup_code"] = randomToken()
	requireStatus(t, f.request("checks", checks), 403)
	badComplete := f.input()
	badComplete["setup_code"] = randomToken()
	requireStatus(t, f.request("complete", badComplete), 403)
	checks["setup_code"] = f.code
	w := f.request("checks", checks)
	requireStatus(t, w, 200)
	var result struct {
		CanComplete bool `json:"can_complete"`
		Checks      []struct {
			ID       string `json:"id"`
			Status   string `json:"status"`
			Required bool   `json:"required"`
		} `json:"checks"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil || !result.CanComplete || len(result.Checks) == 0 {
		t.Fatalf("valid local installation failed checks: %s (%v)", w.Body.String(), err)
	}
	for _, check := range result.Checks {
		if check.Required && check.Status != "ok" {
			t.Fatal("can_complete ignored required failure", check)
		}
	}
	if setupCount(t, f.db, "users") != 0 {
		t.Fatal("read-only checks created administrator")
	}
}

func TestWebSetupFreshCompleteExplicitRolesAndPermanentClosure(t *testing.T) {
	f := webSetupFixture(t)
	input := f.input()
	input["enabled_modules"] = []string{"inventory", "devices"}
	input["material_manager"], input["device_manager"] = true, true
	w := f.request("complete", input)
	requireStatus(t, w, 201)
	if len(w.Result().Cookies()) != 0 {
		for _, c := range w.Result().Cookies() {
			if c.Name == "drk-local-session" || c.Name == cookieName {
				t.Fatal("setup unexpectedly logged browser into account")
			}
		}
	}
	for _, table := range []string{"users", "administrators", "operators", "sites", "locations"} {
		if n := setupCount(t, f.db, table); n != 1 {
			t.Fatal("setup did not create exactly one foundational object", table, n)
		}
	}
	var username, hash string
	var active bool
	if err := f.db.QueryRow(`SELECT username,password_hash,active FROM users`).Scan(&username, &hash, &active); err != nil || username != "setup-admin" || !active || !passwordMatches(hash, testPassword) {
		t.Fatal("invalid initial administrator password/account", err)
	}
	rows, err := f.db.Query(`SELECT role,site_id=(SELECT id FROM sites) FROM role_assignments ORDER BY role`)
	if err != nil {
		t.Fatal(err)
	}
	var roles []string
	for rows.Next() {
		var role string
		var scoped bool
		if err := rows.Scan(&role, &scoped); err != nil || !scoped {
			t.Fatal("unscoped setup role", err)
		}
		roles = append(roles, role)
	}
	if err := rows.Close(); err != nil || strings.Join(roles, ",") != "device_manager,material_manager" {
		t.Fatal("setup did not use requested site roles", roles, err)
	}
	var moduleNames string
	if err := f.db.QueryRow(`SELECT modules FROM module_configuration_events ORDER BY version DESC LIMIT 1`).Scan(&moduleNames); err != nil || moduleNames != "inventory,devices" {
		t.Fatal("initial feature selection not applied", moduleNames, err)
	}
	var eventPayload string
	rows, err = f.db.Query(`SELECT payload_json FROM administrative_events`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		if err := rows.Scan(&eventPayload); err != nil || strings.Contains(eventPayload, testPassword) || strings.Contains(eventPayload, f.code) || strings.Contains(eventPayload, "argon2") {
			t.Fatal("setup persisted a secret in administrative audit", err)
		}
	}
	rows.Close()
	if setupCount(t, f.db, "administrative_events") == 0 {
		t.Fatal("initial setup has no auditable creation")
	}
	if _, err := os.Stat(f.codeFile); !os.IsNotExist(err) {
		t.Fatal("consumed setup code file remains", err)
	}
	requireStatus(t, f.request("complete", input), 409)
	requireStatus(t, f.request("checks", map[string]any{"setup_code": f.code, "enabled_modules": []string{"inventory"}}), 409)
	status := setupRequest(f.handler, "GET", "status", nil, nil, "", "")
	requireStatus(t, status, 200)
	var closed map[string]any
	if err := json.Unmarshal(status.Body.Bytes(), &closed); err != nil || closed["pending"] != false || closed["csrf_token"] != nil || closed["modules"] != nil || len(status.Result().Cookies()) != 0 {
		t.Fatal("closed public setup leaks configuration", status.Body.String(), err)
	}
	for _, statement := range []string{
		`UPDATE web_setup_state SET completed_at=NULL,admin_id=NULL,completion_reason='' WHERE id=1`,
		`DELETE FROM web_setup_state WHERE id=1`,
	} {
		if _, err := f.db.Exec(statement); err == nil {
			t.Fatal("completed bootstrap can be reopened by rewriting state", statement)
		}
	}
	if err := SetAdministrator(f.db, "setup-admin", false); err != nil {
		t.Fatal(err)
	}
	if err := DisableUser(f.db, "setup-admin"); err != nil {
		t.Fatal(err)
	}
	if pending, err := PrepareWebSetup(context.Background(), f.db, f.codeFile); err != nil || pending {
		t.Fatal("removing administrators reopens public bootstrap", pending, err)
	}
	if _, err := os.Stat(f.codeFile); !os.IsNotExist(err) {
		t.Fatal("recovery recreated bootstrap code", err)
	}
}

func TestWebSetupValidationAtomicRollbackAndOptionalRoles(t *testing.T) {
	f := webSetupFixture(t)
	for _, changed := range []map[string]any{
		{"username": "ab"}, {"password": "short"}, {"operator_name": ""}, {"site_name": ""}, {"location_name": ""},
		{"material_manager": "true"}, {"device_manager": "true"},
		{"enabled_modules": []string{}}, {"enabled_modules": []string{"inventory", "DOCUMENTS"}},
		{"enabled_modules": []string{"inventory", "documents"}}, {"enabled_modules": []string{"inventory", "inventory"}},
	} {
		input := f.input()
		for key, value := range changed {
			input[key] = value
		}
		requireStatus(t, f.request("complete", input), 400)
	}
	input := f.input()
	input["expected_revision"] = f.revision + 1
	requireStatus(t, f.request("complete", input), 409)
	if _, err := f.db.Exec(`CREATE TRIGGER setup_synthetic_failure BEFORE INSERT ON administrative_events BEGIN SELECT RAISE(ABORT,'synthetic setup rollback'); END`); err != nil {
		t.Fatal(err)
	}
	requireStatus(t, f.request("complete", f.input()), 503)
	for _, table := range []string{"users", "administrators", "operators", "sites", "locations", "role_assignments", "administrative_events"} {
		if setupCount(t, f.db, table) != 0 {
			t.Fatal("failed setup left partial records", table)
		}
	}
	var closed bool
	if err := f.db.QueryRow(`SELECT completed_at IS NOT NULL FROM web_setup_state WHERE id=1`).Scan(&closed); err != nil || closed {
		t.Fatal("failed transaction consumed setup code", closed, err)
	}
	code, err := os.ReadFile(f.codeFile)
	if err != nil || strings.TrimSpace(string(code)) != f.code {
		t.Fatal("failed setup removed retry code", err)
	}
	if _, err := f.db.Exec(`DROP TRIGGER setup_synthetic_failure`); err != nil {
		t.Fatal(err)
	}
	requireStatus(t, f.request("complete", f.input()), 201)
	if n := setupCount(t, f.db, "role_assignments"); n != 0 {
		t.Fatal("administrator automatically received operational site roles", n)
	}
}

func TestWebSetupConcurrentCompletionCreatesOnlyOneAdministrator(t *testing.T) {
	f := webSetupFixture(t)
	start := make(chan struct{})
	responses := make(chan *httptest.ResponseRecorder, 2)
	var tasks sync.WaitGroup
	for range 2 {
		tasks.Add(1)
		go func() {
			defer tasks.Done()
			<-start
			responses <- f.request("complete", f.input())
		}()
	}
	close(start)
	tasks.Wait()
	close(responses)
	statuses := map[int]int{}
	for response := range responses {
		statuses[response.Code]++
		if response.Code != 201 && response.Code != 409 {
			t.Fatal("unexpected competing setup response", response.Code, response.Body.String())
		}
	}
	if statuses[201] != 1 || statuses[409] != 1 {
		t.Fatal("concurrent setup did not choose exactly one completion", statuses)
	}
	for _, table := range []string{"users", "administrators", "operators", "sites", "locations"} {
		if setupCount(t, f.db, table) != 1 {
			t.Fatal("concurrent completion duplicated data", table)
		}
	}
}

func TestWebSetupRejectsExistingDataAndInterveningCLIAdministrator(t *testing.T) {
	for _, existing := range []string{"demo", "inactive-user", "historical-administration", "historical-version", "product-only"} {
		t.Run(existing, func(t *testing.T) {
			db, dir := freshSetupDB(t)
			if existing == "demo" {
				if err := SeedDemo(db); err != nil {
					t.Fatal(err)
				}
			} else {
				statement := map[string]string{
					"inactive-user":             `INSERT INTO users(id,username,password_hash,active) VALUES('old-user','old-user','unusable',0)`,
					"historical-administration": `INSERT INTO administrative_events VALUES('historical-event',NULL,'local_revoke_administrator','removed-user','2026-01-01T00:00:00Z','{}')`,
					"historical-version":        `INSERT INTO administrative_versions VALUES('user','removed-user',2)`,
					"product-only":              `INSERT INTO product_types VALUES('old-product','Historical equipment type','piece')`,
				}[existing]
				if _, err := db.Exec(statement); err != nil {
					t.Fatal(err)
				}
			}
			file := filepath.Join(dir, "setup-code")
			if pending, err := PrepareWebSetup(context.Background(), db, file); err != nil || pending {
				t.Fatal("nonfresh installation offered public administration bootstrap", pending, err)
			}
			if _, err := os.Stat(file); !os.IsNotExist(err) {
				t.Fatal("nonfresh database has a bootstrap code", err)
			}
		})
	}
	f := webSetupFixture(t)
	requireStatus(t, f.request("checks", map[string]any{"setup_code": f.code, "enabled_modules": []string{"inventory"}}), 200)
	if err := CreateAdministrator(f.db, "cli-admin", testPassword); err != nil {
		t.Fatal(err)
	}
	requireStatus(t, f.request("complete", f.input()), 409)
	if setupCount(t, f.db, "users") != 1 || setupCount(t, f.db, "administrators") != 1 || setupCount(t, f.db, "operators") != 0 {
		t.Fatal("web setup ignored intervening CLI administration")
	}
	if pending, err := PrepareWebSetup(context.Background(), f.db, f.codeFile); err != nil || pending {
		t.Fatal("nonfresh pending setup was reusable after CLI bootstrap", pending, err)
	}
}

func TestWebSetupCodeFileSymlinkAndBrokenEnvironmentFailClosed(t *testing.T) {
	db, dir := freshSetupDB(t)
	protected := filepath.Join(dir, "protected")
	if err := os.WriteFile(protected, []byte("preserve-existing-file"), 0600); err != nil {
		t.Fatal(err)
	}
	codeFile := filepath.Join(dir, "symlink-code")
	if err := os.Symlink(protected, codeFile); err != nil {
		t.Fatal(err)
	}
	if pending, err := PrepareWebSetup(context.Background(), db, codeFile); err == nil || pending {
		t.Fatal("symlink accepted as bootstrap proof", pending, err)
	}
	value, err := os.ReadFile(protected)
	if err != nil || string(value) != "preserve-existing-file" {
		t.Fatal("bootstrap overwrote symlink target", err)
	}
	f := webSetupFixture(t)
	brokenDir := filepath.Join(f.dir, "not-a-directory")
	if err := os.WriteFile(brokenDir, []byte("preserve"), 0600); err != nil {
		t.Fatal(err)
	}
	f.handler = HandlerWithRuntime(f.db, setupTestOrigin, true, RuntimeOptions{Modules: allModules(), DataDir: brokenDir, SetupCodeFile: f.codeFile})
	f.refreshStatus(t)
	w := f.request("checks", map[string]any{"setup_code": f.code, "enabled_modules": []string{"inventory"}})
	requireStatus(t, w, 200)
	var checks struct {
		CanComplete bool `json:"can_complete"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &checks); err != nil || checks.CanComplete {
		t.Fatal("broken data directory passed deployment checks", w.Body.String(), err)
	}
	requireStatus(t, f.request("complete", f.input()), 409)
	if setupCount(t, f.db, "users") != 0 {
		t.Fatal("environment failure still created administrator")
	}
}

func TestWebSetupHTTPSCookieAndBoundedInput(t *testing.T) {
	f := webSetupFixture(t)
	h := HandlerWithRuntime(f.db, testOrigin, false, RuntimeOptions{Modules: allModules(), DataDir: f.dir, SetupCodeFile: f.codeFile})
	w := authRequest(h, "GET", "/api/v1/setup/status", nil, nil, "", "")
	requireStatus(t, w, 200)
	cookies := w.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatal("HTTPS setup cookie missing")
	}
	cookie := cookies[0]
	if cookie.Name != "__Host-drk-setup" || !cookie.Secure || !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode || cookie.Path != "/" || cookie.Domain != "" {
		t.Fatalf("HTTPS setup cookie weakens transport protection: %+v", cookie)
	}
	// An otherwise authorized oversized request must fail without account or
	// configuration writes. The code is still valid for a corrected submission.
	input := f.input()
	input["operator_name"] = strings.Repeat("x", 256<<10)
	requireStatus(t, f.request("complete", input), 413)
	if setupCount(t, f.db, "users") != 0 || setupCount(t, f.db, "administrators") != 0 {
		t.Fatal("oversized request partially completed setup")
	}
}

func TestWebSetupPublicCodeAttemptsAreRateLimited(t *testing.T) {
	f := webSetupFixture(t)
	for i := 0; i < 11; i++ {
		body, err := json.Marshal(map[string]any{"setup_code": randomToken(), "enabled_modules": []string{"inventory"}})
		if err != nil {
			t.Fatal(err)
		}
		r := httptest.NewRequest("POST", setupTestOrigin+"/api/v1/setup/checks", bytes.NewReader(body))
		r.RemoteAddr = "192.0.2.254:23456"
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Origin", setupTestOrigin)
		r.Header.Set("X-CSRF-Token", f.csrf)
		r.AddCookie(f.cookie)
		w := httptest.NewRecorder()
		f.handler.ServeHTTP(w, r)
		status := 403
		if i == 10 {
			status = 429
		}
		requireStatus(t, w, status)
	}
	if setupCount(t, f.db, "users") != 0 {
		t.Fatal("failed code attempts created account")
	}
	requireStatus(t, f.request("checks", map[string]any{"setup_code": f.code, "enabled_modules": []string{"inventory"}}), 200)
}

func TestWebSetupEnvironmentCertificateIdentityAndValidity(t *testing.T) {
	db, dir := freshSetupDB(t)
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	certificate := &x509.Certificate{
		SerialNumber: big.NewInt(1), DNSNames: []string{"localhost"},
		NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour),
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, certificate, certificate, &privateKey.PublicKey, privateKey)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		t.Fatal(err)
	}
	certFile, keyFile := filepath.Join(dir, "setup-cert.pem"), filepath.Join(dir, "setup-key.pem")
	if err := os.WriteFile(certFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyFile, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}), 0600); err != nil {
		t.Fatal(err)
	}
	a := newAuth(db, testOrigin)
	a.runtime = RuntimeOptions{DataDir: dir, TLSCert: certFile, TLSKey: keyFile}
	a.now = func() time.Time { return now }
	for _, scenario := range []struct {
		origin string
		now    time.Time
		valid  bool
	}{
		{testOrigin, now, true},
		{"https://other.example:8080", now, true},
		{testOrigin, now.Add(2 * time.Hour), false},
		{"https://host%zz", now, false},
	} {
		a.origin = scenario.origin
		a.now = func() time.Time { return scenario.now }
		checks, complete := a.setupEnvironmentChecks(context.Background(), modulesSelection(t, "inventory"))
		if complete != scenario.valid {
			t.Fatal("certificate identity/expiry did not control completion", scenario.origin, scenario.now, complete, checks)
		}
		if scenario.origin == "https://other.example:8080" {
			warning := false
			for _, check := range checks {
				if check.ID == "public_certificate" && check.Status == "warning" && !check.Required {
					warning = true
				}
			}
			if !warning {
				t.Fatal("HTTPS proxy certificate distinction was silently accepted", checks)
			}
		}
	}
}
