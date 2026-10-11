package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"
)

type adminRight struct {
	SiteID string `json:"site_id"`
	Role   string `json:"role"`
}
type adminObject struct {
	ID            string       `json:"id"`
	Kind          string       `json:"kind"`
	Name          string       `json:"name"`
	ParentID      string       `json:"parent_id"`
	Version       int64        `json:"version"`
	Active        bool         `json:"active"`
	Administrator bool         `json:"administrator"`
	Rights        []adminRight `json:"rights"`
}
type adminChange struct {
	Action          string       `json:"action"`
	ID              string       `json:"id,omitempty"`
	ExpectedVersion *int64       `json:"expected_version,omitempty"`
	Name            string       `json:"name,omitempty"`
	ParentID        string       `json:"parent_id,omitempty"`
	Username        string       `json:"username,omitempty"`
	Password        string       `json:"password,omitempty"`
	Active          *bool        `json:"active,omitempty"`
	Rights          []adminRight `json:"rights"`
	Note            string       `json:"note"`
}
type adminResult struct {
	ID             string `json:"id"`
	Kind           string `json:"kind"`
	Version        int64  `json:"version"`
	EventID        string `json:"event_id"`
	SessionRevoked bool   `json:"session_revoked"`
}

func adminInvalid(message string) error { return &stockError{400, "invalid_administration", message} }
func requireAdministrator(ctx context.Context, tx *sql.Tx, id string) error {
	var ok bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM administrators a JOIN users u ON u.id=a.user_id WHERE a.user_id=? AND u.active=1)`, id).Scan(&ok); err != nil {
		return err
	}
	if !ok {
		return &stockError{403, "administrator_required", "Administratorrecht erforderlich."}
	}
	return nil
}
func protectAdministrator(ctx context.Context, tx *sql.Tx, id string, active bool, actor string) error {
	if active {
		return nil
	}
	if actor != "" && actor == id {
		return conflict("self_disable", "Das eigene Konto kann hier nicht deaktiviert werden.")
	}
	var isAdmin bool
	var count int
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM administrators a JOIN users u ON u.id=a.user_id WHERE a.user_id=? AND u.active=1)`, id).Scan(&isAdmin); err != nil {
		return err
	}
	if !isAdmin {
		return nil
	}
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM administrators a JOIN users u ON u.id=a.user_id WHERE u.active=1`).Scan(&count); err != nil {
		return err
	}
	if count <= 1 {
		return conflict("last_administrator", "Der letzte aktive Administrator muss erhalten bleiben.")
	}
	return nil
}
func adminObjectByID(ctx context.Context, tx *sql.Tx, kind, id string) (adminObject, error) {
	o := adminObject{Kind: kind, Rights: []adminRight{}}
	var query string
	switch kind {
	case "operator":
		query = `SELECT id,name,'',1,0,v.version FROM operators o JOIN administrative_versions v ON v.kind='operator' AND v.object_id=o.id WHERE id=?`
	case "site":
		query = `SELECT id,name,operator_id,1,0,v.version FROM sites o JOIN administrative_versions v ON v.kind='site' AND v.object_id=o.id WHERE id=?`
	case "location":
		query = `SELECT id,name,site_id,1,0,v.version FROM locations o JOIN administrative_versions v ON v.kind='location' AND v.object_id=o.id WHERE id=?`
	case "user":
		query = `SELECT id,username,'',active,EXISTS(SELECT 1 FROM administrators WHERE user_id=o.id),v.version FROM users o JOIN administrative_versions v ON v.kind='user' AND v.object_id=o.id WHERE id=?`
	default:
		return o, adminInvalid("Unbekannter Objekttyp.")
	}
	if err := tx.QueryRowContext(ctx, query, id).Scan(&o.ID, &o.Name, &o.ParentID, &o.Active, &o.Administrator, &o.Version); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return o, &stockError{404, "not_found", "Verwaltungsobjekt nicht gefunden."}
		}
		return o, err
	}
	if kind == "user" {
		rows, err := tx.QueryContext(ctx, `SELECT site_id,role FROM role_assignments WHERE user_id=? ORDER BY site_id,role`, id)
		if err != nil {
			return o, err
		}
		defer rows.Close()
		for rows.Next() {
			var right adminRight
			if err = rows.Scan(&right.SiteID, &right.Role); err != nil {
				return o, err
			}
			o.Rights = append(o.Rights, right)
		}
		if err = rows.Err(); err != nil {
			return o, err
		}
	}
	return o, nil
}
func administrativeEvent(ctx context.Context, tx *sql.Tx, actor any, action, id string, payload any) (string, error) {
	event := newID()
	body, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO administrative_events VALUES(?,?,?,?,?,?)`, event, actor, action, id, time.Now().UTC().Format(time.RFC3339Nano), string(body))
	return event, err
}

// Local CLI is the only way to grant administrative privileges. It grants no site roles.
func CreateAdministrator(db *sql.DB, username, password string) error {
	if err := Ready(context.Background(), db); err != nil {
		return err
	}
	username = strings.TrimSpace(username)
	if len(username) < 3 || len(username) > 100 || len(password) < 12 || len(password) > 256 {
		return fmt.Errorf("username must be 3..100 and password 12..256 bytes")
	}
	hash := passwordHash(password)
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	id := newID()
	if _, err = tx.Exec(`INSERT INTO users(id,username,password_hash,active) VALUES(?,?,?,1)`, id, username, hash); err != nil {
		return err
	}
	if _, err = tx.Exec(`INSERT INTO administrators VALUES(?)`, id); err != nil {
		return err
	}
	if _, err = administrativeEvent(context.Background(), tx, nil, "local_create_administrator", id, map[string]string{"username": username}); err != nil {
		return err
	}
	return tx.Commit()
}

// Explicit local recovery permits revoking the final administrator; it can be regranted locally.
func SetAdministrator(db *sql.DB, username string, grant bool) error {
	if err := Ready(context.Background(), db); err != nil {
		return err
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var id string
	var active bool
	if err = tx.QueryRow(`SELECT id,active FROM users WHERE username=?`, username).Scan(&id, &active); err != nil {
		return err
	}
	action := "local_revoke_administrator"
	if grant {
		if !active {
			return fmt.Errorf("active user required")
		}
		action = "local_grant_administrator"
		_, err = tx.Exec(`INSERT INTO administrators VALUES(?) ON CONFLICT DO NOTHING`, id)
	} else {
		_, err = tx.Exec(`DELETE FROM administrators WHERE user_id=?`, id)
	}
	if err != nil {
		return err
	}
	if _, err = tx.Exec(`DELETE FROM sessions WHERE user_id=?`, id); err != nil {
		return err
	}
	if _, err = administrativeEvent(context.Background(), tx, nil, action, id, map[string]string{"username": username}); err != nil {
		return err
	}
	return tx.Commit()
}
func administrationRoutes(mux *http.ServeMux, a *authService) {
	mux.HandleFunc("GET /api/v1/admin", func(w http.ResponseWriter, r *http.Request) {
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
		if err = requireAdministrator(r.Context(), tx, user.ID); err != nil {
			sendStockError(w, err)
			return
		}
		objects := []adminObject{}
		for _, kind := range []string{"operator", "site", "location", "user"} {
			rows, err := tx.QueryContext(r.Context(), `SELECT object_id FROM administrative_versions WHERE kind=? ORDER BY object_id LIMIT 1001`, kind)
			if err != nil {
				sendStockError(w, err)
				return
			}
			ids := []string{}
			for rows.Next() {
				var id string
				if err = rows.Scan(&id); err != nil {
					rows.Close()
					sendStockError(w, err)
					return
				}
				ids = append(ids, id)
			}
			err = rows.Err()
			rows.Close()
			if err != nil {
				sendStockError(w, err)
				return
			}
			if len(ids) > 1000 {
				apiError(w, 413, "administration_limit", "Mehr als 1000 Objekte je Typ; lokale Verwaltung erforderlich.")
				return
			}
			for _, id := range ids {
				o, err := adminObjectByID(r.Context(), tx, kind, id)
				if err != nil {
					sendStockError(w, err)
					return
				}
				objects = append(objects, o)
			}
		}
		type audit struct {
			ID        string          `json:"id"`
			ActorID   *string         `json:"actor_id"`
			Action    string          `json:"action"`
			ObjectID  string          `json:"object_id"`
			CreatedAt string          `json:"created_at"`
			Payload   json.RawMessage `json:"payload"`
		}
		events := []audit{}
		rows, err := tx.QueryContext(r.Context(), `SELECT id,actor_id,action,object_id,created_at,payload_json FROM administrative_events ORDER BY created_at DESC,id DESC LIMIT 201`)
		if err != nil {
			sendStockError(w, err)
			return
		}
		for rows.Next() {
			var e audit
			var payload string
			if err = rows.Scan(&e.ID, &e.ActorID, &e.Action, &e.ObjectID, &e.CreatedAt, &payload); err != nil {
				rows.Close()
				sendStockError(w, err)
				return
			}
			e.Payload = json.RawMessage(payload)
			events = append(events, e)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			sendStockError(w, err)
			return
		}
		more := len(events) > 200
		if more {
			events = events[:200]
		}
		if err = tx.Commit(); err != nil {
			sendStockError(w, err)
			return
		}
		json.NewEncoder(w).Encode(struct {
			Objects []adminObject `json:"objects"`
			Events  []audit       `json:"events"`
			More    bool          `json:"more_events"`
		}{objects, events, more})
	})
	mux.HandleFunc("POST /api/v1/admin/changes", func(w http.ResponseWriter, r *http.Request) {
		user, ok := writeAuthorized(a, w, r)
		if !ok {
			return
		}
		key := r.Header.Get("Idempotency-Key")
		if !validKey(key) {
			apiError(w, 400, "invalid_key", "Idempotenzschlüssel erforderlich.")
			return
		}
		var change adminChange
		d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 32<<10))
		d.DisallowUnknownFields()
		if d.Decode(&change) != nil || d.Decode(new(any)) != io.EOF {
			apiError(w, 400, "invalid_json", "Ungültige Verwaltungsdaten.")
			return
		}
		cookie, _ := r.Cookie(a.cookieName())
		response, err := applyAdminChange(r.Context(), a, user.ID, digest(cookie.Value), key, change)
		if err != nil {
			sendStockError(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(response)
	})
}
func applyAdminChange(ctx context.Context, a *authService, user, token, key string, c adminChange) ([]byte, error) {
	tx, err := a.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err = activeStockSession(ctx, tx, user, token, a.now()); err != nil {
		return nil, err
	}
	if err = requireAdministrator(ctx, tx, user); err != nil {
		return nil, err
	}
	raw, _ := json.Marshal(c)
	hash := digest(string(raw))
	if err = rejectOtherKey(ctx, tx, user, key, "administrative_requests"); err != nil {
		return nil, err
	}
	var priorHash, prior string
	err = tx.QueryRowContext(ctx, `SELECT request_hash,response_json FROM administrative_requests WHERE user_id=? AND key=?`, user, key).Scan(&priorHash, &prior)
	if err == nil {
		if hash != priorHash {
			return nil, conflict("idempotency_conflict", "Schlüssel gehört zu anderen Verwaltungsdaten.")
		}
		return []byte(prior), tx.Commit()
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	c.Note = strings.TrimSpace(c.Note)
	if len(c.Note) < 3 || len(c.Note) > 1000 {
		return nil, adminInvalid("Begründung mit 3 bis 1000 Zeichen erforderlich.")
	}
	create := strings.HasPrefix(c.Action, "create_")
	kind := strings.TrimPrefix(strings.TrimPrefix(c.Action, "create_"), "update_")
	if c.Action == "reset_password" {
		kind = "user"
	}
	switch c.Action {
	case "create_operator", "update_operator", "create_site", "update_site", "create_location", "update_location", "create_user", "update_user", "reset_password":
	default:
		return nil, adminInvalid("Unbekannte Verwaltungsaktion.")
	}
	if create {
		if c.ID != "" || c.ExpectedVersion != nil {
			return nil, adminInvalid("Neue Objekte dürfen keine ID oder Version vorgeben.")
		}
		c.ID = newID()
	} else {
		if c.ID == "" || c.ExpectedVersion == nil || *c.ExpectedVersion < 0 {
			return nil, adminInvalid("ID und aktuelle Version erforderlich.")
		}
	}
	var before *adminObject
	if !create {
		o, e := adminObjectByID(ctx, tx, kind, c.ID)
		if e != nil {
			return nil, e
		}
		if o.Version != *c.ExpectedVersion {
			return nil, conflict("administration_changed", "Das Objekt wurde geändert. Neu laden und abgleichen.")
		}
		before = &o
	}
	if kind != "user" {
		if c.Username != "" || c.Password != "" || c.Active != nil || c.Rights != nil {
			return nil, adminInvalid("Benutzerfelder sind bei Organisationen unzulässig.")
		}
		c.Name = strings.TrimSpace(c.Name)
		if len(c.Name) < 1 || len(c.Name) > 200 {
			return nil, adminInvalid("Name mit 1 bis 200 Zeichen erforderlich.")
		}
		if !create && c.ParentID != "" {
			return nil, adminInvalid("Zuordnungen bestehender Objekte bleiben unverändert.")
		}
		if kind == "operator" && c.ParentID != "" {
			return nil, adminInvalid("Betreiber haben keine übergeordnete Zuordnung.")
		}
		if create && kind != "operator" {
			parentKind := "operator"
			if kind == "location" {
				parentKind = "site"
			}
			if _, err = adminObjectByID(ctx, tx, parentKind, c.ParentID); err != nil {
				return nil, err
			}
		}
		table := map[string]string{"operator": "operators", "site": "sites", "location": "locations"}[kind]
		if create {
			switch kind {
			case "operator":
				_, err = tx.ExecContext(ctx, `INSERT INTO operators VALUES(?,?)`, c.ID, c.Name)
			case "site":
				_, err = tx.ExecContext(ctx, `INSERT INTO sites VALUES(?,?,?)`, c.ID, c.ParentID, c.Name)
			case "location":
				_, err = tx.ExecContext(ctx, `INSERT INTO locations VALUES(?,?,?)`, c.ID, c.ParentID, c.Name)
			}
		} else {
			_, err = tx.ExecContext(ctx, `UPDATE `+table+` SET name=? WHERE id=?`, c.Name, c.ID)
		}
		if err != nil {
			return nil, err
		}
	} else {
		if c.Name != "" || c.ParentID != "" {
			return nil, adminInvalid("Organisationsfelder sind bei Benutzern unzulässig.")
		}
		if c.Action != "create_user" && c.Username != "" {
			return nil, adminInvalid("Benutzername bleibt unverändert.")
		}
		if c.Action == "update_user" && c.Password != "" {
			return nil, adminInvalid("Passwort separat zurücksetzen.")
		}
		if c.Action == "reset_password" && (c.Rights != nil || c.Active != nil) {
			return nil, adminInvalid("Passwortänderung verändert keine Rechte.")
		}
		if c.Action != "reset_password" {
			if c.Active == nil || c.Rights == nil || len(c.Rights) > 100 {
				return nil, adminInvalid("Aktivstatus und ausdrückliche Rechte erforderlich (höchstens 100).")
			}
			if create && !*c.Active {
				return nil, adminInvalid("Neue Konten werden aktiv angelegt.")
			}
			seen := map[string]bool{}
			for _, right := range c.Rights {
				if !validRole(right.Role) || seen[right.SiteID+":"+right.Role] {
					return nil, adminInvalid("Ungültige oder doppelte Rechtezuordnung.")
				}
				seen[right.SiteID+":"+right.Role] = true
				if _, err = adminObjectByID(ctx, tx, "site", right.SiteID); err != nil {
					return nil, err
				}
			}
			sort.Slice(c.Rights, func(i, j int) bool {
				if c.Rights[i].SiteID == c.Rights[j].SiteID {
					return c.Rights[i].Role < c.Rights[j].Role
				}
				return c.Rights[i].SiteID < c.Rights[j].SiteID
			})
			if !create {
				if err = protectAdministrator(ctx, tx, c.ID, *c.Active, user); err != nil {
					return nil, err
				}
			}
		}
		if create || c.Action == "reset_password" {
			if len(c.Password) < 12 || len(c.Password) > 256 {
				return nil, adminInvalid("Passwort mit 12 bis 256 Bytes erforderlich.")
			}
			if create {
				c.Username = strings.TrimSpace(c.Username)
				if len(c.Username) < 3 || len(c.Username) > 100 {
					return nil, adminInvalid("Benutzername mit 3 bis 100 Zeichen erforderlich.")
				}
				var exists bool
				if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE username=?)`, c.Username).Scan(&exists); err != nil {
					return nil, err
				}
				if exists {
					return nil, conflict("username_exists", "Benutzername ist bereits vergeben.")
				}
			}
			password := passwordHash(c.Password)
			if create {
				_, err = tx.ExecContext(ctx, `INSERT INTO users(id,username,password_hash,active) VALUES(?,?,?,1)`, c.ID, c.Username, password)
			} else {
				_, err = tx.ExecContext(ctx, `UPDATE users SET password_hash=? WHERE id=?`, password, c.ID)
			}
			if err != nil {
				return nil, err
			}
		}
		if c.Action == "update_user" {
			if _, err = tx.ExecContext(ctx, `UPDATE users SET active=? WHERE id=?`, *c.Active, c.ID); err != nil {
				return nil, err
			}
			if _, err = tx.ExecContext(ctx, `DELETE FROM role_assignments WHERE user_id=?`, c.ID); err != nil {
				return nil, err
			}
		}
		if c.Action != "reset_password" {
			for _, right := range c.Rights {
				if _, err = tx.ExecContext(ctx, `INSERT INTO role_assignments VALUES(?,?,?)`, c.ID, right.SiteID, right.Role); err != nil {
					return nil, err
				}
			}
		}
		if !create {
			if _, err = tx.ExecContext(ctx, `DELETE FROM sessions WHERE user_id=?`, c.ID); err != nil {
				return nil, err
			}
		}
	}
	after, err := adminObjectByID(ctx, tx, kind, c.ID)
	if err != nil {
		return nil, err
	}
	event, err := administrativeEvent(ctx, tx, user, c.Action, c.ID, struct {
		Before *adminObject `json:"before"`
		After  adminObject  `json:"after"`
		Note   string       `json:"note"`
	}{before, after, c.Note})
	if err != nil {
		return nil, err
	}
	response, err := json.Marshal(adminResult{c.ID, kind, after.Version, event, kind == "user" && !create && c.ID == user})
	if err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO administrative_requests VALUES(?,?,?,?,?)`, user, key, hash, event, string(response)); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return response, nil
}
