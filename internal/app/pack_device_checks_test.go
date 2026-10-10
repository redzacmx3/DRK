package app

import (
	"encoding/json"
	"testing"
	"time"
)

func physicalInput(result packAssessment, id string, passed bool) deviceCheckRequest {
	return deviceCheckRequest{Basis: result.Basis, Results: []devicePhysicalResult{{DeviceID: id, Present: &passed, Functional: &passed, AccessoriesComplete: &passed}}, Note: "Am Ort Funktion und zugeordnetes Zubehör geprüft"}
}
func TestPhysicalDevicesReplayStalenessAndBlocks(t *testing.T) {
	f, id := packDeviceFixture(t)
	defer f.db.Close()
	input := packInput(warehouse)
	input.Devices = []packDevice{{DeviceID: id, Accessories: []packAccessory{{ProductID: input.Items[0].ProductID, Quantity: 2}}}}
	p := published(t, f, input, "physical-devices-pack")
	before := assessment(t, f, p.ID)
	request := physicalInput(before, id, true)
	path := "packlists/" + p.ID + "/device-checks"
	first := packHTTP(f, "POST", path, "physical-devices-0001", request)
	requireStatus(t, first, 201)
	after := assessment(t, f, p.ID)
	if after.Check == nil || !after.Check.Current || !after.Check.Passed || after.Status != "blocked" || after.Overall != "not_assessed" {
		t.Fatal("physical proof erased overdue control", after)
	}
	proof := controlProof()
	proof.ExpectedVersion = 2
	proof.BlockReason = "defect"
	deviceResponse(t, f, id, "defects", "physical-device-defect", proof)
	after = assessment(t, f, p.ID)
	if after.Check.Current || after.Basis == before.Basis {
		t.Fatal("device version did not invalidate", after)
	}
	replay := packHTTP(f, "POST", path, "physical-devices-0001", request)
	requireStatus(t, replay, 201)
	if replay.Body.String() != first.Body.String() {
		t.Fatal("replay changed")
	}
	requireStatus(t, packHTTP(f, "POST", path, "physical-devices-stale", request), 409)
	request = physicalInput(after, id, false)
	failed := packHTTP(f, "POST", path, "physical-devices-failed", request)
	requireStatus(t, failed, 201)
	after = assessment(t, f, p.ID)
	if !after.Check.Current || after.Check.Passed || after.Status != "blocked" {
		t.Fatal(after)
	}
	tx, err := f.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	future := time.Now().Add(25 * time.Hour)
	atFuture, err := assessPack(t.Context(), tx, f.user, p, future)
	tx.Rollback()
	if err != nil {
		t.Fatal(err)
	}
	if atFuture.Check.Current {
		t.Fatal("expired proof current")
	}
	requireStatus(t, packHTTP(f, "GET", path, "", nil), 200)
	if _, err = f.db.Exec(`UPDATE pack_device_checks SET note='changed'`); err == nil {
		t.Fatal("mutable proof")
	}
	var count int
	f.db.QueryRow(`SELECT COUNT(*) FROM pack_device_checks`).Scan(&count)
	if count != 2 {
		t.Fatal(count)
	}
	// Failed final outbox insertion must roll back the proof and key.
	f.db.Exec(`CREATE TRIGGER physical_outbox_failure BEFORE INSERT ON pack_device_check_outbox BEGIN SELECT RAISE(ABORT,'outbox unavailable'); END`)
	request = physicalInput(after, id, true)
	requireStatus(t, packHTTP(f, "POST", path, "physical-outbox-fail", request), 503)
	f.db.QueryRow(`SELECT COUNT(*) FROM pack_device_checks`).Scan(&count)
	if count != 2 {
		t.Fatal("partial check committed")
	}
}
func TestPhysicalDevicesRequireCompleteExplicitResultsAndScopedRole(t *testing.T) {
	f, id := packDeviceFixture(t)
	defer f.db.Close()
	input := packInput(warehouse)
	input.Devices = []packDevice{{DeviceID: id}}
	p := published(t, f, input, "physical-validation-pack")
	a := assessment(t, f, p.ID)
	request := physicalInput(a, id, true)
	path := "packlists/" + p.ID + "/device-checks"
	request.Results[0].Functional = nil
	requireStatus(t, packHTTP(f, "POST", path, "physical-missing-result", request), 400)
	request = physicalInput(a, "other-device", true)
	requireStatus(t, packHTTP(f, "POST", path, "physical-other-device", request), 400)
	request = physicalInput(a, id, true)
	request.Results = append(request.Results, request.Results[0])
	requireStatus(t, packHTTP(f, "POST", path, "physical-duplicate-device", request), 400)
	requireStatus(t, materialCheck(t, f, p.ID, "physical-cross-module", readiness(t, f, p.ID).Basis, true), 201)
	request = physicalInput(a, id, true)
	requireStatus(t, packHTTP(f, "POST", path, "physical-cross-module", request), 409)
	if err := SetSiteRole(f.db, "viewer", []string{demoSite}, "viewer"); err != nil {
		t.Fatal(err)
	}
	f.cookie, f.csrf = loginTest(t, f.handler, nil)
	requireStatus(t, packHTTP(f, "POST", path, "physical-viewer-denied", request), 404)
	if err := SetSiteRole(f.db, "viewer", nil, "viewer"); err != nil {
		t.Fatal(err)
	}
	f.cookie, f.csrf = loginTest(t, f.handler, nil)
	requireStatus(t, packHTTP(f, "GET", path, "", nil), 404)
}
func TestPhysicalDevicePassedRemainsSeparateFromOverallReadiness(t *testing.T) {
	f, id := packDeviceFixture(t)
	defer f.db.Close()
	proof := controlProof()
	proof.ExpectedVersion = 2
	proof.ControlType = "STK"
	zone, _ := time.LoadLocation("Europe/Berlin")
	proof.PerformedOn = time.Now().In(zone).Format("2006-01-02")
	proof.Examiner = "Examiner"
	proof.Method = "Method"
	proof.Result = "passed"
	deviceResponse(t, f, id, "inspections", "physical-current-stk", proof)
	proof.ExpectedVersion = 3
	proof.ControlType = "MTK"
	deviceResponse(t, f, id, "inspections", "physical-current-mtk", proof)
	input := packInput(warehouse)
	input.Devices = []packDevice{{DeviceID: id}}
	p := published(t, f, input, "physical-current-pack")
	a := assessment(t, f, p.ID)
	requireStatus(t, packHTTP(f, "POST", "packlists/"+p.ID+"/device-checks", "physical-current-pass", physicalInput(a, id, true)), 201)
	a = assessment(t, f, p.ID)
	if a.Status != "physically_checked" || a.Overall != "not_assessed" || a.Material.Status != "needs_check" {
		t.Fatal(a)
	}
	// Material movements invalidate device/accessory proof even when later reversed.
	requireStatus(t, stockHTTP(f, "receipts", "physical-basis-movement", stockRequest{BatchID: demoBatch, To: warehouse, Quantity: 1}), 201)
	a = assessment(t, f, p.ID)
	if a.Check.Current || a.Status != "needs_device_check" {
		t.Fatal(a)
	}
	b := packHTTP(f, "GET", "packlists/"+p.ID+"/device-checks", "", nil)
	requireStatus(t, b, 200)
	var history []deviceCheckProof
	if err := json.Unmarshal(b.Body.Bytes(), &history); err != nil || len(history) != 1 {
		t.Fatal(err, history)
	}
}
