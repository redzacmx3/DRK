package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"sort"
	"time"
)

type devicePhysicalResult struct {
	DeviceID            string `json:"device_id"`
	Present             *bool  `json:"present"`
	Functional          *bool  `json:"functional"`
	AccessoriesComplete *bool  `json:"accessories_complete"`
}
type deviceCheckRequest struct {
	Basis   string                 `json:"basis_hash"`
	Results []devicePhysicalResult `json:"results"`
	Note    string                 `json:"note"`
}
type deviceCheckProof struct {
	checkProof
	Results []devicePhysicalResult `json:"results"`
}
type deviceCheckBasis struct {
	Pack          packVersion    `json:"packlist"`
	MaterialBasis string         `json:"material_basis"`
	Day           string         `json:"day"`
	Devices       []deviceRecord `json:"devices"`
}

func physicalDeviceBasis(ctx context.Context, tx *sql.Tx, user string, p packVersion, now time.Time) (string, deviceCheckBasis, error) {
	material, _, err := calculateMaterial(ctx, tx, p, now)
	if err != nil {
		return "", deviceCheckBasis{}, err
	}
	zone, _ := time.LoadLocation("Europe/Berlin")
	basis := deviceCheckBasis{Pack: p, MaterialBasis: material.Basis, Day: now.In(zone).Format("2006-01-02"), Devices: []deviceRecord{}}
	for _, required := range p.Devices {
		record, err := readDevice(ctx, tx, user, required.DeviceID, now)
		if err != nil {
			return "", basis, err
		}
		basis.Devices = append(basis.Devices, record)
	}
	encoded, err := json.Marshal(basis)
	return digest(string(encoded)), basis, err
}
func loadDeviceCheck(ctx context.Context, tx *sql.Tx, p packVersion, basis string, current bool, now time.Time) (*deviceCheckProof, error) {
	var proof deviceCheckProof
	var results string
	err := tx.QueryRowContext(ctx, `SELECT c.id,u.username,c.created_at,c.basis_hash,c.physical_passed,c.note,c.results_json FROM pack_device_checks c JOIN users u ON u.id=c.user_id WHERE c.packlist_id=? ORDER BY c.rowid DESC LIMIT 1`, p.ID).Scan(&proof.ID, &proof.User, &proof.Created, &proof.Basis, &proof.Passed, &proof.Note, &results)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if err = json.Unmarshal([]byte(results), &proof.Results); err != nil {
		return nil, err
	}
	stamp, err := time.Parse(time.RFC3339Nano, proof.Created)
	if err != nil {
		return nil, err
	}
	expires := stamp.Add(time.Duration(p.ValidHours) * time.Hour)
	proof.ValidUntil = expires.UTC().Format(time.RFC3339Nano)
	proof.Current = current && proof.Basis == basis && !now.Before(stamp) && now.Before(expires)
	return &proof, nil
}
func recordDeviceCheck(ctx context.Context, db *sql.DB, user, token, key, id string, input deviceCheckRequest, now time.Time) ([]byte, error) {
	if len(input.Basis) != 64 || len(input.Results) < 1 || len(input.Results) > 25 || !validReason(input.Note) {
		return nil, &stockError{400, "invalid_request", "Datenbasis, Geräteergebnisse und Prüfnotiz erforderlich."}
	}
	sort.Slice(input.Results, func(i, j int) bool { return input.Results[i].DeviceID < input.Results[j].DeviceID })
	passed := true
	for i, result := range input.Results {
		if result.DeviceID == "" || result.Present == nil || result.Functional == nil || result.AccessoriesComplete == nil || i > 0 && input.Results[i-1].DeviceID == result.DeviceID {
			return nil, &stockError{400, "invalid_request", "Jedes Gerät und alle drei Prüfergebnisse ausdrücklich angeben."}
		}
		passed = passed && *result.Present && *result.Functional && *result.AccessoriesComplete
	}
	canonical, _ := json.Marshal(input)
	hash := digest("physical_device_check:" + id + ":" + string(canonical))
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err = activeStockSession(ctx, tx, user, token, now); err != nil {
		return nil, err
	}
	if err = requireModule(ctx, tx, "packs"); err != nil {
		return nil, err
	}
	if err = requireModule(ctx, tx, "devices"); err != nil {
		return nil, err
	}
	p, err := loadPack(ctx, tx, user, id, "check")
	if err != nil {
		return nil, err
	}
	if err = rejectOtherKey(ctx, tx, user, key, "pack_device_check_requests"); err != nil {
		return nil, err
	}
	var previous, response string
	err = tx.QueryRowContext(ctx, `SELECT request_hash,response_json FROM pack_device_check_requests WHERE user_id=? AND key=?`, user, key).Scan(&previous, &response)
	if err == nil {
		if previous != hash {
			return nil, conflict("idempotency_conflict", "Schlüssel gehört zu anderen Prüfdaten.")
		}
		return []byte(response), nil
	}
	if err != sql.ErrNoRows {
		return nil, err
	}
	if len(input.Results) != len(p.Devices) {
		return nil, &stockError{400, "invalid_request", "Alle Sollgeräte genau einmal prüfen."}
	}
	for i, required := range p.Devices {
		if input.Results[i].DeviceID != required.DeviceID {
			return nil, &stockError{400, "invalid_request", "Prüfergebnisse passen nicht zu den Sollgeräten."}
		}
	}
	basis, snapshot, err := physicalDeviceBasis(ctx, tx, user, p, now)
	if err != nil {
		return nil, err
	}
	var latest int
	if err = tx.QueryRowContext(ctx, `SELECT MAX(version) FROM packlist_versions WHERE location_id=?`, p.Location).Scan(&latest); err != nil {
		return nil, err
	}
	if latest != p.Version || basis != input.Basis {
		return nil, conflict("basis_conflict", "Sollversion oder Gerätedaten haben sich geändert; neu laden und erneut physisch prüfen.")
	}
	encoded, _ := json.Marshal(snapshot)
	results, _ := json.Marshal(input.Results)
	check := newID()
	if _, err = tx.ExecContext(ctx, `INSERT INTO pack_device_checks VALUES(?,?,?,?,?,?,?,?,?)`, check, p.ID, user, now.UTC().Format(time.RFC3339Nano), basis, string(encoded), string(results), passed, input.Note); err != nil {
		return nil, err
	}
	proof, err := loadDeviceCheck(ctx, tx, p, basis, true, now)
	if err != nil {
		return nil, err
	}
	body, err := json.Marshal(proof)
	if err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO pack_device_check_outbox(event_id) VALUES(?)`, check); err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO pack_device_check_requests VALUES(?,?,?,?,?)`, user, key, hash, check, string(body)); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return body, nil
}
func packDeviceCheckRoutes(mux *http.ServeMux, a *authService) {
	mux.HandleFunc("POST /api/v1/packlists/{id}/device-checks", func(w http.ResponseWriter, r *http.Request) {
		user, ok := writeAuthorized(a, w, r)
		if !ok {
			return
		}
		var input deviceCheckRequest
		if !mutationBody(w, r, &input, 32768) {
			return
		}
		cookie, _ := r.Cookie(a.cookieName())
		body, err := recordDeviceCheck(r.Context(), a.db, user.ID, digest(cookie.Value), r.Header.Get("Idempotency-Key"), r.PathValue("id"), input, a.now())
		packResponse(w, body, err, 201)
	})
	mux.HandleFunc("GET /api/v1/packlists/{id}/device-checks", func(w http.ResponseWriter, r *http.Request) {
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
		basis, _, err := physicalDeviceBasis(r.Context(), tx, user.ID, p, a.now())
		if err != nil {
			sendStockError(w, err)
			return
		}
		var latest int
		if err = tx.QueryRowContext(r.Context(), `SELECT MAX(version) FROM packlist_versions WHERE location_id=?`, p.Location).Scan(&latest); err != nil {
			sendStockError(w, err)
			return
		}
		rows, err := tx.QueryContext(r.Context(), `SELECT c.id,u.username,c.created_at,c.basis_hash,c.physical_passed,c.note,c.results_json FROM pack_device_checks c JOIN users u ON u.id=c.user_id WHERE c.packlist_id=? ORDER BY c.rowid DESC LIMIT 51`, p.ID)
		if err != nil {
			sendStockError(w, err)
			return
		}
		defer rows.Close()
		proofs := []deviceCheckProof{}
		for rows.Next() {
			var proof deviceCheckProof
			var results string
			if err = rows.Scan(&proof.ID, &proof.User, &proof.Created, &proof.Basis, &proof.Passed, &proof.Note, &results); err != nil {
				sendStockError(w, err)
				return
			}
			if err = json.Unmarshal([]byte(results), &proof.Results); err != nil {
				sendStockError(w, err)
				return
			}
			stamp, e := time.Parse(time.RFC3339Nano, proof.Created)
			if e != nil {
				sendStockError(w, e)
				return
			}
			expires := stamp.Add(time.Duration(p.ValidHours) * time.Hour)
			proof.ValidUntil = expires.UTC().Format(time.RFC3339Nano)
			proof.Current = latest == p.Version && proof.Basis == basis && !a.now().Before(stamp) && a.now().Before(expires)
			proofs = append(proofs, proof)
		}
		if err = rows.Err(); err != nil {
			sendStockError(w, err)
			return
		}
		if len(proofs) > 50 {
			proofs = proofs[:50]
			w.Header().Set("X-Has-More", "true")
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(proofs)
	})
}
