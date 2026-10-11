package app

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func functionDutyInput(document documentRecord, location string) productDutyInput {
	in := annex1Proof(document, "", 1, 2)
	in.ExpectedDeviceVersion = 2
	in.Rules[0].FunctionCheck = &dutyFunctionProof{Reviewed: true, PerformedOn: "2024-01-03", LocationID: location, Examiner: "Synthetic examiner <img>", Qualification: "Synthetic documented competence", Assignment: "Synthetic operator appointment", Method: "Synthetic product-specific functional protocol", Conditions: "Synthetic operating location and configuration", Result: "passed", Evidence: "Synthetic protocol reference", DocumentID: document.ID}
	return in
}

func TestDutyFunctionProtocolBindingFailureRevocationAndArchive(t *testing.T) {
	f, id, source := annex1Fixture(t)
	w := packHTTP(f, "POST", "devices/"+id+"/documents", "function-protocol-upload", documentInput{ExpectedVersion: 2, Filename: "Functional protocol.txt", MediaType: "text/plain", Content: []byte("Synthetic functional protocol"), Note: "Synthetic independent protocol"})
	requireStatus(t, w, 200)
	var protocol documentRecord
	if err := json.Unmarshal(w.Body.Bytes(), &protocol); err != nil {
		t.Fatal(err)
	}
	var location string
	if err := f.db.QueryRow(`SELECT location_id FROM devices WHERE id=?`, id).Scan(&location); err != nil {
		t.Fatal(err)
	}
	in := functionDutyInput(source, location)
	in.Rules[0].FunctionCheck.DocumentID = protocol.ID
	dutyPost(t, f, id, "function-quarantined", in, 409)
	reviewPost(t, f, id, protocol.ID, "function-scan", reviewProof(protocol, "scan", 0), 200)
	reviewPost(t, f, id, protocol.ID, "function-release", reviewProof(protocol, "release", 1), 200)
	first := dutyPost(t, f, id, "function-review", in, 200)
	if first.FunctionDocumentSHA256 != protocol.SHA256 || annex1Status(t, f, id, "annex_1_function") != "record_found" || annex1Status(t, f, id, "annex_1") != "unknown" || dutyRead(t, f, id).Overall != "not_assessed" {
		t.Fatal("false acceptance or missing protocol binding")
	}
	book, files := bookArchive(t, f, id)
	if len(book.ProductDuties) != 1 || !strings.Contains(string(files["akte.json"]), protocol.SHA256) || !strings.Contains(string(files["geraeteakte.html"]), "annex_1_function") || strings.Contains(string(files["geraeteakte.html"]), "Synthetic examiner <img>") {
		t.Fatal("missing or unsafe archive proof")
	}
	reviewPost(t, f, id, protocol.ID, "function-withdraw-protocol", reviewProof(protocol, "quarantine", 2), 200)
	if dutyRead(t, f, id).Status != "reviewed" || annex1Status(t, f, id, "annex_1_function") != "unknown" {
		t.Fatal("independent protocol withdrawal ignored")
	}
	if replay := dutyPost(t, f, id, "function-review", in, 200); replay.ID != first.ID || replay.FunctionDocumentSHA256 != first.FunctionDocumentSHA256 {
		t.Fatal("historical retry changed")
	}
	reviewPost(t, f, id, protocol.ID, "function-rescan", reviewProof(protocol, "scan", 3), 200)
	reviewPost(t, f, id, protocol.ID, "function-rerelease", reviewProof(protocol, "release", 4), 200)
	in.ExpectedVersion = 1
	in.Rules[0].FunctionCheck.Result = "failed"
	dutyPost(t, f, id, "function-failed", in, 200)
	if annex1Status(t, f, id, "annex_1_function") != "blocked" || annex1Status(t, f, id, "annex_1") != "blocked" {
		t.Fatal("failed functional proof accepted")
	}
	revoke := dutyProof(source)
	revoke.Action = "revoke"
	revoke.ExpectedVersion = 2
	revoke.ExpectedDeviceVersion = 2
	revoke.DocumentID = ""
	revoke.ValidUntil = ""
	revoke.Reviewed = nil
	revoke.Rules = nil
	dutyPost(t, f, id, "function-revoke", revoke, 200)
	if dutyRead(t, f, id).Status != "blocked" {
		t.Fatal("revocation lost")
	}
}

func TestDutyFunctionValidationAndIndependentDeviceBlock(t *testing.T) {
	f, id, document := annex1Fixture(t)
	var location string
	f.db.QueryRow(`SELECT location_id FROM devices WHERE id=?`, id).Scan(&location)
	for _, variant := range []string{"unchecked", "future", "bad-date", "wrong-location", "no-examiner", "no-qualification", "no-assignment", "no-method", "no-conditions", "no-evidence", "bad-result", "wrong-kind", "exempt", "foreign-document"} {
		in := functionDutyInput(document, location)
		proof := in.Rules[0].FunctionCheck
		status := 400
		switch variant {
		case "unchecked":
			proof.Reviewed = false
		case "future":
			proof.PerformedOn = time.Now().Add(48 * time.Hour).Format("2006-01-02")
		case "bad-date":
			proof.PerformedOn = "2024-02-30"
		case "wrong-location":
			proof.LocationID = "other-location"
		case "no-examiner":
			proof.Examiner = ""
		case "no-qualification":
			proof.Qualification = ""
		case "no-assignment":
			proof.Assignment = ""
		case "no-method":
			proof.Method = ""
		case "no-conditions":
			proof.Conditions = ""
		case "no-evidence":
			proof.Evidence = ""
		case "bad-result":
			proof.Result = "completed"
		case "wrong-kind":
			in.Rules[0].FunctionCheck = nil
			in.Rules[1].FunctionCheck = proof
		case "exempt":
			in.Rules[0].Applicability = "exempt"
		case "foreign-document":
			proof.DocumentID = newID()
			status = 404
		}
		dutyPost(t, f, id, "function-invalid-"+variant, in, status)
	}
	if dutyRead(t, f, id).Version != 0 {
		t.Fatal("invalid mutation persisted")
	}
	in := functionDutyInput(document, location)
	dutyPost(t, f, id, "function-valid", in, 200)
	// The server-bound checksum is essential even when the document is released.
	tx, err := f.db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	record, err := readDevice(context.Background(), tx, f.user, id, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	check, err := checkDutyFunction(context.Background(), tx, record, in.Rules[0].FunctionCheck, "wrong-sha")
	changedLocation := record
	changedLocation.LocationID = "00000000-0000-4000-8000-000000000004"
	assessment, assessmentErr := assessProductDuty(context.Background(), tx, f.user, changedLocation, time.Now())
	tx.Rollback()
	if assessmentErr != nil || assessment.Status != "unknown" || len(assessment.Checks) != 0 {
		t.Fatal("location change accepted", assessment, assessmentErr)
	}
	if err != nil || check.Status != "unknown" {
		t.Fatal("checksum mismatch accepted", check, err)
	}
	defect := controlProof()
	defect.ExpectedVersion = 2
	defect.BlockReason = "defect"
	requireStatus(t, packHTTP(f, "POST", "devices/"+id+"/defects", "function-independent-defect", defect), 200)
	if annex1Status(t, f, id, "annex_1_function") != "blocked" {
		t.Fatal("function proof erased independent block")
	}
	// A supported identity correction invalidates the complete review.
	requireStatus(t, packHTTP(f, "POST", "devices/"+id+"/details", "function-identity-change", detailsInput(3)), 200)
	if dutyRead(t, f, id).Status != "unknown" {
		t.Fatal("identity change accepted")
	}
}

func TestDutyFunctionExplicitAcceptanceAndInvalidation(t *testing.T) {
	f, id, document := annex1Fixture(t)
	in := functionDutyInput(document, warehouse)
	acceptance := &dutyFunctionAcceptance{Reviewed: true, Decision: "accepted", PerformedOn: "2024-01-04", Qualification: "Synthetic acceptance competence", Assignment: "Synthetic acceptance assignment", Evidence: "Synthetic acceptance protocol", Note: "Synthetic examiner, method and conditions suitability assessment"}
	in.Rules[0].FunctionCheck.Acceptance = acceptance
	for _, variant := range []string{"unchecked", "early", "future", "decision", "qualification", "assignment", "evidence", "note", "failed"} {
		bad := in
		bad.Rules = append([]productDutyRule{}, in.Rules...)
		proof := *in.Rules[0].FunctionCheck
		bad.Rules[0].FunctionCheck = &proof
		a := *acceptance
		proof.Acceptance = &a
		switch variant {
		case "unchecked":
			a.Reviewed = false
		case "early":
			a.PerformedOn = "2024-01-02"
		case "future":
			a.PerformedOn = "2099-01-01"
		case "decision":
			a.Decision = "automatic"
		case "qualification":
			a.Qualification = ""
		case "assignment":
			a.Assignment = ""
		case "evidence":
			a.Evidence = ""
		case "note":
			a.Note = ""
		case "failed":
			proof.Result = "failed"
		}
		dutyPost(t, f, id, "accept-invalid-"+variant, bad, 400)
	}
	first := dutyPost(t, f, id, "function-accept", in, 200)
	if annex1Status(t, f, id, "annex_1_function") != "accepted" || annex1Status(t, f, id, "annex_1") != "unknown" || dutyRead(t, f, id).Overall != "not_assessed" {
		t.Fatal("missing partial acceptance or false overall release")
	}
	_, files := bookArchive(t, f, id)
	if !strings.Contains(string(files["akte.json"]), "Synthetic acceptance protocol") || !strings.Contains(string(files["geraeteakte.html"]), "accepted") {
		t.Fatal("missing archive acceptance")
	}
	reviewPost(t, f, id, document.ID, "accept-withdraw", reviewProof(document, "quarantine", 2), 200)
	if dutyRead(t, f, id).Status != "unknown" {
		t.Fatal("withdrawn source accepted")
	}
	if replay := dutyPost(t, f, id, "function-accept", in, 200); replay.ID != first.ID {
		t.Fatal("acceptance retry changed")
	}
	reviewPost(t, f, id, document.ID, "accept-rescan", reviewProof(document, "scan", 3), 200)
	reviewPost(t, f, id, document.ID, "accept-rerelease", reviewProof(document, "release", 4), 200)
	in.ExpectedVersion = 1
	acceptance.Decision = "rejected"
	dutyPost(t, f, id, "function-reject", in, 200)
	if annex1Status(t, f, id, "annex_1_function") != "blocked" || annex1Status(t, f, id, "annex_1") != "blocked" {
		t.Fatal("rejection ignored")
	}
	in.ExpectedVersion = 2
	acceptance.Decision = "accepted"
	dutyPost(t, f, id, "function-accept-again", in, 200)
	defect := controlProof()
	defect.ExpectedVersion = 2
	defect.BlockReason = "defect"
	requireStatus(t, packHTTP(f, "POST", "devices/"+id+"/defects", "accept-independent-block", defect), 200)
	if annex1Status(t, f, id, "annex_1_function") != "blocked" {
		t.Fatal("partial acceptance released independent block")
	}
}
