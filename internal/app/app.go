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

//go:embed migrations/*.sql web/*
var assets embed.FS

func Open(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite3", path+"?_foreign_keys=on&_journal_mode=WAL&_synchronous=FULL&_busy_timeout=5000")
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

func Migrate(db *sql.DB) error {
	tx, err := db.Begin()
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
	if version > 1 {
		return fmt.Errorf("unsupported schema version %d", version)
	}
	if version == 0 {
		script, _ := assets.ReadFile("migrations/001_initial.sql")
		if _, err = tx.Exec(string(script)); err != nil {
			return err
		}
		if _, err = tx.Exec(`INSERT INTO schema_migrations VALUES(1)`); err != nil {
			return err
		}
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
	if version != 1 {
		return fmt.Errorf("schema migration required")
	}
	var count int
	return db.QueryRowContext(ctx, `SELECT COUNT(*) FROM locations`).Scan(&count)
}

type Location struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Site     string `json:"site"`
	Operator string `json:"operator"`
}

func Handler(db *sql.DB) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health/live", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok\n")) })
	mux.HandleFunc("GET /health/ready", func(w http.ResponseWriter, r *http.Request) {
		if Ready(r.Context(), db) != nil {
			http.Error(w, "database not ready", 503)
			return
		}
		w.Write([]byte("ok\n"))
	})
	mux.HandleFunc("GET /api/v1/locations", func(w http.ResponseWriter, r *http.Request) {
		rows, err := db.QueryContext(r.Context(), `SELECT l.id,l.name,s.name,o.name FROM locations l JOIN sites s ON s.id=l.site_id JOIN operators o ON o.id=s.operator_id ORDER BY l.name,l.id`)
		if err != nil {
			http.Error(w, "database unavailable", 503)
			return
		}
		defer rows.Close()
		result := []Location{}
		for rows.Next() {
			var l Location
			if err = rows.Scan(&l.ID, &l.Name, &l.Site, &l.Operator); err != nil {
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
	web, _ := fs.Sub(assets, "web")
	mux.Handle("GET /", http.FileServer(http.FS(web)))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; frame-ancestors 'none'; object-src 'none'; base-uri 'none'")
		mux.ServeHTTP(w, r)
	})
}
