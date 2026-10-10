package app

import (
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"fmt"
	_ "github.com/mattn/go-sqlite3"
	"io/fs"
	"net/http"
)

//go:embed migrations/*.sql web/* templates/*
var assets embed.FS

func Open(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite3", path+"?_foreign_keys=on&_journal_mode=WAL&_synchronous=FULL&_busy_timeout=5000&_txlock=immediate")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if err = db.Ping(); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

func Migrate(db *sql.DB) (migrationErr error) {
	ctx := context.Background()
	conn, err := db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	if _, err = conn.ExecContext(ctx, `PRAGMA foreign_keys=OFF`); err != nil {
		return err
	}
	defer func() {
		if _, err := conn.ExecContext(ctx, `PRAGMA foreign_keys=ON`); err != nil {
			if migrationErr == nil {
				migrationErr = err
			}
			return
		}
		var enabled int
		if err := conn.QueryRowContext(ctx, `PRAGMA foreign_keys`).Scan(&enabled); err != nil || enabled != 1 {
			if migrationErr == nil {
				migrationErr = fmt.Errorf("foreign key enforcement could not be restored")
			}
		}
	}()
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations(version INTEGER PRIMARY KEY)`); err != nil {
		return err
	}
	var version int
	if err = tx.QueryRow(`SELECT COALESCE(MAX(version),0) FROM schema_migrations`).Scan(&version); err != nil {
		return err
	}
	if version > 15 {
		return fmt.Errorf("unsupported schema version %d", version)
	}
	for next := version + 1; next <= 15; next++ {
		name := map[int]string{1: "migrations/001_initial.sql", 2: "migrations/002_auth.sql", 3: "migrations/003_stock.sql", 4: "migrations/004_corrections.sql", 5: "migrations/005_packlists.sql", 6: "migrations/006_identifiers.sql", 7: "migrations/007_devices.sql", 8: "migrations/008_device_controls.sql", 9: "migrations/009_device_inspections.sql", 10: "migrations/010_device_documents.sql", 11: "migrations/011_packlist_devices.sql", 12: "migrations/012_pack_device_checks.sql", 13: "migrations/013_device_intake.sql", 14: "migrations/014_stock_intake.sql", 15: "migrations/015_device_details.sql"}[next]
		script, err := assets.ReadFile(name)
		if err != nil {
			return err
		}
		if _, err = tx.Exec(string(script)); err != nil {
			return err
		}
		if _, err = tx.Exec(`INSERT INTO schema_migrations VALUES(?)`, next); err != nil {
			return err
		}
	}
	rows, err := tx.Query(`PRAGMA foreign_key_check`)
	if err != nil {
		return err
	}
	if rows.Next() {
		rows.Close()
		return fmt.Errorf("foreign key integrity check failed")
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	var integrity string
	if err = tx.QueryRow(`PRAGMA integrity_check`).Scan(&integrity); err != nil {
		return err
	}
	if integrity != "ok" {
		return fmt.Errorf("database integrity check failed")
	}
	return tx.Commit()
}

// SeedDemo adds only synthetic data, explicitly requested by the developer.
func SeedDemo(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, s := range []string{
		`INSERT INTO operators VALUES('00000000-0000-4000-8000-000000000001','Demo-Betreiber (synthetisch)') ON CONFLICT(id) DO NOTHING`,
		`INSERT INTO sites VALUES('00000000-0000-4000-8000-000000000002','00000000-0000-4000-8000-000000000001','Demo-Betriebsstätte') ON CONFLICT(id) DO NOTHING`,
		`INSERT INTO locations VALUES('00000000-0000-4000-8000-000000000003','00000000-0000-4000-8000-000000000002','Demo-Materiallager') ON CONFLICT(id) DO NOTHING`,
		`INSERT INTO locations VALUES('00000000-0000-4000-8000-000000000004','00000000-0000-4000-8000-000000000002','Demo-Sanitätskoffer') ON CONFLICT(id) DO NOTHING`,
		`INSERT INTO product_types VALUES('00000000-0000-4000-8000-000000000010','Demo-Kompresse (synthetisch)','Stück') ON CONFLICT(id) DO NOTHING`,
		`INSERT INTO batches(id,product_id,lot,expires_on) VALUES('00000000-0000-4000-8000-000000000011','00000000-0000-4000-8000-000000000010','DEMO-2026','2028-12-31') ON CONFLICT(id) DO NOTHING`,
	} {
		if _, err = tx.Exec(s); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func Ready(ctx context.Context, db *sql.DB) error {
	var version int
	if err := db.QueryRowContext(ctx, `SELECT MAX(version) FROM schema_migrations`).Scan(&version); err != nil {
		return err
	}
	if version != 15 {
		return fmt.Errorf("schema migration required")
	}
	var count int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM locations`).Scan(&count); err != nil {
		return err
	}
	var users, roles, sessions, stock, packs, identifiers, devices bool
	return db.QueryRowContext(ctx, `SELECT EXISTS(SELECT id,password_hash,active FROM users), EXISTS(SELECT user_id,site_id,role FROM role_assignments), EXISTS(SELECT token_hash,csrf_hash FROM sessions), EXISTS(SELECT 1 FROM product_types JOIN batches ON 1=0 JOIN stock_balances ON 1=0 JOIN stock_movements ON 1=0 JOIN movement_lines ON 1=0 JOIN audit_events ON 1=0 JOIN outbox ON 1=0 JOIN idempotency_keys ON 1=0 JOIN stock_intake ON 1=0 JOIN stock_imports ON 1=0 JOIN stock_import_outbox ON 1=0 JOIN stock_import_requests ON 1=0), EXISTS(SELECT 1 FROM packlist_versions JOIN packlist_items ON 1=0 JOIN readiness_checks ON 1=0 JOIN packlist_events ON 1=0 JOIN packlist_outbox ON 1=0 JOIN packlist_requests ON 1=0 JOIN packlist_devices ON 1=0 JOIN pack_device_checks ON 1=0 JOIN pack_device_check_outbox ON 1=0 JOIN pack_device_check_requests ON 1=0), EXISTS(SELECT 1 FROM product_identifiers JOIN catalog_events ON 1=0 JOIN catalog_outbox ON 1=0 JOIN catalog_requests ON 1=0 JOIN movement_packaging ON 1=0), EXISTS(SELECT 1 FROM devices JOIN device_events ON 1=0 JOIN device_outbox ON 1=0 JOIN device_requests ON 1=0 JOIN device_control_events ON 1=0 JOIN device_control_outbox ON 1=0 JOIN device_control_requests ON 1=0 JOIN device_documents ON 1=0 JOIN document_events ON 1=0 JOIN document_outbox ON 1=0 JOIN document_requests ON 1=0 JOIN device_intake ON 1=0 JOIN device_imports ON 1=0 JOIN device_import_outbox ON 1=0 JOIN device_import_requests ON 1=0)`).Scan(&users, &roles, &sessions, &stock, &packs, &identifiers, &devices)
}

type Location struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Site     string `json:"site"`
	Operator string `json:"operator"`
	SiteID   string `json:"site_id"`
}

func Handler(db *sql.DB) http.Handler { return HandlerWithOrigin(db, "https://localhost:8080") }

func HandlerWithOrigin(db *sql.DB, origin string) http.Handler {
	auth := newAuth(db, origin)
	mux := http.NewServeMux()
	auth.routes(mux)
	stockRoutes(mux, auth)
	stockImportRoutes(mux, auth)
	correctionRoutes(mux, auth)
	packlistRoutes(mux, auth)
	scannerRoutes(mux, auth)
	deviceRoutes(mux, auth)
	mux.HandleFunc("GET /health/live", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok\n")) })
	mux.HandleFunc("GET /health/ready", func(w http.ResponseWriter, r *http.Request) {
		if Ready(r.Context(), db) != nil {
			http.Error(w, "database not ready", 503)
			return
		}
		w.Write([]byte("ok\n"))
	})
	mux.HandleFunc("GET /api/v1/locations", func(w http.ResponseWriter, r *http.Request) {
		user, err := auth.session(r)
		if err != nil {
			auth.sessionError(w, err)
			return
		}
		rows, err := db.QueryContext(r.Context(), `SELECT DISTINCT l.id,l.name,s.name,o.name,l.site_id FROM locations l JOIN sites s ON s.id=l.site_id JOIN operators o ON o.id=s.operator_id JOIN role_assignments a ON a.site_id=s.id WHERE a.user_id=? AND a.role IN ('viewer','helper','material_manager','device_manager') ORDER BY l.name,l.id`, user.ID)
		if err != nil {
			http.Error(w, "database unavailable", 503)
			return
		}
		defer rows.Close()
		result := []Location{}
		for rows.Next() {
			var l Location
			if err = rows.Scan(&l.ID, &l.Name, &l.Site, &l.Operator, &l.SiteID); err != nil {
				http.Error(w, "database unavailable", 503)
				return
			}
			result = append(result, l)
		}
		if rows.Err() != nil {
			http.Error(w, "database unavailable", 503)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(result)
	})
	mux.HandleFunc("GET /api/v1/locations/{id}", func(w http.ResponseWriter, r *http.Request) {
		user, err := auth.session(r)
		if err != nil {
			auth.sessionError(w, err)
			return
		}
		var l Location
		err = db.QueryRowContext(r.Context(), `SELECT DISTINCT l.id,l.name,s.name,o.name,l.site_id FROM locations l JOIN sites s ON s.id=l.site_id JOIN operators o ON o.id=s.operator_id JOIN role_assignments a ON a.site_id=s.id WHERE l.id=? AND a.user_id=? AND a.role IN ('viewer','helper','material_manager','device_manager')`, r.PathValue("id"), user.ID).Scan(&l.ID, &l.Name, &l.Site, &l.Operator, &l.SiteID)
		if err == sql.ErrNoRows {
			apiError(w, 404, "not_found", "Lagerort nicht gefunden.")
			return
		}
		if err != nil {
			apiError(w, 503, "unavailable", "Datenbank nicht verfügbar.")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(l)
	})
	web, _ := fs.Sub(assets, "web")
	mux.Handle("GET /", http.FileServer(http.FS(web)))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; frame-ancestors 'none'; object-src 'none'; base-uri 'none'")
		mux.ServeHTTP(w, r)
	})
}
