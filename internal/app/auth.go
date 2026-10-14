package app

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/argon2"
)

const cookieName = "__Host-session"
const absoluteLifetime = 8 * time.Hour
const idleLifetime = 30 * time.Minute

var errUnauthenticated = errors.New("unauthenticated")

func randomToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}
func newID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	b[6] = (b[6] & 15) | 64
	b[8] = (b[8] & 63) | 128
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[:4], b[4:6], b[6:8], b[8:10], b[10:])
}
func digest(s string) string { h := sha256.Sum256([]byte(s)); return hex.EncodeToString(h[:]) }
func passwordHash(password string) string {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		panic(err)
	}
	hash := argon2.IDKey([]byte(password), salt, 3, 64*1024, 2, 32)
	return "$argon2id$v=19$m=65536,t=3,p=2$" + base64.RawStdEncoding.EncodeToString(salt) + "$" + base64.RawStdEncoding.EncodeToString(hash)
}
func passwordMatches(encoded, password string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" || parts[2] != "v=19" || parts[3] != "m=65536,t=3,p=2" {
		return false
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil || len(salt) != 16 {
		return false
	}
	expected, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil || len(expected) != 32 {
		return false
	}
	actual := argon2.IDKey([]byte(password), salt, 3, 64*1024, 2, 32)
	return subtle.ConstantTimeCompare(expected, actual) == 1
}

// CreateViewer is a local administrative operation, never an unauthenticated HTTP route.
func CreateViewer(db *sql.DB, username, password, site string) error {
	return CreateUser(db, username, password, site, "viewer")
}
func validRole(role string) bool {
	return role == "device_manager" || role == "viewer" || role == "helper" || role == "material_manager"
}
func CreateUser(db *sql.DB, username, password, site, role string) error {
	if !validRole(role) {
		return fmt.Errorf("unknown role")
	}
	if strings.TrimSpace(username) != username || len(username) < 3 || len(username) > 100 {
		return fmt.Errorf("username must have 3–100 characters without surrounding whitespace")
	}
	if len(password) < 12 || len(password) > 256 {
		return fmt.Errorf("password must have 12–256 bytes")
	}
	hash := passwordHash(password)
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	id := newID()
	if _, err = tx.Exec(`INSERT INTO users(id,username,password_hash) VALUES(?,?,?)`, id, username, hash); err != nil {
		return err
	}
	if _, err = tx.Exec(`INSERT INTO role_assignments VALUES(?,?,?)`, id, site, role); err != nil {
		return err
	}
	return tx.Commit()
}

// SetViewerSites replaces scopes and revokes sessions atomically.
func SetViewerSites(db *sql.DB, username string, sites []string) error {
	return SetSiteRole(db, username, sites, "viewer")
}
func SetSiteRole(db *sql.DB, username string, sites []string, role string) error {
	if !validRole(role) {
		return fmt.Errorf("unknown role")
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var id string
	if err = tx.QueryRow(`SELECT id FROM users WHERE username=?`, username).Scan(&id); err != nil {
		return err
	}
	if _, err = tx.Exec(`DELETE FROM role_assignments WHERE user_id=?`, id); err != nil {
		return err
	}
	for _, site := range sites {
		if _, err = tx.Exec(`INSERT INTO role_assignments VALUES(?,?,?)`, id, site, role); err != nil {
			return err
		}
	}
	if _, err = tx.Exec(`DELETE FROM sessions WHERE user_id=?`, id); err != nil {
		return err
	}
	return tx.Commit()
}
func DisableUser(db *sql.DB, username string) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var id string
	if err = tx.QueryRow(`SELECT id FROM users WHERE username=?`, username).Scan(&id); err != nil {
		return err
	}
	if err = protectAdministrator(context.Background(), tx, id, false, ""); err != nil {
		return err
	}
	if _, err = tx.Exec(`UPDATE users SET active=0 WHERE id=?`, id); err != nil {
		return err
	}
	if _, err = tx.Exec(`DELETE FROM sessions WHERE user_id=?`, id); err != nil {
		return err
	}
	return tx.Commit()
}

type identity struct {
	ID       string `json:"id"`
	Username string `json:"username"`
}
type attemptWindow struct {
	Start time.Time
	Count int
}
type authService struct {
	modules        ModuleConfig
	dynamicModules bool
	runtime        RuntimeOptions
	clamdAddress   string
	documentSlots  chan struct{}
	db             *sql.DB
	origin         string
	localHTTP      bool
	now            func() time.Time
	dummy          string
	mu             sync.Mutex
	attempts       map[string]attemptWindow
	loginSlots     chan struct{}
	exportSlots    chan struct{}
}

func newAuth(db *sql.DB, origin string) *authService {
	return &authService{modules: allModules(), clamdAddress: os.Getenv("DRK_CLAMD_ADDRESS"), documentSlots: make(chan struct{}, 1), db: db, origin: origin, now: time.Now, dummy: passwordHash(randomToken()), attempts: map[string]attemptWindow{}, loginSlots: make(chan struct{}, 2), exportSlots: make(chan struct{}, 2)}
}
func (a *authService) cookieName() string {
	if a.localHTTP {
		return "drk-local-session"
	}
	return cookieName
}
func apiError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"code": code, "message": message})
}
func (a *authService) sessionError(w http.ResponseWriter, err error) {
	if errors.Is(err, errUnauthenticated) {
		apiError(w, 401, "unauthenticated", "Bitte anmelden.")
	} else {
		apiError(w, 503, "unavailable", "Datenbank nicht verfügbar.")
	}
}
func (a *authService) session(r *http.Request) (identity, error) {
	var user identity
	cookie, err := r.Cookie(a.cookieName())
	if err != nil || len(cookie.Value) != 43 {
		return user, errUnauthenticated
	}
	now := a.now().Unix()
	tx, err := a.db.BeginTx(r.Context(), nil)
	if err != nil {
		return user, err
	}
	defer tx.Rollback()
	var created, last int64
	err = tx.QueryRowContext(r.Context(), `SELECT u.id,u.username,s.created_at,s.last_seen FROM sessions s JOIN users u ON u.id=s.user_id WHERE s.token_hash=? AND u.active=1`, digest(cookie.Value)).Scan(&user.ID, &user.Username, &created, &last)
	if err == sql.ErrNoRows {
		return user, errUnauthenticated
	}
	if err != nil {
		return user, err
	}
	if now-created >= int64(absoluteLifetime.Seconds()) || now-last >= int64(idleLifetime.Seconds()) {
		if _, err = tx.ExecContext(r.Context(), `DELETE FROM sessions WHERE token_hash=?`, digest(cookie.Value)); err != nil {
			return user, err
		}
		if err = tx.Commit(); err != nil {
			return user, err
		}
		return user, errUnauthenticated
	}
	if _, err = tx.ExecContext(r.Context(), `UPDATE sessions SET last_seen=? WHERE token_hash=?`, now, digest(cookie.Value)); err != nil {
		return user, err
	}
	return user, tx.Commit()
}
func (a *authService) sameOrigin(w http.ResponseWriter, r *http.Request) bool {
	if r.Header.Get("Origin") != a.origin {
		apiError(w, 403, "origin_rejected", "Unzulässiger Ursprung.")
		return false
	}
	return true
}
func (a *authService) allowLogin(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	// Forwarded headers are deliberately ignored until trusted proxies are configured.
	a.mu.Lock()
	defer a.mu.Unlock()
	now := a.now()
	for key, window := range a.attempts {
		if now.Sub(window.Start) >= time.Minute {
			delete(a.attempts, key)
		}
	}
	window := a.attempts[host]
	if window.Count == 0 {
		if len(a.attempts) >= 4096 {
			return false
		}
		window.Start = now
	}
	if window.Count >= 10 {
		return false
	}
	window.Count++
	a.attempts[host] = window
	return true
}
func (a *authService) routes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/auth/login", func(w http.ResponseWriter, r *http.Request) {
		if r.TLS == nil && !a.localHTTP {
			apiError(w, 400, "https_required", "Anmeldung benötigt HTTPS.")
			return
		}
		if !a.sameOrigin(w, r) {
			return
		}
		if !a.allowLogin(r) {
			w.Header().Set("Retry-After", "60")
			apiError(w, 429, "rate_limited", "Zu viele Anmeldeversuche.")
			return
		}
		select {
		case a.loginSlots <- struct{}{}:
			defer func() { <-a.loginSlots }()
		default:
			apiError(w, 429, "rate_limited", "Bitte später erneut versuchen.")
			return
		}
		if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
			apiError(w, 415, "content_type", "JSON erforderlich.")
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 4096)
		var input struct {
			Username string `json:"username"`
			Password string `json:"password"`
		}
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		if decoder.Decode(&input) != nil || decoder.Decode(&struct{}{}) != io.EOF || len(input.Password) > 256 || len(input.Username) > 100 {
			apiError(w, 400, "invalid_request", "Ungültige Anmeldedaten.")
			return
		}
		var id, hash string
		var active int
		err := a.db.QueryRowContext(r.Context(), `SELECT id,password_hash,active FROM users WHERE username=?`, input.Username).Scan(&id, &hash, &active)
		if err != nil && err != sql.ErrNoRows {
			apiError(w, 503, "unavailable", "Datenbank nicht verfügbar.")
			return
		}
		if err == sql.ErrNoRows {
			hash = a.dummy
		}
		if !passwordMatches(hash, input.Password) || active != 1 {
			apiError(w, 401, "invalid_credentials", "Benutzername oder Passwort ungültig.")
			return
		}
		token := randomToken()
		csrf := digest("csrf:" + token)
		now := a.now().Unix()
		tx, err := a.db.BeginTx(r.Context(), nil)
		if err != nil {
			apiError(w, 503, "unavailable", "Datenbank nicht verfügbar.")
			return
		}
		defer tx.Rollback()
		// Recheck account state inside the transaction to avoid issuing a session after deactivation.
		var currentHash string
		if err = tx.QueryRowContext(r.Context(), `SELECT password_hash FROM users WHERE id=? AND active=1`, id).Scan(&currentHash); err != nil || currentHash != hash {
			apiError(w, 401, "invalid_credentials", "Benutzername oder Passwort ungültig.")
			return
		}
		if old, err := r.Cookie(a.cookieName()); err == nil {
			if _, err = tx.ExecContext(r.Context(), `DELETE FROM sessions WHERE token_hash=?`, digest(old.Value)); err != nil {
				apiError(w, 503, "unavailable", "Datenbank nicht verfügbar.")
				return
			}
		}
		if _, err = tx.ExecContext(r.Context(), `DELETE FROM sessions WHERE created_at<=? OR last_seen<=?`, now-int64(absoluteLifetime.Seconds()), now-int64(idleLifetime.Seconds())); err != nil {
			apiError(w, 503, "unavailable", "Datenbank nicht verfügbar.")
			return
		}
		if _, err = tx.ExecContext(r.Context(), `INSERT INTO sessions VALUES(?,?,?,?,?)`, digest(token), id, digest(csrf), now, now); err != nil {
			apiError(w, 503, "unavailable", "Datenbank nicht verfügbar.")
			return
		}
		if err = tx.Commit(); err != nil {
			apiError(w, 503, "unavailable", "Datenbank nicht verfügbar.")
			return
		}
		http.SetCookie(w, &http.Cookie{Name: a.cookieName(), Value: token, Path: "/", Secure: !a.localHTTP, HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: int(absoluteLifetime.Seconds())})
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"csrf_token": csrf, "username": input.Username})
	})
	mux.HandleFunc("GET /api/v1/me", func(w http.ResponseWriter, r *http.Request) {
		user, err := a.session(r)
		if err != nil {
			a.sessionError(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		rows, err := a.db.QueryContext(r.Context(), `SELECT site_id,role FROM role_assignments WHERE user_id=? ORDER BY site_id,role`, user.ID)
		if err != nil {
			a.sessionError(w, err)
			return
		}
		defer rows.Close()
		type right struct {
			SiteID string `json:"site_id"`
			Role   string `json:"role"`
		}
		rights := []right{}
		for rows.Next() {
			var v right
			if err = rows.Scan(&v.SiteID, &v.Role); err != nil {
				a.sessionError(w, err)
				return
			}
			rights = append(rights, v)
		}
		if err = rows.Err(); err != nil {
			a.sessionError(w, err)
			return
		}
		rows.Close()
		cookie, _ := r.Cookie(a.cookieName())
		var administrator bool
		if err = a.db.QueryRowContext(r.Context(), `SELECT EXISTS(SELECT 1 FROM administrators WHERE user_id=?)`, user.ID).Scan(&administrator); err != nil {
			a.sessionError(w, err)
			return
		}
		json.NewEncoder(w).Encode(struct {
			ID            string  `json:"id"`
			Username      string  `json:"username"`
			CSRF          string  `json:"csrf_token"`
			Rights        []right `json:"rights"`
			Administrator bool    `json:"administrator"`
		}{user.ID, user.Username, digest("csrf:" + cookie.Value), rights, administrator})
	})
	mux.HandleFunc("POST /api/v1/auth/logout", func(w http.ResponseWriter, r *http.Request) {
		if !a.sameOrigin(w, r) {
			return
		}
		_, err := a.session(r)
		if err != nil {
			a.sessionError(w, err)
			return
		}
		cookie, _ := r.Cookie(a.cookieName())
		var expected string
		if err = a.db.QueryRowContext(r.Context(), `SELECT csrf_hash FROM sessions WHERE token_hash=?`, digest(cookie.Value)).Scan(&expected); err != nil {
			a.sessionError(w, err)
			return
		}
		if subtle.ConstantTimeCompare([]byte(expected), []byte(digest(r.Header.Get("X-CSRF-Token")))) != 1 {
			apiError(w, 403, "csrf_rejected", "Ungültiger CSRF-Nachweis.")
			return
		}
		if _, err = a.db.ExecContext(r.Context(), `DELETE FROM sessions WHERE token_hash=?`, digest(cookie.Value)); err != nil {
			a.sessionError(w, err)
			return
		}
		http.SetCookie(w, &http.Cookie{Name: a.cookieName(), Path: "/", Secure: !a.localHTTP, HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: -1})
		w.WriteHeader(http.StatusNoContent)
	})
}
