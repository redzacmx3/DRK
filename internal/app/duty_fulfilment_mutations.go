package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"time"
)

func (a *authService) mutateDutyFulfilment(ctx context.Context, user, token, key, id string, in dutyFulfilmentInput) ([]byte, error) {
	if in.ExpectedVersion < 0 || in.ExpectedDeviceVersion < 0 || !validReason(in.Evidence) || !validReason(in.Qualification) || !validReason(in.Assignment) || !validReason(in.Note) {
		return nil, adminInvalid("Versionen und vollständige fachliche Prüfbegründung erforderlich.")
	}
	if in.Action != "accept" && in.Action != "reject" && in.Action != "revoke" {
		return nil, adminInvalid("Erfüllungsabnahme ausdrücklich abnehmen, ablehnen oder widerrufen.")
	}
	canonical, _ := json.Marshal(in)
	hash := digest("duty_fulfilment:" + id + ":" + string(canonical))
	tx, err := a.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err = activeStockSession(ctx, tx, user, token, a.now()); err != nil {
		return nil, err
	}
	if err = a.activeModule(ctx, tx, "compliance"); err != nil {
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
	if err = rejectOtherKey(ctx, tx, user, key, "duty_fulfilment_requests"); err != nil {
		return nil, err
	}
	var old, body string
	err = tx.QueryRowContext(ctx, `SELECT request_hash,response_json FROM duty_fulfilment_requests WHERE user_id=? AND key=?`, user, key).Scan(&old, &body)
	if err == nil {
		if old != hash {
			return nil, conflict("idempotency_conflict", "Schlüssel gehört zu einer anderen Erfüllungsabnahme.")
		}
		return []byte(body), nil
	}
	if err != sql.ErrNoRows {
		return nil, err
	}
	var version int
	if err = tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(version),0) FROM duty_fulfilment_events WHERE device_id=?`, id).Scan(&version); err != nil {
		return nil, err
	}
	if version != in.ExpectedVersion || record.Version != in.ExpectedDeviceVersion {
		return nil, conflict("duty_fulfilment_version_conflict", "Geräte- oder Erfüllungsnachweis geändert; neu laden und prüfen.")
	}
	if version >= 2000 {
		return nil, &stockError{413, "duty_fulfilment_limit", "2000 Erfüllungsabnahmen erreicht."}
	}
	now := a.now()
	event := dutyFulfilmentEvent{ID: newID(), Version: version + 1, ActorID: user, CreatedAt: now.UTC().Format(time.RFC3339Nano), Input: in}
	if in.Action != "revoke" {
		until, err := time.Parse(time.RFC3339, in.ValidUntil)
		date, dateErr := time.Parse("2006-01-02", in.PerformedOn)
		if err != nil || !until.After(now) || until.After(now.Add(366*24*time.Hour)) || dateErr != nil || date.Format("2006-01-02") != in.PerformedOn || in.PerformedOn > now.UTC().Format("2006-01-02") || in.Reviewed == nil || !*in.Reviewed || len(in.Areas) != len(productDutyKinds) {
			return nil, adminInvalid("Alle acht Bereiche ausdrücklich fachlich prüfen, Datum bis heute und Befristung höchstens 366 Tage erfassen.")
		}
		seen := map[string]bool{}
		for _, area := range in.Areas {
			known := false
			for _, kind := range productDutyKinds {
				if kind == area.Kind {
					known = true
				}
			}
			if !known || seen[area.Kind] || area.ScopeComplete == nil || !validReason(area.Evidence) || !validReason(area.Reason) {
				return nil, adminInvalid("Jeden der acht Bereiche genau einmal mit ausdrücklich bewertetem vollständigem Umfang, Nachweis und fachlicher Begründung erfassen.")
			}
			if in.Action == "accept" && !*area.ScopeComplete {
				return nil, adminInvalid("Unvollständig geprüfte Pflichtbereiche erlauben keine positive Erfüllungsabnahme.")
			}
			seen[area.Kind] = true
		}
		duties, err := assessProductDuty(ctx, tx, user, record, now)
		if err != nil {
			return nil, err
		}
		state, sources := dutyFulfilmentPrerequisites(record, duties)
		basis, err := dutyFulfilmentBasis(ctx, tx, record, duties, sources)
		if err != nil {
			return nil, err
		}
		if in.ExpectedBasis == "" || in.ExpectedBasis != basis {
			return nil, conflict("duty_fulfilment_basis_conflict", "Teilnachweise seit Laden geändert; neu laden und fachlich prüfen.")
		}
		if in.Action == "accept" && state != "met" {
			return nil, conflict("duty_fulfilment_not_current", "Erforderliche Teilnachweise offen, nicht aktuell oder blockiert; keine positive Erfüllungsabnahme.")
		}
		var status, reviewStamp string
		err = tx.QueryRowContext(ctx, `SELECT status,sha256,review_version FROM effective_documents WHERE device_id=? AND id=?`, id, in.DocumentID).Scan(&status, &event.DocumentSHA256, &event.DocumentReviewVersion)
		if err == sql.ErrNoRows {
			return nil, scopeError()
		}
		if err != nil {
			return nil, err
		}
		if status != "released" {
			return nil, conflict("duty_fulfilment_document_not_released", "Erfüllungsunterlage nicht freigegeben.")
		}
		if err = tx.QueryRowContext(ctx, `SELECT created_at FROM document_reviews WHERE document_id=? AND version=?`, in.DocumentID, event.DocumentReviewVersion).Scan(&reviewStamp); err != nil {
			return nil, err
		}
		reviewTime, err := time.Parse(time.RFC3339Nano, reviewStamp)
		if err != nil {
			return nil, err
		}
		earliest := reviewTime.UTC().Format("2006-01-02")
		if len(duties.Events) > 0 {
			stamp, err := time.Parse(time.RFC3339Nano, duties.Events[0].CreatedAt)
			if err != nil {
				return nil, err
			}
			if day := stamp.UTC().Format("2006-01-02"); day > earliest {
				earliest = day
			}
		}
		if in.PerformedOn < earliest {
			return nil, adminInvalid("Abnahmedatum darf nicht vor der aktuellen Pflichtbewertung oder Unterlagenfreigabe liegen.")
		}
		event.Basis = basis
		event.ProductDutyVersion = duties.Version
		event.Sources = sources
	} else {
		if in.ExpectedBasis != "" || in.Reviewed != nil || in.PerformedOn != "" || in.ValidUntil != "" || in.DocumentID != "" || len(in.Areas) != 0 {
			return nil, adminInvalid("Widerruf enthält keine neue Erfüllungsabnahme.")
		}
		var exists bool
		if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM duty_fulfilment_events WHERE device_id=?)`, id).Scan(&exists); err != nil {
			return nil, err
		}
		if !exists {
			return nil, conflict("duty_fulfilment_missing", "Keine Erfüllungsabnahme zum Widerruf vorhanden.")
		}
	}
	response, _ := json.Marshal(event)
	if _, err = tx.ExecContext(ctx, `INSERT INTO duty_fulfilment_events VALUES(?,?,?,?,?,?,?)`, event.ID, id, event.Version, user, in.Action, event.CreatedAt, string(response)); err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO duty_fulfilment_outbox(event_id) VALUES(?)`, event.ID); err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO duty_fulfilment_requests VALUES(?,?,?,?,?)`, user, key, hash, event.ID, string(response)); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return response, nil
}
func dutyFulfilmentRoutes(mux *http.ServeMux, a *authService) {
	mux.HandleFunc("GET /api/v1/devices/{id}/duty-fulfilment", func(w http.ResponseWriter, r *http.Request) {
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
		result, err := assessDutyFulfilment(r.Context(), tx, actor.ID, record, a.now())
		if err != nil {
			sendStockError(w, err)
			return
		}
		manager, err := usageManager(r.Context(), tx, actor.ID, record.ID)
		if err != nil {
			sendStockError(w, err)
			return
		}
		if !manager {
			result.Basis = ""
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(struct {
			Assessment dutyFulfilmentAssessment `json:"assessment"`
			Manager    bool                     `json:"manager"`
		}{result, manager})
	})
	mux.HandleFunc("POST /api/v1/devices/{id}/duty-fulfilment", func(w http.ResponseWriter, r *http.Request) {
		actor, ok := writeAuthorized(a, w, r)
		if !ok {
			return
		}
		var in dutyFulfilmentInput
		if !mutationBody(w, r, &in, 16*1024) {
			return
		}
		cookie, _ := r.Cookie(a.cookieName())
		body, err := a.mutateDutyFulfilment(r.Context(), actor.ID, digest(cookie.Value), r.Header.Get("Idempotency-Key"), r.PathValue("id"), in)
		if err != nil {
			sendStockError(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(body)
	})
}
