package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"math"
	"net/http"
	"sort"
	"strings"
	"time"
)

type packItem struct {
	ProductID string `json:"product_id"`
	Quantity  int64  `json:"quantity"`
	MinDays   int    `json:"min_remaining_days"`
}
type publishPackRequest struct {
	Devices         []packDevice `json:"devices,omitempty"`
	Location        string       `json:"location_id"`
	ExpectedVersion int          `json:"expected_version"`
	Title           string       `json:"title"`
	ValidHours      int          `json:"check_valid_hours"`
	Note            string       `json:"note"`
	Items           []packItem   `json:"items"`
}
type packVersion struct {
	Devices    []packDevice `json:"devices,omitempty"`
	ID         string       `json:"id"`
	Location   string       `json:"location_id"`
	Version    int          `json:"version"`
	Title      string       `json:"title"`
	ValidHours int          `json:"check_valid_hours"`
	Created    string       `json:"created_at"`
	Note       string       `json:"note"`
	Items      []packItem   `json:"items"`
}
type materialItem struct {
	packItem
	Product   string `json:"product"`
	Unit      string `json:"unit"`
	Physical  int64  `json:"physical_quantity"`
	Usable    int64  `json:"usable_quantity"`
	Shortfall int64  `json:"shortfall"`
	Unknown   int64  `json:"unknown_quantity"`
	Excluded  int64  `json:"excluded_quantity"`
}
type materialBasisRow struct {
	Batch    string  `json:"batch_id"`
	Product  string  `json:"product_id"`
	State    string  `json:"state"`
	Quantity int64   `json:"quantity"`
	Version  int64   `json:"version"`
	Expiry   *string `json:"expires_on"`
	Blocked  bool    `json:"blocked"`
}
type materialSnapshot struct {
	Pack  packVersion        `json:"packlist"`
	Day   string             `json:"day"`
	Rows  []materialBasisRow `json:"balances"`
	Items []materialItem     `json:"items"`
}
type checkProof struct {
	ID         string `json:"id"`
	User       string `json:"username"`
	Created    string `json:"created_at"`
	ValidUntil string `json:"valid_until"`
	Basis      string `json:"basis_hash"`
	Passed     bool   `json:"physical_passed"`
	Note       string `json:"note"`
	Current    bool   `json:"current"`
}
type materialReadiness struct {
	Scope          string         `json:"scope"`
	Status         string         `json:"status"`
	Overall        string         `json:"overall_readiness"`
	Reasons        []string       `json:"reasons"`
	AsOf           string         `json:"data_as_of"`
	Basis          string         `json:"basis_hash"`
	CurrentVersion bool           `json:"current_version"`
	Pack           packVersion    `json:"packlist"`
	Items          []materialItem `json:"items"`
	Check          *checkProof    `json:"last_check"`
}
type checkPackRequest struct {
	Basis  string `json:"basis_hash"`
	Passed *bool  `json:"physical_passed"`
	Note   string `json:"note"`
}

func packScope(ctx context.Context, tx *sql.Tx, user, location string, write string) error {
	roles := []string{"viewer", "helper", "material_manager"}
	if write == "publish" {
		roles = []string{"material_manager", "material_manager", "material_manager"}
	}
	if write == "check" {
		roles = []string{"helper", "material_manager", "material_manager"}
	}
	var exists bool
	err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM locations l JOIN role_assignments a ON a.site_id=l.site_id WHERE l.id=? AND a.user_id=? AND a.role IN (?,?,?))`, location, user, roles[0], roles[1], roles[2]).Scan(&exists)
	if err != nil {
		return err
	}
	if !exists {
		return scopeError()
	}
	return nil
}
func loadPack(ctx context.Context, tx *sql.Tx, user, id, write string) (packVersion, error) {
	var p packVersion
	err := tx.QueryRowContext(ctx, `SELECT id,location_id,version,title,check_valid_hours,created_at,note FROM packlist_versions WHERE id=?`, id).Scan(&p.ID, &p.Location, &p.Version, &p.Title, &p.ValidHours, &p.Created, &p.Note)
	if err == sql.ErrNoRows {
		return p, scopeError()
	}
	if err != nil {
		return p, err
	}
	if err = packScope(ctx, tx, user, p.Location, write); err != nil {
		return p, err
	}
	p.Items = []packItem{}
	rows, err := tx.QueryContext(ctx, `SELECT product_id,quantity,min_remaining_days FROM packlist_items WHERE packlist_id=? ORDER BY product_id`, id)
	if err != nil {
		return p, err
	}
	defer rows.Close()
	for rows.Next() {
		var item packItem
		if err = rows.Scan(&item.ProductID, &item.Quantity, &item.MinDays); err != nil {
			return p, err
		}
		p.Items = append(p.Items, item)
	}
	if err = rows.Err(); err != nil {
		return p, err
	}
	rows.Close()
	p.Devices, err = loadPackDevices(ctx, tx, p.ID)
	return p, err
}

// The day and every relevant balance version are part of the physical check basis.
// Even a movement subsequently reversed makes an earlier check stale.
func calculateMaterial(ctx context.Context, tx *sql.Tx, p packVersion, now time.Time) (materialReadiness, materialSnapshot, error) {
	snapshot := materialSnapshot{Pack: p, Day: localDay(now), Rows: []materialBasisRow{}, Items: []materialItem{}}
	result := materialReadiness{Scope: "material_only", Status: "needs_check", Overall: "not_assessed", Reasons: []string{}, AsOf: now.UTC().Format(time.RFC3339Nano), Pack: p}
	var latest int
	if err := tx.QueryRowContext(ctx, `SELECT MAX(version) FROM packlist_versions WHERE location_id=?`, p.Location).Scan(&latest); err != nil {
		return result, snapshot, err
	}
	result.CurrentVersion = latest == p.Version
	for _, item := range p.Items {
		m := materialItem{packItem: item}
		if err := tx.QueryRowContext(ctx, `SELECT name,base_unit FROM product_types WHERE id=?`, item.ProductID).Scan(&m.Product, &m.Unit); err != nil {
			return result, snapshot, err
		}
		rows, err := tx.QueryContext(ctx, `SELECT b.batch_id,c.product_id,b.state,b.quantity,b.version,c.expires_on,c.blocked FROM stock_balances b JOIN batches c ON c.id=b.batch_id WHERE b.location_id=? AND c.product_id=? ORDER BY b.batch_id,b.state`, p.Location, item.ProductID)
		if err != nil {
			return result, snapshot, err
		}
		day, _ := time.Parse("2006-01-02", snapshot.Day)
		threshold := day.AddDate(0, 0, item.MinDays).Format("2006-01-02")
		for rows.Next() {
			var row materialBasisRow
			if err = rows.Scan(&row.Batch, &row.Product, &row.State, &row.Quantity, &row.Version, &row.Expiry, &row.Blocked); err != nil {
				rows.Close()
				return result, snapshot, err
			}
			snapshot.Rows = append(snapshot.Rows, row)
			if row.Quantity > math.MaxInt64-m.Physical {
				rows.Close()
				return result, snapshot, conflict("quantity_overflow", "Gesamtmenge außerhalb des Wertebereichs.")
			}
			m.Physical += row.Quantity
			if row.State != "available" || row.Blocked {
				m.Excluded += row.Quantity
			} else if row.Expiry == nil || !validDay(*row.Expiry) {
				m.Unknown += row.Quantity
			} else if *row.Expiry < threshold {
				m.Excluded += row.Quantity
			} else {
				m.Usable += row.Quantity
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return result, snapshot, err
		}
		if m.Usable < item.Quantity {
			m.Shortfall = item.Quantity - m.Usable
		}
		if m.Shortfall > 0 {
			result.Reasons = append(result.Reasons, m.Product+": nutzbarer Sollbestand unterschritten.")
			result.Status = "shortfall"
		}
		// Unknown critical data must not produce an unjustified green, even if other lots meet the target.
		if m.Unknown > 0 {
			result.Reasons = append(result.Reasons, m.Product+": Verfallsdaten ungeklärt.")
			if result.Status != "shortfall" {
				result.Status = "unknown"
			}
		}
		snapshot.Items = append(snapshot.Items, m)
	}
	result.Items = snapshot.Items
	encoded, _ := json.Marshal(snapshot)
	result.Basis = digest(string(encoded))
	var proof checkProof
	err := tx.QueryRowContext(ctx, `SELECT c.id,u.username,c.created_at,c.basis_hash,c.physical_passed,c.note FROM readiness_checks c JOIN users u ON u.id=c.user_id WHERE c.packlist_id=? ORDER BY c.rowid DESC LIMIT 1`, p.ID).Scan(&proof.ID, &proof.User, &proof.Created, &proof.Basis, &proof.Passed, &proof.Note)
	if err != nil && err != sql.ErrNoRows {
		return result, snapshot, err
	}
	if err == nil {
		stamp, e := time.Parse(time.RFC3339Nano, proof.Created)
		if e != nil {
			return result, snapshot, e
		}
		expires := stamp.Add(time.Duration(p.ValidHours) * time.Hour)
		proof.ValidUntil = expires.UTC().Format(time.RFC3339Nano)
		proof.Current = proof.Basis == result.Basis && !now.Before(stamp) && now.Before(expires) && result.CurrentVersion
		result.Check = &proof
		if !proof.Current {
			result.Reasons = append(result.Reasons, "Physischer Prüfnachweis veraltet; erneut prüfen.")
		} else if !proof.Passed {
			result.Reasons = append(result.Reasons, "Physische Prüfung hat eine Abweichung festgestellt.")
			result.Status = "physical_failed"
		}
	}
	if result.Check == nil {
		result.Reasons = append(result.Reasons, "Aktueller physischer Prüfnachweis fehlt.")
	}
	if result.Status == "needs_check" && result.Check != nil && result.Check.Current && result.Check.Passed {
		result.Status = "ready"
	}
	if !result.CurrentVersion {
		result.Status = "obsolete"
		result.Reasons = append(result.Reasons, "Diese Sollversion wurde durch eine neuere Version ersetzt.")
	}
	return result, snapshot, nil
}
func packReplay(ctx context.Context, tx *sql.Tx, user, key, hash string) ([]byte, error) {
	if err := rejectOtherKey(ctx, tx, user, key, "packlist_requests"); err != nil {
		return nil, err
	}
	var old, response string
	err := tx.QueryRowContext(ctx, `SELECT request_hash,response_json FROM packlist_requests WHERE user_id=? AND key=?`, user, key).Scan(&old, &response)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if old != hash {
		return nil, conflict("idempotency_conflict", "Dieser Schlüssel gehört zu einem anderen Vorgang.")
	}
	return []byte(response), nil
}

func savePackEvent(ctx context.Context, tx *sql.Tx, user, key, hash, action, pack, check string, value any, now time.Time) ([]byte, error) {
	id := newID()
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO packlist_events VALUES(?,?,?,?,?,?)`, id, user, action, pack, nullableString(check), now.UTC().Format(time.RFC3339Nano)); err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO packlist_outbox(event_id) VALUES(?)`, id); err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO packlist_requests VALUES(?,?,?,?,?)`, user, key, hash, id, string(encoded)); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return encoded, nil
}
func publishPack(ctx context.Context, db *sql.DB, user, token, key string, input publishPackRequest, now time.Time) ([]byte, error) {
	if strings.TrimSpace(input.Title) == "" || len(input.Title) > 160 || len(input.Location) > 100 || input.ExpectedVersion < 0 || input.ValidHours < 1 || input.ValidHours > 168 || !validReason(input.Note) || len(input.Items) < 1 || len(input.Items) > 100 {
		return nil, &stockError{400, "invalid_request", "Titel, Begründung, Prüfgültigkeit und 1–100 Sollpositionen erforderlich."}
	}
	sort.Slice(input.Items, func(i, j int) bool { return input.Items[i].ProductID < input.Items[j].ProductID })
	for i, item := range input.Items {
		if item.ProductID == "" || len(item.ProductID) > 100 || item.Quantity < 1 || item.Quantity > 1_000_000_000 || item.MinDays < 0 || item.MinDays > 3650 || i > 0 && input.Items[i-1].ProductID == item.ProductID {
			return nil, &stockError{400, "invalid_request", "Sollpositionen müssen eindeutig sein; Menge und Restlaufzeit prüfen."}
		}
	}
	if err := validatePackDevices(&input); err != nil {
		return nil, err
	}
	canonical, _ := json.Marshal(input)
	hash := digest("publish_pack:" + string(canonical))
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err = activeStockSession(ctx, tx, user, token, now); err != nil {
		return nil, err
	}
	if err = packScope(ctx, tx, user, input.Location, "publish"); err != nil {
		return nil, err
	}
	if replay, err := packReplay(ctx, tx, user, key, hash); err != nil || replay != nil {
		return replay, err
	}
	var latest int
	if err = tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(version),0) FROM packlist_versions WHERE location_id=?`, input.Location).Scan(&latest); err != nil {
		return nil, err
	}
	if latest != input.ExpectedVersion {
		return nil, conflict("version_conflict", "Die Sollpackliste wurde inzwischen geändert; erneut laden.")
	}
	p := packVersion{ID: newID(), Location: input.Location, Version: latest + 1, Title: input.Title, ValidHours: input.ValidHours, Created: now.UTC().Format(time.RFC3339Nano), Note: input.Note, Items: input.Items, Devices: input.Devices}
	if _, err = tx.ExecContext(ctx, `INSERT INTO packlist_versions VALUES(?,?,?,?,?,?,?,?)`, p.ID, p.Location, p.Version, p.Title, p.ValidHours, user, p.Created, p.Note); err != nil {
		return nil, err
	}
	for _, item := range input.Items {
		var exists bool
		if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM product_types WHERE id=?)`, item.ProductID).Scan(&exists); err != nil {
			return nil, err
		}
		if !exists {
			return nil, &stockError{400, "invalid_product", "Artikel nicht gefunden."}
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO packlist_items VALUES(?,?,?,?)`, p.ID, item.ProductID, item.Quantity, item.MinDays); err != nil {
			return nil, err
		}
	}
	if err = savePackDevices(ctx, tx, p); err != nil {
		return nil, err
	}
	return savePackEvent(ctx, tx, user, key, hash, "packlist_published", p.ID, "", p, now)
}
func recordMaterialCheck(ctx context.Context, db *sql.DB, user, token, key, id string, input checkPackRequest, now time.Time) ([]byte, error) {
	if len(input.Basis) != 64 || input.Passed == nil || !validReason(input.Note) {
		return nil, &stockError{400, "invalid_request", "Datenbasis, ausdrückliches Prüfergebnis und Begründung erforderlich."}
	}
	canonical, _ := json.Marshal(input)
	hash := digest("material_check:" + id + ":" + string(canonical))
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err = activeStockSession(ctx, tx, user, token, now); err != nil {
		return nil, err
	}
	p, err := loadPack(ctx, tx, user, id, "check")
	if err != nil {
		return nil, err
	}
	if replay, err := packReplay(ctx, tx, user, key, hash); err != nil || replay != nil {
		return replay, err
	}
	result, snapshot, err := calculateMaterial(ctx, tx, p, now)
	if err != nil {
		return nil, err
	}
	if !result.CurrentVersion || result.Basis != input.Basis {
		return nil, conflict("basis_conflict", "Sollversion oder Bestand hat sich geändert; neu laden und physisch erneut prüfen.")
	}
	encoded, _ := json.Marshal(snapshot)
	check := newID()
	if _, err = tx.ExecContext(ctx, `INSERT INTO readiness_checks VALUES(?,?,?,?,?,?,?,?)`, check, p.ID, user, now.UTC().Format(time.RFC3339Nano), input.Basis, string(encoded), *input.Passed, input.Note); err != nil {
		return nil, err
	}
	// Reading the result in the same transaction includes the new physical proof.
	result, _, err = calculateMaterial(ctx, tx, p, now)
	if err != nil {
		return nil, err
	}
	return savePackEvent(ctx, tx, user, key, hash, "material_checked", p.ID, check, result, now)
}
func packlistRoutes(mux *http.ServeMux, a *authService) {
	packAssessmentRoutes(mux, a)
	mux.HandleFunc("POST /api/v1/packlists", func(w http.ResponseWriter, r *http.Request) {
		user, ok := writeAuthorized(a, w, r)
		if !ok {
			return
		}
		var input publishPackRequest
		if !mutationBody(w, r, &input, 32768) {
			return
		}
		cookie, _ := r.Cookie(cookieName)
		body, err := publishPack(r.Context(), a.db, user.ID, digest(cookie.Value), r.Header.Get("Idempotency-Key"), input, a.now())
		packResponse(w, body, err, 201)
	})
	mux.HandleFunc("POST /api/v1/packlists/{id}/checks", func(w http.ResponseWriter, r *http.Request) {
		user, ok := writeAuthorized(a, w, r)
		if !ok {
			return
		}
		var input checkPackRequest
		if !mutationBody(w, r, &input, 32768) {
			return
		}
		cookie, _ := r.Cookie(cookieName)
		body, err := recordMaterialCheck(r.Context(), a.db, user.ID, digest(cookie.Value), r.Header.Get("Idempotency-Key"), r.PathValue("id"), input, a.now())
		packResponse(w, body, err, 201)
	})
	mux.HandleFunc("GET /api/v1/packlists", func(w http.ResponseWriter, r *http.Request) {
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
		history := r.URL.Query().Get("history")
		if history != "" && history != "true" {
			apiError(w, 400, "invalid_request", "history muss true sein.")
			return
		}
		rows, err := tx.QueryContext(r.Context(), `SELECT p.id FROM packlist_versions p JOIN locations l ON l.id=p.location_id WHERE (? OR p.version=(SELECT MAX(v.version) FROM packlist_versions v WHERE v.location_id=p.location_id)) AND EXISTS(SELECT 1 FROM role_assignments a WHERE a.site_id=l.site_id AND a.user_id=? AND a.role IN ('viewer','helper','material_manager')) ORDER BY p.location_id,p.version DESC LIMIT 101`, history == "true", user.ID)
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
		if len(ids) > 100 {
			w.Header().Set("X-Has-More", "true")
			ids = ids[:100]
		}
		packs := []packVersion{}
		for _, id := range ids {
			p, e := loadPack(r.Context(), tx, user.ID, id, "")
			if e != nil {
				sendStockError(w, e)
				return
			}
			packs = append(packs, p)
		}
		body, _ := json.Marshal(packs)
		packResponse(w, body, nil, 200)
	})
	mux.HandleFunc("GET /api/v1/packlists/{id}/checks", func(w http.ResponseWriter, r *http.Request) {
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
		_, err = loadPack(r.Context(), tx, user.ID, r.PathValue("id"), "")
		if err != nil {
			sendStockError(w, err)
			return
		}
		type storedProof struct {
			ID       string          `json:"id"`
			User     string          `json:"username"`
			Created  string          `json:"created_at"`
			Passed   bool            `json:"physical_passed"`
			Note     string          `json:"note"`
			Basis    string          `json:"basis_hash"`
			Snapshot json.RawMessage `json:"snapshot"`
		}
		rows, err := tx.QueryContext(r.Context(), `SELECT c.id,u.username,c.created_at,c.physical_passed,c.note,c.basis_hash,c.snapshot_json FROM readiness_checks c JOIN users u ON u.id=c.user_id WHERE c.packlist_id=? ORDER BY c.rowid DESC LIMIT 51`, r.PathValue("id"))
		if err != nil {
			sendStockError(w, err)
			return
		}
		defer rows.Close()
		proofs := []storedProof{}
		for rows.Next() {
			var p storedProof
			var snapshot string
			if err = rows.Scan(&p.ID, &p.User, &p.Created, &p.Passed, &p.Note, &p.Basis, &snapshot); err != nil {
				sendStockError(w, err)
				return
			}
			p.Snapshot = json.RawMessage(snapshot)
			proofs = append(proofs, p)
		}
		if err = rows.Err(); err != nil {
			sendStockError(w, err)
			return
		}
		if len(proofs) > 50 {
			w.Header().Set("X-Has-More", "true")
			proofs = proofs[:50]
		}
		body, _ := json.Marshal(proofs)
		packResponse(w, body, nil, 200)
	})

	for _, path := range []string{"GET /api/v1/packlists/{id}", "GET /api/v1/packlists/{id}/readiness"} {
		mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
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
			p, err := loadPack(r.Context(), tx, user.ID, r.PathValue("id"), "")
			if err != nil {
				sendStockError(w, err)
				return
			}
			var value any = p
			if strings.HasSuffix(r.URL.Path, "/readiness") {
				result, _, e := calculateMaterial(r.Context(), tx, p, a.now())
				if e != nil {
					sendStockError(w, e)
					return
				}
				value = result
			}
			body, _ := json.Marshal(value)
			packResponse(w, body, nil, 200)
		})
	}
}
func packResponse(w http.ResponseWriter, body []byte, err error, status int) {
	if err != nil {
		sendStockError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	w.Write(body)
}
