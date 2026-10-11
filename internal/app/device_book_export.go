package app

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"html/template"
	"net/http"
	"sort"
	"strings"
	"time"
)

type bookExportEvent struct {
	ID        string          `json:"id"`
	Version   int             `json:"version,omitempty"`
	Action    string          `json:"action"`
	ActorID   string          `json:"actor_id"`
	CreatedAt string          `json:"created_at"`
	Proof     json.RawMessage `json:"proof"`
}
type bookIntakeProvenance struct {
	Kind           string `json:"asset_kind"`
	Condition      string `json:"observed_condition"`
	ExternalCode   string `json:"external_code"`
	SourceLocation string `json:"source_location"`
	RowHash        string `json:"row_hash"`
}

type deviceBookExport struct {
	IntakeProvenance *bookIntakeProvenance `json:"intake_provenance,omitempty"`
	FormatVersion    int                   `json:"format_version"`
	GeneratedAt      string                `json:"generated_at"`
	Operator         string                `json:"operator"`
	Site             string                `json:"site"`
	Location         string                `json:"location"`
	Device           deviceRecord          `json:"device"`
	Registration     []bookExportEvent     `json:"registration_events"`
	Controls         []bookExportEvent     `json:"control_events"`
	Documents        []documentRecord      `json:"documents"`
	ProductDuties    []bookExportEvent     `json:"product_duty_events"`
	ManualAccess     []bookExportEvent     `json:"manual_access_events"`
	DocumentReviews  []bookExportEvent     `json:"document_reviews"`
	DocumentEvents   []bookExportEvent     `json:"document_events"`
	Warnings         []string              `json:"warnings"`
}

type limitedExportBuffer struct {
	bytes.Buffer
	Remaining int
}

func (b *limitedExportBuffer) Write(data []byte) (int, error) {
	if len(data) > b.Remaining {
		return 0, &stockError{413, "export_too_large", "Aktenexport größer als 8 MiB; kein Teilarchiv erzeugt."}
	}
	b.Remaining -= len(data)
	return b.Buffer.Write(data)
}

func bookEvents(ctx context.Context, tx *sql.Tx, query, id string) ([]bookExportEvent, error) {
	rows, err := tx.QueryContext(ctx, query, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []bookExportEvent{}
	for rows.Next() {
		var e bookExportEvent
		var proof string
		if err = rows.Scan(&e.ID, &e.Version, &e.Action, &e.ActorID, &e.CreatedAt, &proof); err != nil {
			return nil, err
		}
		if !json.Valid([]byte(proof)) {
			return nil, &stockError{503, "export_invalid_history", "Historie nicht lesbar; Export abgebrochen."}
		}
		e.Proof = json.RawMessage(proof)
		result = append(result, e)
	}
	return result, rows.Err()
}

type bookProofField struct{ Label, Value string }

func bookProofFields(proof json.RawMessage) []bookProofField {
	var values map[string]json.RawMessage
	if json.Unmarshal(proof, &values) != nil {
		return []bookProofField{{"Originaldaten", string(proof)}}
	}
	labels := map[string]string{"incident": "Störung", "report": "Meldebezug", "correction": "Richtigstellung", "assessment_reference": "Bewertungs-/Entscheidungsreferenz", "description": "Beschreibung", "consequences": "Folgen", "incident_version": "Störungsversion", "responsible": "Verantwortliche Person/Funktion", "authority": "Behörde", "authority_reference": "Behördenreferenz", "authority_reported_at": "Behördenmeldung erfolgt am", "manufacturer_contact": "Herstellerkontakt", "manufacturer_reference": "Herstellerreferenz", "manufacturer_reported_at": "Herstellermeldung erfolgt am", "corrected_event": "Gebundener Ursprungsnachweis", "target_version": "Ursprungsversion", "statement": "Richtiggestellte Angaben", "payload_sha256": "SHA-256 des Ursprungsnachweises", "maintenance_kind": "Art der Instandhaltung", "master_data": "Neue Stammdaten", "before_master_data": "Stammdaten vor der Änderung", "name": "Bezeichnung", "manufacturer": "Hersteller", "model": "Modell", "serial_or_lot": "Serien-/Losnummer", "inventory_number": "Inventarnummer", "location_id": "Lagerort-ID", "performed_on": "Fachliches Datum", "evidence": "Nachweisreferenz", "qualification": "Qualifikationsreferenz", "assignment": "Betrieblicher Auftrag", "note": "Begründung / Notiz", "control_type": "Kontrollart", "result": "Ergebnis", "examiner": "Prüfer / Firma", "method": "Methode / Messwerte", "block_reason": "Sperrgrund", "block_id": "Betroffene Sperre", "rules": "Bestätigte Kontrollregeln", "calculated": "Berechnete Fristen", "instructor": "Einweisende Person", "trainee": "Eingewiesene Person", "instruction_scope": "Geltungsbereich der Einweisung", "transfer_evidence": "Übertragbarkeitsnachweis", "document_version": "Unterlagenversion", "document_id": "Dokument-ID", "document": "Gebundener Anhangsnachweis", "details": "Neue Grunddaten", "before_details": "Grunddaten vor der Änderung", "identity_review": "Identifikationsänderung erfordert Prüfung", "expected_version": "Bearbeitungsbasis: Geräteversion", "filename": "Dateiname", "media_type": "Dateityp", "sha256": "SHA-256", "size": "Dateigröße in Bytes", "status": "Status", "created_by": "Akteur-ID", "created_at": "Erfassungszeitpunkt", "id": "Objekt-ID", "device_id": "Geräte-ID"}
	keys := []string{}
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	fields := []bookProofField{}
	for _, key := range keys {
		value := string(values[key])
		var text string
		if json.Unmarshal(values[key], &text) == nil {
			value = text
		} else {
			var pretty bytes.Buffer
			if json.Indent(&pretty, values[key], "", "  ") == nil {
				value = pretty.String()
			}
		}
		label := labels[key]
		if label == "" {
			label = key
		}
		fields = append(fields, bookProofField{label, value})
	}
	return fields
}

var deviceBookTemplate = template.Must(template.New("device-book.html").Funcs(template.FuncMap{
	"fields": bookProofFields,
	"incidentKind": func(value string) string {
		names := map[string]string{"malfunction": "Störung", "repeated_operating_error": "Wiederholter gleichartiger Bedienungsfehler", "suspected_serious_incident": "Verdacht auf schwerwiegendes Vorkommnis"}
		if names[value] != "" {
			return names[value]
		}
		return value
	},
	"reportStatus": func(value string) string {
		names := map[string]string{"ASSESSMENT_REQUIRED": "Bewertung erforderlich", "REPORT_PENDING": "Behördenmeldung ausstehend", "REPORTED": "An Behörde gemeldet", "FOLLOW_UP": "Nachbearbeitung", "CLOSED": "Bearbeitung abgeschlossen"}
		if names[value] != "" {
			return names[value]
		}
		return value
	},
	"proof": func(value any) string { b, _ := json.MarshalIndent(value, "", "  "); return string(b) },
	"action": func(value string) string {
		names := map[string]string{"incident": "Störung", "report": "Meldebezug", "correction": "Richtigstellung", "decommission": "Außerbetriebnahme", "maintenance": "Instandhaltung", "registration": "Registrierung", "master_data": "Bestandsverzeichnis-Stammdaten", "details": "Grunddatenänderung", "commission": "Inbetriebnahme", "rules": "Kontrollregeln", "inspection": "Prüfung", "defect": "Sperrmeldung", "release": "Sperrenfreigabe", "instruction": "Einweisung", "document": "Anhang erfasst"}
		if name := names[value]; name != "" {
			return name
		}
		return value
	},
}).ParseFS(assets, "templates/device-book.html"))

func deviceBookExportRoutes(mux *http.ServeMux, a *authService) {
	mux.HandleFunc("GET /api/v1/devices/{id}/export.zip", func(w http.ResponseWriter, r *http.Request) {
		user, err := a.session(r)
		if err != nil {
			a.sessionError(w, err)
			return
		}
		select {
		case a.exportSlots <- struct{}{}:
			defer func() { <-a.exportSlots }()
		default:
			apiError(w, 429, "export_busy", "Export ausgelastet; später erneut versuchen.")
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		tx, err := a.db.BeginTx(ctx, nil)
		if err != nil {
			sendExportError(w, err)
			return
		}
		defer tx.Rollback()
		id := r.PathValue("id")
		now := a.now()
		book := deviceBookExport{FormatVersion: 1, GeneratedAt: now.UTC().Format(time.RFC3339Nano), Documents: []documentRecord{}, Warnings: []string{
			"Alle gespeicherten Einträge dieser Geräteakte im gemeinsamen Datenbankstand. Keine vollständige Einsatzfreigabe.",
			"Noch nicht strukturiert erfasst: Buchpflichtbewertung, unabhängige Meldepflichtbewertung, vollständiger Aufbewahrungsplan und technische Umsetzung richtiggestellter Fachangaben. Rechtliche Vollständigkeit ist nicht bestätigt.",
			"Qualifikations-, Auftrags- und Prüfbezüge sind erfasste Referenzen; ihre unabhängige Prüfung ist nicht bestätigt.",
			"Anhangsinhalte in Quarantäne sind ausgeschlossen; Metadaten, Prüfsummen und Erfassungshistorie sind enthalten.",
			"SHA-256 prüft Dateikonsistenz; keine digitale Signatur oder Bestätigung der Herkunft."}}
		err = tx.QueryRowContext(ctx, `SELECT o.name,s.name,l.name FROM devices d JOIN locations l ON l.id=d.location_id JOIN sites s ON s.id=l.site_id JOIN operators o ON o.id=s.operator_id WHERE d.id=? AND EXISTS(SELECT 1 FROM role_assignments a WHERE a.site_id=l.site_id AND a.user_id=? AND a.role IN ('viewer','helper','material_manager','device_manager'))`, id, user.ID).Scan(&book.Operator, &book.Site, &book.Location)
		if err == sql.ErrNoRows {
			sendExportError(w, scopeError())
			return
		}
		if err != nil {
			sendExportError(w, err)
			return
		}
		for _, query := range []string{`SELECT count(*) FROM device_events WHERE device_id=?`, `SELECT count(*) FROM device_control_events WHERE device_id=?`, `SELECT count(*) FROM device_documents WHERE device_id=?`, `SELECT count(*) FROM document_events e JOIN device_documents d ON d.id=e.document_id WHERE d.device_id=?`, `SELECT count(*) FROM document_reviews e JOIN device_documents d ON d.id=e.document_id WHERE d.device_id=?`, `SELECT count(*) FROM manual_access_events WHERE device_id=?`, `SELECT count(*) FROM product_duty_events WHERE device_id=?`} {
			var count int
			if err = tx.QueryRowContext(ctx, query, id).Scan(&count); err != nil {
				sendExportError(w, err)
				return
			}
			if count > 2000 {
				apiError(w, 413, "export_too_large", "Mehr als 2000 Einträge eines Historienbereichs; kein Teilarchiv erzeugt.")
				return
			}
		}
		book.Device, err = readDevice(ctx, tx, user.ID, id, now)
		if err != nil {
			sendExportError(w, err)
			return
		}
		if gaps := masterDataGaps(book.Device); len(gaps) > 0 {
			book.Warnings = append(book.Warnings, "Ungeklärte Bestandsverzeichnis-Stammdaten: "+strings.Join(gaps, ", ")+".")
		}
		var provenance bookIntakeProvenance
		err = tx.QueryRowContext(ctx, `SELECT asset_kind,observed_condition,external_code,source_location,row_hash FROM device_intake WHERE device_id=?`, id).Scan(&provenance.Kind, &provenance.Condition, &provenance.ExternalCode, &provenance.SourceLocation, &provenance.RowHash)
		if err == nil {
			book.IntakeProvenance = &provenance
		} else if err != sql.ErrNoRows {
			sendExportError(w, err)
			return
		}
		book.Registration, err = bookEvents(ctx, tx, `SELECT id,0,'registration',actor_id,created_at,snapshot_json FROM device_events WHERE device_id=? ORDER BY created_at,id`, id)
		if err != nil {
			sendExportError(w, err)
			return
		}
		book.Controls, err = bookEvents(ctx, tx, `SELECT id,version,action,actor_id,created_at,payload_json FROM device_control_events WHERE device_id=? ORDER BY version`, id)
		if err != nil {
			sendExportError(w, err)
			return
		}
		// Only own person records are exported unless the current site manager requests this record.
		manager, err := usageManager(ctx, tx, user.ID, id)
		if err != nil {
			sendExportError(w, err)
			return
		}
		manualRows, err := tx.QueryContext(ctx, `SELECT id,version,action,actor_id,created_at,payload_json FROM manual_access_events WHERE device_id=? AND (? OR subject_id=?) ORDER BY version`, id, manager, user.ID)
		if err != nil {
			sendExportError(w, err)
			return
		}
		book.ManualAccess = []bookExportEvent{}
		for manualRows.Next() {
			var event bookExportEvent
			var payload string
			if err = manualRows.Scan(&event.ID, &event.Version, &event.Action, &event.ActorID, &event.CreatedAt, &payload); err != nil {
				manualRows.Close()
				sendExportError(w, err)
				return
			}
			event.Proof = json.RawMessage(payload)
			book.ManualAccess = append(book.ManualAccess, event)
		}
		err = manualRows.Err()
		manualRows.Close()
		if err != nil {
			sendExportError(w, err)
			return
		}
		book.ProductDuties, err = bookEvents(ctx, tx, `SELECT id,version,action,actor_id,created_at,payload_json FROM product_duty_events WHERE device_id=? ORDER BY version`, id)
		if err != nil {
			sendExportError(w, err)
			return
		}
		book.DocumentReviews, err = bookEvents(ctx, tx, `SELECT e.id,e.version,e.action,e.actor_id,e.created_at,e.payload_json FROM document_reviews e JOIN device_documents d ON d.id=e.document_id WHERE d.device_id=? ORDER BY e.created_at,e.id`, id)
		if err != nil {
			sendExportError(w, err)
			return
		}
		book.DocumentEvents, err = bookEvents(ctx, tx, `SELECT e.id,0,'document',e.actor_id,e.created_at,e.snapshot_json FROM document_events e JOIN device_documents d ON d.id=e.document_id WHERE d.device_id=? ORDER BY e.created_at,e.id`, id)
		if err != nil {
			sendExportError(w, err)
			return
		}
		rows, err := tx.QueryContext(ctx, `SELECT id,device_id,filename,media_type,size,sha256,created_by,created_at,note,status,review_version FROM effective_documents WHERE device_id=? ORDER BY created_at,id`, id)
		if err != nil {
			sendExportError(w, err)
			return
		}
		for rows.Next() {
			var d documentRecord
			if err = rows.Scan(&d.ID, &d.DeviceID, &d.Filename, &d.MediaType, &d.Size, &d.SHA256, &d.CreatedBy, &d.CreatedAt, &d.Note, &d.Status, &d.ReviewVersion); err != nil {
				rows.Close()
				sendExportError(w, err)
				return
			}
			book.Documents = append(book.Documents, d)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			sendExportError(w, err)
			return
		}
		structured, err := json.MarshalIndent(book, "", "  ")
		if err != nil {
			sendExportError(w, err)
			return
		}
		if len(structured) > 8<<20 {
			apiError(w, 413, "export_too_large", "Aktenexport größer als 8 MiB; kein Teilarchiv erzeugt.")
			return
		}
		human := limitedExportBuffer{Remaining: (8 << 20) - len(structured)}
		if err = deviceBookTemplate.Execute(&human, book); err != nil {
			sendExportError(w, err)
			return
		}
		if len(structured)+human.Len() > 8<<20 {
			apiError(w, 413, "export_too_large", "Aktenexport größer als 8 MiB; kein Teilarchiv erzeugt.")
			return
		}
		type file struct {
			Name   string `json:"name"`
			Size   int    `json:"size"`
			SHA256 string `json:"sha256"`
		}
		manifest := struct {
			FormatVersion              int              `json:"format_version"`
			GeneratedAt                string           `json:"generated_at"`
			DeviceID                   string           `json:"device_id"`
			DeviceVersion              int              `json:"device_version"`
			Files                      []file           `json:"files"`
			Documents                  []documentRecord `json:"documents"`
			AttachmentContentsIncluded bool             `json:"attachment_contents_included"`
			Warnings                   []string         `json:"warnings"`
		}{1, book.GeneratedAt, id, book.Device.Version, []file{}, book.Documents, false, book.Warnings}
		var archive bytes.Buffer
		writer := zip.NewWriter(&archive)
		for _, entry := range []struct {
			name string
			data []byte
		}{{"geraeteakte.html", human.Bytes()}, {"akte.json", structured}} {
			sum := sha256.Sum256(entry.data)
			manifest.Files = append(manifest.Files, file{entry.name, len(entry.data), hex.EncodeToString(sum[:])})
			f, e := writer.Create(entry.name)
			if e != nil {
				sendExportError(w, e)
				return
			}
			if _, e = f.Write(entry.data); e != nil {
				sendExportError(w, e)
				return
			}
		}
		metadata, err := json.MarshalIndent(manifest, "", "  ")
		if err != nil {
			sendExportError(w, err)
			return
		}
		if len(structured)+human.Len()+len(metadata) > 8<<20 {
			apiError(w, 413, "export_too_large", "Aktenexport größer als 8 MiB; kein Teilarchiv erzeugt.")
			return
		}
		f, err := writer.Create("manifest.json")
		if err != nil {
			sendExportError(w, err)
			return
		}
		if _, err = f.Write(metadata); err != nil {
			sendExportError(w, err)
			return
		}
		if err = writer.Close(); err != nil {
			sendExportError(w, err)
			return
		}
		if err = ctx.Err(); err != nil {
			sendExportError(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/zip")
		w.Header().Set("Content-Disposition", `attachment; filename="geraeteakte.zip"`)
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Data-As-Of", book.GeneratedAt)
		w.Write(archive.Bytes())
	})
}
