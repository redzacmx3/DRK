package app

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func annex1Proof(document documentRecord, user string, commission, instruction int) productDutyInput {
	in := dutyProof(document)
	for i := range in.Rules {
		if in.Rules[i].Kind == "annex_1" {
			in.Rules[i].Applicability = "required"
			in.Rules[i].Evidence = "Synthetic annex1 product requirement"
			in.Rules[i].CommissionVersion = commission
			in.Rules[i].InstructionVersion = instruction
			if user != "" {
				in.Rules[i].DesignatedUserID = user
				in.Rules[i].DesignatedAssignment = "Synthetic explicit organizational designation"
			}
		}
	}
	return in
}
func annex1Status(t *testing.T, f stockFixture, id, kind string) string {
	t.Helper()
	for _, check := range dutyRead(t, f, id).Checks {
		if check.Kind == kind {
			return check.Status
		}
	}
	t.Fatal("missing annex1 check", kind)
	return ""
}
func annex1Fixture(t *testing.T) (stockFixture, string, documentRecord) {
	t.Helper()
	t.Setenv("DRK_CLAMD_ADDRESS", fakeClamd(t, time.Now(), "stream: OK"))
	f, id, document := documentFixture(t)
	reviewPost(t, f, id, document.ID, "annex1-source-scan", reviewProof(document, "scan", 0), 200)
	reviewPost(t, f, id, document.ID, "annex1-source-release", reviewProof(document, "release", 1), 200)
	commission := controlProof()
	commission.PerformedOn = "2024-01-01"
	requireStatus(t, packHTTP(f, "POST", "devices/"+id+"/commission", "annex1-commission", commission), 200)
	instruction := controlProof()
	instruction.ExpectedVersion = 1
	instruction.PerformedOn = "2024-01-02"
	instruction.Instructor = "Synthetic qualified trainer"
	instruction.Trainee = "Free text is not an account"
	instruction.InstructionScope = "device"
	instruction.DocumentID = document.ID
	instruction.DocumentVersion = "Synthetic manual 1"
	requireStatus(t, packHTTP(f, "POST", "devices/"+id+"/instructions", "annex1-instruction", instruction), 200)
	return f, id, document
}
func annex1Bind(t *testing.T, f stockFixture, id string, version, deviceVersion, instruction int, user string) {
	t.Helper()
	in := usageProof("bind", version)
	in.ExpectedDeviceVersion = deviceVersion
	in.InstructionVersion = instruction
	in.UserID = user
	usagePost(t, f, id, "annex1-bind-"+newID(), in)
}
func TestAnnex1ExplicitCommissionInstructionPersonAndWithdrawal(t *testing.T) {
	f, id, document := annex1Fixture(t)
	in := annex1Proof(document, f.user, 1, 2)
	in.ExpectedDeviceVersion = 2
	first := dutyPost(t, f, id, "annex1-explicit-review", in, 200)
	if annex1Status(t, f, id, "annex_1_commission") != "record_found" || annex1Status(t, f, id, "annex_1_instruction") != "unknown" {
		t.Fatal("free text inferred as person binding")
	}
	annex1Bind(t, f, id, 0, 2, 2, f.user)
	if annex1Status(t, f, id, "annex_1_instruction") != "record_found" || annex1Status(t, f, id, "annex_1") != "unknown" || dutyRead(t, f, id).Overall != "not_assessed" {
		t.Fatal("missing record or false functional release")
	}
	book, files := bookArchive(t, f, id)
	found := false
	for _, check := range book.ProductDutyReview.Checks {
		if check.Kind == "annex_1_instruction" && check.EventVersion == 2 && check.Status == "record_found" {
			found = true
		}
	}
	if !found || !strings.Contains(string(files["geraeteakte.html"]), "annex_1_instruction") {
		t.Fatal("missing archive checks")
	}
	unbind := usageProof("unbind", 1)
	unbind.ExpectedDeviceVersion = 2
	unbind.UserID = f.user
	usagePost(t, f, id, "annex1-unbind", unbind)
	if annex1Status(t, f, id, "annex_1_instruction") != "blocked" || annex1Status(t, f, id, "annex_1") != "blocked" {
		t.Fatal("withdrawn assignment accepted")
	}
	annex1Bind(t, f, id, 2, 2, 2, f.user)
	reviewPost(t, f, id, document.ID, "annex1-withdraw-source", reviewProof(document, "quarantine", 2), 200)
	if got := dutyRead(t, f, id); got.Status != "unknown" || len(got.Checks) != 0 {
		t.Fatal("source withdrawal ignored", got)
	}
	reviewPost(t, f, id, document.ID, "annex1-source-rescan", reviewProof(document, "scan", 3), 200)
	reviewPost(t, f, id, document.ID, "annex1-source-rerelease", reviewProof(document, "release", 4), 200)
	correction := controlProof()
	correction.ExpectedVersion = 2
	correction.Correction = &deviceCorrection{TargetVersion: 2, Statement: "Synthetic substantive instruction correction"}
	requireStatus(t, packHTTP(f, "POST", "devices/"+id+"/corrections", "annex1-correct-instruction", correction), 200)
	if annex1Status(t, f, id, "annex_1_instruction") != "unknown" {
		t.Fatal("corrected instruction accepted")
	}
	if replay := dutyPost(t, f, id, "annex1-explicit-review", in, 200); replay.ID != first.ID {
		t.Fatal("original retry changed")
	}
	SetSiteRole(f.db, "viewer", []string{demoSite}, "viewer")
	f.cookie, f.csrf = loginTest(t, f.handler, nil)
	dutyPost(t, f, id, "annex1-explicit-review", in, 404)
	w := packHTTP(f, "GET", "devices/"+id+"/product-duties", "", nil)
	requireStatus(t, w, 200)
	var out struct {
		People []dutyPersonChoice `json:"people"`
	}
	json.Unmarshal(w.Body.Bytes(), &out)
	if len(out.People) != 0 {
		t.Fatal("manager-only person selector leaked")
	}
	if strings.Contains(w.Body.String(), "usage_events") || strings.Contains(w.Body.String(), "basic_competence") {
		t.Fatal("private qualification history leaked")
	}
}
func TestAnnex1InvalidReferencesInactivePersonAndIndependentBlocks(t *testing.T) {
	f, id, document := annex1Fixture(t)
	in := annex1Proof(document, f.user, 1, 2)
	in.ExpectedDeviceVersion = 2
	for _, variant := range []string{"wrong-commission", "wrong-instruction", "negative", "wrong-kind", "exempt", "foreign-person", "assignment-missing", "person-without-event"} {
		bad := in
		bad.Rules = append([]productDutyRule{}, in.Rules...)
		rule := &bad.Rules[0]
		switch variant {
		case "wrong-commission":
			rule.CommissionVersion = 2
		case "wrong-instruction":
			rule.InstructionVersion = 1
		case "negative":
			rule.CommissionVersion = -1
		case "wrong-kind":
			rule.Kind = "other"
			bad.Rules[7].Kind = "annex_1"
		case "exempt":
			rule.Applicability = "exempt"
		case "foreign-person":
			rule.DesignatedUserID = "foreign-unassigned"
		case "assignment-missing":
			rule.DesignatedAssignment = ""
		case "person-without-event":
			rule.InstructionVersion = 0
		}
		dutyPost(t, f, id, "invalid-annex1-"+variant, bad, 400)
	}
	annex1Bind(t, f, id, 0, 2, 2, f.user)
	dutyPost(t, f, id, "annex1-original", in, 200)
	defect := controlProof()
	defect.ExpectedVersion = 2
	defect.BlockReason = "defect"
	requireStatus(t, packHTTP(f, "POST", "devices/"+id+"/defects", "annex1-independent-block", defect), 200)
	if annex1Status(t, f, id, "annex_1_instruction") != "blocked" || annex1Status(t, f, id, "annex_1_commission") != "blocked" {
		t.Fatal("independent block ignored")
	}
	// A different stable subject may be selected explicitly, but is never inferred.
	if err := CreateUser(f.db, "annex1-person", "SyntheticPassword123", demoSite, "viewer"); err != nil {
		t.Fatal(err)
	}
	var person string
	f.db.QueryRow(`SELECT id FROM users WHERE username='annex1-person'`).Scan(&person)
	alternate := annex1Proof(document, person, 1, 2)
	alternate.ExpectedVersion = 1
	alternate.ExpectedDeviceVersion = 3
	dutyPost(t, f, id, "annex1-other-person", alternate, 200)
	f.db.Exec(`UPDATE users SET active=0 WHERE id=?`, person)
	if annex1Status(t, f, id, "annex_1_instruction") != "blocked" {
		t.Fatal("inactive designated person accepted")
	}
	corrected := controlProof()
	corrected.ExpectedVersion = 3
	corrected.Correction = &deviceCorrection{TargetVersion: 2, Statement: "Synthetic corrected instruction with independently inactive designated account"}
	requireStatus(t, packHTTP(f, "POST", "devices/"+id+"/corrections", "annex1-inactive-corrected", corrected), 200)
	if annex1Status(t, f, id, "annex_1_instruction") != "blocked" {
		t.Fatal("correction erased independent inactive-account block")
	}
	alternate.ExpectedDeviceVersion = 4
	alternate.ExpectedVersion = 2
	dutyPost(t, f, id, "annex1-inactive-new-review", alternate, 400)
}

func TestAnnex1OrganizationalReportDoesNotExposePrivateUsageHistory(t *testing.T) {
	f, id, document := annex1Fixture(t)
	// A separate product basis keeps the instruction-document withdrawal independently visible.
	w := packHTTP(f, "POST", "devices/"+id+"/documents", "annex1-separate-product-source", documentInput{ExpectedVersion: 2, Filename: "Product basis.txt", MediaType: "text/plain", Content: []byte("Synthetic product basis"), Note: "Synthetic separate product classification"})
	requireStatus(t, w, 200)
	var source documentRecord
	json.Unmarshal(w.Body.Bytes(), &source)
	reviewPost(t, f, id, source.ID, "annex1-product-scan", reviewProof(source, "scan", 0), 200)
	reviewPost(t, f, id, source.ID, "annex1-product-release", reviewProof(source, "release", 1), 200)
	if err := CreateUser(f.db, "designated-other", "SyntheticPassword123", demoSite, "viewer"); err != nil {
		t.Fatal(err)
	}
	var person string
	f.db.QueryRow(`SELECT id FROM users WHERE username='designated-other'`).Scan(&person)
	bind := usageProof("bind", 0)
	bind.UserID = person
	bind.ExpectedDeviceVersion = 2
	bind.InstructionVersion = 2
	bind.Evidence = "PRIVATE_USAGE_REFERENCE_SENTINEL"
	usagePost(t, f, id, "annex1-private-bind", bind)
	in := annex1Proof(source, person, 1, 2)
	in.ExpectedDeviceVersion = 2
	dutyPost(t, f, id, "annex1-organizational-review", in, 200)
	SetSiteRole(f.db, "viewer", []string{demoSite}, "viewer")
	f.cookie, f.csrf = loginTest(t, f.handler, nil)
	if annex1Status(t, f, id, "annex_1_instruction") != "record_found" {
		t.Fatal("scoped organizational read failed")
	}
	w = packHTTP(f, "GET", "devices/"+id+"/product-duties", "", nil)
	requireStatus(t, w, 200)
	if strings.Contains(w.Body.String(), "PRIVATE_USAGE_REFERENCE_SENTINEL") {
		t.Fatal("private usage evidence leaked")
	}
	requireStatus(t, packHTTP(f, "GET", "devices/"+id+"/usage?user_id="+person, "", nil), 404)
	_, files := bookArchive(t, f, id)
	for _, data := range files {
		if strings.Contains(string(data), "PRIVATE_USAGE_REFERENCE_SENTINEL") {
			t.Fatal("private usage history exported")
		}
	}
	SetSiteRole(f.db, "viewer", []string{demoSite}, "device_manager")
	f.cookie, f.csrf = loginTest(t, f.handler, nil)
	reviewPost(t, f, id, document.ID, "annex1-instruction-only-withdrawal", reviewProof(document, "quarantine", 2), 200)
	if dutyRead(t, f, id).Status != "reviewed" || annex1Status(t, f, id, "annex_1_instruction") != "unknown" {
		t.Fatal("independent instruction document withdrawal missed")
	}
	correction := controlProof()
	correction.ExpectedVersion = 2
	correction.Correction = &deviceCorrection{TargetVersion: 1, Statement: "Synthetic correction to commissioning protocol"}
	requireStatus(t, packHTTP(f, "POST", "devices/"+id+"/corrections", "annex1-correct-commission", correction), 200)
	if annex1Status(t, f, id, "annex_1_commission") != "unknown" {
		t.Fatal("corrected commissioning proof accepted")
	}
}
