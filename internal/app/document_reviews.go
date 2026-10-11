package app

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

type documentReviewInput struct {
	ExpectedVersion int    `json:"expected_version"`
	SHA256          string `json:"sha256"`
	Action          string `json:"action"`
	Evidence        string `json:"evidence"`
	Qualification   string `json:"qualification"`
	Assignment      string `json:"assignment"`
	Note            string `json:"note"`
}
type documentScan struct {
	Result       string `json:"result"`
	Engine       string `json:"engine"`
	SignaturesAt string `json:"signatures_at,omitempty"`
}
type documentReview struct {
	ID         string              `json:"id"`
	DocumentID string              `json:"document_id"`
	Version    int                 `json:"version"`
	ActorID    string              `json:"actor_id"`
	Action     string              `json:"action"`
	Status     string              `json:"status"`
	CreatedAt  string              `json:"created_at"`
	Input      documentReviewInput `json:"input"`
	Scan       *documentScan       `json:"scan,omitempty"`
}

// clamd has no authentication: use only an operator-controlled private endpoint.
// Unknown replies, stale signatures and all connection failures remain unverified.
func scanDocument(ctx context.Context, address string, content []byte, now time.Time) documentScan {
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	result := documentScan{Result: "unverified"}
	if address == "" {
		return result
	}
	exchange := func(command string, stream bool) (string, error) {
		c, err := (&net.Dialer{Timeout: 3 * time.Second}).DialContext(ctx, "tcp", address)
		if err != nil {
			return "", err
		}
		defer c.Close()
		deadline := time.Now().Add(10 * time.Second)
		if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
			deadline = d
		}
		c.SetDeadline(deadline)
		if _, err = io.WriteString(c, "z"+command+"\x00"); err != nil {
			return "", err
		}
		if stream {
			for offset := 0; offset < len(content); {
				end := offset + 64*1024
				if end > len(content) {
					end = len(content)
				}
				if err = binary.Write(c, binary.BigEndian, uint32(end-offset)); err != nil {
					return "", err
				}
				if _, err = c.Write(content[offset:end]); err != nil {
					return "", err
				}
				offset = end
			}
			if err = binary.Write(c, binary.BigEndian, uint32(0)); err != nil {
				return "", err
			}
		}
		response, err := bufio.NewReader(io.LimitReader(c, 1025)).ReadString(0)
		if err != nil || len(response) > 1024 {
			return "", fmt.Errorf("invalid scanner response")
		}
		return strings.TrimSuffix(response, "\x00"), nil
	}
	version, err := exchange("VERSION", false)
	if err != nil {
		return result
	}
	parts := strings.Split(version, "/")
	if len(parts) != 3 || !strings.HasPrefix(parts[0], "ClamAV ") {
		return result
	}
	stamp, err := time.Parse("Mon Jan _2 15:04:05 2006", parts[2])
	if err != nil || now.Sub(stamp) > 72*time.Hour || stamp.After(now.Add(time.Hour)) {
		return result
	}
	result.Engine = version
	result.SignaturesAt = stamp.UTC().Format(time.RFC3339)
	reply, err := exchange("INSTREAM", true)
	if err != nil {
		return result
	}
	if reply == "stream: OK" {
		result.Result = "clean"
	} else if strings.HasPrefix(reply, "stream: ") && strings.HasSuffix(reply, " FOUND") {
		result.Result = "infected"
	}
	return result
}
func currentDocumentStatus(ctx context.Context, tx *sql.Tx, id, device string) (string, error) {
	var status string
	err := tx.QueryRowContext(ctx, `SELECT status FROM effective_documents WHERE id=? AND device_id=?`, id, device).Scan(&status)
	return status, err
}
func (a *authService) reviewDocument(ctx context.Context, user, token, key, device, id string, in documentReviewInput) ([]byte, error) {
	if in.ExpectedVersion < 0 || len(in.SHA256) != 64 || !validReason(in.Evidence) || !validReason(in.Qualification) || !validReason(in.Assignment) || !validReason(in.Note) {
		return nil, adminInvalid("Dokumenthash, Version und vollständige Prüfbegründung erforderlich.")
	}
	if in.Action != "scan" && in.Action != "release" && in.Action != "reject" && in.Action != "quarantine" {
		return nil, adminInvalid("Unbekannte Dokumentprüfung.")
	}
	encoded, _ := json.Marshal(in)
	hash := digest("document_review:" + device + ":" + id + ":" + string(encoded))
	tx, err := a.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err = activeStockSession(ctx, tx, user, token, a.now()); err != nil {
		return nil, err
	}
	manager, err := usageManager(ctx, tx, user, device)
	if err != nil {
		return nil, err
	}
	if !manager {
		return nil, scopeError()
	}
	var sha string
	var content []byte
	var version int
	if err = tx.QueryRowContext(ctx, `SELECT sha256,content,review_version FROM effective_documents WHERE id=? AND device_id=?`, id, device).Scan(&sha, &content, &version); err == sql.ErrNoRows {
		return nil, scopeError()
	} else if err != nil {
		return nil, err
	}
	if err = rejectOtherKey(ctx, tx, user, key, "document_review_requests"); err != nil {
		return nil, err
	}
	var old, body string
	err = tx.QueryRowContext(ctx, `SELECT request_hash,response_json FROM document_review_requests WHERE user_id=? AND key=?`, user, key).Scan(&old, &body)
	if err == nil {
		if old != hash {
			return nil, conflict("idempotency_conflict", "Schlüssel gehört zu einer anderen Dokumentprüfung.")
		}
		return []byte(body), nil
	}
	if err != sql.ErrNoRows {
		return nil, err
	}
	if version != in.ExpectedVersion || sha != in.SHA256 {
		return nil, conflict("document_version_conflict", "Dokumentstand geändert; erneut laden und prüfen.")
	}
	if digest(string(content)) != sha {
		return nil, &stockError{503, "document_integrity", "Dokumentintegrität nicht bestätigt."}
	}
	if version >= 1000 {
		return nil, &stockError{413, "review_limit", "Mehr als 1000 Prüfereignisse; Vorgang nicht gespeichert."}
	}
	now := a.now()
	review := documentReview{ID: newID(), DocumentID: id, Version: version + 1, ActorID: user, Action: in.Action, Status: "quarantined", CreatedAt: now.UTC().Format(time.RFC3339Nano), Input: in}
	switch in.Action {
	case "scan":
		result := scanDocument(ctx, a.clamdAddress, content, now)
		review.Scan = &result
	case "reject":
		review.Status = "rejected"
	case "release":
		var payload string
		err = tx.QueryRowContext(ctx, `SELECT payload_json FROM document_reviews WHERE document_id=? AND version=? AND action='scan'`, id, version).Scan(&payload)
		if err == sql.ErrNoRows {
			return nil, conflict("scan_required", "Unmittelbar vorhergehende aktuelle Malwareprüfung erforderlich.")
		}
		if err != nil {
			return nil, err
		}
		var previous documentReview
		if err = json.Unmarshal([]byte(payload), &previous); err != nil {
			return nil, err
		}
		scanned, err := time.Parse(time.RFC3339Nano, previous.CreatedAt)
		if err != nil || previous.Scan == nil || previous.Scan.Result != "clean" || previous.Input.SHA256 != sha || now.Before(scanned) || now.Sub(scanned) > 24*time.Hour {
			return nil, conflict("scan_required", "Saubere Malwareprüfung dieses Dateihashes innerhalb von 24 Stunden erforderlich.")
		}
		signatures, err := time.Parse(time.RFC3339, previous.Scan.SignaturesAt)
		if err != nil || now.Sub(signatures) > 72*time.Hour {
			return nil, conflict("scan_required", "Malwaresignaturen nicht aktuell; erneut prüfen.")
		}
		review.Status = "released"
	}
	response, _ := json.Marshal(review)
	if _, err = tx.ExecContext(ctx, `INSERT INTO document_reviews VALUES(?,?,?,?,?,?,?,?)`, review.ID, id, review.Version, user, review.Action, review.Status, review.CreatedAt, string(response)); err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO document_review_outbox(event_id) VALUES(?)`, review.ID); err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO document_review_requests VALUES(?,?,?,?,?)`, user, key, hash, review.ID, string(response)); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return response, nil
}
func documentReviewRoutes(mux *http.ServeMux, a *authService) {
	mux.HandleFunc("POST /api/v1/devices/{id}/documents/{document}/reviews", func(w http.ResponseWriter, r *http.Request) {
		user, ok := writeAuthorized(a, w, r)
		if !ok {
			return
		}
		var in documentReviewInput
		if !mutationBody(w, r, &in, 16*1024) {
			return
		}
		select {
		case a.documentSlots <- struct{}{}:
			defer func() { <-a.documentSlots }()
		default:
			apiError(w, 429, "review_busy", "Dokumentprüfung ausgelastet; später erneut senden.")
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()
		cookie, _ := r.Cookie(a.cookieName())
		response, err := a.reviewDocument(ctx, user.ID, digest(cookie.Value), r.Header.Get("Idempotency-Key"), r.PathValue("id"), r.PathValue("document"), in)
		if err != nil {
			sendStockError(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(response)
	})
	mux.HandleFunc("GET /api/v1/devices/{id}/documents/{document}/reviews", func(w http.ResponseWriter, r *http.Request) {
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
		if _, err = currentDocumentStatus(r.Context(), tx, r.PathValue("document"), r.PathValue("id")); err != nil {
			sendStockError(w, scopeError())
			return
		}
		rows, err := tx.QueryContext(r.Context(), `SELECT payload_json FROM document_reviews WHERE document_id=? ORDER BY version DESC LIMIT 100`, r.PathValue("document"))
		if err != nil {
			sendStockError(w, err)
			return
		}
		defer rows.Close()
		result := []json.RawMessage{}
		for rows.Next() {
			var payload string
			if err = rows.Scan(&payload); err != nil {
				sendStockError(w, err)
				return
			}
			result = append(result, json.RawMessage(payload))
		}
		if err = rows.Err(); err != nil {
			sendStockError(w, err)
			return
		}
		json.NewEncoder(w).Encode(result)
	})
}
