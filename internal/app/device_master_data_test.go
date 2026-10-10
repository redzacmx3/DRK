package app

import (
	"encoding/csv"
	"encoding/json"
	"strings"
	"testing"
)

func masterInput(version int) deviceControlInput {
	return deviceControlInput{ExpectedVersion: version, Evidence: "Synthetic invoice and label", Note: "Synthetic inventory completion", MasterData: &deviceMasterData{AcquisitionYear: "2024", AssetType: "Defibrillator", ActorRole: "manufacturer", ActorName: "Synthetic Maker", ActorAddress: "Example Street 1, 12345 Example", OperationalAssignment: "Bereitschaft"}}
}
func TestMasterDataHistoryReplayExportAndPreservedDetails(t *testing.T) {
	f, id := controlFixture(t)
	input := masterInput(0)
	first := packHTTP(f, "POST", "devices/"+id+"/master-data", "master-data-first01", input)
	requireStatus(t, first, 200)
	var record deviceRecord
	json.Unmarshal(first.Body.Bytes(), &record)
	if record.MasterData == nil || *record.MasterData != *input.MasterData || record.Version != 1 || len(record.ActiveBlocks) != 0 || record.Lifecycle != "NOT_COMMISSIONED" {
		t.Fatal(record)
	}
	requireStatus(t, packHTTP(f, "POST", "devices/"+id+"/master-data", "master-data-stale01", input), 409)
	details := detailsInput(1)
	requireStatus(t, packHTTP(f, "POST", "devices/"+id+"/details", "master-after-details", details), 200)
	if replay := packHTTP(f, "POST", "devices/"+id+"/master-data", "master-data-first01", input); replay.Body.String() != first.Body.String() {
		t.Fatal("exact replay changed")
	}
	input.ExpectedVersion = 2
	input.MasterData.ActorAddress = "Corrected address"
	requireStatus(t, packHTTP(f, "POST", "devices/"+id+"/master-data", "master-data-second1", input), 200)
	book, files := bookArchive(t, f, id)
	if book.Device.MasterData.ActorAddress != "Corrected address" || book.Device.Name != details.Details.Name || book.Controls[2].Action != "master_data" || !strings.Contains(string(book.Controls[2].Proof), "Example Street") {
		t.Fatal(book)
	}
	if !strings.Contains(string(files["geraeteakte.html"]), "Corrected address") {
		t.Fatal("HTML master data missing")
	}
	w := packHTTP(f, "GET", "devices/export.csv", "", nil)
	requireStatus(t, w, 200)
	r := csv.NewReader(strings.NewReader(strings.TrimPrefix(w.Body.String(), "\xef\xbb\xbf")))
	r.Comma = ';'
	rows, err := r.ReadAll()
	if err != nil || rows[1][24] != "2024" || rows[1][27] != "Corrected address" {
		t.Fatal(rows, err)
	}
	var name string
	f.db.QueryRow(`SELECT name FROM devices WHERE id=?`, id).Scan(&name)
	if name != "Demo" {
		t.Fatal("immutable registration overwritten")
	}
	input.ExpectedVersion = 3
	requireStatus(t, packHTTP(f, "POST", "devices/"+id+"/master-data", "master-data-nochange", input), 409)
	input.MasterData = &deviceMasterData{}
	requireStatus(t, packHTTP(f, "POST", "devices/"+id+"/master-data", "master-data-clear01", input), 200)
	cleared, _ := bookArchive(t, f, id)
	if cleared.Device.MasterData == nil || len(masterDataGaps(cleared.Device)) != 6 || cleared.Device.Version != 4 {
		t.Fatal("explicit clearing not preserved")
	}
}
func TestMasterDataValidationRollbackAndScope(t *testing.T) {
	f, id := controlFixture(t)
	for n, mutate := range []func(*deviceControlInput){
		func(i *deviceControlInput) { i.MasterData.AcquisitionYear = "9999" }, func(i *deviceControlInput) { i.MasterData.AcquisitionYear = "20" }, func(i *deviceControlInput) { i.MasterData.ActorRole = "guess" }, func(i *deviceControlInput) { i.MasterData.ActorRole = "" }, func(i *deviceControlInput) { i.MasterData.ActorAddress = "bad\naddress" }, func(i *deviceControlInput) { i.MasterData.AssetType = strings.Repeat("x", 161) }, func(i *deviceControlInput) { i.PerformedOn = "2024-01-01" }, func(i *deviceControlInput) { i.Details = detailsInput(0).Details },
	} {
		input := masterInput(0)
		mutate(&input)
		requireStatus(t, packHTTP(f, "POST", "devices/"+id+"/master-data", "invalid-master-key"+string(rune('a'+n)), input), 400)
	}
	if _, err := f.db.Exec(`CREATE TRIGGER reject_master_outbox BEFORE INSERT ON device_control_outbox BEGIN SELECT RAISE(ABORT,'rollback'); END`); err != nil {
		t.Fatal(err)
	}
	input := masterInput(0)
	requireStatus(t, packHTTP(f, "POST", "devices/"+id+"/master-data", "master-atomic-key01", input), 503)
	var count int
	f.db.QueryRow(`SELECT count(*) FROM device_control_events WHERE device_id=?`, id).Scan(&count)
	if count != 0 {
		t.Fatal("partial history")
	}
	f.db.Exec(`DROP TRIGGER reject_master_outbox`)
	requireStatus(t, packHTTP(f, "POST", "devices/"+id+"/master-data", "master-atomic-key01", input), 200)
	if err := SetSiteRole(f.db, "viewer", []string{demoSite}, "helper"); err != nil {
		t.Fatal(err)
	}
	f.cookie, f.csrf = loginTest(t, f.handler, nil)
	requireStatus(t, packHTTP(f, "POST", "devices/"+id+"/master-data", "master-atomic-key01", input), 404)
	input.ExpectedVersion = 1
	input.MasterData.AcquisitionYear = ""
	requireStatus(t, packHTTP(f, "POST", "devices/"+id+"/master-data", "master-helper-denied", input), 404)
}

func TestMasterDataInvalidatesPhysicalProofWithoutTechnicalRelease(t *testing.T) {
	f, id := packDeviceFixture(t)
	packInput := packInput(warehouse)
	packInput.Devices = []packDevice{{DeviceID: id}}
	pack := published(t, f, packInput, "master-physical-pack")
	before := assessment(t, f, pack.ID)
	physical := physicalInput(before, id, true)
	first := packHTTP(f, "POST", "packlists/"+pack.ID+"/device-checks", "master-physical-proof", physical)
	requireStatus(t, first, 201)
	requireStatus(t, packHTTP(f, "POST", "devices/"+id+"/master-data", "master-physical-data", masterInput(2)), 200)
	after := assessment(t, f, pack.ID)
	if after.Check == nil || after.Check.Current || after.Basis == before.Basis || after.Material.Basis != before.Material.Basis || len(after.Devices[0].Device.ActiveBlocks) != len(before.Devices[0].Device.ActiveBlocks) || after.Devices[0].Device.CommissionedOn != before.Devices[0].Device.CommissionedOn {
		t.Fatal(after)
	}
	replay := packHTTP(f, "POST", "packlists/"+pack.ID+"/device-checks", "master-physical-proof", physical)
	requireStatus(t, replay, 201)
	if replay.Body.String() != first.Body.String() {
		t.Fatal("old proof response changed")
	}
}
