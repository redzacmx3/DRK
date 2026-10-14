package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type moduleDefinition struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	DependsOn []string `json:"depends_on"`
	Required  bool     `json:"required"`
	Enabled   bool     `json:"enabled"`
}

var moduleDefinitions = []moduleDefinition{
	{ID: "inventory", Name: "Inventarisierung", Required: true, DependsOn: []string{}},
	{ID: "devices", Name: "Geräteakten", DependsOn: []string{}},
	{ID: "documents", Name: "Geräteunterlagen und Gebrauchsanweisungen", DependsOn: []string{"devices"}},
	{ID: "compliance", Name: "Personen- und Produktnachweise", DependsOn: []string{"devices", "documents"}},
	{ID: "reprocessing", Name: "Aufbereitung und Hygiene", DependsOn: []string{"compliance"}},
	{ID: "packs", Name: "Packlisten", DependsOn: []string{}},
	{ID: "recalls", Name: "Rückrufe und Sicherheitsmaßnahmen", DependsOn: []string{}},
	{ID: "reminders", Name: "Erinnerungen und E-Mail", DependsOn: []string{"devices"}},
	{ID: "release", Name: "Betriebliche Einsatzfreigabe", DependsOn: []string{"packs", "devices", "documents", "compliance", "reprocessing", "recalls"}},
}

// ModuleConfig is an immutable, validated selection for a single deployment.
// Equipment registration, master data, imports and labels belong to inventory.
type ModuleConfig struct{ bits uint16 }

type moduleGuardContextKey struct{}

func allModules() ModuleConfig { return ModuleConfig{bits: (1 << len(moduleDefinitions)) - 1} }

func (c ModuleConfig) Enabled(id string) bool {
	for i, module := range moduleDefinitions {
		if module.ID == id {
			return c.bits&(1<<i) != 0
		}
	}
	return false
}

func (c ModuleConfig) names() []string {
	names := []string{}
	for _, module := range moduleDefinitions {
		if c.Enabled(module.ID) {
			names = append(names, module.ID)
		}
	}
	return names
}

func (c ModuleConfig) validate() error {
	if !c.Enabled("inventory") || c.bits&^allModules().bits != 0 {
		return fmt.Errorf("invalid module configuration; inventory must stay enabled")
	}
	for _, module := range moduleDefinitions {
		if !c.Enabled(module.ID) {
			continue
		}
		for _, dependency := range module.DependsOn {
			if !c.Enabled(dependency) {
				return fmt.Errorf("module %s requires %s", module.ID, dependency)
			}
		}
	}
	return nil
}

// ParseModules does not enable dependencies implicitly. Omitted configuration
// is handled by the caller; an explicit empty selection is an error.
func ParseModules(value string) (ModuleConfig, error) {
	if strings.TrimSpace(value) == "all" {
		return allModules(), nil
	}
	c := ModuleConfig{bits: 1}
	seen := map[string]bool{}
	for _, raw := range strings.Split(value, ",") {
		id := strings.TrimSpace(raw)
		if seen[id] {
			return ModuleConfig{}, fmt.Errorf("duplicate module %q", id)
		}
		seen[id] = true
		found := false
		for i, module := range moduleDefinitions {
			if module.ID == id {
				c.bits |= 1 << i
				found = true
				break
			}
		}
		if !found {
			return ModuleConfig{}, fmt.Errorf("unknown or empty module %q", id)
		}
	}
	return c, c.validate()
}

func currentModules(ctx context.Context, tx *sql.Tx) (ModuleConfig, int, error) {
	var value string
	var revision int
	err := tx.QueryRowContext(ctx, `SELECT modules,version FROM module_configuration_events ORDER BY version DESC LIMIT 1`).Scan(&value, &revision)
	if err == sql.ErrNoRows {
		return allModules(), 0, nil
	}
	if err != nil {
		return ModuleConfig{}, 0, err
	}
	c, err := ParseModules(value)
	return c, revision, err
}

// ConfigureModules records only actual changes; starting the same selection
// again does not invalidate evidence. Off/on transitions get distinct revisions.
func ConfigureModules(ctx context.Context, db *sql.DB, c ModuleConfig) (int, error) {
	if err := c.validate(); err != nil {
		return 0, err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	old, revision, err := currentModules(ctx, tx)
	if err != nil {
		return 0, err
	}
	if revision == 0 || old != c {
		revision++
		if _, err := tx.ExecContext(ctx, `INSERT INTO module_configuration_events(version,modules,created_at) VALUES(?,?,?)`, revision, strings.Join(c.names(), ","), time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
			return 0, err
		}
	}
	return revision, tx.Commit()
}

// Manual jobs must use the live selection, not silently change it via CLI.
func RequireModuleConfiguration(ctx context.Context, db *sql.DB, c ModuleConfig) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	return matchingModuleConfiguration(ctx, tx, c)
}

func matchingModuleConfiguration(ctx context.Context, tx *sql.Tx, c ModuleConfig) error {
	if err := c.validate(); err != nil {
		return err
	}
	live, _, err := currentModules(ctx, tx)
	if err != nil {
		return err
	}
	if live != c {
		return &stockError{503, "module_configuration_changed", "Modulkonfiguration geändert. Dienst mit der aktuellen Auswahl neu starten."}
	}
	return nil
}

func moduleDisabled(id string) error {
	return &stockError{403, "module_disabled", "Funktionsgruppe deaktiviert: " + id + ". Bestehende Nachweise bleiben erhalten."}
}

func requireModule(ctx context.Context, tx *sql.Tx, id string) error {
	c, _, err := currentModules(ctx, tx)
	if err != nil {
		return err
	}
	if !c.Enabled(id) {
		return moduleDisabled(id)
	}
	return nil
}

func (a *authService) activeModule(ctx context.Context, tx *sql.Tx, id string) error {
	if err := requireModule(ctx, tx, id); err != nil {
		return err
	}
	return matchingModuleConfiguration(ctx, tx, a.modules)
}

func maskOperatingEvidence(ctx context.Context, tx *sql.Tx, evidence *operatingEvidence) error {
	c, _, err := currentModules(ctx, tx)
	if err != nil {
		return err
	}
	dependencies := map[string][]string{
		"material": {"packs"}, "device_function": {"devices"}, "device_controls": {"devices"},
		"user_qualification": {"compliance"}, "manual_access": {"documents"},
		"end_user_acceptance": {"compliance"}, "product_duties": {"compliance", "reprocessing"},
	}
	for i := range evidence.Components {
		component := &evidence.Components[i]
		for _, module := range dependencies[component.Kind] {
			if !c.Enabled(module) && component.Status != "blocked" {
				component.Status = "unknown"
				component.Reason = "Funktionsgruppe deaktiviert: " + module + ". Keine aktive Erfüllungsbewertung."
				if evidence.Status != "blocked" {
					evidence.Status = "incomplete"
				}
				break
			}
		}
	}
	return nil
}

// Historical decisions remain readable, but inactive modules cannot contribute
// a positive current prerequisite. Existing negative results stay negative.
func maskModuleAssessment(ctx context.Context, tx *sql.Tx, id string, status *string, reasons *[]string) (bool, error) {
	c, _, err := currentModules(ctx, tx)
	if err != nil || c.Enabled(id) {
		return false, err
	}
	if *status != "blocked" {
		*status = "unknown"
	}
	*reasons = append(*reasons, "Funktionsgruppe deaktiviert: "+id+". Gespeicherte Nachweise sind keine aktive Einsatzvoraussetzung.")
	return true, nil
}

// Classify the ServeMux's registered pattern, never an unnormalized URL path.
// Reads and exports retain their original authorization and quarantine checks.
func mutationModule(pattern string) string {
	if !strings.HasPrefix(pattern, "POST /api/v1/") {
		return ""
	}
	path := strings.TrimPrefix(pattern, "POST /api/v1/")
	switch {
	case strings.HasPrefix(path, "devices/{id}/documents"), path == "devices/{id}/manual-access":
		return "documents"
	case path == "devices/{id}/usage", path == "devices/{id}/end-user-acceptance", path == "devices/{id}/product-duties", path == "devices/{id}/duty-fulfilment":
		return "compliance"
	case path == "devices/{id}/reprocessing":
		return "reprocessing"
	case strings.HasPrefix(path, "devices/{id}/") && path != "devices/{id}/details" && path != "devices/{id}/master-data":
		return "devices"
	case path == "packlists/{id}/release":
		return "release"
	case strings.HasPrefix(path, "packlists"):
		return "packs"
	case strings.HasPrefix(path, "recalls"):
		return "recalls"
	default:
		return "inventory"
	}
}

func (a *authService) moduleGuard(w http.ResponseWriter, r *http.Request, pattern string) bool {
	if !strings.HasPrefix(pattern, "POST /api/v1/") || strings.HasPrefix(pattern, "POST /api/v1/auth/") || pattern == "POST /api/v1/scanner/resolve" {
		return true
	}
	if _, err := a.session(r); err != nil {
		a.sessionError(w, err)
		return false
	}
	tx, err := a.db.BeginTx(r.Context(), nil)
	if err != nil {
		sendStockError(w, err)
		return false
	}
	defer tx.Rollback()
	err = matchingModuleConfiguration(r.Context(), tx, a.modules)
	if err == nil {
		err = requireModule(r.Context(), tx, mutationModule(pattern))
	}
	if err == nil && (pattern == "POST /api/v1/devices/{id}/instructions" || pattern == "POST /api/v1/packlists/{id}/device-checks") {
		id := "documents"
		if strings.Contains(pattern, "device-checks") {
			id = "devices"
		}
		err = requireModule(r.Context(), tx, id)
	}
	if err != nil {
		sendStockError(w, err)
		return false
	}
	// Recheck this trusted selection inside the actual write transaction too.
	*r = *r.WithContext(context.WithValue(r.Context(), moduleGuardContextKey{}, a.modules))
	return true
}

func moduleRoutes(mux *http.ServeMux, a *authService) {
	mux.HandleFunc("GET /api/v1/modules", func(w http.ResponseWriter, r *http.Request) {
		if _, err := a.session(r); err != nil {
			a.sessionError(w, err)
			return
		}
		tx, err := a.db.BeginTx(r.Context(), nil)
		if err != nil {
			sendStockError(w, err)
			return
		}
		defer tx.Rollback()
		if err := matchingModuleConfiguration(r.Context(), tx, a.modules); err != nil {
			sendStockError(w, err)
			return
		}
		c, revision, err := currentModules(r.Context(), tx)
		if err != nil {
			sendStockError(w, err)
			return
		}
		definitions := append([]moduleDefinition{}, moduleDefinitions...)
		for i := range definitions {
			definitions[i].Enabled = c.Enabled(definitions[i].ID)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(struct {
			Revision int                `json:"revision"`
			Enabled  []string           `json:"enabled_modules"`
			Modules  []moduleDefinition `json:"modules"`
		}{revision, c.names(), definitions})
	})
}
