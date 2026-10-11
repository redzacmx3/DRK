package app

import (
	"bufio"
	"context"
	"encoding/binary"
	"encoding/json"
	"io"
	"net"
	"strings"
	"testing"
	"time"
)

func fakeClamd(t *testing.T, now time.Time, reply string) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	go func() {
		for {
			c, err := listener.Accept()
			if err != nil {
				return
			}
			func() {
				defer c.Close()
				c.SetDeadline(time.Now().Add(3 * time.Second))
				reader := bufio.NewReader(c)
				command, err := reader.ReadString(0)
				if err != nil {
					return
				}
				switch command {
				case "zVERSION\x00":
					io.WriteString(c, "ClamAV 1.4.3/28000/"+now.UTC().Format("Mon Jan _2 15:04:05 2006")+"\x00")
				case "zINSTREAM\x00":
					for {
						var size uint32
						if binary.Read(reader, binary.BigEndian, &size) != nil {
							return
						}
						if size == 0 {
							break
						}
						if size > 65536 {
							return
						}
						if _, err := io.CopyN(io.Discard, reader, int64(size)); err != nil {
							return
						}
					}
					io.WriteString(c, reply+"\x00")
				}
			}()
		}
	}()
	return listener.Addr().String()
}
func reviewProof(document documentRecord, action string, version int) documentReviewInput {
	return documentReviewInput{ExpectedVersion: version, SHA256: document.SHA256, Action: action, Evidence: "Synthetic review reference", Qualification: "Synthetic reviewer qualification", Assignment: "Synthetic operator assignment", Note: "Synthetic documented decision"}
}
func reviewPost(t *testing.T, f stockFixture, id, document, key string, in documentReviewInput, status int) documentReview {
	t.Helper()
	w := packHTTP(f, "POST", "devices/"+id+"/documents/"+document+"/reviews", key, in)
	requireStatus(t, w, status)
	var out documentReview
	if status == 200 {
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
	}
	return out
}
func TestDocumentReviewScanReleaseRevokeScopeAndAtomicity(t *testing.T) {
	now := time.Now()
	t.Setenv("DRK_CLAMD_ADDRESS", fakeClamd(t, now, "stream: OK"))
	f, id, document := documentFixture(t)
	path := "devices/" + id + "/documents/" + document.ID
	release := reviewProof(document, "release", 0)
	reviewPost(t, f, id, document.ID, "release-without-scan", release, 409)
	scan := reviewProof(document, "scan", 0)
	out := reviewPost(t, f, id, document.ID, "clean-document-scan", scan, 200)
	if out.Scan.Result != "clean" || out.Status != "quarantined" {
		t.Fatal(out)
	}
	release.ExpectedVersion = 1
	f.db.Exec(`CREATE TRIGGER fail_review BEFORE INSERT ON document_review_requests BEGIN SELECT RAISE(ABORT,'synthetic failure'); END`)
	reviewPost(t, f, id, document.ID, "atomic-document-release", release, 503)
	var version int
	f.db.QueryRow(`SELECT MAX(version) FROM document_reviews`).Scan(&version)
	if version != 1 {
		t.Fatal("partial commit")
	}
	f.db.Exec(`DROP TRIGGER fail_review`)
	released := reviewPost(t, f, id, document.ID, "atomic-document-release", release, 200)
	if released.Status != "released" {
		t.Fatal(released)
	}
	requireStatus(t, packHTTP(f, "GET", path+"/download", "", nil), 200)
	reviewPost(t, f, id, document.ID, "stale-document-release", release, 409)
	if _, err := f.db.Exec(`UPDATE document_reviews SET status='released'`); err == nil {
		t.Fatal("mutable review")
	}
	if err := SetSiteRole(f.db, "viewer", []string{demoSite}, "viewer"); err != nil {
		t.Fatal(err)
	}
	f.cookie, f.csrf = loginTest(t, f.handler, nil)
	requireStatus(t, packHTTP(f, "GET", path+"/download", "", nil), 200)
	reviewPost(t, f, id, document.ID, "atomic-document-release", release, 404)
	SetSiteRole(f.db, "viewer", []string{demoSite}, "device_manager")
	f.cookie, f.csrf = loginTest(t, f.handler, nil)
	reviewPost(t, f, id, document.ID, "quarantine-document", reviewProof(document, "quarantine", 2), 200)
	requireStatus(t, packHTTP(f, "GET", path+"/download", "", nil), 409)
	replay := reviewPost(t, f, id, document.ID, "atomic-document-release", release, 200)
	if replay.ID != released.ID {
		t.Fatal("replay changed")
	}
	requireStatus(t, packHTTP(f, "GET", path+"/reviews", "", nil), 200)
	archive := packHTTP(f, "GET", "devices/"+id+"/export.zip", "", nil)
	requireStatus(t, archive, 200)
}
func TestDocumentScannerFailsClosed(t *testing.T) {
	now := time.Now()
	for _, test := range []struct {
		name, reply, want string
		age               time.Duration
	}{
		{"clean", "stream: OK", "clean", 0}, {"infected", "stream: Synthetic-Signature FOUND", "infected", 0}, {"error", "stream: timeout ERROR", "unverified", 0}, {"malformed", "some: OK", "unverified", 0}, {"stale", "stream: OK", "unverified", 96 * time.Hour},
		{"future", "stream: OK", "unverified", -2 * time.Hour},
	} {
		t.Run(test.name, func(t *testing.T) {
			got := scanDocument(context.Background(), fakeClamd(t, now.Add(-test.age), test.reply), []byte("Synthetic sample"), now)
			if got.Result != test.want {
				t.Fatal(got)
			}
		})
	}
	if got := scanDocument(context.Background(), "", []byte("sample"), now); got.Result != "unverified" {
		t.Fatal(got)
	}
	t.Setenv("DRK_CLAMD_ADDRESS", fakeClamd(t, now, "stream: Synthetic-Signature FOUND"))
	f, id, document := documentFixture(t)
	scan := reviewPost(t, f, id, document.ID, "infected-document-scan", reviewProof(document, "scan", 0), 200)
	if scan.Scan.Result != "infected" {
		t.Fatal(scan)
	}
	reviewPost(t, f, id, document.ID, "infected-document-release", reviewProof(document, "release", 1), 409)
	rejected := reviewPost(t, f, id, document.ID, "reject-document", reviewProof(document, "reject", 1), 200)
	if rejected.Status != "rejected" {
		t.Fatal(rejected)
	}
	requireStatus(t, packHTTP(f, "GET", "devices/"+id+"/documents/"+document.ID+"/download", "", nil), 409)
}
func TestDocumentReleaseUpdatesUsageWithoutRewritingInstruction(t *testing.T) {
	t.Setenv("DRK_CLAMD_ADDRESS", fakeClamd(t, time.Now(), "stream: OK"))
	f, id, document := documentFixture(t)
	instruction := controlProof()
	instruction.Instructor = "Synthetic trainer"
	instruction.Trainee = "Synthetic person"
	instruction.PerformedOn = "2024-01-01"
	instruction.DocumentVersion = "Manual 1"
	instruction.DocumentID = document.ID
	instruction.InstructionScope = "device"
	requireStatus(t, packHTTP(f, "POST", "devices/"+id+"/instructions", "review-instruction-proof", instruction), 200)
	policy := usageProof("policy", 0)
	policy.ExpectedDeviceVersion = 1
	policy.InstructionMode = "required"
	policy.Codes = []string{"basic_competence"}
	usagePost(t, f, id, "review-usage-policy", policy)
	qualify := usageProof("qualify", 1)
	qualify.ExpectedDeviceVersion = 1
	qualify.UserID = f.user
	qualify.Code = "basic_competence"
	qualify.ValidFrom = "2020-01-01"
	qualify.ValidUntil = "2099-12-31"
	usagePost(t, f, id, "review-usage-qualify", qualify)
	bind := usageProof("bind", 2)
	bind.ExpectedDeviceVersion = 1
	bind.UserID = f.user
	bind.InstructionVersion = 1
	usagePost(t, f, id, "review-usage-binding", bind)
	if usageRead(t, f, id).Status != "unknown" {
		t.Fatal("quarantine ignored")
	}
	before := packHTTP(f, "GET", "devices/"+id+"/book", "", nil).Body.String()
	reviewPost(t, f, id, document.ID, "usage-document-scan", reviewProof(document, "scan", 0), 200)
	reviewPost(t, f, id, document.ID, "usage-document-release", reviewProof(document, "release", 1), 200)
	if got := usageRead(t, f, id); got.Status != "recorded_requirements_met" || got.Overall != "not_assessed" {
		t.Fatal(got)
	}
	after := packHTTP(f, "GET", "devices/"+id+"/book", "", nil).Body.String()
	if !strings.Contains(after, "quarantined") || before != after {
		t.Fatal("historical instruction snapshot rewritten")
	}
	reviewPost(t, f, id, document.ID, "usage-document-quarantine", reviewProof(document, "quarantine", 2), 200)
	if usageRead(t, f, id).Status != "unknown" {
		t.Fatal("revocation ignored")
	}
}
