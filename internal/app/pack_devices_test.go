package app

import (
	"encoding/json"
	"testing"
	"time"
)

func packDeviceFixture(t *testing.T) (stockFixture, string) {
	f, id := inspectedFixture(t)
	if _, err := f.db.Exec(`INSERT INTO role_assignments VALUES(?,?,'material_manager')`, f.user, demoSite); err != nil {
		t.Fatal(err)
	}
	requireStatus(t, stockHTTP(f, "receipts", "pack-device-receipt", stockRequest{BatchID: demoBatch, To: warehouse, Quantity: 5}), 201)
	return f, id
}
func assessment(t *testing.T, f stockFixture, id string) packAssessment {
	t.Helper()
	w := packHTTP(f, "GET", "packlists/"+id+"/assessment", "", nil)
	requireStatus(t, w, 200)
	var result packAssessment
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	return result
}
func TestPackDeviceAssessmentCannotOverrideBlocksOrMaterialProof(t *testing.T) {
	f, id := packDeviceFixture(t)
	input := packInput(warehouse)
	input.Devices = []packDevice{{DeviceID: id, Accessories: []packAccessory{{ProductID: input.Items[0].ProductID, Quantity: 2}}}}
	pack := published(t, f, input, "pack-with-device-0001")
	material := readiness(t, f, pack.ID)
	requireStatus(t, materialCheck(t, f, pack.ID, "physical-material-0001", material.Basis, true), 201)
	result := assessment(t, f, pack.ID)
	if result.Material.Status != "ready" || result.Status != "blocked" || result.Overall != "not_assessed" || len(result.Devices) != 1 {
		t.Fatal(result)
	}
	oldBasis := result.Material.Basis
	proof := controlProof()
	proof.ExpectedVersion = 2
	proof.BlockReason = "defect"
	deviceResponse(t, f, id, "defects", "device-pack-defect-0001", proof)
	result = assessment(t, f, pack.ID)
	if result.Status != "blocked" || result.Material.Basis != oldBasis || result.Material.Check == nil || !result.Material.Check.Current {
		t.Fatal("device change altered material-only proof", result)
	}
	proof = controlProof()
	proof.ExpectedVersion = 3
	proof.ControlType = "STK"
	tz, _ := time.LoadLocation("Europe/Berlin")
	proof.PerformedOn = time.Now().In(tz).Format("2006-01-02")
	proof.Examiner = "Examiner"
	proof.Method = "Method"
	proof.Result = "passed"
	deviceResponse(t, f, id, "inspections", "device-pack-stk-pass", proof)
	proof.ExpectedVersion = 4
	proof.ControlType = "MTK"
	deviceResponse(t, f, id, "inspections", "device-pack-mtk-pass", proof)
	result = assessment(t, f, pack.ID)
	if result.Status != "blocked" || len(result.Devices[0].Device.ActiveBlocks) != 1 {
		t.Fatal("passed inspections erased defect", result)
	}
	release := controlProof()
	release.ExpectedVersion = 5
	release.BlockID = result.Devices[0].Device.ActiveBlocks[0].ID
	deviceResponse(t, f, id, "releases", "release-pack-defect", release)
	result = assessment(t, f, pack.ID)
	if result.Status != "needs_device_check" || result.Overall != "not_assessed" || result.Material.Status != "ready" {
		t.Fatal(result)
	}
	replay := packHTTP(f, "POST", "packlists", "pack-with-device-0001", input)
	requireStatus(t, replay, 201)
	var replayPack packVersion
	json.Unmarshal(replay.Body.Bytes(), &replayPack)
	if replayPack.ID != pack.ID || len(replayPack.Devices) != 1 {
		t.Fatal(replayPack)
	}
	if _, err := f.db.Exec(`UPDATE packlist_devices SET accessories_json='[]'`); err == nil {
		t.Fatal("mutable device requirements")
	}
	if err := SetSiteRole(f.db, "viewer", nil, "viewer"); err != nil {
		t.Fatal(err)
	}
	f.cookie, f.csrf = loginTest(t, f.handler, nil)
	requireStatus(t, packHTTP(f, "GET", "packlists/"+pack.ID+"/assessment", "", nil), 404)
}
func TestPackAccessoriesCannotReuseOrExceedMaterialTarget(t *testing.T) {
	input := packInput(warehouse)
	input.Devices = []packDevice{{DeviceID: "device-1", Accessories: []packAccessory{{ProductID: input.Items[0].ProductID, Quantity: 3}}}, {DeviceID: "device-2", Accessories: []packAccessory{{ProductID: input.Items[0].ProductID, Quantity: 3}}}}
	if err := validatePackDevices(&input); err == nil {
		t.Fatal("shared target counted twice")
	}
	input.Devices[1].Accessories[0].Quantity = 2
	if err := validatePackDevices(&input); err != nil {
		t.Fatal(err)
	}
	input.Devices[1].DeviceID = input.Devices[0].DeviceID
	if err := validatePackDevices(&input); err == nil {
		t.Fatal("duplicate device")
	}
	f, id := packDeviceFixture(t)
	input = packInput(kit)
	input.Devices = []packDevice{{DeviceID: id}}
	requireStatus(t, packHTTP(f, "POST", "packlists", "wrong-location-device", input), 404)
	var count int
	f.db.QueryRow(`SELECT COUNT(*) FROM packlist_versions`).Scan(&count)
	if count != 0 {
		t.Fatal("partial pack committed", count)
	}
	input = packInput(warehouse)
	input.Devices = []packDevice{{DeviceID: id}}
	if _, err := f.db.Exec(`CREATE TRIGGER reject_device_pack_outbox BEFORE INSERT ON packlist_outbox BEGIN SELECT RAISE(ABORT,'rollback'); END`); err != nil {
		t.Fatal(err)
	}
	requireStatus(t, packHTTP(f, "POST", "packlists", "atomic-device-pack-write", input), 503)
	f.db.QueryRow(`SELECT COUNT(*) FROM packlist_devices`).Scan(&count)
	if count != 0 {
		t.Fatal("partial requirement committed", count)
	}
}
func TestLegacyMaterialPackAssessmentRemainsUnassessed(t *testing.T) {
	f := stockDB(t)
	pack := published(t, f, packInput(kit), "legacy-pack-assessment")
	result := assessment(t, f, pack.ID)
	if result.Overall != "not_assessed" || len(result.Devices) != 0 {
		t.Fatal(result)
	}
	if pack.Devices != nil {
		t.Fatal("legacy definition altered")
	}
}
