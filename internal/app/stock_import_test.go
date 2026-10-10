package app

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"sync"
	"testing"
)

const stockCSV = "Erfassungsnummer;Bezeichnung;Standort;Menge;Basiseinheit;Charge;Verfall;Fremdcode;Bemerkung\nM-001;Synthetic CSV material;Offline;12;Stück;000123;2099-12-31;00001234;First capture\n"

func stockImportFixture(t *testing.T) stockFixture {
	t.Helper()
	f := stockDB(t)
	t.Cleanup(func() { f.db.Close() })
	return f
}
func stockPreview(t *testing.T, f stockFixture, input stockImportRequest) stockImportPreview {
	t.Helper()
	response := packHTTP(f, "POST", "stock-import/preview", "stock-preview-00001", input)
	requireStatus(t, response, 200)
	var p stockImportPreview
	if err := json.Unmarshal(response.Body.Bytes(), &p); err != nil {
		t.Fatal(err)
	}
	return p
}
func stockInput() stockImportRequest {
	return stockImportRequest{CSV: stockCSV, Locations: map[string]string{"Offline": warehouse}, Mode: "initial"}
}
func commitStockCSV(t *testing.T, f stockFixture, input *stockImportRequest, key string) stockImportResult {
	t.Helper()
	p := stockPreview(t, f, *input)
	if p.Errors != 0 {
		t.Fatal(p)
	}
	input.Basis = p.Basis
	r := packHTTP(f, "POST", "stock-import/commit", key, *input)
	requireStatus(t, r, 201)
	var result stockImportResult
	json.Unmarshal(r.Body.Bytes(), &result)
	return result
}
func TestStockImportReplayAfterConsumptionAndReversal(t *testing.T) {
	f := stockImportFixture(t)
	input := stockInput()
	unmapped := input
	unmapped.Locations = nil
	if p := stockPreview(t, f, unmapped); p.Errors != 1 {
		t.Fatal(p)
	}
	var count int
	f.db.QueryRow(`SELECT COUNT(*) FROM stock_movements`).Scan(&count)
	if count != 0 {
		t.Fatal("preview wrote movements")
	}
	result := commitStockCSV(t, f, &input, "stock-import-commit01")
	if result.Created != 1 || len(result.Movements) != 1 {
		t.Fatal(result)
	}
	first := packHTTP(f, "POST", "stock-import/commit", "stock-import-commit01", input)
	requireStatus(t, first, 201)
	var batch, code string
	var q int64
	f.db.QueryRow(`SELECT batch_id FROM stock_movements WHERE id=?`, result.Movements[0]).Scan(&batch)
	f.db.QueryRow(`SELECT external_code FROM stock_intake`).Scan(&code)
	if code != "00001234" {
		t.Fatal(code)
	}
	var lot string
	f.db.QueryRow(`SELECT lot FROM batches WHERE id=?`, batch).Scan(&lot)
	if lot != "000123" {
		t.Fatal(lot)
	}
	requireStatus(t, stockHTTP(f, "consumptions", "stock-import-take01", stockRequest{BatchID: batch, From: warehouse, Quantity: 2, ExpectedVersion: 1}), 201)
	p := stockPreview(t, f, input)
	if p.Unchanged != 1 || p.New != 0 {
		t.Fatal(p)
	}
	input.Basis = p.Basis
	requireStatus(t, packHTTP(f, "POST", "stock-import/commit", "stock-import-newkey1", input), 201)
	f.db.QueryRow(`SELECT quantity FROM stock_balances WHERE batch_id=?`, batch).Scan(&q)
	if q != 10 {
		t.Fatal("duplicate receipt", q)
	}
	requireStatus(t, reversalHTTP(f, "stock-import-reverse", result.Movements[0], reversalRequest{Versions: []versionExpectation{{warehouse, "available", 2}}, Note: "Synthetic erroneous receipt"}), 409)
	// Return the consumption before reversing the original receipt.
	var take string
	f.db.QueryRow(`SELECT id FROM stock_movements WHERE kind='consumption'`).Scan(&take)
	requireStatus(t, reversalHTTP(f, "stock-import-return1", take, reversalRequest{Versions: []versionExpectation{{warehouse, "available", 2}}, Note: "Synthetic return"}), 201)
	requireStatus(t, reversalHTTP(f, "stock-import-reverse2", result.Movements[0], reversalRequest{Versions: []versionExpectation{{warehouse, "available", 3}}, Note: "Synthetic erroneous receipt"}), 201)
	p = stockPreview(t, f, input)
	if p.Unchanged != 1 {
		t.Fatal(p)
	}
	input.Basis = p.Basis
	requireStatus(t, packHTTP(f, "POST", "stock-import/commit", "stock-import-postundo", input), 201)
	f.db.QueryRow(`SELECT quantity FROM stock_balances WHERE batch_id=?`, batch).Scan(&q)
	if q != 0 {
		t.Fatal("reimport undid reversal", q)
	}
	// Exact replay of the original body works after both inventory changes.
	original := stockInput()
	// Recover the original request basis from the immutable audit.
	f.db.QueryRow(`SELECT preview_hash FROM stock_imports WHERE id=?`, result.ID).Scan(&original.Basis)
	replay := packHTTP(f, "POST", "stock-import/commit", "stock-import-commit01", original)
	requireStatus(t, replay, 201)
	if replay.Body.String() != first.Body.String() {
		t.Fatal("replay changed")
	}
	changed := stockInput()
	changed.CSV = strings.Replace(changed.CSV, ";12;", ";13;", 1)
	p = stockPreview(t, f, changed)
	if p.Errors != 1 {
		t.Fatal(p)
	}
	if _, err := f.db.Exec(`DELETE FROM stock_intake`); err == nil {
		t.Fatal("identity deletable")
	}
}
func TestStockImportInitialVsReceiptAndStaleBasis(t *testing.T) {
	f := stockImportFixture(t)
	input := stockInput()
	commitStockCSV(t, f, &input, "stock-first-0000001")
	input.CSV = strings.Replace(input.CSV, "M-001", "M-002", 1)
	p := stockPreview(t, f, input)
	if p.Errors != 1 {
		t.Fatal("first amount added to existing", p)
	}
	input.Mode = "receipt"
	p = stockPreview(t, f, input)
	if p.Errors != 0 || p.New != 1 {
		t.Fatal(p)
	}
	input.Basis = p.Basis
	var batch string
	f.db.QueryRow(`SELECT id FROM batches WHERE lot='000123'`).Scan(&batch)
	requireStatus(t, stockHTTP(f, "receipts", "stock-stale-receipt", stockRequest{BatchID: batch, To: warehouse, Quantity: 1}), 201)
	requireStatus(t, packHTTP(f, "POST", "stock-import/commit", "stock-stale-import1", input), 409)
	result := commitStockCSV(t, f, &input, "stock-later-0000001")
	if result.Created != 1 {
		t.Fatal(result)
	}
	var q int
	f.db.QueryRow(`SELECT quantity FROM stock_balances WHERE batch_id=?`, batch).Scan(&q)
	if q != 25 {
		t.Fatal(q)
	}
	// A changed date cannot modify a known batch.
	input.CSV = strings.Replace(strings.Replace(input.CSV, "M-002", "M-003", 1), "2099-12-31", "2098-12-31", 1)
	if p = stockPreview(t, f, input); p.Errors != 1 {
		t.Fatal(p)
	}
}
func TestStockImportQuarantineAtomicityAndCatalog(t *testing.T) {
	f := stockImportFixture(t)
	input := stockInput()
	input.CSV = "Erfassungsnummer;Bezeichnung;Standort;Menge;Basiseinheit;Charge;Verfall\nQ-001;Unclear CSV material;Offline;5;Stück;;2099-12-31\nQ-002;Unclear CSV material;Offline;3;Stück;lot;\n"
	result := commitStockCSV(t, f, &input, "stock-quarantine001")
	if result.Created != 2 {
		t.Fatal(result)
	}
	var products, batches, amount, bindings int
	f.db.QueryRow(`SELECT COUNT(*) FROM product_types WHERE name='Unclear CSV material'`).Scan(&products)
	f.db.QueryRow(`SELECT COUNT(*) FROM batches b JOIN product_types p ON p.id=b.product_id WHERE p.name='Unclear CSV material'`).Scan(&batches)
	f.db.QueryRow(`SELECT SUM(quantity) FROM stock_balances WHERE state='quarantined'`).Scan(&amount)
	f.db.QueryRow(`SELECT COUNT(*) FROM product_identifiers`).Scan(&bindings)
	if products != 1 || batches != 2 || amount != 8 || bindings != 0 {
		t.Fatal(products, batches, amount, bindings)
	}
	input = stockInput()
	input.CSV += "M-BAD;Bad amount;Offline;1,5;Stück;;;;\n"
	p := stockPreview(t, f, input)
	if p.Errors != 1 || p.New != 1 {
		t.Fatal(p)
	}
	input.Basis = p.Basis
	requireStatus(t, packHTTP(f, "POST", "stock-import/commit", "stock-atomic-000001", input), 400)
	f.db.QueryRow(`SELECT COUNT(*) FROM product_types WHERE name='Synthetic CSV material'`).Scan(&products)
	if products != 0 {
		t.Fatal("partial import")
	}
	input = stockInput()
	if _, err := CreateProduct(f.db, "Synthetic CSV material", "Packung"); err != nil {
		t.Fatal(err)
	}
	if p = stockPreview(t, f, input); p.Errors != 1 {
		t.Fatal("silent unit conversion", p)
	}
}
func TestStockImportScopeKeysAndConcurrentReplay(t *testing.T) {
	f := stockImportFixture(t)
	input := stockInput()
	p := stockPreview(t, f, input)
	input.Basis = p.Basis
	statuses := make(chan int, 2)
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			statuses <- packHTTP(f, "POST", "stock-import/commit", "stock-concurrent001", input).Code
		}()
	}
	wg.Wait()
	close(statuses)
	for s := range statuses {
		if s != 201 {
			t.Fatal(s)
		}
	}
	var count int
	f.db.QueryRow(`SELECT COUNT(*) FROM stock_movements`).Scan(&count)
	if count != 1 {
		t.Fatal(count)
	}
	requireStatus(t, stockHTTP(f, "receipts", "stock-concurrent001", stockRequest{BatchID: demoBatch, To: warehouse, Quantity: 1}), 409)
	forbidden := stockInput()
	forbidden.Locations["Offline"] = "unscoped"
	requireStatus(t, packHTTP(f, "POST", "stock-import/preview", "stock-scope-000001", forbidden), 404)
	if err := SetSiteRole(f.db, "viewer", []string{demoSite}, "viewer"); err != nil {
		t.Fatal(err)
	}
	f.cookie, f.csrf = loginTest(t, f.handler, nil)
	requireStatus(t, packHTTP(f, "POST", "stock-import/commit", "stock-concurrent001", input), 404)
	requireStatus(t, packHTTP(f, "GET", "stock-import/template.xlsx", "", nil), 404)
}
func TestStockIntakeTemplateNamespaces(t *testing.T) {
	first, err := NewStockIntakeTemplate()
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewStockIntakeTemplate()
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(first, second) {
		t.Fatal("namespace reused")
	}
	reader, err := zip.NewReader(bytes.NewReader(first), int64(len(first)))
	if err != nil {
		t.Fatal(err)
	}
	images := 0
	var shared string
	for _, file := range reader.File {
		if strings.HasPrefix(file.Name, "xl/media/") {
			images++
		}
		if file.Name == "xl/sharedStrings.xml" {
			r, _ := file.Open()
			b, _ := io.ReadAll(r)
			r.Close()
			shared = string(b)
		}
	}
	if images != 100 || !strings.Contains(shared, "Erfassungsnummer") || !strings.Contains(shared, "Basiseinheit") || strings.Contains(shared, "D-TEMPLATE0000") || !strings.Contains(shared, "M-") {
		t.Fatal("invalid workbook")
	}
}

func TestStockImportRollsBackOnLedgerFailure(t *testing.T) {
	f := stockImportFixture(t)
	input := stockInput()
	p := stockPreview(t, f, input)
	input.Basis = p.Basis
	if _, err := f.db.Exec(`CREATE TRIGGER fail_csv_audit BEFORE INSERT ON audit_events BEGIN SELECT RAISE(ABORT,'synthetic journal failure'); END`); err != nil {
		t.Fatal(err)
	}
	requireStatus(t, packHTTP(f, "POST", "stock-import/commit", "stock-rollback-0001", input), 503)
	for _, table := range []string{"stock_imports", "stock_intake", "stock_movements", "stock_balances", "movement_lines", "audit_events", "outbox", "stock_import_outbox", "stock_import_requests"} {
		var n int
		if err := f.db.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&n); err != nil || n != 0 {
			t.Fatal(table, n, err)
		}
	}
	var n int
	f.db.QueryRow(`SELECT COUNT(*) FROM product_types WHERE name='Synthetic CSV material'`).Scan(&n)
	if n != 0 {
		t.Fatal("catalog partially committed")
	}
	f.db.Exec(`DROP TRIGGER fail_csv_audit`)
	requireStatus(t, packHTTP(f, "POST", "stock-import/commit", "stock-rollback-0001", input), 201)
}
func TestStockImportStrictCSVAndUnknownExpiry(t *testing.T) {
	f := stockImportFixture(t)
	for _, bad := range []string{"Erfassungsnummer;Bezeichnung;Standort;Menge;Basiseinheit;Unknown\nx;n;l;1;u;x\n", "Erfassungsnummer;Bezeichnung;Standort;Menge;Basiseinheit\nx;n;l;1\n", string([]byte{0xff})} {
		requireStatus(t, packHTTP(f, "POST", "stock-import/preview", "stock-invalid-00001", stockImportRequest{CSV: bad, Mode: "initial"}), 400)
	}
	for _, q := range []string{"0", "-1", "1.5", "1000000001", "9223372036854775808"} {
		in := stockInput()
		in.CSV = strings.Replace(in.CSV, ";12;", ";"+q+";", 1)
		if p := stockPreview(t, f, in); p.Errors != 1 {
			t.Fatal(q, p)
		}
	}
	in := stockInput()
	in.CSV = strings.Replace(in.CSV, "2099-12-31", "2099-12", 1)
	if p := stockPreview(t, f, in); p.Errors != 1 {
		t.Fatal(p)
	}
	in = stockInput()
	in.CSV = strings.Replace(in.CSV, "2099-12-31", "2000-01-01", 1)
	if p := stockPreview(t, f, in); p.Errors != 0 || p.Rows[0].State != "quarantined" {
		t.Fatal(p)
	}
	in = stockInput()
	in.CSV = "\ufeff" + strings.ReplaceAll(in.CSV, ";", ",")
	if p := stockPreview(t, f, in); p.Errors != 0 {
		t.Fatal(p)
	}
}

func TestStockImportRejectsDuplicateRowsAndDefinitions(t *testing.T) {
	f := stockImportFixture(t)
	input := stockInput()
	input.CSV += "m-001;Synthetic CSV material;Offline;2;Stück;000123;2099-12-31;;\n"
	if p := stockPreview(t, f, input); p.Errors != 2 {
		t.Fatal("case-folded source duplicate", p)
	}
	input = stockInput()
	input.CSV += "M-002;Synthetic CSV material;Offline;2;Packung;000123;2099-12-31;;\n"
	if p := stockPreview(t, f, input); p.Errors == 0 {
		t.Fatal("inconsistent units", p)
	}
	input = stockInput()
	input.CSV += "M-002;Synthetic CSV material;Offline;2;Stück;000123;2098-12-31;;\n"
	if p := stockPreview(t, f, input); p.Errors == 0 {
		t.Fatal("inconsistent expiry", p)
	}
	input = stockInput()
	input.CSV += "M-002;Synthetic CSV material;Offline;2;Stück;000123;2099-12-31;;\n"
	result := commitStockCSV(t, f, &input, "stock-same-batch001")
	if result.Created != 2 {
		t.Fatal(result)
	}
	var n, q int
	f.db.QueryRow(`SELECT COUNT(*) FROM batches WHERE lot='000123'`).Scan(&n)
	f.db.QueryRow(`SELECT SUM(quantity) FROM stock_balances`).Scan(&q)
	if n != 1 || q != 14 {
		t.Fatal(n, q)
	}
}
func TestStockImportCatalogMappingAndEmptyExistingBalance(t *testing.T) {
	f := stockImportFixture(t)
	product, err := CreateProduct(f.db, "Synthetic CSV material", "Stück")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = CreateProduct(f.db, "Synthetic CSV material", "Stück"); err != nil {
		t.Fatal(err)
	}
	input := stockInput()
	if p := stockPreview(t, f, input); p.Errors != 1 {
		t.Fatal("ambiguous name silently mapped", p)
	}
	input.CSV = strings.Replace(input.CSV, ";Bemerkung\n", ";Bemerkung;Artikel-ID\n", 1)
	input.CSV = strings.TrimSuffix(input.CSV, "\n") + ";" + product + "\n"
	result := commitStockCSV(t, f, &input, "stock-explicit-id01")
	var batch string
	f.db.QueryRow(`SELECT batch_id FROM stock_movements WHERE id=?`, result.Movements[0]).Scan(&batch)
	requireStatus(t, reversalHTTP(f, "stock-empty-return1", result.Movements[0], reversalRequest{Versions: []versionExpectation{{warehouse, "available", 1}}, Note: "Synthetic complete return"}), 201)
	input.CSV = strings.Replace(input.CSV, "M-001", "M-NEW", 1)
	if p := stockPreview(t, f, input); p.Errors != 1 {
		t.Fatal("reinitialized previously captured zero balance", p)
	}
	input.Mode = "receipt"
	if p := stockPreview(t, f, input); p.Errors != 0 || p.Rows[0].Product != product {
		t.Fatal(p)
	}
}
