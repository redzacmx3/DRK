package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// ActiveModules returns the saved deployment selection, including changes made
// in the administrator interface. Manual jobs must use this live selection.
func ActiveModules(ctx context.Context, db *sql.DB) (ModuleConfig, int, error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return ModuleConfig{}, 0, err
	}
	defer tx.Rollback()
	return currentModules(ctx, tx)
}

// InitializeModules treats the environment as a startup default and an explicit
// override when its value changes. Repeated starts preserve web administration.
func InitializeModules(ctx context.Context, db *sql.DB, initial ModuleConfig) (ModuleConfig, int, error) {
	if err := initial.validate(); err != nil {
		return ModuleConfig{}, 0, err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return ModuleConfig{}, 0, err
	}
	defer tx.Rollback()
	live, revision, err := currentModules(ctx, tx)
	if err != nil {
		return ModuleConfig{}, 0, err
	}
	var previous string
	err = tx.QueryRowContext(ctx, `SELECT modules FROM module_startup_selection WHERE id=1`).Scan(&previous)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return ModuleConfig{}, 0, err
	}
	value := strings.Join(initial.names(), ",")
	if revision == 0 || previous != value {
		revision, err = configureModulesTx(ctx, tx, initial, "", "environment", "Modulauswahl beim Dienststart")
		if err != nil {
			return ModuleConfig{}, 0, err
		}
		live = initial
		if _, err = tx.ExecContext(ctx, `INSERT INTO module_startup_selection(id,modules) VALUES(1,?) ON CONFLICT(id) DO UPDATE SET modules=excluded.modules`, value); err != nil {
			return ModuleConfig{}, 0, err
		}
	}
	return live, revision, tx.Commit()
}

// configureModulesTx is also used by the initial setup transaction. Metadata
// and the immutable selection event are committed together, only on a change.
func configureModulesTx(ctx context.Context, tx *sql.Tx, c ModuleConfig, actor, source, note string) (int, error) {
	if err := c.validate(); err != nil {
		return 0, err
	}
	if source != "environment" && source != "admin" && source != "setup" {
		return 0, fmt.Errorf("invalid module configuration source")
	}
	note = strings.TrimSpace(note)
	if len(note) == 0 || len(note) > 1000 {
		return 0, fmt.Errorf("invalid module configuration note")
	}
	old, revision, err := currentModules(ctx, tx)
	if err != nil {
		return 0, err
	}
	if revision != 0 && old == c {
		return revision, nil
	}
	revision++
	if _, err := tx.ExecContext(ctx, `INSERT INTO module_configuration_events(version,modules,created_at) VALUES(?,?,?)`, revision, strings.Join(c.names(), ","), time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		return 0, err
	}
	var actorValue any
	if actor != "" {
		actorValue = actor
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO module_management_events(configuration_version,actor_id,source,note) VALUES(?,?,?,?)`, revision, actorValue, source, note); err != nil {
		return 0, err
	}
	return revision, nil
}

type moduleManagementInput struct {
	ExpectedRevision *int     `json:"expected_revision"`
	EnabledModules   []string `json:"enabled_modules"`
	Note             string   `json:"note"`
}

type moduleManagementEvent struct {
	Revision  int      `json:"revision"`
	Enabled   []string `json:"enabled_modules"`
	ActorID   *string  `json:"actor_id"`
	Source    string   `json:"source"`
	Note      string   `json:"note"`
	CreatedAt string   `json:"created_at"`
}

type moduleManagementState struct {
	Revision int                     `json:"revision"`
	Enabled  []string                `json:"enabled_modules"`
	Modules  []moduleDefinition      `json:"modules"`
	Editable bool                    `json:"editable"`
	History  []moduleManagementEvent `json:"history"`
	More     bool                    `json:"more_events"`
}

type moduleManagementResult struct {
	Revision int      `json:"revision"`
	Enabled  []string `json:"enabled_modules"`
	EventID  string   `json:"event_id"`
	Changed  bool     `json:"changed"`
}

// Web selections are explicit: inventory is mandatory, dependencies are never
// silently added, and each submitted entry must be a canonical module ID.
func modulesFromNames(names []string) (ModuleConfig, error) {
	if len(names) == 0 || len(names) > len(moduleDefinitions) {
		return ModuleConfig{}, adminInvalid("Inventarisierung muss aktiviert bleiben; gültige Funktionsgruppen auswählen.")
	}
	var c ModuleConfig
	seen := map[string]bool{}
	for _, id := range names {
		if seen[id] {
			return ModuleConfig{}, adminInvalid("Funktionsgruppen dürfen nicht doppelt ausgewählt sein.")
		}
		seen[id] = true
		found := false
		for i, definition := range moduleDefinitions {
			if id == definition.ID {
				c.bits |= 1 << i
				found = true
				break
			}
		}
		if !found {
			return ModuleConfig{}, adminInvalid("Unbekannte Funktionsgruppe: " + id)
		}
	}
	if err := c.validate(); err != nil {
		return ModuleConfig{}, adminInvalid("Ungültige Modulauswahl: " + err.Error())
	}
	return c, nil
}

func moduleManagementRoutes(mux *http.ServeMux, a *authService) {
	mux.HandleFunc("GET /api/v1/admin/modules", func(w http.ResponseWriter, r *http.Request) {
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
		if err = a.matchingModules(r.Context(), tx); err != nil {
			sendStockError(w, err)
			return
		}
		c, revision, err := currentModules(r.Context(), tx)
		if err != nil {
			sendStockError(w, err)
			return
		}
		state := moduleManagementState{Revision: revision, Enabled: c.names(), Editable: a.dynamicModules, History: []moduleManagementEvent{}, Modules: append([]moduleDefinition{}, moduleDefinitions...)}
		for i := range state.Modules {
			state.Modules[i].Enabled = c.Enabled(state.Modules[i].ID)
		}
		rows, err := tx.QueryContext(r.Context(), `SELECT c.version,c.modules,c.created_at,m.actor_id,COALESCE(m.source,'environment'),COALESCE(m.note,'Modulkonfiguration vor der Webverwaltung') FROM module_configuration_events c LEFT JOIN module_management_events m ON m.configuration_version=c.version ORDER BY c.version DESC LIMIT 101`)
		if err != nil {
			sendStockError(w, err)
			return
		}
		for rows.Next() {
			var event moduleManagementEvent
			var names string
			if err = rows.Scan(&event.Revision, &names, &event.CreatedAt, &event.ActorID, &event.Source, &event.Note); err != nil {
				rows.Close()
				sendStockError(w, err)
				return
			}
			event.Enabled = strings.Split(names, ",")
			state.History = append(state.History, event)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			sendStockError(w, err)
			return
		}
		if len(state.History) > 100 {
			state.More = true
			state.History = state.History[:100]
		}
		if err = tx.Commit(); err != nil {
			sendStockError(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(state)
	})
	mux.HandleFunc("POST /api/v1/admin/modules", func(w http.ResponseWriter, r *http.Request) {
		user, ok := writeAuthorized(a, w, r)
		if !ok {
			return
		}
		key := r.Header.Get("Idempotency-Key")
		if !validKey(key) {
			apiError(w, 400, "invalid_key", "Idempotenzschlüssel erforderlich.")
			return
		}
		var input moduleManagementInput
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&input) != nil || decoder.Decode(new(any)) != io.EOF {
			apiError(w, 400, "invalid_json", "Ungültige Modulkonfiguration.")
			return
		}
		cookie, _ := r.Cookie(a.cookieName())
		response, err := a.manageModules(r.Context(), user.ID, digest(cookie.Value), key, input)
		if err != nil {
			sendStockError(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(response)
	})
}

func (a *authService) manageModules(ctx context.Context, user, token, key string, input moduleManagementInput) ([]byte, error) {
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
	if !a.dynamicModules {
		return nil, &stockError{403, "module_administration_unavailable", "Modulverwaltung benötigt den regulären Dienst mit Webkonfiguration."}
	}
	if err = rejectOtherKey(ctx, tx, user, key, "module_management_requests"); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(input)
	if err != nil {
		return nil, err
	}
	hash := digest(string(raw))
	var previousHash, response string
	err = tx.QueryRowContext(ctx, `SELECT request_hash,response_json FROM module_management_requests WHERE user_id=? AND key=?`, user, key).Scan(&previousHash, &response)
	if err == nil {
		if hash != previousHash {
			return nil, conflict("idempotency_conflict", "Schlüssel gehört zu einer anderen Modulkonfiguration.")
		}
		return []byte(response), tx.Commit()
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if input.ExpectedRevision == nil || *input.ExpectedRevision < 0 {
		return nil, adminInvalid("Aktuelle Modulrevision erforderlich.")
	}
	input.Note = strings.TrimSpace(input.Note)
	if len(input.Note) < 3 || len(input.Note) > 1000 {
		return nil, adminInvalid("Begründung mit 3 bis 1000 Zeichen erforderlich.")
	}
	c, err := modulesFromNames(input.EnabledModules)
	if err != nil {
		return nil, err
	}
	old, revision, err := currentModules(ctx, tx)
	if err != nil {
		return nil, err
	}
	if revision != *input.ExpectedRevision {
		return nil, conflict("module_configuration_changed", "Die Modulkonfiguration wurde geändert. Neu laden und abgleichen.")
	}
	changed := revision == 0 || old != c
	newRevision, err := configureModulesTx(ctx, tx, c, user, "admin", input.Note)
	if err != nil {
		return nil, err
	}
	event, err := administrativeEvent(ctx, tx, user, "configure_modules", "module_configuration", struct {
		Before         []string `json:"before"`
		After          []string `json:"after"`
		BeforeRevision int      `json:"before_revision"`
		AfterRevision  int      `json:"after_revision"`
		Note           string   `json:"note"`
	}{old.names(), c.names(), revision, newRevision, input.Note})
	if err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(moduleManagementResult{newRevision, c.names(), event, changed})
	if err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO module_management_requests(user_id,key,request_hash,event_id,response_json) VALUES(?,?,?,?,?)`, user, key, hash, event, string(encoded)); err != nil {
		return nil, err
	}
	return encoded, tx.Commit()
}
