package app

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

const demoBatch = "00000000-0000-4000-8000-000000000011"
const warehouse = "00000000-0000-4000-8000-000000000003"
const kit = "00000000-0000-4000-8000-000000000004"

type stockFixture struct {
	db         *sql.DB
	handler    http.Handler
	cookie     *http.Cookie
	csrf, user string
}

func stockDB(t *testing.T) stockFixture {
	t.Helper()
	db := authDB(t)
	if err := SetSiteRole(db, "viewer", []string{demoSite}, "material_manager"); err != nil {
		t.Fatal(err)
	}
	h := Handler(db)
	cookie, csrf := loginTest(t, h, nil)
	var user string
	if err := db.QueryRow(`SELECT id FROM users WHERE username='viewer'`).Scan(&user); err != nil {
		t.Fatal(err)
	}
	return stockFixture{db, h, cookie, csrf, user}
}
func stockHTTP(f stockFixture, kind, key string, input stockRequest) *httptest.ResponseRecorder {
	var b bytes.Buffer
	json.NewEncoder(&b).Encode(input)
	r := httptest.NewRequest("POST", testOrigin+"/api/v1/stock/"+kind, &b)
	r.AddCookie(f.cookie)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Origin", testOrigin)
	r.Header.Set("X-CSRF-Token", f.csrf)
	r.Header.Set("Idempotency-Key", key)
	w := httptest.NewRecorder()
	f.handler.ServeHTTP(w, r)
	return w
}
func quantity(t *testing.T, db *sql.DB, loc, state string) (int64, int64) {
	t.Helper()
	var q, v int64
	if err := db.QueryRow(`SELECT quantity,version FROM stock_balances WHERE batch_id=? AND location_id=? AND state=?`, demoBatch, loc, state).Scan(&q, &v); err != nil {
		t.Fatal(err)
	}
	return q, v
}
func assertJournal(t *testing.T, db *sql.DB, want int) {
	t.Helper()
	for _, table := range []string{"stock_movements", "audit_events", "outbox", "idempotency_keys"} {
		var count int
		if err := db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&count); err != nil || count != want {
			t.Fatalf("%s: %d want %d (%v)", table, count, want, err)
		}
	}
}
func TestStockFlowAndReplay(t *testing.T) {
	f := stockDB(t)
	receipt := stockRequest{BatchID: demoBatch, To: warehouse, Quantity: 100}
	original := stockHTTP(f, "receipts", "receipt-key-000001", receipt)
	requireStatus(t, original, 201)
	replay := stockHTTP(f, "receipts", "receipt-key-000001", receipt)
	requireStatus(t, replay, 201)
	if original.Body.String() != replay.Body.String() {
		t.Fatal("replay response changed")
	}
	receipt.Quantity = 101
	requireStatus(t, stockHTTP(f, "receipts", "receipt-key-000001", receipt), 409)
	transfer := stockRequest{BatchID: demoBatch, From: warehouse, To: kit, Quantity: 30, ExpectedVersion: 1}
	original = stockHTTP(f, "transfers", "transfer-key-00001", transfer)
	requireStatus(t, original, 201)
	if q, v := quantity(t, f.db, warehouse, "available"); q != 70 || v != 2 {
		t.Fatalf("source %d/%d", q, v)
	}
	if q, v := quantity(t, f.db, kit, "available"); q != 30 || v != 1 {
		t.Fatalf("target %d/%d", q, v)
	}
	replay = stockHTTP(f, "transfers", "transfer-key-00001", transfer)
	requireStatus(t, replay, 201)
	if original.Body.String() != replay.Body.String() {
		t.Fatal("transfer replay changed")
	}
	requireStatus(t, stockHTTP(f, "transfers", "stale-key-00000001", transfer), 409)
	take := stockRequest{BatchID: demoBatch, From: kit, Quantity: 10, ExpectedVersion: 1}
	requireStatus(t, stockHTTP(f, "consumptions", "consume-key-00001", take), 201)
	if q, v := quantity(t, f.db, kit, "available"); q != 20 || v != 2 {
		t.Fatalf("consumption %d/%d", q, v)
	}
	assertJournal(t, f.db, 3)
	var sum int64
	if err := f.db.QueryRow(`SELECT SUM(delta) FROM movement_lines`).Scan(&sum); err != nil || sum != 90 {
		t.Fatalf("ledger total %d %v", sum, err)
	}
	var net int64
	if err := f.db.QueryRow(`SELECT SUM(delta) FROM movement_lines WHERE movement_id=(SELECT id FROM stock_movements WHERE kind='transfer')`).Scan(&net); err != nil || net != 0 {
		t.Fatal("unbalanced transfer")
	}
	for _, table := range []string{"stock_movements", "movement_lines", "audit_events"} {
		if _, err := f.db.Exec("DELETE FROM " + table); err == nil {
			t.Fatalf("%s deletion allowed", table)
		}
	}
	if _, err := f.db.Exec(`UPDATE stock_movements SET note='hidden edit'`); err == nil {
		t.Fatal("history update allowed")
	}
	if _, err := f.db.Exec(`UPDATE stock_balances SET quantity=1.5`); err == nil {
		t.Fatal("fractional quantity accepted")
	}
}
func TestStockPermissionsAndRollback(t *testing.T) {
	f := stockDB(t)
	input := stockRequest{BatchID: demoBatch, To: warehouse, Quantity: 20}
	requireStatus(t, stockHTTP(f, "receipts", "permissions-key01", input), 201)
	_, err := f.db.Exec(`INSERT INTO sites VALUES('foreign-site','00000000-0000-4000-8000-000000000001','Foreign');INSERT INTO locations VALUES('foreign-location','foreign-site','Foreign')`)
	if err != nil {
		t.Fatal(err)
	}
	input = stockRequest{BatchID: demoBatch, From: warehouse, To: "foreign-location", Quantity: 5, ExpectedVersion: 1}
	requireStatus(t, stockHTTP(f, "transfers", "permissions-key02", input), 404)
	if q, _ := quantity(t, f.db, warehouse, "available"); q != 20 {
		t.Fatal("foreign target changed source")
	}
	assertJournal(t, f.db, 1)
	// A late failure must roll back source, target, journal, audit and replay response.
	if _, err = f.db.Exec(`CREATE TRIGGER fail_outbox BEFORE INSERT ON outbox BEGIN SELECT RAISE(ABORT,'simulated write failure'); END`); err != nil {
		t.Fatal(err)
	}
	input.To = kit
	requireStatus(t, stockHTTP(f, "transfers", "rollback-key-0001", input), 503)
	if q, v := quantity(t, f.db, warehouse, "available"); q != 20 || v != 1 {
		t.Fatal("source not rolled back")
	}
	var targets int
	f.db.QueryRow(`SELECT COUNT(*) FROM stock_balances WHERE location_id=?`, kit).Scan(&targets)
	if targets != 0 {
		t.Fatal("target not rolled back")
	}
	assertJournal(t, f.db, 1)
	f.db.Exec(`DROP TRIGGER fail_outbox`)
	requireStatus(t, stockHTTP(f, "transfers", "rollback-key-0001", input), 201)
	if err = SetSiteRole(f.db, "viewer", []string{demoSite}, "viewer"); err != nil {
		t.Fatal(err)
	}
	f.cookie, f.csrf = loginTest(t, f.handler, nil)
	requireStatus(t, stockHTTP(f, "receipts", "viewer-write-key1", stockRequest{BatchID: demoBatch, To: warehouse, Quantity: 1}), 404)
	if err = SetSiteRole(f.db, "viewer", []string{demoSite}, "helper"); err != nil {
		t.Fatal(err)
	}
	f.cookie, f.csrf = loginTest(t, f.handler, nil)
	requireStatus(t, stockHTTP(f, "transfers", "helper-write-key1", stockRequest{BatchID: demoBatch, From: warehouse, To: kit, Quantity: 1, ExpectedVersion: 2}), 404)
	requireStatus(t, stockHTTP(f, "consumptions", "helper-write-key2", stockRequest{BatchID: demoBatch, From: kit, Quantity: 1, ExpectedVersion: 1}), 201)
	badCSRF := f
	badCSRF.csrf = "incorrect"
	requireStatus(t, stockHTTP(badCSRF, "consumptions", "csrf-write-key001", stockRequest{BatchID: demoBatch, From: kit, Quantity: 1, ExpectedVersion: 2}), 403)
	_, err = bookStock(context.Background(), f.db, f.user, "revoked-session", "revoked-key-0001", "consumption", stockRequest{BatchID: demoBatch, From: kit, Quantity: 1, ExpectedVersion: 2}, time.Now())
	if err != errUnauthenticated {
		t.Fatalf("revoked session: %v", err)
	}
}
func TestUnusableStockAndDates(t *testing.T) {
	f := stockDB(t)
	requireStatus(t, stockHTTP(f, "receipts", "expiry-receipt001", stockRequest{BatchID: demoBatch, To: warehouse, Quantity: 10}), 201)
	for _, update := range []string{`UPDATE batches SET blocked=1`, `UPDATE batches SET blocked=0,expires_on='2020-01-01'`, `UPDATE batches SET expires_on=NULL`} {
		if _, err := f.db.Exec(update); err != nil {
			t.Fatal(err)
		}
		requireStatus(t, stockHTTP(f, "consumptions", "expiry-consume001", stockRequest{BatchID: demoBatch, From: warehouse, Quantity: 1, ExpectedVersion: 1}), 409)
		w := authRequest(f.handler, "GET", "/api/v1/stock/balances", nil, f.cookie, "", "")
		requireStatus(t, w, 200)
		var rows []struct {
			Usable int64 `json:"usable_quantity"`
		}
		json.Unmarshal(w.Body.Bytes(), &rows)
		if len(rows) != 1 || rows[0].Usable != 0 {
			t.Fatal("unusable stock counted")
		}
	}
	requireStatus(t, stockHTTP(f, "receipts", "unknown-receipt01", stockRequest{BatchID: demoBatch, To: kit, Quantity: 3}), 201)
	if q, _ := quantity(t, f.db, kit, "quarantined"); q != 3 {
		t.Fatal("unknown expiry not quarantined")
	}
	if validDay("2027-02-29") || !validDay("2028-02-29") || validDay("2026-2-1") {
		t.Fatal("date validation")
	}
	if day := localDay(time.Date(2026, 10, 9, 22, 30, 0, 0, time.UTC)); day != "2026-10-10" {
		t.Fatalf("Berlin boundary: %s", day)
	}
	requireStatus(t, stockHTTP(f, "receipts", "negative-key-0001", stockRequest{BatchID: demoBatch, To: warehouse, Quantity: -1}), 400)
	requireStatus(t, stockHTTP(f, "receipts", "", stockRequest{BatchID: demoBatch, To: warehouse, Quantity: 1}), 400)
}
func secondDB(t *testing.T, db *sql.DB) *sql.DB {
	t.Helper()
	var name, path string
	var n int
	if err := db.QueryRow(`PRAGMA database_list`).Scan(&n, &name, &path); err != nil {
		t.Fatal(err)
	}
	other, err := Open(filepath.Clean(path))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { other.Close() })
	return other
}
func TestConcurrentConsumption(t *testing.T) {
	f := stockDB(t)
	other := secondDB(t, f.db)
	requireStatus(t, stockHTTP(f, "receipts", "parallel-receipt1", stockRequest{BatchID: demoBatch, To: warehouse, Quantity: 10}), 201)
	start := make(chan struct{})
	results := make(chan error, 2)
	for i, db := range []*sql.DB{f.db, other} {
		go func() {
			<-start
			_, err := bookStock(context.Background(), db, f.user, digest(f.cookie.Value), []string{"parallel-take-001", "parallel-take-002"}[i], "consumption", stockRequest{BatchID: demoBatch, From: warehouse, Quantity: 8, ExpectedVersion: 1}, time.Now())
			results <- err
		}()
	}
	close(start)
	success := 0
	for range 2 {
		err := <-results
		if err == nil {
			success++
		} else {
			e, ok := err.(*stockError)
			if !ok || e.Code != "stock_conflict" {
				t.Fatal(err)
			}
		}
	}
	if success != 1 {
		t.Fatalf("successes %d", success)
	}
	if q, v := quantity(t, f.db, warehouse, "available"); q != 2 || v != 2 {
		t.Fatalf("oversold %d/%d", q, v)
	}
	assertJournal(t, f.db, 2)
	requireStatus(t, stockHTTP(f, "consumptions", "too-much-key-0001", stockRequest{BatchID: demoBatch, From: warehouse, Quantity: 8, ExpectedVersion: 2}), 409)
}
func TestConcurrentReplay(t *testing.T) {
	f := stockDB(t)
	other := secondDB(t, f.db)
	var wg sync.WaitGroup
	results := make(chan []byte, 8)
	errs := make(chan error, 8)
	for i := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			db := f.db
			if i%2 == 1 {
				db = other
			}
			response, err := bookStock(context.Background(), db, f.user, digest(f.cookie.Value), "concurrent-key001", "receipt", stockRequest{BatchID: demoBatch, To: warehouse, Quantity: 10}, time.Now())
			results <- response
			errs <- err
		}()
	}
	wg.Wait()
	close(results)
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	var first []byte
	for response := range results {
		if first == nil {
			first = response
		}
		if !bytes.Equal(first, response) {
			t.Fatal("replay changed")
		}
	}
	if q, v := quantity(t, f.db, warehouse, "available"); q != 10 || v != 1 {
		t.Fatal("double receipt")
	}
	assertJournal(t, f.db, 1)
}
