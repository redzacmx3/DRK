package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

type deviceInput struct {
	LocationID      string `json:"location_id"`
	InventoryNumber string `json:"inventory_number"`
	Name            string `json:"name"`
	Manufacturer    string `json:"manufacturer"`
	Model           string `json:"model"`
	SerialOrLot     string `json:"serial_or_lot"`
	Note            string `json:"note"`
}
type deviceBlock struct {
	ID          string `json:"id"`
	Reason      string `json:"reason"`
	ControlType string `json:"control_type,omitempty"`
	Version     int    `json:"version"`
	Note        string `json:"note"`
}
type instructionProof struct {
	Version         int    `json:"version"`
	Instructor      string `json:"instructor"`
	Trainee         string `json:"trainee"`
	PerformedOn     string `json:"performed_on"`
	Scope           string `json:"scope"`
	DocumentID      string `json:"document_id"`
	DocumentVersion string `json:"document_version"`
	DocumentSHA256  string `json:"document_sha256"`
	DocumentStatus  string `json:"document_status"`
}
type deviceRecord struct {
	RecallRevision int64              `json:"recall_revision,omitempty"`
	Incidents      []incidentProof    `json:"incidents,omitempty"`
	Reports        []reportProof      `json:"reports,omitempty"`
	Corrections    []correctionProof  `json:"corrections,omitempty"`
	Decommission   *decommissionProof `json:"decommission,omitempty"`
	Maintenances   []maintenanceProof `json:"maintenances,omitempty"`
	MasterData     *deviceMasterData  `json:"master_data,omitempty"`
	Intake         *deviceIntake      `json:"intake,omitempty"`
	Instructions   []instructionProof `json:"instructions"`

	ActiveBlocks    []deviceBlock           `json:"active_blocks"`
	EffectiveRules  map[string]DeadlineRule `json:"control_rules,omitempty"`
	PassedDates     map[string]string       `json:"-"`
	InspectionDates map[string]string       `json:"-"`
	PassedVersions  map[string]int          `json:"-"`

	deviceInput
	Version        int                 `json:"version"`
	CommissionedOn string              `json:"commissioned_on,omitempty"`
	ID             string              `json:"id"`
	CreatedBy      string              `json:"created_by"`
	CreatedAt      string              `json:"created_at"`
	Lifecycle      string              `json:"lifecycle"`
	Readiness      string              `json:"readiness"`
	Reasons        []string            `json:"reasons"`
	Controls       map[string]Deadline `json:"controls"`
}

func registeredDevice(input deviceInput, id, actor, created string) deviceRecord {
	return deviceRecord{Instructions: []instructionProof{}, ActiveBlocks: []deviceBlock{}, EffectiveRules: map[string]DeadlineRule{}, PassedDates: map[string]string{}, InspectionDates: map[string]string{}, PassedVersions: map[string]int{}, deviceInput: input, ID: id, CreatedBy: actor, CreatedAt: created, Lifecycle: "NOT_COMMISSIONED", Readiness: "not_assessed", Reasons: []string{"Inbetriebnahmenachweis fehlt", "Kontrollpflichten noch nicht bewertet"}, Controls: map[string]Deadline{"STK": {State: "UNKNOWN"}, "MTK": {State: "UNKNOWN"}, "electrical": {State: "UNKNOWN"}}}
}
func registerDevice(ctx context.Context, db *sql.DB, user, token, key string, input deviceInput, now time.Time) ([]byte, error) {
	for _, v := range []string{input.LocationID, input.InventoryNumber, input.Name, input.Manufacturer, input.Model, input.SerialOrLot} {
		if strings.TrimSpace(v) != v || v == "" || len(v) > 160 {
			return nil, &stockError{400, "invalid_device", "Lagerort, Inventarnummer und vollständige Geräteidentifikation erforderlich."}
		}
	}
	if !validReason(input.Note) {
		return nil, &stockError{400, "invalid_device", "Begründung erforderlich."}
	}
	canonical, _ := json.Marshal(input)
	hash := digest("register_device:" + string(canonical))
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err = activeStockSession(ctx, tx, user, token, now); err != nil {
		return nil, err
	}
	var allowed bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM locations l JOIN role_assignments a ON a.site_id=l.site_id WHERE l.id=? AND a.user_id=? AND a.role='device_manager')`, input.LocationID, user).Scan(&allowed); err != nil {
		return nil, err
	}
	if !allowed {
		return nil, scopeError()
	}
	if err = rejectOtherKey(ctx, tx, user, key, "device_requests"); err != nil {
		return nil, err
	}
	var old, body string
	err = tx.QueryRowContext(ctx, `SELECT request_hash,response_json FROM device_requests WHERE user_id=? AND key=?`, user, key).Scan(&old, &body)
	if err == nil {
		if old != hash {
			return nil, conflict("idempotency_conflict", "Schlüssel gehört zu einem anderen Vorgang.")
		}
		return []byte(body), nil
	}
	if err != sql.ErrNoRows {
		return nil, err
	}
	var duplicate bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM devices WHERE inventory_number=?)`, input.InventoryNumber).Scan(&duplicate); err != nil {
		return nil, err
	}
	if duplicate {
		return nil, conflict("inventory_number_exists", "Inventarnummer ist bereits vergeben.")
	}
	record := registeredDevice(input, newID(), user, now.UTC().Format(time.RFC3339Nano))
	encoded, _ := json.Marshal(record)
	if _, err = tx.ExecContext(ctx, `INSERT INTO devices VALUES(?,?,?,?,?,?,?,?,?,?)`, record.ID, input.LocationID, input.InventoryNumber, input.Name, input.Manufacturer, input.Model, input.SerialOrLot, user, record.CreatedAt, input.Note); err != nil {
		return nil, err
	}
	event := newID()
	if _, err = tx.ExecContext(ctx, `INSERT INTO device_events VALUES(?,?,?,?,?)`, event, record.ID, user, record.CreatedAt, string(encoded)); err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO device_outbox(event_id) VALUES(?)`, event); err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO device_requests VALUES(?,?,?,?,?)`, user, key, hash, event, string(encoded)); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return encoded, nil
}
func deviceRoutes(mux *http.ServeMux, a *authService) {
	deviceAttentionRoutes(mux, a)
	deviceExportRoutes(mux, a)
	deviceBookExportRoutes(mux, a)
	deviceControlRoutes(mux, a)
	deviceImportRoutes(mux, a)
	mux.HandleFunc("GET /api/v1/devices/{id}/book", func(w http.ResponseWriter, r *http.Request) {
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
		defer tx.Rollback()
		record, err := readDevice(r.Context(), tx, user.ID, r.PathValue("id"), a.now())
		if err != nil {
			sendStockError(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(record)
	})

	mux.HandleFunc("POST /api/v1/devices", func(w http.ResponseWriter, r *http.Request) {
		user, ok := writeAuthorized(a, w, r)
		if !ok {
			return
		}
		var input deviceInput
		if !mutationBody(w, r, &input, 4096) {
			return
		}
		cookie, _ := r.Cookie(a.cookieName())
		body, err := registerDevice(r.Context(), a.db, user.ID, digest(cookie.Value), r.Header.Get("Idempotency-Key"), input, a.now())
		if err != nil {
			sendStockError(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(body)
	})
	mux.HandleFunc("GET /api/v1/devices", func(w http.ResponseWriter, r *http.Request) {
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
		defer tx.Rollback()
		rows, err := tx.QueryContext(r.Context(), `SELECT d.id,d.location_id,d.inventory_number,d.name,d.manufacturer,d.model,d.serial_or_lot,d.created_by,d.created_at,d.note FROM devices d JOIN locations l ON l.id=d.location_id WHERE EXISTS(SELECT 1 FROM role_assignments a WHERE a.site_id=l.site_id AND a.user_id=? AND a.role IN ('viewer','helper','material_manager','device_manager')) ORDER BY d.inventory_number LIMIT 501`, user.ID)
		if err != nil {
			sendStockError(w, err)
			return
		}
		defer rows.Close()
		records := []deviceRecord{}
		for rows.Next() {
			var in deviceInput
			var id, actor, created string
			if err = rows.Scan(&id, &in.LocationID, &in.InventoryNumber, &in.Name, &in.Manufacturer, &in.Model, &in.SerialOrLot, &actor, &created, &in.Note); err != nil {
				sendStockError(w, err)
				return
			}
			records = append(records, registeredDevice(in, id, actor, created))
		}
		if err = rows.Err(); err != nil {
			sendStockError(w, err)
			return
		}
		rows.Close()
		for i := range records {
			if err = projectDevice(r.Context(), tx, &records[i], a.now()); err != nil {
				sendStockError(w, err)
				return
			}
		}
		if len(records) > 500 {
			w.Header().Set("X-Has-More", "true")
			records = records[:500]
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(records)
	})
}
