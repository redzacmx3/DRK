package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"
)

type recallSelector struct {
	Kind         string `json:"kind"`
	ProductID    string `json:"product_id"`
	BatchID      string `json:"batch_id"`
	Manufacturer string `json:"manufacturer"`
	Model        string `json:"model"`
	SerialExact  string `json:"serial_exact"`
	SerialFrom   string `json:"serial_from"`
	SerialTo     string `json:"serial_to"`
}
type recallRecord struct {
	recallSelector
	ID        string `json:"id"`
	Title     string `json:"title"`
	Reference string `json:"reference"`
	Version   int    `json:"version"`
	Status    string `json:"status"`
	CreatedAt string `json:"created_at"`
}
type recallTarget struct {
	SiteID     string `json:"site_id"`
	Key        string `json:"key"`
	LocationID string `json:"location_id"`
	Location   string `json:"location"`
	ID         string `json:"id"`
	Name       string `json:"name"`
	Identity   string `json:"identity"`
	State      string `json:"state"`
	Quantity   int64  `json:"quantity"`
	Version    int64  `json:"version"`
	Basis      string `json:"basis_hash"`
	Completed  bool   `json:"completed"`
}
type recallInput struct {
	PreviewBasis    string          `json:"preview_basis"`
	Action          string          `json:"action"`
	ID              string          `json:"id"`
	ExpectedVersion int             `json:"expected_version"`
	Selector        *recallSelector `json:"selector"`
	Title           string          `json:"title"`
	Reference       string          `json:"reference"`
	TargetKey       string          `json:"target_key"`
	Basis           string          `json:"basis_hash"`
	Evidence        string          `json:"evidence"`
	Note            string          `json:"note"`
}

func recallRole(kind string) string {
	if kind == "device" {
		return "device_manager"
	}
	return "material_manager"
}
func recallGlobal(ctx context.Context, tx *sql.Tx, user, kind string) (bool, error) {
	var ok bool
	err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM administrators a JOIN users u ON u.id=a.user_id WHERE a.user_id=? AND u.active=1) AND EXISTS(SELECT 1 FROM role_assignments WHERE user_id=? AND role=?)`, user, user, recallRole(kind)).Scan(&ok)
	return ok, err
}
func validateRecallSelector(ctx context.Context, tx *sql.Tx, s recallSelector) error {
	bad := func() error {
		return &stockError{400, "invalid_recall", "Material: ausdrückliches Produkt und optional passende Charge. Geräte: exakter Hersteller/Bauart und optional exakte Kennung oder gleich langer numerischer Serienbereich."}
	}
	if s.Kind == "material" {
		if s.ProductID == "" || s.Manufacturer != "" || s.Model != "" || s.SerialExact != "" || s.SerialFrom != "" || s.SerialTo != "" {
			return bad()
		}
		var ok bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM product_types WHERE id=?) AND (?='' OR EXISTS(SELECT 1 FROM batches WHERE id=? AND product_id=?))`, s.ProductID, s.BatchID, s.BatchID, s.ProductID).Scan(&ok); err != nil {
			return err
		}
		if !ok {
			return bad()
		}
		return nil
	}
	if s.Kind != "device" || s.ProductID != "" || s.BatchID != "" || strings.TrimSpace(s.Manufacturer) == "" || strings.TrimSpace(s.Model) == "" {
		return bad()
	}
	for _, v := range []string{s.Manufacturer, s.Model, s.SerialExact, s.SerialFrom, s.SerialTo} {
		if strings.TrimSpace(v) != v || len(v) > 160 {
			return bad()
		}
	}
	if s.SerialFrom != "" || s.SerialTo != "" {
		if s.SerialExact != "" || len(s.SerialFrom) == 0 || len(s.SerialFrom) != len(s.SerialTo) || s.SerialFrom > s.SerialTo {
			return bad()
		}
		for _, v := range []string{s.SerialFrom, s.SerialTo} {
			for _, c := range v {
				if c < '0' || c > '9' {
					return bad()
				}
			}
		}
	}
	return nil
}

const recallDeviceMatch = `r.kind='device' AND r.manufacturer=? AND r.model=? AND (r.serial_exact='' OR r.serial_exact=?) AND (r.serial_from='' OR (length(?)=length(r.serial_from) AND ? NOT GLOB '*[^0-9]*' AND ? BETWEEN r.serial_from AND r.serial_to))`

func projectRecalls(ctx context.Context, tx *sql.Tx, record *deviceRecord) error {
	rows, err := tx.QueryContext(ctx, `SELECT r.id,r.title,s.status,s.version FROM recalls r JOIN recall_state s ON s.recall_id=r.id WHERE `+recallDeviceMatch+` ORDER BY r.id`, record.Manufacturer, record.Model, record.SerialOrLot, record.SerialOrLot, record.SerialOrLot, record.SerialOrLot)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id, title, status string
		var version int
		if err = rows.Scan(&id, &title, &status, &version); err != nil {
			return err
		}
		record.RecallRevision += int64(version)
		if status == "OPEN" {
			record.ActiveBlocks = append(record.ActiveBlocks, deviceBlock{ID: "recall:" + id, Reason: "recall", Version: version, Note: title})
			record.Reasons = append(record.Reasons, "Aktiver zentraler Rückruf: "+title)
		}
	}
	return rows.Err()
}
func readRecall(ctx context.Context, tx *sql.Tx, id string) (recallRecord, error) {
	var r recallRecord
	var product, batch sql.NullString
	err := tx.QueryRowContext(ctx, `SELECT r.id,r.kind,r.title,r.reference,r.product_id,r.batch_id,r.manufacturer,r.model,r.serial_exact,r.serial_from,r.serial_to,r.created_at,s.version,s.status FROM recalls r JOIN recall_state s ON s.recall_id=r.id WHERE r.id=?`, id).Scan(&r.ID, &r.Kind, &r.Title, &r.Reference, &product, &batch, &r.Manufacturer, &r.Model, &r.SerialExact, &r.SerialFrom, &r.SerialTo, &r.CreatedAt, &r.Version, &r.Status)
	r.ProductID = product.String
	r.BatchID = batch.String
	if errors.Is(err, sql.ErrNoRows) {
		err = scopeError()
	}
	return r, err
}
func recallTargets(ctx context.Context, tx *sql.Tx, user string, r recallRecord, global bool) ([]recallTarget, error) {
	var rows *sql.Rows
	var err error
	scope := `(? OR EXISTS(SELECT 1 FROM role_assignments a WHERE a.site_id=l.site_id AND a.user_id=? AND a.role IN ('viewer','helper','material_manager','device_manager')))`
	if r.Kind == "material" {
		rows, err = tx.QueryContext(ctx, `SELECT b.batch_id||'/'||b.location_id||'/'||b.state,l.id,l.name,l.site_id,b.batch_id,p.name,c.lot,b.state,b.quantity,b.version FROM stock_balances b JOIN batches c ON c.id=b.batch_id JOIN product_types p ON p.id=c.product_id JOIN locations l ON l.id=b.location_id WHERE b.quantity>0 AND c.product_id=? AND (?='' OR c.id=?) AND `+scope+` ORDER BY b.batch_id,l.id,b.state LIMIT 2001`, r.ProductID, r.BatchID, r.BatchID, global, user)
	} else {
		rows, err = tx.QueryContext(ctx, `SELECT d.id,l.id,l.name,l.site_id,d.id,d.name,d.serial_or_lot,'device',1,COALESCE((SELECT MAX(version) FROM device_control_events WHERE device_id=d.id),0) FROM (SELECT d.id,d.location_id,COALESCE(json_extract(e.payload_json,'$.details.name'),d.name) AS name,COALESCE(json_extract(e.payload_json,'$.details.manufacturer'),d.manufacturer) AS manufacturer,COALESCE(json_extract(e.payload_json,'$.details.model'),d.model) AS model,COALESCE(json_extract(e.payload_json,'$.details.serial_or_lot'),d.serial_or_lot) AS serial_or_lot FROM devices d LEFT JOIN device_control_events e ON e.device_id=d.id AND e.version=(SELECT MAX(version) FROM device_control_events WHERE device_id=d.id AND action='details')) d JOIN locations l ON l.id=d.location_id WHERE d.manufacturer=? AND d.model=? AND (?='' OR d.serial_or_lot=?) AND (?='' OR (length(d.serial_or_lot)=length(?) AND d.serial_or_lot NOT GLOB '*[^0-9]*' AND d.serial_or_lot BETWEEN ? AND ?)) AND `+scope+` ORDER BY d.id LIMIT 2001`, r.Manufacturer, r.Model, r.SerialExact, r.SerialExact, r.SerialFrom, r.SerialFrom, r.SerialFrom, r.SerialTo, global, user)
	}
	if err != nil {
		return nil, err
	}
	targets := []recallTarget{}
	for rows.Next() {
		var t recallTarget
		if err = rows.Scan(&t.Key, &t.LocationID, &t.Location, &t.SiteID, &t.ID, &t.Name, &t.Identity, &t.State, &t.Quantity, &t.Version); err != nil {
			rows.Close()
			return nil, err
		}
		basis, _ := json.Marshal(struct {
			Key, Location, Identity string
			Quantity, Version       int64
		}{t.Key, t.LocationID, t.Identity, t.Quantity, t.Version})
		t.Basis = digest(string(basis))
		targets = append(targets, t)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	if len(targets) > 2000 {
		return nil, &stockError{413, "recall_limit", "Mehr als 2000 Treffer; der Vorgang wird vollständig abgelehnt."}
	}
	for i := range targets {
		var basis string
		err = tx.QueryRowContext(ctx, `SELECT basis_hash FROM recall_actions WHERE recall_id=? AND target_key=?`, r.ID, targets[i].Key).Scan(&basis)
		if err != nil && err != sql.ErrNoRows {
			return nil, err
		}
		targets[i].Completed = err == nil && basis == targets[i].Basis
	}
	return targets, nil
}
func writeRecallEvent(ctx context.Context, tx *sql.Tx, user, action, location string, r recallRecord, payload any) (string, error) {
	id := newID()
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	var loc any
	if location != "" {
		loc = location
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO recall_events VALUES(?,?,?,?,?,?,?,?)`, id, r.ID, r.Version, user, action, loc, time.Now().UTC().Format(time.RFC3339Nano), string(raw))
	return id, err
}
func changeRecall(ctx context.Context, a *authService, user, token, key string, in recallInput) ([]byte, error) {
	tx, err := a.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err = activeStockSession(ctx, tx, user, token, a.now()); err != nil {
		return nil, err
	}
	var record recallRecord
	if in.Action == "create" {
		if in.Selector == nil {
			return nil, adminInvalid("Ausdrücklicher Rückrufbereich erforderlich.")
		}
		record.recallSelector = *in.Selector
		if err = validateRecallSelector(ctx, tx, record.recallSelector); err != nil {
			return nil, err
		}
	} else {
		if in.ID == "" {
			return nil, adminInvalid("Rückruf-ID erforderlich.")
		}
		record, err = readRecall(ctx, tx, in.ID)
		if err != nil {
			return nil, err
		}
	}
	if err = requireModule(ctx, tx, "recalls"); err != nil {
		return nil, err
	}
	global, err := recallGlobal(ctx, tx, user, record.Kind)
	if err != nil {
		return nil, err
	}
	location := ""
	if in.Action == "complete" {
		targets, err := recallTargets(ctx, tx, user, record, false)
		if err != nil {
			return nil, err
		}
		for _, t := range targets {
			if t.Key == in.TargetKey {
				location = t.LocationID
			}
		}
		if location == "" {
			err = tx.QueryRowContext(ctx, `SELECT location_id FROM recall_events WHERE recall_id=? AND action='complete' AND json_extract(payload_json,'$.input.target_key')=? ORDER BY version DESC LIMIT 1`, record.ID, in.TargetKey).Scan(&location)
			if err != nil && err != sql.ErrNoRows {
				return nil, err
			}
		}
		if location == "" {
			return nil, scopeError()
		}
		var allowed bool
		if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM locations l JOIN role_assignments a ON a.site_id=l.site_id WHERE l.id=? AND a.user_id=? AND a.role=?)`, location, user, recallRole(record.Kind)).Scan(&allowed); err != nil {
			return nil, err
		}
		if !allowed {
			return nil, scopeError()
		}
	} else if in.Action == "create" || in.Action == "close" {
		if !global {
			return nil, &stockError{403, "recall_coordinator_required", "Globale Rückrufkoordination benötigt Administratorrecht und die betreffende Fachrolle."}
		}
	} else {
		return nil, adminInvalid("Unbekannte Rückrufaktion.")
	}
	if err = rejectOtherKey(ctx, tx, user, key, "recall_requests"); err != nil {
		return nil, err
	}
	raw, _ := json.Marshal(in)
	hash := digest(string(raw))
	var previousHash, previous string
	err = tx.QueryRowContext(ctx, `SELECT request_hash,response_json FROM recall_requests WHERE user_id=? AND key=?`, user, key).Scan(&previousHash, &previous)
	if err == nil {
		if hash != previousHash {
			return nil, conflict("idempotency_conflict", "Schlüssel gehört zu einem anderen Rückrufvorgang.")
		}
		return []byte(previous), tx.Commit()
	}
	if err != sql.ErrNoRows {
		return nil, err
	}
	if !validReason(in.Note) || !validReason(in.Evidence) {
		return nil, adminInvalid("Begründung und Quellen-/Maßnahmen-/Abschlussnachweis erforderlich.")
	}
	if in.Action == "create" {
		if in.ID != "" || in.ExpectedVersion != 0 || in.TargetKey != "" || in.Basis != "" || strings.TrimSpace(in.Title) == "" || len(in.Title) > 200 || !validReason(in.Reference) {
			return nil, adminInvalid("Titel und Quellenreferenz erforderlich; keine fremden Aktionsfelder.")
		}
		record.ID = newID()
		record.Title = in.Title
		record.Reference = in.Reference
		record.Version = 1
		record.Status = "OPEN"
		record.CreatedAt = a.now().UTC().Format(time.RFC3339Nano)
		targets, e := recallTargets(ctx, tx, user, record, true)
		if e != nil {
			return nil, e
		}
		preview, _ := json.Marshal(targets)
		if in.PreviewBasis != digest(string(preview)) {
			return nil, conflict("recall_preview_changed", "Treffer geändert oder Vorschau fehlt. Erneut prüfen.")
		}
		var product, batch any
		if record.ProductID != "" {
			product = record.ProductID
		}
		if record.BatchID != "" {
			batch = record.BatchID
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO recalls VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`, record.ID, record.Kind, record.Title, record.Reference, product, batch, record.Manufacturer, record.Model, record.SerialExact, record.SerialFrom, record.SerialTo, user, record.CreatedAt); err != nil {
			return nil, err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO recall_state VALUES(?,1,'OPEN')`, record.ID); err != nil {
			return nil, err
		}
	} else {
		if in.Selector != nil || in.Title != "" || in.Reference != "" || in.PreviewBasis != "" {
			return nil, adminInvalid("Rückrufdefinition bleibt unverändert.")
		}
		if record.Status != "OPEN" || record.Version != in.ExpectedVersion {
			return nil, conflict("recall_changed", "Rückruf geändert oder bereits geschlossen. Neu laden und abgleichen.")
		}
		if in.Action == "complete" {
			targets, err := recallTargets(ctx, tx, user, record, false)
			if err != nil {
				return nil, err
			}
			found := false
			for _, target := range targets {
				if target.Key == in.TargetKey {
					found = true
				}
				if target.Key == in.TargetKey && target.Basis != in.Basis {
					return nil, conflict("recall_target_changed", "Bestand oder Geräteakte geändert. Neu laden und Maßnahmen prüfen.")
				}
			}
			if !found {
				return nil, conflict("recall_target_changed", "Treffer nicht mehr aktuell; neu laden.")
			}
		} else {
			if in.TargetKey != "" || in.Basis != "" {
				return nil, adminInvalid("Abschluss enthält keine Einzelmaßnahmenfelder.")
			}
			targets, err := recallTargets(ctx, tx, user, record, true)
			if err != nil {
				return nil, err
			}
			for _, target := range targets {
				if !target.Completed {
					return nil, conflict("recall_measures_open", "Mindestens ein aktueller Treffer hat keinen gültigen Maßnahmenabschluss.")
				}
			}
			record.Status = "CLOSED"
		}
		record.Version++
		if _, err = tx.ExecContext(ctx, `UPDATE recall_state SET version=?,status=? WHERE recall_id=?`, record.Version, record.Status, record.ID); err != nil {
			return nil, err
		}
	}
	snapshot, e := recallTargets(ctx, tx, user, record, in.Action != "complete")
	if e != nil {
		return nil, e
	}
	event, err := writeRecallEvent(ctx, tx, user, in.Action, location, record, struct {
		Input   recallInput    `json:"input"`
		Targets []recallTarget `json:"targets"`
	}{in, snapshot})
	if err != nil {
		return nil, err
	}
	if in.Action == "complete" {
		if _, err = tx.ExecContext(ctx, `INSERT INTO recall_actions VALUES(?,?,?,?) ON CONFLICT(recall_id,target_key) DO UPDATE SET basis_hash=excluded.basis_hash,event_id=excluded.event_id`, record.ID, in.TargetKey, in.Basis, event); err != nil {
			return nil, err
		}
	}
	response, err := json.Marshal(record)
	if err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO recall_requests VALUES(?,?,?,?,?)`, user, key, hash, event, string(response)); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return response, nil
}

type recallAudit struct {
	ID        string      `json:"id"`
	Version   int         `json:"version"`
	Actor     string      `json:"actor"`
	Action    string      `json:"action"`
	CreatedAt string      `json:"created_at"`
	Input     recallInput `json:"input"`
}

func recallAuditRows(ctx context.Context, tx *sql.Tx, user, id string, global bool) ([]recallAudit, bool, error) {
	rows, err := tx.QueryContext(ctx, `SELECT e.id,e.version,u.username,e.action,e.created_at,e.payload_json FROM recall_events e JOIN users u ON u.id=e.actor_id WHERE e.recall_id=? AND (e.location_id IS NULL OR ? OR EXISTS(SELECT 1 FROM locations l JOIN role_assignments a ON a.site_id=l.site_id WHERE l.id=e.location_id AND a.user_id=? AND a.role IN ('viewer','helper','material_manager','device_manager'))) ORDER BY e.version DESC LIMIT 101`, id, global, user)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	result := []recallAudit{}
	for rows.Next() {
		var e recallAudit
		var raw string
		if err = rows.Scan(&e.ID, &e.Version, &e.Actor, &e.Action, &e.CreatedAt, &raw); err != nil {
			return nil, false, err
		}
		var payload struct {
			Input recallInput `json:"input"`
		}
		if err = json.Unmarshal([]byte(raw), &payload); err != nil {
			return nil, false, err
		}
		e.Input = payload.Input
		result = append(result, e)
	}
	if err = rows.Err(); err != nil {
		return nil, false, err
	}
	more := len(result) > 100
	if more {
		result = result[:100]
	}
	return result, more, nil
}
func recallRoutes(mux *http.ServeMux, a *authService) {
	mux.HandleFunc("POST /api/v1/recalls/preview", func(w http.ResponseWriter, r *http.Request) {
		user, ok := writeAuthorized(a, w, r)
		if !ok {
			return
		}
		var in recallInput
		d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10))
		d.DisallowUnknownFields()
		if d.Decode(&in) != nil || d.Decode(new(any)) != io.EOF || in.Selector == nil {
			apiError(w, 400, "invalid_json", "Ausdrücklicher Rückrufbereich erforderlich.")
			return
		}
		tx, err := a.db.BeginTx(r.Context(), nil)
		if err != nil {
			sendStockError(w, err)
			return
		}
		defer tx.Rollback()
		cookie, _ := r.Cookie(a.cookieName())
		if err = activeStockSession(r.Context(), tx, user.ID, digest(cookie.Value), a.now()); err != nil {
			sendStockError(w, err)
			return
		}
		if err = validateRecallSelector(r.Context(), tx, *in.Selector); err != nil {
			sendStockError(w, err)
			return
		}
		global, err := recallGlobal(r.Context(), tx, user.ID, in.Selector.Kind)
		if err != nil {
			sendStockError(w, err)
			return
		}
		if !global {
			apiError(w, 403, "recall_coordinator_required", "Administratorrecht und Fachrolle erforderlich.")
			return
		}
		targets, err := recallTargets(r.Context(), tx, user.ID, recallRecord{recallSelector: *in.Selector}, true)
		if err != nil {
			sendStockError(w, err)
			return
		}
		raw, _ := json.Marshal(targets)
		if err = tx.Commit(); err != nil {
			sendStockError(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(struct {
			Basis   string         `json:"basis_hash"`
			Targets []recallTarget `json:"targets"`
		}{digest(string(raw)), targets})
	})

	mux.HandleFunc("POST /api/v1/recalls", func(w http.ResponseWriter, r *http.Request) {
		user, ok := writeAuthorized(a, w, r)
		if !ok {
			return
		}
		key := r.Header.Get("Idempotency-Key")
		if !validKey(key) {
			apiError(w, 400, "invalid_key", "Idempotenzschlüssel erforderlich.")
			return
		}
		var in recallInput
		d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10))
		d.DisallowUnknownFields()
		if d.Decode(&in) != nil || d.Decode(new(any)) != io.EOF {
			apiError(w, 400, "invalid_json", "Ungültige Rückrufdaten.")
			return
		}
		cookie, _ := r.Cookie(a.cookieName())
		body, err := changeRecall(r.Context(), a, user.ID, digest(cookie.Value), key, in)
		if err != nil {
			sendStockError(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(body)
	})
	mux.HandleFunc("GET /api/v1/recalls", func(w http.ResponseWriter, r *http.Request) {
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
		rows, err := tx.QueryContext(r.Context(), `SELECT id FROM recalls ORDER BY created_at DESC,id DESC LIMIT 501`)
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
		if len(ids) > 500 {
			apiError(w, 413, "recall_limit", "Mehr als 500 Rückrufe; Blätterfunktion erforderlich.")
			return
		}
		type overview struct {
			recallRecord
			Targets     []recallTarget `json:"targets"`
			Coordinator bool           `json:"coordinator"`
			Events      []recallAudit  `json:"events"`
			MoreEvents  bool           `json:"more_events"`
		}
		result := []overview{}
		totalTargets, totalEvents := 0, 0
		for _, id := range ids {
			record, err := readRecall(r.Context(), tx, id)
			if err != nil {
				sendStockError(w, err)
				return
			}
			global, err := recallGlobal(r.Context(), tx, user.ID, record.Kind)
			if err != nil {
				sendStockError(w, err)
				return
			}
			targets, err := recallTargets(r.Context(), tx, user.ID, record, global)
			if err != nil {
				sendStockError(w, err)
				return
			}
			if !global && len(targets) == 0 {
				var historic bool
				if err = tx.QueryRowContext(r.Context(), `SELECT EXISTS(SELECT 1 FROM recall_events e JOIN locations l ON l.id=e.location_id JOIN role_assignments a ON a.site_id=l.site_id WHERE e.recall_id=? AND a.user_id=? AND a.role IN ('viewer','helper','material_manager','device_manager'))`, record.ID, user.ID).Scan(&historic); err != nil {
					sendStockError(w, err)
					return
				}
				if !historic {
					continue
				}
			}
			events, more, err := recallAuditRows(r.Context(), tx, user.ID, record.ID, global)
			if err != nil {
				sendStockError(w, err)
				return
			}
			totalTargets += len(targets)
			totalEvents += len(events)
			if totalTargets > 2000 || totalEvents > 2000 {
				apiError(w, 413, "recall_limit", "Mehr als 2000 sichtbare Treffer oder Historieneinträge; Blätterfunktion erforderlich.")
				return
			}
			result = append(result, overview{record, targets, global, events, more})
		}
		if err = tx.Commit(); err != nil {
			sendStockError(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(result)
	})
}
