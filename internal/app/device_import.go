package app

import (
	"context"
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const maxIntakeCSV = 1 << 20
const maxIntakeRows = 500

var intakeNumber = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]{0,99}$`)
var intakeHeaders = []string{"Inventarnummer", "Art", "Bezeichnung", "Hersteller", "Modell", "Seriennummer", "Standort", "Zustand", "Fremdcode", "Bemerkung"}

type deviceIntake struct {
	Kind           string `json:"asset_kind"`
	Condition      string `json:"observed_condition"`
	ExternalCode   string `json:"external_code"`
	SourceLocation string `json:"source_location"`
}
type deviceImportRequest struct {
	CSV       string            `json:"csv"`
	Locations map[string]string `json:"locations"`
	Basis     string            `json:"basis_hash,omitempty"`
}
type intakeLocation struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Site string `json:"site"`
}
type intakeRow struct {
	Row         int          `json:"row"`
	Device      deviceInput  `json:"device"`
	Intake      deviceIntake `json:"intake"`
	Status      string       `json:"status"`
	Message     string       `json:"message"`
	Fingerprint string       `json:"-"`
	ExistingID  string       `json:"-"`
}
type deviceImportPreview struct {
	Basis          string           `json:"basis_hash"`
	Rows           []intakeRow      `json:"rows"`
	Choices        []intakeLocation `json:"location_choices"`
	LocationValues []string         `json:"location_values"`
	New            int              `json:"new_count"`
	Unchanged      int              `json:"unchanged_count"`
	Errors         int              `json:"error_count"`
	Unused         int              `json:"unused_count"`
}
type deviceImportResult struct {
	ID        string   `json:"import_id"`
	Created   int      `json:"created_count"`
	Unchanged int      `json:"unchanged_count"`
	Devices   []string `json:"created_device_ids"`
}

func intakeFileError(message string) error { return &stockError{400, "invalid_csv", message} }
func intakeValue(value string, max int) bool {
	if len(value) > max || !utf8.ValidString(value) {
		return false
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}
func parseIntakeCSV(input string) ([]intakeRow, int, error) {
	if len(input) == 0 || len(input) > maxIntakeCSV || !utf8.ValidString(input) {
		return nil, 0, intakeFileError("CSV muss UTF-8 sein und darf höchstens 1 MiB groß sein. In Excel CSV UTF-8 auswählen.")
	}
	input = strings.TrimPrefix(input, "\ufeff")
	var reader *csv.Reader
	var columns map[string]int
	for _, delimiter := range []rune{';', ','} {
		candidate := csv.NewReader(strings.NewReader(input))
		candidate.Comma = delimiter
		header, err := candidate.Read()
		if err != nil {
			continue
		}
		mapping := map[string]int{}
		valid := true
		for i, title := range header {
			title = strings.TrimSpace(title)
			if _, exists := mapping[title]; exists {
				valid = false
				break
			}
			known := false
			for _, name := range intakeHeaders {
				if title == name {
					known = true
					break
				}
			}
			if !known {
				valid = false
				break
			}
			mapping[title] = i
		}
		if valid {
			for _, required := range []string{"Inventarnummer", "Bezeichnung", "Standort"} {
				if _, ok := mapping[required]; !ok {
					valid = false
				}
			}
		}
		if valid {
			reader = candidate
			columns = mapping
			break
		}
	}
	if reader == nil {
		return nil, 0, intakeFileError("Kopfzeile mit Inventarnummer, Bezeichnung und Standort erforderlich; nur die Spalten der Vorlage verwenden. Semikolon und Komma werden unterstützt.")
	}
	rows := []intakeRow{}
	unused := 0
	records := 0
	for {
		fields, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, unused, intakeFileError("CSV enthält ungültige Anführungszeichen oder eine abweichende Spaltenzahl.")
		}
		records++
		if records > 2000 {
			return nil, unused, intakeFileError("Höchstens 2.000 vorbereitete Zeilen und 500 erfasste Gegenstände je Import.")
		}
		get := func(name string) string {
			if column, ok := columns[name]; ok {
				return strings.TrimSpace(fields[column])
			}
			return ""
		}
		empty := true
		for title, index := range columns {
			if title != "Inventarnummer" && strings.TrimSpace(fields[index]) != "" {
				empty = false
				break
			}
		}
		if empty {
			unused++
			continue
		}
		if len(rows) >= maxIntakeRows {
			return nil, unused, intakeFileError("Höchstens 500 erfasste Gegenstände je Import; Datei aufteilen.")
		}
		line, _ := reader.FieldPos(0)
		row := intakeRow{Row: line, Status: "new", Message: "Wird neu angelegt.", Device: deviceInput{InventoryNumber: get("Inventarnummer"), Name: get("Bezeichnung"), Manufacturer: get("Hersteller"), Model: get("Modell"), SerialOrLot: get("Seriennummer"), Note: get("Bemerkung")}, Intake: deviceIntake{SourceLocation: get("Standort"), ExternalCode: get("Fremdcode")}}
		issue := func(message string) {
			if row.Status != "error" {
				row.Status = "error"
				row.Message = message
			}
		}
		if !intakeNumber.MatchString(row.Device.InventoryNumber) || row.Device.Name == "" || row.Intake.SourceLocation == "" {
			issue("Inventarnummer (Buchstaben/Ziffern und . _ / -), Bezeichnung und Standort erforderlich.")
		}
		for _, value := range []string{row.Device.Name, row.Device.Manufacturer, row.Device.Model, row.Device.SerialOrLot, row.Intake.SourceLocation} {
			if !intakeValue(value, 160) {
				issue("Grunddaten auf 160 Zeichen begrenzen; Steuerzeichen sind nicht zulässig.")
			}
		}
		if !intakeValue(row.Device.Note, 500) || !intakeValue(row.Intake.ExternalCode, 1024) {
			issue("Bemerkung höchstens 500 Zeichen, Fremdcode höchstens 1.024 Zeichen; keine Steuerzeichen.")
		}
		switch strings.ToLower(get("Art")) {
		case "", "ungeklärt", "ungeklaert", "unknown":
			row.Intake.Kind = "unknown"
		case "gerät", "geraet", "device":
			row.Intake.Kind = "device"
		case "equipment":
			row.Intake.Kind = "equipment"
		default:
			issue("Art muss Gerät, Equipment oder ungeklärt sein.")
		}
		switch strings.ToLower(get("Zustand")) {
		case "", "ungeklärt", "ungeklaert", "unknown":
			row.Intake.Condition = "unknown"
		case "unauffällig", "unauffaellig", "unremarkable":
			row.Intake.Condition = "unremarkable"
		case "defekt", "defective":
			row.Intake.Condition = "defective"
		case "quarantäne", "quarantaene", "quarantined":
			row.Intake.Condition = "quarantined"
		default:
			issue("Zustand muss ungeklärt, unauffällig, defekt oder Quarantäne sein; keine Freigabe aus CSV.")
		}
		if row.Device.Note == "" {
			row.Device.Note = "CSV-Erstinventarisierung"
		}
		rows = append(rows, row)
	}
	if len(rows) == 0 {
		return nil, unused, intakeFileError("Keine erfassten Gegenstände gefunden. Bezeichnung und Standort im Blatt Inventar ergänzen.")
	}
	duplicates := map[string][]int{}
	for i, row := range rows {
		normalized := strings.ToUpper(row.Device.InventoryNumber)
		duplicates[normalized] = append(duplicates[normalized], i)
	}
	for _, positions := range duplicates {
		if len(positions) > 1 {
			for _, i := range positions {
				rows[i].Status = "error"
				rows[i].Message = "Inventarnummer kommt mehrfach in dieser Datei vor."
			}
		}
	}
	return rows, unused, nil
}
func intakeLocations(ctx context.Context, tx *sql.Tx, user string) ([]intakeLocation, error) {
	return roleIntakeLocations(ctx, tx, user, "device_manager")
}
func roleIntakeLocations(ctx context.Context, tx *sql.Tx, user, role string) ([]intakeLocation, error) {
	rows, err := tx.QueryContext(ctx, `SELECT DISTINCT l.id,l.name,s.name FROM locations l JOIN sites s ON s.id=l.site_id JOIN role_assignments a ON a.site_id=l.site_id WHERE a.user_id=? AND a.role=? ORDER BY l.name,l.id`, user, role)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	choices := []intakeLocation{}
	for rows.Next() {
		var value intakeLocation
		if err = rows.Scan(&value.ID, &value.Name, &value.Site); err != nil {
			return nil, err
		}
		choices = append(choices, value)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	if len(choices) == 0 {
		return nil, scopeError()
	}
	return choices, nil
}
func previewDeviceImport(ctx context.Context, tx *sql.Tx, user string, input deviceImportRequest) (deviceImportPreview, error) {
	result := deviceImportPreview{LocationValues: []string{}}
	choices, err := intakeLocations(ctx, tx, user)
	if err != nil {
		return result, err
	}
	result.Choices = choices
	rows, unused, err := parseIntakeCSV(input.CSV)
	if err != nil {
		return result, err
	}
	result.Rows = rows
	result.Unused = unused
	byID := map[string]bool{}
	byName := map[string][]string{}
	for _, location := range choices {
		byID[location.ID] = true
		byName[location.Name] = append(byName[location.Name], location.ID)
	}
	if len(input.Locations) > maxIntakeRows {
		return result, intakeFileError("Zu viele Standortzuordnungen.")
	}
	for name, id := range input.Locations {
		if strings.TrimSpace(name) != name || name == "" || !intakeValue(name, 160) {
			return result, intakeFileError("Ungültiger Standortname in der Zuordnung.")
		}
		if !byID[id] {
			return result, scopeError()
		}
	}
	names := map[string]bool{}
	for i := range result.Rows {
		row := &result.Rows[i]
		source := row.Intake.SourceLocation
		names[source] = true
		if mapped, ok := input.Locations[source]; ok {
			row.Device.LocationID = mapped
		} else if byID[source] {
			row.Device.LocationID = source
		} else if len(byName[source]) == 1 {
			row.Device.LocationID = byName[source][0]
		}
		if row.Device.LocationID == "" && row.Status != "error" {
			row.Status = "error"
			row.Message = "Standort unbekannt oder mehrdeutig: unten ausdrücklich einem Lagerort zuordnen."
		}
		if row.Status == "error" {
			result.Errors++
			continue
		}
		canonical, _ := json.Marshal(struct {
			Device deviceInput
			Intake deviceIntake
		}{row.Device, row.Intake})
		row.Fingerprint = digest(string(canonical))
		var oldID, oldHash, oldLocation string
		err = tx.QueryRowContext(ctx, `SELECT d.id,COALESCE(i.row_hash,''),d.location_id FROM devices d LEFT JOIN device_intake i ON i.device_id=d.id WHERE d.inventory_number=? COLLATE NOCASE`, row.Device.InventoryNumber).Scan(&oldID, &oldHash, &oldLocation)
		if err == nil {
			if oldHash == row.Fingerprint && byID[oldLocation] {
				row.ExistingID = oldID
				row.Status = "unchanged"
				row.Message = "Bereits mit denselben Daten importiert; wird übersprungen."
				result.Unchanged++
			} else {
				row.Status = "error"
				row.Message = "Inventarnummer kann nicht übernommen werden: bereits vergeben oder abweichende Daten. Bestehende Einträge werden nicht überschrieben."
				result.Errors++
			}
		} else if errors.Is(err, sql.ErrNoRows) {
			result.New++
		} else {
			return result, err
		}
	}
	for name := range names {
		result.LocationValues = append(result.LocationValues, name)
	}
	sort.Strings(result.LocationValues)
	canonical, err := json.Marshal(result)
	if err != nil {
		return result, err
	}
	result.Basis = digest(string(canonical))
	return result, nil
}
func createIntakeDevice(ctx context.Context, tx *sql.Tx, user string, row intakeRow, now time.Time) (string, error) {
	record := registeredDevice(row.Device, newID(), user, now.UTC().Format(time.RFC3339Nano))
	record.Intake = &row.Intake
	if _, err := tx.ExecContext(ctx, `INSERT INTO devices VALUES(?,?,?,?,?,?,?,?,?,?)`, record.ID, row.Device.LocationID, row.Device.InventoryNumber, row.Device.Name, row.Device.Manufacturer, row.Device.Model, row.Device.SerialOrLot, user, record.CreatedAt, row.Device.Note); err != nil {
		return "", err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO device_intake VALUES(?,?,?,?,?,?)`, record.ID, row.Intake.Kind, row.Intake.Condition, row.Intake.ExternalCode, row.Intake.SourceLocation, row.Fingerprint); err != nil {
		return "", err
	}
	encoded, _ := json.Marshal(record)
	event := newID()
	if _, err := tx.ExecContext(ctx, `INSERT INTO device_events VALUES(?,?,?,?,?)`, event, record.ID, user, record.CreatedAt, string(encoded)); err != nil {
		return "", err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO device_outbox(event_id) VALUES(?)`, event); err != nil {
		return "", err
	}
	// Defect/quarantine are independent real blocks, never a fictitious inspection
	// or qualification. Later release uses the existing explicit single-block flow.
	if row.Intake.Condition == "defective" || row.Intake.Condition == "quarantined" {
		reason := "defect"
		if row.Intake.Condition == "quarantined" {
			reason = "quarantine"
		}
		payload, _ := json.Marshal(deviceControlInput{ExpectedVersion: 0, Evidence: "CSV-Erstinventarisierung", Note: row.Device.Note, BlockReason: reason})
		block := newID()
		if _, err := tx.ExecContext(ctx, `INSERT INTO device_control_events VALUES(?,?,1,'defect',?,?,?)`, block, record.ID, user, record.CreatedAt, string(payload)); err != nil {
			return "", err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO device_control_outbox(event_id) VALUES(?)`, block); err != nil {
			return "", err
		}
	}
	return record.ID, nil
}
func commitDeviceImport(ctx context.Context, db *sql.DB, user, token, key string, input deviceImportRequest, now time.Time) ([]byte, error) {
	if len(input.Basis) != 64 {
		return nil, intakeFileError("CSV zuerst prüfen; Vorschau-Datenbasis erforderlich.")
	}
	canonical, _ := json.Marshal(input)
	hash := digest("device_import:" + string(canonical))
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err = activeStockSession(ctx, tx, user, token, now); err != nil {
		return nil, err
	}
	preview, err := previewDeviceImport(ctx, tx, user, input)
	if err != nil {
		return nil, err
	}
	if err = rejectOtherKey(ctx, tx, user, key, "device_import_requests"); err != nil {
		return nil, err
	}
	var old, body string
	err = tx.QueryRowContext(ctx, `SELECT request_hash,response_json FROM device_import_requests WHERE user_id=? AND key=?`, user, key).Scan(&old, &body)
	if err == nil {
		if old != hash {
			return nil, conflict("idempotency_conflict", "Schlüssel gehört zu einem anderen Import.")
		}
		for _, row := range preview.Rows {
			if row.Device.LocationID == "" {
				return nil, scopeError()
			}
		}
		return []byte(body), nil
	}
	if err != sql.ErrNoRows {
		return nil, err
	}
	if preview.Basis != input.Basis {
		return nil, conflict("import_basis_conflict", "Daten oder Berechtigungen haben sich seit der Vorschau geändert. CSV erneut prüfen.")
	}
	if preview.Errors > 0 {
		return nil, intakeFileError("Fehler in der Vorschau korrigieren; es wurden keine Geräte übernommen.")
	}
	result := deviceImportResult{ID: newID(), Unchanged: preview.Unchanged, Devices: []string{}}
	for _, row := range preview.Rows {
		if row.Status == "new" {
			id, err := createIntakeDevice(ctx, tx, user, row, now)
			if err != nil {
				return nil, err
			}
			result.Created++
			result.Devices = append(result.Devices, id)
		}
	}
	snapshot, _ := json.Marshal(preview)
	if _, err = tx.ExecContext(ctx, `INSERT INTO device_imports VALUES(?,?,?,?,?,?,?,?)`, result.ID, user, now.UTC().Format(time.RFC3339Nano), digest(input.CSV), input.Basis, string(snapshot), result.Created, result.Unchanged); err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO device_import_outbox(event_id) VALUES(?)`, result.ID); err != nil {
		return nil, err
	}
	encoded, _ := json.Marshal(result)
	if _, err = tx.ExecContext(ctx, `INSERT INTO device_import_requests VALUES(?,?,?,?,?)`, user, key, hash, result.ID, string(encoded)); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return encoded, nil
}
func deviceImportRoutes(mux *http.ServeMux, a *authService) {
	mux.HandleFunc("GET /api/v1/device-import/template.xlsx", func(w http.ResponseWriter, r *http.Request) {
		user, err := a.session(r)
		if err != nil {
			a.sessionError(w, err)
			return
		}
		tx, err := a.db.BeginTx(r.Context(), nil)
		if err != nil {
			sendStockError(w, err)
			return
		}
		_, err = intakeLocations(r.Context(), tx, user.ID)
		tx.Rollback()
		if err != nil {
			sendStockError(w, err)
			return
		}
		body, err := NewDeviceIntakeTemplate()
		if err != nil {
			sendStockError(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
		w.Header().Set("Content-Disposition", `attachment; filename="Geraete-Ersterfassung.xlsx"`)
		w.Header().Set("Cache-Control", "no-store")
		w.Write(body)
	})
	mux.HandleFunc("POST /api/v1/device-import/preview", func(w http.ResponseWriter, r *http.Request) {
		user, ok := writeAuthorized(a, w, r)
		if !ok {
			return
		}
		var input deviceImportRequest
		if !mutationBody(w, r, &input, 4<<20) {
			return
		}
		tx, err := a.db.BeginTx(r.Context(), nil)
		if err != nil {
			sendStockError(w, err)
			return
		}
		defer tx.Rollback()
		cookie, _ := r.Cookie(cookieName)
		if err = activeStockSession(r.Context(), tx, user.ID, digest(cookie.Value), a.now()); err != nil {
			sendStockError(w, err)
			return
		}
		result, err := previewDeviceImport(r.Context(), tx, user.ID, input)
		if err != nil {
			sendStockError(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(result)
	})
	mux.HandleFunc("POST /api/v1/device-import/commit", func(w http.ResponseWriter, r *http.Request) {
		user, ok := writeAuthorized(a, w, r)
		if !ok {
			return
		}
		var input deviceImportRequest
		if !mutationBody(w, r, &input, 4<<20) {
			return
		}
		cookie, _ := r.Cookie(cookieName)
		body, err := commitDeviceImport(r.Context(), a.db, user.ID, digest(cookie.Value), r.Header.Get("Idempotency-Key"), input, a.now())
		packResponse(w, body, err, 201)
	})
}
