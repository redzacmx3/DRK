package app

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"image/png"
	"io"
	"strings"
	"testing"
)

func importFixture(t *testing.T) stockFixture {
	t.Helper()
	f := stockDB(t)
	if err := SetSiteRole(f.db, "viewer", []string{demoSite}, "device_manager"); err != nil {
		t.Fatal(err)
	}
	f.cookie, f.csrf = loginTest(t, f.handler, nil)
	t.Cleanup(func() { f.db.Close() })
	return f
}

const minimalIntakeCSV = "Inventarnummer;Bezeichnung;Standort\nCSV-0001;Synthetic device;Offline warehouse\nCSV-0002;Synthetic equipment;Offline warehouse\n"

func importPreview(t *testing.T, f stockFixture, input deviceImportRequest) deviceImportPreview {
	t.Helper()
	response := packHTTP(f, "POST", "device-import/preview", "intake-preview-0001", input)
	requireStatus(t, response, 200)
	var result deviceImportPreview
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	return result
}
func TestDeviceImportPreviewMappingReplayAndNewBatch(t *testing.T) {
	f := importFixture(t)
	input := deviceImportRequest{CSV: minimalIntakeCSV}
	preview := importPreview(t, f, input)
	if preview.Errors != 2 || preview.New != 0 || len(preview.Choices) != 2 {
		t.Fatal(preview)
	}
	var count int
	f.db.QueryRow(`SELECT COUNT(*) FROM devices`).Scan(&count)
	if count != 0 {
		t.Fatal("preview mutated data")
	}
	input.Locations = map[string]string{"Offline warehouse": warehouse}
	preview = importPreview(t, f, input)
	if preview.New != 2 || preview.Errors != 0 {
		t.Fatal(preview)
	}
	input.Basis = preview.Basis
	first := packHTTP(f, "POST", "device-import/commit", "intake-commit-0001", input)
	requireStatus(t, first, 201)
	var result deviceImportResult
	json.Unmarshal(first.Body.Bytes(), &result)
	if result.Created != 2 || result.Unchanged != 0 || len(result.Devices) != 2 {
		t.Fatal(result)
	}
	replay := packHTTP(f, "POST", "device-import/commit", "intake-commit-0001", input)
	requireStatus(t, replay, 201)
	if first.Body.String() != replay.Body.String() {
		t.Fatal("replay changed")
	}
	book := packHTTP(f, "GET", "devices/"+result.Devices[0]+"/book", "", nil)
	requireStatus(t, book, 200)
	var record deviceRecord
	json.Unmarshal(book.Body.Bytes(), &record)
	if record.Intake == nil || record.Intake.Kind != "unknown" || record.Manufacturer != "" || record.Lifecycle != "NOT_COMMISSIONED" || record.Readiness != "not_assessed" || record.Controls["STK"].State != "UNKNOWN" {
		t.Fatal(record)
	}
	preview = importPreview(t, f, input)
	if preview.New != 0 || preview.Unchanged != 2 {
		t.Fatal(preview)
	}
	input.Basis = preview.Basis
	requireStatus(t, packHTTP(f, "POST", "device-import/commit", "intake-new-key-same-file", input), 201)
	input.CSV += "CSV-0003;New batch device;Offline warehouse\n"
	preview = importPreview(t, f, input)
	if preview.New != 1 || preview.Unchanged != 2 {
		t.Fatal(preview)
	}
	input.Basis = preview.Basis
	requireStatus(t, packHTTP(f, "POST", "device-import/commit", "intake-later-batch", input), 201)
	input.CSV = strings.Replace(input.CSV, "Synthetic device", "Changed description", 1)
	preview = importPreview(t, f, input)
	if preview.Errors != 1 || preview.Rows[0].Status != "error" {
		t.Fatal(preview)
	}
	input.Basis = preview.Basis
	requireStatus(t, packHTTP(f, "POST", "device-import/commit", "intake-no-overwrite", input), 400)
	f.db.QueryRow(`SELECT COUNT(*) FROM devices`).Scan(&count)
	if count != 3 {
		t.Fatal(count)
	}
	var name string
	f.db.QueryRow(`SELECT name FROM devices WHERE inventory_number='CSV-0001'`).Scan(&name)
	if name != "Synthetic device" {
		t.Fatal("overwritten", name)
	}
	if _, err := f.db.Exec(`UPDATE device_intake SET external_code='changed'`); err == nil {
		t.Fatal("mutable intake")
	}
}
func TestDeviceImportDefectsAndQuarantineAreIndependentRealBlocks(t *testing.T) {
	f := importFixture(t)
	input := deviceImportRequest{CSV: "Inventarnummer,Art,Bezeichnung,Hersteller,Modell,Seriennummer,Standort,Zustand,Fremdcode,Bemerkung\nCSV-DEF,Gerät,Synthetic defective,,,000123,Demo-Materiallager,defekt,00001234,Observed defect\nCSV-QUAR,Equipment,Synthetic quarantine,,,,Demo-Materiallager,Quarantäne,00001234,Observed quarantine\n"}
	preview := importPreview(t, f, input)
	if preview.New != 2 || preview.Errors != 0 {
		t.Fatal(preview)
	}
	input.Basis = preview.Basis
	w := packHTTP(f, "POST", "device-import/commit", "intake-defect-quarantine", input)
	requireStatus(t, w, 201)
	var result deviceImportResult
	json.Unmarshal(w.Body.Bytes(), &result)
	for i, id := range result.Devices {
		w = packHTTP(f, "GET", "devices/"+id+"/book", "", nil)
		requireStatus(t, w, 200)
		var record deviceRecord
		json.Unmarshal(w.Body.Bytes(), &record)
		if record.Version != 1 || len(record.ActiveBlocks) != 1 || record.CommissionedOn != "" || record.Readiness != "not_assessed" || record.Intake.ExternalCode != "00001234" {
			t.Fatal(record)
		}
		expected := []string{"defect", "quarantine"}[i]
		if record.ActiveBlocks[0].Reason != expected {
			t.Fatal(record)
		}
		release := controlProof()
		release.ExpectedVersion = 1
		release.BlockID = record.ActiveBlocks[0].ID
		deviceResponse(t, f, id, "releases", fmt.Sprintf("intake-release-%d", i), release)
		if i == 0 && record.SerialOrLot != "000123" {
			t.Fatal("lost leading zeros")
		}
	}
	preview = importPreview(t, f, input)
	if preview.Unchanged != 2 {
		t.Fatal("device history changed intake matching", preview)
	}
}
func TestDeviceImportStalePreviewRollbackAndSharedKeys(t *testing.T) {
	f := importFixture(t)
	input := deviceImportRequest{CSV: minimalIntakeCSV, Locations: map[string]string{"Offline warehouse": warehouse}}
	preview := importPreview(t, f, input)
	input.Basis = preview.Basis
	if _, err := f.db.Exec(`CREATE TRIGGER intake_outbox_fail BEFORE INSERT ON device_import_outbox BEGIN SELECT RAISE(ABORT,'outbox unavailable'); END`); err != nil {
		t.Fatal(err)
	}
	requireStatus(t, packHTTP(f, "POST", "device-import/commit", "intake-atomic-rollback", input), 503)
	for _, table := range []string{"devices", "device_intake", "device_events", "device_outbox", "device_imports", "device_import_requests"} {
		var count int
		f.db.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&count)
		if count != 0 {
			t.Fatal(table, count)
		}
	}
	f.db.Exec(`DROP TRIGGER intake_outbox_fail`)
	requireStatus(t, packHTTP(f, "POST", "device-import/commit", "intake-atomic-rollback", input), 201)
	requireStatus(t, packHTTP(f, "POST", "device-import/commit", "intake-stale-new-key", input), 409)
	// Import and all other mutations share the same namespace in both directions.
	device := deviceInput{LocationID: warehouse, InventoryNumber: "OTHER-DEVICE", Name: "Synthetic", Manufacturer: "Demo", Model: "Demo", SerialOrLot: "Demo", Note: "Synthetic"}
	requireStatus(t, packHTTP(f, "POST", "devices", "intake-atomic-rollback", device), 409)
	requireStatus(t, packHTTP(f, "POST", "devices", "intake-cross-key-device", device), 200)
	preview = importPreview(t, f, input)
	input.Basis = preview.Basis
	requireStatus(t, packHTTP(f, "POST", "device-import/commit", "intake-cross-key-device", input), 409)
	input.CSV = strings.Replace(input.CSV, "CSV-0001", "CSV-CHANGED", 1)
	requireStatus(t, packHTTP(f, "POST", "device-import/commit", "intake-atomic-rollback", input), 409)
}
func TestDeviceImportRightsAndScopeDoNotLeakExistingRecords(t *testing.T) {
	f := importFixture(t)
	input := deviceImportRequest{CSV: minimalIntakeCSV, Locations: map[string]string{"Offline warehouse": warehouse}}
	preview := importPreview(t, f, input)
	input.Basis = preview.Basis
	requireStatus(t, packHTTP(f, "POST", "device-import/commit", "intake-scope-replay", input), 201)
	input.Locations["Offline warehouse"] = "outside"
	requireStatus(t, packHTTP(f, "POST", "device-import/preview", "intake-outside-location", input), 404)
	for _, role := range []string{"viewer", "helper", "material_manager"} {
		if err := SetSiteRole(f.db, "viewer", []string{demoSite}, role); err != nil {
			t.Fatal(err)
		}
		f.cookie, f.csrf = loginTest(t, f.handler, nil)
		input.Locations["Offline warehouse"] = warehouse
		requireStatus(t, packHTTP(f, "POST", "device-import/preview", "intake-role-preview", input), 404)
		requireStatus(t, packHTTP(f, "POST", "device-import/commit", "intake-scope-replay", input), 404)
		requireStatus(t, packHTTP(f, "GET", "device-import/template.xlsx", "", nil), 404)
	}
}
func TestIntakeCSVParsingLimitsAndDuplicates(t *testing.T) {
	for _, test := range []struct {
		csv       string
		fileError bool
		rowErrors int
	}{
		{"\ufeffInventarnummer;Bezeichnung;Standort\r\n0000123;\"Gerät; mit Semikolon\";Lager\r\n", false, 0},
		{"Standort,Bezeichnung,Inventarnummer\nLager,Gerät,000001\n", false, 0},
		{"Inventarnummer;Bezeichnung;Standort\nPREPARED;;\nCSV-1;Equipment;Lager\n", false, 0},
		{"Inventarnummer;Bezeichnung;Standort\nCSV-1;A;Lager\ncsv-1;B;Lager\n", false, 2},
		{"Inventarnummer;Bezeichnung;Standort;Zustand\nCSV-1;A;Lager;einsatzbereit\n", false, 1},
		{"Inventarnummer;Bezeichnung;Standort\nCSV-1;;Lager\n", false, 1},
		{"Inventarnummer;Bezeichnung;Standort\nCSV-1;A\n", true, 0},
		{"Inventarnummer;Bezeichnung;Standort\nCSV-1;\"unterminated;Lager\n", true, 0},
		{"Inventarnummer;Bezeichnung;Standort;Bezeichnung\nCSV-1;A;Lager;B\n", true, 0},
		{"Inventarnummer;Bezeichnung;Standort;Freigabe\nCSV-1;A;Lager;Ja\n", true, 0},
		{"\xff\xfeInvalid", true, 0},
		{strings.Repeat("x", maxIntakeCSV+1), true, 0},
	} {
		rows, _, err := parseIntakeCSV(test.csv)
		if (err != nil) != test.fileError {
			t.Fatalf("%q: %v", test.csv[:min(100, len(test.csv))], err)
		}
		count := 0
		for _, row := range rows {
			if row.Status == "error" {
				count++
			}
		}
		if count != test.rowErrors {
			t.Fatal(count, test.rowErrors)
		}
	}
	input := "Inventarnummer;Bezeichnung;Standort\n"
	for i := range 501 {
		input += fmt.Sprintf("CSV-%d;Synthetic;Lager\n", i)
	}
	if _, _, err := parseIntakeCSV(input); err == nil {
		t.Fatal("unbounded rows")
	}
}
func TestDeviceIntakeTemplateHasFixedUniqueIDsAndEmbeddedBarcodes(t *testing.T) {
	seen := map[string]bool{}
	for range 2 {
		body, err := NewDeviceIntakeTemplate()
		if err != nil {
			t.Fatal(err)
		}
		reader, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
		if err != nil {
			t.Fatal(err)
		}
		images := 0
		var shared []string
		var sheet []byte
		for _, file := range reader.File {
			if strings.Contains(file.Name, "externalLink") || strings.Contains(file.Name, "vbaProject") {
				t.Fatal("external or macro dependency")
			}
			source, _ := file.Open()
			content, _ := io.ReadAll(source)
			source.Close()
			if bytes.Contains(content, []byte(intakeTemplatePrefix)) {
				t.Fatal("placeholder ID survived", file.Name)
			}
			if file.Name == "xl/sharedStrings.xml" {
				var values struct {
					Items []struct {
						Text string `xml:"t"`
					} `xml:"si"`
				}
				if err = xml.Unmarshal(content, &values); err != nil {
					t.Fatal(err)
				}
				for _, item := range values.Items {
					shared = append(shared, item.Text)
				}
			}
			if file.Name == "xl/worksheets/sheet2.xml" {
				sheet = content
			}
			if strings.HasPrefix(file.Name, "xl/media/image") {
				picture, err := png.Decode(bytes.NewReader(content))
				if err != nil || picture.Bounds().Dy() != 90 {
					t.Fatal("invalid barcode", err)
				}
				images++
			}
		}
		if images != 100 {
			t.Fatal(images)
		}
		var cells struct {
			Rows []struct {
				Cells []struct {
					Reference string `xml:"r,attr"`
					Type      string `xml:"t,attr"`
					Formula   string `xml:"f"`
					Value     int    `xml:"v"`
				} `xml:"c"`
			} `xml:"sheetData>row"`
		}
		if err = xml.Unmarshal(sheet, &cells); err != nil {
			t.Fatal(err)
		}
		for i := 1; i <= 100; i++ {
			cell := cells.Rows[i].Cells[0]
			if cell.Reference != fmt.Sprintf("A%d", i+1) || cell.Formula != "" || cell.Type != "s" {
				t.Fatal("ID must be a fixed string", cell)
			}
			id := shared[cell.Value]
			if !intakeNumber.MatchString(id) || seen[id] {
				t.Fatal("invalid or repeated namespace", id)
			}
			seen[id] = true
		}
	}
}
