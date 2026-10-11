package app

import (
	"encoding/json"
	"strings"
	"testing"
)

func recallCoordinator(t *testing.T, f *stockFixture) {
	t.Helper()
	if err := SetAdministrator(f.db, "viewer", true); err != nil {
		t.Fatal(err)
	}
	f.cookie, f.csrf = loginTest(t, f.handler, nil)
}
func preparedRecall(t *testing.T, f stockFixture, s recallSelector) recallInput {
	t.Helper()
	in := recallInput{Action: "create", Selector: &s, Title: "Synthetic recall", Reference: "Manufacturer notice TEST", Evidence: "Notice reference", Note: "Synthetic documented recall"}
	w := packHTTP(f, "POST", "recalls/preview", "", in)
	requireStatus(t, w, 200)
	var preview struct {
		Basis string `json:"basis_hash"`
	}
	json.Unmarshal(w.Body.Bytes(), &preview)
	in.PreviewBasis = preview.Basis
	return in
}
func recallPosted(t *testing.T, f stockFixture, key string, in recallInput) recallRecord {
	t.Helper()
	w := packHTTP(f, "POST", "recalls", key, in)
	requireStatus(t, w, 200)
	var r recallRecord
	if err := json.Unmarshal(w.Body.Bytes(), &r); err != nil {
		t.Fatal(err)
	}
	return r
}
func recallRows(t *testing.T, f stockFixture) []struct {
	recallRecord
	Targets []recallTarget `json:"targets"`
} {
	t.Helper()
	w := packHTTP(f, "GET", "recalls", "", nil)
	requireStatus(t, w, 200)
	var rows []struct {
		recallRecord
		Targets []recallTarget `json:"targets"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &rows); err != nil {
		t.Fatal(err)
	}
	return rows
}
func TestRecallMaterialLifecycleFutureReceiptsAndProof(t *testing.T) {
	f := stockDB(t)
	recallCoordinator(t, &f)
	requireStatus(t, stockHTTP(f, "receipts", "recall-stock-001", stockRequest{BatchID: demoBatch, To: kit, Quantity: 10}), 201)
	p := published(t, f, packInput(kit), "recall-pack-0001")
	before := readiness(t, f, p.ID)
	requireStatus(t, materialCheck(t, f, p.ID, "recall-proof-001", before.Basis, true), 201)
	in := preparedRecall(t, f, recallSelector{Kind: "material", ProductID: demoProduct})
	r := recallPosted(t, f, "recall-create-01", in)
	if readiness(t, f, p.ID).Basis == before.Basis || readiness(t, f, p.ID).Items[0].Usable != 0 {
		t.Fatal("recall failed to invalidate material proof")
	}
	requireStatus(t, stockHTTP(f, "consumptions", "recall-consume01", stockRequest{BatchID: demoBatch, From: kit, Quantity: 1, ExpectedVersion: 1}), 409)
	close := recallInput{Action: "close", ID: r.ID, ExpectedVersion: r.Version, Evidence: "Explicit closure decision", Note: "Close recall"}
	requireStatus(t, packHTTP(f, "POST", "recalls", "recall-close-001", close), 409)
	rows := recallRows(t, f)
	target := rows[0].Targets[0]
	r = recallPosted(t, f, "recall-action-01", recallInput{Action: "complete", ID: r.ID, ExpectedVersion: r.Version, TargetKey: target.Key, Basis: target.Basis, Evidence: "Physical isolation reference", Note: "Physically checked"})
	// Receiving another matching batch after creation must quarantine it too.
	batch, err := CreateBatch(f.db, demoProduct, "FUTURE", "2028-12-31")
	if err != nil {
		t.Fatal(err)
	}
	requireStatus(t, stockHTTP(f, "receipts", "recall-future-01", stockRequest{BatchID: batch, To: warehouse, Quantity: 2}), 201)
	q, _ := quantity(t, f.db, kit, "available")
	if q != 10 {
		t.Fatal("recall rewrote physical stock")
	}
	var state string
	f.db.QueryRow(`SELECT state FROM stock_balances WHERE batch_id=?`, batch).Scan(&state)
	if state != "quarantined" {
		t.Fatal(state)
	}
	close.ExpectedVersion = r.Version
	requireStatus(t, packHTTP(f, "POST", "recalls", "recall-close-002", close), 409)
	for _, tgt := range recallRows(t, f)[0].Targets {
		if !tgt.Completed {
			r = recallPosted(t, f, "recall-future-action", recallInput{Action: "complete", ID: r.ID, ExpectedVersion: r.Version, TargetKey: tgt.Key, Basis: tgt.Basis, Evidence: "Isolation evidence", Note: "Checked future stock"})
		}
	}
	close.ExpectedVersion = r.Version
	r = recallPosted(t, f, "recall-close-final", close)
	if r.Status != "CLOSED" {
		t.Fatal(r)
	}
	after := readiness(t, f, p.ID)
	if after.Basis == before.Basis || after.Check != nil && after.Check.Current {
		t.Fatal("old proof revived after close")
	}
	if again := recallPosted(t, f, "recall-create-01", in); again.Version != 1 {
		t.Fatal("replay changed")
	}
	if _, err = f.db.Exec(`UPDATE recall_events SET action='tamper'`); err == nil {
		t.Fatal("mutable history")
	}
	if err = SetAdministrator(f.db, "viewer", false); err != nil {
		t.Fatal(err)
	}
	f.cookie, f.csrf = loginTest(t, f.handler, nil)
	requireStatus(t, packHTTP(f, "POST", "recalls", "recall-create-01", in), 403)
}
func TestRecallDeviceRangeAndIndependentBlock(t *testing.T) {
	f, id := controlFixture(t)
	recallCoordinator(t, &f)
	// Exact arbitrary serial/lot identifiers are supported independently of numeric ranges.
	in := preparedRecall(t, f, recallSelector{Kind: "device", Manufacturer: "Demo", Model: "Demo", SerialExact: "Demo"})
	r := recallPosted(t, f, "device-recall-create", in)
	w := packHTTP(f, "GET", "devices/"+id+"/book", "", nil)
	requireStatus(t, w, 200)
	var book deviceRecord
	json.Unmarshal(w.Body.Bytes(), &book)
	if len(book.ActiveBlocks) != 1 || !strings.HasPrefix(book.ActiveBlocks[0].ID, "recall:") {
		t.Fatal(book)
	}
	proof := controlProof()
	proof.BlockID = book.ActiveBlocks[0].ID
	requireStatus(t, packHTTP(f, "POST", "devices/"+id+"/releases", "recall-wrong-release", proof), 409)
	target := recallRows(t, f)[0].Targets[0]
	r = recallPosted(t, f, "recall-device-action", recallInput{Action: "complete", ID: r.ID, ExpectedVersion: r.Version, TargetKey: target.Key, Basis: target.Basis, Evidence: "Manufacturer correction", Note: "Checked device"})
	defect := controlProof()
	defect.BlockReason = "defect"
	requireStatus(t, packHTTP(f, "POST", "devices/"+id+"/defects", "recall-independent-defect", defect), 200)
	close := recallInput{Action: "close", ID: r.ID, ExpectedVersion: r.Version, Evidence: "Manufacturer closure reference", Note: "Explicit release"}
	requireStatus(t, packHTTP(f, "POST", "recalls", "recall-device-stale-close", close), 409)
	target = recallRows(t, f)[0].Targets[0]
	r = recallPosted(t, f, "recall-device-new-action", recallInput{Action: "complete", ID: r.ID, ExpectedVersion: r.Version, TargetKey: target.Key, Basis: target.Basis, Evidence: "Current device measure", Note: "Device rechecked"})
	close.ExpectedVersion = r.Version
	recallPosted(t, f, "recall-device-close", close)
	w = packHTTP(f, "GET", "devices/"+id+"/book", "", nil)
	json.Unmarshal(w.Body.Bytes(), &book)
	if len(book.ActiveBlocks) != 1 || book.ActiveBlocks[0].Reason != "defect" || book.RecallRevision <= 1 || book.Readiness != "not_assessed" {
		t.Fatal(book)
	}
	bad := recallInput{Selector: &recallSelector{Kind: "device", Manufacturer: "Demo", Model: "Demo", SerialFrom: "A01", SerialTo: "A99"}}
	requireStatus(t, packHTTP(f, "POST", "recalls/preview", "", bad), 400)
	bad.Selector.SerialFrom = "01"
	bad.Selector.SerialTo = "100"
	requireStatus(t, packHTTP(f, "POST", "recalls/preview", "", bad), 400)
}

func TestRecallNumericRangeScopesAndAtomicity(t *testing.T) {
	f, _ := controlFixture(t)
	recallCoordinator(t, &f)
	_, err := f.db.Exec(`INSERT INTO sites VALUES('other-site','00000000-0000-4000-8000-000000000001','Other');INSERT INTO locations VALUES('other-location','other-site','Other location');INSERT INTO role_assignments VALUES(?,'other-site','device_manager')`, f.user)
	if err != nil {
		t.Fatal(err)
	}
	for _, serial := range []string{"001", "010", "099", "100", "10"} {
		location := warehouse
		if serial == "099" {
			location = "other-location"
		}
		requireStatus(t, packHTTP(f, "POST", "devices", "range-register-"+serial, deviceInput{LocationID: location, InventoryNumber: "RANGE-" + serial, Name: "Range device", Manufacturer: "Range maker", Model: "Range model", SerialOrLot: serial, Note: "Synthetic range test"}), 200)
	}
	// Keep only the primary site; global coordination must not imply remote action rights.
	if err = SetSiteRole(f.db, "viewer", []string{demoSite}, "device_manager"); err != nil {
		t.Fatal(err)
	}
	f.cookie, f.csrf = loginTest(t, f.handler, nil)
	in := preparedRecall(t, f, recallSelector{Kind: "device", Manufacturer: "Range maker", Model: "Range model", SerialFrom: "001", SerialTo: "099"})
	if _, err = f.db.Exec(`CREATE TRIGGER fail_recall_request BEFORE INSERT ON recall_requests BEGIN SELECT RAISE(ABORT,'synthetic failure'); END`); err != nil {
		t.Fatal(err)
	}
	requireStatus(t, packHTTP(f, "POST", "recalls", "range-create-atomic", in), 503)
	var count int
	f.db.QueryRow(`SELECT COUNT(*) FROM recalls`).Scan(&count)
	if count != 0 {
		t.Fatal("partial recall committed")
	}
	f.db.Exec(`DROP TRIGGER fail_recall_request`)
	r := recallPosted(t, f, "range-create-atomic", in)
	rows := recallRows(t, f)
	if len(rows) != 1 || len(rows[0].Targets) != 3 {
		t.Fatal(rows)
	}
	for _, target := range rows[0].Targets {
		if target.LocationID == "other-location" {
			requireStatus(t, packHTTP(f, "POST", "recalls", "foreign-recall-action", recallInput{Action: "complete", ID: r.ID, ExpectedVersion: r.Version, TargetKey: target.Key, Basis: target.Basis, Evidence: "Foreign action", Note: "No local rights"}), 404)
		}
	}
	if err = SetAdministrator(f.db, "viewer", false); err != nil {
		t.Fatal(err)
	}
	f.cookie, f.csrf = loginTest(t, f.handler, nil)
	rows = recallRows(t, f)
	if len(rows) != 1 || len(rows[0].Targets) != 2 {
		t.Fatal("foreign target leaked", rows)
	}
	w := packHTTP(f, "GET", "recalls", "", nil)
	if strings.Contains(w.Body.String(), "other-location") {
		t.Fatal("foreign audit target leaked")
	}
}
func TestRecallCSVPreviewInvalidatedAndNewBatchQuarantined(t *testing.T) {
	f := stockImportFixture(t)
	recallCoordinator(t, &f)
	input := stockImportRequest{CSV: "Erfassungsnummer;Bezeichnung;Standort;Menge;Basiseinheit;Charge;Verfall;Artikel-ID\nRECALL-CSV-1;Demo-Kompresse (synthetisch);Offline;2;Stück;RECALL-NEW;2099-12-31;" + demoProduct + "\n", Locations: map[string]string{"Offline": warehouse}, Mode: "receipt"}
	old := stockPreview(t, f, input)
	if old.Errors != 0 {
		t.Fatal(old)
	}
	input.Basis = old.Basis
	recallPosted(t, f, "csv-recall-create", preparedRecall(t, f, recallSelector{Kind: "material", ProductID: demoProduct}))
	requireStatus(t, packHTTP(f, "POST", "stock-import/commit", "csv-before-recall", input), 409)
	now := stockPreview(t, f, input)
	if now.Rows[0].State != "quarantined" {
		t.Fatal("new batch not quarantined", now)
	}
	input.Basis = now.Basis
	requireStatus(t, packHTTP(f, "POST", "stock-import/commit", "csv-after-recall", input), 201)
}

func TestRecallUsesLatestDeviceIdentityWithoutRevisionAccumulation(t *testing.T) {
	f, id := controlFixture(t)
	recallCoordinator(t, &f)
	details := detailsInput(0)
	requireStatus(t, packHTTP(f, "POST", "devices/"+id+"/details", "recall-current-identity", details), 200)
	in := preparedRecall(t, f, recallSelector{Kind: "device", Manufacturer: "Maker", Model: "Model", SerialExact: "000123"})
	r := recallPosted(t, f, "recall-current-create", in)
	rows := recallRows(t, f)
	if len(rows) != 1 || len(rows[0].Targets) != 1 || rows[0].Targets[0].ID != id {
		t.Fatal("immutable registration used instead of current identity", rows)
	}
	// A further name edit projects the record twice during mutation. The recall revision must agree with a fresh read.
	details.ExpectedVersion = 1
	details.Details.Name = "Updated display name"
	first := packHTTP(f, "POST", "devices/"+id+"/details", "recall-current-name", details)
	requireStatus(t, first, 200)
	var changed, fresh deviceRecord
	json.Unmarshal(first.Body.Bytes(), &changed)
	json.Unmarshal(packHTTP(f, "GET", "devices/"+id+"/book", "", nil).Body.Bytes(), &fresh)
	if changed.RecallRevision != int64(r.Version) || changed.RecallRevision != fresh.RecallRevision {
		t.Fatal("recall revision accumulated", changed.RecallRevision, fresh.RecallRevision)
	}
}
