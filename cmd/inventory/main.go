package main

import (
	"context"
	"flag"
	"fmt"
	"github.com/redzacmx3/DRK/internal/app"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}
func run() error {
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		f := flag.NewFlagSet("healthcheck", flag.ContinueOnError)
		url := f.String("url", "http://127.0.0.1:8080/health/ready", "readiness URL")
		if err := f.Parse(os.Args[2:]); err != nil {
			return err
		}
		client := http.Client{Timeout: 3 * time.Second}
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
		return fmt.Errorf("usage: inventory migrate|seed-demo|serve --db PATH [--listen 127.0.0.1:8080]")
	}
	flags := flag.NewFlagSet(os.Args[1], flag.ContinueOnError)
	path := flags.String("db", "inventory.db", "SQLite path")
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
			return fmt.Errorf("development server requires a loopback address; authentication is not implemented")
		}
	}
	db, err := app.Open(*path)
	if err != nil {
		return err
	}
	defer db.Close()
	switch os.Args[1] {
	case "migrate":
		return app.Migrate(db)
	case "seed-demo":
		if err := app.Ready(context.Background(), db); err != nil {
			return err
		}
		return app.SeedDemo(db)
	case "serve":
		if err := app.Ready(context.Background(), db); err != nil {
			return fmt.Errorf("run migrate first: %w", err)
		}
		srv := &http.Server{Addr: *addr, Handler: app.Handler(db), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 15 * time.Second, IdleTimeout: 60 * time.Second}
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		go func() {
			<-ctx.Done()
			shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			srv.Shutdown(shutdown)
		}()
		log.Printf("development server listening on %s", *addr)
		err = srv.ListenAndServe()
		if err == http.ErrServerClosed {
			return nil
		}
		return err
	default:
		return fmt.Errorf("unknown command %q", os.Args[1])
	}
}
