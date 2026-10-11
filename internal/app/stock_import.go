package app

import (
	"context"
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

type stockImportRequest struct {
	CSV       string            `json:"csv"`
	Locations map[string]string `json:"locations"`
	Mode      string            `json:"mode"` // initial is a captured starting amount; receipt adds a later delivery.
	Basis     string            `json:"basis_hash,omitempty"`
}
type stockIntakeData struct {
	SourceID       string `json:"source_id"`
	Name           string `json:"name"`
	SourceLocation string `json:"source_location"`
	Quantity       int64  `json:"quantity"`
	Unit           string `json:"base_unit"`
	Lot            string `json:"lot"`
	Expiry         string `json:"expires_on"`
	Code           string `json:"external_code"`
	Note           string `json:"note"`
	Product        string `json:"product_id"`
}
type stockIntakeRow struct {
	Row         int             `json:"row"`
	Data        stockIntakeData `json:"data"`
	Location    string          `json:"location_id"`
	Product     string          `json:"resolved_product_id"`
	Batch       string          `json:"batch_id"`
	State       string          `json:"state"`
	Status      string          `json:"status"`
	Message     string          `json:"message"`
	StockBasis  string          `json:"stock_basis"`
	Fingerprint string          `json:"-"`
}
type stockImportPreview struct {
	Basis          string           `json:"basis_hash"`
	Rows           []stockIntakeRow `json:"rows"`
	Choices        []intakeLocation `json:"location_choices"`
	LocationValues []string         `json:"location_values"`
	New            int              `json:"new_count"`
	Unchanged      int              `json:"unchanged_count"`
	Errors         int              `json:"error_count"`
	Unused         int              `json:"unused_count"`
}
type stockImportResult struct {
	ID        string   `json:"import_id"`
	Created   int      `json:"created_count"`
	Unchanged int      `json:"unchanged_count"`
	Movements []string `json:"movement_ids"`
}

var stockIntakeHeaders = []string{"Erfassungsnummer", "Bezeichnung", "Standort", "Menge", "Basiseinheit", "Charge", "Verfall", "Fremdcode", "Bemerkung", "Artikel-ID"}

func parseStockIntakeCSV(input string) ([]stockIntakeRow, int, error) {
	if input == "" || len(input) > maxIntakeCSV || !utf8.ValidString(input) {
		return nil, 0, intakeFileError("CSV UTF-8 erforderlich, höchstens 1 MiB.")
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
			known := false
			for _, name := range stockIntakeHeaders {
				if name == title {
					known = true
				}
			}
			if _, exists := mapping[title]; exists || !known {
				valid = false
			}
			mapping[title] = i
		}
		for _, required := range stockIntakeHeaders[:5] {
			if _, ok := mapping[required]; !ok {
				valid = false
			}
		}
		if valid {
			reader = candidate
			columns = mapping
			break
		}
	}
	if reader == nil {
		return nil, 0, intakeFileError("Kopfzeile der Materialvorlage mit Erfassungsnummer, Bezeichnung, Standort, Menge und Basiseinheit erforderlich.")
	}
	rows := []stockIntakeRow{}
	unused := 0
	records := 0
	for {
		fields, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, unused, intakeFileError("Ungültige CSV oder abweichende Spaltenzahl.")
		}
		records++
		if records > 2000 {
			return nil, unused, intakeFileError("Höchstens 2.000 vorbereitete Zeilen.")
		}
		get := func(name string) string {
			if i, ok := columns[name]; ok {
				return strings.TrimSpace(fields[i])
			}
			return ""
		}
		empty := true
		for name, i := range columns {
			if name != "Erfassungsnummer" && strings.TrimSpace(fields[i]) != "" {
				empty = false
			}
		}
		if empty {
			unused++
			continue
		}
		if len(rows) >= maxIntakeRows {
			return nil, unused, intakeFileError("Höchstens 500 erfasste Zeilen.")
		}
		line, _ := reader.FieldPos(0)
		q, quantityErr := strconv.ParseInt(get("Menge"), 10, 64)
		row := stockIntakeRow{Row: line, Status: "new", Message: "Wird einmalig eingebucht.", Data: stockIntakeData{SourceID: get("Erfassungsnummer"), Name: get("Bezeichnung"), SourceLocation: get("Standort"), Quantity: q, Unit: get("Basiseinheit"), Lot: get("Charge"), Expiry: get("Verfall"), Code: get("Fremdcode"), Note: get("Bemerkung"), Product: get("Artikel-ID")}}
		if !intakeNumber.MatchString(row.Data.SourceID) || row.Data.Name == "" || row.Data.SourceLocation == "" || row.Data.Unit == "" || quantityErr != nil || q <= 0 || q > 1_000_000_000 {
			row.Status = "error"
			row.Message = "Kennung, Bezeichnung, Standort, Basiseinheit und ganzzahlige Menge 1–1.000.000.000 erforderlich."
		}
		for _, value := range []struct {
			s string
			n int
		}{{row.Data.Name, 160}, {row.Data.SourceLocation, 160}, {row.Data.Unit, 40}, {row.Data.Lot, 100}, {row.Data.Code, 1024}, {row.Data.Note, 500}, {row.Data.Product, 100}} {
			if !intakeValue(value.s, value.n) {
				row.Status = "error"
				row.Message = "Feld zu lang oder enthält Steuerzeichen."
			}
		}
		if row.Data.Expiry != "" && !validDay(row.Data.Expiry) {
			row.Status = "error"
			row.Message = "Verfall als exakter Tag JJJJ-MM-TT, sonst leer lassen; keine Datumsannahmen."
		}
		if strings.HasPrefix(strings.ToUpper(row.Data.Lot), "UNGEKLAERT-") {
			row.Status = "error"
			row.Message = "Chargenpräfix UNGEKLAERT- ist für ungeklärte Erfassungen reserviert."
		}
		rows = append(rows, row)
	}
	if len(rows) == 0 {
		return nil, unused, intakeFileError("Keine erfassten Materialzeilen.")
	}
	duplicates := map[string][]int{}
	for i, row := range rows {
		key := strings.ToUpper(row.Data.SourceID)
		duplicates[key] = append(duplicates[key], i)
	}
	for _, positions := range duplicates {
		if len(positions) > 1 {
			for _, i := range positions {
				rows[i].Status = "error"
				rows[i].Message = "Erfassungsnummer mehrfach in dieser Datei."
			}
		}
	}
	return rows, unused, nil
}

// Match SQLite NOCASE's ASCII folding; do not silently merge distinct Unicode names.
func stockNameKey(name string) string {
	b := []byte(name)
	for i, c := range b {
		if c >= 'a' && c <= 'z' {
			b[i] = c - ('a' - 'A')
		}
	}
	return string(b)
}
func intakeLot(row stockIntakeRow) string {
	if row.Data.Lot != "" {
		return row.Data.Lot
	}
	return "UNGEKLAERT-" + digest(strings.ToUpper(row.Data.SourceID))[:24]
}
func previewStockImport(ctx context.Context, tx *sql.Tx, user string, input stockImportRequest, now time.Time) (stockImportPreview, error) {
	result := stockImportPreview{LocationValues: []string{}}
	if input.Mode != "initial" && input.Mode != "receipt" {
		return result, intakeFileError("Importart initial (Erstbestand) oder receipt (späterer Zugang) ausdrücklich wählen.")
	}
	choices, err := roleIntakeLocations(ctx, tx, user, "material_manager")
	if err != nil {
		return result, err
	}
	result.Choices = choices
	result.Rows, result.Unused, err = parseStockIntakeCSV(input.CSV)
	if err != nil {
		return result, err
	}
	byID := map[string]bool{}
	byName := map[string][]string{}
	for _, l := range choices {
		byID[l.ID] = true
		byName[l.Name] = append(byName[l.Name], l.ID)
	}
	if len(input.Locations) > maxIntakeRows {
		return result, intakeFileError("Zu viele Standortzuordnungen.")
	}
	for name, id := range input.Locations {
		if name == "" || strings.TrimSpace(name) != name || !intakeValue(name, 160) {
			return result, intakeFileError("Ungültige Standortzuordnung.")
		}
		if !byID[id] {
			return result, scopeError()
		}
	}
	names := map[string]bool{}
	// Prevent inconsistent catalog or batch definitions within a single file.
	definitions := map[string]string{}
	lots := map[string]string{}
	for i := range result.Rows {
		row := &result.Rows[i]
		d := row.Data
		names[d.SourceLocation] = true
		if id, ok := input.Locations[d.SourceLocation]; ok {
			row.Location = id
		} else if byID[d.SourceLocation] {
			row.Location = d.SourceLocation
		} else if len(byName[d.SourceLocation]) == 1 {
			row.Location = byName[d.SourceLocation][0]
		}
		issue := func(message string) { row.Status = "error"; row.Message = message }
		if row.Location == "" {
			issue("Standort unbekannt oder mehrdeutig; ausdrücklich zuordnen.")
		}
		canonical, _ := json.Marshal(struct {
			Data           stockIntakeData
			Location, Mode string
		}{d, row.Location, input.Mode})
		row.Fingerprint = digest(string(canonical))
		if row.Status != "error" {
			var oldHash, oldLocation string
			err = tx.QueryRowContext(ctx, `SELECT row_hash,location_id FROM stock_intake WHERE source_id=?`, d.SourceID).Scan(&oldHash, &oldLocation)
			if err == nil {
				if oldHash == row.Fingerprint && byID[oldLocation] {
					row.Status = "unchanged"
					row.Message = "Bereits importiert; keine erneute Buchung, auch nach Entnahme oder Storno."
				} else {
					issue("Erfassungsnummer bereits vergeben oder Daten geändert; kein Überschreiben.")
				}
			} else if err != sql.ErrNoRows {
				return result, err
			}
		}
		if row.Status == "new" {
			if d.Product != "" {
				var name, unit string
				err = tx.QueryRowContext(ctx, `SELECT name,base_unit FROM product_types WHERE id=?`, d.Product).Scan(&name, &unit)
				if err == sql.ErrNoRows {
					issue("Artikel-ID nicht gefunden.")
				} else if err != nil {
					return result, err
				} else if name != d.Name || unit != d.Unit {
					issue("Bezeichnung oder Basiseinheit stimmt nicht mit der Artikel-ID überein.")
				} else {
					row.Product = d.Product
				}
			} else {
				matches, err := tx.QueryContext(ctx, `SELECT id,base_unit FROM product_types WHERE name=? COLLATE NOCASE ORDER BY id`, d.Name)
				if err != nil {
					return result, err
				}
				count := 0
				var unit string
				for matches.Next() {
					count++
					if err = matches.Scan(&row.Product, &unit); err != nil {
						matches.Close()
						return result, err
					}
				}
				err = matches.Err()
				matches.Close()
				if err != nil {
					return result, err
				}
				if count > 1 {
					issue("Artikelname mehrdeutig; Artikel-ID aus dem Materialkatalog ergänzen.")
				} else if count == 1 && unit != d.Unit {
					issue("Basiseinheit des vorhandenen Artikels weicht ab; keine automatische Packungsumrechnung.")
				}
			}
			defKey := stockNameKey(d.Name)
			if unit, ok := definitions[defKey]; ok && unit != d.Unit {
				issue("Unterschiedliche Basiseinheiten für denselben Namen in der Datei.")
			}
			definitions[defKey] = d.Unit
			lotKey := defKey + "\x00" + intakeLot(*row)
			if expiry, ok := lots[lotKey]; ok && expiry != d.Expiry {
				issue("Widersprüchlicher Verfall für dieselbe Charge in der Datei.")
			}
			lots[lotKey] = d.Expiry
			var expiry sql.NullString
			blocked := 0
			if row.Product != "" && row.Status == "new" {
				err = tx.QueryRowContext(ctx, `SELECT id,expires_on,blocked FROM effective_batches WHERE product_id=? AND lot=?`, row.Product, intakeLot(*row)).Scan(&row.Batch, &expiry, &blocked)
				if err == nil && expiry.String != d.Expiry {
					issue("Verfall weicht von vorhandener Charge ab; keine Änderung durch Import.")
				} else if err != nil && err != sql.ErrNoRows {
					return result, err
				}
				balances, err := tx.QueryContext(ctx, `SELECT b.batch_id,b.state,b.quantity,b.version FROM stock_balances b JOIN effective_batches c ON c.id=b.batch_id WHERE c.product_id=? AND b.location_id=? ORDER BY b.batch_id,b.state`, row.Product, row.Location)
				if err != nil {
					return result, err
				}
				basis := []struct {
					Batch, State      string
					Quantity, Version int64
				}{}
				for balances.Next() {
					var b struct {
						Batch, State      string
						Quantity, Version int64
					}
					if err = balances.Scan(&b.Batch, &b.State, &b.Quantity, &b.Version); err != nil {
						balances.Close()
						return result, err
					}
					basis = append(basis, b)
					if input.Mode == "initial" {
						issue("Artikel wurde an diesem Ort bereits als Bestand erfasst. Erstbestand abgelehnt; für tatsächliche Lieferungen Zugang wählen, für Zählungen die Bestandskorrektur.")
					}
				}
				err = balances.Err()
				balances.Close()
				if err != nil {
					return result, err
				}
				var revision int64
				var recallBlocked bool
				if err = tx.QueryRowContext(ctx, `SELECT COALESCE(SUM(s.version),0),COALESCE(MAX(s.status='OPEN'),0) FROM recalls r JOIN recall_state s ON s.recall_id=r.id WHERE r.kind='material' AND r.product_id=? AND (r.batch_id IS NULL OR r.batch_id=?)`, row.Product, row.Batch).Scan(&revision, &recallBlocked); err != nil {
					return result, err
				}
				if recallBlocked {
					blocked = 1
				}
				encoded, _ := json.Marshal(basis)
				row.StockBasis = digest(string(encoded))
				if revision > 0 {
					row.StockBasis = digest(row.StockBasis + fmt.Sprint(revision))
				}
			}
			row.State = "quarantined"
			if d.Lot != "" && d.Expiry != "" && d.Expiry >= localDay(now) && blocked == 0 {
				row.State = "available"
			}
			if row.Status == "new" {
				stateLabel := "Quarantäne"
				if row.State == "available" {
					stateLabel = "verfügbar"
				}
				row.Message = "Einmaliger Zugang in Basiseinheiten · " + stateLabel
				if row.Product == "" {
					row.Message += " · neuer Artikel"
				}
				if row.Batch == "" {
					row.Message += " · neue Charge"
				}
				if d.Lot == "" {
					row.Message += " · Charge ungeklärt"
				}
			}
		}
		switch row.Status {
		case "new":
			result.New++
		case "unchanged":
			result.Unchanged++
		case "error":
			result.Errors++
		}
	}
	// Mark all peers of conflicting definitions rather than silently taking the last.
	for i := range result.Rows {
		row := &result.Rows[i]
		if row.Status == "new" && (definitions[stockNameKey(row.Data.Name)] != row.Data.Unit || lots[stockNameKey(row.Data.Name)+"\x00"+intakeLot(*row)] != row.Data.Expiry) {
			row.Status = "error"
			row.Message = "Widersprüchliche Artikel-/Chargendaten in dieser Datei."
			result.New--
			result.Errors++
		}
	}
	for name := range names {
		result.LocationValues = append(result.LocationValues, name)
	}
	sort.Strings(result.LocationValues)
	canonical, _ := json.Marshal(result)
	result.Basis = digest(string(canonical))
	return result, nil
}
func commitStockImport(ctx context.Context, db *sql.DB, user, token, key string, input stockImportRequest, now time.Time) ([]byte, error) {
	canonical, _ := json.Marshal(input)
	hash := digest("stock-import:" + string(canonical))
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err = activeStockSession(ctx, tx, user, token, now); err != nil {
		return nil, err
	}
	preview, err := previewStockImport(ctx, tx, user, input, now)
	if err != nil {
		return nil, err
	}
	if err = rejectOtherKey(ctx, tx, user, key, "stock_import_requests"); err != nil {
		return nil, err
	}
	var old, body string
	err = tx.QueryRowContext(ctx, `SELECT request_hash,response_json FROM stock_import_requests WHERE user_id=? AND key=?`, user, key).Scan(&old, &body)
	if err == nil {
		if old != hash {
			return nil, conflict("idempotency_conflict", "Schlüssel gehört zu einem anderen Import.")
		}
		for _, row := range preview.Rows {
			if row.Location == "" {
				return nil, scopeError()
			}
		}
		return []byte(body), nil
	}
	if err != sql.ErrNoRows {
		return nil, err
	}
	if input.Basis == "" || input.Basis != preview.Basis {
		return nil, conflict("import_basis_conflict", "Vorschau hat sich geändert. CSV erneut prüfen.")
	}
	if preview.Errors > 0 {
		return nil, intakeFileError("Fehler korrigieren; es wurde kein Material übernommen.")
	}
	result := stockImportResult{ID: newID(), Unchanged: preview.Unchanged, Movements: []string{}}
	stamp := now.UTC().Format(time.RFC3339Nano)
	snapshot, _ := json.Marshal(preview)
	if _, err = tx.ExecContext(ctx, `INSERT INTO stock_imports VALUES(?,?,?,?,?,?,?,?)`, result.ID, user, stamp, digest(input.CSV), input.Basis, string(snapshot), preview.New, preview.Unchanged); err != nil {
		return nil, err
	}
	products := map[string]string{}
	batches := map[string]string{}
	for _, row := range preview.Rows {
		if row.Status != "new" {
			continue
		}
		product := row.Product
		productKey := stockNameKey(row.Data.Name)
		if product == "" {
			product = products[productKey]
			if product == "" {
				product = newID()
				if _, err = tx.ExecContext(ctx, `INSERT INTO product_types VALUES(?,?,?)`, product, row.Data.Name, row.Data.Unit); err != nil {
					return nil, err
				}
				products[productKey] = product
			}
		}
		batch := row.Batch
		batchKey := product + "\x00" + intakeLot(row)
		if batch == "" {
			batch = batches[batchKey]
			if batch == "" {
				batch = newID()
				var expiry any
				if row.Data.Expiry != "" {
					expiry = row.Data.Expiry
				}
				if _, err = tx.ExecContext(ctx, `INSERT INTO batches(id,product_id,lot,expires_on) VALUES(?,?,?,?)`, batch, product, intakeLot(row), expiry); err != nil {
					return nil, err
				}
				batches[batchKey] = batch
			}
		}
		changed, err := tx.ExecContext(ctx, `INSERT INTO stock_balances VALUES(?,?,?,?,1) ON CONFLICT(batch_id,location_id,state) DO UPDATE SET quantity=quantity+excluded.quantity,version=version+1 WHERE quantity<=? AND version<?`, batch, row.Location, row.State, row.Data.Quantity, math.MaxInt64-row.Data.Quantity, math.MaxInt64)
		if err != nil {
			return nil, err
		}
		n, err := changed.RowsAffected()
		if err != nil {
			return nil, err
		}
		if n != 1 {
			return nil, conflict("stock_conflict", "Bestand kann nicht erhöht werden.")
		}
		movement := newID()
		note := "CSV " + input.Mode + " · " + row.Data.SourceID
		if row.Data.Note != "" {
			note += " · " + row.Data.Note
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO stock_movements(id,kind,user_id,batch_id,to_location_id,quantity,state,created_at,note) VALUES(?,'receipt',?,?,?,?,?,?,?)`, movement, user, batch, row.Location, row.Data.Quantity, row.State, stamp, note); err != nil {
			return nil, err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO movement_lines VALUES(?,?,?,?,?)`, movement, batch, row.Location, row.State, row.Data.Quantity); err != nil {
			return nil, err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO audit_events VALUES(?,?,?,?,?)`, newID(), user, "stock.receipt", movement, stamp); err != nil {
			return nil, err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO outbox(id,movement_id,event_type,created_at) VALUES(?,?,?,?)`, newID(), movement, "stock.receipt", stamp); err != nil {
			return nil, err
		}
		unknown := 0
		if row.Data.Lot == "" {
			unknown = 1
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO stock_intake VALUES(?,?,?,?,?,?,?)`, row.Data.SourceID, row.Fingerprint, row.Location, movement, result.ID, row.Data.Code, unknown); err != nil {
			return nil, err
		}
		result.Created++
		result.Movements = append(result.Movements, movement)
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO stock_import_outbox(event_id) VALUES(?)`, result.ID); err != nil {
		return nil, err
	}
	encoded, _ := json.Marshal(result)
	if _, err = tx.ExecContext(ctx, `INSERT INTO stock_import_requests VALUES(?,?,?,?,?)`, user, key, hash, result.ID, string(encoded)); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return encoded, nil
}
func stockImportRoutes(mux *http.ServeMux, a *authService) {
	mux.HandleFunc("GET /api/v1/stock-import/template.xlsx", func(w http.ResponseWriter, r *http.Request) {
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
		_, err = roleIntakeLocations(r.Context(), tx, user.ID, "material_manager")
		tx.Rollback()
		if err != nil {
			sendStockError(w, err)
			return
		}
		body, err := NewStockIntakeTemplate()
		if err != nil {
			sendStockError(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
		w.Header().Set("Content-Disposition", `attachment; filename="Verbrauchsmaterial-Ersterfassung.xlsx"`)
		w.Header().Set("Cache-Control", "no-store")
		w.Write(body)
	})
	mux.HandleFunc("POST /api/v1/stock-import/preview", func(w http.ResponseWriter, r *http.Request) {
		user, ok := writeAuthorized(a, w, r)
		if !ok {
			return
		}
		var input stockImportRequest
		if !mutationBody(w, r, &input, 4<<20) {
			return
		}
		tx, err := a.db.BeginTx(r.Context(), nil)
		if err != nil {
			sendStockError(w, err)
			return
		}
		defer tx.Rollback()
		cookie, _ := r.Cookie(a.cookieName())
		if err = activeStockSession(r.Context(), tx, user.ID, digest(cookie.Value), a.now()); err != nil {
			sendStockError(w, err)
			return
		}
		result, err := previewStockImport(r.Context(), tx, user.ID, input, a.now())
		if err != nil {
			sendStockError(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(result)
	})
	mux.HandleFunc("POST /api/v1/stock-import/commit", func(w http.ResponseWriter, r *http.Request) {
		user, ok := writeAuthorized(a, w, r)
		if !ok {
			return
		}
		var input stockImportRequest
		if !mutationBody(w, r, &input, 4<<20) {
			return
		}
		cookie, _ := r.Cookie(a.cookieName())
		body, err := commitStockImport(r.Context(), a.db, user.ID, digest(cookie.Value), r.Header.Get("Idempotency-Key"), input, a.now())
		packResponse(w, body, err, 201)
	})
}
