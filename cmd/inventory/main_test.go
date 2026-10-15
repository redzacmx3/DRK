package main

import (
	"context"
	"github.com/redzacmx3/DRK/internal/app"
	"net"
	"os"
	"path/filepath"
	"strings"
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
	if _, err = db.Exec("DELETE FROM schema_migrations WHERE version=35"); err != nil {
		t.Fatal(err)
	}
	if err = run(); err == nil {
		t.Fatal("existing old schema automatically upgraded")
	}
	var version int
	if err = db.QueryRow("SELECT MAX(version) FROM schema_migrations").Scan(&version); err != nil || version != 34 {
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

func TestDisabledAndInvalidModulesRejectedBeforeDatabaseCreation(t *testing.T) {
	t.Setenv("DRK_SMTP_CONFIG", "")
	oldArgs := os.Args
	defer func() { os.Args = oldArgs }()
	for _, args := range [][]string{
		{"serve", "--modules", ""},
		{"serve", "--modules", "inventory,documents"},
		{"serve", "--modules", "unknown"},
		{"remind-devices", "--modules", "inventory"},
		{"deliver-reminders", "--modules", "inventory"},
		{"retry-reminder-mail", "--modules", "inventory"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "must-not-exist.db")
			os.Args = append(append([]string{"inventory"}, args...), "--db", path)
			if err := run(); err == nil {
				t.Fatal("invalid or disabled module operation accepted")
			}
			if _, err := os.Stat(path); !os.IsNotExist(err) {
				t.Fatal("invalid module configuration touched database")
			}
		})
	}
}

func TestModulesEnvironmentAndMaintenanceIndependence(t *testing.T) {
	t.Setenv("DRK_MODULES", "")
	t.Setenv("DRK_SMTP_CONFIG", "")
	oldArgs := os.Args
	defer func() { os.Args = oldArgs }()
	path := filepath.Join(t.TempDir(), "inventory.db")
	os.Args = []string{"inventory", "serve", "--db", path}
	if err := run(); err == nil || !strings.Contains(err.Error(), "invalid module configuration") {
		t.Fatalf("explicit empty environment value accepted: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("invalid environment touched database")
	}
	// An explicit flag overrides the environment, without reaching the database
	// because this intentionally invalid reminder setting fails first.
	os.Args = []string{"inventory", "serve", "--db", path, "--modules", "all", "--reminder-days", "7,7"}
	if err := run(); err == nil || strings.Contains(err.Error(), "module configuration") {
		t.Fatalf("explicit --modules did not override environment: %v", err)
	}
	t.Setenv("DRK_MODULES", "unknown")
	os.Args = []string{"inventory", "init", "--db", path}
	if err := run(); err != nil {
		t.Fatalf("maintenance blocked by deployment configuration: %v", err)
	}
	os.Args = []string{"inventory", "backup", "--db", path, "--out", filepath.Join(t.TempDir(), "backup.db")}
	if err := run(); err != nil {
		t.Fatalf("backup blocked by deployment configuration: %v", err)
	}
}

func TestManualReminderCommandsCannotChangeDeploymentModules(t *testing.T) {
	t.Setenv("DRK_SMTP_CONFIG", "")
	oldArgs := os.Args
	defer func() { os.Args = oldArgs }()
	path := filepath.Join(t.TempDir(), "inventory.db")
	os.Args = []string{"inventory", "init", "--db", path}
	if err := run(); err != nil {
		t.Fatal(err)
	}
	db, err := app.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	config, err := app.ParseModules("inventory")
	if err != nil {
		t.Fatal(err)
	}
	revision, err := app.ConfigureModules(context.Background(), db, config)
	if err != nil {
		t.Fatal(err)
	}
	smtpPath := filepath.Join(t.TempDir(), "smtp.json")
	if err = os.WriteFile(smtpPath, []byte(`{"host":"localhost","port":465,"mode":"tls","from":"test@example.invalid","recipients":[{"site_id":"synthetic-site","address":"test@example.invalid"}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	for _, command := range []string{"remind-devices", "deliver-reminders", "retry-reminder-mail"} {
		os.Args = []string{"inventory", command, "--db", path, "--modules", "all", "--smtp-config", smtpPath}
		if err = run(); err == nil || err.Error() != "module_configuration_changed" {
			t.Fatalf("%s did not reject a deployment configuration mismatch: %v", command, err)
		}
	}
	currentRevision, err := app.ConfigureModules(context.Background(), db, config)
	if err != nil || currentRevision != revision {
		t.Fatalf("manual commands changed module configuration: %d -> %d, %v", revision, currentRevision, err)
	}
	var reminders, deliveries, attempts int
	if err = db.QueryRow("SELECT (SELECT COUNT(*) FROM device_reminders), (SELECT COUNT(*) FROM device_reminder_deliveries), (SELECT COUNT(*) FROM device_reminder_mail_attempts)").Scan(&reminders, &deliveries, &attempts); err != nil {
		t.Fatal(err)
	}
	if reminders != 0 || deliveries != 0 || attempts != 0 {
		t.Fatalf("blocked manual jobs wrote reminders or mail: %d %d %d", reminders, deliveries, attempts)
	}
}

func TestManualReminderUsesAdminSelectionInsteadOfBootstrapEnvironment(t *testing.T) {
	t.Setenv("DRK_SMTP_CONFIG", "")
	t.Setenv("DRK_MODULES", "inventory")
	oldArgs := os.Args
	defer func() { os.Args = oldArgs }()
	path := filepath.Join(t.TempDir(), "inventory.db")
	os.Args = []string{"inventory", "init", "--db", path}
	if err := run(); err != nil {
		t.Fatal(err)
	}
	db, err := app.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	active, err := app.ParseModules("inventory,devices,reminders")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = app.ConfigureModules(context.Background(), db, active); err != nil {
		t.Fatal(err)
	}
	os.Args = []string{"inventory", "remind-devices", "--db", path}
	if err = run(); err != nil {
		t.Fatalf("persisted administrator selection blocked by bootstrap environment: %v", err)
	}
	// An invalid startup environment must not obstruct local maintenance either.
	t.Setenv("DRK_MODULES", "unknown")
	if err = run(); err != nil {
		t.Fatalf("manual job parsed an irrelevant bootstrap value: %v", err)
	}
	core, err := app.ParseModules("inventory")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = app.ConfigureModules(context.Background(), db, core); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DRK_MODULES", "all")
	if err = run(); err == nil || !strings.Contains(err.Error(), "requires the reminders module") {
		t.Fatalf("disabled persisted module was bypassed by bootstrap environment: %v", err)
	}
}

func TestServePreparesPrivateWebSetupWithoutCreatingAnAccount(t *testing.T) {
	t.Setenv("DRK_SMTP_CONFIG", "")
	t.Setenv("DRK_MODULES", "all")
	oldArgs := os.Args
	defer func() { os.Args = oldArgs }()
	dir := t.TempDir()
	path := filepath.Join(dir, "inventory.db")
	os.Args = []string{"inventory", "init", "--db", path}
	if err := run(); err != nil {
		t.Fatal(err)
	}
	// A reserved port makes serve finish predictably after startup preparation;
	// an occupied listener must not silently create a default administrator.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	os.Args = []string{"inventory", "serve", "--db", path, "--http", "--origin", "http://localhost:8080", "--listen", listener.Addr().String(), "--device-reminders=false"}
	if err = run(); err == nil || !strings.Contains(err.Error(), "address already in use") {
		t.Fatalf("reserved listener did not stop server startup: %v", err)
	}
	codePath := filepath.Join(dir, "setup-code.txt")
	info, err := os.Stat(codePath)
	if err != nil || info.Mode().Perm() != 0600 || info.Size() == 0 {
		t.Fatalf("private setup file was not created: %v, %v", info, err)
	}
	before, err := os.ReadFile(codePath)
	if err != nil {
		t.Fatal(err)
	}
	// SMTP can be prepared before the wizard creates its first site. The
	// configured transport stays dormant while setup and the module are closed.
	smtpPath := filepath.Join(dir, "smtp.json")
	if err = os.WriteFile(smtpPath, []byte(`{"host":"localhost","port":465,"mode":"tls","from":"test@example.invalid","recipients":[{"site_id":"synthetic-site","address":"test@example.invalid"}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	os.Args = []string{"inventory", "serve", "--db", path, "--http", "--origin", "http://localhost:8080", "--listen", listener.Addr().String(), "--modules", "inventory", "--smtp-config", smtpPath}
	if err = run(); err == nil || !strings.Contains(err.Error(), "address already in use") {
		t.Fatalf("second reserved listener startup did not stop: %v", err)
	}
	after, err := os.ReadFile(codePath)
	if err != nil || string(before) != string(after) {
		t.Fatal("a pending setup code was replaced on restart")
	}
	db, err := app.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var users int
	if err = db.QueryRow("SELECT COUNT(*) FROM users").Scan(&users); err != nil || users != 0 {
		t.Fatalf("startup created a default account: %d, %v", users, err)
	}
	if pending, err := app.WebSetupPending(context.Background(), db); err != nil || !pending {
		t.Fatalf("fresh setup is not pending: %v, %v", pending, err)
	}
	var jobs, deliveries int
	if err = db.QueryRow("SELECT (SELECT COUNT(*) FROM device_reminder_worker), (SELECT COUNT(*) FROM device_reminder_deliveries)").Scan(&jobs, &deliveries); err != nil || jobs != 0 || deliveries != 0 {
		t.Fatalf("first-start workers changed reminder state: %d %d, %v", jobs, deliveries, err)
	}
}
