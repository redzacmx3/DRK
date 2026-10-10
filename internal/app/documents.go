package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"mime"
	"net/http"
	"path/filepath"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const maxDocument = 4 * 1024 * 1024

type documentInput struct {
	ExpectedVersion int    `json:"expected_version"`
	Filename        string `json:"filename"`
	MediaType       string `json:"media_type"`
	Content         []byte `json:"content"` // JSON base64, bounded before decoding.
	Note            string `json:"note"`
}
type documentRecord struct {
	ID        string `json:"id"`
	DeviceID  string `json:"device_id"`
	Filename  string `json:"filename"`
	MediaType string `json:"media_type"`
	Size      int    `json:"size"`
	SHA256    string `json:"sha256"`
	CreatedBy string `json:"created_by"`
	CreatedAt string `json:"created_at"`
	Note      string `json:"note"`
	Status    string `json:"status"`
}

func validateDocument(input documentInput) error {
	bad := func() error {
		return &stockError{400, "invalid_document", "PDF, PNG, JPEG oder UTF-8-Text mit passendem Dateinamen und Inhalt erforderlich (maximal 4 MiB)."}
	}
	if input.ExpectedVersion < 0 || !validReason(input.Note) || input.Filename == "" || len(input.Filename) > 160 || strings.TrimSpace(input.Filename) != input.Filename || strings.ContainsAny(input.Filename, "/\\") || strings.IndexFunc(input.Filename, unicode.IsControl) >= 0 || len(input.Content) == 0 || len(input.Content) > maxDocument {
		return bad()
	}
	ext := strings.ToLower(filepath.Ext(input.Filename))
	switch input.MediaType {
	case "application/pdf":
		if ext != ".pdf" || !bytes.HasPrefix(input.Content, []byte("%PDF-")) || !bytes.HasSuffix(bytes.TrimSpace(input.Content), []byte("%%EOF")) {
			return bad()
		}
	case "image/png", "image/jpeg":
		if input.MediaType == "image/png" && ext != ".png" || input.MediaType == "image/jpeg" && ext != ".jpg" && ext != ".jpeg" {
			return bad()
		}
		config, format, err := image.DecodeConfig(bytes.NewReader(input.Content))
		if err != nil || config.Width < 1 || config.Height < 1 || int64(config.Width)*int64(config.Height) > 16_000_000 || "image/"+format != input.MediaType {
			return bad()
		}
		if _, _, err = image.Decode(bytes.NewReader(input.Content)); err != nil {
			return bad()
		}
	case "text/plain":
		if ext != ".txt" || !utf8.Valid(input.Content) || bytes.IndexFunc(input.Content, func(r rune) bool { return unicode.IsControl(r) && r != '\n' && r != '\r' && r != '\t' }) >= 0 {
			return bad()
		}
	default:
		return bad()
	}
	return nil
}
func uploadDocument(ctx context.Context, db *sql.DB, user, token, key, id string, input documentInput, now time.Time) ([]byte, error) {
	if err := validateDocument(input); err != nil {
		return nil, err
	}
	checksum := sha256.Sum256(input.Content)
	sha := hex.EncodeToString(checksum[:])
	inputForHash := input
	inputForHash.Content = nil
	canonical, _ := json.Marshal(inputForHash)
	hash := digest("document:" + id + ":" + sha + ":" + string(canonical))
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err = activeStockSession(ctx, tx, user, token, now); err != nil {
		return nil, err
	}
	var allowed bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM devices d JOIN locations l ON l.id=d.location_id JOIN role_assignments a ON a.site_id=l.site_id WHERE d.id=? AND a.user_id=? AND a.role='device_manager')`, id, user).Scan(&allowed); err != nil {
		return nil, err
	}
	if !allowed {
		return nil, scopeError()
	}
	if err = rejectOtherKey(ctx, tx, user, key, "document_requests"); err != nil {
		return nil, err
	}
	var old, body string
	err = tx.QueryRowContext(ctx, `SELECT request_hash,response_json FROM document_requests WHERE user_id=? AND key=?`, user, key).Scan(&old, &body)
	if err == nil {
		if old != hash {
			return nil, conflict("idempotency_conflict", "Schlüssel gehört zu einem anderen Dokument.")
		}
		return []byte(body), nil
	}
	if err != sql.ErrNoRows {
		return nil, err
	}
	device, err := readDevice(ctx, tx, user, id, now)
	if err != nil {
		return nil, err
	}
	if device.Version != input.ExpectedVersion {
		return nil, conflict("device_version_conflict", "Geräteakte wurde geändert; erneut prüfen.")
	}
	record := documentRecord{newID(), id, input.Filename, input.MediaType, len(input.Content), sha, user, now.UTC().Format(time.RFC3339Nano), input.Note, "quarantined"}
	encoded, _ := json.Marshal(record)
	if _, err = tx.ExecContext(ctx, `INSERT INTO device_documents VALUES(?,?,?,?,?,?,?,?,?,?,?)`, record.ID, id, record.Filename, record.MediaType, record.Size, sha, input.Content, user, record.CreatedAt, record.Note, record.Status); err != nil {
		return nil, err
	}
	event := newID()
	if _, err = tx.ExecContext(ctx, `INSERT INTO document_events VALUES(?,?,?,?,?)`, event, record.ID, user, record.CreatedAt, string(encoded)); err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO document_outbox(event_id) VALUES(?)`, event); err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO document_requests VALUES(?,?,?,?,?)`, user, key, hash, event, string(encoded)); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return encoded, nil
}
func documentRoutes(mux *http.ServeMux, a *authService) {
	mux.HandleFunc("POST /api/v1/devices/{id}/documents", func(w http.ResponseWriter, r *http.Request) {
		user, ok := writeAuthorized(a, w, r)
		if !ok {
			return
		}
		var input documentInput
		if !mutationBody(w, r, &input, 6*1024*1024) {
			return
		}
		cookie, _ := r.Cookie(cookieName)
		body, err := uploadDocument(r.Context(), a.db, user.ID, digest(cookie.Value), r.Header.Get("Idempotency-Key"), r.PathValue("id"), input, a.now())
		if err != nil {
			sendStockError(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(body)
	})
	mux.HandleFunc("GET /api/v1/devices/{id}/documents", func(w http.ResponseWriter, r *http.Request) {
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
		if _, err = readDevice(r.Context(), tx, user.ID, r.PathValue("id"), a.now()); err != nil {
			sendStockError(w, err)
			return
		}
		rows, err := tx.QueryContext(r.Context(), `SELECT id,device_id,filename,media_type,size,sha256,created_by,created_at,note,status FROM device_documents WHERE device_id=? ORDER BY rowid DESC LIMIT 501`, r.PathValue("id"))
		if err != nil {
			sendStockError(w, err)
			return
		}
		defer rows.Close()
		records := []documentRecord{}
		for rows.Next() {
			var record documentRecord
			if err = rows.Scan(&record.ID, &record.DeviceID, &record.Filename, &record.MediaType, &record.Size, &record.SHA256, &record.CreatedBy, &record.CreatedAt, &record.Note, &record.Status); err != nil {
				sendStockError(w, err)
				return
			}
			records = append(records, record)
		}
		if err = rows.Err(); err != nil {
			sendStockError(w, err)
			return
		}
		if len(records) > 500 {
			records = records[:500]
			w.Header().Set("X-Has-More", "true")
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(records)
	})
	mux.HandleFunc("GET /api/v1/devices/{id}/documents/{document}/download", func(w http.ResponseWriter, r *http.Request) {
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
		var allowed bool
		if err = tx.QueryRowContext(r.Context(), `SELECT EXISTS(SELECT 1 FROM devices d JOIN locations l ON l.id=d.location_id JOIN role_assignments a ON a.site_id=l.site_id WHERE d.id=? AND a.user_id=? AND a.role='device_manager')`, r.PathValue("id"), user.ID).Scan(&allowed); err != nil {
			sendStockError(w, err)
			return
		}
		if !allowed {
			sendStockError(w, scopeError())
			return
		}
		if r.URL.Query().Get("review") != "true" {
			apiError(w, 409, "document_quarantined", "Dokument ist ungeprüft. Nur ausdrücklicher Prüfdownload für Geräteverantwortliche verfügbar.")
			return
		}
		var filename, sha string
		var content []byte
		err = tx.QueryRowContext(r.Context(), `SELECT filename,sha256,content FROM device_documents WHERE id=? AND device_id=?`, r.PathValue("document"), r.PathValue("id")).Scan(&filename, &sha, &content)
		if err == sql.ErrNoRows {
			sendStockError(w, scopeError())
			return
		}
		if err != nil {
			sendStockError(w, err)
			return
		}
		sum := sha256.Sum256(content)
		if hex.EncodeToString(sum[:]) != sha {
			apiError(w, 503, "document_integrity", "Dokumentintegrität konnte nicht bestätigt werden.")
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": filename}))
		w.Header().Set("Content-Security-Policy", "sandbox; default-src 'none'")
		w.Header().Set("X-Document-Status", "quarantined")
		w.Write(content)
	})
}
