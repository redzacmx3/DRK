package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"time"
)

func (a *authService) mutatePackRelease(ctx context.Context, user, token, key, id string, in packReleaseInput) ([]byte, error) {
	if in.ExpectedVersion < 0 || in.UserID == "" || len(in.UserID) > 100 || !validReason(in.Evidence) || !validReason(in.Qualification) || !validReason(in.Assignment) || !validReason(in.Note) {
		return nil, adminInvalid("Version, konkrete Person und vollständige fachliche Prüfbegründung erforderlich.")
	}
	if in.Action != "release" && in.Action != "reject" && in.Action != "revoke" {
		return nil, adminInvalid("Einsatzfreigabe ausdrücklich erteilen, ablehnen oder widerrufen.")
	}
	canonical, _ := json.Marshal(in)
	hash := digest("pack_release:" + id + ":" + string(canonical))
	tx, err := a.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err = activeStockSession(ctx, tx, user, token, a.now()); err != nil {
		return nil, err
	}
	p, err := loadPack(ctx, tx, user, id, "")
	if err != nil {
		return nil, err
	}
	manager, err := packReleaseManager(ctx, tx, user, p.Location)
	if err != nil {
		return nil, err
	}
	if !manager {
		return nil, scopeError()
	}
	if err = requireModule(ctx, tx, "release"); err != nil {
		return nil, err
	}
	if err = a.matchingModules(ctx, tx); err != nil {
		return nil, err
	}
	if err = rejectOtherKey(ctx, tx, user, key, "pack_release_requests"); err != nil {
		return nil, err
	}
	var old, body string
	err = tx.QueryRowContext(ctx, `SELECT request_hash,response_json FROM pack_release_requests WHERE user_id=? AND key=?`, user, key).Scan(&old, &body)
	if err == nil {
		if old != hash {
			return nil, conflict("idempotency_conflict", "Schlüssel gehört zu einer anderen Einsatzfreigabe.")
		}
		return []byte(body), nil
	}
	if err != sql.ErrNoRows {
		return nil, err
	}
	var version int
	if err = tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(version),0) FROM pack_release_events WHERE packlist_id=?`, id).Scan(&version); err != nil {
		return nil, err
	}
	if version != in.ExpectedVersion {
		return nil, conflict("pack_release_version_conflict", "Freigabehistorie geändert; neu laden und prüfen.")
	}
	if version >= 2000 {
		return nil, &stockError{413, "pack_release_limit", "2000 Einsatzfreigaben erreicht."}
	}
	exists, err := releaseUserExists(ctx, tx, in.UserID, p)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, scopeError()
	}
	now := a.now()
	actorVersion, err := administrativeVersion(ctx, tx, "user", user)
	if err != nil {
		return nil, err
	}
	event := packReleaseEvent{ID: newID(), Version: version + 1, ActorID: user, ActorVersion: actorVersion, CreatedAt: now.UTC().Format(time.RFC3339Nano), Input: in}
	if in.Action != "revoke" {
		until, err := time.Parse(time.RFC3339, in.ValidUntil)
		if err != nil || !until.After(now) || until.After(now.Add(24*time.Hour)) || in.PerformedOn != now.UTC().Format("2006-01-02") || in.Reviewed == nil || !*in.Reviewed {
			return nil, adminInvalid("Fachprüfung ausdrücklich bestätigen, Freigabedatum heute (UTC), Gültigkeit höchstens 24 Stunden erfassen.")
		}
		for _, value := range []string{in.MissionReference, in.OperatingScope, in.ConditionsEvidence, in.HandoverEvidence} {
			if !validReason(value) {
				return nil, adminInvalid("Konkreten Einsatzbezug, vollständigen Nutzungsumfang, Einsatzbedingungen und Übergabenachweis erfassen.")
			}
		}
		result, err := assessPackFor(ctx, tx, user, in.UserID, p, now)
		if err != nil {
			return nil, err
		}
		basis, err := packReleaseBasis(ctx, tx, in.UserID, p, result)
		if err != nil {
			return nil, err
		}
		if in.ExpectedBasis == "" || in.ExpectedBasis != basis {
			return nil, conflict("pack_release_basis_conflict", "Einsatzgrundlagen seit Laden geändert; neu laden und fachlich prüfen.")
		}
		if in.Action == "release" && result.Evidence.Status != "recorded_prerequisites_met" {
			return nil, conflict("pack_release_not_current", "Material-, Geräte-, Personen- oder Produktnachweise nicht vollständig aktuell; keine Einsatzfreigabe.")
		}
		event.Basis = basis
		event.Components = result.Evidence.Components
	} else {
		if in.ExpectedBasis != "" || in.Reviewed != nil || in.PerformedOn != "" || in.ValidUntil != "" || in.MissionReference != "" || in.OperatingScope != "" || in.ConditionsEvidence != "" || in.HandoverEvidence != "" {
			return nil, adminInvalid("Widerruf enthält keine neue Einsatzfreigabe.")
		}
		var found bool
		if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM pack_release_events WHERE packlist_id=? AND subject_id=?)`, id, in.UserID).Scan(&found); err != nil {
			return nil, err
		}
		if !found {
			return nil, conflict("pack_release_missing", "Keine Freigabeentscheidung für diese Person / Sollversion zum Widerruf vorhanden.")
		}
	}
	response, _ := json.Marshal(event)
	if _, err = tx.ExecContext(ctx, `INSERT INTO pack_release_events VALUES(?,?,?,?,?,?,?,?)`, event.ID, id, event.Version, user, in.UserID, in.Action, event.CreatedAt, string(response)); err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO pack_release_outbox(event_id) VALUES(?)`, event.ID); err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO pack_release_requests VALUES(?,?,?,?,?)`, user, key, hash, event.ID, string(response)); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return response, nil
}
func packReleaseRoutes(mux *http.ServeMux, a *authService) {
	read := func(export bool) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
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
			p, err := loadPack(r.Context(), tx, actor.ID, r.PathValue("id"), "")
			if err != nil {
				sendStockError(w, err)
				return
			}
			manager, err := packReleaseManager(r.Context(), tx, actor.ID, p.Location)
			if err != nil {
				sendStockError(w, err)
				return
			}
			subject := r.URL.Query().Get("user_id")
			if subject == "" {
				subject = actor.ID
			}
			if subject != actor.ID && !manager {
				sendStockError(w, scopeError())
				return
			}
			exists, err := releaseUserExists(r.Context(), tx, subject, p)
			if err != nil {
				sendStockError(w, err)
				return
			}
			if !exists {
				sendStockError(w, scopeError())
				return
			}
			result, err := assessPackFor(r.Context(), tx, actor.ID, subject, p, a.now())
			if err != nil {
				sendStockError(w, err)
				return
			}
			assessment, err := assessPackReleaseWith(r.Context(), tx, subject, p, result, a.now())
			if err != nil {
				sendStockError(w, err)
				return
			}
			if !manager {
				assessment.Basis = ""
			}
			if export {
				assessment.Basis = ""
				assessment.Events = nil
				events := []packReleaseEvent{}
				rows, err := tx.QueryContext(r.Context(), `SELECT payload_json FROM pack_release_events WHERE packlist_id=? AND subject_id=? ORDER BY version`, p.ID, subject)
				if err != nil {
					sendStockError(w, err)
					return
				}
				for rows.Next() {
					var raw string
					var event packReleaseEvent
					if err = rows.Scan(&raw); err != nil {
						rows.Close()
						sendStockError(w, err)
						return
					}
					if err = json.Unmarshal([]byte(raw), &event); err != nil {
						rows.Close()
						sendStockError(w, err)
						return
					}
					events = append(events, event)
				}
				err = rows.Err()
				rows.Close()
				if err != nil {
					sendStockError(w, err)
					return
				}
				buffer := &limitedExportBuffer{Remaining: 8 * 1024 * 1024}
				err = json.NewEncoder(buffer).Encode(struct {
					Format     int                   `json:"format_version"`
					Pack       packVersion           `json:"packlist"`
					Assessment packReleaseAssessment `json:"assessment"`
					Events     []packReleaseEvent    `json:"events"`
				}{1, p, assessment, events})
				if err != nil {
					apiError(w, 413, "export_too_large", "Freigabenarchiv überschreitet Größenlimit; kein Teilarchiv.")
					return
				}
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("Content-Disposition", "attachment; filename=\"einsatzfreigaben.json\"")
				w.Write(buffer.Bytes())
				return
			}
			type person struct {
				Active bool   `json:"active"`
				ID     string `json:"id"`
				Name   string `json:"name"`
			}
			people := []person{}
			if manager {
				rows, err := tx.QueryContext(r.Context(), `SELECT DISTINCT u.id,u.username,u.active FROM users u WHERE EXISTS(SELECT 1 FROM role_assignments x JOIN locations l ON l.site_id=x.site_id WHERE x.user_id=u.id AND l.id=?) OR EXISTS(SELECT 1 FROM pack_release_events e WHERE e.subject_id=u.id AND e.packlist_id=?) ORDER BY u.username,u.id LIMIT 1001`, p.Location, p.ID)
				if err != nil {
					sendStockError(w, err)
					return
				}
				for rows.Next() {
					var item person
					if err = rows.Scan(&item.ID, &item.Name, &item.Active); err != nil {
						rows.Close()
						sendStockError(w, err)
						return
					}
					people = append(people, item)
				}
				err = rows.Err()
				rows.Close()
				if err != nil {
					sendStockError(w, err)
					return
				}
				if len(people) > 1000 {
					apiError(w, 413, "release_people_limit", "Mehr als 1000 Personen; keine Teilübersicht.")
					return
				}
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(struct {
				Assessment packReleaseAssessment `json:"assessment"`
				Manager    bool                  `json:"manager"`
				People     []person              `json:"people"`
			}{assessment, manager, people})
		}
	}
	mux.HandleFunc("GET /api/v1/packlists/{id}/release", read(false))
	mux.HandleFunc("GET /api/v1/packlists/{id}/release/export.json", read(true))
	mux.HandleFunc("POST /api/v1/packlists/{id}/release", func(w http.ResponseWriter, r *http.Request) {
		actor, ok := writeAuthorized(a, w, r)
		if !ok {
			return
		}
		var in packReleaseInput
		if !mutationBody(w, r, &in, 16*1024) {
			return
		}
		cookie, _ := r.Cookie(a.cookieName())
		body, err := a.mutatePackRelease(r.Context(), actor.ID, digest(cookie.Value), r.Header.Get("Idempotency-Key"), r.PathValue("id"), in)
		if err != nil {
			sendStockError(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(body)
	})
}
