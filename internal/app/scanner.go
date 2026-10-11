package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	qrcode "github.com/skip2/go-qrcode"
)

type productIdentifier struct {
	ID           string `json:"id"`
	ProductID    string `json:"product_id"`
	Product      string `json:"product"`
	Unit         string `json:"unit"`
	Kind         string `json:"kind"`
	Value        string `json:"value"`
	PackQuantity int64  `json:"pack_quantity"`
	Active       bool   `json:"active"`
	Created      string `json:"created_at"`
}
type identifierRequest struct {
	ProductID    string `json:"product_id"`
	Kind         string `json:"kind"`
	Value        string `json:"value"`
	PackQuantity int64  `json:"pack_quantity"`
	Note         string `json:"note"`
}
type scanBatch struct {
	ID      string  `json:"id"`
	Lot     string  `json:"lot"`
	Expiry  *string `json:"expires_on"`
	Blocked bool    `json:"blocked"`
}
type scanResult struct {
	Status       string      `json:"status"`
	Parsed       barcodeData `json:"parsed"`
	ProductID    string      `json:"product_id,omitempty"`
	Product      string      `json:"product,omitempty"`
	Unit         string      `json:"unit,omitempty"`
	PackQuantity int64       `json:"pack_quantity,omitempty"`
	IdentifierID string      `json:"identifier_id,omitempty"`
	Batch        *scanBatch  `json:"batch,omitempty"`
	Location     *Location   `json:"location,omitempty"`
	Warnings     []string    `json:"warnings"`
	AsOf         string      `json:"data_as_of"`
}

func catalogRole(ctx context.Context, tx *sql.Tx, user string, write bool) error {
	var exists bool
	err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM role_assignments WHERE user_id=? AND (role='material_manager' OR (?=0 AND role IN ('viewer','helper'))))`, user, write).Scan(&exists)
	if err != nil {
		return err
	}
	if !exists {
		return scopeError()
	}
	return nil
}
func normalizeIdentifier(kind, value string) (string, error) {
	if kind != "gtin" && kind != "pzn" && kind != "ref" {
		return "", &stockError{400, "invalid_identifier", "GTIN, PZN oder REF ausdrücklich angeben."}
	}
	parsed, err := parseBarcode(value, kind)
	if err != nil || parsed.Kind != kind {
		message := "Kennung passt nicht zum Format."
		if err != nil {
			message = err.Error()
		}
		return "", &stockError{400, "invalid_identifier", message}
	}
	return parsed.Value, nil
}
func catalogReplay(ctx context.Context, tx *sql.Tx, user, key, hash string) ([]byte, error) {
	if err := rejectOtherKey(ctx, tx, user, key, "catalog_requests"); err != nil {
		return nil, err
	}
	var old, response string
	err := tx.QueryRowContext(ctx, `SELECT request_hash,response_json FROM catalog_requests WHERE user_id=? AND key=?`, user, key).Scan(&old, &response)
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
func saveCatalogEvent(ctx context.Context, tx *sql.Tx, user, key, hash, action, note string, item productIdentifier, now time.Time) ([]byte, error) {
	id := newID()
	encoded, err := json.Marshal(item)
	if err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO catalog_events VALUES(?,?,?,?,?,?,?)`, id, user, action, item.ID, now.UTC().Format(time.RFC3339Nano), note, string(encoded)); err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO catalog_outbox(event_id) VALUES(?)`, id); err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO catalog_requests VALUES(?,?,?,?,?)`, user, key, hash, id, string(encoded)); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return encoded, nil
}
func addIdentifier(ctx context.Context, db *sql.DB, user, token, key string, input identifierRequest, now time.Time) ([]byte, error) {
	value, err := normalizeIdentifier(input.Kind, input.Value)
	if err != nil {
		return nil, err
	}
	input.Value = value
	if input.ProductID == "" || len(input.ProductID) > 100 || input.PackQuantity < 1 || input.PackQuantity > 1_000_000_000 || !validReason(input.Note) {
		return nil, &stockError{400, "invalid_request", "Artikel, explizite Packungsmenge und Begründung erforderlich."}
	}
	canonical, _ := json.Marshal(input)
	hash := digest("add_identifier:" + string(canonical))
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err = activeStockSession(ctx, tx, user, token, now); err != nil {
		return nil, err
	}
	if err = catalogRole(ctx, tx, user, true); err != nil {
		return nil, err
	}
	if replay, err := catalogReplay(ctx, tx, user, key, hash); err != nil || replay != nil {
		return replay, err
	}
	item := productIdentifier{ID: newID(), ProductID: input.ProductID, Kind: input.Kind, Value: value, PackQuantity: input.PackQuantity, Active: true, Created: now.UTC().Format(time.RFC3339Nano)}
	if err = tx.QueryRowContext(ctx, `SELECT name,base_unit FROM product_types WHERE id=?`, input.ProductID).Scan(&item.Product, &item.Unit); err == sql.ErrNoRows {
		return nil, &stockError{400, "invalid_product", "Artikel nicht gefunden."}
	} else if err != nil {
		return nil, err
	}
	var exists bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM product_identifiers WHERE kind=? AND value=? AND active=1)`, item.Kind, item.Value).Scan(&exists); err != nil {
		return nil, err
	}
	if exists {
		return nil, conflict("identifier_conflict", "Diese Kennung ist bereits zugeordnet. Falsche Zuordnung zuerst mit Begründung widerrufen.")
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO product_identifiers VALUES(?,?,?,?,?,1,?,?,?)`, item.ID, item.ProductID, item.Kind, item.Value, item.PackQuantity, user, item.Created, input.Note); err != nil {
		return nil, err
	}
	return saveCatalogEvent(ctx, tx, user, key, hash, "identifier_added", input.Note, item, now)
}
func revokeIdentifier(ctx context.Context, db *sql.DB, user, token, key, id, note string, now time.Time) ([]byte, error) {
	if !validReason(note) {
		return nil, &stockError{400, "invalid_request", "Widerruf benötigt eine Begründung."}
	}
	canonical, _ := json.Marshal(struct {
		ID   string
		Note string
	}{id, note})
	hash := digest("revoke_identifier:" + string(canonical))
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err = activeStockSession(ctx, tx, user, token, now); err != nil {
		return nil, err
	}
	if err = catalogRole(ctx, tx, user, true); err != nil {
		return nil, err
	}
	if replay, err := catalogReplay(ctx, tx, user, key, hash); err != nil || replay != nil {
		return replay, err
	}
	var item productIdentifier
	err = tx.QueryRowContext(ctx, `SELECT i.id,i.product_id,p.name,p.base_unit,i.kind,i.value,i.pack_quantity,i.active,i.created_at FROM product_identifiers i JOIN product_types p ON p.id=i.product_id WHERE i.id=?`, id).Scan(&item.ID, &item.ProductID, &item.Product, &item.Unit, &item.Kind, &item.Value, &item.PackQuantity, &item.Active, &item.Created)
	if err == sql.ErrNoRows {
		return nil, scopeError()
	}
	if err != nil {
		return nil, err
	}
	if !item.Active {
		return nil, conflict("already_revoked", "Die Kennung wurde bereits widerrufen.")
	}
	if _, err = tx.ExecContext(ctx, `UPDATE product_identifiers SET active=0 WHERE id=?`, id); err != nil {
		return nil, err
	}
	item.Active = false
	return saveCatalogEvent(ctx, tx, user, key, hash, "identifier_revoked", note, item, now)
}
func resolveBarcode(ctx context.Context, db *sql.DB, user, text, format string, now time.Time) (scanResult, error) {
	result := scanResult{Status: "unknown", Warnings: []string{}, AsOf: now.UTC().Format(time.RFC3339Nano)}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return result, err
	}
	defer tx.Rollback()
	if err = catalogRole(ctx, tx, user, false); err != nil {
		return result, err
	}
	parsed, err := parseBarcode(text, format)
	result.Parsed = parsed
	result.Warnings = append(result.Warnings, parsed.Warnings...)
	if err != nil {
		result.Status = "invalid"
		result.Warnings = append(result.Warnings, err.Error())
		return result, nil
	}
	if parsed.Kind == "ambiguous" {
		result.Status = "ambiguous"
		return result, nil
	}
	if parsed.Kind == "unknown" {
		return result, nil
	}
	if parsed.Kind == "internal" {
		switch parsed.ObjectKind {
		case "location":
			if err = packScope(ctx, tx, user, parsed.ObjectID, ""); err != nil {
				return result, err
			}
			var l Location
			err = tx.QueryRowContext(ctx, `SELECT l.id,l.name,s.name,o.name,l.site_id FROM locations l JOIN sites s ON s.id=l.site_id JOIN operators o ON o.id=s.operator_id WHERE l.id=?`, parsed.ObjectID).Scan(&l.ID, &l.Name, &l.Site, &l.Operator, &l.SiteID)
			if err == sql.ErrNoRows {
				return result, scopeError()
			}
			if err != nil {
				return result, err
			}
			result.Location = &l
			result.Status = "location_match"
			return result, nil
		case "product":
			result.ProductID = parsed.ObjectID
		case "batch":
			b := scanBatch{}
			err = tx.QueryRowContext(ctx, `SELECT id,product_id,lot,expires_on,blocked FROM effective_batches WHERE id=?`, parsed.ObjectID).Scan(&b.ID, &result.ProductID, &b.Lot, &b.Expiry, &b.Blocked)
			if err == sql.ErrNoRows {
				return result, scopeError()
			}
			if err != nil {
				return result, err
			}
			result.Batch = &b
		}
	} else {
		kind := parsed.Kind
		if kind == "gs1" {
			kind = "gtin"
		}
		err = tx.QueryRowContext(ctx, `SELECT id,product_id,pack_quantity FROM product_identifiers WHERE kind=? AND value=? AND active=1`, kind, parsed.Value).Scan(&result.IdentifierID, &result.ProductID, &result.PackQuantity)
		if err == sql.ErrNoRows {
			result.Status = "unregistered_identifier"
			result.Warnings = append(result.Warnings, "Kennung noch keinem Artikel zugeordnet; keine externe Produktdatenbank wird abgefragt.")
			return result, nil
		}
		if err != nil {
			return result, err
		}
	}
	if err = tx.QueryRowContext(ctx, `SELECT name,base_unit FROM product_types WHERE id=?`, result.ProductID).Scan(&result.Product, &result.Unit); err == sql.ErrNoRows {
		return result, scopeError()
	} else if err != nil {
		return result, err
	}
	result.Status = "product_match"
	if parsed.Lot != "" {
		b := scanBatch{}
		err = tx.QueryRowContext(ctx, `SELECT id,lot,expires_on,blocked FROM effective_batches WHERE product_id=? AND lot=?`, result.ProductID, parsed.Lot).Scan(&b.ID, &b.Lot, &b.Expiry, &b.Blocked)
		if err == sql.ErrNoRows {
			result.Status = "unregistered_batch"
			result.Warnings = append(result.Warnings, "Charge noch nicht angelegt; Verfall muss vor Anlage fachlich bestätigt werden.")
		} else if err != nil {
			return result, err
		} else {
			result.Batch = &b
		}
	}
	if result.Batch != nil {
		result.Status = "batch_match"
		b := result.Batch
		if parsed.ExpiryRaw != "" {
			if parsed.ExpiryPrecision != "day" || b.Expiry == nil || !validDay(*b.Expiry) {
				result.Status = "requires_review"
				result.Warnings = append(result.Warnings, "Gescannter Verfall kann nicht sicher mit dem hinterlegten Grenztag abgeglichen werden.")
			} else {
				date, _ := time.Parse("2006-01-02", *b.Expiry)
				// Match against the registered full date; never guess the century from YY.
				if date.Format("060102") != parsed.ExpiryRaw {
					result.Status = "batch_mismatch"
					result.Warnings = append(result.Warnings, "Gescannter Verfall widerspricht dem hinterlegten Chargendatum.")
				}
			}
		}
		if b.Blocked {
			result.Warnings = append(result.Warnings, "Charge ist gesperrt; Scan hebt die Sperre nicht auf.")
		}
		if b.Expiry == nil || !validDay(*b.Expiry) {
			result.Warnings = append(result.Warnings, "Hinterlegter Verfall ungeklärt.")
		} else if *b.Expiry < localDay(now) {
			result.Warnings = append(result.Warnings, "Charge ist abgelaufen.")
		}
	}
	if parsed.Serial != "" || parsed.ExpiryPrecision == "month" || len(parsed.Warnings) > 0 && parsed.Kind == "gs1" {
		if result.Status != "batch_mismatch" {
			result.Status = "requires_review"
		}
	}
	if result.Batch == nil {
		result.Warnings = append(result.Warnings, "Artikelkennung bestimmt allein keine Charge, keinen Verfall und keine Basismenge ohne hinterlegte Packungsumrechnung.")
	}
	result.Warnings = append(result.Warnings, "Scanergebnis vor Übernahme prüfen; es erfolgt keine automatische Buchung oder Freigabe.")
	return result, nil
}
func internalCode(kind, id string) (string, error) {
	if kind != "location" && kind != "product" && kind != "batch" || !opaqueID.MatchString(id) {
		return "", &stockError{400, "invalid_label", "Objektart oder ID ungültig."}
	}
	return "drk:v1:" + kind + ":" + id, nil
}
func scannerRoutes(mux *http.ServeMux, a *authService) {
	mux.HandleFunc("POST /api/v1/scanner/resolve", func(w http.ResponseWriter, r *http.Request) {
		user, ok := writeAuthorized(a, w, r)
		if !ok {
			return
		}
		if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
			apiError(w, 415, "content_type", "JSON erforderlich.")
			return
		}
		var input struct {
			Text   string `json:"text"`
			Format string `json:"format"`
		}
		r.Body = http.MaxBytesReader(w, r.Body, 4096)
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		if decoder.Decode(&input) != nil || decoder.Decode(&struct{}{}) != io.EOF || len(input.Text) < 1 || len(input.Text) > 1024 {
			apiError(w, 400, "invalid_request", "Begrenzten Scannertext und optionales Format angeben.")
			return
		}
		result, err := resolveBarcode(r.Context(), a.db, user.ID, input.Text, input.Format, a.now())
		if err != nil {
			sendStockError(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(result)
	})
	mux.HandleFunc("POST /api/v1/product-identifiers", func(w http.ResponseWriter, r *http.Request) {
		user, ok := writeAuthorized(a, w, r)
		if !ok {
			return
		}
		var input identifierRequest
		if !mutationBody(w, r, &input, 4096) {
			return
		}
		cookie, _ := r.Cookie(a.cookieName())
		body, err := addIdentifier(r.Context(), a.db, user.ID, digest(cookie.Value), r.Header.Get("Idempotency-Key"), input, a.now())
		packResponse(w, body, err, 201)
	})
	mux.HandleFunc("POST /api/v1/product-identifiers/{id}/revoke", func(w http.ResponseWriter, r *http.Request) {
		user, ok := writeAuthorized(a, w, r)
		if !ok {
			return
		}
		var input struct {
			Note string `json:"note"`
		}
		if !mutationBody(w, r, &input, 4096) {
			return
		}
		cookie, _ := r.Cookie(a.cookieName())
		body, err := revokeIdentifier(r.Context(), a.db, user.ID, digest(cookie.Value), r.Header.Get("Idempotency-Key"), r.PathValue("id"), input.Note, a.now())
		packResponse(w, body, err, 201)
	})
	mux.HandleFunc("GET /api/v1/product-identifiers", func(w http.ResponseWriter, r *http.Request) {
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
		if err = catalogRole(r.Context(), tx, user.ID, false); err != nil {
			sendStockError(w, err)
			return
		}
		rows, err := tx.QueryContext(r.Context(), `SELECT i.id,i.product_id,p.name,p.base_unit,i.kind,i.value,i.pack_quantity,i.active,i.created_at FROM product_identifiers i JOIN product_types p ON p.id=i.product_id WHERE (?='' OR i.product_id=?) ORDER BY i.rowid DESC LIMIT 501`, r.URL.Query().Get("product_id"), r.URL.Query().Get("product_id"))
		if err != nil {
			sendStockError(w, err)
			return
		}
		defer rows.Close()
		items := []productIdentifier{}
		for rows.Next() {
			var item productIdentifier
			if err = rows.Scan(&item.ID, &item.ProductID, &item.Product, &item.Unit, &item.Kind, &item.Value, &item.PackQuantity, &item.Active, &item.Created); err != nil {
				sendStockError(w, err)
				return
			}
			items = append(items, item)
		}
		if err = rows.Err(); err != nil {
			sendStockError(w, err)
			return
		}
		if len(items) > 500 {
			w.Header().Set("X-Has-More", "true")
			items = items[:500]
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(items)
	})
	mux.HandleFunc("GET /api/v1/labels/{kind}/{id}", func(w http.ResponseWriter, r *http.Request) {
		user, err := a.session(r)
		if err != nil {
			a.sessionError(w, err)
			return
		}
		code, err := internalCode(r.PathValue("kind"), r.PathValue("id"))
		if err != nil {
			sendStockError(w, err)
			return
		}
		result, err := resolveBarcode(r.Context(), a.db, user.ID, code, "internal", a.now())
		if err != nil {
			sendStockError(w, err)
			return
		}
		if result.Status == "invalid" {
			apiError(w, 400, "invalid_label", "Ungültige Kennung.")
			return
		}
		png, err := qrcode.Encode(code, qrcode.Medium, 256)
		if err != nil {
			sendStockError(w, err)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("Content-Disposition", `inline; filename="drk-`+r.PathValue("kind")+`-`+r.PathValue("id")+`.png"`)
		w.Header().Set("Content-Length", strconv.Itoa(len(png)))
		w.Write(png)
	})
}
