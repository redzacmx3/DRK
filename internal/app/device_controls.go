package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"
)

type deviceControlInput struct {
	MaintenanceKind  string            `json:"maintenance_kind,omitempty"`
	MasterData       *deviceMasterData `json:"master_data,omitempty"`
	Details          *deviceDetails    `json:"details,omitempty"`
	Instructor       string            `json:"instructor,omitempty"`
	Trainee          string            `json:"trainee,omitempty"`
	InstructionScope string            `json:"instruction_scope,omitempty"`
	TransferEvidence string            `json:"transfer_evidence,omitempty"`
	DocumentVersion  string            `json:"document_version,omitempty"`
	DocumentID       string            `json:"document_id,omitempty"`
	ExpectedVersion  int               `json:"expected_version"`
	PerformedOn      string            `json:"performed_on,omitempty"`
	Evidence         string            `json:"evidence"`
	Qualification    string            `json:"qualification"`
	Assignment       string            `json:"assignment"`
	Note             string            `json:"note"`
	ControlType      string            `json:"control_type,omitempty"`
	Result           string            `json:"result,omitempty"`
	Examiner         string            `json:"examiner,omitempty"`
	Method           string            `json:"method,omitempty"`
	NextDueOn        string            `json:"next_due_on,omitempty"`
	BlockReason      string            `json:"block_reason,omitempty"`
	BlockID          string            `json:"block_id,omitempty"`
	Rules            []DeadlineRule    `json:"rules,omitempty"`
}

func readDevice(ctx context.Context, tx *sql.Tx, user, id string, now time.Time) (deviceRecord, error) {
	var input deviceInput
	var actor, created string
	err := tx.QueryRowContext(ctx, `SELECT d.location_id,d.inventory_number,d.name,d.manufacturer,d.model,d.serial_or_lot,d.created_by,d.created_at,d.note FROM devices d JOIN locations l ON l.id=d.location_id WHERE d.id=? AND EXISTS(SELECT 1 FROM role_assignments a WHERE a.site_id=l.site_id AND a.user_id=? AND a.role IN ('viewer','helper','material_manager','device_manager'))`, id, user).Scan(&input.LocationID, &input.InventoryNumber, &input.Name, &input.Manufacturer, &input.Model, &input.SerialOrLot, &actor, &created, &input.Note)
	if err == sql.ErrNoRows {
		return deviceRecord{}, scopeError()
	}
	if err != nil {
		return deviceRecord{}, err
	}
	record := registeredDevice(input, id, actor, created)
	err = projectDevice(ctx, tx, &record, now)
	return record, err
}
func projectDevice(ctx context.Context, tx *sql.Tx, record *deviceRecord, now time.Time) error {
	var intake deviceIntake
	intakeErr := tx.QueryRowContext(ctx, `SELECT asset_kind,observed_condition,external_code,source_location FROM device_intake WHERE device_id=?`, record.ID).Scan(&intake.Kind, &intake.Condition, &intake.ExternalCode, &intake.SourceLocation)
	if intakeErr == nil {
		record.Intake = &intake
	} else if intakeErr != sql.ErrNoRows {
		return intakeErr
	}

	rows, err := tx.QueryContext(ctx, `SELECT id,version,action,payload_json FROM device_control_events WHERE device_id=? ORDER BY version`, record.ID)
	if err != nil {
		return err
	}
	defer rows.Close()
	record.Maintenances = nil
	record.Instructions = []instructionProof{}
	record.ActiveBlocks = []deviceBlock{}
	record.EffectiveRules = map[string]DeadlineRule{}
	record.PassedVersions = map[string]int{}
	record.InspectionDates = map[string]string{}
	record.PassedDates = map[string]string{}
	for rows.Next() {
		var version int
		var eventID, action, payload string
		if err = rows.Scan(&eventID, &version, &action, &payload); err != nil {
			return err
		}
		var input deviceControlInput
		if err = json.Unmarshal([]byte(payload), &input); err != nil {
			return err
		}
		record.Version = version
		if action == "master_data" {
			if input.MasterData == nil {
				return fmt.Errorf("missing master data")
			}
			copy := *input.MasterData
			record.MasterData = &copy
		} else if action == "details" {
			if input.Details == nil {
				return fmt.Errorf("missing device details")
			}
			record.Name = input.Details.Name
			record.Manufacturer = input.Details.Manufacturer
			record.Model = input.Details.Model
			record.SerialOrLot = input.Details.SerialOrLot
			var proof struct {
				IdentityReview bool `json:"identity_review"`
			}
			if err = json.Unmarshal([]byte(payload), &proof); err != nil {
				return err
			}
			if proof.IdentityReview {
				record.ActiveBlocks = append(record.ActiveBlocks, deviceBlock{eventID, "quarantine", "", version, "Geräteidentifikation geändert: technische Nachweise und Einweisungen vor Freigabe prüfen. " + input.Note})
			}
		} else if action == "commission" {
			record.CommissionedOn = input.PerformedOn
			record.Lifecycle = "IN_SERVICE"
		} else if action == "rules" {
			record.EffectiveRules = map[string]DeadlineRule{}
			for _, rule := range input.Rules {
				record.EffectiveRules[rule.ControlType] = rule
			}
		} else if action == "inspection" {
			record.InspectionDates[input.ControlType] = input.PerformedOn
			if input.Result == "passed" {
				for _, rule := range input.Rules {
					record.EffectiveRules[rule.ControlType] = rule
				}
				record.PassedVersions[input.ControlType] = version
				record.PassedDates[input.ControlType] = input.PerformedOn
			} else {
				record.ActiveBlocks = append(record.ActiveBlocks, deviceBlock{eventID, "inspection_failed", input.ControlType, version, input.Note})
			}
		} else if action == "defect" {
			record.ActiveBlocks = append(record.ActiveBlocks, deviceBlock{eventID, input.BlockReason, "", version, input.Note})
		} else if action == "maintenance" {
			var proof struct {
				Document *documentRecord `json:"document,omitempty"`
			}
			if err = json.Unmarshal([]byte(payload), &proof); err != nil {
				return err
			}
			maintenance := maintenanceProof{EventID: eventID, Version: version, Kind: input.MaintenanceKind, PerformedOn: input.PerformedOn, Executor: input.Examiner, Measure: input.Method, Result: input.Result, Evidence: input.Evidence, DocumentID: input.DocumentID}
			if proof.Document != nil {
				maintenance.DocumentSHA256 = proof.Document.SHA256
			}
			record.Maintenances = append(record.Maintenances, maintenance)
			if input.Result == "failed" {
				record.ActiveBlocks = append(record.ActiveBlocks, deviceBlock{eventID, "defect", "", version, "Instandhaltung fehlgeschlagen: " + input.Note})
			} else if input.Result == "follow_up_required" {
				record.ActiveBlocks = append(record.ActiveBlocks, deviceBlock{eventID, "quarantine", "", version, "Nacharbeit aus Instandhaltung erforderlich: " + input.Note})
			}
		} else if action == "instruction" {
			var proof struct {
				Document documentRecord `json:"document"`
			}
			if err = json.Unmarshal([]byte(payload), &proof); err != nil {
				return err
			}
			record.Instructions = append(record.Instructions, instructionProof{version, input.Instructor, input.Trainee, input.PerformedOn, input.InstructionScope, input.DocumentID, input.DocumentVersion, proof.Document.SHA256, proof.Document.Status})
		} else if action == "release" {
			remaining := []deviceBlock{}
			for _, block := range record.ActiveBlocks {
				if block.ID != input.BlockID {
					remaining = append(remaining, block)
				}
			}
			record.ActiveBlocks = remaining
		}

	}
	if err = rows.Err(); err != nil {
		return err
	}
	if record.Version == 0 {
		return nil
	}
	record.Reasons = []string{"Vollständige Gerätefreigabe noch nicht bewertet"}
	if record.CommissionedOn == "" {
		record.Reasons = append(record.Reasons, "Inbetriebnahmenachweis fehlt")
	}
	for _, rule := range record.EffectiveRules {
		deadline, err := CalculateDeadline(rule, now, 30)
		if err != nil {
			return err
		}
		record.Controls[rule.ControlType] = deadline
	}
	if len(record.Instructions) > 0 {
		record.Reasons = append(record.Reasons, "Einweisungsanhänge noch in Quarantäne")
	}
	for _, block := range record.ActiveBlocks {
		record.Reasons = append(record.Reasons, "Aktive Sperre: "+block.Reason)
	}
	for _, kind := range []string{"STK", "MTK", "electrical"} {
		state := record.Controls[kind].State
		if state == "UNKNOWN" {
			record.Reasons = append(record.Reasons, kind+": Kontrollpflicht ungeklärt")
		}
		if state == "OVERDUE" {
			record.Reasons = append(record.Reasons, kind+": Kontrolle überfällig")
		}
	}
	return nil
}
func controlDevice(ctx context.Context, db *sql.DB, user, token, key, id, action string, input deviceControlInput, now time.Time) ([]byte, error) {
	bad := func(message string) ([]byte, error) { return nil, &stockError{400, "invalid_device_control", message} }
	if input.ExpectedVersion < 0 || !validReason(input.Evidence) || (action != "defect" && action != "details" && action != "master_data" && (!validReason(input.Qualification) || !validReason(input.Assignment))) || !validReason(input.Note) {
		return bad("Nachweis, Qualifikation, betrieblicher Auftrag und Begründung erforderlich.")
	}
	canonical, _ := json.Marshal(input)
	hash := digest("device_control:" + id + ":" + action + ":" + string(canonical))
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err = activeStockSession(ctx, tx, user, token, now); err != nil {
		return nil, err
	}
	var allowed bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM devices d JOIN locations l ON l.id=d.location_id JOIN role_assignments a ON a.site_id=l.site_id WHERE d.id=? AND a.user_id=? AND (a.role='device_manager' OR (?='defect' AND a.role='helper')))`, id, user, action).Scan(&allowed); err != nil {
		return nil, err
	}
	if !allowed {
		return nil, scopeError()
	}
	if err = rejectOtherKey(ctx, tx, user, key, "device_control_requests"); err != nil {
		return nil, err
	}
	var old, body string
	err = tx.QueryRowContext(ctx, `SELECT request_hash,response_json FROM device_control_requests WHERE user_id=? AND key=?`, user, key).Scan(&old, &body)
	if err == nil {
		if old != hash {
			return nil, conflict("idempotency_conflict", "Schlüssel gehört zu einem anderen Vorgang.")
		}
		return []byte(body), nil
	}
	if err != sql.ErrNoRows {
		return nil, err
	}
	record, err := readDevice(ctx, tx, user, id, now)
	if err != nil {
		return nil, err
	}
	if record.Version != input.ExpectedVersion {
		return nil, conflict("device_version_conflict", "Geräteakte wurde geändert; erneut prüfen.")
	}
	if action != "details" && input.Details != nil {
		return bad("Grunddaten separat erfassen.")
	}
	if action != "maintenance" && input.MaintenanceKind != "" {
		return bad("Instandhaltungsangaben separat erfassen.")
	}
	if action == "maintenance" {
		if err := validateMaintenance(input, now); err != nil {
			return nil, err
		}
	}
	var beforeMasterData *deviceMasterData
	if action != "master_data" && input.MasterData != nil {
		return bad("Stammdaten separat erfassen.")
	}
	if action == "master_data" {
		if err := validateMasterData(input, record, now); err != nil {
			return nil, err
		}
		before := currentMasterData(record)
		beforeMasterData = &before
	}
	var beforeDetails *deviceDetails
	identityReview := false
	if action == "details" {
		if err := validateDeviceDetails(input, record); err != nil {
			return nil, err
		}
		before := currentDeviceDetails(record)
		beforeDetails = &before
		identityReview = (before.Manufacturer != input.Details.Manufacturer || before.Model != input.Details.Model || before.SerialOrLot != input.Details.SerialOrLot) && (record.CommissionedOn != "" || len(record.Instructions) > 0 || len(record.InspectionDates) > 0 || len(record.Maintenances) > 0)
	}
	version := record.Version + 1
	if action != "instruction" && (input.Instructor != "" || input.Trainee != "" || input.InstructionScope != "" || input.TransferEvidence != "" || input.DocumentVersion != "" || (input.DocumentID != "" && action != "maintenance")) {
		return bad("Einweisungsangaben separat erfassen.")
	}

	if action == "commission" || action == "rules" {
		if input.ControlType != "" || input.Result != "" || input.Examiner != "" || input.Method != "" || input.NextDueOn != "" || input.BlockReason != "" || input.BlockID != "" {
			return bad("Prüf- und Sperrfelder separat erfassen.")
		}
	}

	if action == "commission" {
		if record.CommissionedOn != "" {
			return nil, conflict("already_commissioned", "Inbetriebnahme ist bereits dokumentiert.")
		}
		date, err := civilDate(input.PerformedOn)
		if err != nil || len(input.Rules) != 0 {
			return bad("Gültiges Inbetriebnahmedatum erforderlich; Regeln separat erfassen.")
		}
		berlin, err := time.LoadLocation("Europe/Berlin")
		if err != nil {
			return nil, err
		}
		if date.Format("2006-01-02") > now.In(berlin).Format("2006-01-02") {
			return bad("Inbetriebnahme darf nicht in der Zukunft liegen.")
		}
	} else if action == "rules" {
		if record.CommissionedOn == "" {
			return nil, conflict("commission_required", "Zuerst Inbetriebnahme nachweisen.")
		}
		if input.PerformedOn != "" || len(input.Rules) != 3 {
			return bad("STK, MTK und elektrische Kontrolle ausdrücklich bewerten.")
		}
		seen := map[string]bool{}
		for i := range input.Rules {
			rule := &input.Rules[i]
			if (rule.ControlType != "STK" && rule.ControlType != "MTK" && rule.ControlType != "electrical") || seen[rule.ControlType] || rule.BasisDate != "" || rule.Version != "" {
				return bad("Kontrollarten eindeutig angeben; Basis und Version werden serverseitig bestimmt.")
			}
			seen[rule.ControlType] = true
			rule.Version = strconv.Itoa(version)
			if !validReason(rule.Evidence) {
				return bad("Jede Kontrollbewertung benötigt einen Nachweis.")
			}
			if rule.Applicability == "unknown" {
				if rule.Mode != "" || rule.IntervalMonths != 0 || rule.ExactDate != "" || rule.EarlierDate != "" || rule.EarlierEvidence != "" {
					return bad("Ungeklärte Kontrollpflicht darf keine Frist enthalten.")
				}
			}
			if rule.Applicability == "required" {
				rule.BasisDate = record.CommissionedOn
				if previous, ok := record.EffectiveRules[rule.ControlType]; ok && previous.BasisDate != "" {
					rule.BasisDate = previous.BasisDate
				}
				if record.PassedVersions[rule.ControlType] > 0 {
					rule.BasisDate = record.PassedDates[rule.ControlType]
				}
			}
			if rule.ControlType == "MTK" && rule.Applicability == "required" && rule.Mode != "year_end" {
				return bad("MTK verwendet Jahresende; kürzere Fristen separat nachweisen.")
			}
			if _, err := CalculateDeadline(*rule, now, 30); err != nil {
				return bad("Ungültige Kontrollregel: " + err.Error())
			}
		}
	} else if action == "inspection" || action == "defect" || action == "release" || action == "instruction" {
		if err = validateDeviceAction(ctx, tx, user, action, &input, record, now); err != nil {
			return nil, err
		}
	} else if action != "details" && action != "master_data" && action != "maintenance" {
		return bad("Unbekannter Vorgang.")
	}

	calculated := map[string]Deadline{}
	for _, rule := range input.Rules {
		deadline, err := CalculateDeadline(rule, now, 30)
		if err != nil {
			return nil, err
		}
		calculated[rule.ControlType] = deadline
	}
	var document *documentRecord
	if action == "instruction" || (action == "maintenance" && input.DocumentID != "") {
		document = &documentRecord{}
		if err = tx.QueryRowContext(ctx, `SELECT id,device_id,filename,media_type,size,sha256,created_by,created_at,note,status FROM device_documents WHERE id=? AND device_id=?`, input.DocumentID, id).Scan(&document.ID, &document.DeviceID, &document.Filename, &document.MediaType, &document.Size, &document.SHA256, &document.CreatedBy, &document.CreatedAt, &document.Note, &document.Status); err != nil {
			if err == sql.ErrNoRows {
				return bad("Anhang gehört nicht zu dieser Geräteakte.")
			}
			return nil, err
		}
	}
	payload, _ := json.Marshal(struct {
		deviceControlInput
		Calculated       map[string]Deadline `json:"calculated"`
		Document         *documentRecord     `json:"document,omitempty"`
		BeforeMasterData *deviceMasterData   `json:"before_master_data,omitempty"`
		BeforeDetails    *deviceDetails      `json:"before_details,omitempty"`
		IdentityReview   bool                `json:"identity_review,omitempty"`
	}{input, calculated, document, beforeMasterData, beforeDetails, identityReview})
	event := newID()
	if _, err = tx.ExecContext(ctx, `INSERT INTO device_control_events VALUES(?,?,?,?,?,?,?)`, event, id, version, action, user, now.UTC().Format(time.RFC3339Nano), string(payload)); err != nil {
		return nil, err
	}
	if err = projectDevice(ctx, tx, &record, now); err != nil {
		return nil, err
	}
	encoded, _ := json.Marshal(record)
	if _, err = tx.ExecContext(ctx, `INSERT INTO device_control_outbox(event_id) VALUES(?)`, event); err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO device_control_requests VALUES(?,?,?,?,?)`, user, key, hash, event, string(encoded)); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return encoded, nil
}
func deviceControlRoutes(mux *http.ServeMux, a *authService) {
	documentRoutes(mux, a)
	mux.HandleFunc("GET /api/v1/devices/{id}/events", func(w http.ResponseWriter, r *http.Request) {
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
		if _, err = readDevice(r.Context(), tx, user.ID, r.PathValue("id"), a.now()); err != nil {
			sendStockError(w, err)
			return
		}
		rows, err := tx.QueryContext(r.Context(), `SELECT version,action,actor_id,created_at,payload_json FROM device_control_events WHERE device_id=? ORDER BY version DESC LIMIT 501`, r.PathValue("id"))
		if err != nil {
			sendStockError(w, err)
			return
		}
		defer rows.Close()
		events := []map[string]any{}
		for rows.Next() {
			var version int
			var action, actor, created, payload string
			if err = rows.Scan(&version, &action, &actor, &created, &payload); err != nil {
				sendStockError(w, err)
				return
			}
			events = append(events, map[string]any{"version": version, "action": action, "actor_id": actor, "created_at": created, "proof": json.RawMessage(payload)})
		}
		if err = rows.Err(); err != nil {
			sendStockError(w, err)
			return
		}
		if len(events) > 500 {
			events = events[:500]
			w.Header().Set("X-Has-More", "true")
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(events)
	})

	for _, action := range []string{"commission", "rules", "inspection", "defect", "release", "instruction", "details", "master_data", "maintenance"} {
		route := map[string]string{"inspection": "inspections", "defect": "defects", "release": "releases", "instruction": "instructions", "master_data": "master-data", "maintenance": "maintenance"}[action]
		if route == "" {
			route = action
		}
		mux.HandleFunc("POST /api/v1/devices/{id}/"+route, func(w http.ResponseWriter, r *http.Request) {
			user, ok := writeAuthorized(a, w, r)
			if !ok {
				return
			}
			var input deviceControlInput
			if !mutationBody(w, r, &input, 8192) {
				return
			}
			cookie, _ := r.Cookie(a.cookieName())
			body, err := controlDevice(r.Context(), a.db, user.ID, digest(cookie.Value), r.Header.Get("Idempotency-Key"), r.PathValue("id"), action, input, a.now())
			if err != nil {
				sendStockError(w, err)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.Write(body)
		})
	}
}
