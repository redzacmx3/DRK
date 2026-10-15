package app

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"
)

func TestInventorySearchFiltersScopeCurrentIdentityAndPagination(t *testing.T) {
	f, id := controlFixture(t)
	defer f.db.Close()
	proof := detailsInput(0)
	proof.Details = &deviceDetails{Name: "Überwachungsgerät", Manufacturer: "ÄRZTE", Model: "Model", SerialOrLot: "000X"}
	requireStatus(t, packHTTP(f, "POST", "devices/"+id+"/details", "inventory-search-details", proof), 200)
	var page inventoryPage
	get := func(query string) inventoryPage {
		t.Helper()
		response := packHTTP(f, "GET", "inventory/devices?"+query, "", nil)
		requireStatus(t, response, 200)
		if err := json.Unmarshal(response.Body.Bytes(), &page); err != nil {
			t.Fatal(err)
		}
		return page
	}
	if page = get("q=%C3%BCberwachungsger%C3%A4t"); page.Total != 1 || page.Items[0].Name != "Überwachungsgerät" {
		t.Fatal(page)
	}
	if page = get("q=%C3%A4rzte"); page.Total != 1 {
		t.Fatal(page)
	}
	if page = get("q=000X"); page.Total != 1 {
		t.Fatal(page)
	}
	if page = get("q=%25"); page.Total != 0 {
		t.Fatal("search interpreted wildcard", page)
	}
	tx, err := f.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	for i := range 510 {
		deviceID := fmt.Sprintf("workspace-device-%04d", i)
		if _, err = tx.Exec(`INSERT INTO devices VALUES(?,?,?,?,?,?,?,?,?,?)`, deviceID, warehouse, fmt.Sprintf("WORK-%04d", i), "Additional equipment", "", "", "", f.user, "2026-10-16T00:00:00Z", "Synthetic search fixture"); err != nil {
			t.Fatal(err)
		}
		if _, err = tx.Exec(`INSERT INTO device_intake VALUES(?,?,?,?,?,?)`, deviceID, "equipment", "unknown", "EXT-"+deviceID, "", "manual:test"); err != nil {
			t.Fatal(err)
		}
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	page = get("q=WORK-0509&limit=1")
	if page.Total != 1 || page.Items[0].InventoryNumber != "WORK-0509" {
		t.Fatal("search misses records beyond legacy 500 limit", page)
	}
	page = get("kind=equipment&limit=10&offset=500")
	if page.Total != 510 || page.Equipment != 510 || len(page.Items) != 10 || page.Items[0].InventoryNumber != "WORK-0500" {
		t.Fatal(page)
	}
	page = get("kind=equipment&limit=1&direction=desc")
	if page.Items[0].InventoryNumber != "WORK-0509" {
		t.Fatal(page)
	}
	page = get("location=out-of-scope")
	if page.Total != 0 {
		t.Fatal("scope leaked", page)
	}
	for _, query := range []string{"limit=101", "offset=-1", "sort=id;DROP+TABLE+devices", "kind=anything"} {
		requireStatus(t, packHTTP(f, "GET", "inventory/devices?"+query, "", nil), 400)
	}
	if err = SetSiteRole(f.db, "viewer", nil, "viewer"); err != nil {
		t.Fatal(err)
	}
	f.cookie, f.csrf = loginTest(t, f.handler, nil)
	if page = get("q=WORK"); page.Total != 0 || len(page.Items) != 0 {
		t.Fatal("unauthorized inventory disclosed", page)
	}
}
func TestManualInventoryCreationAllowsUnknownIdentityAndClassifiesEquipment(t *testing.T) {
	f, _ := controlFixture(t)
	defer f.db.Close()
	input := deviceInput{LocationID: warehouse, InventoryNumber: "MANUAL-1", Name: "Tragetasche", AssetKind: "equipment", Note: "Manuelle Erstinventarisierung"}
	first := packHTTP(f, "POST", "devices", "manual-inventory-create", input)
	requireStatus(t, first, 200)
	var record deviceRecord
	json.Unmarshal(first.Body.Bytes(), &record)
	if record.Manufacturer != "" || record.SerialOrLot != "" || record.Intake == nil || record.Intake.Kind != "equipment" || record.Version != 0 {
		t.Fatal(record)
	}
	replay := packHTTP(f, "POST", "devices", "manual-inventory-create", input)
	requireStatus(t, replay, 200)
	if replay.Body.String() != first.Body.String() {
		t.Fatal("creation retry changed")
	}
	input.InventoryNumber = "manual-1"
	requireStatus(t, packHTTP(f, "POST", "devices", "manual-inventory-case", input), 409)
	input.InventoryNumber = "MANUAL-2"
	input.AssetKind = "wrong"
	requireStatus(t, packHTTP(f, "POST", "devices", "manual-inventory-kind", input), 400)
	input.AssetKind = "equipment"
	input.Manufacturer = "bad\nmanufacturer"
	requireStatus(t, packHTTP(f, "POST", "devices", "manual-inventory-control", input), 400)
}
func TestDeviceRecognitionPhotosVersionsNormalizationAndRights(t *testing.T) {
	f, id := controlFixture(t)
	defer f.db.Close()
	var source bytes.Buffer
	picture := image.NewRGBA(image.Rect(0, 0, 2, 2))
	picture.Set(0, 0, color.RGBA{255, 0, 0, 255})
	png.Encode(&source, picture)
	raw := append(source.Bytes(), []byte("<script>original trailing metadata</script>")...)
	input := devicePhotoInput{Action: "set", Content: raw, Note: "Foto zur Wiedererkennung"}
	response := packHTTP(f, "POST", "devices/"+id+"/photo", "recognition-photo-key", input)
	requireStatus(t, response, 200)
	var p devicePhoto
	json.Unmarshal(response.Body.Bytes(), &p)
	if p.Version != 1 || p.Action != "set" {
		t.Fatal(p)
	}
	replay := packHTTP(f, "POST", "devices/"+id+"/photo", "recognition-photo-key", input)
	requireStatus(t, replay, 200)
	if replay.Body.String() != response.Body.String() {
		t.Fatal("photo replay changed")
	}
	content := packHTTP(f, "GET", "devices/"+id+"/photo/content", "", nil)
	requireStatus(t, content, 200)
	if content.Header().Get("Content-Type") != "image/png" || bytes.Contains(content.Body.Bytes(), []byte("script")) || digest(content.Body.String()) != p.SHA256 {
		t.Fatal("original upload served or hash mismatch")
	}
	if _, _, err := image.Decode(bytes.NewReader(content.Body.Bytes())); err != nil {
		t.Fatal(err)
	}
	requireStatus(t, packHTTP(f, "POST", "devices/"+id+"/photo", "recognition-photo-stale", input), 409)
	input.ExpectedVersion = 1
	input.Content = []byte("<svg><script/></svg>")
	requireStatus(t, packHTTP(f, "POST", "devices/"+id+"/photo", "recognition-photo-svg", input), 400)
	input = devicePhotoInput{ExpectedVersion: 1, Action: "remove", Note: "Foto ausdrücklich entfernen"}
	requireStatus(t, packHTTP(f, "POST", "devices/"+id+"/photo", "recognition-photo-remove", input), 200)
	requireStatus(t, packHTTP(f, "GET", "devices/"+id+"/photo/content", "", nil), 404)
	history := packHTTP(f, "GET", "devices/"+id+"/photo", "", nil)
	requireStatus(t, history, 200)
	var events []devicePhoto
	json.Unmarshal(history.Body.Bytes(), &events)
	if len(events) != 2 || events[0].Action != "remove" {
		t.Fatal(events)
	}
	book := packHTTP(f, "GET", "devices/"+id+"/book", "", nil)
	var record deviceRecord
	json.Unmarshal(book.Body.Bytes(), &record)
	if record.Version != 0 {
		t.Fatal("photo changed technical proof version")
	}
	if _, err := f.db.Exec(`UPDATE device_photo_events SET note='changed'`); err == nil {
		t.Fatal("photo history mutable")
	}
	if err := SetSiteRole(f.db, "viewer", []string{demoSite}, "viewer"); err != nil {
		t.Fatal(err)
	}
	f.cookie, f.csrf = loginTest(t, f.handler, nil)
	input.ExpectedVersion = 2
	requireStatus(t, packHTTP(f, "POST", "devices/"+id+"/photo", "recognition-photo-denied", input), 404)
	requireStatus(t, packHTTP(f, "POST", "devices/"+id+"/photo", "recognition-photo-key", devicePhotoInput{Action: "set", Content: raw, Note: "Foto zur Wiedererkennung"}), 404)
	if err := SetSiteRole(f.db, "viewer", nil, "viewer"); err != nil {
		t.Fatal(err)
	}
	f.cookie, f.csrf = loginTest(t, f.handler, nil)
	requireStatus(t, packHTTP(f, "GET", "devices/"+id+"/photo", "", nil), 404)
}
func TestRecognitionPhotoSharedKeysAndAtomicity(t *testing.T) {
	f, id := controlFixture(t)
	defer f.db.Close()
	input := devicePhotoInput{Action: "remove", Note: "Synthetic photo removal"}
	// Registration claimed this namespace in controlFixture; use an actual stock key too.
	if _, err := f.db.Exec(`INSERT INTO role_assignments VALUES(?,?,'material_manager')`, f.user, demoSite); err != nil {
		t.Fatal(err)
	}
	requireStatus(t, stockHTTP(f, "receipts", "workspace-shared-photo", stockRequest{BatchID: demoBatch, To: warehouse, Quantity: 1}), 201)
	var b bytes.Buffer
	png.Encode(&b, image.NewRGBA(image.Rect(0, 0, 1, 1)))
	input.Action = "set"
	input.Content = b.Bytes()
	requireStatus(t, packHTTP(f, "POST", "devices/"+id+"/photo", "workspace-shared-photo", input), 409)
	if _, err := f.db.Exec(`CREATE TRIGGER reject_photo_request BEFORE INSERT ON device_photo_requests BEGIN SELECT RAISE(ABORT,'test photo rollback'); END`); err != nil {
		t.Fatal(err)
	}
	requireStatus(t, packHTTP(f, "POST", "devices/"+id+"/photo", "workspace-photo-atomic", input), 503)
	history := packHTTP(f, "GET", "devices/"+id+"/photo", "", nil)
	if strings.TrimSpace(history.Body.String()) != "[]" {
		t.Fatal("partial photo persisted", history.Body.String())
	}
}
