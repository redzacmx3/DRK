package app

import (
	"encoding/json"
	"strings"
	"testing"
)

func instructionAcceptanceProof() *dutyInstructionAcceptance {
	return &dutyInstructionAcceptance{Reviewed: true, Decision: "accepted", PerformedOn: "2024-01-03", Qualification: "Synthetic reviewer competence", Assignment: "Synthetic reviewer appointment", Evidence: "Synthetic instruction acceptance <img>", Note: "Synthetic organizational suitability assessment", TrainerEvidence: "Synthetic trainer competence reference", PersonEvidence: "Synthetic designated person suitability", ScopeEvidence: "Synthetic product-specific instruction scope"}
}

func TestInstructionAcceptanceBindingWithdrawalCorrectionAndArchive(t *testing.T) {
	f, id, instructionDoc := annex1Fixture(t)
	w := packHTTP(f, "POST", "devices/"+id+"/documents", "instruction-acceptance-source", documentInput{ExpectedVersion: 2, Filename: "Product source.txt", MediaType: "text/plain", Content: []byte("Synthetic product source"), Note: "Synthetic independent classification source"})
	requireStatus(t, w, 200)
	var source documentRecord
	if err := json.Unmarshal(w.Body.Bytes(), &source); err != nil {
		t.Fatal(err)
	}
	reviewPost(t, f, id, source.ID, "instruction-acceptance-source-scan", reviewProof(source, "scan", 0), 200)
	reviewPost(t, f, id, source.ID, "instruction-acceptance-source-release", reviewProof(source, "release", 1), 200)
	in := annex1Proof(source, f.user, 1, 2)
	in.ExpectedDeviceVersion = 2
	in.Rules[0].InstructionAcceptance = instructionAcceptanceProof()
	dutyPost(t, f, id, "accept-instruction-unbound", in, 409)
	annex1Bind(t, f, id, 0, 2, 2, f.user)
	first := dutyPost(t, f, id, "accept-instruction", in, 200)
	if first.InstructionAcceptanceBasis == nil || first.InstructionAcceptanceBasis.BindingVersion != 1 || first.InstructionAcceptanceBasis.DocumentReviewVersion != 2 || annex1Status(t, f, id, "annex_1_instruction") != "accepted" || annex1Status(t, f, id, "annex_1") != "unknown" || dutyRead(t, f, id).Overall != "not_assessed" {
		t.Fatal("missing binding or false overall release")
	}
	_, files := bookArchive(t, f, id)
	if !strings.Contains(string(files["akte.json"]), "trainer_evidence") || !strings.Contains(string(files["geraeteakte.html"]), "accepted") || strings.Contains(string(files["geraeteakte.html"]), "Synthetic instruction acceptance <img>") {
		t.Fatal("missing or unsafe acceptance archive")
	}
	unbind := usageProof("unbind", 1)
	unbind.ExpectedDeviceVersion = 2
	unbind.UserID = f.user
	usagePost(t, f, id, "accepted-instruction-unbind", unbind)
	if annex1Status(t, f, id, "annex_1_instruction") != "blocked" {
		t.Fatal("withdrawn binding accepted")
	}
	annex1Bind(t, f, id, 2, 2, 2, f.user)
	if annex1Status(t, f, id, "annex_1_instruction") != "unknown" {
		t.Fatal("rebind revived old acceptance")
	}
	in.ExpectedVersion = 1
	dutyPost(t, f, id, "accept-instruction-after-rebind", in, 200)
	if annex1Status(t, f, id, "annex_1_instruction") != "accepted" {
		t.Fatal("explicit new acceptance not recorded")
	}
	reviewPost(t, f, id, instructionDoc.ID, "accepted-instruction-quarantine", reviewProof(instructionDoc, "quarantine", 2), 200)
	if dutyRead(t, f, id).Status != "reviewed" || annex1Status(t, f, id, "annex_1_instruction") != "unknown" {
		t.Fatal("independent instruction withdrawal ignored")
	}
	reviewPost(t, f, id, instructionDoc.ID, "accepted-instruction-rescan", reviewProof(instructionDoc, "scan", 3), 200)
	reviewPost(t, f, id, instructionDoc.ID, "accepted-instruction-rerelease", reviewProof(instructionDoc, "release", 4), 200)
	if annex1Status(t, f, id, "annex_1_instruction") != "unknown" {
		t.Fatal("rerelease revived old acceptance")
	}
	in.ExpectedVersion = 2
	dutyPost(t, f, id, "accept-instruction-new-release", in, 200)
	in.ExpectedVersion = 3
	in.Rules[0].InstructionAcceptance.Decision = "rejected"
	dutyPost(t, f, id, "reject-instruction", in, 200)
	if annex1Status(t, f, id, "annex_1_instruction") != "blocked" || annex1Status(t, f, id, "annex_1") != "blocked" {
		t.Fatal("rejection ignored")
	}
	in.ExpectedVersion = 4
	in.Rules[0].InstructionAcceptance.Decision = "accepted"
	dutyPost(t, f, id, "reaccept-instruction", in, 200)
	correction := controlProof()
	correction.ExpectedVersion = 2
	correction.Correction = &deviceCorrection{TargetVersion: 2, Statement: "Synthetic instruction correction"}
	requireStatus(t, packHTTP(f, "POST", "devices/"+id+"/corrections", "acceptance-instruction-correction", correction), 200)
	if annex1Status(t, f, id, "annex_1_instruction") != "unknown" {
		t.Fatal("corrected instruction accepted")
	}
	if replay := dutyPost(t, f, id, "accept-instruction", first.Input, 200); replay.ID != first.ID || *replay.InstructionAcceptanceBasis != *first.InstructionAcceptanceBasis {
		t.Fatal("historical retry changed")
	}
	defect := controlProof()
	defect.ExpectedVersion = 3
	defect.BlockReason = "defect"
	requireStatus(t, packHTTP(f, "POST", "devices/"+id+"/defects", "corrected-instruction-independent-block", defect), 200)
	if annex1Status(t, f, id, "annex_1_instruction") != "blocked" {
		t.Fatal("corrected proof hid independent block")
	}
	if err := SetSiteRole(f.db, "viewer", []string{demoSite}, "viewer"); err != nil {
		t.Fatal(err)
	}
	f.cookie, f.csrf = loginTest(t, f.handler, nil)
	dutyPost(t, f, id, "accept-instruction", first.Input, 404)
}

func TestInstructionAcceptanceValidationAndIndependentBlocks(t *testing.T) {
	f, id, source := annex1Fixture(t)
	annex1Bind(t, f, id, 0, 2, 2, f.user)
	for _, variant := range []string{"unchecked", "early", "future", "decision", "qualification", "assignment", "evidence", "note", "trainer", "person-evidence", "scope", "no-person", "no-instruction", "wrong-kind", "exempt"} {
		in := annex1Proof(source, f.user, 1, 2)
		in.ExpectedDeviceVersion = 2
		a := instructionAcceptanceProof()
		in.Rules[0].InstructionAcceptance = a
		switch variant {
		case "unchecked":
			a.Reviewed = false
		case "early":
			a.PerformedOn = "2024-01-01"
		case "future":
			a.PerformedOn = "2099-01-01"
		case "decision":
			a.Decision = "implicit"
		case "qualification":
			a.Qualification = ""
		case "assignment":
			a.Assignment = ""
		case "evidence":
			a.Evidence = ""
		case "note":
			a.Note = ""
		case "trainer":
			a.TrainerEvidence = ""
		case "person-evidence":
			a.PersonEvidence = ""
		case "scope":
			a.ScopeEvidence = ""
		case "no-person":
			in.Rules[0].DesignatedUserID = ""
			in.Rules[0].DesignatedAssignment = ""
		case "no-instruction":
			in.Rules[0].InstructionVersion = 0
		case "wrong-kind":
			in.Rules[0].InstructionAcceptance = nil
			in.Rules[1].InstructionAcceptance = a
		case "exempt":
			in.Rules[0].Applicability = "exempt"
		}
		dutyPost(t, f, id, "invalid-instruction-acceptance-"+variant, in, 400)
	}
	if dutyRead(t, f, id).Version != 0 {
		t.Fatal("invalid acceptance persisted")
	}
	in := annex1Proof(source, f.user, 1, 2)
	in.ExpectedDeviceVersion = 2
	in.Rules[0].InstructionAcceptance = instructionAcceptanceProof()
	dutyPost(t, f, id, "instruction-accept-before-block", in, 200)
	defect := controlProof()
	defect.ExpectedVersion = 2
	defect.BlockReason = "defect"
	requireStatus(t, packHTTP(f, "POST", "devices/"+id+"/defects", "instruction-accept-independent-block", defect), 200)
	if annex1Status(t, f, id, "annex_1_instruction") != "blocked" {
		t.Fatal("acceptance released independent block")
	}
	in.ExpectedVersion = 1
	in.ExpectedDeviceVersion = 3
	dutyPost(t, f, id, "instruction-accept-blocked", in, 409)
	// The acceptance itself is organizational; private usage evidence stays out of reports.
	w := packHTTP(f, "GET", "devices/"+id+"/product-duties", "", nil)
	requireStatus(t, w, 200)
	if strings.Contains(w.Body.String(), "usage_events") {
		t.Fatal("private usage history leaked")
	}
}
