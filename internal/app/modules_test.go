package app

import (
	"context"
	"encoding/json"
	"io"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func modulesSelection(t *testing.T, value string) ModuleConfig {
	t.Helper()
	c, err := ParseModules(value)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func modulesConfigure(t *testing.T, f *stockFixture, value string) int {
	t.Helper()
	c := modulesSelection(t, value)
	revision, err := ConfigureModules(context.Background(), f.db, c)
	if err != nil {
		t.Fatal(err)
	}
	f.handler = HandlerWithModules(f.db, testOrigin, false, c)
	return revision
}

func modulesError(t *testing.T, response *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	requireStatus(t, response, status)
	var result struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil || result.Code != code {
		t.Fatalf("error %q expected %q: %s (%v)", result.Code, code, response.Body.String(), err)
	}
}

func modulesDirectDisabled(t *testing.T, err error) {
	t.Helper()
	e, ok := err.(*stockError)
	if !ok || e.Status != 403 || e.Code != "module_disabled" {
		t.Fatalf("direct operation bypassed inactive module: %v", err)
	}
}

type moduleChangeBody struct {
	io.Reader
	change func()
}

func (body *moduleChangeBody) Read(data []byte) (int, error) {
	if body.change != nil {
		change := body.change
		body.change = nil
		change()
	}
	return body.Reader.Read(data)
}

func TestModulesExplicitSelectionAndDependencyValidation(t *testing.T) {
	for _, value := range []string{"", " ", "all,inventory", "unknown", "devices,devices", "inventory,", "documents", "compliance", "devices,compliance", "reprocessing", "reminders", "packs,release", "inventory,devices,documents,compliance,packs,recalls,release"} {
		if _, err := ParseModules(value); err == nil {
			t.Errorf("invalid selection accepted: %q", value)
		}
	}
	for _, value := range []string{"inventory", "devices", " packs , recalls ", "inventory,devices,documents,compliance,reprocessing", "all"} {
		c := modulesSelection(t, value)
		if !c.Enabled("inventory") || c.Enabled("unknown") {
			t.Fatal("inventory omitted or unknown module enabled", value)
		}
	}
	if c := modulesSelection(t, "all"); !c.Enabled("release") || len(c.names()) != len(moduleDefinitions) {
		t.Fatal("all does not retain existing functionality")
	}
	// A preconfiguration database retains the previously available modules.
	f := stockDB(t)
	tx, err := f.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	c, revision, err := currentModules(context.Background(), tx)
	tx.Rollback()
	if err != nil || c != allModules() || revision != 0 {
		t.Fatal("unconfigured installation lost compatibility", c, revision, err)
	}
}

func TestModulesCanonicalRevisionImmutableHistoryAndRestart(t *testing.T) {
	f := stockDB(t)
	if revision := modulesConfigure(t, &f, "packs,inventory"); revision != 1 {
		t.Fatal("initial revision", revision)
	}
	if revision := modulesConfigure(t, &f, "inventory,packs"); revision != 1 {
		t.Fatal("equivalent restart invalidated evidence", revision)
	}
	var stored string
	if err := f.db.QueryRow(`SELECT modules FROM module_configuration_events WHERE version=1`).Scan(&stored); err != nil || stored != "inventory,packs" {
		t.Fatal("noncanonical selection", stored, err)
	}
	if _, err := ConfigureModules(context.Background(), f.db, ModuleConfig{}); err == nil {
		t.Fatal("core can be disabled through configuration API")
	}
	if err := RequireModuleConfiguration(context.Background(), f.db, allModules()); err == nil {
		t.Fatal("manual job ignored different live deployment selection")
	}
	if revision := modulesConfigure(t, &f, "inventory"); revision != 2 {
		t.Fatal("disabled transition missing", revision)
	}
	if revision := modulesConfigure(t, &f, "packs"); revision != 3 {
		t.Fatal("reenabling reused original revision", revision)
	}
	for _, statement := range []string{
		`UPDATE module_configuration_events SET modules='inventory' WHERE version=1`,
		`DELETE FROM module_configuration_events WHERE version=1`,
		`INSERT INTO module_configuration_events VALUES(5,'inventory','synthetic')`,
	} {
		if _, err := f.db.Exec(statement); err == nil {
			t.Fatal("module history can be rewritten or skip revisions", statement)
		}
	}
	if revision := modulesConfigure(t, &f, "inventory,packs"); revision != 3 {
		t.Fatal("failed tampering changed live configuration", revision)
	}
	w := packHTTP(f, "GET", "modules", "", nil)
	requireStatus(t, w, 200)
	var result struct {
		Revision int                `json:"revision"`
		Enabled  []string           `json:"enabled_modules"`
		Modules  []moduleDefinition `json:"modules"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil || result.Revision != 3 || strings.Join(result.Enabled, ",") != "inventory,packs" || len(result.Modules) != len(moduleDefinitions) {
		t.Fatal("module metadata missing persisted state", w.Body.String(), err)
	}
	for _, module := range result.Modules {
		if module.ID == "inventory" && (!module.Required || !module.Enabled) {
			t.Fatal("inventory metadata allows disabling core")
		}
	}
}

func TestModulesInventoryIncludesEquipmentWithoutDeviceFiles(t *testing.T) {
	f, id := controlFixture(t)
	if _, err := f.db.Exec(`INSERT INTO role_assignments VALUES(?,?,'material_manager')`, f.user, demoSite); err != nil {
		t.Fatal(err)
	}
	modulesConfigure(t, &f, "inventory")
	input := deviceInput{LocationID: warehouse, InventoryNumber: "MODULE-CORE-1", Name: "Synthetic equipment", Manufacturer: "Demo", Model: "Demo", SerialOrLot: "MODULE-1", Note: "Synthetic core inventory"}
	requireStatus(t, packHTTP(f, "POST", "devices", "modules-core-register", input), 200)
	requireStatus(t, packHTTP(f, "POST", "devices/"+id+"/details", "modules-core-details", detailsInput(0)), 200)
	requireStatus(t, packHTTP(f, "POST", "devices/"+id+"/master-data", "modules-core-master-data", masterInput(1)), 200)
	intake := deviceImportRequest{CSV: minimalIntakeCSV, Locations: map[string]string{"Offline warehouse": warehouse}}
	preview := importPreview(t, f, intake)
	if preview.New != 2 || preview.Errors != 0 {
		t.Fatal("equipment inventory preview requires optional files", preview)
	}
	intake.Basis = preview.Basis
	requireStatus(t, packHTTP(f, "POST", "device-import/commit", "modules-core-import", intake), 201)
	for _, path := range []string{"devices", "devices/export.csv", "device-import/template.xlsx", "stock-import/template.xlsx", "labels/location/" + warehouse, "devices/" + id + "/book", "devices/" + id + "/events"} {
		requireStatus(t, packHTTP(f, "GET", path, "", nil), 200)
	}
	requireStatus(t, stockHTTP(f, "receipts", "modules-core-stock", stockRequest{BatchID: demoBatch, To: warehouse, Quantity: 4}), 201)
	requireStatus(t, packHTTP(f, "POST", "scanner/resolve", "", map[string]string{"text": "drk:v1:location:" + warehouse}), 200)
	var count int
	if err := f.db.QueryRow(`SELECT COUNT(*) FROM devices`).Scan(&count); err != nil || count != 4 {
		t.Fatal("equipment core registration/import missing", count, err)
	}
	if q, _ := quantity(t, f.db, warehouse, "available"); q != 4 {
		t.Fatal("material inventory core unavailable", q)
	}
	for _, path := range []string{
		"devices/" + id + "/commission", "devices/" + id + "/rules", "devices/" + id + "/inspections", "devices/" + id + "/defects", "devices/" + id + "/releases", "devices/" + id + "/instructions", "devices/" + id + "/maintenance", "devices/" + id + "/decommission", "devices/" + id + "/corrections", "devices/" + id + "/incidents", "devices/" + id + "/reports", "devices/" + id + "/documents", "devices/" + id + "/documents/missing/reviews", "devices/" + id + "/manual-access", "devices/" + id + "/usage", "devices/" + id + "/end-user-acceptance", "devices/" + id + "/product-duties", "devices/" + id + "/duty-fulfilment", "devices/" + id + "/reprocessing", "packlists", "recalls",
	} {
		modulesError(t, packHTTP(f, "POST", path, "modules-inactive-write", map[string]any{}), 403, "module_disabled")
	}
	// ServeMux decodes escaped path segments; URL spelling cannot bypass a gate.
	escaped := "%" + string("0123456789abcdef"[id[0]>>4]) + string("0123456789abcdef"[id[0]&15]) + id[1:]
	modulesError(t, packHTTP(f, "POST", "devices/"+escaped+"/commission", "modules-escaped-write", controlProof()), 403, "module_disabled")
	anonymous := f
	anonymous.cookie = nil
	requireStatus(t, packHTTP(anonymous, "POST", "devices/"+id+"/commission", "modules-anonymous-write", controlProof()), 401)
	requireStatus(t, packHTTP(anonymous, "GET", "modules", "", nil), 401)
	// Readable historical files do not grant access to another site.
	if err := SetSiteRole(f.db, "viewer", nil, "viewer"); err != nil {
		t.Fatal(err)
	}
	f.cookie, f.csrf = loginTest(t, f.handler, nil)
	requireStatus(t, packHTTP(f, "GET", "devices/"+id+"/book", "", nil), 404)
	requireStatus(t, packHTTP(f, "POST", "devices", "modules-core-out-of-scope", input), 404)
}

func TestModulesOldProcessFailsClosedAfterConfigurationChange(t *testing.T) {
	f := stockDB(t)
	modulesConfigure(t, &f, "all")
	old := f
	modulesConfigure(t, &f, "inventory")
	input := stockRequest{BatchID: demoBatch, To: warehouse, Quantity: 1}
	modulesError(t, stockHTTP(old, "receipts", "modules-old-process", input), 503, "module_configuration_changed")
	modulesError(t, packHTTP(old, "GET", "modules", "", nil), 503, "module_configuration_changed")
	requireStatus(t, stockHTTP(f, "receipts", "modules-current-process", input), 201)
	assertJournal(t, f.db, 1)
}

func TestModulesInFlightConfigurationChangeIsCheckedInsideMutation(t *testing.T) {
	f := stockDB(t)
	modulesConfigure(t, &f, "all")
	input, err := json.Marshal(stockRequest{BatchID: demoBatch, To: warehouse, Quantity: 5})
	if err != nil {
		t.Fatal(err)
	}
	changed := false
	body := &moduleChangeBody{Reader: strings.NewReader(string(input)), change: func() {
		// The HTTP gate has already passed. Change deployment selection while
		// reading the valid payload, before the mutation starts its transaction.
		if _, err := ConfigureModules(context.Background(), f.db, modulesSelection(t, "inventory")); err != nil {
			t.Fatal(err)
		}
		changed = true
	}}
	r := httptest.NewRequest("POST", testOrigin+"/api/v1/stock/receipts", body)
	r.AddCookie(f.cookie)
	r.Header.Set("Origin", testOrigin)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-CSRF-Token", f.csrf)
	r.Header.Set("Idempotency-Key", "modules-in-flight-receipt")
	w := httptest.NewRecorder()
	f.handler.ServeHTTP(w, r)
	if !changed {
		t.Fatal("request did not reach the valid body after the initial gate")
	}
	modulesError(t, w, 503, "module_configuration_changed")
	assertJournal(t, f.db, 0)
	var balances int
	if err := f.db.QueryRow(`SELECT COUNT(*) FROM stock_balances`).Scan(&balances); err != nil || balances != 0 {
		t.Fatal("in-flight configuration change committed stock", balances, err)
	}
}

func TestModulesDocumentsCannotBeChangedThroughGenericDeviceRoutes(t *testing.T) {
	f, id, document := documentFixture(t)
	modulesConfigure(t, &f, "inventory,devices")
	path := "devices/" + id
	upload := documentInput{Filename: document.Filename, MediaType: document.MediaType, Content: []byte("Synthetic instruction proof\n"), Note: "Synthetic document upload"}
	modulesError(t, packHTTP(f, "POST", path+"/documents", "document-upload-key", upload), 403, "module_disabled")
	modulesError(t, packHTTP(f, "POST", path+"/documents/"+document.ID+"/reviews", "modules-disabled-document-scan", reviewProof(document, "scan", 0)), 403, "module_disabled")
	// Service functions are also protected if a future caller bypasses the mux.
	a := newAuth(f.db, testOrigin)
	a.modules = modulesSelection(t, "inventory,devices")
	ctx, token := context.Background(), digest(f.cookie.Value)
	_, err := uploadDocument(ctx, f.db, f.user, token, "document-upload-key", id, upload, time.Now())
	modulesDirectDisabled(t, err)
	_, err = a.reviewDocument(ctx, f.user, token, "modules-direct-document-scan", id, document.ID, reviewProof(document, "scan", 0))
	modulesDirectDisabled(t, err)
	_, err = a.mutateManualAccess(ctx, f.user, token, "modules-direct-manual", id, accessProof(document, f.user, 0))
	modulesDirectDisabled(t, err)
	_, err = mutateUsage(ctx, a, f.user, token, "modules-direct-person-policy", id, usageProof("policy", 0))
	modulesDirectDisabled(t, err)
	for _, action := range []string{"instructions", "maintenance", "corrections", "incidents", "reports"} {
		in := controlProof()
		in.DocumentID = document.ID
		modulesError(t, packHTTP(f, "POST", path+"/"+action, "modules-bound-document-"+action, in), 403, "module_disabled")
	}
	// Historical quarantine access rules remain in force when the UI is hidden.
	requireStatus(t, packHTTP(f, "GET", path+"/documents", "", nil), 200)
	requireStatus(t, packHTTP(f, "GET", path+"/documents/"+document.ID+"/download", "", nil), 409)
	requireStatus(t, packHTTP(f, "GET", path+"/documents/"+document.ID+"/download?review=true", "", nil), 200)
	var count int
	if err := f.db.QueryRow(`SELECT COUNT(*) FROM device_documents`).Scan(&count); err != nil || count != 1 {
		t.Fatal("inactive writes added or destroyed document history", count, err)
	}
}

func TestModulesIndependentPacksCannotDiscardInactiveDeviceRequirements(t *testing.T) {
	f, id := packDeviceFixture(t)
	modulesConfigure(t, &f, "all")
	input := packInput(warehouse)
	input.Devices = []packDevice{{DeviceID: id}}
	withDevices := published(t, f, input, "modules-original-device-pack")
	modulesConfigure(t, &f, "inventory,packs")
	// Historical retries are blocked before returning old successful payloads.
	modulesError(t, packHTTP(f, "POST", "packlists", "modules-original-device-pack", input), 403, "module_disabled")
	removeDevices := packInput(warehouse)
	removeDevices.ExpectedVersion = withDevices.Version
	modulesError(t, packHTTP(f, "POST", "packlists", "modules-strip-device-requirements", removeDevices), 403, "module_disabled")
	modulesError(t, packHTTP(f, "POST", "packlists/"+withDevices.ID+"/device-checks", "modules-disabled-device-check", map[string]any{}), 403, "module_disabled")
	read := packHTTP(f, "GET", "packlists/"+withDevices.ID, "", nil)
	requireStatus(t, read, 200)
	var retained packVersion
	if err := json.Unmarshal(read.Body.Bytes(), &retained); err != nil || len(retained.Devices) != 1 || retained.Devices[0].DeviceID != id {
		t.Fatal("disabled group lost recorded device requirements", read.Body.String(), err)
	}
	materialOnly := published(t, f, packInput(kit), "modules-independent-material-pack")
	requireStatus(t, stockHTTP(f, "receipts", "modules-independent-pack-stock", stockRequest{BatchID: demoBatch, To: kit, Quantity: 5}), 201)
	current := readiness(t, f, materialOnly.ID)
	requireStatus(t, materialCheck(t, f, materialOnly.ID, "modules-independent-material-check", current.Basis, true), 201)
	current = readiness(t, f, materialOnly.ID)
	if current.Status != "ready" || current.Overall != "not_assessed" {
		t.Fatal("independent material pack disabled or inferred operational release", current)
	}
}

func TestModulesRecallsRemainIndependentAndKeepSafetyBlocks(t *testing.T) {
	f, id := controlFixture(t)
	if _, err := f.db.Exec(`INSERT INTO role_assignments VALUES(?,?,'material_manager')`, f.user, demoSite); err != nil {
		t.Fatal(err)
	}
	recallCoordinator(t, &f)
	modulesConfigure(t, &f, "all")
	requireStatus(t, stockHTTP(f, "receipts", "modules-recall-stock", stockRequest{BatchID: demoBatch, To: warehouse, Quantity: 5}), 201)
	deviceInput := preparedRecall(t, f, recallSelector{Kind: "device", Manufacturer: "Demo", Model: "Demo", SerialExact: "Demo"})
	deviceRecall := recallPosted(t, f, "modules-device-recall", deviceInput)
	modulesConfigure(t, &f, "inventory,recalls")
	if replay := recallPosted(t, f, "modules-device-recall", deviceInput); replay.ID != deviceRecall.ID {
		t.Fatal("independent equipment recall retry unavailable")
	}
	target := recallRows(t, f)[0].Targets[0]
	deviceRecall = recallPosted(t, f, "modules-independent-device-measure", recallInput{Action: "complete", ID: deviceRecall.ID, ExpectedVersion: deviceRecall.Version, TargetKey: target.Key, Basis: target.Basis, Evidence: "Synthetic manufacturer correction", Note: "Synthetic recall measure without optional device files"})
	deviceRecall = recallPosted(t, f, "modules-independent-device-closure", recallInput{Action: "close", ID: deviceRecall.ID, ExpectedVersion: deviceRecall.Version, Evidence: "Synthetic manufacturer closure reference", Note: "Synthetic recall closure without optional device files"})
	if deviceRecall.Status != "CLOSED" || len(cycleDevice(t, f, id).ActiveBlocks) != 0 {
		t.Fatal("independent equipment recall could not complete and close", deviceRecall)
	}
	// The asset registry is core: a new equipment recall also needs no device-file module.
	newDeviceInput := preparedRecall(t, f, recallSelector{Kind: "device", Manufacturer: "Demo", Model: "Demo", SerialExact: "Demo"})
	newDeviceRecall := recallPosted(t, f, "modules-new-independent-device-recall", newDeviceInput)
	materialInput := preparedRecall(t, f, recallSelector{Kind: "material", ProductID: demoProduct})
	materialRecall := recallPosted(t, f, "modules-independent-material-recall", materialInput)
	modulesConfigure(t, &f, "inventory")
	modulesError(t, packHTTP(f, "POST", "recalls", "modules-new-independent-device-recall", newDeviceInput), 403, "module_disabled")
	for _, action := range []string{"close", "complete"} {
		in := recallInput{Action: action, ID: newDeviceRecall.ID, ExpectedVersion: newDeviceRecall.Version, Evidence: "Synthetic recall operation", Note: "Synthetic disabled recall selection"}
		modulesError(t, packHTTP(f, "POST", "recalls", "modules-disabled-device-recall-"+action, in), 403, "module_disabled")
	}
	close := recallInput{Action: "close", ID: materialRecall.ID, ExpectedVersion: materialRecall.Version, Evidence: "Synthetic closure reference", Note: "Synthetic closure decision"}
	modulesError(t, packHTTP(f, "POST", "recalls", "modules-disabled-material-recall", close), 403, "module_disabled")
	requireStatus(t, stockHTTP(f, "consumptions", "modules-existing-material-block", stockRequest{BatchID: demoBatch, From: warehouse, Quantity: 1, ExpectedVersion: 1}), 409)
	book := cycleDevice(t, f, id)
	if len(book.ActiveBlocks) != 1 || !strings.HasPrefix(book.ActiveBlocks[0].ID, "recall:") {
		t.Fatal("disabled recall UI removed current device safety block", book)
	}
	if len(recallRows(t, f)) != 3 {
		t.Fatal("recall history inaccessible or lost")
	}
}

func TestModulesReleaseOffOnNeedsNewExplicitDecisionAndPreservesArchive(t *testing.T) {
	f, id, device, subject := releaseFixture(t, false)
	modulesConfigure(t, &f, "all")
	before := assessment(t, f, id)
	if before.Evidence.Status != "recorded_prerequisites_met" {
		t.Fatal("release fixture missing prerequisites", before.Evidence)
	}
	in := releaseProof(t, f, id, subject)
	saved := releasePost(t, f, id, "modules-explicit-original-release", in, 200)
	if releaseRead(t, f, id, subject).Status != "released" {
		t.Fatal("initial explicit release missing")
	}
	oldProcess := f
	modulesConfigure(t, &f, "inventory")
	modulesError(t, packHTTP(oldProcess, "POST", "packlists/"+id+"/release", "modules-explicit-original-release", in), 503, "module_configuration_changed")
	modulesError(t, packHTTP(f, "POST", "packlists/"+id+"/release", "modules-explicit-original-release", in), 403, "module_disabled")
	current := releaseRead(t, f, id, subject)
	if current.Status == "released" || current.Overall == "released_for_user" || current.Current == nil || current.Current.ID != saved.ID || len(current.Events) != 1 {
		t.Fatal("inactive module erased history or retained current release", current)
	}
	projected := assessment(t, f, id)
	if projected.Overall == "released_for_user" || projected.Evidence.Status == "recorded_prerequisites_met" || len(projected.Devices) != 1 {
		t.Fatal("pack projection inferred prerequisites while modules disabled", projected)
	}
	d := projected.Devices[0]
	if d.UserUsage.Status == "recorded_requirements_met" || d.ManualAccess.Status == "confirmed" || d.EndUser.Status == "accepted" || d.ProductDuties.Status == "reviewed" || d.DutyFulfilment.Status == "accepted" {
		t.Fatal("inactive partial evidence contributes positive prerequisites", d)
	}
	archive := packHTTP(f, "GET", "packlists/"+id+"/release/export.json", "", nil)
	requireStatus(t, archive, 200)
	var exported struct {
		Assessment packReleaseAssessment `json:"assessment"`
		Events     []packReleaseEvent    `json:"events"`
	}
	if err := json.Unmarshal(archive.Body.Bytes(), &exported); err != nil || len(exported.Events) != 1 || exported.Events[0].ID != saved.ID || exported.Assessment.Status == "released" {
		t.Fatal("historical export lost event or presents inactive release as current", archive.Body.String(), err)
	}
	book, _ := bookArchive(t, f, device)
	if len(book.DutyFulfilmentEvents) != 1 || book.DutyFulfilment.Status == "accepted" || book.EndUserReview.Status == "accepted" || book.ProductDutyReview.Status == "reviewed" {
		t.Fatal("device archive uses disabled current assessments or loses history", book)
	}
	if revision := modulesConfigure(t, &f, "all"); revision != 3 {
		t.Fatal("off/on cycle revision missing", revision)
	}
	current = releaseRead(t, f, id, subject)
	if current.Status == "released" || current.Basis == in.ExpectedBasis || assessment(t, f, id).Overall == "released_for_user" {
		t.Fatal("reenabling modules silently reactivated original release", current)
	}
	// A successful historical response still cannot restore current acceptance.
	replayed := releasePost(t, f, id, "modules-explicit-original-release", in, 200)
	if replayed.ID != saved.ID || releaseRead(t, f, id, subject).Status == "released" {
		t.Fatal("historical idempotent retry revived release")
	}
	fresh := releaseProof(t, f, id, subject)
	releasePost(t, f, id, "modules-explicit-renewed-release", fresh, 200)
	if releaseRead(t, f, id, subject).Status != "released" {
		t.Fatal("new explicit review did not restore release")
	}
	modulesConfigure(t, &f, "all")
	if releaseRead(t, f, id, subject).Status != "released" {
		t.Fatal("unchanged restart invalidated qualified release")
	}
}

func TestModulesReprocessingInactiveKeepsHistoryAndNegativeBlocks(t *testing.T) {
	f, id, doc, _ := cycleFixture(t)
	modulesConfigure(t, &f, "all")
	cyclePost(t, f, id, "modules-cycle-needed", cycleProof("need", 0), 200)
	completed := cyclePost(t, f, id, "modules-cycle-complete", completedCycle(t, f, id, doc, "MODULE-CYCLE", "passed", 1), 200)
	in := releaseCycle(t, f, id, doc, 2, completed.Version, completed.Input.ProcessVersion)
	cyclePost(t, f, id, "modules-cycle-release", in, 200)
	if current, _ := cycleRead(t, f, id); current.Status != "released" {
		t.Fatal("fixture lacks accepted reprocessing result", current)
	}
	modulesConfigure(t, &f, "inventory,devices,documents,compliance")
	current, _ := cycleRead(t, f, id)
	if current.Status == "released" || len(current.Events) != 3 || cycleDevice(t, f, id).Reprocessing.Status == "released" {
		t.Fatal("inactive cycle module retains positive assessment or lost history", current)
	}
	modulesError(t, packHTTP(f, "POST", "devices/"+id+"/reprocessing", "synthetic-modules-cycle-release", in), 403, "module_disabled")
	modulesConfigure(t, &f, "all")
	cyclePost(t, f, id, "modules-cycle-new-need", cycleProof("need", 3), 200)
	modulesConfigure(t, &f, "inventory,devices,documents,compliance")
	current, _ = cycleRead(t, f, id)
	book := cycleDevice(t, f, id)
	if current.Status != "blocked" || len(book.ActiveBlocks) == 0 || book.Reprocessing.Status != "blocked" {
		t.Fatal("deactivation concealed outstanding hygiene block", current, book)
	}
	var count int
	if err := f.db.QueryRow(`SELECT COUNT(*) FROM reprocessing_events WHERE device_id=?`, id).Scan(&count); err != nil || count != 4 {
		t.Fatal("reprocessing history mutated by configuration transitions", count, err)
	}
}

func TestModulesInactiveReminderJobsPreserveQueueAndNeverSend(t *testing.T) {
	f, _ := reminderFixture(t)
	modulesConfigure(t, &f, "all")
	now := reminderTime("2026-10-11")
	if err := ReconcileDeviceReminders(context.Background(), f.db, now, []int{60, 30, 7}); err != nil {
		t.Fatal(err)
	}
	if err := enqueueReminderMail(context.Background(), f.db, mailConfig(), now); err != nil {
		t.Fatal(err)
	}
	before := map[string]int{}
	for _, table := range []string{"device_reminders", "device_reminder_outbox", "device_reminder_worker", "device_reminder_deliveries", "device_reminder_mail_attempts"} {
		before[table] = reminderCount(t, f, table)
	}
	if before["device_reminders"] == 0 || before["device_reminder_deliveries"] == 0 {
		t.Fatal("fixture has no existing reminder or pending mail")
	}
	var delivery string
	if err := f.db.QueryRow(`SELECT id FROM device_reminder_deliveries ORDER BY id LIMIT 1`).Scan(&delivery); err != nil {
		t.Fatal(err)
	}
	modulesConfigure(t, &f, "inventory")
	modulesDirectDisabled(t, ReconcileDeviceReminders(context.Background(), f.db, now.Add(time.Hour), []int{60, 30, 7}))
	sender := &fakeReminderSender{}
	modulesDirectDisabled(t, DispatchReminderMail(context.Background(), f.db, mailConfig(), sender, func() time.Time { return now }))
	modulesDirectDisabled(t, RetryReminderMail(context.Background(), f.db, delivery, true, now))
	if len(sender.messages) != 0 {
		t.Fatal("inactive reminders sent mail", sender.messages)
	}
	for table, count := range before {
		if after := reminderCount(t, f, table); after != count {
			t.Fatal("disabled job changed preserved reminder history/queue", table, count, after)
		}
	}
	var active int
	if err := f.db.QueryRow(`SELECT COUNT(*) FROM device_reminders WHERE active=1`).Scan(&active); err != nil || active != before["device_reminders"] {
		t.Fatal("disabled reconciliation resolved pending reminder rows", active, err)
	}
	if mailStates(t, f)["pending"] != before["device_reminder_deliveries"] {
		t.Fatal("inactive mail job leased or retried pending deliveries", mailStates(t, f))
	}
}
