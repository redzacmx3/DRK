package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"time"
)

type manualAccessInput struct {
	ExpectedVersion       int    `json:"expected_version"`
	ExpectedDeviceVersion int    `json:"expected_device_version"`
	UserID                string `json:"user_id"`
	Action                string `json:"action"`
	DocumentID            string `json:"document_id"`
	ManualVersion         string `json:"manual_version"`
	Language              string `json:"language"`
	Accessible            *bool  `json:"accessible,omitempty"`
	ValidUntil            string `json:"valid_until"`
	Evidence              string `json:"evidence"`
	Qualification         string `json:"qualification"`
	Assignment            string `json:"assignment"`
	Note                  string `json:"note"`
}
type manualAccessEvent struct {
	ID             string            `json:"id"`
	Version        int               `json:"version"`
	ActorID        string            `json:"actor_id"`
	CreatedAt      string            `json:"created_at"`
	Input          manualAccessInput `json:"input"`
	Identity       string            `json:"identity_basis"`
	DocumentSHA256 string            `json:"document_sha256,omitempty"`
}
type manualAccessAssessment struct {
	UserID        string              `json:"user_id"`
	Version       int                 `json:"version"`
	DeviceVersion int                 `json:"device_version"`
	Status        string              `json:"status"`
	Reasons       []string            `json:"reasons"`
	Events        []manualAccessEvent `json:"events"`
	Overall       string              `json:"overall_readiness"`
}
type manualAccessSummary struct {
	UserID  string   `json:"user_id"`
	Version int      `json:"version"`
	Status  string   `json:"status"`
	Reasons []string `json:"reasons"`
}

func manualIdentity(ctx context.Context, tx *sql.Tx, record deviceRecord) (string, error) {
	var site string
	if err := tx.QueryRowContext(ctx, `SELECT site_id FROM locations WHERE id=?`, record.LocationID).Scan(&site); err != nil {
		return "", err
	}
	value, _ := json.Marshal(struct {
		Details  deviceDetails `json:"details"`
		Location string        `json:"location"`
		Site     string        `json:"site"`
	}{currentDeviceDetails(record), record.LocationID, site})
	return digest(string(value)), nil
}
func manualEligible(ctx context.Context, tx *sql.Tx, user string, record deviceRecord) (bool, error) {
	var eligible bool
	err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM users u JOIN role_assignments a ON a.user_id=u.id JOIN locations l ON l.site_id=a.site_id WHERE u.id=? AND u.active=1 AND l.id=?)`, user, record.LocationID).Scan(&eligible)
	return eligible, err
}
func assessManualAccess(ctx context.Context, tx *sql.Tx, actor, subject string, record deviceRecord, now time.Time) (result manualAccessAssessment, assessmentErr error) {
	result = manualAccessAssessment{UserID: subject, DeviceVersion: record.Version, Status: "unknown", Reasons: []string{}, Events: []manualAccessEvent{}, Overall: "not_assessed"}
	defer func() {
		if assessmentErr == nil {
			_, assessmentErr = maskModuleAssessment(ctx, tx, "documents", &result.Status, &result.Reasons)
		}
	}()
	manager, err := usageManager(ctx, tx, actor, record.ID)
	if err != nil {
		return result, err
	}
	if actor != subject && !manager {
		return result, scopeError()
	}
	var count int
	if err = tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(version),0),COUNT(*) FROM manual_access_events WHERE device_id=?`, record.ID).Scan(&result.Version, &count); err != nil {
		return result, err
	}
	if count > 2000 {
		return result, &stockError{413, "access_limit", "Mehr als 2000 Zugangsnachweise; Bewertung abgebrochen."}
	}
	rows, err := tx.QueryContext(ctx, `SELECT payload_json FROM manual_access_events WHERE device_id=? AND subject_id=? ORDER BY version DESC LIMIT 100`, record.ID, subject)
	if err != nil {
		return result, err
	}
	for rows.Next() {
		var payload string
		var event manualAccessEvent
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
	eligible, err := manualEligible(ctx, tx, subject, record)
	if err != nil {
		return result, err
	}
	if !eligible {
		result.Status = "blocked"
		result.Reasons = append(result.Reasons, "Benutzerkonto nicht aktiv im Gerätebereich.")
		return result, nil
	}
	if len(result.Events) == 0 {
		result.Reasons = append(result.Reasons, "Zugang zur passenden Gebrauchsanweisung für diese Person nicht bestätigt.")
		return result, nil
	}
	last := result.Events[0]
	if last.Input.Action == "revoke" {
		result.Status = "blocked"
		result.Reasons = append(result.Reasons, "Gebrauchsanweisungszugang ausdrücklich widerrufen.")
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
		result.Reasons = append(result.Reasons, "Befristeter Zugangsnachweis nicht aktuell; erneut prüfen.")
		return result, nil
	}
	identity, err := manualIdentity(ctx, tx, record)
	if err != nil {
		return result, err
	}
	if identity != last.Identity {
		result.Reasons = append(result.Reasons, "Geräteidentifikation oder Standortbereich geändert; Zugang erneut prüfen.")
		return result, nil
	}
	var status, sha string
	err = tx.QueryRowContext(ctx, `SELECT status,sha256 FROM effective_documents WHERE id=? AND device_id=?`, last.Input.DocumentID, record.ID).Scan(&status, &sha)
	if err != nil {
		return result, err
	}
	if status != "released" || sha != last.DocumentSHA256 {
		result.Reasons = append(result.Reasons, "Gebundene Gebrauchsanweisung aktuell nicht freigegeben.")
		return result, nil
	}
	result.Status = "confirmed"
	result.Reasons = append(result.Reasons, "Zugang zu der ausdrücklich benannten Unterlage für diese Person am Geräteort befristet bestätigt; keine vollständige Einsatzfreigabe.")
	return result, nil
}
func (a *authService) mutateManualAccess(ctx context.Context, user, token, key, id string, in manualAccessInput) ([]byte, error) {
	if in.ExpectedVersion < 0 || in.ExpectedDeviceVersion < 0 || in.UserID == "" || !validReason(in.Evidence) || !validReason(in.Qualification) || !validReason(in.Assignment) || !validReason(in.Note) {
		return nil, adminInvalid("Versionen, konkretes Konto und vollständige Prüfbegründung erforderlich.")
	}
	if in.Action != "confirm" && in.Action != "revoke" {
		return nil, adminInvalid("Zugang bestätigen oder widerrufen.")
	}
	canonical, _ := json.Marshal(in)
	hash := digest("manual_access:" + id + ":" + string(canonical))
	tx, err := a.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err = activeStockSession(ctx, tx, user, token, a.now()); err != nil {
		return nil, err
	}
	if err = a.activeModule(ctx, tx, "documents"); err != nil {
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
	if err = rejectOtherKey(ctx, tx, user, key, "manual_access_requests"); err != nil {
		return nil, err
	}
	var old, body string
	err = tx.QueryRowContext(ctx, `SELECT request_hash,response_json FROM manual_access_requests WHERE user_id=? AND key=?`, user, key).Scan(&old, &body)
	if err == nil {
		if old != hash {
			return nil, conflict("idempotency_conflict", "Schlüssel gehört zu einem anderen Zugangsnachweis.")
		}
		return []byte(body), nil
	}
	if err != sql.ErrNoRows {
		return nil, err
	}
	var version int
	if err = tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(version),0) FROM manual_access_events WHERE device_id=?`, id).Scan(&version); err != nil {
		return nil, err
	}
	if version != in.ExpectedVersion || record.Version != in.ExpectedDeviceVersion {
		return nil, conflict("access_version_conflict", "Geräte- oder Zugangsnachweis geändert; neu laden und abgleichen.")
	}
	if version >= 2000 {
		return nil, &stockError{413, "access_limit", "2000 Zugangsnachweise erreicht."}
	}
	now := a.now()
	identity, err := manualIdentity(ctx, tx, record)
	if err != nil {
		return nil, err
	}
	event := manualAccessEvent{ID: newID(), Version: version + 1, ActorID: user, CreatedAt: now.UTC().Format(time.RFC3339Nano), Input: in, Identity: identity}
	if in.Action == "confirm" {
		eligible, err := manualEligible(ctx, tx, in.UserID, record)
		if err != nil {
			return nil, err
		}
		if !eligible {
			return nil, scopeError()
		}
		until, err := time.Parse(time.RFC3339, in.ValidUntil)
		if err != nil || !until.After(now) || until.After(now.Add(24*time.Hour)) || in.Accessible == nil || !*in.Accessible || !validReason(in.ManualVersion) || !validReason(in.Language) {
			return nil, adminInvalid("Passende Unterlage, verständliche Sprache und tatsächlichen Zugang ausdrücklich bestätigen; Gültigkeit höchstens 24 Stunden.")
		}
		var status string
		if err = tx.QueryRowContext(ctx, `SELECT status,sha256 FROM effective_documents WHERE id=? AND device_id=?`, in.DocumentID, id).Scan(&status, &event.DocumentSHA256); err == sql.ErrNoRows {
			return nil, scopeError()
		} else if err != nil {
			return nil, err
		}
		if status != "released" {
			return nil, conflict("manual_not_released", "Gebrauchsanweisung nicht freigegeben.")
		}
	} else {
		if in.DocumentID != "" || in.ManualVersion != "" || in.Language != "" || in.ValidUntil != "" || in.Accessible != nil {
			return nil, adminInvalid("Widerruf enthält keine neue Zugangsbestätigung.")
		}
		var exists bool
		if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM manual_access_events WHERE device_id=? AND subject_id=? AND action='confirm')`, id, in.UserID).Scan(&exists); err != nil {
			return nil, err
		}
		if !exists {
			return nil, conflict("access_missing", "Kein Zugangsnachweis zum Widerruf vorhanden.")
		}
	}
	response, _ := json.Marshal(event)
	if _, err = tx.ExecContext(ctx, `INSERT INTO manual_access_events VALUES(?,?,?,?,?,?,?,?)`, event.ID, id, event.Version, user, in.UserID, in.Action, event.CreatedAt, string(response)); err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO manual_access_outbox(event_id) VALUES(?)`, event.ID); err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO manual_access_requests VALUES(?,?,?,?,?)`, user, key, hash, event.ID, string(response)); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return response, nil
}
func manualAccessRoutes(mux *http.ServeMux, a *authService) {
	mux.HandleFunc("GET /api/v1/devices/{id}/manual-access", func(w http.ResponseWriter, r *http.Request) {
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
		subject := r.URL.Query().Get("user_id")
		if subject == "" {
			subject = actor.ID
		}
		result, err := assessManualAccess(r.Context(), tx, actor.ID, subject, record, a.now())
		if err != nil {
			sendStockError(w, err)
			return
		}
		manager, err := usageManager(r.Context(), tx, actor.ID, record.ID)
		if err != nil {
			sendStockError(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(struct {
			Assessment manualAccessAssessment `json:"assessment"`
			Manager    bool                   `json:"manager"`
		}{result, manager})
	})
	mux.HandleFunc("POST /api/v1/devices/{id}/manual-access", func(w http.ResponseWriter, r *http.Request) {
		actor, ok := writeAuthorized(a, w, r)
		if !ok {
			return
		}
		var in manualAccessInput
		if !mutationBody(w, r, &in, 16*1024) {
			return
		}
		cookie, _ := r.Cookie(a.cookieName())
		body, err := a.mutateManualAccess(r.Context(), actor.ID, digest(cookie.Value), r.Header.Get("Idempotency-Key"), r.PathValue("id"), in)
		if err != nil {
			sendStockError(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(body)
	})
}
