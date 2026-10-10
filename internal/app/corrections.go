package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

type versionExpectation struct {
	Location string `json:"location_id"`
	State    string `json:"state"`
	Version  int64  `json:"version"`
}
type reversalRequest struct {
	Versions []versionExpectation `json:"expected_versions"`
	Note     string               `json:"note"`
}
type inventoryRequest struct {
	BatchID         string `json:"batch_id"`
	Location        string `json:"location_id"`
	State           string `json:"state,omitempty"`
	ExpectedVersion int64  `json:"expected_version"`
	Counted         *int64 `json:"counted_quantity"`
	Note            string `json:"note"`
}
type correctionRecord struct {
	ID, Kind, Batch, From, To, State, Note, Reverses string
	Quantity                                         int64
	Counted, Basis, BasisVersion                     *int64
}
type correctionLine struct {
	Location, State string
	Delta           int64
}
type correctionResult struct {
	ID       string          `json:"movement_id"`
	Kind     string          `json:"kind"`
	Reverses string          `json:"reversed_movement_id,omitempty"`
	Quantity int64           `json:"quantity"`
	Counted  *int64          `json:"counted_quantity,omitempty"`
	Basis    *int64          `json:"basis_quantity,omitempty"`
	Delta    *int64          `json:"delta,omitempty"`
	Balances []balanceResult `json:"balances"`
}

func validState(s string) bool     { return s == "available" || s == "quarantined" || s == "damaged" }
func validReason(note string) bool { return strings.TrimSpace(note) != "" && len(note) <= 500 }
func nullableString(s string) any {
	if s == "" {
		return nil
	}
	return s
}
func activeStockSession(ctx context.Context, tx *sql.Tx, user, tokenHash string, now time.Time) error {
	var active bool
	err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM users u JOIN sessions s ON s.user_id=u.id WHERE u.id=? AND u.active=1 AND s.token_hash=? AND s.created_at>? AND s.last_seen>?)`, user, tokenHash, now.Add(-absoluteLifetime).Unix(), now.Add(-idleLifetime).Unix()).Scan(&active)
	if err != nil {
		return err
	}
	if !active {
		return errUnauthenticated
	}
	return nil
}
func replayCorrection(ctx context.Context, tx *sql.Tx, user, key, hash string) ([]byte, error) {
	if err := rejectOtherKey(ctx, tx, user, key, "idempotency_keys"); err != nil {
		return nil, err
	}
	var oldHash, response string
	err := tx.QueryRowContext(ctx, `SELECT request_hash,response_json FROM idempotency_keys WHERE user_id=? AND key=?`, user, key).Scan(&oldHash, &response)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if oldHash != hash {
		return nil, conflict("idempotency_conflict", "Dieser Schlüssel gehört zu einer anderen Buchung.")
	}
	return []byte(response), nil
}
func currentBalance(ctx context.Context, tx *sql.Tx, batch, location, state string) (int64, int64, error) {
	var quantity, version int64
	err := tx.QueryRowContext(ctx, `SELECT quantity,version FROM stock_balances WHERE batch_id=? AND location_id=? AND state=?`, batch, location, state).Scan(&quantity, &version)
	if err == sql.ErrNoRows {
		return 0, 0, nil
	}
	return quantity, version, err
}
func saveCorrection(ctx context.Context, tx *sql.Tx, user, key, hash string, record correctionRecord, lines []correctionLine, now time.Time) ([]byte, error) {
	stamp := now.UTC().Format(time.RFC3339Nano)
	_, err := tx.ExecContext(ctx, `INSERT INTO stock_movements(id,kind,user_id,batch_id,from_location_id,to_location_id,quantity,state,created_at,note,reversed_movement_id,counted_quantity,basis_quantity,basis_version) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, record.ID, record.Kind, user, record.Batch, nullableString(record.From), nullableString(record.To), record.Quantity, record.State, stamp, record.Note, nullableString(record.Reverses), record.Counted, record.Basis, record.BasisVersion)
	if err != nil {
		return nil, err
	}
	result := correctionResult{ID: record.ID, Kind: record.Kind, Reverses: record.Reverses, Quantity: record.Quantity, Counted: record.Counted, Basis: record.Basis, Balances: []balanceResult{}}
	if record.Counted != nil {
		delta := *record.Counted - *record.Basis
		result.Delta = &delta
	}
	for _, line := range lines {
		if line.Delta != 0 {
			if _, err = tx.ExecContext(ctx, `INSERT INTO movement_lines VALUES(?,?,?,?,?)`, record.ID, record.Batch, line.Location, line.State, line.Delta); err != nil {
				return nil, err
			}
		}
		quantity, version, err := currentBalance(ctx, tx, record.Batch, line.Location, line.State)
		if err != nil {
			return nil, err
		}
		result.Balances = append(result.Balances, balanceResult{LocationID: line.Location, State: line.State, Quantity: quantity, Version: version})
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO audit_events VALUES(?,?,?,?,?)`, newID(), user, "stock."+record.Kind, record.ID, stamp); err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO outbox(id,movement_id,event_type,created_at) VALUES(?,?,?,?)`, newID(), record.ID, "stock."+record.Kind, stamp); err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO idempotency_keys VALUES(?,?,?,?,?)`, user, key, hash, record.ID, string(encoded)); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return encoded, nil
}

func bookInventory(ctx context.Context, db *sql.DB, user, tokenHash, key string, input inventoryRequest, now time.Time) ([]byte, error) {
	if input.State == "" {
		input.State = "available"
	}
	if input.BatchID == "" || len(input.BatchID) > 100 || input.Location == "" || len(input.Location) > 100 || !validState(input.State) || input.Counted == nil || *input.Counted < 0 || *input.Counted > 1_000_000_000 || input.ExpectedVersion < 0 || !validReason(input.Note) {
		return nil, &stockError{400, "invalid_request", "Charge, Lagerort, Zustand, gezählte Menge, Basisversion und Begründung sind erforderlich."}
	}
	canonical, _ := json.Marshal(input)
	hash := digest("inventory:" + string(canonical))
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err = activeStockSession(ctx, tx, user, tokenHash, now); err != nil {
		return nil, err
	}
	if err = checkLocation(ctx, tx, user, input.Location, "inventory"); err != nil {
		return nil, err
	}
	if response, err := replayCorrection(ctx, tx, user, key, hash); err != nil || response != nil {
		return response, err
	}
	var expiry sql.NullString
	var blocked int
	if err = tx.QueryRowContext(ctx, `SELECT expires_on,blocked FROM batches WHERE id=?`, input.BatchID).Scan(&expiry, &blocked); err == sql.ErrNoRows {
		return nil, &stockError{404, "not_found", "Charge nicht gefunden."}
	} else if err != nil {
		return nil, err
	}
	basis, version, err := currentBalance(ctx, tx, input.BatchID, input.Location, input.State)
	if err != nil {
		return nil, err
	}
	if version != input.ExpectedVersion || version == math.MaxInt64 {
		return nil, conflict("stock_conflict", "Seit dem Zählstart hat sich der Bestand geändert. Neu zählen und abgleichen.")
	}
	delta := *input.Counted - basis
	if delta > 0 && input.State == "available" && (blocked != 0 || !expiry.Valid || !validDay(expiry.String) || expiry.String < localDay(now)) {
		return nil, conflict("not_usable", "Ungeklärte oder gesperrte Mehrmengen müssen im passenden Quarantänebestand erfasst werden.")
	}
	result, err := tx.ExecContext(ctx, `INSERT INTO stock_balances(batch_id,location_id,state,quantity,version) VALUES(?,?,?,?,1) ON CONFLICT(batch_id,location_id,state) DO UPDATE SET quantity=excluded.quantity,version=stock_balances.version+1 WHERE stock_balances.version=? AND stock_balances.version<?`, input.BatchID, input.Location, input.State, *input.Counted, input.ExpectedVersion, math.MaxInt64)
	if err != nil {
		return nil, err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return nil, err
	}
	if changed != 1 {
		return nil, conflict("stock_conflict", "Inventurversion hat sich geändert.")
	}
	amount := delta
	if amount < 0 {
		amount = -amount
	}
	record := correctionRecord{ID: newID(), Kind: "inventory", Batch: input.BatchID, To: input.Location, State: input.State, Note: input.Note, Quantity: amount, Counted: input.Counted, Basis: &basis, BasisVersion: &version}
	return saveCorrection(ctx, tx, user, key, hash, record, []correctionLine{{input.Location, input.State, delta}}, now)
}
func bookReversal(ctx context.Context, db *sql.DB, user, tokenHash, key, originalID string, input reversalRequest, now time.Time) ([]byte, error) {
	if originalID == "" || len(originalID) > 100 || len(input.Versions) < 1 || len(input.Versions) > 2 || !validReason(input.Note) {
		return nil, &stockError{400, "invalid_request", "Originalbuchung, aktuelle Bestandsversionen und Begründung sind erforderlich."}
	}
	expected := map[string]int64{}
	for _, v := range input.Versions {
		index := v.Location + "|" + v.State
		if v.Location == "" || len(v.Location) > 100 || !validState(v.State) || v.Version <= 0 {
			return nil, &stockError{400, "invalid_request", "Ungültige Bestandsversion."}
		}
		if _, exists := expected[index]; exists {
			return nil, &stockError{400, "invalid_request", "Doppelte Bestandsversion."}
		}
		expected[index] = v.Version
	}
	sort.Slice(input.Versions, func(i, j int) bool {
		return input.Versions[i].Location+"|"+input.Versions[i].State < input.Versions[j].Location+"|"+input.Versions[j].State
	})
	canonical, _ := json.Marshal(input)
	hash := digest("reversal:" + originalID + ":" + string(canonical))
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err = activeStockSession(ctx, tx, user, tokenHash, now); err != nil {
		return nil, err
	}
	var kind, batch, state string
	var quantity int64
	var from, to sql.NullString
	err = tx.QueryRowContext(ctx, `SELECT kind,batch_id,state,quantity,from_location_id,to_location_id FROM stock_movements WHERE id=?`, originalID).Scan(&kind, &batch, &state, &quantity, &from, &to)
	if err == sql.ErrNoRows {
		return nil, scopeError()
	}
	if err != nil {
		return nil, err
	}
	for _, loc := range []sql.NullString{from, to} {
		if loc.Valid {
			if err = checkLocation(ctx, tx, user, loc.String, "reversal"); err != nil {
				return nil, err
			}
		}
	}
	if response, err := replayCorrection(ctx, tx, user, key, hash); err != nil || response != nil {
		return response, err
	}
	if kind == "reversal" || quantity == 0 {
		return nil, conflict("not_reversible", "Gegenbuchungen und Inventuren ohne Mengendifferenz werden nicht erneut gegengebucht.")
	}
	var reversed bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM stock_movements WHERE reversed_movement_id=?)`, originalID).Scan(&reversed); err != nil {
		return nil, err
	}
	if reversed {
		return nil, conflict("already_reversed", "Diese Bewegung wurde bereits gegengebucht.")
	}
	rows, err := tx.QueryContext(ctx, `SELECT location_id,state,delta FROM movement_lines WHERE movement_id=? ORDER BY location_id,state`, originalID)
	if err != nil {
		return nil, err
	}
	lines := []correctionLine{}
	for rows.Next() {
		var line correctionLine
		if err = rows.Scan(&line.Location, &line.State, &line.Delta); err != nil {
			rows.Close()
			return nil, err
		}
		line.Delta = -line.Delta
		lines = append(lines, line)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	if len(lines) != len(expected) || len(lines) == 0 {
		return nil, &stockError{400, "invalid_request", "Versionen aller betroffenen Bestände sind erforderlich."}
	}
	record := correctionRecord{ID: newID(), Kind: "reversal", Batch: batch, State: state, Note: input.Note, Quantity: quantity, Reverses: originalID}
	for _, line := range lines {
		wanted, exists := expected[line.Location+"|"+line.State]
		if !exists {
			return nil, &stockError{400, "invalid_request", "Versionen passen nicht zu den betroffenen Beständen."}
		}
		if err = checkLocation(ctx, tx, user, line.Location, "reversal"); err != nil {
			return nil, err
		}
		q, v, err := currentBalance(ctx, tx, batch, line.Location, line.State)
		if err != nil {
			return nil, err
		}
		if v != wanted || v == math.MaxInt64 || line.Delta < 0 && q < -line.Delta || line.Delta > 0 && q > math.MaxInt64-line.Delta {
			return nil, conflict("stock_conflict", "Gegenbuchung scheitert an Bestand oder Version. Bestand neu prüfen.")
		}
		result, err := tx.ExecContext(ctx, `UPDATE stock_balances SET quantity=quantity+?,version=version+1 WHERE batch_id=? AND location_id=? AND state=? AND version=?`, line.Delta, batch, line.Location, line.State, wanted)
		if err != nil {
			return nil, err
		}
		changed, err := result.RowsAffected()
		if err != nil {
			return nil, err
		}
		if changed != 1 {
			return nil, conflict("stock_conflict", "Bestandsversion hat sich geändert.")
		}
		if line.Delta < 0 {
			record.From = line.Location
		} else {
			record.To = line.Location
		}
	}
	return saveCorrection(ctx, tx, user, key, hash, record, lines, now)
}
func correctionBody(w http.ResponseWriter, r *http.Request, input any) bool {
	return mutationBody(w, r, input, 4096)
}
func mutationBody(w http.ResponseWriter, r *http.Request, input any, limit int64) bool {
	if !validKey(r.Header.Get("Idempotency-Key")) {
		apiError(w, 400, "idempotency_required", "Gültiger Idempotency-Key erforderlich.")
		return false
	}
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		apiError(w, 415, "content_type", "JSON erforderlich.")
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if decoder.Decode(input) != nil || decoder.Decode(&struct{}{}) != io.EOF {
		apiError(w, 400, "invalid_request", "Ungültige Anfragedaten.")
		return false
	}
	return true
}
func correctionRoutes(mux *http.ServeMux, a *authService) {
	mux.HandleFunc("POST /api/v1/stock/inventories", func(w http.ResponseWriter, r *http.Request) {
		user, ok := writeAuthorized(a, w, r)
		if !ok {
			return
		}
		var input inventoryRequest
		if !correctionBody(w, r, &input) {
			return
		}
		cookie, _ := r.Cookie(a.cookieName())
		response, err := bookInventory(r.Context(), a.db, user.ID, digest(cookie.Value), r.Header.Get("Idempotency-Key"), input, a.now())
		if err != nil {
			sendStockError(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(201)
		w.Write(response)
	})
	mux.HandleFunc("POST /api/v1/stock/movements/{id}/reversal", func(w http.ResponseWriter, r *http.Request) {
		user, ok := writeAuthorized(a, w, r)
		if !ok {
			return
		}
		var input reversalRequest
		if !correctionBody(w, r, &input) {
			return
		}
		cookie, _ := r.Cookie(a.cookieName())
		response, err := bookReversal(r.Context(), a.db, user.ID, digest(cookie.Value), r.Header.Get("Idempotency-Key"), r.PathValue("id"), input, a.now())
		if err != nil {
			sendStockError(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(201)
		w.Write(response)
	})
	mux.HandleFunc("GET /api/v1/stock/movements", func(w http.ResponseWriter, r *http.Request) {
		user, err := a.session(r)
		if err != nil {
			a.sessionError(w, err)
			return
		}
		limit, offset := 50, 0
		if value := r.URL.Query().Get("limit"); value != "" {
			limit, err = strconv.Atoi(value)
			if err != nil || limit < 1 || limit > 100 {
				apiError(w, 400, "invalid_request", "Limit muss zwischen 1 und 100 liegen.")
				return
			}
		}
		if value := r.URL.Query().Get("offset"); value != "" {
			offset, err = strconv.Atoi(value)
			if err != nil || offset < 0 || offset > 100000 {
				apiError(w, 400, "invalid_request", "Ungültiger Seitenoffset.")
				return
			}
		}
		type movement struct {
			IdentifierID    *string `json:"identifier_id,omitempty"`
			IdentifierKind  *string `json:"identifier_kind,omitempty"`
			IdentifierValue *string `json:"identifier_value,omitempty"`
			PackCount       *int64  `json:"pack_count,omitempty"`
			PackQuantity    *int64  `json:"pack_quantity,omitempty"`
			ID              string  `json:"id"`
			Kind            string  `json:"kind"`
			Batch           string  `json:"batch_id"`
			Product         string  `json:"product"`
			Lot             string  `json:"lot"`
			Unit            string  `json:"unit"`
			From            *string `json:"from_location_id"`
			To              *string `json:"to_location_id"`
			FromName        *string `json:"from_location"`
			ToName          *string `json:"to_location"`
			Quantity        int64   `json:"quantity"`
			State           string  `json:"state"`
			Created         string  `json:"created_at"`
			Note            string  `json:"note"`
			Reverses        *string `json:"reversed_movement_id"`
			ReversedBy      *string `json:"reversed_by"`
			Counted         *int64  `json:"counted_quantity"`
			Basis           *int64  `json:"basis_quantity"`
			BasisVersion    *int64  `json:"basis_version"`
		}
		rows, err := a.db.QueryContext(r.Context(), `SELECT m.id,m.kind,m.batch_id,p.name,b.lot,p.base_unit,m.from_location_id,m.to_location_id,src.name,dst.name,m.quantity,m.state,m.created_at,m.note,m.reversed_movement_id,(SELECT r.id FROM stock_movements r WHERE r.reversed_movement_id=m.id),m.counted_quantity,m.basis_quantity,m.basis_version,pack.identifier_id,ident.kind,ident.value,pack.pack_count,pack.pack_quantity FROM stock_movements m JOIN batches b ON b.id=m.batch_id JOIN product_types p ON p.id=b.product_id LEFT JOIN locations src ON src.id=m.from_location_id LEFT JOIN locations dst ON dst.id=m.to_location_id LEFT JOIN movement_packaging pack ON pack.movement_id=m.id LEFT JOIN product_identifiers ident ON ident.id=pack.identifier_id WHERE (m.from_location_id IS NULL OR EXISTS(SELECT 1 FROM role_assignments a WHERE a.site_id=src.site_id AND a.user_id=? AND a.role IN ('viewer','helper','material_manager'))) AND (m.to_location_id IS NULL OR EXISTS(SELECT 1 FROM role_assignments a WHERE a.site_id=dst.site_id AND a.user_id=? AND a.role IN ('viewer','helper','material_manager'))) ORDER BY m.created_at DESC,m.id DESC LIMIT ? OFFSET ?`, user.ID, user.ID, limit+1, offset)
		if err != nil {
			sendStockError(w, err)
			return
		}
		defer rows.Close()
		items := []movement{}
		for rows.Next() {
			var m movement
			if err = rows.Scan(&m.ID, &m.Kind, &m.Batch, &m.Product, &m.Lot, &m.Unit, &m.From, &m.To, &m.FromName, &m.ToName, &m.Quantity, &m.State, &m.Created, &m.Note, &m.Reverses, &m.ReversedBy, &m.Counted, &m.Basis, &m.BasisVersion, &m.IdentifierID, &m.IdentifierKind, &m.IdentifierValue, &m.PackCount, &m.PackQuantity); err != nil {
				sendStockError(w, err)
				return
			}
			items = append(items, m)
		}
		if err = rows.Err(); err != nil {
			sendStockError(w, err)
			return
		}
		more := len(items) > limit
		if more {
			items = items[:limit]
		}
		w.Header().Set("X-Has-More", fmt.Sprint(more))
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(items)
	})
}
