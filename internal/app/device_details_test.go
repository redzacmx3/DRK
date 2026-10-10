package app

import (
	"encoding/json"
	"strings"
	"testing"
)

func detailsInput(version int) deviceControlInput {
	return deviceControlInput{ExpectedVersion: version, Evidence: "Synthetic nameplate check", Note: "Synthetic correction", Details: &deviceDetails{"Corrected demo", "Maker", "Model", "000123"}}
}
func TestDeviceDetailsVersionHistoryImmutableRegistrationAndReplay(t *testing.T) {
	f, id := controlFixture(t)
	defer f.db.Close()
	input := detailsInput(0)
	first := packHTTP(f, "POST", "devices/"+id+"/details", "device-details-first1", input)
	requireStatus(t, first, 200)
	var record deviceRecord
	json.Unmarshal(first.Body.Bytes(), &record)
	if record.Version != 1 || record.Manufacturer != "Maker" || record.SerialOrLot != "000123" || record.Lifecycle != "NOT_COMMISSIONED" || len(record.ActiveBlocks) != 0 {
		t.Fatal(record)
	}
	var name string
	f.db.QueryRow(`SELECT name FROM devices WHERE id=?`, id).Scan(&name)
	if name != "Demo" {
		t.Fatal("registration overwritten")
	}
	replay := packHTTP(f, "POST", "devices/"+id+"/details", "device-details-first1", input)
	requireStatus(t, replay, 200)
	if replay.Body.String() != first.Body.String() {
		t.Fatal("replay changed")
	}
	requireStatus(t, packHTTP(f, "POST", "devices/"+id+"/details", "device-details-stale1", input), 409)
	input.ExpectedVersion = 1
	requireStatus(t, packHTTP(f, "POST", "devices/"+id+"/details", "device-details-empty1", input), 409)
	input.Details.Name = "Second name"
	requireStatus(t, packHTTP(f, "POST", "devices/"+id+"/details", "device-details-second", input), 200)
	history := packHTTP(f, "GET", "devices/"+id+"/events", "", nil)
	requireStatus(t, history, 200)
	var events []struct {
		Action string
		Proof  struct {
			Before deviceDetails `json:"before_details"`
			After  deviceDetails `json:"details"`
		}
	}
	json.Unmarshal(history.Body.Bytes(), &events)
	if len(events) != 2 || events[0].Proof.Before.Name != "Corrected demo" || events[0].Proof.After.Name != "Second name" || events[1].Proof.Before.Name != "Demo" {
		t.Fatal(events)
	}
	list := packHTTP(f, "GET", "devices", "", nil)
	requireStatus(t, list, 200)
	var records []deviceRecord
	json.Unmarshal(list.Body.Bytes(), &records)
	if records[0].Name != "Second name" {
		t.Fatal(records)
	}
	if _, err := f.db.Exec(`UPDATE device_control_events SET payload_json='{}'`); err == nil {
		t.Fatal("history mutable")
	}
}
func TestDeviceDetailsIdentityChangeQuarantinesCommissionedDevice(t *testing.T) {
	f, id := controlFixture(t)
	defer f.db.Close()
	commission := controlProof()
	commission.PerformedOn = "2020-01-01"
	requireStatus(t, packHTTP(f, "POST", "devices/"+id+"/commission", "details-commission01", commission), 200)
	rules := controlRules(1)
	requireStatus(t, packHTTP(f, "POST", "devices/"+id+"/rules", "details-rules-000001", rules), 200)
	input := detailsInput(2)
	r := packHTTP(f, "POST", "devices/"+id+"/details", "details-identity-001", input)
	requireStatus(t, r, 200)
	var record deviceRecord
	json.Unmarshal(r.Body.Bytes(), &record)
	if record.Version != 3 || record.CommissionedOn != "2020-01-01" || len(record.ActiveBlocks) != 1 || record.ActiveBlocks[0].Reason != "quarantine" || record.EffectiveRules["STK"].BasisDate != "2020-01-01" {
		t.Fatal(record)
	}
	block := record.ActiveBlocks[0].ID
	input.ExpectedVersion = 3
	input.Details.Name = "Only a name change"
	r = packHTTP(f, "POST", "devices/"+id+"/details", "details-name-only001", input)
	requireStatus(t, r, 200)
	json.Unmarshal(r.Body.Bytes(), &record)
	if len(record.ActiveBlocks) != 1 || record.ActiveBlocks[0].ID != block {
		t.Fatal("block removed/duplicated", record)
	}
	release := controlProof()
	release.ExpectedVersion = 4
	release.BlockID = block
	requireStatus(t, packHTTP(f, "POST", "devices/"+id+"/releases", "details-release-0001", release), 200)
	replay := packHTTP(f, "POST", "devices/"+id+"/details", "details-identity-001", detailsInput(2))
	requireStatus(t, replay, 200)
	r = packHTTP(f, "GET", "devices/"+id+"/book", "", nil)
	requireStatus(t, r, 200)
	json.Unmarshal(r.Body.Bytes(), &record)
	if len(record.ActiveBlocks) != 0 {
		t.Fatal("replay recreated block")
	}
}
func TestDeviceDetailsImportCompletionDoesNotRevertOnCSVReplay(t *testing.T) {
	f := importFixture(t)
	input := deviceImportRequest{CSV: "Inventarnummer;Bezeichnung;Standort\nDETAIL-CSV;Partial;Demo-Materiallager\n"}
	p := importPreview(t, f, input)
	input.Basis = p.Basis
	r := packHTTP(f, "POST", "device-import/commit", "details-import-0001", input)
	requireStatus(t, r, 201)
	var imported deviceImportResult
	json.Unmarshal(r.Body.Bytes(), &imported)
	id := imported.Devices[0]
	requireStatus(t, packHTTP(f, "POST", "devices/"+id+"/details", "details-complete-001", detailsInput(0)), 200)
	p = importPreview(t, f, input)
	if p.Unchanged != 1 {
		t.Fatal(p)
	}
	input.Basis = p.Basis
	requireStatus(t, packHTTP(f, "POST", "device-import/commit", "details-reimport001", input), 201)
	r = packHTTP(f, "GET", "devices/"+id+"/book", "", nil)
	requireStatus(t, r, 200)
	var record deviceRecord
	json.Unmarshal(r.Body.Bytes(), &record)
	if record.Manufacturer != "Maker" || record.Intake == nil || record.Version != 1 {
		t.Fatal(record)
	}
}
func TestDeviceDetailsValidationScopeAndAtomicFailure(t *testing.T) {
	f, id := controlFixture(t)
	defer f.db.Close()
	input := detailsInput(0)
	invalid := input
	invalid.Details = &deviceDetails{"", "", "", ""}
	requireStatus(t, packHTTP(f, "POST", "devices/"+id+"/details", "details-invalid-001", invalid), 400)
	invalid = input
	invalid.Rules = controlRules(0).Rules
	requireStatus(t, packHTTP(f, "POST", "devices/"+id+"/details", "details-mixed-00001", invalid), 400)
	invalid = input
	invalid.Details = &deviceDetails{strings.Repeat("x", 161), "", "", ""}
	requireStatus(t, packHTTP(f, "POST", "devices/"+id+"/details", "details-long-000001", invalid), 400)
	_, err := f.db.Exec(`CREATE TRIGGER details_fail_outbox BEFORE INSERT ON device_control_outbox BEGIN SELECT RAISE(ABORT,'synthetic failure'); END`)
	if err != nil {
		t.Fatal(err)
	}
	requireStatus(t, packHTTP(f, "POST", "devices/"+id+"/details", "details-rollback001", input), 503)
	var n int
	f.db.QueryRow(`SELECT COUNT(*) FROM device_control_events`).Scan(&n)
	if n != 0 {
		t.Fatal("partial event persisted")
	}
	f.db.Exec(`DROP TRIGGER details_fail_outbox`)
	requireStatus(t, packHTTP(f, "POST", "devices/"+id+"/details", "details-rollback001", input), 200)
	if err = SetSiteRole(f.db, "viewer", []string{demoSite}, "helper"); err != nil {
		t.Fatal(err)
	}
	f.cookie, f.csrf = loginTest(t, f.handler, nil)
	requireStatus(t, packHTTP(f, "POST", "devices/"+id+"/details", "details-helper-0001", input), 404)
	requireStatus(t, packHTTP(f, "POST", "devices/"+id+"/details", "details-rollback001", input), 404)
}

func TestDeviceDetailsInvalidatesPhysicalDeviceProofOnly(t *testing.T) {
	f, id := packDeviceFixture(t)
	defer f.db.Close()
	input := packInput(warehouse)
	input.Devices = []packDevice{{DeviceID: id}}
	pack := published(t, f, input, "details-physical-pack")
	before := assessment(t, f, pack.ID)
	request := physicalInput(before, id, true)
	first := packHTTP(f, "POST", "packlists/"+pack.ID+"/device-checks", "details-physical-001", request)
	requireStatus(t, first, 201)
	requireStatus(t, packHTTP(f, "POST", "devices/"+id+"/details", "details-physical-id1", detailsInput(2)), 200)
	after := assessment(t, f, pack.ID)
	if after.Check == nil || after.Check.Current || after.Basis == before.Basis || after.Material.Basis != before.Material.Basis || len(after.Devices[0].Device.ActiveBlocks) != 1 {
		t.Fatal(after)
	}
	replay := packHTTP(f, "POST", "packlists/"+pack.ID+"/device-checks", "details-physical-001", request)
	requireStatus(t, replay, 201)
	if replay.Body.String() != first.Body.String() {
		t.Fatal("historical proof replay altered")
	}
	requireStatus(t, packHTTP(f, "POST", "packlists/"+pack.ID+"/device-checks", "details-physical-old2", request), 409)
}
