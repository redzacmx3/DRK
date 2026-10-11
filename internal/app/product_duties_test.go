package app

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"
)

func dutyProof(document documentRecord) productDutyInput {
	yes := true
	in := productDutyInput{Action: "review", DocumentID: document.ID, ValidUntil: time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339), Reviewed: &yes, Evidence: "Synthetic <img> product source", Qualification: "Synthetic reviewer qualification", Assignment: "Synthetic operator assignment", Note: "Synthetic assessment"}
	for _, kind := range productDutyKinds {
		in.Rules = append(in.Rules, productDutyRule{kind, "exempt", "Synthetic confirmed exception", ""})
	}
	return in
}
func dutyPost(t *testing.T, f stockFixture, id, key string, in productDutyInput, status int) productDutyEvent {
	t.Helper()
	w := packHTTP(f, "POST", "devices/"+id+"/product-duties", key, in)
	requireStatus(t, w, status)
	var event productDutyEvent
	if status == 200 {
		if err := json.Unmarshal(w.Body.Bytes(), &event); err != nil {
			t.Fatal(err)
		}
	}
	return event
}
func dutyRead(t *testing.T, f stockFixture, id string) productDutyAssessment {
	t.Helper()
	w := packHTTP(f, "GET", "devices/"+id+"/product-duties", "", nil)
	requireStatus(t, w, 200)
	var result struct {
		Assessment productDutyAssessment `json:"assessment"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	return result.Assessment
}
func TestProductDutiesValidationAtomicityRevocationAndDocumentBinding(t *testing.T) {
	t.Setenv("DRK_CLAMD_ADDRESS", fakeClamd(t, time.Now(), "stream: OK"))
	f, id, document := documentFixture(t)
	in := dutyProof(document)
	if dutyRead(t, f, id).Status != "unknown" {
		t.Fatal("missing review accepted")
	}
	dutyPost(t, f, id, "duties-quarantined", in, 409)
	reviewPost(t, f, id, document.ID, "duties-scan", reviewProof(document, "scan", 0), 200)
	reviewPost(t, f, id, document.ID, "duties-release", reviewProof(document, "release", 1), 200)
	for _, variant := range []string{"implicit", "missing", "duplicate", "unsupported", "no-evidence", "long", "empty-reason"} {
		bad := in
		bad.Rules = append([]productDutyRule{}, in.Rules...)
		switch variant {
		case "implicit":
			bad.Reviewed = nil
		case "missing":
			bad.Rules = bad.Rules[:7]
		case "duplicate":
			bad.Rules[0] = bad.Rules[1]
		case "unsupported":
			bad.Rules[0].Kind = "arbitrary"
		case "no-evidence":
			bad.Rules[0].Applicability = "required"
		case "long":
			bad.ValidUntil = time.Now().Add(367 * 24 * time.Hour).Format(time.RFC3339)
		case "empty-reason":
			bad.Rules[0].Reason = ""
		}
		dutyPost(t, f, id, "duties-invalid-"+variant, bad, 400)
	}
	if _, err := f.db.Exec(`CREATE TRIGGER duty_fail BEFORE INSERT ON product_duty_requests BEGIN SELECT RAISE(ABORT,'synthetic failure'); END`); err != nil {
		t.Fatal(err)
	}
	dutyPost(t, f, id, "duties-atomic", in, 503)
	if dutyRead(t, f, id).Version != 0 {
		t.Fatal("partial event")
	}
	f.db.Exec(`DROP TRIGGER duty_fail`)
	initial := dutyPost(t, f, id, "duties-atomic", in, 200)
	if got := dutyRead(t, f, id); got.Status != "reviewed" || got.Overall != "not_assessed" {
		t.Fatal(got)
	}
	dutyPost(t, f, id, "duties-stale", in, 409)
	tx, _ := f.db.Begin()
	record, err := readDevice(context.Background(), tx, f.user, id, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	expired, err := assessProductDuty(context.Background(), tx, f.user, record, time.Now().Add(48*time.Hour))
	tx.Rollback()
	if err != nil || expired.Status != "unknown" {
		t.Fatal(expired, err)
	}
	for _, query := range []string{`UPDATE product_duty_events SET version=2`, `DELETE FROM product_duty_events`, `DELETE FROM product_duty_requests`} {
		if _, err := f.db.Exec(query); err == nil {
			t.Fatal("mutable evidence", query)
		}
	}
	w := packHTTP(f, "GET", "devices/"+id+"/export.zip", "", nil)
	requireStatus(t, w, 200)
	z, err := zip.NewReader(bytes.NewReader(w.Body.Bytes()), int64(w.Body.Len()))
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range z.File {
		r, _ := file.Open()
		data, _ := io.ReadAll(r)
		r.Close()
		if file.Name == "akte.json" {
			var book deviceBookExport
			json.Unmarshal(data, &book)
			if len(book.ProductDuties) != 1 {
				t.Fatal("missing archived review")
			}
		}
		if file.Name == "geraeteakte.html" {
			if !bytes.Contains(data, []byte("Besondere Produktpflichten")) || bytes.Contains(data, []byte("Synthetic <img>")) {
				t.Fatal("missing/unsafe HTML")
			}
		}
	}
	reviewPost(t, f, id, document.ID, "duties-quarantine", reviewProof(document, "quarantine", 2), 200)
	if got := dutyRead(t, f, id); got.Status != "unknown" {
		t.Fatal("withdrawn source accepted", got)
	}
	revoke := productDutyInput{Action: "revoke", ExpectedVersion: 1, Evidence: in.Evidence, Qualification: in.Qualification, Assignment: in.Assignment, Note: "Synthetic revocation"}
	dutyPost(t, f, id, "duties-revoke", revoke, 200)
	if dutyRead(t, f, id).Status != "blocked" {
		t.Fatal("revocation ignored")
	}
	if replay := dutyPost(t, f, id, "duties-atomic", in, 200); replay.ID != initial.ID {
		t.Fatal("retry not identical")
	}
	other := in
	other.Note = "different request"
	dutyPost(t, f, id, "duties-atomic", other, 409)
	SetSiteRole(f.db, "viewer", []string{demoSite}, "viewer")
	f.cookie, f.csrf = loginTest(t, f.handler, nil)
	dutyPost(t, f, id, "duties-atomic", in, 404)
	if dutyRead(t, f, id).Status != "blocked" {
		t.Fatal("scoped reader cannot read")
	}
	f.db.Exec(`DELETE FROM role_assignments WHERE user_id=?`, f.user)
	requireStatus(t, packHTTP(f, "GET", "devices/"+id+"/product-duties", "", nil), 404)
}
func TestProductDutiesUnknownIdentityAndOperatingAcceptanceRemainSeparate(t *testing.T) {
	t.Setenv("DRK_CLAMD_ADDRESS", fakeClamd(t, time.Now(), "stream: OK"))
	f, id, document := documentFixture(t)
	reviewPost(t, f, id, document.ID, "unknown-duty-scan", reviewProof(document, "scan", 0), 200)
	reviewPost(t, f, id, document.ID, "unknown-duty-release", reviewProof(document, "release", 1), 200)
	in := dutyProof(document)
	in.Rules[0].Applicability = "unknown"
	dutyPost(t, f, id, "unknown-duty-review", in, 200)
	if got := dutyRead(t, f, id); got.Status != "unknown" || !strings.Contains(strings.Join(got.Reasons, " "), "annex_1") {
		t.Fatal(got)
	}
	in.Rules[0].Applicability = "required"
	in.Rules[0].Evidence = "Synthetic functional/instruction references"
	in.ExpectedVersion = 1
	dutyPost(t, f, id, "required-duty-review", in, 200)
	if dutyRead(t, f, id).Status != "reviewed" {
		t.Fatal("complete applicability not recorded")
	}
	prepared := packAssessment{Material: materialReadiness{CurrentVersion: true}, Devices: []deviceAssessment{{ProductDuties: &productDutyAssessment{Status: "reviewed"}}}}
	for _, component := range consolidateEvidence(prepared).Components {
		if component.Kind == "product_duties" && component.Status != "unknown" {
			t.Fatal("review treated as acceptance")
		}
	}
	prepared.Devices[0].ProductDuties.Status = "blocked"
	for _, component := range consolidateEvidence(prepared).Components {
		if component.Kind == "product_duties" && component.Status != "blocked" {
			t.Fatal("revocation not consolidated")
		}
	}
	tx, _ := f.db.Begin()
	record, _ := readDevice(context.Background(), tx, f.user, id, time.Now())
	record.SerialOrLot = "changed"
	assessment, err := assessProductDuty(context.Background(), tx, f.user, record, time.Now())
	tx.Rollback()
	if err != nil || assessment.Status != "unknown" {
		t.Fatal(assessment, err)
	}
	in.ExpectedVersion = 2
	in.ExpectedDeviceVersion = 99
	dutyPost(t, f, id, "changed-duty-device", in, 409)
	// The request namespace is shared with other mutation types.
	w := packHTTP(f, "POST", "devices/"+id+"/manual-access", "required-duty-review", accessProof(document, f.user, 0))
	requireStatus(t, w, 409)
}
