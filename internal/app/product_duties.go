package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"time"
)

// This review records applicability and evidence references. It never grants use permission.
var productDutyKinds = []string{"annex_1", "annex_2", "device_book", "manufacturer_maintenance", "it_security", "reprocessing", "special_user_group", "other"}

type productDutyRule struct {
	STKAcceptance         *dutySTKAcceptance         `json:"stk_acceptance,omitempty"`
	MTKAcceptance         *dutyMTKAcceptance         `json:"mtk_acceptance,omitempty"`
	BookAcceptance        *dutyBookAcceptance        `json:"book_acceptance,omitempty"`
	MaintenanceAcceptance *dutyMaintenanceAcceptance `json:"maintenance_acceptance,omitempty"`
	InstructionAcceptance *dutyInstructionAcceptance `json:"instruction_acceptance,omitempty"`
	FunctionCheck         *dutyFunctionProof         `json:"function_check,omitempty"`
	CommissionVersion     int                        `json:"commission_version,omitempty"`
	InstructionVersion    int                        `json:"instruction_version,omitempty"`
	DesignatedUserID      string                     `json:"designated_user_id,omitempty"`
	DesignatedAssignment  string                     `json:"designated_assignment,omitempty"`
	EventVersion          int                        `json:"event_version,omitempty"`
	Kind                  string                     `json:"kind"`
	Applicability         string                     `json:"applicability"`
	Reason                string                     `json:"reason"`
	Evidence              string                     `json:"evidence"`
}
type productDutyInput struct {
	ExpectedVersion       int               `json:"expected_version"`
	ExpectedDeviceVersion int               `json:"expected_device_version"`
	Action                string            `json:"action"`
	DocumentID            string            `json:"document_id"`
	ValidUntil            string            `json:"valid_until"`
	Reviewed              *bool             `json:"reviewed,omitempty"`
	Rules                 []productDutyRule `json:"rules"`
	Evidence              string            `json:"evidence"`
	Qualification         string            `json:"qualification"`
	Assignment            string            `json:"assignment"`
	Note                  string            `json:"note"`
}
type productDutyEvent struct {
	STKBasis                   *dutyBookBasis        `json:"stk_basis,omitempty"`
	MTKBasis                   *dutyBookBasis        `json:"mtk_basis,omitempty"`
	BookBasis                  *dutyBookBasis        `json:"book_basis,omitempty"`
	MaintenanceBasis           *dutyMaintenanceBasis `json:"maintenance_basis,omitempty"`
	InstructionAcceptanceBasis *dutyInstructionBasis `json:"instruction_acceptance_basis,omitempty"`
	FunctionDocumentSHA256     string                `json:"function_document_sha256,omitempty"`
	ID                         string                `json:"id"`
	Version                    int                   `json:"version"`
	ActorID                    string                `json:"actor_id"`
	CreatedAt                  string                `json:"created_at"`
	Input                      productDutyInput      `json:"input"`
	Identity                   string                `json:"identity_basis"`
	DocumentSHA256             string                `json:"document_sha256,omitempty"`
}
type productDutyAssessment struct {
	Checks        []productDutyCheck `json:"checks,omitempty"`
	Version       int                `json:"version"`
	DeviceVersion int                `json:"device_version"`
	Status        string             `json:"status"`
	Reasons       []string           `json:"reasons"`
	Events        []productDutyEvent `json:"events"`
	Overall       string             `json:"overall_readiness"`
}

func assessProductDuty(ctx context.Context, tx *sql.Tx, actor string, record deviceRecord, now time.Time) (productDutyAssessment, error) {
	result := productDutyAssessment{DeviceVersion: record.Version, Status: "unknown", Reasons: []string{}, Events: []productDutyEvent{}, Overall: "not_assessed"}
	var err error
	var count int
	if err = tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(version),0),COUNT(*) FROM product_duty_events WHERE device_id=?`, record.ID).Scan(&result.Version, &count); err != nil {
		return result, err
	}
	if count > 2000 {
		return result, &stockError{413, "product_duty_limit", "Mehr als 2000 Produktpflichtenbewertungen; Bewertung abgebrochen."}
	}
	rows, err := tx.QueryContext(ctx, `SELECT payload_json FROM product_duty_events WHERE device_id=? ORDER BY version DESC LIMIT 100`, record.ID)
	if err != nil {
		return result, err
	}
	for rows.Next() {
		var payload string
		var event productDutyEvent
		if err = rows.Scan(&payload); err != nil {
			rows.Close()
			return result, err
		}
		if err = json.Unmarshal([]byte(payload), &event); err != nil {
			rows.Close()
			return result, err
		}
		result.Events = append(result.Events, event)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return result, err
	}
	if len(result.Events) == 0 {
		result.Reasons = append(result.Reasons, "Besondere Produktpflichten noch nicht fachlich bewertet.")
		return result, nil
	}
	last := result.Events[0]
	if last.Input.Action == "revoke" {
		result.Status = "blocked"
		result.Reasons = append(result.Reasons, "Produktpflichtenbewertung ausdrücklich widerrufen.")
		return result, nil
	}
	until, err := time.Parse(time.RFC3339, last.Input.ValidUntil)
	if err != nil {
		return result, err
	}
	stamp, err := time.Parse(time.RFC3339Nano, last.CreatedAt)
	if err != nil {
		return result, err
	}
	if now.Before(stamp) || !now.Before(until) {
		result.Reasons = append(result.Reasons, "Produktpflichtenbewertung abgelaufen oder noch nicht aktuell; erneut prüfen.")
		return result, nil
	}
	identity, err := manualIdentity(ctx, tx, record)
	if err != nil {
		return result, err
	}
	if identity != last.Identity {
		result.Reasons = append(result.Reasons, "Geräteidentifikation oder Standortbereich geändert; Produktpflichten erneut prüfen.")
		return result, nil
	}
	var status, sha string
	err = tx.QueryRowContext(ctx, `SELECT status,sha256 FROM effective_documents WHERE id=? AND device_id=?`, last.Input.DocumentID, record.ID).Scan(&status, &sha)
	if err != nil {
		return result, err
	}
	if status != "released" || sha != last.DocumentSHA256 {
		result.Reasons = append(result.Reasons, "Gebundene Produktgrundlage aktuell nicht freigegeben.")
		return result, nil
	}
	for _, rule := range last.Input.Rules {
		if rule.Applicability == "unknown" {
			result.Reasons = append(result.Reasons, "Anwendbarkeit besonderer Pflichten noch ungeklärt: "+rule.Kind)
			return result, nil
		}
	}
	result.Checks, err = checkProductDutyProofs(ctx, tx, record, last.Input.Rules, last.FunctionDocumentSHA256, last.InstructionAcceptanceBasis, last.MaintenanceBasis)
	if err != nil {
		return result, err
	}
	for _, rule := range last.Input.Rules {
		if rule.Kind == "device_book" && rule.Applicability == "required" {
			for i := range result.Checks {
				if result.Checks[i].Kind == "device_book" {
					if err := checkDutyBookAcceptance(ctx, tx, record, last.Input.DocumentID, rule, last.BookBasis, &result.Checks[i]); err != nil {
						return result, err
					}
				}
			}
		}
	}
	for _, rule := range last.Input.Rules {
		if rule.Kind == "annex_2" && rule.Applicability == "required" {
			check, err := checkDutyMTK(ctx, tx, record, rule, last.Input.DocumentID, last.MTKBasis)
			if err != nil {
				return result, err
			}
			result.Checks = append(result.Checks, check)
			for i := range result.Checks {
				if result.Checks[i].Kind == "annex_2" {
					result.Checks[i].Status = check.Status
					result.Checks[i].Reason = "Anlage-2-MTK separat bewertet; produktspezifische Gesamtpflichten und Einsatzfreigabe bleiben offen."
					if check.Status == "accepted" {
						result.Checks[i].Status = "unknown"
					}
				}
			}
		}
	}
	for _, rule := range last.Input.Rules {
		if rule.Kind == "annex_1" && rule.Applicability == "required" {
			check, err := checkDutySTK(ctx, tx, record, rule, last.Input.DocumentID, last.STKBasis)
			if err != nil {
				return result, err
			}
			result.Checks = append(result.Checks, check)
			if check.Status == "blocked" {
				for i := range result.Checks {
					if result.Checks[i].Kind == "annex_1" {
						result.Checks[i].Status = "blocked"
					}
				}
			}
		}
	}
	result.Status = "reviewed"
	result.Reasons = append(result.Reasons, "Anwendbarkeit und Nachweisreferenzen fachlich dokumentiert; Erfüllung der besonderen Pflichten und Einsatzfreigabe noch gesondert abzunehmen.")
	return result, nil
}
func (a *authService) mutateProductDuty(ctx context.Context, user, token, key, id string, in productDutyInput) ([]byte, error) {
	if in.ExpectedVersion < 0 || in.ExpectedDeviceVersion < 0 || !validReason(in.Evidence) || !validReason(in.Qualification) || !validReason(in.Assignment) || !validReason(in.Note) {
		return nil, adminInvalid("Versionen, vollständige Prüfbegründung erforderlich.")
	}
	if in.Action != "review" && in.Action != "revoke" {
		return nil, adminInvalid("Produktpflichten bewerten oder Bewertung widerrufen.")
	}
	canonical, _ := json.Marshal(in)
	hash := digest("product_duty:" + id + ":" + string(canonical))
	tx, err := a.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err = activeStockSession(ctx, tx, user, token, a.now()); err != nil {
		return nil, err
	}
	manager, err := usageManager(ctx, tx, user, id)
	if err != nil {
		return nil, err
	}
	if !manager {
		return nil, scopeError()
	}
	record, err := readDevice(ctx, tx, user, id, a.now())
	if err != nil {
		return nil, err
	}
	if err = rejectOtherKey(ctx, tx, user, key, "product_duty_requests"); err != nil {
		return nil, err
	}
	var old, body string
	err = tx.QueryRowContext(ctx, `SELECT request_hash,response_json FROM product_duty_requests WHERE user_id=? AND key=?`, user, key).Scan(&old, &body)
	if err == nil {
		if old != hash {
			return nil, conflict("idempotency_conflict", "Schlüssel gehört zu einem anderen Produktpflichtenbewertung.")
		}
		return []byte(body), nil
	}
	if err != sql.ErrNoRows {
		return nil, err
	}
	var version int
	if err = tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(version),0) FROM product_duty_events WHERE device_id=?`, id).Scan(&version); err != nil {
		return nil, err
	}
	if version != in.ExpectedVersion || record.Version != in.ExpectedDeviceVersion {
		return nil, conflict("product_duty_version_conflict", "Geräte- oder Produktpflichtenbewertung geändert; neu laden und abgleichen.")
	}
	if version >= 2000 {
		return nil, &stockError{413, "product_duty_limit", "2000 Produktpflichtenbewertungen erreicht."}
	}
	now := a.now()
	identity, err := manualIdentity(ctx, tx, record)
	if err != nil {
		return nil, err
	}
	event := productDutyEvent{ID: newID(), Version: version + 1, ActorID: user, CreatedAt: now.UTC().Format(time.RFC3339Nano), Input: in, Identity: identity}
	if in.Action == "review" {
		until, err := time.Parse(time.RFC3339, in.ValidUntil)
		if err != nil || !until.After(now) || until.After(now.Add(366*24*time.Hour)) || in.Reviewed == nil || !*in.Reviewed || len(in.Rules) != len(productDutyKinds) {
			return nil, adminInvalid("Ausdrückliche fachliche Prüfung aller acht Pflichtbereiche und Gültigkeit höchstens 366 Tage erforderlich.")
		}
		seen := map[string]bool{}
		for _, rule := range in.Rules {
			known := false
			for _, kind := range productDutyKinds {
				if rule.Kind == kind {
					known = true
				}
			}
			if !known || seen[rule.Kind] || !validReason(rule.Reason) || (rule.Applicability != "required" && rule.Applicability != "exempt" && rule.Applicability != "unknown") || (rule.Applicability == "required" && !validReason(rule.Evidence)) || (rule.Evidence != "" && !validReason(rule.Evidence)) {
				return nil, adminInvalid("Jeden Pflichtbereich genau einmal mit Anwendbarkeit, Begründung und bei erforderlichen Pflichten einer Nachweisreferenz erfassen.")
			}
			if err := validateProductDutyReference(rule, record); err != nil {
				return nil, err
			}
			if err := validateAnnex1Reference(ctx, tx, rule, record); err != nil {
				return nil, err
			}
			sha, err := validateDutyFunction(ctx, tx, rule, record, now)
			if err != nil {
				return nil, err
			}
			if sha != "" {
				event.FunctionDocumentSHA256 = sha
			}
			basis, err := validateDutyInstructionAcceptance(ctx, tx, rule, record, now)
			if err != nil {
				return nil, err
			}
			if basis != nil {
				event.InstructionAcceptanceBasis = basis
			}
			maintenanceBasis, err := validateDutyMaintenanceAcceptance(ctx, tx, rule, record, in.DocumentID, now)
			if err != nil {
				return nil, err
			}
			if maintenanceBasis != nil {
				event.MaintenanceBasis = maintenanceBasis
			}
			bookBasis, err := validateDutyBookAcceptance(ctx, tx, rule, record, in.DocumentID, now)
			if err != nil {
				return nil, err
			}
			if bookBasis != nil {
				event.BookBasis = bookBasis
			}
			mtkBasis, err := validateDutyMTK(ctx, tx, record, rule, in.DocumentID, now)
			if err != nil {
				return nil, err
			}
			if mtkBasis != nil {
				event.MTKBasis = mtkBasis
			}
			stkBasis, err := validateDutySTK(ctx, tx, record, rule, in.DocumentID, now)
			if err != nil {
				return nil, err
			}
			if stkBasis != nil {
				event.STKBasis = stkBasis
			}
			seen[rule.Kind] = true
		}
		var status string
		if err = tx.QueryRowContext(ctx, `SELECT status,sha256 FROM effective_documents WHERE id=? AND device_id=?`, in.DocumentID, id).Scan(&status, &event.DocumentSHA256); err == sql.ErrNoRows {
			return nil, scopeError()
		} else if err != nil {
			return nil, err
		}
		if status != "released" {
			return nil, conflict("product_source_not_released", "Produktgrundlage nicht freigegeben.")
		}
	} else {
		if in.DocumentID != "" || in.ValidUntil != "" || in.Reviewed != nil || len(in.Rules) != 0 {
			return nil, adminInvalid("Widerruf enthält keine neue Produktpflichtenbewertung.")
		}
		var exists bool
		if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM product_duty_events WHERE device_id=? AND action='review')`, id).Scan(&exists); err != nil {
			return nil, err
		}
		if !exists {
			return nil, conflict("product_duty_missing", "Keine Produktpflichtenbewertung zum Widerruf vorhanden.")
		}
	}
	response, _ := json.Marshal(event)
	if _, err = tx.ExecContext(ctx, `INSERT INTO product_duty_events VALUES(?,?,?,?,?,?,?)`, event.ID, id, event.Version, user, in.Action, event.CreatedAt, string(response)); err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO product_duty_outbox(event_id) VALUES(?)`, event.ID); err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO product_duty_requests VALUES(?,?,?,?,?)`, user, key, hash, event.ID, string(response)); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return response, nil
}
func productDutyRoutes(mux *http.ServeMux, a *authService) {
	mux.HandleFunc("GET /api/v1/devices/{id}/product-duties", func(w http.ResponseWriter, r *http.Request) {
		actor, err := a.session(r)
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
		record, err := readDevice(r.Context(), tx, actor.ID, r.PathValue("id"), a.now())
		if err != nil {
			sendStockError(w, err)
			return
		}
		result, err := assessProductDuty(r.Context(), tx, actor.ID, record, a.now())
		if err != nil {
			sendStockError(w, err)
			return
		}
		manager, err := usageManager(r.Context(), tx, actor.ID, record.ID)
		if err != nil {
			sendStockError(w, err)
			return
		}
		commissions, err := dutyCommissionChoices(r.Context(), tx, record.ID)
		if err != nil {
			sendStockError(w, err)
			return
		}
		people, err := dutyPersonChoices(r.Context(), tx, record, manager)
		if err != nil {
			sendStockError(w, err)
			return
		}
		bookSnapshot := ""
		if manager {
			bookSnapshot, err = currentDutyBookSnapshot(r.Context(), tx, record)
			if err != nil {
				sendStockError(w, err)
				return
			}
		}
		inspections, err := dutyInspectionChoices(r.Context(), tx, record.ID)
		if err != nil {
			sendStockError(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(struct {
			Inspections  []dutyInspectionChoice `json:"inspections"`
			BookSnapshot string                 `json:"book_snapshot,omitempty"`
			LocationID   string                 `json:"location_id"`
			Assessment   productDutyAssessment  `json:"assessment"`
			Manager      bool                   `json:"manager"`
			Maintenance  []maintenanceProof     `json:"maintenance"`
			Commissions  []dutyCommissionChoice `json:"commissions"`
			Instructions []instructionProof     `json:"instructions"`
			People       []dutyPersonChoice     `json:"people"`
		}{inspections, bookSnapshot, record.LocationID, result, manager, record.Maintenances, commissions, record.Instructions, people})
	})
	mux.HandleFunc("POST /api/v1/devices/{id}/product-duties", func(w http.ResponseWriter, r *http.Request) {
		actor, ok := writeAuthorized(a, w, r)
		if !ok {
			return
		}
		var in productDutyInput
		if !mutationBody(w, r, &in, 16*1024) {
			return
		}
		cookie, _ := r.Cookie(a.cookieName())
		body, err := a.mutateProductDuty(r.Context(), actor.ID, digest(cookie.Value), r.Header.Get("Idempotency-Key"), r.PathValue("id"), in)
		if err != nil {
			sendStockError(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(body)
	})
}
