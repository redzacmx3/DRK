package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"image"
	"image/png"
	"path/filepath"
	"testing"
)

func documentFixture(t *testing.T) (stockFixture, string, documentRecord) {
	t.Helper()
	f, id := controlFixture(t)
	input := documentInput{Filename: "Synthetic instruction.txt", MediaType: "text/plain", Content: []byte("Synthetic instruction proof\n"), Note: "Synthetic document upload"}
	w := packHTTP(f, "POST", "devices/"+id+"/documents", "document-upload-key", input)
	requireStatus(t, w, 200)
	var document documentRecord
	if err := json.Unmarshal(w.Body.Bytes(), &document); err != nil {
		t.Fatal(err)
	}
	return f, id, document
}
func TestDocumentScopeQuarantineIntegrityAndAtomicity(t *testing.T) {
	f, id, document := documentFixture(t)
	path := "devices/" + id + "/documents"
	input := documentInput{Filename: document.Filename, MediaType: document.MediaType, Content: []byte("Synthetic instruction proof\n"), Note: "Synthetic document upload"}
	replay := packHTTP(f, "POST", path, "document-upload-key", input)
	requireStatus(t, replay, 200)
	var got documentRecord
	json.Unmarshal(replay.Body.Bytes(), &got)
	if got.ID != document.ID || got.Status != "quarantined" {
		t.Fatal(got)
	}
	requireStatus(t, packHTTP(f, "GET", path+"/"+document.ID+"/download", "", nil), 409)
	download := packHTTP(f, "GET", path+"/"+document.ID+"/download?review=true", "", nil)
	requireStatus(t, download, 200)
	if !bytes.Equal(download.Body.Bytes(), input.Content) || download.Header().Get("Content-Type") != "application/octet-stream" || download.Header().Get("X-Content-Type-Options") != "nosniff" || download.Header().Get("Content-Disposition") == "" {
		t.Fatal(download.Header())
	}
	sum := sha256.Sum256(download.Body.Bytes())
	if hex.EncodeToString(sum[:]) != document.SHA256 {
		t.Fatal("wrong checksum")
	}
	input.Content = []byte("Different proof")
	requireStatus(t, packHTTP(f, "POST", path, "document-upload-key", input), 409)
	requireStatus(t, packHTTP(f, "POST", path, "control-device-registration", input), 409)
	if _, err := f.db.Exec(`CREATE TRIGGER reject_document_outbox BEFORE INSERT ON document_outbox BEGIN SELECT RAISE(ABORT,'test rollback'); END`); err != nil {
		t.Fatal(err)
	}
	requireStatus(t, packHTTP(f, "POST", path, "atomic-document-upload", input), 503)
	var count int
	f.db.QueryRow(`SELECT COUNT(*) FROM device_documents`).Scan(&count)
	if count != 1 {
		t.Fatal(count)
	}
	if _, err := f.db.Exec(`UPDATE device_documents SET content=x'00'`); err == nil {
		t.Fatal("mutable attachment")
	}
	if err := SetSiteRole(f.db, "viewer", []string{demoSite}, "viewer"); err != nil {
		t.Fatal(err)
	}
	f.cookie, f.csrf = loginTest(t, f.handler, nil)
	requireStatus(t, packHTTP(f, "GET", path, "", nil), 200)
	requireStatus(t, packHTTP(f, "GET", path+"/"+document.ID+"/download?review=true", "", nil), 404)
	requireStatus(t, packHTTP(f, "POST", path, "viewer-upload-denied", input), 404)
	if err := SetSiteRole(f.db, "viewer", nil, "viewer"); err != nil {
		t.Fatal(err)
	}
	f.cookie, f.csrf = loginTest(t, f.handler, nil)
	requireStatus(t, packHTTP(f, "GET", path, "", nil), 404)
}
func TestDocumentFormatBounds(t *testing.T) {
	valid := documentInput{Filename: "proof.txt", MediaType: "text/plain", Content: []byte("Synthetic\n"), Note: "Test"}
	for _, mutate := range []func(*documentInput){
		func(in *documentInput) { in.Filename = "../proof.txt" }, func(in *documentInput) { in.Filename = "proof\r\n.txt" }, func(in *documentInput) { in.Content = []byte{0} }, func(in *documentInput) { in.Content = []byte{255} }, func(in *documentInput) { in.Content = make([]byte, maxDocument+1) }, func(in *documentInput) { in.MediaType = "text/html" }, func(in *documentInput) { in.MediaType = "application/pdf"; in.Filename = "proof.pdf" }, func(in *documentInput) { in.MediaType = "image/png"; in.Filename = "proof.png" },
	} {
		bad := valid
		mutate(&bad)
		if err := validateDocument(bad); err == nil {
			t.Fatal("accepted", bad.Filename)
		}
	}
	if err := validateDocument(valid); err != nil {
		t.Fatal(err)
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	valid.Filename = "proof.png"
	valid.MediaType = "image/png"
	valid.Content = encoded.Bytes()
	if err := validateDocument(valid); err != nil {
		t.Fatal(err)
	}
	valid.Content = encoded.Bytes()[:len(encoded.Bytes())-10]
	if err := validateDocument(valid); err == nil {
		t.Fatal("accepted truncated PNG")
	}
}
func TestInstructionDocumentBindingAndHistory(t *testing.T) {
	f, id, document := documentFixture(t)
	input := controlProof()
	input.PerformedOn = "2020-03-15"
	input.Instructor = "Synthetic instructor"
	input.Trainee = "Synthetic trainee"
	input.InstructionScope = "device"
	input.DocumentVersion = "Manual revision 2"
	input.DocumentID = document.ID
	w := packHTTP(f, "POST", "devices/"+id+"/instructions", "instruction-proof-key", input)
	requireStatus(t, w, 200)
	var record deviceRecord
	json.Unmarshal(w.Body.Bytes(), &record)
	if len(record.Instructions) != 1 || record.Instructions[0].DocumentSHA256 != document.SHA256 || record.Instructions[0].DocumentStatus != "quarantined" || record.Readiness != "not_assessed" {
		t.Fatal(record)
	}
	replay := packHTTP(f, "POST", "devices/"+id+"/instructions", "instruction-proof-key", input)
	requireStatus(t, replay, 200)
	if replay.Body.String() != w.Body.String() {
		t.Fatal("changed replay")
	}
	input.ExpectedVersion = 1
	input.InstructionScope = "model"
	requireStatus(t, packHTTP(f, "POST", "devices/"+id+"/instructions", "model-transfer-needed", input), 400)
	input.TransferEvidence = "Explicit manufacturer/model transfer evidence"
	requireStatus(t, packHTTP(f, "POST", "devices/"+id+"/instructions", "model-transfer-proven", input), 200)
	input.ExpectedVersion = 2
	input.DocumentID = "not-this-device"
	requireStatus(t, packHTTP(f, "POST", "devices/"+id+"/instructions", "wrong-document-proof", input), 400)
	history := packHTTP(f, "GET", "devices/"+id+"/events", "", nil)
	requireStatus(t, history, 200)
	if !bytes.Contains(history.Body.Bytes(), []byte(document.SHA256)) {
		t.Fatal("document snapshot missing")
	}
}

func TestDocumentSnapshotRestoresContentAndInstruction(t *testing.T) {
	f, id, document := documentFixture(t)
	proof := controlProof()
	proof.PerformedOn = "2020-03-15"
	proof.Instructor = "Instructor"
	proof.Trainee = "Trainee"
	proof.InstructionScope = "device"
	proof.DocumentVersion = "Revision 1"
	proof.DocumentID = document.ID
	requireStatus(t, packHTTP(f, "POST", "devices/"+id+"/instructions", "snapshot-instruction", proof), 200)
	snapshot := filepath.Join(t.TempDir(), "documents.db")
	if _, err := f.db.Exec(`VACUUM INTO ?`, snapshot); err != nil {
		t.Fatal(err)
	}
	restored, err := Open(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	if err = Ready(context.Background(), restored); err != nil {
		t.Fatal(err)
	}
	var integrity string
	if err = restored.QueryRow(`PRAGMA integrity_check`).Scan(&integrity); err != nil || integrity != "ok" {
		t.Fatal(integrity, err)
	}
	rows, err := restored.Query(`PRAGMA foreign_key_check`)
	if err != nil {
		t.Fatal(err)
	}
	if rows.Next() {
		t.Fatal("restore foreign key violation")
	}
	rows.Close()
	fixture := f
	fixture.db = restored
	fixture.handler = Handler(restored)
	download := packHTTP(fixture, "GET", "devices/"+id+"/documents/"+document.ID+"/download?review=true", "", nil)
	requireStatus(t, download, 200)
	sum := sha256.Sum256(download.Body.Bytes())
	if hex.EncodeToString(sum[:]) != document.SHA256 {
		t.Fatal("restored file hash changed")
	}
	book := packHTTP(fixture, "GET", "devices/"+id+"/book", "", nil)
	requireStatus(t, book, 200)
	var record deviceRecord
	json.Unmarshal(book.Body.Bytes(), &record)
	if len(record.Instructions) != 1 || record.Instructions[0].DocumentSHA256 != document.SHA256 {
		t.Fatal("restored proof link changed")
	}
}
