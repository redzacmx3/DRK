package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"time"
)

func cycleHistory(ctx context.Context, tx *sql.Tx, id string) (*cycleEvent, error) {
	var payload string
	err := tx.QueryRowContext(ctx, `SELECT payload_json FROM reprocessing_events WHERE device_id=? ORDER BY version DESC LIMIT 1`, id).Scan(&payload)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var event cycleEvent
	err = json.Unmarshal([]byte(payload), &event)
	return &event, err
}
func (a *authService) mutateCycle(ctx context.Context, user, token, key, id string, in cycleInput) ([]byte, error) {
	if in.ExpectedVersion < 0 || in.ExpectedDeviceVersion < 0 || !validReason(in.Evidence) || !validReason(in.Qualification) || !validReason(in.Assignment) || !validReason(in.Note) {
		return nil, adminInvalid("Versionen, Nachweis, Qualifikation, Beauftragung und Begründung vollständig erfassen.")
	}
	if in.Action != "need" && in.Action != "cycle" && in.Action != "release" && in.Action != "reject" && in.Action != "revoke" {
		return nil, adminInvalid("Aufbereitungsbedarf, Zyklus, Ergebnisfreigabe, Ablehnung oder Widerruf auswählen.")
	}
	raw, _ := json.Marshal(in)
	hash := digest("reprocessing:" + id + ":" + string(raw))
	tx, err := a.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	now := a.now()
	if err := activeStockSession(ctx, tx, user, token, now); err != nil {
		return nil, err
	}
	if err := a.activeModule(ctx, tx, "reprocessing"); err != nil {
		return nil, err
	}
	manager, err := usageManager(ctx, tx, user, id)
	if err != nil {
		return nil, err
	}
	if !manager {
		return nil, scopeError()
	}
	record, err := readDevice(ctx, tx, user, id, now)
	if err != nil {
		return nil, err
	}
	if err := rejectOtherKey(ctx, tx, user, key, "reprocessing_requests"); err != nil {
		return nil, err
	}
	var previous, body string
	err = tx.QueryRowContext(ctx, `SELECT request_hash,response_json FROM reprocessing_requests WHERE user_id=? AND key=?`, user, key).Scan(&previous, &body)
	if err == nil {
		if previous != hash {
			return nil, conflict("idempotency_conflict", "Schlüssel gehört zu anderem Aufbereitungsnachweis.")
		}
		return []byte(body), nil
	}
	if err != sql.ErrNoRows {
		return nil, err
	}
	last, err := cycleHistory(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	version := 0
	if last != nil {
		version = last.Version
	}
	if version != in.ExpectedVersion || record.Version != in.ExpectedDeviceVersion {
		return nil, conflict("cycle_version_conflict", "Geräte- oder Aufbereitungsstand geändert; neu laden.")
	}
	if version >= 2000 {
		return nil, &stockError{413, "reprocessing_limit", "2000 Aufbereitungsereignisse erreicht."}
	}
	event := cycleEvent{ID: newID(), Version: version + 1, ActorID: user, CreatedAt: now.UTC().Format(time.RFC3339Nano), Input: in}
	cycleFields := in.PerformedAt != "" || in.CycleReference != "" || in.Executor != "" || in.Result != "" || in.ParametersEvidence != ""
	switch in.Action {
	case "need":
		if in.CycleVersion != 0 || in.ProcessVersion != 0 || in.Reviewed != nil || in.PerformedOn != "" || in.ValidUntil != "" || in.ExpectedBasis != "" || in.DocumentID != "" || in.ReleaseEvidence != "" || cycleFields {
			return nil, adminInvalid("Bedarf enthält keine Zyklus- oder Freigabefelder.")
		}
	case "cycle", "release":
		day, err := time.Parse("2006-01-02", in.PerformedOn)
		if err != nil || day.Format("2006-01-02") != in.PerformedOn || in.PerformedOn > now.UTC().Format("2006-01-02") {
			return nil, adminInvalid("Gültiges Nachweisdatum bis heute erforderlich.")
		}
		prereq, err := currentProcessPrerequisites(ctx, tx, record, now)
		if err != nil {
			return nil, err
		}
		if prereq.Status != "met" || prereq.Basis == "" || in.ExpectedBasis != prereq.Basis || (in.Action == "cycle" && in.ProcessVersion != prereq.ProcessVersion) {
			return nil, conflict("cycle_process_conflict", "Prozess, Geräte- oder Dokumentbasis nicht aktuell; neu laden und prüfen.")
		}
		event.Basis = prereq.Basis
		var status string
		err = tx.QueryRowContext(ctx, `SELECT status,sha256,review_version FROM effective_documents WHERE device_id=? AND id=?`, id, in.DocumentID).Scan(&status, &event.DocumentSHA256, &event.DocumentReviewVersion)
		if err == sql.ErrNoRows {
			return nil, scopeError()
		}
		if err != nil {
			return nil, err
		}
		if status != "released" {
			return nil, conflict("cycle_proof_not_released", "Zyklus- oder Freigabeunterlage nicht freigegeben.")
		}
		if in.Action == "cycle" {
			performed, err := time.Parse(time.RFC3339, in.PerformedAt)
			if err != nil || performed.After(now) || performed.UTC().Format("2006-01-02") != in.PerformedOn {
				return nil, adminInvalid("Zyklusabschluss mit gültigem Zeitpunkt bis jetzt und passendem UTC-Datum erfassen.")
			}
			if last != nil {
				boundary, err := time.Parse(time.RFC3339Nano, last.CreatedAt)
				if err != nil || performed.Before(boundary) {
					return nil, adminInvalid("Zyklusabschluss muss nach dem zuletzt dokumentierten Bedarf beziehungsweise vorherigen Ergebnis liegen.")
				}
			}
			if in.PerformedOn < prereq.ProcessReviewedOn {
				return nil, adminInvalid("Zyklusdatum darf nicht vor der maßgeblichen Prozessabnahme liegen.")
			}
			if !validReason(in.CycleReference) || !validReason(in.Executor) || !validReason(in.ParametersEvidence) || (in.Result != "passed" && in.Result != "failed") || in.CycleVersion != 0 || in.Reviewed != nil || in.ValidUntil != "" || in.ReleaseEvidence != "" {
				return nil, adminInvalid("Konkrete Zyklusreferenz, Ausführenden, Parameterbezug, Ergebnis und Protokoll erfassen; Ergebnisfreigabe separat.")
			}
			var exists bool
			if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM reprocessing_events WHERE device_id=? AND action='cycle' AND json_extract(payload_json,'$.input.cycle_reference')=?)`, id, in.CycleReference).Scan(&exists); err != nil {
				return nil, err
			}
			if exists {
				return nil, conflict("cycle_reference_exists", "Zyklusreferenz derselben Geräteakte bereits erfasst; neuen Zyklus ausdrücklich unterscheiden.")
			}
		} else {
			if last == nil || last.Input.Action != "cycle" || last.Version != in.CycleVersion || last.Input.Result != "passed" || last.Basis != prereq.Basis || last.Input.ProcessVersion != in.ProcessVersion {
				return nil, conflict("cycle_not_current", "Nur neuester bestandener, noch nicht freigegebener Zyklus mit aktueller Prozessbasis freigeben.")
			}
			cycleTime, timeErr := time.Parse(time.RFC3339Nano, last.CreatedAt)
			if timeErr != nil || cycleTime.After(now) {
				return nil, conflict("cycle_clock_conflict", "Zeitbasis des Zyklus ungeklärt; keine positive Freigabe.")
			}
			current, err := cycleDocumentCurrent(ctx, tx, id, *last)
			if err != nil {
				return nil, err
			}
			if !current {
				return nil, conflict("cycle_document_changed", "Zyklusunterlage geändert oder entzogen; neuen geprüften Zyklus erfassen.")
			}
			until, err := time.Parse(time.RFC3339, in.ValidUntil)
			if err != nil || !until.After(now) || until.After(now.Add(366*24*time.Hour)) || in.PerformedOn < last.Input.PerformedOn || in.Reviewed == nil || !*in.Reviewed || !validReason(in.ReleaseEvidence) || cycleFields {
				return nil, adminInvalid("Ergebnisfreigabe ausdrücklich bestätigen: Datum ab Zyklustag, Nachweis erfüllter Freigabekriterien und bestätigte Nutzungs-/Lagergültigkeit höchstens 366 Tage erforderlich.")
			}
		}
	case "reject", "revoke":
		if last == nil || in.CycleVersion <= 0 || in.ExpectedBasis != "" || in.ProcessVersion != 0 || in.Reviewed != nil || in.PerformedOn != "" || in.ValidUntil != "" || in.DocumentID != "" || in.ReleaseEvidence != "" || cycleFields {
			return nil, adminInvalid("Ablehnung/Widerruf benötigt den aktuellen Zyklus und Begründung, keine neue Durchführung oder positive Freigabe.")
		}
		if in.Action == "reject" && (last.Input.Action != "cycle" || last.Version != in.CycleVersion) || in.Action == "revoke" && (last.Input.Action != "release" || last.Input.CycleVersion != in.CycleVersion) {
			return nil, conflict("cycle_target_conflict", "Nur aktuellen Zyklus ablehnen beziehungsweise dessen aktuelle Freigabe widerrufen.")
		}
	}
	response, err := json.Marshal(event)
	if err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO reprocessing_events VALUES(?,?,?,?,?,?,?)`, event.ID, id, event.Version, user, in.Action, event.CreatedAt, string(response)); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO reprocessing_outbox(event_id) VALUES(?)`, event.ID); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO reprocessing_requests VALUES(?,?,?,?,?)`, user, key, hash, event.ID, string(response)); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return response, nil
}
func reprocessingRoutes(mux *http.ServeMux, a *authService) {
	mux.HandleFunc("GET /api/v1/devices/{id}/reprocessing", func(w http.ResponseWriter, r *http.Request) {
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
		assessment, err := assessCycles(r.Context(), tx, record, a.now())
		if err != nil {
			sendStockError(w, err)
			return
		}
		manager, err := usageManager(r.Context(), tx, actor.ID, record.ID)
		if err != nil {
			sendStockError(w, err)
			return
		}
		var prerequisites *cycleAssessment
		if manager {
			p, err := currentProcessPrerequisites(r.Context(), tx, record, a.now())
			if err != nil {
				sendStockError(w, err)
				return
			}
			prerequisites = &p
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(struct {
			Assessment    cycleAssessment  `json:"assessment"`
			Manager       bool             `json:"manager"`
			Prerequisites *cycleAssessment `json:"prerequisites,omitempty"`
		}{assessment, manager, prerequisites})
	})
	mux.HandleFunc("POST /api/v1/devices/{id}/reprocessing", func(w http.ResponseWriter, r *http.Request) {
		actor, ok := writeAuthorized(a, w, r)
		if !ok {
			return
		}
		var in cycleInput
		if !mutationBody(w, r, &in, 16*1024) {
			return
		}
		cookie, _ := r.Cookie(a.cookieName())
		body, err := a.mutateCycle(r.Context(), actor.ID, digest(cookie.Value), r.Header.Get("Idempotency-Key"), r.PathValue("id"), in)
		if err != nil {
			sendStockError(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(body)
	})
}
