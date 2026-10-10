package app

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"image/png"
	"strings"
	"sync"
	"testing"
	"time"
)

const demoProduct = "00000000-0000-4000-8000-000000000010"

func identifierInput() identifierRequest {
	return identifierRequest{ProductID: demoProduct, Kind: "gtin", Value: "4006381333931", PackQuantity: 10, Note: "Packung enthält zehn Stück; geprüft"}
}
func assigned(t *testing.T, f stockFixture, key string, input identifierRequest) productIdentifier {
	t.Helper()
	w := packHTTP(f, "POST", "product-identifiers", key, input)
	requireStatus(t, w, 201)
	var item productIdentifier
	if err := json.Unmarshal(w.Body.Bytes(), &item); err != nil {
		t.Fatal(err)
	}
	return item
}
func scan(t *testing.T, f stockFixture, text, format string) scanResult {
	t.Helper()
	w := packHTTP(f, "POST", "scanner/resolve", "", map[string]string{"text": text, "format": format})
	requireStatus(t, w, 200)
	var result scanResult
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	return result
}
func TestScannerResolvesWithoutBookingOrGuessing(t *testing.T) {
	f := stockDB(t)
	defer f.db.Close()
	if r := scan(t, f, "4006381333931", "auto"); r.Status != "unregistered_identifier" || r.Batch != nil {
		t.Fatal(r)
	}
	item := assigned(t, f, "assign-gtin-0001", identifierInput())
	if item.Value != "04006381333931" {
		t.Fatal(item)
	}
	r := scan(t, f, "4006381333931", "auto")
	if r.Status != "product_match" || r.Batch != nil || r.PackQuantity != 10 || r.ProductID != demoProduct {
		t.Fatal(r)
	}
	r = scan(t, f, "(01)04006381333931(10)DEMO-2026(17)281231", "auto")
	if r.Status != "batch_match" || r.Batch.ID != demoBatch || *r.Batch.Expiry != "2028-12-31" {
		t.Fatal(r)
	}
	r = scan(t, f, "(01)04006381333931(10)DEMO-2026(17)281230", "auto")
	if r.Status != "batch_mismatch" {
		t.Fatal(r)
	}
	r = scan(t, f, "(01)04006381333931(10)DEMO-2026(17)281200", "auto")
	if r.Status != "requires_review" {
		t.Fatal(r)
	}
	r = scan(t, f, "(01)04006381333931(10)DEMO-2026(21)SERIAL", "auto")
	if r.Status != "requires_review" || r.Parsed.Serial != "SERIAL" {
		t.Fatal(r)
	}
	r = scan(t, f, "(01)04006381333931(10)UNREGISTERED(17)281231", "auto")
	if r.Status != "unregistered_batch" || r.Batch != nil {
		t.Fatal(r)
	}
	r = scan(t, f, "https://example.invalid/redirect", "auto")
	if r.Status != "unknown" || r.ProductID != "" {
		t.Fatal(r)
	}
	r = scan(t, f, "4006381333932", "auto")
	if r.Status != "invalid" || r.ProductID != "" {
		t.Fatal(r)
	}
	r = scan(t, f, "drk:v1:batch:"+demoBatch, "auto")
	if r.Status != "batch_match" || r.PackQuantity != 0 {
		t.Fatal("internal code guessed packaging", r)
	}
	var n int
	for _, table := range []string{"stock_movements", "stock_balances", "readiness_checks"} {
		if err := f.db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&n); err != nil || n != 0 {
			t.Fatalf("scan changed %s: %d %v", table, n, err)
		}
	}
}
func TestIdentifierRevocationAuditReplayAndRoles(t *testing.T) {
	f := stockDB(t)
	defer f.db.Close()
	input := identifierInput()
	first := packHTTP(f, "POST", "product-identifiers", "assign-audit-001", input)
	requireStatus(t, first, 201)
	var item productIdentifier
	json.Unmarshal(first.Body.Bytes(), &item)
	input.Value = "04006381333931"
	replay := packHTTP(f, "POST", "product-identifiers", "assign-audit-001", input)
	requireStatus(t, replay, 201)
	if replay.Body.String() != first.Body.String() {
		t.Fatal("canonical retry changed")
	}
	requireStatus(t, packHTTP(f, "POST", "product-identifiers", "duplicate-gtin01", input), 409)
	for _, statement := range []string{`UPDATE product_identifiers SET pack_quantity=20`, `DELETE FROM product_identifiers`, `UPDATE catalog_events SET note='rewrite'`, `DELETE FROM catalog_events`} {
		if _, err := f.db.Exec(statement); err == nil {
			t.Fatal("mutable history", statement)
		}
	}
	path := "product-identifiers/" + item.ID + "/revoke"
	note := map[string]string{"note": "Falsche Packungsmenge; korrigieren"}
	revoked := packHTTP(f, "POST", path, "revoke-audit-001", note)
	requireStatus(t, revoked, 201)
	requireStatus(t, packHTTP(f, "POST", path, "revoke-second-01", note), 409)
	if _, err := f.db.Exec(`UPDATE product_identifiers SET active=1 WHERE id=?`, item.ID); err == nil {
		t.Fatal("revoked binding reactivated")
	}
	if r := scan(t, f, input.Value, "gtin"); r.Status != "unregistered_identifier" {
		t.Fatal(r)
	}
	input.PackQuantity = 20
	newItem := assigned(t, f, "reassign-gtin001", input)
	if newItem.ID == item.ID {
		t.Fatal("history replaced")
	}
	r := scan(t, f, input.Value, "gtin")
	if r.PackQuantity != 20 || r.IdentifierID != newItem.ID {
		t.Fatal(r)
	}
	old := packHTTP(f, "POST", "product-identifiers", "assign-audit-001", identifierInput())
	requireStatus(t, old, 201)
	if old.Body.String() != first.Body.String() {
		t.Fatal("historical response replaced")
	}
	var events int
	f.db.QueryRow(`SELECT COUNT(*) FROM catalog_events`).Scan(&events)
	if events != 3 {
		t.Fatal(events)
	}
	requireStatus(t, stockHTTP(f, "receipts", "assign-audit-001", stockRequest{BatchID: demoBatch, To: kit, Quantity: 1}), 409)
	requireStatus(t, inventoryHTTP(f, "assign-audit-001", inventoryRequest{BatchID: demoBatch, Location: kit, Counted: intPointer(0), Note: "Collision"}), 409)
	requireStatus(t, packHTTP(f, "POST", "packlists", "assign-audit-001", packInput(kit)), 409)
	requireStatus(t, stockHTTP(f, "receipts", "collision-stock1", stockRequest{BatchID: demoBatch, To: kit, Quantity: 1}), 201)
	input.Kind = "ref"
	input.Value = "REF1"
	requireStatus(t, packHTTP(f, "POST", "product-identifiers", "collision-stock1", input), 409)
	published(t, f, packInput(kit), "collision-pack01")
	requireStatus(t, packHTTP(f, "POST", "product-identifiers", "collision-pack01", input), 409)
	for _, role := range []string{"helper", "viewer"} {
		if err := SetSiteRole(f.db, "viewer", []string{demoSite}, role); err != nil {
			t.Fatal(err)
		}
		f.cookie, f.csrf = loginTest(t, f.handler, nil)
		requireStatus(t, packHTTP(f, "POST", "product-identifiers", "denied-assign-01", input), 404)
		requireStatus(t, packHTTP(f, "POST", path, "denied-revoke-01", note), 404)
		if r = scan(t, f, "4006381333931", "gtin"); r.Status != "product_match" {
			t.Fatal(r)
		}
	}
}
func TestInternalQRScopeAndPNG(t *testing.T) {
	f := stockDB(t)
	defer f.db.Close()
	if _, err := f.db.Exec(`INSERT INTO sites VALUES('foreign-site','00000000-0000-4000-8000-000000000001','Foreign');INSERT INTO locations VALUES('00000000-0000-4000-8000-000000000099','foreign-site','Foreign')`); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"labels/location/00000000-0000-4000-8000-000000000099", "labels/location/00000000-0000-4000-8000-000000000098"} {
		requireStatus(t, packHTTP(f, "GET", path, "", nil), 404)
	}
	requireStatus(t, packHTTP(f, "POST", "scanner/resolve", "", map[string]string{"text": "drk:v1:location:00000000-0000-4000-8000-000000000099"}), 404)
	r := scan(t, f, "drk:v1:location:"+kit, "internal")
	if r.Status != "location_match" || r.Location.ID != kit {
		t.Fatal(r)
	}
	for kind, id := range map[string]string{"location": kit, "product": demoProduct, "batch": demoBatch} {
		w := packHTTP(f, "GET", "labels/"+kind+"/"+id, "", nil)
		requireStatus(t, w, 200)
		if w.Header().Get("Content-Type") != "image/png" || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal(w.Header())
		}
		image, err := png.Decode(bytes.NewReader(w.Body.Bytes()))
		if err != nil || image.Bounds().Dx() != 256 || image.Bounds().Dy() != 256 {
			t.Fatal("bad PNG", err)
		}
		code, _ := internalCode(kind, id)
		parsed, err := parseBarcode(code, "internal")
		if err != nil || parsed.ObjectID != id {
			t.Fatal(parsed, err)
		}
	}
	requireStatus(t, packHTTP(f, "GET", "labels/user/"+kit, "", nil), 400)
	f.cookie = nil
	requireStatus(t, packHTTP(f, "GET", "labels/location/"+kit, "", nil), 401)
}
func TestAmbiguousNumbersAndPZNRequireExplicitIntent(t *testing.T) {
	f := stockDB(t)
	defer f.db.Close()
	input := identifierInput()
	input.Kind = "pzn"
	input.Value = "00001175"
	input.PackQuantity = 1
	assigned(t, f, "assign-pzn-00001", input)
	if r := scan(t, f, input.Value, "auto"); r.Status != "ambiguous" || r.ProductID != "" {
		t.Fatal("catalog guessed format", r)
	}
	if r := scan(t, f, input.Value, "pzn"); r.Status != "product_match" || r.PackQuantity != 1 {
		t.Fatal(r)
	}
	if r := scan(t, f, input.Value, "gtin"); r.Status != "unregistered_identifier" {
		t.Fatal(r)
	}
	input.Kind = "pzn"
	input.Value = "PZN:01234562"
	assigned(t, f, "assign-pzn-00002", input)
	if r := scan(t, f, "-1234562", "auto"); r.Status != "product_match" {
		t.Fatal(r)
	}
	input.Kind = "ref"
	input.Value = "ABC-42"
	assigned(t, f, "assign-ref-00001", input)
	if r := scan(t, f, input.Value, "auto"); r.Status != "unknown" {
		t.Fatal("REF guessed", r)
	}
	if r := scan(t, f, input.Value, "ref"); r.Status != "product_match" {
		t.Fatal(r)
	}
}
func TestIdentifierRollbackAndConcurrentClaims(t *testing.T) {
	f := stockDB(t)
	defer f.db.Close()
	other := secondDB(t, f.db)
	var wg sync.WaitGroup
	results := make(chan error, 2)
	start := make(chan struct{})
	for i, db := range []*sql.DB{f.db, other} {
		wg.Add(1)
		go func(i int, db *sql.DB) {
			defer wg.Done()
			<-start
			_, err := addIdentifier(context.Background(), db, f.user, digest(f.cookie.Value), []string{"race-identifier1", "race-identifier2"}[i], identifierInput(), time.Now())
			results <- err
		}(i, db)
	}
	close(start)
	wg.Wait()
	close(results)
	success, conflicts := 0, 0
	for err := range results {
		if err == nil {
			success++
		} else if err.Error() == "identifier_conflict" {
			conflicts++
		} else {
			t.Fatal(err)
		}
	}
	if success != 1 || conflicts != 1 {
		t.Fatal(success, conflicts)
	}
	var id string
	f.db.QueryRow(`SELECT id FROM product_identifiers WHERE active=1`).Scan(&id)
	if _, err := f.db.Exec(`CREATE TRIGGER fail_catalog_outbox BEFORE INSERT ON catalog_outbox BEGIN SELECT RAISE(ABORT,'fault'); END;`); err != nil {
		t.Fatal(err)
	}
	requireStatus(t, packHTTP(f, "POST", "product-identifiers/"+id+"/revoke", "rollback-revoke1", map[string]string{"note": "Fault rollback"}), 503)
	if r := scan(t, f, "4006381333931", "gtin"); r.Status != "product_match" {
		t.Fatal("revoke not rolled back", r)
	}
	input := identifierInput()
	input.Kind = "ref"
	input.Value = "NEWREF"
	requireStatus(t, packHTTP(f, "POST", "product-identifiers", "rollback-assign1", input), 503)
	for _, table := range []string{"product_identifiers", "catalog_events", "catalog_outbox", "catalog_requests"} {
		var n int
		f.db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&n)
		if n != 1 {
			t.Fatal(table, n)
		}
	}
	input.PackQuantity = 0
	requireStatus(t, packHTTP(f, "POST", "product-identifiers", "missing-pack-001", input), 400)
	requireStatus(t, packHTTP(f, "POST", "scanner/resolve", "", map[string]string{"text": strings.Repeat("x", 1025)}), 400)
}

func TestScannedPackagingIsValidatedAndRecordedWithBooking(t *testing.T) {
	f := stockDB(t)
	defer f.db.Close()
	item := assigned(t, f, "packaging-code01", identifierInput())
	input := stockRequest{BatchID: demoBatch, To: kit, Quantity: 30, IdentifierID: item.ID, PackCount: 3}
	first := stockHTTP(f, "receipts", "packaging-book01", input)
	requireStatus(t, first, 201)
	var result movementResult
	if err := json.Unmarshal(first.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.PackQuantity != 10 || result.PackCount != 3 || result.IdentifierID != item.ID {
		t.Fatal(result)
	}
	var id string
	var packs, factor int64
	if err := f.db.QueryRow(`SELECT identifier_id,pack_count,pack_quantity FROM movement_packaging WHERE movement_id=?`, result.ID).Scan(&id, &packs, &factor); err != nil || id != item.ID || packs != 3 || factor != 10 {
		t.Fatal(id, packs, factor, err)
	}
	journal := packHTTP(f, "GET", "stock/movements", "", nil)
	requireStatus(t, journal, 200)
	var movements []struct {
		ID     string `json:"id"`
		Code   string `json:"identifier_value"`
		Packs  int64  `json:"pack_count"`
		Factor int64  `json:"pack_quantity"`
	}
	if err := json.Unmarshal(journal.Body.Bytes(), &movements); err != nil || len(movements) != 1 || movements[0].ID != result.ID || movements[0].Code != item.Value || movements[0].Packs != 3 || movements[0].Factor != 10 {
		t.Fatalf("packaging basis absent from scoped journal: %s / %v", journal.Body.String(), err)
	}

	input.Quantity = 20
	requireStatus(t, stockHTTP(f, "receipts", "packaging-wrong1", input), 400)
	input.Quantity = 30
	input.PackCount = 1_000_000_000
	requireStatus(t, stockHTTP(f, "receipts", "packaging-large1", input), 400)
	input.PackCount = 3
	input.BatchID = "missing"
	requireStatus(t, stockHTTP(f, "receipts", "packaging-other1", input), 404)
	product, err := CreateProduct(f.db, "Other material", "Stück")
	if err != nil {
		t.Fatal(err)
	}
	batch, err := CreateBatch(f.db, product, "OTHER", "2030-12-31")
	if err != nil {
		t.Fatal(err)
	}
	input.BatchID = batch
	requireStatus(t, stockHTTP(f, "receipts", "packaging-other2", input), 409)
	input.BatchID = demoBatch
	requireStatus(t, packHTTP(f, "POST", "product-identifiers/"+item.ID+"/revoke", "packaging-revoke", map[string]string{"note": "Verpackungscode berichtigen"}), 201)
	requireStatus(t, stockHTTP(f, "receipts", "packaging-stale1", input), 409)
	replay := stockHTTP(f, "receipts", "packaging-book01", input)
	requireStatus(t, replay, 201)
	if replay.Body.String() != first.Body.String() {
		t.Fatal("revocation invalidated original booking replay")
	}
	if q, _ := quantity(t, f.db, kit, "available"); q != 30 {
		t.Fatal("failed packaging requests changed stock", q)
	}
	for _, statement := range []string{`UPDATE movement_packaging SET pack_count=1`, `DELETE FROM movement_packaging`} {
		if _, err := f.db.Exec(statement); err == nil {
			t.Fatal("packaging proof mutable")
		}
	}
}
func TestScannerWriteBoundary(t *testing.T) {
	f := stockDB(t)
	defer f.db.Close()
	for _, path := range []string{"/api/v1/scanner/resolve", "/api/v1/product-identifiers", "/api/v1/product-identifiers/invalid/revoke"} {
		requireStatus(t, authRequest(f.handler, "POST", path, map[string]string{"text": "4006381333931"}, f.cookie, "bad-csrf", testOrigin), 403)
		requireStatus(t, authRequest(f.handler, "POST", path, map[string]string{"text": "4006381333931"}, f.cookie, f.csrf, "https://attacker.invalid"), 403)
		requireStatus(t, authRequest(f.handler, "POST", path, map[string]string{"text": "4006381333931"}, nil, f.csrf, testOrigin), 401)
	}
}
