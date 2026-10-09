package app

import (
	"encoding/json"
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
	var got []Location
	if err = json.Unmarshal(check("/api/v1/locations", 200).Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Operator != "Demo-Betreiber (synthetisch)" {
		t.Fatalf("unexpected locations: %+v", got)
	}
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
	check("/api/v1/locations", 503)
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
	db.Exec(`CREATE TABLE schema_migrations(version INTEGER PRIMARY KEY);INSERT INTO schema_migrations VALUES(2)`)
	if Migrate(db) == nil {
		t.Fatal("accepted future schema")
	}
}
func TestReadOnlyAPI(t *testing.T) {
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
