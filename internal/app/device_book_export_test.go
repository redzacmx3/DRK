package app

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"testing"
)

func bookArchive(t *testing.T, f stockFixture, id string) (deviceBookExport, map[string][]byte) {
	t.Helper()
	w := packHTTP(f, "GET", "devices/"+id+"/export.zip", "", nil)
	requireStatus(t, w, 200)
	if w.Header().Get("Content-Type") != "application/zip" || w.Header().Get("Cache-Control") != "no-store" || !strings.Contains(w.Header().Get("Content-Disposition"), "attachment;") || w.Header().Get("X-Data-As-Of") == "" {
		t.Fatal(w.Header())
	}
	z, err := zip.NewReader(bytes.NewReader(w.Body.Bytes()), int64(w.Body.Len()))
	if err != nil {
		t.Fatal(err)
	}
	files := map[string][]byte{}
	for _, file := range z.File {
		r, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(r)
		r.Close()
		if err != nil {
			t.Fatal(err)
		}
		files[file.Name] = data
	}
	if len(files) != 3 {
		t.Fatal(files)
	}
	var book deviceBookExport
	if err = json.Unmarshal(files["akte.json"], &book); err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		DeviceID                   string `json:"device_id"`
		DeviceVersion              int    `json:"device_version"`
		AttachmentContentsIncluded bool   `json:"attachment_contents_included"`
		Files                      []struct {
			Name   string `json:"name"`
			Size   int    `json:"size"`
			SHA256 string `json:"sha256"`
		} `json:"files"`
	}
	if err = json.Unmarshal(files["manifest.json"], &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.DeviceID != id || manifest.DeviceVersion != book.Device.Version || manifest.AttachmentContentsIncluded || len(manifest.Files) != 2 {
		t.Fatal(manifest)
	}
	for _, file := range manifest.Files {
		data, ok := files[file.Name]
		sum := sha256.Sum256(data)
		if !ok || len(data) != file.Size || hex.EncodeToString(sum[:]) != file.SHA256 {
			t.Fatal("manifest checksum mismatch", file.Name)
		}
	}
	return book, files
}
func TestDeviceBookExportHistoryHashesQuarantineAndEscaping(t *testing.T) {
	f, id, document := documentFixture(t)
	if _, err := f.db.Exec(`INSERT INTO device_intake VALUES(?,?,?,?,?,?)`, id, "equipment", "unknown", "000987", "Synthetic source", digest("synthetic row")); err != nil {
		t.Fatal(err)
	}
	instruction := controlProof()
	instruction.PerformedOn = "2020-03-15"
	instruction.Instructor = "Synthetic instructor"
	instruction.Trainee = "Synthetic trainee"
	instruction.InstructionScope = "device"
	instruction.DocumentVersion = "Manual v1"
	instruction.DocumentID = document.ID
	requireStatus(t, packHTTP(f, "POST", "devices/"+id+"/instructions", "book-instruction-001", instruction), 200)
	details := detailsInput(1)
	details.Details.Name = "<script>alert(1)</script>"
	details.Note = "Synthetic & quoted correction"
	requireStatus(t, packHTTP(f, "POST", "devices/"+id+"/details", "book-details-000001", details), 200)
	before := packHTTP(f, "GET", "devices/"+id+"/events", "", nil).Body.String()
	book, files := bookArchive(t, f, id)
	if len(book.Registration) != 1 || len(book.Controls) != 2 || len(book.DocumentEvents) != 1 || len(book.Documents) != 1 || len(book.Device.Instructions) != 1 || len(book.Device.ActiveBlocks) != 1 || book.Device.Name != details.Details.Name || book.Operator == "" || book.Site == "" || book.Location == "" {
		t.Fatal(book)
	}
	if book.IntakeProvenance == nil || book.IntakeProvenance.RowHash != digest("synthetic row") || book.IntakeProvenance.ExternalCode != "000987" {
		t.Fatal("intake provenance missing")
	}
	if book.Documents[0].SHA256 != document.SHA256 || book.Documents[0].Status != "quarantined" || !bytes.Contains(book.Controls[0].Proof, []byte(document.SHA256)) {
		t.Fatal("document proof missing")
	}
	var registration deviceRecord
	if err := json.Unmarshal(book.Registration[0].Proof, &registration); err != nil {
		t.Fatal(err)
	}
	if registration.Name != "Demo" {
		t.Fatal("original registration lost")
	}
	html := string(files["geraeteakte.html"])
	if strings.Contains(html, "<script>") || !strings.Contains(html, "&lt;script&gt;") || !strings.Contains(html, "default-src &#39;none&#39;") && !strings.Contains(html, "default-src 'none'") {
		t.Fatal("unsafe HTML")
	}
	if packHTTP(f, "GET", "devices/"+id+"/events", "", nil).Body.String() != before {
		t.Fatal("export changed history")
	}
	requireStatus(t, packHTTP(f, "GET", "devices/"+id+"/documents/"+document.ID+"/download", "", nil), 409)
}
func TestDeviceBookExportScopeAndUntruncatedHistory(t *testing.T) {
	f, id := controlFixture(t)
	payload, _ := json.Marshal(struct{ deviceControlInput }{detailsInput(0)})
	tx, err := f.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	stmt, err := tx.Prepare(`INSERT INTO device_control_events VALUES(?,?,?,?,?,?,?)`)
	if err != nil {
		t.Fatal(err)
	}
	for version := 1; version <= 501; version++ {
		if _, err = stmt.Exec(fmt.Sprintf("book-event-%d", version), id, version, "details", f.user, "2026-01-01T00:00:00Z", string(payload)); err != nil {
			t.Fatal(err)
		}
	}
	stmt.Close()
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	book, _ := bookArchive(t, f, id)
	if len(book.Controls) != 501 || book.Controls[0].Version != 1 || book.Controls[500].Version != 501 || book.Device.Version != 501 {
		t.Fatal("history truncated")
	}
	if err = SetSiteRole(f.db, "viewer", []string{demoSite}, "viewer"); err != nil {
		t.Fatal(err)
	}
	f.cookie, f.csrf = loginTest(t, f.handler, nil)
	bookArchive(t, f, id)
	requireStatus(t, packHTTP(stockFixture{handler: f.handler}, "GET", "devices/"+id+"/export.zip", "", nil), 401)
	requireStatus(t, packHTTP(f, "GET", "devices/missing/export.zip", "", nil), 404)
	if err = SetSiteRole(f.db, "viewer", nil, "viewer"); err != nil {
		t.Fatal(err)
	}
	f.cookie, f.csrf = loginTest(t, f.handler, nil)
	requireStatus(t, packHTTP(f, "GET", "devices/"+id+"/export.zip", "", nil), 404)
}
func TestDeviceBookExportLimitRejectsWholeArchive(t *testing.T) {
	f, id := controlFixture(t)
	payload, _ := json.Marshal(struct{ deviceControlInput }{detailsInput(0)})
	tx, err := f.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	stmt, err := tx.Prepare(`INSERT INTO device_control_events VALUES(?,?,?,?,?,?,?)`)
	if err != nil {
		t.Fatal(err)
	}
	for version := 1; version <= 2001; version++ {
		if _, err = stmt.Exec(fmt.Sprintf("book-large-%d", version), id, version, "details", f.user, "2026-01-01T00:00:00Z", string(payload)); err != nil {
			t.Fatal(err)
		}
	}
	stmt.Close()
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	w := packHTTP(f, "GET", "devices/"+id+"/export.zip", "", nil)
	requireStatus(t, w, 413)
	if bytes.HasPrefix(w.Body.Bytes(), []byte("PK")) || w.Header().Get("Content-Type") == "application/zip" {
		t.Fatal("partial ZIP leaked")
	}
	if err = SetSiteRole(f.db, "viewer", nil, "viewer"); err != nil {
		t.Fatal(err)
	}
	f.cookie, f.csrf = loginTest(t, f.handler, nil)
	requireStatus(t, packHTTP(f, "GET", "devices/"+id+"/export.zip", "", nil), 404)
}
