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
		return fmt.Errorf("usage: inventory init|device-template|stock-template|backup|migrate|remind-devices|seed-demo|create-product|create-batch|create-user|set-sites|disable-user|serve --db PATH [--listen 127.0.0.1:8080]")
	}
	flags := flag.NewFlagSet(os.Args[1], flag.ContinueOnError)
	path := flags.String("db", "inventory.db", "SQLite path")
	localHTTP := flags.Bool("http", false, "explicit plaintext mode for trusted local testing")
	cert := flags.String("tls-cert", "var/tls/cert.pem", "TLS certificate")
	key := flags.String("tls-key", "var/tls/key.pem", "TLS private key")
	origin := flags.String("origin", "https://localhost:8080", "exact browser origin matching the selected transport")
	out := flags.String("out", "", "new backup or Excel template destination")
	username := flags.String("username", "", "account username")
	role := flags.String("role", "viewer", "viewer, helper, material_manager or device_manager")
	name := flags.String("name", "", "product name")
	unit := flags.String("unit", "", "explicit base unit")
	product := flags.String("product", "", "product UUID")
	lot := flags.String("lot", "", "manufacturer lot")
	expires := flags.String("expires-on", "", "exact expiry day YYYY-MM-DD, empty if unknown")
	site := flags.String("site", "", "viewer site UUID (comma-separated for set-sites)")
	addr := flags.String("listen", "127.0.0.1:8080", "listen IP address")
	reminderDays := flags.String("reminder-days", "60,30,7", "device reminder thresholds in days")
	reminders := flags.Bool("device-reminders", true, "run hourly local device reminders")
	allowNetwork := flags.Bool("allow-network-listen", false, "allow an explicit non-loopback HTTPS listener behind a protected network")
	if err := flags.Parse(os.Args[2:]); err != nil {
		return err
	}
	if os.Args[1] == "serve" {
		if err := validateListen(*addr, *allowNetwork); err != nil {
			return err
		}
	}
	if os.Args[1] == "serve" {
		u, err := url.Parse(*origin)
		scheme := "https"
		if *localHTTP {
			scheme = "http"
		}
		if err != nil || u.Scheme != scheme || u.Host == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
			return fmt.Errorf("origin must match the selected HTTP/HTTPS transport without path or credentials")
		}
	}
	if os.Args[1] == "device-template" || os.Args[1] == "stock-template" {
		if *out == "" {
			return fmt.Errorf("--out is required")
		}
		var body []byte
		var err error
		if os.Args[1] == "stock-template" {
			body, err = app.NewStockIntakeTemplate()
		} else {
			body, err = app.NewDeviceIntakeTemplate()
		}
		if err != nil {
			return err
		}
		file, err := os.OpenFile(*out, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			return err
		}
		_, err = file.Write(body)
		closeErr := file.Close()
		if err != nil {
			return err
		}
		return closeErr
	}
	var thresholds []int
	if os.Args[1] == "serve" || os.Args[1] == "remind-devices" {
		var err error
		thresholds, err = app.ParseReminderThresholds(*reminderDays)
		if err != nil {
			return err
		}
	}
	freshDatabase := false
	if os.Args[1] == "init" {
		_, statErr := os.Stat(*path)
		freshDatabase = os.IsNotExist(statErr)
		if statErr != nil && !freshDatabase {
			return statErr
		}
	}
	db, err := app.Open(*path)
	if err != nil {
		return err
	}
	defer db.Close()
	switch os.Args[1] {
	case "init":
		if freshDatabase {
			return app.Migrate(db)
		}
		return app.Ready(context.Background(), db)
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
	case "remind-devices":
		if err := app.Ready(context.Background(), db); err != nil {
			return err
		}
		return app.ReconcileDeviceReminders(context.Background(), db, time.Now(), thresholds)
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
		srv := &http.Server{Addr: *addr, Handler: app.HandlerWithTransport(db, *origin, *localHTTP), TLSConfig: &tls.Config{MinVersion: tls.VersionTLS12}, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 15 * time.Second, IdleTimeout: 60 * time.Second}
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		workerDone := make(chan struct{})
		if *reminders {
			go func() {
				defer close(workerDone)
				app.RunDeviceReminderWorker(ctx, db, thresholds, func(error) { log.Print("Device reminder job failed; last successful reminders retained") })
			}()
		} else {
			close(workerDone)
		}
		defer func() { stop(); <-workerDone }()
		go func() {
			<-ctx.Done()
			shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			srv.Shutdown(shutdown)
		}()
		if *localHTTP {
			log.Printf("Local HTTP server listening on %s", *addr)
			err = srv.ListenAndServe()
		} else {
			log.Printf("HTTPS server listening on %s", *addr)
			err = srv.ListenAndServeTLS(*cert, *key)
		}
		if err == http.ErrServerClosed {
			return nil
		}
		return err
	default:
		return fmt.Errorf("unknown command %q", os.Args[1])
	}
}

// Network exposure is opt-in. Exact browser origin remains required;
// forwarded headers do not establish trust or bypass HTTPS authentication.
func validateListen(addr string, allowNetwork bool) error {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return err
	}
	ip := net.ParseIP(host)
	if ip == nil || port == "" {
		return fmt.Errorf("listen requires an explicit IP address and port")
	}
	if !ip.IsLoopback() && !allowNetwork {
		return fmt.Errorf("non-loopback listener requires --allow-network-listen; restrict published ports to the intended private network")
	}
	return nil
}
