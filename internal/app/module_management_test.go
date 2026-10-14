package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Keep this fixture independent of the setup wizard: these tests exercise the
// actual session, CSRF, module guard and protected administration routes.
func managedModulesFixture(t *testing.T, selection string) stockFixture {
	t.Helper()
	f := stockDB(t)
	if _, _, err := InitializeModules(context.Background(), f.db, modulesSelection(t, selection)); err != nil {
		t.Fatal(err)
	}
	a := newAuth(f.db, testOrigin)
	a.dynamicModules = true
	mux := http.NewServeMux()
	a.routes(mux)
	moduleRoutes(mux, a)
	stockRoutes(mux, a)
	packlistRoutes(mux, a)
	administrationRoutes(mux, a)
	f.handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, pattern := mux.Handler(r)
		if a.moduleGuard(w, r, pattern) {
			mux.ServeHTTP(w, r)
		}
	})
	f.cookie, f.csrf = loginTest(t, f.handler, nil)
	return f
}

func moduleManagementChange(revision int, modules ...string) moduleManagementInput {
	return moduleManagementInput{ExpectedRevision: &revision, EnabledModules: modules, Note: "Synthetische Verwaltung der Funktionsgruppen"}
}

func moduleManagementRevision(t *testing.T, f stockFixture, names string, expected int) {
	t.Helper()
	c, revision, err := ActiveModules(context.Background(), f.db)
	if err != nil || strings.Join(c.names(), ",") != names || revision != expected {
		t.Fatalf("live configuration %v/%d, want %s/%d: %v", c.names(), revision, names, expected, err)
	}
}

func TestModuleManagementRestartKeepsAdminSelectionAndExplicitEnvironmentChanges(t *testing.T) {
	f := managedModulesFixture(t, "inventory,devices")
	if err := SetAdministrator(f.db, "viewer", true); err != nil {
		t.Fatal(err)
	}
	f.cookie, f.csrf = loginTest(t, f.handler, nil)
	requireStatus(t, packHTTP(f, "POST", "admin/modules", "managed-startup-change-001", moduleManagementChange(1, "inventory")), 200)
	live, revision, err := InitializeModules(context.Background(), f.db, modulesSelection(t, "devices,inventory"))
	if err != nil || revision != 2 || strings.Join(live.names(), ",") != "inventory" {
		t.Fatal("unchanged startup input reverted web administration", live, revision, err)
	}
	live, revision, err = InitializeModules(context.Background(), f.db, allModules())
	if err != nil || revision != 3 || live != allModules() {
		t.Fatal("changed environment did not override explicitly", live, revision, err)
	}
	if _, again, err := InitializeModules(context.Background(), f.db, allModules()); err != nil || again != revision {
		t.Fatal("repeat startup created redundant revision", again, err)
	}
	var startup string
	if err := f.db.QueryRow(`SELECT modules FROM module_startup_selection WHERE id=1`).Scan(&startup); err != nil || startup != strings.Join(allModules().names(), ",") {
		t.Fatal("startup input not canonical or saved", startup, err)
	}
	state := packHTTP(f, "GET", "admin/modules", "", nil)
	requireStatus(t, state, 200)
	var data moduleManagementState
	if err := json.Unmarshal(state.Body.Bytes(), &data); err != nil || data.Revision != 3 || len(data.History) != 3 || data.History[0].Source != "environment" || data.History[1].Source != "admin" || data.History[1].ActorID == nil || *data.History[1].ActorID != f.user {
		t.Fatal("change attribution missing", state.Body.String(), err)
	}
	for _, statement := range []string{
		`UPDATE module_management_events SET source='setup' WHERE configuration_version=2`,
		`DELETE FROM module_management_events WHERE configuration_version=2`,
		`UPDATE module_management_requests SET response_json='{}'`,
		`DELETE FROM module_management_requests`,
	} {
		if _, err := f.db.Exec(statement); err == nil {
			t.Fatal("module audit/request can be rewritten", statement)
		}
	}
}

func TestModuleManagementLiveChangesReplayNoOpAndAuthorization(t *testing.T) {
	f := managedModulesFixture(t, "inventory")
	requireStatus(t, packHTTP(f, "GET", "admin/modules", "", nil), 403)
	requireStatus(t, packHTTP(f, "POST", "admin/modules", "managed-denied-change-001", moduleManagementChange(1, "inventory", "packs")), 403)
	if err := SetAdministrator(f.db, "viewer", true); err != nil {
		t.Fatal(err)
	}
	f.cookie, f.csrf = loginTest(t, f.handler, nil)
	state := packHTTP(f, "GET", "modules", "", nil)
	requireStatus(t, state, 200)
	if !strings.Contains(state.Body.String(), `"editable":true`) {
		t.Fatal("administrator cannot edit live modules", state.Body.String())
	}
	on := moduleManagementChange(1, "inventory", "packs")
	w := packHTTP(f, "POST", "admin/modules", "managed-live-enable-0001", on)
	requireStatus(t, w, 200)
	var result moduleManagementResult
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil || result.Revision != 2 || !result.Changed {
		t.Fatal("module activation not applied", w.Body.String(), err)
	}
	requireStatus(t, packHTTP(f, "POST", "packlists", "managed-new-pack-0000001", packInput(warehouse)), 201)
	off := moduleManagementChange(2, "inventory")
	requireStatus(t, packHTTP(f, "POST", "admin/modules", "managed-live-disable-001", off), 200)
	modulesError(t, packHTTP(f, "POST", "packlists", "managed-pack-disabled01", packInput(warehouse)), 403, "module_disabled")
	replay := packHTTP(f, "POST", "admin/modules", "managed-live-enable-0001", on)
	requireStatus(t, replay, 200)
	if replay.Body.String() != w.Body.String() {
		t.Fatal("lost-reply recovery returned different historical result")
	}
	moduleManagementRevision(t, f, "inventory", 3)
	requireStatus(t, packHTTP(f, "POST", "admin/modules", "managed-stale-change-001", on), 409)
	on.Note = "Andere Begründung"
	requireStatus(t, packHTTP(f, "POST", "admin/modules", "managed-live-enable-0001", on), 409)
	noOp := packHTTP(f, "POST", "admin/modules", "managed-no-op-change-001", moduleManagementChange(3, "inventory"))
	requireStatus(t, noOp, 200)
	if err := json.Unmarshal(noOp.Body.Bytes(), &result); err != nil || result.Revision != 3 || result.Changed {
		t.Fatal("no-op invalidated current evidence", noOp.Body.String(), err)
	}
	var requestAudit int
	if err := f.db.QueryRow(`SELECT COUNT(*) FROM administrative_events WHERE action='configure_modules'`).Scan(&requestAudit); err != nil || requestAudit != 3 {
		t.Fatal("successful no-op was not audited exactly once", requestAudit, err)
	}
	// Both directions share the global user/key namespace.
	requireStatus(t, adminHTTP(f.handler, f.cookie, f.csrf, "managed-new-operator-01", adminChange{Action: "create_operator", Name: "Synthetic module administration", Note: "Synthetischer Schlüsseltest"}), 200)
	requireStatus(t, packHTTP(f, "POST", "admin/modules", "managed-new-operator-01-test", moduleManagementChange(3, "inventory")), 409)
	tx, err := f.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if err := rejectOtherKey(context.Background(), tx, f.user, "managed-live-enable-0001", "administrative_requests"); err == nil {
		t.Fatal("module administration key can be claimed by a different operation")
	}
	tx.Rollback()
	// Revoking administrative status also prevents replaying previous decisions.
	if err := SetAdministrator(f.db, "viewer", false); err != nil {
		t.Fatal(err)
	}
	f.cookie, f.csrf = loginTest(t, f.handler, nil)
	requireStatus(t, packHTTP(f, "POST", "admin/modules", "managed-live-disable-001", off), 403)
	requireStatus(t, packHTTP(f, "GET", "admin/modules", "", nil), 403)
	moduleManagementRevision(t, f, "inventory", 3)
}

func TestModuleManagementRejectsIncompleteUnsafeSelectionAndRollsBack(t *testing.T) {
	f := managedModulesFixture(t, "inventory")
	if err := SetAdministrator(f.db, "viewer", true); err != nil {
		t.Fatal(err)
	}
	f.cookie, f.csrf = loginTest(t, f.handler, nil)
	for _, names := range [][]string{nil, {}, {"devices"}, {"all"}, {"inventory", "devices", "documents", "compliance", "release"}, {"inventory", "documents"}, {"inventory", "inventory"}, {"inventory", " devices"}, {"inventory", "unknown"}} {
		requireStatus(t, packHTTP(f, "POST", "admin/modules", "managed-invalid-change-01", moduleManagementChange(1, names...)), 400)
	}
	incomplete := moduleManagementChange(1, "inventory")
	incomplete.ExpectedRevision = nil
	requireStatus(t, packHTTP(f, "POST", "admin/modules", "managed-missing-version01", incomplete), 400)
	incomplete.ExpectedRevision = new(int)
	*incomplete.ExpectedRevision = 1
	incomplete.Note = " "
	requireStatus(t, packHTTP(f, "POST", "admin/modules", "managed-missing-reason-01", incomplete), 400)
	requireStatus(t, packHTTP(f, "POST", "admin/modules", "managed-unknown-field001", map[string]any{"expected_revision": 1, "enabled_modules": []string{"inventory"}, "note": "Synthetic", "unknown": true}), 400)
	badCSRF := f
	badCSRF.csrf = "wrong"
	requireStatus(t, packHTTP(badCSRF, "POST", "admin/modules", "managed-csrf-change-0001", moduleManagementChange(1, "inventory", "packs")), 403)
	if _, err := f.db.Exec(`CREATE TRIGGER synthetic_module_request_failure BEFORE INSERT ON module_management_requests BEGIN SELECT RAISE(ABORT,'synthetic rollback'); END`); err != nil {
		t.Fatal(err)
	}
	requireStatus(t, packHTTP(f, "POST", "admin/modules", "managed-rollback-change1", moduleManagementChange(1, "inventory", "packs")), 503)
	moduleManagementRevision(t, f, "inventory", 1)
	var metadata, audits, requests int
	if err := f.db.QueryRow(`SELECT (SELECT COUNT(*) FROM module_management_events),(SELECT COUNT(*) FROM administrative_events WHERE action='configure_modules'),(SELECT COUNT(*) FROM module_management_requests)`).Scan(&metadata, &audits, &requests); err != nil || metadata != 1 || audits != 0 || requests != 0 {
		t.Fatal("failed request left partial changes", metadata, audits, requests, err)
	}
	if _, err := f.db.Exec(`DROP TRIGGER synthetic_module_request_failure`); err != nil {
		t.Fatal(err)
	}
	// Static test/embedded handlers retain their original matching semantics.
	static := f
	static.handler = HandlerWithModules(f.db, testOrigin, false, modulesSelection(t, "inventory"))
	static.cookie, static.csrf = loginTest(t, static.handler, nil)
	state := packHTTP(static, "GET", "admin/modules", "", nil)
	requireStatus(t, state, 200)
	if !strings.Contains(state.Body.String(), `"editable":false`) {
		t.Fatal("static handler advertises dynamic administration")
	}
	modulesError(t, packHTTP(static, "POST", "admin/modules", "managed-static-change-01", moduleManagementChange(1, "inventory", "packs")), 403, "module_administration_unavailable")
}

func TestModuleManagementOffOnInvalidatesInFlightMutation(t *testing.T) {
	f := managedModulesFixture(t, "inventory,packs")
	if err := SetAdministrator(f.db, "viewer", true); err != nil {
		t.Fatal(err)
	}
	f.cookie, f.csrf = loginTest(t, f.handler, nil)
	reader := &moduleChangeBody{Reader: strings.NewReader(`{"batch_id":"` + demoBatch + `","to_location_id":"` + warehouse + `","quantity":1,"note":"Synthetic guarded receipt"}`)}
	reader.change = func() {
		requireStatus(t, packHTTP(f, "POST", "admin/modules", "managed-inflight-off-001", moduleManagementChange(1, "inventory")), 200)
		requireStatus(t, packHTTP(f, "POST", "admin/modules", "managed-inflight-on-0001", moduleManagementChange(2, "inventory", "packs")), 200)
	}
	r := httptest.NewRequest("POST", testOrigin+"/api/v1/stock/receipts", reader)
	// A valid core mutation still must not execute under the obsolete revision.
	r.AddCookie(f.cookie)
	r.Header.Set("Origin", testOrigin)
	r.Header.Set("X-CSRF-Token", f.csrf)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Idempotency-Key", "managed-inflight-receipt")
	w := httptest.NewRecorder()
	f.handler.ServeHTTP(w, r)
	modulesError(t, w, 409, "module_configuration_changed")
	moduleManagementRevision(t, f, "inventory,packs", 3)
	var count int
	if err := f.db.QueryRow(`SELECT COUNT(*) FROM stock_movements`).Scan(&count); err != nil || count != 0 {
		t.Fatal("in-flight write survived a disable/enable transition", count, err)
	}
}
