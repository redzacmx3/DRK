package app

import (
	"encoding/json"
	"strings"
	"testing"
)

func incidentInput(version int) deviceControlInput {
	return deviceControlInput{ExpectedVersion: version, PerformedOn: "2024-02-01", Evidence: "Synthetic incident reference", Note: "Synthetic incident capture", Incident: &deviceIncident{"suspected_serious_incident", "Synthetic malfunction", "Unknown consequences; protected case SYNTH-1"}}
}
func reportInput(version, target int, status string) deviceControlInput {
	in := controlProof()
	in.ExpectedVersion = version
	in.Report = &incidentReport{IncidentVersion: target, Status: status, Responsible: "Synthetic responsible function"}
	if status == "CLOSED" {
		in.Report.AssessmentReference = "Synthetic assessment decision reference"
	}
	return in
}
func reportedInput(version, target int) deviceControlInput {
	in := reportInput(version, target, "REPORTED")
	in.Report.Authority = "Synthetic authority"
	in.Report.AuthorityReference = "SYNTH-A-1"
	in.Report.AuthorityReportedAt = "2024-02-02T12:00:00+01:00"
	return in
}
func TestIncidentAndReportingSeparateBlocksAndImmutableHistory(t *testing.T) {
	f, id, doc := documentFixture(t)
	in := incidentInput(0)
	in.DocumentID = doc.ID
	first := packHTTP(f, "POST", "devices/"+id+"/incidents", "incident-first0001", in)
	requireStatus(t, first, 200)
	var record deviceRecord
	json.Unmarshal(first.Body.Bytes(), &record)
	if len(record.Incidents) != 1 || record.Incidents[0].Report.Status != "ASSESSMENT_REQUIRED" || len(record.ActiveBlocks) != 1 || record.ActiveBlocks[0].Reason != "quarantine" || record.Incidents[0].DocumentSHA256 != doc.SHA256 {
		t.Fatal(record)
	}
	onlyManufacturer := reportInput(1, 1, "REPORTED")
	onlyManufacturer.Report.ManufacturerContact = "Synthetic manufacturer"
	onlyManufacturer.Report.ManufacturerReference = "SYNTH-M-1"
	onlyManufacturer.Report.ManufacturerReportedAt = "2024-02-02T12:00:00+01:00"
	requireStatus(t, packHTTP(f, "POST", "devices/"+id+"/reports", "incident-onlymaker", onlyManufacturer), 400)
	pending := reportInput(1, 1, "REPORT_PENDING")
	pending.Report.ManufacturerContact = onlyManufacturer.Report.ManufacturerContact
	pending.Report.ManufacturerReference = onlyManufacturer.Report.ManufacturerReference
	pending.Report.ManufacturerReportedAt = onlyManufacturer.Report.ManufacturerReportedAt
	requireStatus(t, packHTTP(f, "POST", "devices/"+id+"/reports", "incident-pending01", pending), 200)
	reported := reportedInput(2, 1)
	reported.DocumentID = doc.ID
	reported.Report.ManufacturerContact = pending.Report.ManufacturerContact
	reported.Report.ManufacturerReference = pending.Report.ManufacturerReference
	reported.Report.ManufacturerReportedAt = pending.Report.ManufacturerReportedAt
	posted := packHTTP(f, "POST", "devices/"+id+"/reports", "incident-reported1", reported)
	requireStatus(t, posted, 200)
	closed := reported
	closed.Report = &incidentReport{}
	*closed.Report = *reported.Report
	closed.ExpectedVersion = 3
	closed.Report.Status = "CLOSED"
	closed.Report.AssessmentReference = "Synthetic closure review reference"
	requireStatus(t, packHTTP(f, "POST", "devices/"+id+"/reports", "incident-closed001", closed), 200)
	book, files := bookArchive(t, f, id)
	if len(book.Device.Reports) != 3 || book.Device.Incidents[0].Report.Status != "CLOSED" || len(book.Device.ActiveBlocks) != 1 || !strings.Contains(string(files["geraeteakte.html"]), "SYNTH-A-1") {
		t.Fatal(book)
	}
	if book.Controls[0].Action != "incident" || !strings.Contains(string(book.Controls[0].Proof), "Synthetic malfunction") {
		t.Fatal("original incident changed")
	}
	release := controlProof()
	release.ExpectedVersion = 4
	release.BlockID = record.ActiveBlocks[0].ID
	requireStatus(t, packHTTP(f, "POST", "devices/"+id+"/releases", "incident-release01", release), 200)
	replay := packHTTP(f, "POST", "devices/"+id+"/reports", "incident-reported1", reported)
	requireStatus(t, replay, 200)
	if replay.Body.String() != posted.Body.String() {
		t.Fatal("exact report retry changed")
	}
	requireStatus(t, packHTTP(f, "POST", "devices/"+id+"/incidents", "incident-first0001", in), 200)
	book, _ = bookArchive(t, f, id)
	if book.Device.Version != 5 || len(book.Device.ActiveBlocks) != 0 || book.Device.Incidents[0].Report.Status != "CLOSED" {
		t.Fatal("replay reset lifecycle or block")
	}
	dropped := reportInput(5, 1, "FOLLOW_UP")
	requireStatus(t, packHTTP(f, "POST", "devices/"+id+"/reports", "incident-droprefs1", dropped), 400)
	requireStatus(t, packHTTP(f, "POST", "devices/"+id+"/corrections", "incident-correction", correctionInput(5, 1)), 200)
}
func TestIncidentHelperRightsValidationAndRollback(t *testing.T) {
	f, id := controlFixture(t)
	if err := SetSiteRole(f.db, "viewer", []string{demoSite}, "helper"); err != nil {
		t.Fatal(err)
	}
	f.cookie, f.csrf = loginTest(t, f.handler, nil)
	in := incidentInput(0)
	in.Incident.Kind = "repeated_operating_error"
	in.Incident.Description = "<svg onload=alert(1)> synthetic error"
	if _, err := f.db.Exec(`CREATE TRIGGER reject_incident_outbox BEFORE INSERT ON device_control_outbox BEGIN SELECT RAISE(ABORT,'rollback'); END`); err != nil {
		t.Fatal(err)
	}
	requireStatus(t, packHTTP(f, "POST", "devices/"+id+"/incidents", "incident-atomic01", in), 503)
	var count int
	f.db.QueryRow(`SELECT count(*) FROM device_control_events WHERE device_id=?`, id).Scan(&count)
	if count != 0 {
		t.Fatal("partial incident")
	}
	f.db.Exec(`DROP TRIGGER reject_incident_outbox`)
	requireStatus(t, packHTTP(f, "POST", "devices/"+id+"/incidents", "incident-atomic01", in), 200)
	requireStatus(t, packHTTP(f, "POST", "devices/"+id+"/reports", "incident-helperdeny", reportInput(1, 1, "CLOSED")), 404)
	book, files := bookArchive(t, f, id)
	if len(book.Device.Incidents) != 1 || strings.Contains(string(files["geraeteakte.html"]), "<svg") {
		t.Fatal(book)
	}
	if err := SetSiteRole(f.db, "viewer", []string{demoSite}, "device_manager"); err != nil {
		t.Fatal(err)
	}
	f.cookie, f.csrf = loginTest(t, f.handler, nil)
	for n, mutate := range []func(*deviceControlInput){func(i *deviceControlInput) { i.Report.Status = "unknown" }, func(i *deviceControlInput) { i.Report.IncidentVersion = 999 }, func(i *deviceControlInput) { i.Report.Responsible = "" }, func(i *deviceControlInput) { i.Report.AuthorityReportedAt = "2099-01-01T12:00:00Z" }, func(i *deviceControlInput) { i.Report.AuthorityReportedAt = "2024-01-01T12:00:00Z" }, func(i *deviceControlInput) { i.Report.AuthorityReportedAt = "2024-02-02T12:00:00" }, func(i *deviceControlInput) { i.Report.AuthorityReference = "" }, func(i *deviceControlInput) { i.Incident = in.Incident }, func(i *deviceControlInput) { i.DocumentID = "foreign-document" }, func(i *deviceControlInput) { i.PerformedOn = "2024-02-02" }} {
		p := reportedInput(1, 1)
		mutate(&p)
		requireStatus(t, packHTTP(f, "POST", "devices/"+id+"/reports", "report-invalid-"+string(rune('a'+n)), p), 400)
	}
	for n, mutate := range []func(*deviceControlInput){func(i *deviceControlInput) { i.Incident.Kind = "unknown" }, func(i *deviceControlInput) { i.Incident.Description = "" }, func(i *deviceControlInput) { i.Incident.Consequences = "" }, func(i *deviceControlInput) { i.PerformedOn = "2099-01-01" }, func(i *deviceControlInput) { i.PerformedOn = "2024-02-30" }, func(i *deviceControlInput) { i.Qualification = "mixed" }, func(i *deviceControlInput) { i.Incident.Description = "bad\x00text" }} {
		p := incidentInput(1)
		mutate(&p)
		requireStatus(t, packHTTP(f, "POST", "devices/"+id+"/incidents", "incident-invalid-"+string(rune('a'+n)), p), 400)
	}
	if _, err := f.db.Exec(`CREATE TRIGGER reject_report_outbox BEFORE INSERT ON device_control_outbox BEGIN SELECT RAISE(ABORT,'rollback'); END`); err != nil {
		t.Fatal(err)
	}
	requireStatus(t, packHTTP(f, "POST", "devices/"+id+"/reports", "incident-noreport01", reportInput(1, 1, "CLOSED")), 503)
	unchanged, _ := bookArchive(t, f, id)
	if unchanged.Device.Incidents[0].Report.Status != "ASSESSMENT_REQUIRED" || len(unchanged.Device.Reports) != 0 {
		t.Fatal("partial report update")
	}
	f.db.Exec(`DROP TRIGGER reject_report_outbox`)
	incomplete := reportInput(1, 1, "CLOSED")
	incomplete.Report.AssessmentReference = ""
	requireStatus(t, packHTTP(f, "POST", "devices/"+id+"/reports", "incident-noassess01", incomplete), 400)
	// Closing a reviewed nonreportable incident is an explicit evidenced decision, not a manufactured authority report.
	requireStatus(t, packHTTP(f, "POST", "devices/"+id+"/reports", "incident-noreport01", reportInput(1, 1, "CLOSED")), 200)
}
func TestPendingIncidentAssessmentIndependentOfReleasedBlock(t *testing.T) {
	f, id := packDeviceFixture(t)
	in := packInput(warehouse)
	in.Devices = []packDevice{{DeviceID: id}}
	pack := published(t, f, in, "incident-physical01")
	before := assessment(t, f, pack.ID)
	physical := physicalInput(before, id, true)
	first := packHTTP(f, "POST", "packlists/"+pack.ID+"/device-checks", "incident-physproof1", physical)
	requireStatus(t, first, 201)
	requireStatus(t, packHTTP(f, "POST", "devices/"+id+"/incidents", "incident-forpack01", incidentInput(2)), 200)
	after := assessment(t, f, pack.ID)
	if after.Status != "blocked" || after.Basis == before.Basis || after.Material.Basis != before.Material.Basis || after.Check == nil || after.Check.Current {
		t.Fatal(after)
	}
	release := controlProof()
	release.ExpectedVersion = 3
	release.BlockID = after.Devices[0].Device.ActiveBlocks[0].ID
	requireStatus(t, packHTTP(f, "POST", "devices/"+id+"/releases", "incident-packrelease", release), 200)
	after = assessment(t, f, pack.ID)
	if after.Devices[0].Status != "blocked" || len(after.Devices[0].Device.ActiveBlocks) != 0 {
		t.Fatal("pending assessment erased by block release")
	}
	requireStatus(t, packHTTP(f, "POST", "devices/"+id+"/reports", "incident-packclosed", reportInput(4, 3, "CLOSED")), 200)
	after = assessment(t, f, pack.ID)
	if after.Material.Basis != before.Material.Basis || after.Check.Current {
		t.Fatal(after)
	}
	replay := packHTTP(f, "POST", "packlists/"+pack.ID+"/device-checks", "incident-physproof1", physical)
	if replay.Body.String() != first.Body.String() {
		t.Fatal("old physical proof changed")
	}
}
