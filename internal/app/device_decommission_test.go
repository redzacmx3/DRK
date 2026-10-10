package app

import (
	"encoding/csv"
	"strings"
	"testing"
	"time"
)

func decommissionInput(version int) deviceControlInput {
	return deviceControlInput{ExpectedVersion: version, PerformedOn: "2024-02-29", Evidence: "Synthetic retirement reference", Note: "Synthetic disposal decision"}
}
func TestDecommissionPreservesHistoryBlocksAndExactReplay(t *testing.T) {
	f, id := controlFixture(t)
	commission := controlProof()
	commission.PerformedOn = "2020-01-01"
	requireStatus(t, packHTTP(f, "POST", "devices/"+id+"/commission", "retire-commission01", commission), 200)
	failed := maintenanceInput(1, "failed")
	requireStatus(t, packHTTP(f, "POST", "devices/"+id+"/maintenance", "retire-failed-proof", failed), 200)
	in := decommissionInput(2)
	first := packHTTP(f, "POST", "devices/"+id+"/decommission", "retire-proof-first1", in)
	requireStatus(t, first, 200)
	book, files := bookArchive(t, f, id)
	if book.Device.Lifecycle != "DECOMMISSIONED" || book.Device.Decommission == nil || book.Device.Decommission.RetainThrough != "2029-03-01" || len(book.Device.ActiveBlocks) != 1 || len(book.Device.Maintenances) != 1 || book.Device.CommissionedOn != commission.PerformedOn {
		t.Fatal(book)
	}
	if !strings.Contains(string(files["geraeteakte.html"]), "2029-03-01") {
		t.Fatal("retention missing in export")
	}
	requireStatus(t, packHTTP(f, "POST", "devices/"+id+"/decommission", "retire-stale-proof1", in), 409)
	in.ExpectedVersion = 3
	requireStatus(t, packHTTP(f, "POST", "devices/"+id+"/decommission", "retire-second-proof", in), 409)
	for _, action := range []string{"commission", "rules", "inspections", "instructions", "maintenance"} {
		proof := controlProof()
		proof.ExpectedVersion = 3
		requireStatus(t, packHTTP(f, "POST", "devices/"+id+"/"+action, "retire-denied-"+action, proof), 409)
	}
	release := controlProof()
	release.ExpectedVersion = 3
	release.BlockID = book.Device.ActiveBlocks[0].ID
	requireStatus(t, packHTTP(f, "POST", "devices/"+id+"/releases", "retire-release-one1", release), 200)
	book, _ = bookArchive(t, f, id)
	if book.Device.Lifecycle != "DECOMMISSIONED" || len(book.Device.ActiveBlocks) != 0 {
		t.Fatal("release reactivated retired device")
	}
	replay := packHTTP(f, "POST", "devices/"+id+"/decommission", "retire-proof-first1", decommissionInput(2))
	requireStatus(t, replay, 200)
	if replay.Body.String() != first.Body.String() {
		t.Fatal("retirement replay changed")
	}
	requireStatus(t, packHTTP(f, "POST", "devices/"+id+"/maintenance", "retire-failed-proof", failed), 200)
	book, _ = bookArchive(t, f, id)
	if book.Device.Version != 4 || len(book.Device.ActiveBlocks) != 0 {
		t.Fatal("old replay reactivated block")
	}
	csvResponse := packHTTP(f, "GET", "devices/export.csv", "", nil)
	requireStatus(t, csvResponse, 200)
	reader := csv.NewReader(strings.NewReader(strings.TrimPrefix(csvResponse.Body.String(), "\xef\xbb\xbf")))
	reader.Comma = ';'
	rows, err := reader.ReadAll()
	if err != nil || rows[1][30] != "2024-02-29" || rows[1][31] != "2029-03-01" {
		t.Fatal(rows, err)
	}
	if err := SetSiteRole(f.db, "viewer", []string{demoSite}, "helper"); err != nil {
		t.Fatal(err)
	}
	f.cookie, f.csrf = loginTest(t, f.handler, nil)
	requireStatus(t, packHTTP(f, "POST", "devices/"+id+"/decommission", "retire-proof-first1", decommissionInput(2)), 404)
}
func TestDecommissionValidationAndAtomicity(t *testing.T) {
	f, id := controlFixture(t)
	for n, mutate := range []func(*deviceControlInput){func(i *deviceControlInput) { i.PerformedOn = "2099-01-01" }, func(i *deviceControlInput) { i.PerformedOn = "2024-02-30" }, func(i *deviceControlInput) { i.PerformedOn = "" }, func(i *deviceControlInput) { i.DocumentID = "mixed" }, func(i *deviceControlInput) { i.Qualification = "mixed" }, func(i *deviceControlInput) { i.MaintenanceKind = "repair" }, func(i *deviceControlInput) { i.Details = detailsInput(0).Details }} {
		in := decommissionInput(0)
		mutate(&in)
		requireStatus(t, packHTTP(f, "POST", "devices/"+id+"/decommission", "retire-invalid-"+string(rune('a'+n)), in), 400)
	}
	if _, err := f.db.Exec(`CREATE TRIGGER reject_retire_outbox BEFORE INSERT ON device_control_outbox BEGIN SELECT RAISE(ABORT,'rollback'); END`); err != nil {
		t.Fatal(err)
	}
	in := decommissionInput(0)
	requireStatus(t, packHTTP(f, "POST", "devices/"+id+"/decommission", "retire-atomic-proof", in), 503)
	var count int
	f.db.QueryRow(`SELECT count(*) FROM device_control_events WHERE device_id=?`, id).Scan(&count)
	if count != 0 {
		t.Fatal("partial retirement")
	}
	f.db.Exec(`DROP TRIGGER reject_retire_outbox`)
	requireStatus(t, packHTTP(f, "POST", "devices/"+id+"/decommission", "retire-atomic-proof", in), 200)
	// Reject dates before any already documented technical event.
	record := deviceRecord{CommissionedOn: "2024-03-01"}
	if validateDecommission(in, record, time.Now()) == nil {
		t.Fatal("before commissioning accepted")
	}
	record = deviceRecord{Maintenances: []maintenanceProof{{PerformedOn: "2024-03-01"}}}
	if validateDecommission(in, record, time.Now()) == nil {
		t.Fatal("before maintenance accepted")
	}
}
func TestDecommissionBlocksAssessmentAndInvalidatesPhysicalProof(t *testing.T) {
	f, id := packDeviceFixture(t)
	input := packInput(warehouse)
	input.Devices = []packDevice{{DeviceID: id}}
	pack := published(t, f, input, "retire-physical-pack")
	before := assessment(t, f, pack.ID)
	physical := physicalInput(before, id, true)
	first := packHTTP(f, "POST", "packlists/"+pack.ID+"/device-checks", "retire-physical-proof", physical)
	requireStatus(t, first, 201)
	in := decommissionInput(2)
	in.PerformedOn = time.Now().In(time.FixedZone("test", 7200)).Format("2006-01-02")
	requireStatus(t, packHTTP(f, "POST", "devices/"+id+"/decommission", "retire-physical-data", in), 200)
	after := assessment(t, f, pack.ID)
	if after.Status != "blocked" || after.Devices[0].Status != "blocked" || after.Check == nil || after.Check.Current || after.Basis == before.Basis || after.Material.Basis != before.Material.Basis {
		t.Fatal(after)
	}
	newProof := physicalInput(after, id, true)
	requireStatus(t, packHTTP(f, "POST", "packlists/"+pack.ID+"/device-checks", "retire-positive-proof", newProof), 201)
	current := assessment(t, f, pack.ID)
	if current.Status != "blocked" || current.Check == nil || !current.Check.Current {
		t.Fatal("positive physical proof cleared retirement", current)
	}
	replay := packHTTP(f, "POST", "packlists/"+pack.ID+"/device-checks", "retire-physical-proof", physical)
	requireStatus(t, replay, 201)
	if replay.Body.String() != first.Body.String() {
		t.Fatal("old physical proof changed")
	}
}
