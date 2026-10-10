package app

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func packHTTP(f stockFixture, method, path, key string, input any) *httptest.ResponseRecorder {
	if key != "" && len(key) < 16 {
		key = "test-" + key
	}
	var b bytes.Buffer
	if input != nil {
		json.NewEncoder(&b).Encode(input)
	}
	r := httptest.NewRequest(method, testOrigin+"/api/v1/"+path, &b)
	r.AddCookie(f.cookie)
	r.Header.Set("Origin", testOrigin)
	r.Header.Set("X-CSRF-Token", f.csrf)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Idempotency-Key", key)
	w := httptest.NewRecorder()
	f.handler.ServeHTTP(w, r)
	return w
}
func packInput(location string) publishPackRequest {
	return publishPackRequest{Location: location, Title: "Sanitätsmaterial", ValidHours: 24, Note: "Sollstand festlegen", Items: []packItem{{ProductID: "00000000-0000-4000-8000-000000000010", Quantity: 5, MinDays: 30}}}
}
func published(t *testing.T, f stockFixture, input publishPackRequest, key string) packVersion {
	t.Helper()
	w := packHTTP(f, "POST", "packlists", key, input)
	requireStatus(t, w, 201)
	var p packVersion
	if err := json.Unmarshal(w.Body.Bytes(), &p); err != nil {
		t.Fatal(err)
	}
	return p
}
func readiness(t *testing.T, f stockFixture, id string) materialReadiness {
	t.Helper()
	w := packHTTP(f, "GET", "packlists/"+id+"/readiness", "", nil)
	requireStatus(t, w, 200)
	var r materialReadiness
	if err := json.Unmarshal(w.Body.Bytes(), &r); err != nil {
		t.Fatal(err)
	}
	return r
}
func materialCheck(t *testing.T, f stockFixture, id, key, basis string, passed bool) *httptest.ResponseRecorder {
	t.Helper()
	return packHTTP(f, "POST", "packlists/"+id+"/checks", key, checkPackRequest{Basis: basis, Passed: &passed, Note: "Physisch Inhalt und Verpackung geprüft"})
}
func TestMaterialReadinessRequiresCurrentPhysicalCheck(t *testing.T) {
	f := stockDB(t)
	defer f.db.Close()
	p := published(t, f, packInput(kit), "pack-publish-0001")
	r := readiness(t, f, p.ID)
	if r.Status != "shortfall" || r.Items[0].Shortfall != 5 || r.Overall != "not_assessed" {
		t.Fatalf("empty readiness: %+v", r)
	}
	requireStatus(t, stockHTTP(f, "receipts", "pack-receipt-001", stockRequest{BatchID: demoBatch, To: kit, Quantity: 5}), 201)
	r = readiness(t, f, p.ID)
	if r.Status != "needs_check" {
		t.Fatalf("unproved green: %+v", r)
	}
	proof := materialCheck(t, f, p.ID, "physical-proof01", r.Basis, true)
	requireStatus(t, proof, 201)
	r = readiness(t, f, p.ID)
	if r.Status != "ready" || r.Check == nil || !r.Check.Current || r.Overall != "not_assessed" {
		t.Fatalf("proof: %+v", r)
	}
	originalBasis := r.Basis
	requireStatus(t, stockHTTP(f, "consumptions", "pack-consume-0001", stockRequest{BatchID: demoBatch, From: kit, Quantity: 1, ExpectedVersion: 1}), 201)
	r = readiness(t, f, p.ID)
	if r.Status != "shortfall" || r.Check.Current || r.Basis == originalBasis {
		t.Fatalf("stale proof: %+v", r)
	}
	requireStatus(t, materialCheck(t, f, p.ID, "stale-proof-0001", originalBasis, true), 409)
	replay := materialCheck(t, f, p.ID, "physical-proof01", originalBasis, true)
	requireStatus(t, replay, 201)
	if replay.Body.String() != proof.Body.String() {
		t.Fatal("retry must return original check response")
	}
	requireStatus(t, stockHTTP(f, "receipts", "pack-receipt-002", stockRequest{BatchID: demoBatch, To: kit, Quantity: 1}), 201)
	r = readiness(t, f, p.ID)
	if r.Status != "needs_check" || r.Check.Current {
		t.Fatalf("restored amount must not restore physical proof: %+v", r)
	}
	requireStatus(t, materialCheck(t, f, p.ID, "failed-proof-001", r.Basis, false), 201)
	r = readiness(t, f, p.ID)
	if r.Status != "physical_failed" {
		t.Fatalf("failed check: %+v", r)
	}
	requireStatus(t, materialCheck(t, f, p.ID, "fresh-proof-0001", r.Basis, true), 201)
	r = readiness(t, f, p.ID)
	if r.Status != "ready" {
		t.Fatalf("new proof: %+v", r)
	}
	var proofs int
	f.db.QueryRow(`SELECT COUNT(*) FROM readiness_checks`).Scan(&proofs)
	if proofs != 3 {
		t.Fatalf("proof history %d", proofs)
	}
	for _, table := range []string{"packlist_versions", "packlist_items", "readiness_checks", "packlist_events"} {
		if _, err := f.db.Exec("DELETE FROM " + table); err == nil {
			t.Fatalf("mutable %s", table)
		}
	}
}
func TestPacklistUsabilityAndCalendarBoundaries(t *testing.T) {
	f := stockDB(t)
	defer f.db.Close()
	now := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
	product := packInput(kit).Items[0].ProductID
	cases := []struct {
		id, expiry, state string
		blocked           int
		q                 int64
	}{
		{"sufficient", "2026-11-09", "available", 0, 5}, {"too-short", "2026-11-08", "available", 0, 4}, {"expired", "2026-10-09", "available", 0, 3}, {"blocked", "2030-01-01", "available", 1, 2}, {"quarantine", "2030-01-01", "quarantined", 0, 7}, {"damaged", "2030-01-01", "damaged", 0, 8}, {"unknown", "", "available", 0, 1},
	}
	for _, c := range cases {
		var expiry any
		if c.expiry != "" {
			expiry = c.expiry
		}
		if _, err := f.db.Exec(`INSERT INTO batches(id,product_id,lot,expires_on,blocked) VALUES(?,?,?,?,?)`, c.id, product, c.id, expiry, c.blocked); err != nil {
			t.Fatal(err)
		}
		if _, err := f.db.Exec(`INSERT INTO stock_balances VALUES(?,?,?,?,1)`, c.id, kit, c.state, c.q); err != nil {
			t.Fatal(err)
		}
	}
	p := published(t, f, packInput(kit), "pack-fefo-000001")
	tx, err := f.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	r, _, err := calculateMaterial(context.Background(), tx, p, now)
	tx.Rollback()
	if err != nil {
		t.Fatal(err)
	}
	item := r.Items[0]
	if item.Physical != 30 || item.Usable != 5 || item.Unknown != 1 || item.Excluded != 24 || r.Status != "unknown" {
		t.Fatalf("exclusions: %+v / %s", item, r.Status)
	}
	// Unknown dates remain unknown, including when a different lot fulfills the target.
	f.db.Exec(`UPDATE stock_balances SET quantity=0,version=2 WHERE batch_id='unknown'`)
	tx, _ = f.db.Begin()
	r, _, err = calculateMaterial(context.Background(), tx, p, time.Date(2026, 10, 10, 22, 0, 0, 0, time.UTC))
	tx.Rollback()
	if err != nil {
		t.Fatal(err)
	}
	if r.Items[0].Usable != 0 || r.Status != "shortfall" {
		t.Fatalf("Berlin next day threshold: %+v", r)
	}
}
func TestPacklistVersionsRightsAndSharedKeys(t *testing.T) {
	f := stockDB(t)
	defer f.db.Close()
	input := packInput(kit)
	p := published(t, f, input, "version-publish1")
	first := packHTTP(f, "POST", "packlists", "version-publish1", input)
	requireStatus(t, first, 201)
	input.ExpectedVersion = 1
	input.Items[0].Quantity = 7
	p2 := published(t, f, input, "version-publish2")
	requireStatus(t, packHTTP(f, "POST", "packlists", "stale-version-01", input), 409)
	old := readiness(t, f, p.ID)
	if old.Status != "obsolete" || old.CurrentVersion {
		t.Fatalf("old version: %+v", old)
	}
	requireStatus(t, materialCheck(t, f, p.ID, "obsolete-proof01", old.Basis, true), 409)
	replay := packHTTP(f, "POST", "packlists", "version-publish1", packInput(kit))
	requireStatus(t, replay, 201)
	if replay.Body.String() != first.Body.String() {
		t.Fatal("version replay changed")
	}
	requireStatus(t, stockHTTP(f, "receipts", "version-publish1", stockRequest{BatchID: demoBatch, To: kit, Quantity: 1}), 409)
	requireStatus(t, stockHTTP(f, "receipts", "shared-stock-001", stockRequest{BatchID: demoBatch, To: kit, Quantity: 1}), 201)
	input.ExpectedVersion = 2
	requireStatus(t, packHTTP(f, "POST", "packlists", "shared-stock-001", input), 409)
	f.db.Exec(`INSERT INTO sites VALUES('foreign-site','00000000-0000-4000-8000-000000000001','Foreign');INSERT INTO locations VALUES('foreign-location','foreign-site','Foreign')`)
	requireStatus(t, packHTTP(f, "POST", "packlists", "foreign-scope001", packInput("foreign-location")), 404)
	if err := SetSiteRole(f.db, "viewer", []string{demoSite}, "helper"); err != nil {
		t.Fatal(err)
	}
	f.cookie, f.csrf = loginTest(t, f.handler, nil)
	requireStatus(t, packHTTP(f, "POST", "packlists", "helper-publish01", input), 404)
	r := readiness(t, f, p2.ID)
	requireStatus(t, materialCheck(t, f, p2.ID, "helper-check-001", r.Basis, false), 201)
	if err := SetSiteRole(f.db, "viewer", []string{demoSite}, "viewer"); err != nil {
		t.Fatal(err)
	}
	f.cookie, f.csrf = loginTest(t, f.handler, nil)
	requireStatus(t, materialCheck(t, f, p2.ID, "viewer-check-001", r.Basis, true), 404)
	requireStatus(t, packHTTP(f, "GET", "packlists", "", nil), 200)
	if err := SetSiteRole(f.db, "viewer", []string{"foreign-site"}, "viewer"); err != nil {
		t.Fatal(err)
	}
	f.cookie, f.csrf = loginTest(t, f.handler, nil)
	requireStatus(t, packHTTP(f, "GET", "packlists/"+p.ID+"/readiness", "", nil), 404)
	w := packHTTP(f, "GET", "packlists", "", nil)
	requireStatus(t, w, 200)
	if strings.TrimSpace(w.Body.String()) != "[]" {
		t.Fatal("scope leaked")
	}
}
func TestPacklistProofExpiryAndRollback(t *testing.T) {
	f := stockDB(t)
	defer f.db.Close()
	input := packInput(kit)
	input.ValidHours = 1
	p := published(t, f, input, "ttl-publish-0001")
	requireStatus(t, stockHTTP(f, "receipts", "ttl-receipt-00001", stockRequest{BatchID: demoBatch, To: kit, Quantity: 5}), 201)
	r := readiness(t, f, p.ID)
	requireStatus(t, materialCheck(t, f, p.ID, "ttl-check-00001", r.Basis, true), 201)
	r = readiness(t, f, p.ID)
	stamp, _ := time.Parse(time.RFC3339Nano, r.Check.Created)
	tx, _ := f.db.Begin()
	expired, _, err := calculateMaterial(context.Background(), tx, p, stamp.Add(time.Hour))
	tx.Rollback()
	if err != nil {
		t.Fatal(err)
	}
	if expired.Check.Current || expired.Status == "ready" {
		t.Fatalf("expired proof: %+v", expired)
	}
	if _, err = f.db.Exec(`CREATE TRIGGER fail_pack_outbox BEFORE INSERT ON packlist_outbox BEGIN SELECT RAISE(ABORT,'fault'); END;`); err != nil {
		t.Fatal(err)
	}
	input.ExpectedVersion = 1
	w := packHTTP(f, "POST", "packlists", "rollback-pack001", input)
	requireStatus(t, w, 503)
	for table, want := range map[string]int{"packlist_versions": 1, "packlist_items": 1, "readiness_checks": 1, "packlist_events": 2, "packlist_outbox": 2, "packlist_requests": 2} {
		var n int
		if err = f.db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&n); err != nil || n != want {
			t.Fatalf("%s %d want %d %v", table, n, want, err)
		}
	}
	requireStatus(t, materialCheck(t, f, p.ID, "rollback-check01", r.Basis, false), 503)
	var n int
	f.db.QueryRow(`SELECT COUNT(*) FROM readiness_checks`).Scan(&n)
	if n != 1 {
		t.Fatal("proof not rolled back")
	}
}
func TestConcurrentPacklistPublishAndValidation(t *testing.T) {
	f := stockDB(t)
	defer f.db.Close()
	input := packInput(kit)
	other := secondDB(t, f.db)
	var wg sync.WaitGroup
	codes := make(chan int, 2)
	for i, key := range []string{"race-publish-001", "race-publish-002"} {
		handle := f
		handle.handler = Handler([]*sql.DB{f.db, other}[i])
		wg.Add(1)
		go func(key string) { defer wg.Done(); codes <- packHTTP(handle, "POST", "packlists", key, input).Code }(key)
	}
	wg.Wait()
	close(codes)
	success, conflicts := 0, 0
	for code := range codes {
		if code == 201 {
			success++
		} else if code == 409 {
			conflicts++
		} else {
			t.Fatalf("unexpected %d", code)
		}
	}
	if success != 1 || conflicts != 1 {
		t.Fatalf("%d/%d", success, conflicts)
	}
	input.ExpectedVersion = 1
	input.Items = append(input.Items, input.Items[0])
	requireStatus(t, packHTTP(f, "POST", "packlists", "duplicate-item01", input), 400)
	input = packInput(kit)
	input.ExpectedVersion = 1
	input.Items[0].ProductID = "missing"
	requireStatus(t, packHTTP(f, "POST", "packlists", "missing-product1", input), 400)
	requireStatus(t, packHTTP(f, "POST", "packlists", "", packInput(kit)), 400)
	// Physical result cannot be inferred from an omitted JSON boolean.
	list := packHTTP(f, "GET", "packlists", "", nil)
	var ps []packVersion
	json.Unmarshal(list.Body.Bytes(), &ps)
	requireStatus(t, packHTTP(f, "POST", "packlists/"+ps[0].ID+"/checks", "missing-result01", checkPackRequest{Basis: strings.Repeat("a", 64), Note: "Missing explicit result"}), 400)
}

func TestMaterialProofNeverOverridesStockOrBlocks(t *testing.T) {
	f := stockDB(t)
	defer f.db.Close()
	p := published(t, f, packInput(kit), "proof-blocks-001")
	r := readiness(t, f, p.ID)
	requireStatus(t, materialCheck(t, f, p.ID, "empty-good-proof", r.Basis, true), 201)
	if r = readiness(t, f, p.ID); r.Status != "shortfall" {
		t.Fatal("physical proof overrode missing stock")
	}
	requireStatus(t, stockHTTP(f, "receipts", "proof-receipt001", stockRequest{BatchID: demoBatch, To: kit, Quantity: 5}), 201)
	r = readiness(t, f, p.ID)
	requireStatus(t, materialCheck(t, f, p.ID, "ready-proof-0001", r.Basis, true), 201)
	r = readiness(t, f, p.ID)
	if r.Status != "ready" {
		t.Fatal(r.Status)
	}
	if _, err := f.db.Exec(`UPDATE batches SET blocked=1 WHERE id=?`, demoBatch); err != nil {
		t.Fatal(err)
	}
	r = readiness(t, f, p.ID)
	if r.Status != "shortfall" || r.Check.Current || r.Items[0].Physical != 5 || r.Items[0].Usable != 0 {
		t.Fatalf("block did not invalidate proof: %+v", r)
	}
	w := packHTTP(f, "GET", "packlists/"+p.ID+"/checks", "", nil)
	requireStatus(t, w, 200)
	var history []struct {
		Snapshot materialSnapshot `json:"snapshot"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &history); err != nil {
		t.Fatal(err)
	}
	if len(history) != 2 || history[0].Snapshot.Rows[0].Blocked || history[0].Snapshot.Items[0].Usable != 5 {
		t.Fatal("historical data basis was rewritten")
	}
	for _, path := range []string{"packlists", "packlists/" + p.ID + "/checks"} {
		values := map[string]any{"irrelevant": true}
		w := authRequest(f.handler, "POST", "/api/v1/"+path, values, f.cookie, "", "https://attacker.invalid")
		requireStatus(t, w, 403)
	}
}
