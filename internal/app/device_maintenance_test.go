package app

import (
	"encoding/json"
	"strings"
	"testing"
)

func maintenanceInput(version int, result string) deviceControlInput {
	in := controlProof()
	in.ExpectedVersion = version
	in.MaintenanceKind = "repair"
	in.PerformedOn = "2024-02-01"
	in.Examiner = "Synthetic repair company"
	in.Method = "Synthetic replacement and service"
	in.Result = result
	return in
}
func TestMaintenanceProofBlocksHistoryAndExactReplay(t *testing.T) {
	f, id := controlFixture(t)
	commission := controlProof()
	commission.PerformedOn = "2020-03-15"
	requireStatus(t, packHTTP(f, "POST", "devices/"+id+"/commission", "maintenance-commission", commission), 200)
	requireStatus(t, packHTTP(f, "POST", "devices/"+id+"/rules", "maintenance-rules01", controlRules(1)), 200)
	before := packHTTP(f, "GET", "devices/"+id+"/book", "", nil)
	var original deviceRecord
	json.Unmarshal(before.Body.Bytes(), &original)
	failed := maintenanceInput(2, "failed")
	first := packHTTP(f, "POST", "devices/"+id+"/maintenance", "maintenance-failed01", failed)
	requireStatus(t, first, 200)
	var record deviceRecord
	json.Unmarshal(first.Body.Bytes(), &record)
	if record.Version != 3 || len(record.Maintenances) != 1 || len(record.ActiveBlocks) != 1 || record.ActiveBlocks[0].Reason != "defect" || record.Controls["STK"] != original.Controls["STK"] || record.CommissionedOn != original.CommissionedOn {
		t.Fatal(record)
	}
	completed := maintenanceInput(3, "completed")
	requireStatus(t, packHTTP(f, "POST", "devices/"+id+"/maintenance", "maintenance-completed", completed), 200)
	book, _ := bookArchive(t, f, id)
	if len(book.Device.Maintenances) != 2 || len(book.Device.ActiveBlocks) != 1 || book.Device.Maintenances[1].Result != "completed" {
		t.Fatal("completion released old block", book)
	}
	replay := packHTTP(f, "POST", "devices/"+id+"/maintenance", "maintenance-failed01", failed)
	requireStatus(t, replay, 200)
	if replay.Body.String() != first.Body.String() {
		t.Fatal("exact replay changed")
	}
	requireStatus(t, packHTTP(f, "POST", "devices/"+id+"/maintenance", "maintenance-stale01", failed), 409)
	release := controlProof()
	release.ExpectedVersion = 4
	release.BlockID = record.ActiveBlocks[0].ID
	requireStatus(t, packHTTP(f, "POST", "devices/"+id+"/releases", "maintenance-release01", release), 200)
	requireStatus(t, packHTTP(f, "POST", "devices/"+id+"/maintenance", "maintenance-failed01", failed), 200)
	book, _ = bookArchive(t, f, id)
	if len(book.Device.ActiveBlocks) != 0 || len(book.Device.Maintenances) != 2 {
		t.Fatal("replay recreated block/proof")
	}
	requireStatus(t, packHTTP(f, "POST", "devices/"+id+"/maintenance", "maintenance-followup", maintenanceInput(5, "follow_up_required")), 200)
	book, _ = bookArchive(t, f, id)
	if len(book.Device.ActiveBlocks) != 1 || book.Device.ActiveBlocks[0].Reason != "quarantine" {
		t.Fatal(book)
	}
}
func TestMaintenanceDocumentBindingAndScope(t *testing.T) {
	f, id, document := documentFixture(t)
	input := maintenanceInput(0, "completed")
	input.DocumentID = document.ID
	input.Method = "<script>alert(1)</script>"
	requireStatus(t, packHTTP(f, "POST", "devices/"+id+"/maintenance", "maintenance-document", input), 200)
	book, files := bookArchive(t, f, id)
	if len(book.Device.Maintenances) != 1 || book.Device.Maintenances[0].DocumentSHA256 != document.SHA256 || !strings.Contains(string(book.Controls[0].Proof), document.SHA256) {
		t.Fatal(book)
	}
	if strings.Contains(string(files["geraeteakte.html"]), "<script>") || !strings.Contains(string(files["geraeteakte.html"]), "&lt;script&gt;") {
		t.Fatal("unsafe maintenance text")
	}
	details := detailsInput(1)
	requireStatus(t, packHTTP(f, "POST", "devices/"+id+"/details", "maintenance-identity", details), 200)
	book, _ = bookArchive(t, f, id)
	if len(book.Device.ActiveBlocks) != 1 || book.Device.ActiveBlocks[0].Reason != "quarantine" {
		t.Fatal("identity change after technical proof not quarantined")
	}
	input.ExpectedVersion = 2
	input.DocumentID = "foreign-document"
	requireStatus(t, packHTTP(f, "POST", "devices/"+id+"/maintenance", "maintenance-foreign1", input), 400)
	if err := SetSiteRole(f.db, "viewer", []string{demoSite}, "helper"); err != nil {
		t.Fatal(err)
	}
	f.cookie, f.csrf = loginTest(t, f.handler, nil)
	requireStatus(t, packHTTP(f, "POST", "devices/"+id+"/maintenance", "maintenance-document", input), 404)
}
func TestMaintenanceValidationAndAtomicity(t *testing.T) {
	f, id := controlFixture(t)
	for n, mutate := range []func(*deviceControlInput){func(i *deviceControlInput) { i.PerformedOn = "2099-01-01" }, func(i *deviceControlInput) { i.PerformedOn = "2024-02-30" }, func(i *deviceControlInput) { i.MaintenanceKind = "guess" }, func(i *deviceControlInput) { i.Result = "passed" }, func(i *deviceControlInput) { i.Examiner = "" }, func(i *deviceControlInput) { i.Method = "" }, func(i *deviceControlInput) { i.ControlType = "STK" }, func(i *deviceControlInput) { i.DocumentVersion = "mixed" }, func(i *deviceControlInput) { i.Instructor = "mixed" }, func(i *deviceControlInput) { i.Rules = controlRules(0).Rules }, func(i *deviceControlInput) { i.MasterData = masterInput(0).MasterData }} {
		input := maintenanceInput(0, "completed")
		mutate(&input)
		requireStatus(t, packHTTP(f, "POST", "devices/"+id+"/maintenance", "invalid-maintenance-"+string(rune('a'+n)), input), 400)
	}
	if _, err := f.db.Exec(`CREATE TRIGGER reject_maintenance_outbox BEFORE INSERT ON device_control_outbox BEGIN SELECT RAISE(ABORT,'rollback'); END`); err != nil {
		t.Fatal(err)
	}
	input := maintenanceInput(0, "failed")
	requireStatus(t, packHTTP(f, "POST", "devices/"+id+"/maintenance", "maintenance-atomic01", input), 503)
	var count int
	f.db.QueryRow(`SELECT count(*) FROM device_control_events WHERE device_id=?`, id).Scan(&count)
	if count != 0 {
		t.Fatal("partial maintenance committed")
	}
	f.db.Exec(`DROP TRIGGER reject_maintenance_outbox`)
	requireStatus(t, packHTTP(f, "POST", "devices/"+id+"/maintenance", "maintenance-atomic01", input), 200)
}

func TestMaintenanceInvalidatesPhysicalProofWithoutTechnicalRelease(t *testing.T) {
	f, id := packDeviceFixture(t)
	packInput := packInput(warehouse)
	packInput.Devices = []packDevice{{DeviceID: id}}
	pack := published(t, f, packInput, "maintenance-physical-pack")
	before := assessment(t, f, pack.ID)
	physical := physicalInput(before, id, true)
	first := packHTTP(f, "POST", "packlists/"+pack.ID+"/device-checks", "maintenance-physical-proof", physical)
	requireStatus(t, first, 201)
	requireStatus(t, packHTTP(f, "POST", "devices/"+id+"/maintenance", "maintenance-physical-data", maintenanceInput(2, "completed")), 200)
	after := assessment(t, f, pack.ID)
	if after.Check == nil || after.Check.Current || after.Basis == before.Basis || after.Material.Basis != before.Material.Basis || len(after.Devices[0].Device.ActiveBlocks) != len(before.Devices[0].Device.ActiveBlocks) || after.Devices[0].Device.CommissionedOn != before.Devices[0].Device.CommissionedOn {
		t.Fatal(after)
	}
	replay := packHTTP(f, "POST", "packlists/"+pack.ID+"/device-checks", "maintenance-physical-proof", physical)
	requireStatus(t, replay, 201)
	if replay.Body.String() != first.Body.String() {
		t.Fatal("old proof response changed")
	}
}
