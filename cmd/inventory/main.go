package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"flag"
	"fmt"
	"github.com/redzacmx3/DRK/internal/app"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
	_ "time/tzdata"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}
func run() error {
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		f := flag.NewFlagSet("healthcheck", flag.ContinueOnError)
		url := f.String("url", "https://localhost:8080/health/ready", "readiness URL")
		ca := f.String("ca", "", "trusted CA certificate file (optional)")
		if err := f.Parse(os.Args[2:]); err != nil {
			return err
		}
		client := http.Client{Timeout: 3 * time.Second}
		if *ca != "" {
			pem, err := os.ReadFile(*ca)
			if err != nil {
				return err
			}
			roots, err := x509.SystemCertPool()
			if err != nil {
				roots = x509.NewCertPool()
			}
			if !roots.AppendCertsFromPEM(pem) {
				return fmt.Errorf("invalid CA certificate")
			}
			client.Transport = &http.Transport{TLSClientConfig: &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}}
		}
		resp, err := client.Get(*url)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("unhealthy: %s", resp.Status)
		}
		return nil
	}

	if len(os.Args) < 2 {
		return fmt.Errorf("usage: inventory backup|migrate|seed-demo|create-product|create-batch|create-user|set-sites|disable-user|serve --db PATH [--listen 127.0.0.1:8080]")
	}
	flags := flag.NewFlagSet(os.Args[1], flag.ContinueOnError)
	path := flags.String("db", "inventory.db", "SQLite path")
	cert := flags.String("tls-cert", "var/tls/cert.pem", "TLS certificate")
	key := flags.String("tls-key", "var/tls/key.pem", "TLS private key")
	origin := flags.String("origin", "https://localhost:8080", "exact HTTPS browser origin")
	out := flags.String("out", "", "new SQLite backup destination")
	username := flags.String("username", "", "account username")
	role := flags.String("role", "viewer", "viewer, helper or material_manager")
	name := flags.String("name", "", "product name")
	unit := flags.String("unit", "", "explicit base unit")
	product := flags.String("product", "", "product UUID")
	lot := flags.String("lot", "", "manufacturer lot")
	expires := flags.String("expires-on", "", "exact expiry day YYYY-MM-DD, empty if unknown")
	site := flags.String("site", "", "viewer site UUID (comma-separated for set-sites)")
	addr := flags.String("listen", "127.0.0.1:8080", "loopback listen address")
	if err := flags.Parse(os.Args[2:]); err != nil {
		return err
	}
	if os.Args[1] == "serve" {
		host, _, err := net.SplitHostPort(*addr)
		if err != nil {
			return err
		}
		ip := net.ParseIP(host)
		if ip == nil || !ip.IsLoopback() {
			return fmt.Errorf("development server requires a loopback address; production proxy support is not configured")
		}
	}
	if os.Args[1] == "serve" {
		u, err := url.Parse(*origin)
		if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
			return fmt.Errorf("origin must be an HTTPS origin without path or credentials")
		}
	}
	db, err := app.Open(*path)
	if err != nil {
		return err
	}
	defer db.Close()
	switch os.Args[1] {
	case "create-product":
		id, err := app.CreateProduct(db, *name, *unit)
		if err == nil {
			fmt.Println(id)
		}
		return err
	case "create-batch":
		id, err := app.CreateBatch(db, *product, *lot, *expires)
		if err == nil {
			fmt.Println(id)
		}
		return err
	case "backup":
		if *out == "" {
			return fmt.Errorf("--out is required")
		}
		_, err := db.Exec(`VACUUM INTO ?`, *out)
		return err
	case "migrate":
		return app.Migrate(db)
	case "seed-demo":
		if err := app.Ready(context.Background(), db); err != nil {
			return err
		}
		return app.SeedDemo(db)
	case "create-user":
		bytes, err := io.ReadAll(io.LimitReader(os.Stdin, 259))
		if err != nil {
			return err
		}
		if len(bytes) > 258 {
			return fmt.Errorf("password input too long")
		}
		password := strings.TrimSuffix(strings.TrimSuffix(string(bytes), "\n"), "\r")
		return app.CreateUser(db, *username, password, *site, *role)
	case "set-sites":
		sites := []string{}
		if *site != "" {
			sites = strings.Split(*site, ",")
		}
		return app.SetSiteRole(db, *username, sites, *role)
	case "disable-user":
		return app.DisableUser(db, *username)
	case "serve":
		if err := app.Ready(context.Background(), db); err != nil {
			return fmt.Errorf("run migrate first: %w", err)
		}
		srv := &http.Server{Addr: *addr, Handler: app.HandlerWithOrigin(db, *origin), TLSConfig: &tls.Config{MinVersion: tls.VersionTLS12}, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 15 * time.Second, IdleTimeout: 60 * time.Second}
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		go func() {
			<-ctx.Done()
			shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			srv.Shutdown(shutdown)
		}()
		log.Printf("development server listening on %s", *addr)
		err = srv.ListenAndServeTLS(*cert, *key)
		if err == http.ErrServerClosed {
			return nil
		}
		return err
	default:
		return fmt.Errorf("unknown command %q", os.Args[1])
	}
}
