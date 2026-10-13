package app

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

func cyclePost(t *testing.T, f stockFixture, id, key string, in cycleInput, status int) cycleEvent {
	t.Helper()
	w := packHTTP(f, "POST", "devices/"+id+"/reprocessing", "synthetic-"+key, in)
	requireStatus(t, w, status)
	var event cycleEvent
	if status == 200 {
		if err := json.Unmarshal(w.Body.Bytes(), &event); err != nil {
			t.Fatal(err)
		}
	}
	return event
}
func cycleRead(t *testing.T, f stockFixture, id string) (cycleAssessment, cycleAssessment) {
	t.Helper()
	w := packHTTP(f, "GET", "devices/"+id+"/reprocessing", "", nil)
	requireStatus(t, w, 200)
	var r struct {
		Assessment    cycleAssessment `json:"assessment"`
		Prerequisites cycleAssessment `json:"prerequisites"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &r); err != nil {
		t.Fatal(err)
	}
	return r.Assessment, r.Prerequisites
}
func cycleProof(action string, version int) cycleInput {
	return cycleInput{Action: action, ExpectedVersion: version, Evidence: "Synthetic <img> cycle proof", Qualification: "Synthetic qualified executor/reviewer", Assignment: "Synthetic assignment", Note: "Synthetic recorded decision"}
}
func cycleFixture(t *testing.T) (stockFixture, string, documentRecord, productDutyInput) {
	t.Helper()
	t.Setenv("DRK_CLAMD_ADDRESS", fakeClamd(t, time.Now(), "stream: OK"))
	f, id, doc := documentFixture(t)
	reviewPost(t, f, id, doc.ID, "cycle-source-scan", reviewProof(doc, "scan", 0), 200)
	reviewPost(t, f, id, doc.ID, "cycle-source-release", reviewProof(doc, "release", 1), 200)
	in := dutyProof(doc)
	yes := true
	for i := range in.Rules {
		r := &in.Rules[i]
		if r.Kind == "reprocessing" {
			r.Applicability = "required"
			r.Evidence = "Synthetic required external reprocessing"
			r.ReprocessingAcceptance = &dutyReprocessingAcceptance{dutyFunctionAcceptance: dutyFunctionAcceptance{Reviewed: true, Decision: "accepted", PerformedOn: time.Now().UTC().Format("2006-01-02"), Qualification: "Synthetic reviewer", Assignment: "Synthetic assignment", Evidence: "Synthetic process assessment", Note: "Synthetic controlled process"}, ExpectedSnapshot: bookSnapshot(t, f, id), DocumentID: doc.ID, ProcessDescription: "Synthetic process", ProcessVersion: "Validated 1", ManufacturerEvidence: "Synthetic maker requirements", ClassificationEvidence: "Synthetic classification", ValidationEvidence: "Synthetic validation", ExecutorEvidence: "Synthetic executor competence", ReleaseProcedureEvidence: "Synthetic independent release procedure", StorageEvidence: "Synthetic storage criteria", RequirementsMet: &yes, ProcessValidated: &yes}
		}
	}
	dutyPost(t, f, id, "synthetic-cycle-process", in, 200)
	return f, id, doc, in
}
func completedCycle(t *testing.T, f stockFixture, id string, doc documentRecord, reference, result string, version int) cycleInput {
	t.Helper()
	_, p := cycleRead(t, f, id)
	in := cycleProof("cycle", version)
	in.ExpectedBasis = p.Basis
	in.ProcessVersion = p.ProcessVersion
	in.DocumentID = doc.ID
	in.PerformedAt = time.Now().UTC().Format(time.RFC3339Nano)
	in.PerformedOn = time.Now().UTC().Format("2006-01-02")
	in.CycleReference = reference
	in.Executor = "Synthetic trained executor"
	in.Result = result
	in.ParametersEvidence = "Synthetic measured procedure parameters"
	return in
}
func releaseCycle(t *testing.T, f stockFixture, id string, doc documentRecord, version, cycleVersion, processVersion int) cycleInput {
	t.Helper()
	_, p := cycleRead(t, f, id)
	yes := true
	in := cycleProof("release", version)
	in.ExpectedBasis = p.Basis
	in.ProcessVersion = processVersion
	in.CycleVersion = cycleVersion
	in.DocumentID = doc.ID
	in.PerformedOn = time.Now().UTC().Format("2006-01-02")
	in.Reviewed = &yes
	in.ReleaseEvidence = "Synthetic fulfilled release/storage criteria"
	in.ValidUntil = time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	return in
}
func cycleDevice(t *testing.T, f stockFixture, id string) deviceRecord {
	t.Helper()
	w := packHTTP(f, "GET", "devices/"+id+"/book", "", nil)
	requireStatus(t, w, 200)
	var record deviceRecord
	if err := json.Unmarshal(w.Body.Bytes(), &record); err != nil {
		t.Fatal(err)
	}
	return record
}
func TestReprocessingCycleNeedReleaseRevokeAndIndependentBlocks(t *testing.T) {
	f, id, doc, process := cycleFixture(t)
	need := cycleProof("need", 0)
	savedNeed := cyclePost(t, f, id, "cycle-need", need, 200)
	record := cycleDevice(t, f, id)
	if len(record.ActiveBlocks) != 1 || record.ActiveBlocks[0].Reason != "reprocessing_required" {
		t.Fatal("missing independent dirty-state block", record)
	}
	generic := controlProof()
	generic.BlockID = record.ActiveBlocks[0].ID
	requireStatus(t, packHTTP(f, "POST", "devices/"+id+"/releases", "synthetic-no-generic-cycle-release", generic), 409)
	in := completedCycle(t, f, id, doc, "external-cycle-1", "passed", 1)
	bad := in
	bad.PerformedAt = time.Now().Add(-time.Hour).UTC().Format(time.RFC3339Nano)
	bad.PerformedOn = time.Now().Add(-time.Hour).UTC().Format("2006-01-02")
	cyclePost(t, f, id, "old-cycle-cannot-clear-need", bad, 400)
	cycle := cyclePost(t, f, id, "cycle-first-completed", in, 200)
	assessment, _ := cycleRead(t, f, id)
	if assessment.Status != "blocked" {
		t.Fatal("completion granted result release")
	}
	release := releaseCycle(t, f, id, doc, 2, cycle.Version, cycle.Input.ProcessVersion)
	bad = release
	bad.Reviewed = nil
	cyclePost(t, f, id, "implicit-cycle-release", bad, 400)
	saved := cyclePost(t, f, id, "cycle-first-release", release, 200)
	if r := cycleDevice(t, f, id); r.Reprocessing.Status != "released" || len(r.ActiveBlocks) != 0 {
		t.Fatal("result release did not clear only cycle block", r)
	}
	// A book/applicability review retaining identical process approval must not invalidate a released cycle.
	process.ExpectedVersion = 1
	for i := range process.Rules {
		if process.Rules[i].ReprocessingAcceptance != nil {
			process.Rules[i].ReprocessingAcceptance.ExpectedSnapshot = bookSnapshot(t, f, id)
		}
	}
	dutyPost(t, f, id, "cycle-unchanged-process-review", process, 200)
	assessment, _ = cycleRead(t, f, id)
	if assessment.Status != "released" {
		t.Fatal("unrelated review invalidated released cycle", assessment)
	}
	book, files := bookArchive(t, f, id)
	if len(book.Reprocessing) != 3 || len(files["manifest.json"]) == 0 {
		t.Fatal("missing cycle archive")
	}
	revoke := cycleProof("revoke", 3)
	revoke.CycleVersion = 2
	cyclePost(t, f, id, "cycle-revoke", revoke, 200)
	in = completedCycle(t, f, id, doc, "external-cycle-2", "passed", 4)
	cycle = cyclePost(t, f, id, "cycle-second-completed", in, 200)
	release = releaseCycle(t, f, id, doc, 5, cycle.Version, cycle.Input.ProcessVersion)
	cyclePost(t, f, id, "cycle-second-release", release, 200)
	need = cycleProof("need", 6)
	cyclePost(t, f, id, "cycle-new-need", need, 200)
	assessment, _ = cycleRead(t, f, id)
	if assessment.Status != "blocked" {
		t.Fatal("new need retained old release")
	}
	in = completedCycle(t, f, id, doc, "external-cycle-3", "failed", 7)
	cycle = cyclePost(t, f, id, "cycle-failed", in, 200)
	release = releaseCycle(t, f, id, doc, 8, cycle.Version, cycle.Input.ProcessVersion)
	cyclePost(t, f, id, "failed-cycle-release", release, 409)
	reject := cycleProof("reject", 8)
	reject.CycleVersion = cycle.Version
	cyclePost(t, f, id, "cycle-reject", reject, 200)
	in = completedCycle(t, f, id, doc, "external-cycle-1", "passed", 9)
	cyclePost(t, f, id, "duplicate-cycle-reference", in, 409)
	// Exact original reply is retained without resurrecting the old state.
	replay := cyclePost(t, f, id, "cycle-need", savedNeed.Input, 200)
	if replay.ID != savedNeed.ID {
		t.Fatal("retry changed response")
	}
	replay = cyclePost(t, f, id, "cycle-first-release", saved.Input, 200)
	if replay.ID != saved.ID {
		t.Fatal("release retry changed response")
	}
	assessment, _ = cycleRead(t, f, id)
	if assessment.Status != "blocked" || assessment.Version != 9 {
		t.Fatal("retry changed current state")
	}
	defect := controlProof()
	defect.BlockReason = "defect"
	requireStatus(t, packHTTP(f, "POST", "devices/"+id+"/defects", "synthetic-cycle-independent-defect", defect), 200)
	record = cycleDevice(t, f, id)
	if len(record.ActiveBlocks) != 2 {
		t.Fatal("independent defect missing", record.ActiveBlocks)
	}
	if _, err := f.db.Exec(`UPDATE reprocessing_events SET action='release' WHERE device_id=?`, id); err == nil {
		t.Fatal("mutable cycle history")
	}
	if _, err := f.db.Exec(`DELETE FROM reprocessing_requests`); err == nil {
		t.Fatal("mutable replay history")
	}
	if _, err := f.db.Exec(`CREATE TRIGGER cycle_test_fail BEFORE INSERT ON reprocessing_requests BEGIN SELECT RAISE(ABORT,'synthetic rollback'); END`); err != nil {
		t.Fatal(err)
	}
	rollback := cycleProof("need", 9)
	rollback.ExpectedDeviceVersion = 1
	cyclePost(t, f, id, "cycle-atomic-rollback", rollback, 503)
	assessment, _ = cycleRead(t, f, id)
	if assessment.Version != 9 {
		t.Fatal("partial mutation persisted")
	}
	if _, err := f.db.Exec(`DELETE FROM role_assignments WHERE user_id=? AND role='device_manager'`, f.user); err != nil {
		t.Fatal(err)
	}
	cyclePost(t, f, id, "cycle-first-release", saved.Input, 404)
}

func TestReprocessingSeparateProtocolEpochExpiryAndBookBinding(t *testing.T) {
	f, id, source, process := cycleFixture(t)
	input := documentInput{Filename: "cycle-protocol.txt", MediaType: "text/plain", Content: []byte("Synthetic document review proof"), Note: "Synthetic separate cycle protocol"}
	w := packHTTP(f, "POST", "devices/"+id+"/documents", "synthetic-cycle-protocol-upload", input)
	requireStatus(t, w, 200)
	var doc documentRecord
	if err := json.Unmarshal(w.Body.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	reviewPost(t, f, id, doc.ID, "cycle-protocol-scan", reviewProof(doc, "scan", 0), 200)
	reviewPost(t, f, id, doc.ID, "cycle-protocol-release", reviewProof(doc, "release", 1), 200)
	_, p := cycleRead(t, f, id)
	if p.Status != "met" {
		t.Fatal("new protocol invalidated governing process", p)
	}
	in := completedCycle(t, f, id, doc, "separate-external-cycle-1", "passed", 0)
	cycle := cyclePost(t, f, id, "separate-cycle-first", in, 200)
	release := releaseCycle(t, f, id, doc, 1, cycle.Version, cycle.Input.ProcessVersion)
	saved := cyclePost(t, f, id, "separate-cycle-release", release, 200)
	// Expiry is evaluated live, independent of storage or background jobs.
	tx, _ := f.db.Begin()
	raw := registeredDevice(deviceInput{}, id, f.user, time.Now().UTC().Format(time.RFC3339Nano))
	assessment, err := assessCycles(context.Background(), tx, raw, time.Now().Add(2*time.Hour))
	tx.Rollback()
	if err != nil || assessment.Status != "blocked" {
		t.Fatal("expired cycle remained released", assessment, err)
	}
	reviewPost(t, f, id, doc.ID, "cycle-protocol-quarantine", reviewProof(doc, "quarantine", 2), 200)
	assessment, p = cycleRead(t, f, id)
	if assessment.Status != "blocked" || p.Status != "met" {
		t.Fatal("cycle document withdrawal altered process or failed to block result", assessment, p)
	}
	reviewPost(t, f, id, doc.ID, "cycle-protocol-rescan", reviewProof(doc, "scan", 3), 200)
	reviewPost(t, f, id, doc.ID, "cycle-protocol-rerelease", reviewProof(doc, "release", 4), 200)
	assessment, _ = cycleRead(t, f, id)
	if assessment.Status != "blocked" {
		t.Fatal("rerelease resurrected old result")
	}
	in = completedCycle(t, f, id, doc, "separate-external-cycle-2", "passed", 2)
	cycle = cyclePost(t, f, id, "separate-cycle-new", in, 200)
	release = releaseCycle(t, f, id, doc, 3, cycle.Version, cycle.Input.ProcessVersion)
	cyclePost(t, f, id, "separate-cycle-new-release", release, 200)
	// Process and book assessments can coexist with a current cycle result.
	process.ExpectedVersion = 1
	for i := range process.Rules {
		r := &process.Rules[i]
		if r.ReprocessingAcceptance != nil {
			r.ReprocessingAcceptance.ExpectedSnapshot = bookSnapshot(t, f, id)
		}
		if r.Kind == "device_book" {
			r.Applicability = "required"
			r.Evidence = "Synthetic book obligation"
			r.BookAcceptance = &dutyBookAcceptance{ExpectedSnapshot: bookSnapshot(t, f, id), Reviewed: true, Decision: "accepted", PerformedOn: time.Now().UTC().Format("2006-01-02"), DocumentID: source.ID, Qualification: "Synthetic book reviewer", Assignment: "Synthetic book assignment", Evidence: "Synthetic contents review", Note: "Synthetic current cycle records included", CompletenessEvidence: "Synthetic complete book", AccessEvidence: "Synthetic access", RetentionEvidence: "Synthetic retention"}
		}
	}
	dutyPost(t, f, id, "synthetic-cycle-book-review", process, 200)
	if bookDutyStatus(t, f, id) != "accepted" {
		t.Fatal("current cycle prevented book acceptance")
	}
	assessment, _ = cycleRead(t, f, id)
	if assessment.Status != "released" {
		t.Fatal("book approval changed process basis", assessment)
	}
	need := cycleProof("need", 4)
	cyclePost(t, f, id, "separate-cycle-new-need", need, 200)
	if bookDutyStatus(t, f, id) != "unknown" {
		t.Fatal("new need not reflected in book completeness")
	}
	if replay := cyclePost(t, f, id, "separate-cycle-release", saved.Input, 200); replay.ID != saved.ID {
		t.Fatal("historical release not immutable")
	}
}
