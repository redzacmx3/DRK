package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"testing"
	"time"
)

func TestDeviceRegistrationScopeReplayAndAtomicity(t *testing.T) {
	f := stockDB(t)
	input := deviceInput{LocationID: "00000000-0000-4000-8000-000000000003", InventoryNumber: "TEST-1", Name: "Synthetic device", Manufacturer: "Demo", Model: "Demo", SerialOrLot: "same serial", Note: "synthetic registration"}
	requireStatus(t, packHTTP(f, "POST", "devices", "device-registration", input), 404)
	if err := SetSiteRole(f.db, "viewer", []string{"00000000-0000-4000-8000-000000000002"}, "device_manager"); err != nil {
		t.Fatal(err)
	}
	f.cookie, f.csrf = loginTest(t, f.handler, nil)
	first := packHTTP(f, "POST", "devices", "device-registration", input)
	requireStatus(t, first, 200)
	var record deviceRecord
	if err := json.Unmarshal(first.Body.Bytes(), &record); err != nil {
		t.Fatal(err)
	}
	if record.Readiness != "not_assessed" || record.Controls["STK"].State != "UNKNOWN" {
		t.Fatal(record)
	}
	replay := packHTTP(f, "POST", "devices", "device-registration", input)
	requireStatus(t, replay, 200)
	if replay.Body.String() != first.Body.String() {
		t.Fatal("replay changed")
	}
	requireStatus(t, packHTTP(f, "POST", "devices", "duplicate-inventory", input), 409)
	input.InventoryNumber = "TEST-2"
	requireStatus(t, packHTTP(f, "POST", "devices", "device-registration", input), 409)
	requireStatus(t, packHTTP(f, "POST", "devices", "same-serial-allowed", input), 200)
	requireStatus(t, packHTTP(f, "GET", "devices", "", nil), 200)
	requireStatus(t, packHTTP(f, "GET", "devices/"+record.ID+"/book", "", nil), 200)
	if _, err := f.db.Exec(`CREATE TRIGGER reject_device_outbox BEFORE INSERT ON device_outbox BEGIN SELECT RAISE(ABORT,'test rollback'); END`); err != nil {
		t.Fatal(err)
	}
	input.InventoryNumber = "TEST-3"
	requireStatus(t, packHTTP(f, "POST", "devices", "atomic-device-write", input), 503)
	var count int
	f.db.QueryRow(`SELECT COUNT(*) FROM devices`).Scan(&count)
	if count != 2 {
		t.Fatal(count)
	}
	if _, err := f.db.Exec(`UPDATE devices SET name='changed'`); err == nil {
		t.Fatal("mutable registration")
	}
	if err := SetSiteRole(f.db, "viewer", nil, "viewer"); err != nil {
		t.Fatal(err)
	}
	f.cookie, f.csrf = loginTest(t, f.handler, nil)
	requireStatus(t, packHTTP(f, "GET", "devices/"+record.ID+"/book", "", nil), 404)
	w := packHTTP(f, "GET", "devices", "", nil)
	requireStatus(t, w, 200)
	if w.Body.String() != "[]\n" {
		t.Fatal(w.Body.String())
	}
}

func TestDeviceSharedKeysAndConcurrentInventoryNumbers(t *testing.T) {
	f := stockDB(t)
	stock := stockRequest{BatchID: demoBatch, To: warehouse, Quantity: 1}
	requireStatus(t, stockHTTP(f, "receipts", "shared-device-key-1234", stock), 201)
	// Add an independent role without revoking this valid session; material rights
	// remain separate, as they would for a person with both assignments.
	if _, err := f.db.Exec(`INSERT INTO role_assignments VALUES(?,?,'device_manager')`, f.user, demoSite); err != nil {
		t.Fatal(err)
	}
	input := deviceInput{LocationID: warehouse, InventoryNumber: "CONCURRENT", Name: "Synthetic", Manufacturer: "Demo", Model: "Demo", SerialOrLot: "serial", Note: "synthetic concurrency"}
	requireStatus(t, packHTTP(f, "POST", "devices", "shared-device-key-1234", input), 409)
	cookieToken := digest(f.cookie.Value)
	other := secondDB(t, f.db)
	results := make(chan error, 2)
	for i, db := range []*sql.DB{f.db, other} {
		go func(db *sql.DB, i int) {
			_, err := registerDevice(context.Background(), db, f.user, cookieToken, fmt.Sprintf("concurrent-device-%d", i), input, time.Now())
			results <- err
		}(db, i)
	}
	successes := 0
	conflicts := 0
	for range 2 {
		err := <-results
		if err == nil {
			successes++
		} else if e, ok := err.(*stockError); ok && e.Status == 409 {
			conflicts++
		} else {
			t.Fatal(err)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatal(successes, conflicts)
	}
}
