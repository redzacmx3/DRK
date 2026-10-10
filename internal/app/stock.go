package app

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"
	"time"
	_ "time/tzdata"
)

var operatorZone = func() *time.Location {
	z, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		panic(err)
	}
	return z
}()

// The runtime image may not have system tzdata.
// (Time zone data is embedded via cmd/inventory.)
func localDay(now time.Time) string { return now.In(operatorZone).Format("2006-01-02") }
func validDay(s string) bool {
	d, err := time.Parse("2006-01-02", s)
	return err == nil && d.Format("2006-01-02") == s
}
func CreateProduct(db *sql.DB, name, unit string) (string, error) {
	if strings.TrimSpace(name) == "" || len(name) > 160 || strings.TrimSpace(unit) == "" || len(unit) > 40 {
		return "", fmt.Errorf("name and explicit base unit are required")
	}
	id := newID()
	_, err := db.Exec(`INSERT INTO product_types VALUES(?,?,?)`, id, name, unit)
	return id, err
}
func CreateBatch(db *sql.DB, product, lot, expiry string) (string, error) {
	if strings.TrimSpace(lot) == "" || len(lot) > 100 || expiry != "" && !validDay(expiry) {
		return "", fmt.Errorf("lot and a valid exact expiry date (or no date) are required")
	}
	var date any
	if expiry != "" {
		date = expiry
	}
	id := newID()
	_, err := db.Exec(`INSERT INTO batches(id,product_id,lot,expires_on) VALUES(?,?,?,?)`, id, product, lot, date)
	return id, err
}

type stockRequest struct {
	BatchID         string `json:"batch_id"`
	From            string `json:"from_location_id,omitempty"`
	To              string `json:"to_location_id,omitempty"`
	Quantity        int64  `json:"quantity"`
	ExpectedVersion int64  `json:"expected_version,omitempty"`
	State           string `json:"state,omitempty"`
	Note            string `json:"note,omitempty"`
}
type balanceResult struct {
	LocationID string `json:"location_id"`
	State      string `json:"state"`
	Quantity   int64  `json:"quantity"`
	Version    int64  `json:"version"`
}
type movementResult struct {
	ID       string          `json:"movement_id"`
	Kind     string          `json:"kind"`
	Quantity int64           `json:"quantity"`
	Balances []balanceResult `json:"balances"`
}
type stockError struct {
	Status        int
	Code, Message string
}

func (e *stockError) Error() string       { return e.Code }
func conflict(code, message string) error { return &stockError{409, code, message} }
func scopeError() error {
	return &stockError{404, "not_found", "Lagerort nicht gefunden oder Aktion nicht erlaubt."}
}
func sendStockError(w http.ResponseWriter, err error) {
	var e *stockError
	if errors.As(err, &e) {
		apiError(w, e.Status, e.Code, e.Message)
		return
	}
	if errors.Is(err, errUnauthenticated) {
		apiError(w, 401, "unauthenticated", "Bitte erneut anmelden.")
		return
	}
	apiError(w, 503, "unavailable", "Buchung nicht bestätigt. Mit demselben Idempotenzschlüssel erneut versuchen.")
}

func checkLocation(ctx context.Context, tx *sql.Tx, user, location, kind string) error {
	roles := []string{"material_manager"}
	if kind == "consumption" {
		roles = append(roles, "helper")
	}
	var exists bool
	err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM locations l JOIN role_assignments a ON a.site_id=l.site_id WHERE l.id=? AND a.user_id=? AND (a.role=? OR a.role=?))`, location, user, roles[0], roles[len(roles)-1]).Scan(&exists)
	if err != nil {
		return err
	}
	if !exists {
		return scopeError()
	}
	return nil
}

// bookStock holds an IMMEDIATE transaction across authorization, balances,
// journal, audit, outbox and the exact replay response. No network work occurs here.
func bookStock(ctx context.Context, db *sql.DB, user, tokenHash, key, kind string, input stockRequest, now time.Time) ([]byte, error) {
	if input.State == "" {
		input.State = "available"
	}
	if input.Quantity <= 0 || input.Quantity > 1_000_000_000 || len(input.BatchID) > 100 || input.BatchID == "" || len(input.From) > 100 || len(input.To) > 100 || len(input.Note) > 500 ||
		(input.State != "available" && input.State != "quarantined" && input.State != "damaged") ||
		(kind == "receipt" && (input.To == "" || input.From != "" || input.ExpectedVersion != 0)) ||
		(kind == "transfer" && (input.From == "" || input.To == "" || input.From == input.To || input.ExpectedVersion <= 0)) ||
		(kind == "consumption" && (input.From == "" || input.To != "" || input.ExpectedVersion <= 0 || input.State != "available")) ||
		(kind != "receipt" && kind != "transfer" && kind != "consumption") {
		return nil, &stockError{400, "invalid_request", "Charge, Lagerorte, Menge, Zustand und Quellversion prüfen."}
	}
	canonical, _ := json.Marshal(input)
	hash := digest(kind + ":" + string(canonical))
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err = activeStockSession(ctx, tx, user, tokenHash, now); err != nil {
		return nil, err
	}
	for _, location := range []string{input.From, input.To} {
		if location != "" {
			if err = checkLocation(ctx, tx, user, location, kind); err != nil {
				return nil, err
			}
		}
	}
	if err = rejectPackKey(ctx, tx, user, key); err != nil {
		return nil, err
	}
	var existingHash, response string
	err = tx.QueryRowContext(ctx, `SELECT request_hash,response_json FROM idempotency_keys WHERE user_id=? AND key=?`, user, key).Scan(&existingHash, &response)
	if err == nil {
		if existingHash != hash {
			return nil, conflict("idempotency_conflict", "Dieser Schlüssel gehört zu einer anderen Buchung.")
		}
		return []byte(response), nil
	}
	if err != sql.ErrNoRows {
		return nil, err
	}
	var expiry sql.NullString
	var blocked int
	err = tx.QueryRowContext(ctx, `SELECT expires_on,blocked FROM batches WHERE id=?`, input.BatchID).Scan(&expiry, &blocked)
	if err == sql.ErrNoRows {
		return nil, &stockError{404, "not_found", "Charge nicht gefunden."}
	}
	if err != nil {
		return nil, err
	}
	usable := blocked == 0 && expiry.Valid && validDay(expiry.String) && expiry.String >= localDay(now)
	if kind == "consumption" && !usable {
		return nil, conflict("not_usable", "Charge ist gesperrt, abgelaufen oder ihr Verfallsdatum ist ungeklärt.")
	}
	state := input.State
	if kind == "receipt" && state == "available" && !usable {
		state = "quarantined"
	}
	if input.From != "" {
		result, err := tx.ExecContext(ctx, `UPDATE stock_balances SET quantity=quantity-?,version=version+1 WHERE batch_id=? AND location_id=? AND state=? AND quantity>=? AND version=? AND version<?`, input.Quantity, input.BatchID, input.From, input.State, input.Quantity, input.ExpectedVersion, math.MaxInt64)
		if err != nil {
			return nil, err
		}
		changed, err := result.RowsAffected()
		if err != nil {
			return nil, err
		}
		if changed != 1 {
			return nil, conflict("stock_conflict", "Bestand oder Version hat sich geändert. Aktuellen Bestand neu laden.")
		}
	}
	if input.To != "" {
		result, err := tx.ExecContext(ctx, `INSERT INTO stock_balances(batch_id,location_id,state,quantity,version) VALUES(?,?,?,?,1) ON CONFLICT(batch_id,location_id,state) DO UPDATE SET quantity=stock_balances.quantity+excluded.quantity,version=stock_balances.version+1 WHERE stock_balances.quantity<=? AND stock_balances.version<?`, input.BatchID, input.To, state, input.Quantity, math.MaxInt64-input.Quantity, math.MaxInt64)
		if err != nil {
			return nil, err
		}
		changed, err := result.RowsAffected()
		if err != nil {
			return nil, err
		}
		if changed != 1 {
			return nil, conflict("stock_conflict", "Zielbestand kann nicht erhöht werden.")
		}
	}
	id := newID()
	stamp := now.UTC().Format(time.RFC3339Nano)
	nullable := func(s string) any {
		if s == "" {
			return nil
		}
		return s
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO stock_movements(id,kind,user_id,batch_id,from_location_id,to_location_id,quantity,state,created_at,note) VALUES(?,?,?,?,?,?,?,?,?,?)`, id, kind, user, input.BatchID, nullable(input.From), nullable(input.To), input.Quantity, state, stamp, input.Note); err != nil {
		return nil, err
	}
	result := movementResult{ID: id, Kind: kind, Quantity: input.Quantity, Balances: []balanceResult{}}
	for _, line := range []struct {
		Location, State string
		Delta           int64
	}{{input.From, input.State, -input.Quantity}, {input.To, state, input.Quantity}} {
		if line.Location == "" {
			continue
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO movement_lines VALUES(?,?,?,?,?)`, id, input.BatchID, line.Location, line.State, line.Delta); err != nil {
			return nil, err
		}
		balance := balanceResult{LocationID: line.Location, State: line.State}
		if err = tx.QueryRowContext(ctx, `SELECT quantity,version FROM stock_balances WHERE batch_id=? AND location_id=? AND state=?`, input.BatchID, line.Location, line.State).Scan(&balance.Quantity, &balance.Version); err != nil {
			return nil, err
		}
		result.Balances = append(result.Balances, balance)
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO audit_events VALUES(?,?,?,?,?)`, newID(), user, "stock."+kind, id, stamp); err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO outbox(id,movement_id,event_type,created_at) VALUES(?,?,?,?)`, newID(), id, "stock."+kind, stamp); err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO idempotency_keys VALUES(?,?,?,?,?)`, user, key, hash, id, string(encoded)); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return encoded, nil
}

func writeAuthorized(a *authService, w http.ResponseWriter, r *http.Request) (identity, bool) {
	user, err := a.session(r)
	if err != nil {
		a.sessionError(w, err)
		return user, false
	}
	if !a.sameOrigin(w, r) {
		return user, false
	}
	cookie, _ := r.Cookie(cookieName)
	var hash string
	if err = a.db.QueryRowContext(r.Context(), `SELECT csrf_hash FROM sessions WHERE token_hash=?`, digest(cookie.Value)).Scan(&hash); err != nil {
		a.sessionError(w, err)
		return user, false
	}
	if subtle.ConstantTimeCompare([]byte(hash), []byte(digest(r.Header.Get("X-CSRF-Token")))) != 1 {
		apiError(w, 403, "csrf_rejected", "Ungültiger CSRF-Nachweis.")
		return user, false
	}
	return user, true
}
func validKey(s string) bool {
	if len(s) < 16 || len(s) > 100 {
		return false
	}
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_') {
			return false
		}
	}
	return true
}
func stockRoutes(mux *http.ServeMux, a *authService) {
	for path, kind := range map[string]string{"receipts": "receipt", "transfers": "transfer", "consumptions": "consumption"} {
		mux.HandleFunc("POST /api/v1/stock/"+path, func(w http.ResponseWriter, r *http.Request) {
			user, ok := writeAuthorized(a, w, r)
			if !ok {
				return
			}
			key := r.Header.Get("Idempotency-Key")
			if !validKey(key) {
				apiError(w, 400, "idempotency_required", "Idempotency-Key mit 16–100 ASCII-Zeichen erforderlich.")
				return
			}
			if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
				apiError(w, 415, "content_type", "JSON erforderlich.")
				return
			}
			r.Body = http.MaxBytesReader(w, r.Body, 4096)
			decoder := json.NewDecoder(r.Body)
			decoder.DisallowUnknownFields()
			var input stockRequest
			if decoder.Decode(&input) != nil || decoder.Decode(&struct{}{}) != io.EOF {
				apiError(w, 400, "invalid_request", "Ungültige Buchungsdaten.")
				return
			}
			cookie, _ := r.Cookie(cookieName)
			result, err := bookStock(r.Context(), a.db, user.ID, digest(cookie.Value), key, kind, input, a.now())
			if err != nil {
				sendStockError(w, err)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(201)
			w.Write(result)
		})
	}
	mux.HandleFunc("GET /api/v1/stock/catalog", func(w http.ResponseWriter, r *http.Request) {
		user, err := a.session(r)
		if err != nil {
			a.sessionError(w, err)
			return
		}
		type batch struct {
			ID        string  `json:"id"`
			ProductID string  `json:"product_id"`
			Product   string  `json:"product"`
			Unit      string  `json:"unit"`
			Lot       string  `json:"lot"`
			Expires   *string `json:"expires_on"`
			Blocked   bool    `json:"blocked"`
		}
		rows, err := a.db.QueryContext(r.Context(), `SELECT b.id,p.id,p.name,p.base_unit,b.lot,b.expires_on,b.blocked FROM batches b JOIN product_types p ON p.id=b.product_id WHERE EXISTS(SELECT 1 FROM role_assignments WHERE user_id=? AND role IN ('viewer','helper','material_manager')) ORDER BY p.name,b.lot,b.id LIMIT 500`, user.ID)
		if err != nil {
			sendStockError(w, err)
			return
		}
		defer rows.Close()
		items := []batch{}
		for rows.Next() {
			var b batch
			if err = rows.Scan(&b.ID, &b.ProductID, &b.Product, &b.Unit, &b.Lot, &b.Expires, &b.Blocked); err != nil {
				sendStockError(w, err)
				return
			}
			items = append(items, b)
		}
		if err = rows.Err(); err != nil {
			sendStockError(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Data-As-Of", a.now().UTC().Format(time.RFC3339))
		json.NewEncoder(w).Encode(items)
	})
	mux.HandleFunc("GET /api/v1/stock/balances", func(w http.ResponseWriter, r *http.Request) {
		user, err := a.session(r)
		if err != nil {
			a.sessionError(w, err)
			return
		}
		type balance struct {
			BatchID    string  `json:"batch_id"`
			LocationID string  `json:"location_id"`
			State      string  `json:"state"`
			Quantity   int64   `json:"quantity"`
			Usable     int64   `json:"usable_quantity"`
			Version    int64   `json:"version"`
			Product    string  `json:"product"`
			Unit       string  `json:"unit"`
			Lot        string  `json:"lot"`
			Location   string  `json:"location"`
			Expires    *string `json:"expires_on"`
			Blocked    bool    `json:"blocked"`
		}
		rows, err := a.db.QueryContext(r.Context(), `SELECT b.batch_id,b.location_id,b.state,b.quantity,b.version,p.name,p.base_unit,c.lot,l.name,c.expires_on,c.blocked FROM stock_balances b JOIN batches c ON c.id=b.batch_id JOIN product_types p ON p.id=c.product_id JOIN locations l ON l.id=b.location_id WHERE EXISTS(SELECT 1 FROM role_assignments a WHERE a.user_id=? AND a.site_id=l.site_id AND a.role IN ('viewer','helper','material_manager')) ORDER BY p.name,c.lot,l.name,b.state LIMIT 500`, user.ID)
		if err != nil {
			sendStockError(w, err)
			return
		}
		defer rows.Close()
		items := []balance{}
		day := localDay(a.now())
		for rows.Next() {
			var b balance
			if err = rows.Scan(&b.BatchID, &b.LocationID, &b.State, &b.Quantity, &b.Version, &b.Product, &b.Unit, &b.Lot, &b.Location, &b.Expires, &b.Blocked); err != nil {
				sendStockError(w, err)
				return
			}
			if b.State == "available" && !b.Blocked && b.Expires != nil && validDay(*b.Expires) && *b.Expires >= day {
				b.Usable = b.Quantity
			}
			items = append(items, b)
		}
		if err = rows.Err(); err != nil {
			sendStockError(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Data-As-Of", a.now().UTC().Format(time.RFC3339))
		json.NewEncoder(w).Encode(items)
	})
}
