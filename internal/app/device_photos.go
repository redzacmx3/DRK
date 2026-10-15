package app

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"image"
	"image/png"
	"net/http"
	"time"
)

type devicePhotoInput struct {
	ExpectedVersion int    `json:"expected_version"`
	Action          string `json:"action"`
	Content         []byte `json:"content"`
	Note            string `json:"note"`
}
type devicePhoto struct {
	ID        string `json:"id"`
	DeviceID  string `json:"device_id"`
	Version   int    `json:"version"`
	Action    string `json:"action"`
	ActorID   string `json:"actor_id"`
	CreatedAt string `json:"created_at"`
	Note      string `json:"note"`
	SHA256    string `json:"sha256"`
}

func photoHistory(ctx context.Context, tx *sql.Tx, id string) ([]devicePhoto, error) {
	rows, err := tx.QueryContext(ctx, `SELECT id,device_id,version,action,actor_id,created_at,note,sha256 FROM device_photo_events WHERE device_id=? ORDER BY version DESC`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []devicePhoto{}
	for rows.Next() {
		var p devicePhoto
		if err = rows.Scan(&p.ID, &p.DeviceID, &p.Version, &p.Action, &p.ActorID, &p.CreatedAt, &p.Note, &p.SHA256); err != nil {
			return nil, err
		}
		result = append(result, p)
	}
	return result, rows.Err()
}
func normalizePhoto(input devicePhotoInput) ([]byte, error) {
	bad := func() ([]byte, error) {
		return nil, &stockError{400, "invalid_photo", "PNG oder JPEG, höchstens 4 MiB und 6 Millionen Pixel; Begründung und gültige Fotoversion erforderlich."}
	}
	if input.ExpectedVersion < 0 || !validReason(input.Note) {
		return bad()
	}
	if input.Action == "remove" {
		if len(input.Content) != 0 {
			return bad()
		}
		return []byte{}, nil
	}
	if input.Action != "set" || len(input.Content) == 0 || len(input.Content) > maxDocument {
		return bad()
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(input.Content))
	if err != nil || (format != "png" && format != "jpeg") || config.Width < 1 || config.Height < 1 || config.Width > 6000 || config.Height > 6000 || int64(config.Width)*int64(config.Height) > 6_000_000 {
		return bad()
	}
	decoded, _, err := image.Decode(bytes.NewReader(input.Content))
	if err != nil {
		return bad()
	}
	// Encode only decoded pixels: no SVG, metadata, scripts or original file bytes
	// are served. This is an identification image, never a released document.
	var out bytes.Buffer
	if err = png.Encode(&out, decoded); err != nil || out.Len() > maxDocument {
		return bad()
	}
	return out.Bytes(), nil
}
func saveDevicePhoto(ctx context.Context, db *sql.DB, user, token, key, id string, input devicePhotoInput, now time.Time) ([]byte, error) {
	content, err := normalizePhoto(input)
	if err != nil {
		return nil, err
	}
	canonical, _ := json.Marshal(input)
	hash := digest("recognition_photo:" + id + ":" + string(canonical))
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err = activeStockSession(ctx, tx, user, token, now); err != nil {
		return nil, err
	}
	if err = requireModule(ctx, tx, "inventory"); err != nil {
		return nil, err
	}
	var allowed bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM devices d JOIN locations l ON l.id=d.location_id JOIN role_assignments a ON a.site_id=l.site_id WHERE d.id=? AND a.user_id=? AND a.role='device_manager')`, id, user).Scan(&allowed); err != nil {
		return nil, err
	}
	if !allowed {
		return nil, scopeError()
	}
	if err = rejectOtherKey(ctx, tx, user, key, "device_photo_requests"); err != nil {
		return nil, err
	}
	var old, response string
	err = tx.QueryRowContext(ctx, `SELECT request_hash,response_json FROM device_photo_requests WHERE user_id=? AND key=?`, user, key).Scan(&old, &response)
	if err == nil {
		if old != hash {
			return nil, conflict("idempotency_conflict", "Schlüssel gehört zu einem anderen Fotovorgang.")
		}
		return []byte(response), nil
	}
	if err != sql.ErrNoRows {
		return nil, err
	}
	var version int
	if err = tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(version),0) FROM device_photo_events WHERE device_id=?`, id).Scan(&version); err != nil {
		return nil, err
	}
	if version != input.ExpectedVersion {
		return nil, conflict("photo_version_conflict", "Foto wurde zwischenzeitlich geändert; neu laden und abgleichen.")
	}
	if input.Action == "remove" && version == 0 {
		return nil, conflict("photo_missing", "Kein Foto vorhanden.")
	}
	p := devicePhoto{ID: newID(), DeviceID: id, Version: version + 1, Action: input.Action, ActorID: user, CreatedAt: now.UTC().Format(time.RFC3339Nano), Note: input.Note}
	if input.Action == "set" {
		p.SHA256 = digest(string(content))
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO device_photo_events VALUES(?,?,?,?,?,?,?,?,?)`, p.ID, id, p.Version, p.Action, user, p.CreatedAt, p.Note, p.SHA256, content); err != nil {
		return nil, err
	}
	body, _ := json.Marshal(p)
	if _, err = tx.ExecContext(ctx, `INSERT INTO device_photo_requests VALUES(?,?,?,?,?)`, user, key, hash, p.ID, string(body)); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return body, nil
}
func devicePhotoRoutes(mux *http.ServeMux, a *authService) {
	mux.HandleFunc("POST /api/v1/devices/{id}/photo", func(w http.ResponseWriter, r *http.Request) {
		user, ok := writeAuthorized(a, w, r)
		if !ok {
			return
		}
		var input devicePhotoInput
		if !mutationBody(w, r, &input, 6<<20) {
			return
		}
		cookie, _ := r.Cookie(a.cookieName())
		body, err := saveDevicePhoto(r.Context(), a.db, user.ID, digest(cookie.Value), r.Header.Get("Idempotency-Key"), r.PathValue("id"), input, a.now())
		if err != nil {
			sendStockError(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(body)
	})
	for _, route := range []string{"GET /api/v1/devices/{id}/photo", "GET /api/v1/devices/{id}/photo/content"} {
		mux.HandleFunc(route, func(w http.ResponseWriter, r *http.Request) {
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
			history, err := photoHistory(r.Context(), tx, r.PathValue("id"))
			if err != nil {
				sendStockError(w, err)
				return
			}
			if r.Pattern == "GET /api/v1/devices/{id}/photo" {
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(history)
				return
			}
			if len(history) == 0 || history[0].Action != "set" {
				apiError(w, 404, "photo_missing", "Kein aktuelles Foto vorhanden.")
				return
			}
			var content []byte
			if err = tx.QueryRowContext(r.Context(), `SELECT content FROM device_photo_events WHERE id=?`, history[0].ID).Scan(&content); err != nil {
				sendStockError(w, err)
				return
			}
			w.Header().Set("Content-Type", "image/png")
			w.Header().Set("Content-Disposition", `inline; filename="inventarfoto.png"`)
			w.Write(content)
		})
	}
}
