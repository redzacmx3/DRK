package app

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"
)

func correctionHTTP(f stockFixture, path, key string, input any) *httptest.ResponseRecorder {
	var b bytes.Buffer
	json.NewEncoder(&b).Encode(input)
	r := httptest.NewRequest("POST", testOrigin+path, &b)
	r.AddCookie(f.cookie)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Origin", testOrigin)
	r.Header.Set("X-CSRF-Token", f.csrf)
	r.Header.Set("Idempotency-Key", key)
	w := httptest.NewRecorder()
	f.handler.ServeHTTP(w, r)
	return w
}
func inventoryHTTP(f stockFixture, key string, input inventoryRequest) *httptest.ResponseRecorder {
	return correctionHTTP(f, "/api/v1/stock/inventories", key, input)
}
func reversalHTTP(f stockFixture, key, id string, input reversalRequest) *httptest.ResponseRecorder {
	return correctionHTTP(f, "/api/v1/stock/movements/"+id+"/reversal", key, input)
}
func intPointer(n int64) *int64 { return &n }
func resultID(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	requireStatus(t, w, 201)
	var r struct {
		ID string `json:"movement_id"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &r); err != nil || r.ID == "" {
		t.Fatal("missing movement ID")
	}
	return r.ID
}
func TestReverseMaterialFlow(t *testing.T) {
	f := stockDB(t)
	receipt := resultID(t, stockHTTP(f, "receipts", "reverse-receipt01", stockRequest{BatchID: demoBatch, To: warehouse, Quantity: 20}))
	transfer := resultID(t, stockHTTP(f, "transfers", "reverse-transfer1", stockRequest{BatchID: demoBatch, From: warehouse, To: kit, Quantity: 5, ExpectedVersion: 1}))
	consumption := resultID(t, stockHTTP(f, "consumptions", "reverse-consume01", stockRequest{BatchID: demoBatch, From: kit, Quantity: 2, ExpectedVersion: 1}))
	takeBack := reversalRequest{Versions: []versionExpectation{{kit, "available", 2}}, Note: "Synthetische Fehlentnahme korrigieren"}
	original := reversalHTTP(f, "reverse-takeback1", consumption, takeBack)
	id := resultID(t, original)
	replay := reversalHTTP(f, "reverse-takeback1", consumption, takeBack)
	requireStatus(t, replay, 201)
	if original.Body.String() != replay.Body.String() {
		t.Fatal("reversal replay changed")
	}
	requireStatus(t, reversalHTTP(f, "reverse-twice-001", consumption, takeBack), 409)
	requireStatus(t, reversalHTTP(f, "reverse-reverse1", id, reversalRequest{Versions: []versionExpectation{{kit, "available", 3}}, Note: "Invalid second reversal"}), 409)
	if q, v := quantity(t, f.db, kit, "available"); q != 5 || v != 3 {
		t.Fatalf("restored consumption %d/%d", q, v)
	}
	resultID(t, reversalHTTP(f, "reverse-transfer2", transfer, reversalRequest{Versions: []versionExpectation{{kit, "available", 3}, {warehouse, "available", 2}}, Note: "Falsche Umlagerung"}))
	if q, v := quantity(t, f.db, warehouse, "available"); q != 20 || v != 3 {
		t.Fatal("warehouse not restored")
	}
	if q, v := quantity(t, f.db, kit, "available"); q != 0 || v != 4 {
		t.Fatal("kit not restored")
	}
	resultID(t, reversalHTTP(f, "reverse-receipt02", receipt, reversalRequest{Versions: []versionExpectation{{warehouse, "available", 3}}, Note: "Doppelter Eingang"}))
	if q, v := quantity(t, f.db, warehouse, "available"); q != 0 || v != 4 {
		t.Fatal("receipt not reversed")
	}
	assertJournal(t, f.db, 6)
	var net int64
	if err := f.db.QueryRow(`SELECT SUM(delta) FROM movement_lines`).Scan(&net); err != nil || net != 0 {
		t.Fatal("ledger not balanced after reversals")
	}
	var oldQuantity int64
	if err := f.db.QueryRow(`SELECT quantity FROM stock_movements WHERE id=?`, receipt).Scan(&oldQuantity); err != nil || oldQuantity != 20 {
		t.Fatal("original changed")
	}
}
func TestInventoryCountsIncludingZero(t *testing.T) {
	f := stockDB(t)
	resultID(t, stockHTTP(f, "receipts", "count-receipt001", stockRequest{BatchID: demoBatch, To: warehouse, Quantity: 10}))
	input := inventoryRequest{BatchID: demoBatch, Location: warehouse, ExpectedVersion: 1, Counted: intPointer(7), Note: "Physisch sieben gezählt"}
	first := inventoryHTTP(f, "count-seven-0001", input)
	id := resultID(t, first)
	var response correctionResult
	json.Unmarshal(first.Body.Bytes(), &response)
	if response.Basis == nil || *response.Basis != 10 || response.Counted == nil || *response.Counted != 7 || response.Delta == nil || *response.Delta != -3 {
		t.Fatal("count basis/delta missing")
	}
	requireStatus(t, inventoryHTTP(f, "stale-count-0001", input), 409)
	same := input
	same.ExpectedVersion = 2
	same.Note = "Erneut sieben gezählt"
	unchangedID := resultID(t, inventoryHTTP(f, "same-count-00001", same))
	if q, v := quantity(t, f.db, warehouse, "available"); q != 7 || v != 3 {
		t.Fatal("unchanged count not versioned")
	}
	var lines int
	f.db.QueryRow(`SELECT COUNT(*) FROM movement_lines WHERE movement_id=?`, unchangedID).Scan(&lines)
	if lines != 0 {
		t.Fatal("zero count fabricated a quantity movement")
	}
	assertJournal(t, f.db, 3)
	replay := inventoryHTTP(f, "count-seven-0001", input)
	requireStatus(t, replay, 201)
	if replay.Body.String() != first.Body.String() {
		t.Fatal("count replay changed")
	}
	zero := input
	zero.ExpectedVersion = 3
	zero.Counted = intPointer(0)
	zero.Note = "Leer gezählt"
	zeroID := resultID(t, inventoryHTTP(f, "empty-count-0001", zero))
	if q, v := quantity(t, f.db, warehouse, "available"); q != 0 || v != 4 {
		t.Fatal("zero count failed")
	}
	requireStatus(t, reversalHTTP(f, "reverse-no-delta1", unchangedID, reversalRequest{Versions: []versionExpectation{{warehouse, "available", 4}}, Note: "Keine Differenz"}), 409)
	resultID(t, reversalHTTP(f, "reverse-count001", zeroID, reversalRequest{Versions: []versionExpectation{{warehouse, "available", 4}}, Note: "Zählfehler korrigieren"}))
	if q, v := quantity(t, f.db, warehouse, "available"); q != 7 || v != 5 {
		t.Fatal("inventory reversal failed")
	}
	var originalCount int64
	if err := f.db.QueryRow(`SELECT counted_quantity FROM stock_movements WHERE id=?`, id).Scan(&originalCount); err != nil || originalCount != 7 {
		t.Fatal("original count changed")
	}
	resultID(t, inventoryHTTP(f, "new-zero-count01", inventoryRequest{BatchID: demoBatch, Location: kit, ExpectedVersion: 0, Counted: intPointer(0), Note: "Neuer Platz leer gezählt"}))
	if q, v := quantity(t, f.db, kit, "available"); q != 0 || v != 1 {
		t.Fatal("new zero balance missing")
	}
}
func TestCorrectionConflictsPermissionsAndRollback(t *testing.T) {
	f := stockDB(t)
	receipt := resultID(t, stockHTTP(f, "receipts", "guard-receipt001", stockRequest{BatchID: demoBatch, To: warehouse, Quantity: 10}))
	transfer := resultID(t, stockHTTP(f, "transfers", "guard-transfer01", stockRequest{BatchID: demoBatch, From: warehouse, To: kit, Quantity: 5, ExpectedVersion: 1}))
	// A late version mismatch after touching the first inverse line rolls it all back.
	requireStatus(t, reversalHTTP(f, "guard-reverse001", transfer, reversalRequest{Versions: []versionExpectation{{warehouse, "available", 2}, {kit, "available", 2}}, Note: "Stale source"}), 409)
	if q, v := quantity(t, f.db, kit, "available"); q != 5 || v != 1 {
		t.Fatal("partial reversal")
	}
	if q, v := quantity(t, f.db, warehouse, "available"); q != 5 || v != 2 {
		t.Fatal("first inverse line was not rolled back")
	}
	requireStatus(t, reversalHTTP(f, "too-late-receipt1", receipt, reversalRequest{Versions: []versionExpectation{{warehouse, "available", 2}}, Note: "Already partly moved"}), 409)
	count := inventoryRequest{BatchID: demoBatch, Location: kit, ExpectedVersion: 1, Counted: intPointer(2), Note: "Testzählung"}
	f.db.Exec(`CREATE TRIGGER fail_correction_outbox BEFORE INSERT ON outbox BEGIN SELECT RAISE(ABORT,'simulated failure'); END`)
	requireStatus(t, inventoryHTTP(f, "failed-count-0001", count), 503)
	if q, v := quantity(t, f.db, kit, "available"); q != 5 || v != 1 {
		t.Fatal("failed count not rolled back")
	}
	assertJournal(t, f.db, 2)
	f.db.Exec(`DROP TRIGGER fail_correction_outbox`)
	resultID(t, inventoryHTTP(f, "failed-count-0001", count))
	missing := count
	missing.Counted = nil
	requireStatus(t, inventoryHTTP(f, "missing-count001", missing), 400)
	missing = count
	missing.Note = " "
	requireStatus(t, inventoryHTTP(f, "missing-reason01", missing), 400)
	missing = count
	missing.Counted = intPointer(-1)
	requireStatus(t, inventoryHTTP(f, "negative-count01", missing), 400)
	requireStatus(t, inventoryHTTP(f, "guard-receipt001", count), 409) // shared key namespace
	if err := SetSiteRole(f.db, "viewer", []string{demoSite}, "helper"); err != nil {
		t.Fatal(err)
	}
	f.cookie, f.csrf = loginTest(t, f.handler, nil)
	requireStatus(t, inventoryHTTP(f, "helper-count-001", count), 404)
	requireStatus(t, reversalHTTP(f, "helper-reverse01", transfer, reversalRequest{Versions: []versionExpectation{{warehouse, "available", 2}, {kit, "available", 2}}, Note: "Not permitted"}), 404)
}
func TestInventoriesDoNotReleaseBlockedStock(t *testing.T) {
	f := stockDB(t)
	resultID(t, stockHTTP(f, "receipts", "blocked-receipt1", stockRequest{BatchID: demoBatch, To: warehouse, Quantity: 10}))
	f.db.Exec(`UPDATE batches SET blocked=1`)
	input := inventoryRequest{BatchID: demoBatch, Location: warehouse, ExpectedVersion: 1, Counted: intPointer(12), Note: "Gesperrte Mehrmenge"}
	requireStatus(t, inventoryHTTP(f, "blocked-count001", input), 409)
	input.Counted = intPointer(8)
	resultID(t, inventoryHTTP(f, "blocked-count002", input))
	input.Location = kit
	input.State = "quarantined"
	input.ExpectedVersion = 0
	input.Counted = intPointer(4)
	resultID(t, inventoryHTTP(f, "quarantine-count", input))
	w := authRequest(f.handler, "GET", "/api/v1/stock/balances", nil, f.cookie, "", "")
	requireStatus(t, w, 200)
	var rows []struct {
		Usable int64 `json:"usable_quantity"`
	}
	json.Unmarshal(w.Body.Bytes(), &rows)
	for _, row := range rows {
		if row.Usable != 0 {
			t.Fatal("inventory released blocked stock")
		}
	}
}
func TestConcurrentReversalAndInventory(t *testing.T) {
	f := stockDB(t)
	other := secondDB(t, f.db)
	original := resultID(t, stockHTTP(f, "receipts", "parallel-orig001", stockRequest{BatchID: demoBatch, To: warehouse, Quantity: 10}))
	ready := make(chan struct{})
	results := make(chan error, 2)
	for i, db := range []*sql.DB{f.db, other} {
		go func() {
			<-ready
			_, err := bookReversal(context.Background(), db, f.user, digest(f.cookie.Value), []string{"parallel-rev-001", "parallel-rev-002"}[i], original, reversalRequest{Versions: []versionExpectation{{warehouse, "available", 1}}, Note: "Parallel correction"}, time.Now())
			results <- err
		}()
	}
	close(ready)
	success := 0
	for range 2 {
		err := <-results
		if err == nil {
			success++
		} else if e, ok := err.(*stockError); !ok || e.Code != "already_reversed" {
			t.Fatal(err)
		}
	}
	if success != 1 {
		t.Fatal("original reversed twice")
	}
	if q, _ := quantity(t, f.db, warehouse, "available"); q != 0 {
		t.Fatal("incorrect inverse quantity")
	}
	assertJournal(t, f.db, 2)
	ready = make(chan struct{})
	results = make(chan error, 2)
	for i, db := range []*sql.DB{f.db, other} {
		go func() {
			<-ready
			_, err := bookInventory(context.Background(), db, f.user, digest(f.cookie.Value), []string{"parallel-inv-001", "parallel-inv-002"}[i], inventoryRequest{BatchID: demoBatch, Location: warehouse, ExpectedVersion: 2, Counted: intPointer(int64(i + 4)), Note: "Parallel count"}, time.Now())
			results <- err
		}()
	}
	close(ready)
	success = 0
	for range 2 {
		err := <-results
		if err == nil {
			success++
		} else if e, ok := err.(*stockError); !ok || e.Code != "stock_conflict" {
			t.Fatal(err)
		}
	}
	if success != 1 {
		t.Fatal("both stale inventories committed")
	}
	if q, v := quantity(t, f.db, warehouse, "available"); (q != 4 && q != 5) || v != 3 {
		t.Fatal("bad inventory result")
	}
	assertJournal(t, f.db, 3)
}
func TestSchemaThreeUpgradePreservesJournalAndReplay(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "v3.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, name := range []string{"001_initial.sql", "002_auth.sql", "003_stock.sql"} {
		script, err := assets.ReadFile("migrations/" + name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = db.Exec(string(script)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = db.Exec(`CREATE TABLE schema_migrations(version INTEGER PRIMARY KEY);INSERT INTO schema_migrations VALUES(1),(2),(3)`); err != nil {
		t.Fatal(err)
	}
	if err = SeedDemo(db); err != nil {
		t.Fatal(err)
	}
	if err = CreateUser(db, "upgrade", testPassword, demoSite, "material_manager"); err != nil {
		t.Fatal(err)
	}
	var user string
	db.QueryRow(`SELECT id FROM users WHERE username='upgrade'`).Scan(&user)
	token := randomToken()
	now := time.Now()
	if _, err = db.Exec(`INSERT INTO sessions VALUES(?,?,?,?,?)`, digest(token), user, digest("csrf"), now.Unix(), now.Unix()); err != nil {
		t.Fatal(err)
	}
	input := stockRequest{BatchID: demoBatch, To: warehouse, Quantity: 10}
	before, err := bookStock(context.Background(), db, user, digest(token), "upgrade-key-0001", "receipt", input, now)
	if err != nil {
		t.Fatal(err)
	}
	if err = Migrate(db); err != nil {
		t.Fatal(err)
	}
	if err = Migrate(db); err != nil {
		t.Fatal("repeat migration", err)
	}
	after, err := bookStock(context.Background(), db, user, digest(token), "upgrade-key-0001", "receipt", input, now)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("upgrade changed replay", err)
	}
	assertJournal(t, db, 1)
	if q, v := quantity(t, db, warehouse, "available"); q != 10 || v != 1 {
		t.Fatal("upgrade changed quantities")
	}
	var enforcement int
	if err = db.QueryRow(`PRAGMA foreign_keys`).Scan(&enforcement); err != nil || enforcement != 1 {
		t.Fatal("foreign keys not restored")
	}
	if _, err = db.Exec(`DELETE FROM stock_movements`); err == nil {
		t.Fatal("append-only trigger lost")
	}
}

func TestScopedJournalAndReversal(t *testing.T) {
	f := stockDB(t)
	if _, err := f.db.Exec(`INSERT INTO sites VALUES('foreign-site','00000000-0000-4000-8000-000000000001','Foreign');INSERT INTO locations VALUES('foreign-location','foreign-site','Foreign')`); err != nil {
		t.Fatal(err)
	}
	if err := SetSiteRole(f.db, "viewer", []string{demoSite, "foreign-site"}, "material_manager"); err != nil {
		t.Fatal(err)
	}
	f.cookie, f.csrf = loginTest(t, f.handler, nil)
	receipt := resultID(t, stockHTTP(f, "receipts", "journal-receipt1", stockRequest{BatchID: demoBatch, To: warehouse, Quantity: 10}))
	transfer := resultID(t, stockHTTP(f, "transfers", "journal-transfer1", stockRequest{BatchID: demoBatch, From: warehouse, To: "foreign-location", Quantity: 4, ExpectedVersion: 1}))
	w := authRequest(f.handler, "GET", "/api/v1/stock/movements?limit=1", nil, f.cookie, "", "")
	requireStatus(t, w, 200)
	if w.Header().Get("X-Has-More") != "true" {
		t.Fatal("pagination missing")
	}
	requireStatus(t, authRequest(f.handler, "GET", "/api/v1/stock/movements?limit=101", nil, f.cookie, "", ""), 400)
	if err := SetSiteRole(f.db, "viewer", []string{demoSite}, "material_manager"); err != nil {
		t.Fatal(err)
	}
	f.cookie, f.csrf = loginTest(t, f.handler, nil)
	w = authRequest(f.handler, "GET", "/api/v1/stock/movements", nil, f.cookie, "", "")
	requireStatus(t, w, 200)
	var entries []struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &entries); err != nil || len(entries) != 1 || entries[0].ID != receipt {
		t.Fatal("journal leaked a foreign destination")
	}
	requireStatus(t, reversalHTTP(f, "journal-reverse1", transfer, reversalRequest{Versions: []versionExpectation{{warehouse, "available", 2}, {"foreign-location", "available", 1}}, Note: "Outside current scope"}), 404)
	if q, _ := quantity(t, f.db, warehouse, "available"); q != 6 {
		t.Fatal("unauthorized inverse affected stock")
	}
}
