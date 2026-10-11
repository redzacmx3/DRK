package main

import (
	"github.com/redzacmx3/DRK/internal/app"
	"os"
	"path/filepath"
	"testing"
)

func TestNetworkListenerRequiresExplicitOptIn(t *testing.T) {
	for _, tt := range []struct {
		addr         string
		allow, valid bool
	}{
		{"127.0.0.1:8080", false, true}, {"[::1]:8080", false, true},
		{"0.0.0.0:8080", false, false}, {"100.64.1.2:8080", false, false},
		{"0.0.0.0:8080", true, true}, {"100.64.1.2:8080", true, true},
		{"[::]:8080", true, true}, {"localhost:8080", true, false}, {":8080", true, false}, {"127.0.0.1", true, false},
	} {
		t.Run(tt.addr, func(t *testing.T) {
			err := validateListen(tt.addr, tt.allow)
			if (err == nil) != tt.valid {
				t.Fatalf("allow=%v: %v", tt.allow, err)
			}
		})
	}
}

func TestFreshInitializationDoesNotUpgradeExistingDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "inventory.db")
	oldArgs := os.Args
	defer func() { os.Args = oldArgs }()
	os.Args = []string{"inventory", "init", "--db", path}
	if err := run(); err != nil {
		t.Fatal(err)
	}
	if err := run(); err != nil {
		t.Fatal(err)
	}
	db, err := app.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err = db.Exec("DELETE FROM schema_migrations WHERE version=25"); err != nil {
		t.Fatal(err)
	}
	if err = run(); err == nil {
		t.Fatal("existing old schema automatically upgraded")
	}
	var version int
	if err = db.QueryRow("SELECT MAX(version) FROM schema_migrations").Scan(&version); err != nil || version != 24 {
		t.Fatalf("existing schema changed: %d %v", version, err)
	}
}

func TestOriginMustMatchExplicitTransport(t *testing.T) {
	oldArgs := os.Args
	defer func() { os.Args = oldArgs }()
	for _, args := range [][]string{
		{"serve", "--http", "--origin", "https://localhost:8080"},
		{"serve", "--origin", "http://localhost:8080"},
		{"serve", "--http", "--origin", "http://localhost:8080/path"},
	} {
		path := filepath.Join(t.TempDir(), "must-not-exist.db")
		os.Args = append(append([]string{"inventory"}, args...), "--db", path)
		if err := run(); err == nil {
			t.Fatal("invalid origin accepted")
		}
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatal("invalid transport created database")
		}
	}
}

func TestReminderConfigurationRejectedBeforeDatabaseCreation(t *testing.T) {
	oldArgs := os.Args
	defer func() { os.Args = oldArgs }()
	for _, command := range []string{"serve", "remind-devices"} {
		path := filepath.Join(t.TempDir(), "must-not-exist.db")
		os.Args = []string{"inventory", command, "--db", path, "--reminder-days", "7,7"}
		if err := run(); err == nil {
			t.Fatal("invalid thresholds accepted")
		}
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatal("database touched")
		}
	}
}
