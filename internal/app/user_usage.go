package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"regexp"
	"sort"
	"time"
)

type usageInput struct {
	ExpectedDeviceVersion int      `json:"expected_device_version"`
	Action                string   `json:"action"`
	ExpectedVersion       int      `json:"expected_version"`
	UserID                string   `json:"user_id"`
	InstructionMode       string   `json:"instruction_mode"`
	Codes                 []string `json:"codes"`
	Code                  string   `json:"code"`
	ValidFrom             string   `json:"valid_from"`
	ValidUntil            string   `json:"valid_until"`
	InstructionVersion    int      `json:"instruction_version"`
	Evidence              string   `json:"evidence"`
	Qualification         string   `json:"qualification"`
	Assignment            string   `json:"assignment"`
	Note                  string   `json:"note"`
}
type usageEvent struct {
	ID        string     `json:"id"`
	Version   int        `json:"version"`
	Actor     string     `json:"actor"`
	UserID    *string    `json:"user_id"`
	CreatedAt string     `json:"created_at"`
	Input     usageInput `json:"input"`
	Identity  string     `json:"identity_basis"`
}
type usageAssessment struct {
	DeviceVersion   int          `json:"device_version"`
	UserID          string       `json:"user_id"`
	Version         int          `json:"version"`
	Status          string       `json:"status"`
	InstructionMode string       `json:"instruction_mode"`
	Codes           []string     `json:"codes"`
	Reasons         []string     `json:"reasons"`
	Events          []usageEvent `json:"events"`
	AsOf            string       `json:"as_of"`
	Overall         string       `json:"overall_readiness"`
}

var usageCode = regexp.MustCompile(`^[a-z][a-z0-9_-]{1,39}$`)

func usageManager(ctx context.Context, tx *sql.Tx, user, device string) (bool, error) {
	var ok bool
	err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM devices d JOIN locations l ON l.id=d.location_id JOIN role_assignments a ON a.site_id=l.site_id WHERE d.id=? AND a.user_id=? AND a.role='device_manager')`, device, user).Scan(&ok)
	return ok, err
}
func assessUsage(ctx context.Context, tx *sql.Tx, actor, subject string, record deviceRecord, now time.Time) (usageAssessment, error) {
	result := usageAssessment{DeviceVersion: record.Version, UserID: subject, Status: "unknown", InstructionMode: "unknown", Codes: []string{}, Reasons: []string{}, Events: []usageEvent{}, AsOf: now.UTC().Format(time.RFC3339Nano), Overall: "not_assessed"}
	manager, err := usageManager(ctx, tx, actor, record.ID)
	if err != nil {
		return result, err
	}
	if subject != actor && !manager {
		return result, scopeError()
	}
	var eligible bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM users u JOIN role_assignments a ON a.user_id=u.id JOIN locations l ON l.site_id=a.site_id WHERE u.id=? AND u.active=1 AND l.id=? AND a.role IN ('viewer','helper','material_manager','device_manager'))`, subject, record.LocationID).Scan(&eligible); err != nil {
		return result, err
	}
	if !eligible {
		result.Status = "blocked"
		result.Reasons = append(result.Reasons, "Konto inaktiv oder ohne aktuellen Betriebsstättenbereich.")
	}
	rows, err := tx.QueryContext(ctx, `SELECT e.id,e.version,u.username,e.subject_id,e.created_at,e.payload_json FROM usage_events e JOIN users u ON u.id=e.actor_id WHERE e.device_id=? ORDER BY e.version LIMIT 2001`, record.ID)
	if err != nil {
		return result, err
	}
	events := []usageEvent{}
	for rows.Next() {
		var e usageEvent
		var raw string
		if err = rows.Scan(&e.ID, &e.Version, &e.Actor, &e.UserID, &e.CreatedAt, &raw); err != nil {
			rows.Close()
			return result, err
		}
		var payload struct {
			Input    usageInput `json:"input"`
			Identity string     `json:"identity_basis"`
		}
		if err = json.Unmarshal([]byte(raw), &payload); err != nil {
			rows.Close()
			return result, err
		}
		e.Input = payload.Input
		e.Identity = payload.Identity
		events = append(events, e)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return result, err
	}
	if len(events) > 2000 {
		return result, &stockError{413, "usage_limit", "Mehr als 2000 Personennachweise je Gerät; keine Teilbewertung erstellt."}
	}
	var policy *usageInput
	policyIdentity := ""
	qualifications := map[string]usageEvent{}
	var binding *usageEvent
	for i := range events {
		e := events[i]
		result.Version = e.Version
		if e.Input.Action == "policy" {
			copy := e.Input
			policy = &copy
			policyIdentity = e.Identity
		}
		if e.UserID != nil && *e.UserID == subject {
			if e.Input.Action == "qualify" || e.Input.Action == "revoke" {
				qualifications[e.Input.Code] = e
			}
			if e.Input.Action == "unbind" {
				binding = nil
			}
			if e.Input.Action == "bind" {
				copy := e
				binding = &copy
			}
		}
		if e.UserID == nil || *e.UserID == subject {
			result.Events = append(result.Events, e)
		}
	}
	if policy == nil {
		result.Reasons = append(result.Reasons, "Einweisungspflicht und notwendige Qualifikationen noch nicht ausdrücklich bewertet.")
		return result, nil
	}
	result.InstructionMode = policy.InstructionMode
	result.Codes = policy.Codes
	identity, _ := json.Marshal(currentDeviceDetails(record))
	identityHash := digest(string(identity))
	all := eligible
	if policyIdentity != identityHash {
		all = false
		result.Reasons = append(result.Reasons, "Geräteidentifikation geändert; Pflichtbewertung anhand aktueller Unterlagen erneut prüfen.")
	}
	for _, code := range policy.Codes {
		proof, exists := qualifications[code]
		if !exists {
			all = false
			result.Reasons = append(result.Reasons, code+": konkreter Qualifikationsnachweis fehlt.")
			continue
		}
		if proof.Input.Action == "revoke" {
			all = false
			result.Status = "blocked"
			result.Reasons = append(result.Reasons, code+": Nachweis ausdrücklich widerrufen.")
			continue
		}
		day := localDay(now)
		if day < proof.Input.ValidFrom || day > proof.Input.ValidUntil {
			all = false
			result.Status = "blocked"
			result.Reasons = append(result.Reasons, code+": Nachweis am Bewertungstag nicht gültig.")
		}
		if proof.Identity != identityHash {
			all = false
			result.Reasons = append(result.Reasons, code+": Geräteidentifikation geändert; Zuordnung erneut prüfen.")
		}
	}
	if policy.InstructionMode == "required" {
		if binding == nil {
			all = false
			result.Reasons = append(result.Reasons, "Kein Einweisungsereignis ausdrücklich diesem Benutzerkonto zugeordnet.")
		} else {
			found := false
			for _, proof := range record.Instructions {
				if proof.Version == binding.Input.InstructionVersion {
					found = true
					if binding.Identity != identityHash {
						all = false
						result.Reasons = append(result.Reasons, "Einweisungszuordnung nach Identifikationsänderung erneut prüfen.")
					}
					if proof.DocumentStatus != "released" {
						all = false
						result.Reasons = append(result.Reasons, "Einweisungsanhang nicht fachlich freigegeben (Quarantäne).")
					}
				}
			}
			if !found {
				all = false
				result.Reasons = append(result.Reasons, "Zugeordnetes Einweisungsereignis nicht vorhanden.")
			}
		}
	}
	if all {
		result.Status = "recorded_requirements_met"
		result.Reasons = append(result.Reasons, "Die ausdrücklich erfassten personenbezogenen Anforderungen sind erfüllt; keine vollständige Einsatzfreigabe.")
	}
	return result, nil
}
func mutateUsage(ctx context.Context, a *authService, user, token, key, device string, in usageInput) ([]byte, error) {
	tx, err := a.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err = activeStockSession(ctx, tx, user, token, a.now()); err != nil {
		return nil, err
	}
	manager, err := usageManager(ctx, tx, user, device)
	if err != nil {
		return nil, err
	}
	if !manager {
		return nil, scopeError()
	}
	record, err := readDevice(ctx, tx, user, device, a.now())
	if err != nil {
		return nil, err
	}
	if err = rejectOtherKey(ctx, tx, user, key, "usage_requests"); err != nil {
		return nil, err
	}
	canonical, _ := json.Marshal(in)
	hash := digest(device + ":" + string(canonical))
	var oldHash, old string
	err = tx.QueryRowContext(ctx, `SELECT request_hash,response_json FROM usage_requests WHERE user_id=? AND key=?`, user, key).Scan(&oldHash, &old)
	if err == nil {
		if hash != oldHash {
			return nil, conflict("idempotency_conflict", "Schlüssel gehört zu einem anderen Personennachweis.")
		}
		return []byte(old), tx.Commit()
	}
	if err != sql.ErrNoRows {
		return nil, err
	}
	var version int
	if err = tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(version),0) FROM usage_events WHERE device_id=?`, device).Scan(&version); err != nil {
		return nil, err
	}
	if in.ExpectedDeviceVersion != record.Version {
		return nil, conflict("device_changed", "Geräteakte geändert. Personenprüfung neu laden.")
	}
	if version >= 2000 {
		return nil, &stockError{413, "usage_limit", "Nachweisgrenze erreicht; kein weiteres Ereignis gespeichert."}
	}
	if in.ExpectedVersion != version {
		return nil, conflict("usage_changed", "Personennachweise geändert. Neu laden und abgleichen.")
	}
	if !validReason(in.Evidence) || !validReason(in.Qualification) || !validReason(in.Assignment) || !validReason(in.Note) {
		return nil, adminInvalid("Nachweis, Qualifikation der prüfenden Person, Auftrag und Begründung erforderlich.")
	}
	switch in.Action {
	case "policy":
		if in.UserID != "" || in.Code != "" || in.ValidFrom != "" || in.ValidUntil != "" || in.InstructionVersion != 0 || (in.InstructionMode != "required" && in.InstructionMode != "exempt") || len(in.Codes) < 1 || len(in.Codes) > 20 {
			return nil, adminInvalid("Einweisungspflicht required/exempt und ausdrücklich notwendige Qualifikationscodes erforderlich.")
		}
		sort.Strings(in.Codes)
		for i, code := range in.Codes {
			if !usageCode.MatchString(code) || i > 0 && code == in.Codes[i-1] {
				return nil, adminInvalid("Qualifikationscodes: 2–40 kleine ASCII-Zeichen, Ziffern, Unterstrich oder Bindestrich; keine Duplikate.")
			}
		}
	case "qualify", "revoke", "bind", "unbind":
		if in.UserID == "" || in.InstructionMode != "" || len(in.Codes) > 0 {
			return nil, adminInvalid("Konkretes Benutzerkonto auswählen; Richtlinien separat ändern.")
		}
		var eligible bool
		if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM users u JOIN role_assignments a ON a.user_id=u.id JOIN locations l ON l.site_id=a.site_id WHERE u.id=? AND u.active=1 AND l.id=?)`, in.UserID, record.LocationID).Scan(&eligible); err != nil {
			return nil, err
		}
		if in.Action == "revoke" || in.Action == "unbind" {
			if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM usage_events WHERE device_id=? AND subject_id=?)`, device, in.UserID).Scan(&eligible); err != nil {
				return nil, err
			}
		}
		if !eligible {
			return nil, scopeError()
		}
		if in.Action == "unbind" && (in.Code != "" || in.ValidFrom != "" || in.ValidUntil != "" || in.InstructionVersion != 0) {
			return nil, adminInvalid("Zuordnungswiderruf enthält keine neuen Qualifikations- oder Einweisungsfelder.")
		}
		if in.Action == "bind" {
			if in.Code != "" || in.ValidFrom != "" || in.ValidUntil != "" || in.InstructionVersion < 1 {
				return nil, adminInvalid("Einweisungsereignis ausdrücklich auswählen.")
			}
			found := false
			for _, proof := range record.Instructions {
				if proof.Version == in.InstructionVersion {
					found = true
				}
			}
			if !found {
				return nil, adminInvalid("Einweisungsereignis gehört nicht zu dieser Akte.")
			}
		}
		if in.Action == "qualify" || in.Action == "revoke" {
			if !usageCode.MatchString(in.Code) || in.InstructionVersion != 0 {
				return nil, adminInvalid("Qualifikationscode ausdrücklich angeben.")
			}
			if in.Action == "qualify" {
				if !validDay(in.ValidFrom) || !validDay(in.ValidUntil) || in.ValidFrom > in.ValidUntil || in.ValidFrom > localDay(a.now()) {
					return nil, adminInvalid("Bestätigter Gültigkeitszeitraum erforderlich; Beginn nicht zukünftig.")
				}
			} else {
				if in.ValidFrom != "" || in.ValidUntil != "" {
					return nil, adminInvalid("Widerruf enthält keinen neuen Gültigkeitszeitraum.")
				}
				var exists bool
				if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM usage_events WHERE device_id=? AND subject_id=? AND action='qualify' AND json_extract(payload_json,'$.input.code')=?)`, device, in.UserID, in.Code).Scan(&exists); err != nil {
					return nil, err
				}
				if !exists {
					return nil, conflict("qualification_missing", "Kein Qualifikationsnachweis für den Widerruf vorhanden.")
				}
			}
		}
	default:
		return nil, adminInvalid("Unbekannte Personenaktion.")
	}
	identity, _ := json.Marshal(currentDeviceDetails(record))
	payload, _ := json.Marshal(struct {
		Input    usageInput `json:"input"`
		Identity string     `json:"identity_basis"`
	}{in, digest(string(identity))})
	event := newID()
	var subject any
	if in.UserID != "" {
		subject = in.UserID
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO usage_events VALUES(?,?,?,?,?,?,?,?)`, event, device, version+1, user, subject, in.Action, a.now().UTC().Format(time.RFC3339Nano), string(payload)); err != nil {
		return nil, err
	}
	response, _ := json.Marshal(struct {
		ID      string `json:"event_id"`
		Version int    `json:"version"`
	}{event, version + 1})
	if _, err = tx.ExecContext(ctx, `INSERT INTO usage_requests VALUES(?,?,?,?,?)`, user, key, hash, event, string(response)); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return response, nil
}
func usageRoutes(mux *http.ServeMux, a *authService) {
	mux.HandleFunc("GET /api/v1/devices/{id}/usage", func(w http.ResponseWriter, r *http.Request) {
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
		result, err := assessUsage(r.Context(), tx, actor.ID, subject, record, a.now())
		if err != nil {
			sendStockError(w, err)
			return
		}
		manager, err := usageManager(r.Context(), tx, actor.ID, record.ID)
		if err != nil {
			sendStockError(w, err)
			return
		}
		type person struct {
			ID     string `json:"id"`
			Name   string `json:"name"`
			Active bool   `json:"active"`
		}
		people := []person{}
		rows, err := tx.QueryContext(r.Context(), `SELECT DISTINCT u.id,u.username,u.active FROM users u JOIN role_assignments a ON a.user_id=u.id JOIN locations l ON l.site_id=a.site_id WHERE l.id=? AND (? OR u.id=?) ORDER BY u.username LIMIT 501`, record.LocationID, manager, actor.ID)
		if err != nil {
			sendStockError(w, err)
			return
		}
		for rows.Next() {
			var p person
			if err = rows.Scan(&p.ID, &p.Name, &p.Active); err != nil {
				rows.Close()
				sendStockError(w, err)
				return
			}
			people = append(people, p)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			sendStockError(w, err)
			return
		}
		if len(people) > 500 {
			apiError(w, 413, "usage_limit", "Mehr als 500 Konten im Betriebsstättenbereich.")
			return
		}
		if err = tx.Commit(); err != nil {
			sendStockError(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(struct {
			Assessment usageAssessment `json:"assessment"`
			People     []person        `json:"people"`
			Manager    bool            `json:"manager"`
		}{result, people, manager})
	})
	mux.HandleFunc("POST /api/v1/devices/{id}/usage", func(w http.ResponseWriter, r *http.Request) {
		actor, ok := writeAuthorized(a, w, r)
		if !ok {
			return
		}
		key := r.Header.Get("Idempotency-Key")
		if !validKey(key) {
			apiError(w, 400, "invalid_key", "Idempotenzschlüssel erforderlich.")
			return
		}
		var in usageInput
		d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10))
		d.DisallowUnknownFields()
		if d.Decode(&in) != nil || d.Decode(new(any)) != io.EOF {
			apiError(w, 400, "invalid_json", "Ungültige Personennachweise.")
			return
		}
		cookie, _ := r.Cookie(a.cookieName())
		body, err := mutateUsage(r.Context(), a, actor.ID, digest(cookie.Value), key, r.PathValue("id"), in)
		if err != nil {
			sendStockError(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(body)
	})
}

type usageSummary struct {
	UserID  string   `json:"user_id"`
	Version int      `json:"version"`
	Status  string   `json:"status"`
	Reasons []string `json:"reasons"`
}
