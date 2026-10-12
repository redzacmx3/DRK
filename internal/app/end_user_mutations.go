package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"time"
)

func (a *authService) mutateEndUser(ctx context.Context, user, token, key, id string, in endUserInput) ([]byte, error) {
	if in.ExpectedVersion < 0 || in.ExpectedDeviceVersion < 0 || in.UserID == "" || !validReason(in.Evidence) || !validReason(in.Qualification) || !validReason(in.Assignment) || !validReason(in.Note) {
		return nil, adminInvalid("Versionen, konkretes Konto und vollständige Prüfbegründung erforderlich.")
	}
	if in.Action != "accept" && in.Action != "reject" && in.Action != "revoke" {
		return nil, adminInvalid("Endanwenderbewertung abnehmen, ablehnen oder widerrufen.")
	}
	canonical, _ := json.Marshal(in)
	hash := digest("end_user_acceptance:" + id + ":" + string(canonical))
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
	if err = rejectOtherKey(ctx, tx, user, key, "end_user_acceptance_requests"); err != nil {
		return nil, err
	}
	var old, body string
	err = tx.QueryRowContext(ctx, `SELECT request_hash,response_json FROM end_user_acceptance_requests WHERE user_id=? AND key=?`, user, key).Scan(&old, &body)
	if err == nil {
		if old != hash {
			return nil, conflict("idempotency_conflict", "Schlüssel gehört zu einem anderen Endanwendernachweis.")
		}
		return []byte(body), nil
	}
	if err != sql.ErrNoRows {
		return nil, err
	}
	var version int
	if err = tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(version),0) FROM end_user_acceptance_events WHERE device_id=?`, id).Scan(&version); err != nil {
		return nil, err
	}
	if version != in.ExpectedVersion || record.Version != in.ExpectedDeviceVersion {
		return nil, conflict("end_user_version_conflict", "Geräte- oder Endanwendernachweis geändert; neu laden und abgleichen.")
	}
	if version >= 2000 {
		return nil, &stockError{413, "end_user_limit", "2000 Endanwendernachweise erreicht."}
	}
	var subjectExists bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM users u WHERE u.id=? AND (EXISTS(SELECT 1 FROM role_assignments a JOIN locations l ON l.site_id=a.site_id WHERE a.user_id=u.id AND l.id=?) OR EXISTS(SELECT 1 FROM end_user_acceptance_events e WHERE e.subject_id=u.id AND e.device_id=?) OR EXISTS(SELECT 1 FROM usage_events e WHERE e.subject_id=u.id AND e.device_id=?) OR EXISTS(SELECT 1 FROM manual_access_events e WHERE e.subject_id=u.id AND e.device_id=?)))`, in.UserID, record.LocationID, id, id, id).Scan(&subjectExists); err != nil {
		return nil, err
	}
	if !subjectExists {
		return nil, scopeError()
	}
	now := a.now()
	identity, err := manualIdentity(ctx, tx, record)
	if err != nil {
		return nil, err
	}
	event := endUserEvent{ID: newID(), Version: version + 1, ActorID: user, CreatedAt: now.UTC().Format(time.RFC3339Nano), Input: in, Identity: identity}
	if in.Action != "revoke" {
		until, err := time.Parse(time.RFC3339, in.ValidUntil)
		date, dateErr := time.Parse("2006-01-02", in.PerformedOn)
		if err != nil || !until.After(now) || until.After(now.Add(366*24*time.Hour)) || dateErr != nil || date.Format("2006-01-02") != in.PerformedOn || in.PerformedOn > now.UTC().Format("2006-01-02") || in.Reviewed == nil || !*in.Reviewed {
			return nil, adminInvalid("Entscheidung ausdrücklich bestätigen: Abnahmedatum nicht zukünftig und Gültigkeit höchstens 366 Tage erforderlich.")
		}
		for _, value := range []string{in.CompetenceEvidence, in.InstructionEvidence, in.ManualEvidence} {
			if !validReason(value) {
				return nil, adminInvalid("Fachliche Nachweise zu Anwenderkompetenz, passender Einweisung oder Ausnahme und verständlicher Gebrauchsanweisung vollständig erfassen.")
			}
		}
		prerequisites, err := assessEndUserPrerequisites(ctx, tx, user, in.UserID, record, now)
		if err != nil {
			return nil, err
		}
		if in.ExpectedBasis != prerequisites.Basis {
			return nil, conflict("end_user_basis_conflict", "Personennachweise geändert; neu laden und fachlich abgleichen.")
		}
		if in.Action == "accept" && prerequisites.Status != "met" {
			return nil, conflict("end_user_not_current", "Qualifikation, Einweisung, Gebrauchsanweisungszugang oder Gerätebedingungen nicht aktuell; keine positive Teilabnahme möglich.")
		}
		if in.PerformedOn < prerequisites.EarliestDate {
			return nil, adminInvalid("Abnahmedatum darf nicht vor Einweisung oder Beginn der erforderlichen Qualifikationsgültigkeit liegen.")
		}
		event.Basis = prerequisites.Basis
		event.Sources = prerequisites.Sources
	} else {
		if in.ValidUntil != "" || in.PerformedOn != "" || in.Reviewed != nil || in.ExpectedBasis != "" || in.CompetenceEvidence != "" || in.InstructionEvidence != "" || in.ManualEvidence != "" {
			return nil, adminInvalid("Widerruf enthält keine neue Endanwenderabnahme.")
		}
		var exists bool
		if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM end_user_acceptance_events WHERE device_id=? AND subject_id=? AND action IN ('accept','reject'))`, id, in.UserID).Scan(&exists); err != nil {
			return nil, err
		}
		if !exists {
			return nil, conflict("end_user_missing", "Keine Endanwenderbewertung zum Widerruf vorhanden.")
		}
	}
	response, _ := json.Marshal(event)
	if _, err = tx.ExecContext(ctx, `INSERT INTO end_user_acceptance_events VALUES(?,?,?,?,?,?,?,?)`, event.ID, id, event.Version, user, in.UserID, in.Action, event.CreatedAt, string(response)); err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO end_user_acceptance_outbox(event_id) VALUES(?)`, event.ID); err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO end_user_acceptance_requests VALUES(?,?,?,?,?)`, user, key, hash, event.ID, string(response)); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return response, nil
}
func endUserRoutes(mux *http.ServeMux, a *authService) {
	mux.HandleFunc("GET /api/v1/devices/{id}/end-user-acceptance", func(w http.ResponseWriter, r *http.Request) {
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
		result, err := assessEndUser(r.Context(), tx, actor.ID, subject, record, a.now())
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
			Assessment endUserAssessment `json:"assessment"`
			Manager    bool              `json:"manager"`
		}{result, manager})
	})
	mux.HandleFunc("POST /api/v1/devices/{id}/end-user-acceptance", func(w http.ResponseWriter, r *http.Request) {
		actor, ok := writeAuthorized(a, w, r)
		if !ok {
			return
		}
		var in endUserInput
		if !mutationBody(w, r, &in, 16*1024) {
			return
		}
		cookie, _ := r.Cookie(a.cookieName())
		body, err := a.mutateEndUser(r.Context(), actor.ID, digest(cookie.Value), r.Header.Get("Idempotency-Key"), r.PathValue("id"), in)
		if err != nil {
			sendStockError(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(body)
	})
}
