package app

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

func TestWorkflow(t *testing.T) {
	path := filepath.Join(t.TempDir(), "inventory.db")
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	h := Handler(db)
	check := func(path string, want int) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRecorder()
		h.ServeHTTP(r, httptest.NewRequest("GET", path, nil))
		if r.Code != want {
			t.Fatalf("%s: %d %s", path, r.Code, r.Body.String())
		}
		return r
	}
	check("/health/live", 200)
	check("/health/ready", 503)
	for range 2 {
		if err = Migrate(db); err != nil {
			t.Fatal(err)
		}
	}
	check("/health/ready", 200)
	for range 2 {
		if err = SeedDemo(db); err != nil {
			t.Fatal(err)
		}
	}
	check("/api/v1/locations", 401)
	check("/", 200)
	check("/app.js", 200)
	for name, want := range map[string]any{"foreign_keys": 1, "journal_mode": "wal", "synchronous": 2} {
		var got any
		if err = db.QueryRow("PRAGMA " + name).Scan(&got); err != nil {
			t.Fatal(err)
		}
		switch v := got.(type) {
		case int64:
			got = int(v)
		}
		if got != want {
			t.Fatalf("%s=%v want %v", name, got, want)
		}
	}
	if _, err = db.Exec(`INSERT INTO locations VALUES('bad','missing','Invalid')`); err == nil {
		t.Fatal("foreign key accepted")
	}
	db.Close()
	check("/health/ready", 503)
	check("/api/v1/locations", 401)
	db2, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db2.Close()
	var count int
	if err = db2.QueryRow("SELECT COUNT(*) FROM locations").Scan(&count); err != nil || count != 2 {
		t.Fatalf("persistence: %d %v", count, err)
	}
}
func TestUnsupportedSchema(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "bad.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.Exec(`CREATE TABLE schema_migrations(version INTEGER PRIMARY KEY);INSERT INTO schema_migrations VALUES(30)`)
	if Migrate(db) == nil {
		t.Fatal("accepted future schema")
	}
}
func TestLocationsRemainReadOnly(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = Migrate(db); err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRecorder()
	Handler(db).ServeHTTP(r, httptest.NewRequest(http.MethodPost, "/api/v1/locations", nil))
	if r.Code != 405 {
		t.Fatalf("status %d", r.Code)
	}
}

func TestMigrationFromVersionOne(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "v1.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	script, err := assets.ReadFile("migrations/001_initial.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(string(script) + `;CREATE TABLE schema_migrations(version INTEGER PRIMARY KEY);INSERT INTO schema_migrations VALUES(1)`); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO operators VALUES('operator','Demo');INSERT INTO sites VALUES('site','operator','Demo');INSERT INTO locations VALUES('one','site','One');INSERT INTO locations VALUES('two','site','Two')`); err != nil {
		t.Fatal(err)
	}
	snapshot := filepath.Join(t.TempDir(), "backup.db")
	if _, err = db.Exec(`VACUUM INTO ?`, snapshot); err != nil {
		t.Fatal(err)
	}
	if err = Migrate(db); err != nil {
		t.Fatal(err)
	}
	if err = CreateViewer(db, "viewer", testPassword, "site"); err != nil {
		t.Fatal(err)
	}
	var count int
	if err = db.QueryRow(`SELECT COUNT(*) FROM locations`).Scan(&count); err != nil || count != 2 {
		t.Fatal("migration changed locations")
	}
	restored, err := Open(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	var version int
	if err = restored.QueryRow(`SELECT MAX(version) FROM schema_migrations`).Scan(&version); err != nil || version != 1 {
		t.Fatal("backup not pre-migration")
	}
	var integrity string
	if err = restored.QueryRow(`PRAGMA integrity_check`).Scan(&integrity); err != nil || integrity != "ok" {
		t.Fatal("backup integrity")
	}
}
