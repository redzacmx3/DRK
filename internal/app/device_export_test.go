package app

import (
	"encoding/csv"
	"fmt"
	"strings"
	"testing"
)

func exportRows(t *testing.T, f stockFixture) [][]string {
	t.Helper()
	w := packHTTP(f, "GET", "devices/export.csv", "", nil)
	requireStatus(t, w, 200)
	if w.Header().Get("Content-Type") != "text/csv; charset=utf-8" || !strings.Contains(w.Header().Get("Content-Disposition"), "attachment;") || w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("X-Data-As-Of") == "" {
		t.Fatal(w.Header())
	}
	if !strings.HasPrefix(w.Body.String(), "\xef\xbb\xbf") {
		t.Fatal("UTF8 BOM missing")
	}
	r := csv.NewReader(strings.NewReader(strings.TrimPrefix(w.Body.String(), "\xef\xbb\xbf")))
	r.Comma = ';'
	rows, err := r.ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	return rows
}
func TestDeviceExportCurrentMetadataBlocksScopedReadAndNoMutation(t *testing.T) {
	f, id := controlFixture(t)
	details := detailsInput(0)
	details.Details.Name = "=HYPERLINK(\"https://example.invalid\")"
	requireStatus(t, packHTTP(f, "POST", "devices/"+id+"/details", "export-details-0001", details), 200)
	commission := controlProof()
	commission.ExpectedVersion = 1
	commission.PerformedOn = "2025-01-01"
	requireStatus(t, packHTTP(f, "POST", "devices/"+id+"/commission", "export-commission1", commission), 200)
	details.ExpectedVersion = 2
	details.Details.Model = "Corrected; model"
	requireStatus(t, packHTTP(f, "POST", "devices/"+id+"/details", "export-details-0002", details), 200)
	before := packHTTP(f, "GET", "devices/"+id+"/events", "", nil).Body.String()
	rows := exportRows(t, f)
	if len(rows) != 2 || rows[1][6] != "'"+details.Details.Name || rows[1][8] != "Corrected; model" || rows[1][9] != "'000123" || rows[1][10] != "3" || rows[1][11] != "2025-01-01" || !strings.Contains(rows[1][19], "quarantine") || !strings.Contains(rows[1][21], "ungeklärt") {
		t.Fatal(rows)
	}
	if after := packHTTP(f, "GET", "devices/"+id+"/events", "", nil).Body.String(); after != before {
		t.Fatal("export changed history")
	}
	requireStatus(t, packHTTP(stockFixture{handler: f.handler}, "GET", "devices/export.csv", "", nil), 401)
	if err := SetSiteRole(f.db, "viewer", []string{demoSite}, "viewer"); err != nil {
		t.Fatal(err)
	}
	f.cookie, f.csrf = loginTest(t, f.handler, nil)
	if len(exportRows(t, f)) != 2 {
		t.Fatal("viewer cannot export")
	}
	if err := SetSiteRole(f.db, "viewer", nil, "viewer"); err != nil {
		t.Fatal(err)
	}
	f.cookie, f.csrf = loginTest(t, f.handler, nil)
	if len(exportRows(t, f)) != 1 {
		t.Fatal("foreign scope leaked")
	}
}
func TestDeviceExportRefusesPartialDataset(t *testing.T) {
	f, _ := controlFixture(t)
	tx, err := f.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	statement, err := tx.Prepare(`INSERT INTO devices VALUES(?,?,?,?,?,?,?,?,?,?)`)
	if err != nil {
		t.Fatal(err)
	}
	for n := 0; n < 1000; n++ {
		if _, err = statement.Exec(fmt.Sprintf("export-id-%d", n), warehouse, fmt.Sprintf("export-inv-%d", n), "Synthetic", "Maker", "Model", "Serial", f.user, "2026-01-01T00:00:00Z", "Synthetic fixture"); err != nil {
			t.Fatal(err)
		}
	}
	statement.Close()
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	w := packHTTP(f, "GET", "devices/export.csv", "", nil)
	requireStatus(t, w, 413)
	if strings.Contains(w.Header().Get("Content-Type"), "csv") || strings.HasPrefix(w.Body.String(), "\xef\xbb\xbf") {
		t.Fatal("partial CSV exposed")
	}
}
func TestCSVSpreadsheetFormulaProtection(t *testing.T) {
	for _, v := range []string{"=1+1", "+CMD", "-1+2", "@SUM(1)", " \t=1", "\tplain", "\rplain", "\nplain"} {
		if csvText(v) != "'"+v {
			t.Fatalf("unprotected %q", v)
		}
	}
	for _, v := range []string{"Maker", "000123", "normal; \"quoted\"", ""} {
		if csvText(v) != v {
			t.Fatal(v)
		}
	}
	if csvIdentifier("") != "" || csvIdentifier("000123") != "'000123" {
		t.Fatal("identifier text protection")
	}
}
