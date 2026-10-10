package app

import (
	"encoding/csv"
	"encoding/json"
	"strings"
	"testing"
)

func correctionInput(version, target int) deviceControlInput {
	in := controlProof()
	in.ExpectedVersion = version
	in.Correction = &deviceCorrection{target, "Synthetic corrected examiner reference"}
	return in
}
func TestDeviceCorrectionOriginBindingBlocksAndExactReplay(t *testing.T) {
	f, id, doc := documentFixture(t)
	original := maintenanceInput(0, "failed")
	original.DocumentID = doc.ID
	first := packHTTP(f, "POST", "devices/"+id+"/maintenance", "correction-original1", original)
	requireStatus(t, first, 200)
	var sourceID, payload, actor, created string
	f.db.QueryRow(`SELECT id,payload_json,actor_id,created_at FROM device_control_events WHERE device_id=? AND version=1`, id).Scan(&sourceID, &payload, &actor, &created)
	in := correctionInput(1, 1)
	in.DocumentID = doc.ID
	in.Correction.Statement = "<svg onload=alert(1)> corrected executor\nZusätzliche Erläuterung"
	corrected := packHTTP(f, "POST", "devices/"+id+"/corrections", "correction-first001", in)
	requireStatus(t, corrected, 200)
	book, files := bookArchive(t, f, id)
	if len(book.Device.Corrections) != 1 || len(book.Device.ActiveBlocks) != 2 || book.Device.ActiveBlocks[0].Reason != "defect" || book.Device.ActiveBlocks[1].Reason != "quarantine" || book.Device.Maintenances[0].Result != "failed" {
		t.Fatal(book)
	}
	proof := book.Device.Corrections[0]
	if proof.Target.ID != sourceID || proof.Target.PayloadSHA256 != digest(payload) || proof.Target.ActorID != actor || proof.Target.CreatedAt != created || proof.DocumentSHA256 != doc.SHA256 {
		t.Fatal(proof)
	}
	if strings.Contains(string(files["geraeteakte.html"]), "<svg") || !strings.Contains(string(files["geraeteakte.html"]), "&lt;svg") || !strings.Contains(string(files["geraeteakte.html"]), digest(payload)) {
		t.Fatal("unsafe or incomplete archive")
	}
	var after string
	f.db.QueryRow(`SELECT payload_json FROM device_control_events WHERE id=?`, sourceID).Scan(&after)
	if after != payload {
		t.Fatal("original rewritten")
	}
	requireStatus(t, packHTTP(f, "POST", "devices/"+id+"/corrections", "correction-stale001", in), 409)
	release := controlProof()
	release.ExpectedVersion = 2
	release.BlockID = book.Device.ActiveBlocks[1].ID
	requireStatus(t, packHTTP(f, "POST", "devices/"+id+"/releases", "correction-release1", release), 200)
	replay := packHTTP(f, "POST", "devices/"+id+"/corrections", "correction-first001", in)
	requireStatus(t, replay, 200)
	if replay.Body.String() != corrected.Body.String() {
		t.Fatal("exact response changed")
	}
	book, _ = bookArchive(t, f, id)
	if len(book.Device.ActiveBlocks) != 1 || book.Device.Version != 3 {
		t.Fatal("replay recreated quarantine")
	}
	requireStatus(t, packHTTP(f, "POST", "devices/"+id+"/corrections", "correction-second01", correctionInput(3, 1)), 200)
	book, _ = bookArchive(t, f, id)
	if len(book.Device.Corrections) != 2 || len(book.Device.ActiveBlocks) != 2 {
		t.Fatal("additional correction missing")
	}
	csvResponse := packHTTP(f, "GET", "devices/export.csv", "", nil)
	requireStatus(t, csvResponse, 200)
	reader := csv.NewReader(strings.NewReader(strings.TrimPrefix(csvResponse.Body.String(), "\xef\xbb\xbf")))
	reader.Comma = ';'
	rows, err := reader.ReadAll()
	if err != nil || rows[1][32] != "2" {
		t.Fatal(rows, err)
	}
	requireStatus(t, packHTTP(f, "POST", "devices/"+id+"/maintenance", "correction-original1", original), 200)
	if err := SetSiteRole(f.db, "viewer", []string{demoSite}, "helper"); err != nil {
		t.Fatal(err)
	}
	f.cookie, f.csrf = loginTest(t, f.handler, nil)
	requireStatus(t, packHTTP(f, "POST", "devices/"+id+"/corrections", "correction-first001", in), 404)
}
func TestDeviceCorrectionValidationRollbackAndRetiredDevice(t *testing.T) {
	f, id := controlFixture(t)
	requireStatus(t, packHTTP(f, "POST", "devices/"+id+"/maintenance", "correction-base001", maintenanceInput(0, "completed")), 200)
	for n, mutate := range []func(*deviceControlInput){func(i *deviceControlInput) { i.Correction = nil }, func(i *deviceControlInput) { i.Correction.TargetVersion = 0 }, func(i *deviceControlInput) { i.Correction.TargetVersion = 999 }, func(i *deviceControlInput) { i.Correction.Statement = "" }, func(i *deviceControlInput) { i.Correction.Statement = strings.Repeat("x", 2001) }, func(i *deviceControlInput) { i.Correction.Statement = "bad\x00text" }, func(i *deviceControlInput) { i.PerformedOn = "2024-02-01" }, func(i *deviceControlInput) { i.Result = "passed" }, func(i *deviceControlInput) { i.MasterData = masterInput(1).MasterData }, func(i *deviceControlInput) { i.DocumentID = "foreign-document" }} {
		in := correctionInput(1, 1)
		mutate(&in)
		requireStatus(t, packHTTP(f, "POST", "devices/"+id+"/corrections", "correction-invalid-"+string(rune('a'+n)), in), 400)
	}
	// Do not silently accept correction fields in unrelated actions.
	requireStatus(t, packHTTP(f, "POST", "devices/"+id+"/decommission", "correction-mixed001", correctionInput(1, 1)), 400)
	if _, err := f.db.Exec(`CREATE TRIGGER reject_correction_outbox BEFORE INSERT ON device_control_outbox BEGIN SELECT RAISE(ABORT,'rollback'); END`); err != nil {
		t.Fatal(err)
	}
	in := correctionInput(1, 1)
	requireStatus(t, packHTTP(f, "POST", "devices/"+id+"/corrections", "correction-atomic01", in), 503)
	var count int
	f.db.QueryRow(`SELECT count(*) FROM device_control_events WHERE device_id=?`, id).Scan(&count)
	if count != 1 {
		t.Fatal("partial correction")
	}
	f.db.Exec(`DROP TRIGGER reject_correction_outbox`)
	requireStatus(t, packHTTP(f, "POST", "devices/"+id+"/corrections", "correction-atomic01", in), 200)
	requireStatus(t, packHTTP(f, "POST", "devices/"+id+"/corrections", "correction-of-corr1", correctionInput(2, 2)), 400)
	requireStatus(t, packHTTP(f, "POST", "devices/"+id+"/master-data", "correction-master01", masterInput(2)), 200)
	requireStatus(t, packHTTP(f, "POST", "devices/"+id+"/corrections", "correction-of-meta1", correctionInput(3, 3)), 400)
	requireStatus(t, packHTTP(f, "POST", "devices/"+id+"/decommission", "correction-retired1", decommissionInput(3)), 200)
	requireStatus(t, packHTTP(f, "POST", "devices/"+id+"/corrections", "correction-afterret", correctionInput(4, 4)), 200)
	book, _ := bookArchive(t, f, id)
	if book.Device.Lifecycle != "DECOMMISSIONED" || book.Device.Decommission.RetainThrough != "2029-03-01" {
		t.Fatal("correction reactivated retirement")
	}
}
func TestDeviceCorrectionInvalidatesOnlyPhysicalDeviceBasis(t *testing.T) {
	f, id := packDeviceFixture(t)
	in := packInput(warehouse)
	in.Devices = []packDevice{{DeviceID: id}}
	pack := published(t, f, in, "correction-pack001")
	before := assessment(t, f, pack.ID)
	physical := physicalInput(before, id, true)
	first := packHTTP(f, "POST", "packlists/"+pack.ID+"/device-checks", "correction-phys001", physical)
	requireStatus(t, first, 201)
	requireStatus(t, packHTTP(f, "POST", "devices/"+id+"/corrections", "correction-physical", correctionInput(2, 1)), 200)
	after := assessment(t, f, pack.ID)
	if after.Basis == before.Basis || after.Material.Basis != before.Material.Basis || after.Check == nil || after.Check.Current || after.Devices[0].Status != "blocked" {
		t.Fatal(after)
	}
	var record deviceRecord
	json.Unmarshal(packHTTP(f, "GET", "devices/"+id+"/book", "", nil).Body.Bytes(), &record)
	if record.CommissionedOn != before.Devices[0].Device.CommissionedOn || record.Controls["STK"] != before.Devices[0].Device.Controls["STK"] {
		t.Fatal("correction changed technical facts")
	}
	replay := packHTTP(f, "POST", "packlists/"+pack.ID+"/device-checks", "correction-phys001", physical)
	if replay.Body.String() != first.Body.String() {
		t.Fatal("old physical proof changed")
	}
}
