package app

import (
	"context"
	"crypto/subtle"
	"crypto/tls"
	"crypto/x509"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// RuntimeOptions describes actual deployment capabilities; it never contains
// SMTP passwords or the plaintext bootstrap code.
type RuntimeOptions struct {
	Modules          ModuleConfig
	DataDir          string
	TLSCert          string
	TLSKey           string
	SMTPConfigured   bool
	RemindersEnabled bool
	SetupCodeFile    string
}

type setupState struct {
	Hash      string
	Completed sql.NullString
}

func readSetupState(ctx context.Context, tx *sql.Tx) (setupState, error) {
	var state setupState
	err := tx.QueryRowContext(ctx, `SELECT token_hash,completed_at FROM web_setup_state WHERE id=1`).Scan(&state.Hash, &state.Completed)
	return state, err
}

// A missing administrator does not establish first use. Existing accounts,
// organization, inventory or audit history require local recovery instead.
func setupDatabaseEmpty(ctx context.Context, tx *sql.Tx) (bool, error) {
	var empty bool
	err := tx.QueryRowContext(ctx, `SELECT NOT(EXISTS(SELECT 1 FROM users) OR EXISTS(SELECT 1 FROM operators) OR EXISTS(SELECT 1 FROM sites) OR EXISTS(SELECT 1 FROM locations) OR EXISTS(SELECT 1 FROM devices) OR EXISTS(SELECT 1 FROM product_types) OR EXISTS(SELECT 1 FROM stock_movements) OR EXISTS(SELECT 1 FROM packlist_versions) OR EXISTS(SELECT 1 FROM administrative_events) OR EXISTS(SELECT 1 FROM administrative_versions) OR EXISTS(SELECT 1 FROM recalls) OR EXISTS(SELECT 1 FROM device_intake) OR EXISTS(SELECT 1 FROM stock_intake))`).Scan(&empty)
	return empty, err
}

func validSetupCode(code string) bool {
	bytes, err := base64.RawURLEncoding.DecodeString(code)
	return err == nil && len(code) == 43 && len(bytes) == 32
}

func readSetupCodeFile(path string) (string, error) {
	before, err := os.Lstat(path)
	if err != nil {
		return "", err
	}
	if !before.Mode().IsRegular() || before.Mode().Perm()&0077 != 0 || before.Size() > 128 {
		return "", fmt.Errorf("setup code must be a private regular file")
	}
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	after, err := file.Stat()
	if err != nil || !os.SameFile(before, after) {
		return "", fmt.Errorf("setup code file changed while opening")
	}
	bytes := make([]byte, 129)
	n, err := file.Read(bytes)
	if err != nil || n > 128 {
		return "", fmt.Errorf("setup code file is not readable")
	}
	code := strings.TrimSpace(string(bytes[:n]))
	if !validSetupCode(code) {
		return "", fmt.Errorf("setup code file has an invalid format")
	}
	return code, nil
}

func createOrReadSetupCode(path string) (string, error) {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if os.IsExist(err) {
		return readSetupCodeFile(path)
	}
	if err != nil {
		return "", err
	}
	code := randomToken()
	_, err = file.WriteString(code + "\n")
	if err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err != nil {
		return "", err
	}
	return code, closeErr
}

// PrepareWebSetup creates a one-time code automatically on genuine first use.
// It never prints the code and never overwrites an existing file. Keeping the
// file before committing the hash permits recovery after an interrupted start.
func PrepareWebSetup(ctx context.Context, db *sql.DB, codeFile string) (bool, error) {
	if codeFile == "" {
		return false, fmt.Errorf("setup code file must be configured")
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	state, err := readSetupState(ctx, tx)
	if err != nil && err != sql.ErrNoRows {
		return false, err
	}
	exists := err == nil
	if exists && state.Completed.Valid {
		return false, nil
	}
	empty, err := setupDatabaseEmpty(ctx, tx)
	if err != nil {
		return false, err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if !empty {
		if exists {
			_, err = tx.ExecContext(ctx, `UPDATE web_setup_state SET token_hash='',completed_at=?,completion_reason='existing_installation' WHERE id=1 AND completed_at IS NULL`, now)
		} else {
			_, err = tx.ExecContext(ctx, `INSERT INTO web_setup_state(id,token_hash,created_at,completed_at,completion_reason) VALUES(1,'',?,?,'existing_installation')`, now, now)
		}
		if err != nil {
			return false, err
		}
		return false, tx.Commit()
	}
	code, err := createOrReadSetupCode(codeFile)
	if err != nil {
		return false, fmt.Errorf("prepare private setup code file: %w", err)
	}
	if exists {
		if subtle.ConstantTimeCompare([]byte(digest(code)), []byte(state.Hash)) != 1 {
			return false, fmt.Errorf("setup code file does not match the pending installation")
		}
	} else if _, err = tx.ExecContext(ctx, `INSERT INTO web_setup_state(id,token_hash,created_at) VALUES(1,?,?)`, digest(code), now); err != nil {
		return false, err
	}
	return true, tx.Commit()
}

// Once any existing installation is observed, the bootstrap stays closed even
// if its last administrator is later removed through local recovery.
func WebSetupPending(ctx context.Context, db *sql.DB) (bool, error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	state, err := readSetupState(ctx, tx)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if state.Completed.Valid {
		return false, nil
	}
	empty, err := setupDatabaseEmpty(ctx, tx)
	if err != nil || empty {
		return empty, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE web_setup_state SET token_hash='',completed_at=?,completion_reason='existing_installation' WHERE id=1 AND completed_at IS NULL`, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		return false, err
	}
	return false, tx.Commit()
}

func setupClosed() error {
	return &stockError{409, "setup_closed", "Die Ersteinrichtung ist geschlossen. Bitte anmelden; Kontowiederherstellung erfolgt lokal."}
}

func authorizeSetup(ctx context.Context, tx *sql.Tx, code string) error {
	state, err := readSetupState(ctx, tx)
	if err == sql.ErrNoRows {
		return setupClosed()
	}
	if err != nil {
		return err
	}
	if state.Completed.Valid {
		return setupClosed()
	}
	empty, err := setupDatabaseEmpty(ctx, tx)
	if err != nil {
		return err
	}
	if !empty {
		return setupClosed()
	}
	if !validSetupCode(code) || subtle.ConstantTimeCompare([]byte(digest(code)), []byte(state.Hash)) != 1 {
		return &stockError{403, "invalid_setup_code", "Einrichtungscode ungültig."}
	}
	return nil
}

func (a *authService) checkSetupCode(ctx context.Context, code string) error {
	tx, err := a.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	return authorizeSetup(ctx, tx, code)
}

func (a *authService) setupCookieName() string {
	if a.localHTTP {
		return "drk-local-setup"
	}
	return "__Host-drk-setup"
}

func (a *authService) setupRequest(w http.ResponseWriter, r *http.Request) bool {
	if r.TLS == nil && !a.localHTTP {
		apiError(w, 400, "https_required", "Ersteinrichtung benötigt die konfigurierte HTTPS-Verbindung.")
		return false
	}
	if !a.sameOrigin(w, r) {
		return false
	}
	cookie, err := r.Cookie(a.setupCookieName())
	expected := ""
	if err == nil && validSetupCode(cookie.Value) {
		expected = digest("setup-csrf:" + cookie.Value)
	}
	if expected == "" || subtle.ConstantTimeCompare([]byte(expected), []byte(r.Header.Get("X-CSRF-Token"))) != 1 {
		apiError(w, 403, "csrf_failed", "Einrichtungssitzung fehlt oder wurde geändert. Einrichtung neu laden.")
		return false
	}
	if !a.allowLogin(r) {
		w.Header().Set("Retry-After", "60")
		apiError(w, 429, "rate_limited", "Zu viele Versuche. Bitte später erneut prüfen.")
		return false
	}
	return true
}

type setupCheck struct {
	ID       string `json:"id"`
	Label    string `json:"label"`
	Status   string `json:"status"`
	Required bool   `json:"required"`
	Message  string `json:"message"`
}

func (a *authService) setupEnvironmentChecks(ctx context.Context, modules ModuleConfig) ([]setupCheck, bool) {
	checks := []setupCheck{}
	complete := true
	add := func(id, label, status string, required bool, message string) {
		checks = append(checks, setupCheck{id, label, status, required, message})
		if required && status != "ok" {
			complete = false
		}
	}
	var integrity string
	dbOK := Ready(ctx, a.db) == nil && a.db.QueryRowContext(ctx, `PRAGMA integrity_check`).Scan(&integrity) == nil && integrity == "ok"
	if dbOK {
		rows, err := a.db.QueryContext(ctx, `PRAGMA foreign_key_check`)
		if err != nil {
			dbOK = false
		} else {
			dbOK = !rows.Next() && rows.Err() == nil
			rows.Close()
		}
	}
	if dbOK {
		add("database", "Datenbank", "ok", true, "Schema, Integrität und Verknüpfungen geprüft.")
	} else {
		add("database", "Datenbank", "error", true, "Datenbankprüfung fehlgeschlagen; Startkonfiguration oder Migration prüfen.")
	}
	writeOK := false
	if a.runtime.DataDir != "" {
		file, err := os.CreateTemp(a.runtime.DataDir, ".drk-setup-probe-")
		if err == nil {
			_, writeErr := file.WriteString("setup write check")
			syncErr := file.Sync()
			closeErr := file.Close()
			removeErr := os.Remove(file.Name())
			writeOK = writeErr == nil && syncErr == nil && closeErr == nil && removeErr == nil
		}
	}
	if writeOK {
		add("storage", "Datenvolume", "ok", true, "Schreiben und Entfernen einer temporären Prüfdatei erfolgreich.")
	} else {
		add("storage", "Datenvolume", "error", true, "Datenverzeichnis nicht schreibbar; Volumeeinbindung und Berechtigungen prüfen.")
	}
	u, err := url.Parse(a.origin)
	scheme := "https"
	if a.localHTTP {
		scheme = "http"
	}
	if err == nil && u.Scheme == scheme && u.Host != "" && u.User == nil && u.Path == "" && u.RawQuery == "" && u.Fragment == "" {
		add("origin", "Anwendungsadresse", "ok", true, "Browseradresse stimmt mit der konfigurierten Verbindung überein.")
	} else {
		add("origin", "Anwendungsadresse", "error", true, "Anwendungsadresse und HTTP-/HTTPS-Modus passen nicht zusammen.")
	}
	if a.localHTTP {
		add("transport", "Lokaler HTTP-Zugriff", "warning", false, "HTTP ist ausdrücklich aktiviert. Für externe Verbindungen HTTPS in Compose konfigurieren.")
	} else if a.runtime.TLSCert == "" && a.runtime.TLSKey == "" {
		add("transport", "HTTPS", "warning", false, "HTTPS-Modus aktiv; Zertifikatsdateien sind für diese Prüfung nicht hinterlegt.")
	} else {
		pair, pairErr := tls.LoadX509KeyPair(a.runtime.TLSCert, a.runtime.TLSKey)
		var cert *x509.Certificate
		if pairErr == nil && len(pair.Certificate) > 0 {
			cert, err = x509.ParseCertificate(pair.Certificate[0])
		}
		now := a.now()
		if pairErr != nil || err != nil || cert == nil || now.Before(cert.NotBefore) || !now.Before(cert.NotAfter) {
			add("transport", "HTTPS-Zertifikat", "error", true, "Zertifikat, Schlüssel oder Gültigkeit nicht bestätigt.")
		} else {
			add("transport", "HTTPS-Zertifikat", "ok", true, "Zertifikat und Schlüssel lesbar und aktuell gültig. Browservertrauen wird vom Browser geprüft.")
			if u == nil || cert.VerifyHostname(u.Hostname()) != nil {
				add("public_certificate", "Öffentliche HTTPS-Adresse", "warning", false, "Backend-Zertifikat deckt die Browseradresse nicht ab. Bei vorgeschaltetem HTTPS-Proxy dessen externe Zertifikatsprüfung kontrollieren.")
			}
		}
	}
	if modules.Enabled("documents") {
		if a.clamdAddress == "" {
			add("scanner", "Dokumentprüfung", "warning", false, "ClamAV nicht konfiguriert. Anhänge bleiben bis zur erforderlichen Prüfung in Quarantäne.")
		} else {
			connection, err := (&net.Dialer{Timeout: time.Second}).DialContext(ctx, "tcp", a.clamdAddress)
			if err != nil {
				add("scanner", "Dokumentprüfung", "warning", false, "Konfigurierter ClamAV-Anschluss nicht erreichbar. Freigaben bleiben gesperrt.")
			} else {
				connection.Close()
				add("scanner", "Dokumentprüfung", "warning", false, "ClamAV-Anschluss erreichbar; aktuelle Signaturen und echte Scanabnahme noch gesondert prüfen.")
			}
		}
	}
	if modules.Enabled("reminders") {
		if !a.runtime.RemindersEnabled {
			add("reminders", "Kontrollerinnerungen", "warning", false, "Automatischer Erinnerungsjob ist in der Startkonfiguration ausgeschaltet.")
		} else {
			add("reminders", "Kontrollerinnerungen", "ok", false, "Lokaler Erinnerungsjob aktiviert; keine Bestätigung einer Zustellung.")
		}
		message := "SMTP nicht konfiguriert. Erinnerungen bleiben in der Anwendung verfügbar."
		if a.runtime.SMTPConfigured {
			message = "SMTP-Konfiguration geladen. Empfängerzuordnung und echte Postfachzustellung nach der Einrichtung prüfen."
		}
		add("smtp", "E-Mail", "warning", false, message)
	}
	return checks, complete
}

type setupCheckInput struct {
	Code    string   `json:"setup_code"`
	Modules []string `json:"enabled_modules"`
}

type setupCompleteInput struct {
	Code             string   `json:"setup_code"`
	ExpectedRevision *int     `json:"expected_revision"`
	Username         string   `json:"username"`
	Password         string   `json:"password"`
	Operator         string   `json:"operator_name"`
	Site             string   `json:"site_name"`
	Location         string   `json:"location_name"`
	Modules          []string `json:"enabled_modules"`
	MaterialManager  *bool    `json:"material_manager"`
	DeviceManager    *bool    `json:"device_manager"`
}

func (a *authService) completeWebSetup(ctx context.Context, in setupCompleteInput) error {
	if in.ExpectedRevision == nil || *in.ExpectedRevision < 0 || in.Username != strings.TrimSpace(in.Username) || len(in.Username) < 3 || len(in.Username) > 100 || len(in.Password) < 12 || len(in.Password) > 256 || in.MaterialManager == nil || in.DeviceManager == nil {
		return &stockError{400, "invalid_setup", "Benutzername, Passwort mit 12–256 Bytes, aktueller Prüfstand und ausdrückliche Fachrechte erforderlich."}
	}
	for _, name := range []string{in.Operator, in.Site, in.Location} {
		if strings.TrimSpace(name) == "" || len(name) > 200 {
			return &stockError{400, "invalid_setup", "Betreiber, Betriebsstätte und Lagerort mit jeweils 1–200 Bytes erforderlich."}
		}
	}
	modules, err := modulesFromNames(in.Modules)
	if err != nil {
		return err
	}
	if err = a.checkSetupCode(ctx, in.Code); err != nil {
		return err
	}
	if _, complete := a.setupEnvironmentChecks(ctx, modules); !complete {
		return &stockError{409, "setup_checks_failed", "Erforderliche Umgebungsprüfung fehlgeschlagen. Bitte erneut prüfen."}
	}
	hash := passwordHash(in.Password)
	tx, err := a.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = authorizeSetup(ctx, tx, in.Code); err != nil {
		return err
	}
	_, revision, err := currentModules(ctx, tx)
	if err != nil {
		return err
	}
	if revision != *in.ExpectedRevision {
		return conflict("module_revision_conflict", "Einrichtungsvorgabe geändert. Auswahl neu laden und prüfen.")
	}
	admin, operator, site, location := newID(), newID(), newID(), newID()
	for _, insert := range []struct {
		SQL  string
		Args []any
	}{
		{`INSERT INTO users(id,username,password_hash,active) VALUES(?,?,?,1)`, []any{admin, in.Username, hash}},
		{`INSERT INTO administrators VALUES(?)`, []any{admin}},
		{`INSERT INTO operators VALUES(?,?)`, []any{operator, strings.TrimSpace(in.Operator)}},
		{`INSERT INTO sites VALUES(?,?,?)`, []any{site, operator, strings.TrimSpace(in.Site)}},
		{`INSERT INTO locations VALUES(?,?,?)`, []any{location, site, strings.TrimSpace(in.Location)}},
	} {
		if _, err = tx.ExecContext(ctx, insert.SQL, insert.Args...); err != nil {
			return err
		}
	}
	for _, right := range []struct {
		Enabled bool
		Role    string
	}{{*in.MaterialManager, "material_manager"}, {*in.DeviceManager, "device_manager"}} {
		if right.Enabled {
			if _, err = tx.ExecContext(ctx, `INSERT INTO role_assignments VALUES(?,?,?)`, admin, site, right.Role); err != nil {
				return err
			}
		}
	}
	revision, err = configureModulesTx(ctx, tx, modules, admin, "setup", "Funktionsauswahl bei der Ersteinrichtung")
	if err != nil {
		return err
	}
	if _, err = administrativeEvent(ctx, tx, admin, "web_setup_completed", admin, map[string]any{"operator_id": operator, "site_id": site, "location_id": location, "module_revision": revision, "material_manager": *in.MaterialManager, "device_manager": *in.DeviceManager}); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE web_setup_state SET token_hash='',completed_at=?,admin_id=?,completion_reason='completed' WHERE id=1 AND completed_at IS NULL`, a.now().UTC().Format(time.RFC3339Nano), admin); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	// The database is the authority; a failed cleanup never reopens the setup.
	if code, err := readSetupCodeFile(a.runtime.SetupCodeFile); err == nil && subtle.ConstantTimeCompare([]byte(code), []byte(in.Code)) == 1 {
		_ = os.Remove(a.runtime.SetupCodeFile)
	}
	return nil
}

func webSetupRoutes(mux *http.ServeMux, a *authService) {
	mux.HandleFunc("GET /api/v1/setup/status", func(w http.ResponseWriter, r *http.Request) {
		pending, err := WebSetupPending(r.Context(), a.db)
		if err != nil {
			sendStockError(w, err)
			return
		}
		result := map[string]any{"pending": pending}
		if pending {
			_, revision, err := ActiveModules(r.Context(), a.db)
			if err != nil {
				sendStockError(w, err)
				return
			}
			nonce := randomToken()
			http.SetCookie(w, &http.Cookie{Name: a.setupCookieName(), Value: nonce, Path: "/", HttpOnly: true, Secure: !a.localHTTP, SameSite: http.SameSiteStrictMode, MaxAge: 900})
			definitions := append([]moduleDefinition{}, moduleDefinitions...)
			for i := range definitions {
				definitions[i].Enabled = definitions[i].ID == "inventory"
			}
			result["csrf_token"] = digest("setup-csrf:" + nonce)
			result["revision"] = revision
			result["modules"] = definitions
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(result)
	})
	mux.HandleFunc("POST /api/v1/setup/checks", func(w http.ResponseWriter, r *http.Request) {
		if !a.setupRequest(w, r) {
			return
		}
		var input setupCheckInput
		if !setupBody(w, r, &input, 8192) {
			return
		}
		if err := a.checkSetupCode(r.Context(), input.Code); err != nil {
			sendStockError(w, err)
			return
		}
		modules, err := modulesFromNames(input.Modules)
		if err != nil {
			sendStockError(w, err)
			return
		}
		checks, complete := a.setupEnvironmentChecks(r.Context(), modules)
		_, revision, err := ActiveModules(r.Context(), a.db)
		if err != nil {
			sendStockError(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"checks": checks, "can_complete": complete, "revision": revision})
	})
	mux.HandleFunc("POST /api/v1/setup/complete", func(w http.ResponseWriter, r *http.Request) {
		if !a.setupRequest(w, r) {
			return
		}
		var input setupCompleteInput
		if !setupBody(w, r, &input, 16384) {
			return
		}
		select {
		case a.loginSlots <- struct{}{}:
			defer func() { <-a.loginSlots }()
		default:
			apiError(w, 429, "rate_limited", "Eine Einrichtung wird bereits geprüft. Bitte später erneut versuchen.")
			return
		}
		if err := a.completeWebSetup(r.Context(), input); err != nil {
			sendStockError(w, err)
			return
		}
		http.SetCookie(w, &http.Cookie{Name: a.setupCookieName(), Path: "/", Secure: !a.localHTTP, HttpOnly: true, SameSite: http.SameSiteStrictMode, MaxAge: -1})
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]bool{"completed": true})
	})
}

// Bootstrap proof is consumed on completion; no credential-bearing anonymous
// historical reply is stored. It has its own bounded JSON reader.
func setupBody(w http.ResponseWriter, r *http.Request, input any, limit int64) bool {
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		apiError(w, 415, "content_type", "JSON erforderlich.")
		return false
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, limit))
	decoder.DisallowUnknownFields()
	err := decoder.Decode(input)
	if err == nil {
		err = decoder.Decode(new(any))
		if err == io.EOF {
			return true
		}
	}
	var size *http.MaxBytesError
	if errors.As(err, &size) {
		apiError(w, 413, "setup_body_limit", "Einrichtungsdaten zu groß.")
	} else {
		apiError(w, 400, "invalid_json", "Ungültige Einrichtungsdaten.")
	}
	return false
}
