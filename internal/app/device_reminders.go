package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Positive thresholds are operator settings; today and overdue are always included.
func ParseReminderThresholds(value string) ([]int, error) {
	result := []int{}
	seen := map[int]bool{}
	for _, part := range strings.Split(value, ",") {
		n, err := strconv.Atoi(part)
		if err != nil || n < 1 || n > 365 || seen[n] || len(result) >= 12 {
			return nil, fmt.Errorf("reminder thresholds require 1–12 distinct integers between 1 and 365")
		}
		seen[n] = true
		result = append(result, n)
	}
	sort.Sort(sort.Reverse(sort.IntSlice(result)))
	return result, nil
}
func reminderStage(days int, thresholds []int) (int, bool) {
	if days < 0 {
		return -1, true
	}
	if days == 0 {
		return 0, true
	}
	stage := 366
	for _, threshold := range thresholds {
		if days <= threshold && threshold < stage {
			stage = threshold
		}
	}
	return stage, stage != 366
}
func reminderBasis(record deviceRecord, kind string) string {
	// Administrative changes do not start a new control cycle. A passed proof or
	// effective control rule does, even when its calculated due day is unchanged.
	data, _ := json.Marshal(struct {
		Due        string
		Commission string
		Rule       DeadlineRule
		Passed     int
	}{record.Controls[kind].DueOn, record.CommissionedOn, record.EffectiveRules[kind], record.PassedVersions[kind]})
	return digest(string(data))
}
func reminderDay(now time.Time) (time.Time, error) {
	zone, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		return time.Time{}, err
	}
	return civilDate(now.In(zone).Format("2006-01-02"))
}

// ReconcileDeviceReminders is a bounded, atomic local job: no external delivery.
// It can be retried or run by multiple processes without duplicate events.
func ReconcileDeviceReminders(ctx context.Context, db *sql.DB, now time.Time, thresholds []int) error {
	if len(thresholds) == 0 || len(thresholds) > 12 {
		return fmt.Errorf("invalid reminder thresholds")
	}
	seen := map[int]bool{}
	for _, n := range thresholds {
		if n < 1 || n > 365 || seen[n] {
			return fmt.Errorf("invalid reminder thresholds")
		}
		seen[n] = true
	}
	today, err := reminderDay(now)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = requireModule(ctx, tx, "reminders"); err != nil {
		return err
	}
	rows, err := tx.QueryContext(ctx, `SELECT id,location_id,inventory_number,name,manufacturer,model,serial_or_lot,created_by,created_at,note FROM devices ORDER BY id LIMIT 1001`)
	if err != nil {
		return err
	}
	records := []deviceRecord{}
	for rows.Next() {
		var id, actor, created string
		var input deviceInput
		if err = rows.Scan(&id, &input.LocationID, &input.InventoryNumber, &input.Name, &input.Manufacturer, &input.Model, &input.SerialOrLot, &actor, &created, &input.Note); err != nil {
			rows.Close()
			return err
		}
		records = append(records, registeredDevice(input, id, actor, created))
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if len(records) > 1000 {
		return fmt.Errorf("reminder job exceeds 1000 devices; no partial reconciliation")
	}
	stamp := now.UTC().Format(time.RFC3339Nano)
	// Clearing then rebuilding happens in one transaction: readers never see an
	// empty intermediate queue and failures retain the entire previous job.
	if _, err = tx.ExecContext(ctx, `UPDATE device_reminders SET active=0,resolved_at=? WHERE active=1`, stamp); err != nil {
		return err
	}
	for _, record := range records {
		if err = projectDevice(ctx, tx, &record, now); err != nil {
			return err
		}
		if record.Decommission != nil {
			continue
		}
		for _, kind := range []string{"STK", "MTK", "electrical"} {
			control := record.Controls[kind]
			if control.DueOn == "" || control.State == "UNKNOWN" || control.State == "NOT_APPLICABLE" {
				continue
			}
			due, err := civilDate(control.DueOn)
			if err != nil {
				return err
			}
			stage, ok := reminderStage(int(due.Sub(today).Hours()/24), thresholds)
			if !ok {
				continue
			}
			basis := reminderBasis(record, kind)
			id := newID()
			result, err := tx.ExecContext(ctx, `INSERT INTO device_reminders(id,device_id,control_type,due_on,basis_hash,threshold_days,source_version,created_at) VALUES(?,?,?,?,?,?,?,?) ON CONFLICT(device_id,control_type,basis_hash,threshold_days) DO NOTHING`, id, record.ID, kind, control.DueOn, basis, stage, record.Version, stamp)
			if err != nil {
				return err
			}
			count, err := result.RowsAffected()
			if err != nil {
				return err
			}
			if count == 1 {
				if _, err = tx.ExecContext(ctx, `INSERT INTO device_reminder_outbox VALUES(?,?)`, id, stamp); err != nil {
					return err
				}
			}
			if _, err = tx.ExecContext(ctx, `UPDATE device_reminders SET active=1,resolved_at=NULL WHERE device_id=? AND control_type=? AND basis_hash=? AND threshold_days=?`, record.ID, kind, basis, stage); err != nil {
				return err
			}
		}
	}
	config, _ := json.Marshal(thresholds)
	if _, err = tx.ExecContext(ctx, `INSERT INTO device_reminder_worker VALUES(1,?,?) ON CONFLICT(id) DO UPDATE SET last_success_at=excluded.last_success_at,thresholds_json=excluded.thresholds_json`, stamp, string(config)); err != nil {
		return err
	}
	return tx.Commit()
}

// RunDeviceReminderWorker reconciles immediately, hourly, and after a module
// configuration change. The cheap minute poll lets administrators enable the
// worker without restarting the server; a disabled module stays dormant.
// Errors are reported without record contents. Cancellation rolls back a job.
func RunDeviceReminderWorker(ctx context.Context, db *sql.DB, thresholds []int, report func(error)) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	lastRun := time.Time{}
	lastRevision := -1
	for {
		if ctx.Err() != nil {
			return
		}
		pending, err := WebSetupPending(ctx, db)
		var config ModuleConfig
		var revision int
		if err == nil && !pending {
			config, revision, err = ActiveModules(ctx, db)
		}
		now := time.Now()
		if err == nil && (pending || !config.Enabled("reminders")) {
			lastRun = time.Time{}
		} else if err == nil && (lastRun.IsZero() || lastRevision != revision || now.Sub(lastRun) >= time.Hour) {
			lastRun, lastRevision = now, revision
			err = ReconcileDeviceReminders(ctx, db, now, thresholds)
		}
		if err != nil && err.Error() != "module_disabled" && err.Error() != "module_configuration_changed" && ctx.Err() == nil && report != nil {
			report(err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

type deviceReminder struct {
	ID              string `json:"id"`
	DeviceID        string `json:"device_id"`
	InventoryNumber string `json:"inventory_number"`
	Name            string `json:"name"`
	ControlType     string `json:"control_type"`
	DueOn           string `json:"due_on"`
	ThresholdDays   int    `json:"threshold_days"`
	DaysRemaining   int    `json:"days_remaining"`
	SourceVersion   int    `json:"source_version"`
	CreatedAt       string `json:"created_at"`
	basis           string
}
type deviceReminderBoard struct {
	AsOf              string           `json:"as_of"`
	LastWorkerSuccess string           `json:"last_worker_success"`
	Thresholds        []int            `json:"thresholds"`
	Reminders         []deviceReminder `json:"reminders"`
}

func deviceReminderRoutes(mux *http.ServeMux, a *authService) {
	mux.HandleFunc("GET /api/v1/devices/reminders", func(w http.ResponseWriter, r *http.Request) {
		user, err := a.session(r)
		if err != nil {
			a.sessionError(w, err)
			return
		}
		if r.URL.RawQuery != "" {
			apiError(w, 400, "invalid_reminder_query", "Keine Abfrageparameter vorgesehen.")
			return
		}
		select {
		case a.exportSlots <- struct{}{}:
			defer func() { <-a.exportSlots }()
		default:
			apiError(w, 429, "reminder_busy", "Erinnerungsübersicht ausgelastet; später erneut laden.")
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		unavailable := func() { apiError(w, 503, "reminders_unavailable", "Erinnerungen nicht geladen; erneut versuchen.") }
		tx, err := a.db.BeginTx(ctx, nil)
		if err != nil {
			unavailable()
			return
		}
		defer tx.Rollback()
		now := a.now()
		today, err := reminderDay(now)
		if err != nil {
			unavailable()
			return
		}
		board := deviceReminderBoard{AsOf: now.UTC().Format(time.RFC3339Nano), Thresholds: []int{}, Reminders: []deviceReminder{}}
		var config string
		err = tx.QueryRowContext(ctx, `SELECT last_success_at,thresholds_json FROM device_reminder_worker WHERE id=1`).Scan(&board.LastWorkerSuccess, &config)
		if err != nil && err != sql.ErrNoRows {
			unavailable()
			return
		}
		if err == nil && json.Unmarshal([]byte(config), &board.Thresholds) != nil {
			unavailable()
			return
		}
		rows, err := tx.QueryContext(ctx, `SELECT m.id,m.device_id,m.control_type,m.due_on,m.basis_hash,m.threshold_days,m.source_version,m.created_at FROM device_reminders m JOIN devices d ON d.id=m.device_id JOIN locations l ON l.id=d.location_id WHERE m.active=1 AND EXISTS(SELECT 1 FROM role_assignments a WHERE a.site_id=l.site_id AND a.user_id=? AND a.role IN ('viewer','helper','material_manager','device_manager')) ORDER BY m.due_on,m.device_id,m.control_type LIMIT 3001`, user.ID)
		if err != nil {
			unavailable()
			return
		}
		items := []deviceReminder{}
		for rows.Next() {
			var item deviceReminder
			if err = rows.Scan(&item.ID, &item.DeviceID, &item.ControlType, &item.DueOn, &item.basis, &item.ThresholdDays, &item.SourceVersion, &item.CreatedAt); err != nil {
				rows.Close()
				unavailable()
				return
			}
			items = append(items, item)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			unavailable()
			return
		}
		if len(items) > 3000 {
			apiError(w, 413, "reminders_too_large", "Mehr als 3000 Erinnerungen; keine Teilübersicht erstellt.")
			return
		}
		records := map[string]deviceRecord{}
		for _, item := range items {
			record, ok := records[item.DeviceID]
			if !ok {
				record, err = readDevice(ctx, tx, user.ID, item.DeviceID, now)
				if err != nil {
					unavailable()
					return
				}
				records[item.DeviceID] = record
			}
			// Worker downtime never resurrects retired devices or outdated control cycles.
			if record.Decommission != nil || reminderBasis(record, item.ControlType) != item.basis || record.Controls[item.ControlType].State == "NOT_APPLICABLE" || record.Controls[item.ControlType].State == "UNKNOWN" {
				continue
			}
			due, err := civilDate(item.DueOn)
			if err != nil {
				unavailable()
				return
			}
			item.DaysRemaining = int(due.Sub(today).Hours() / 24)
			if item.ThresholdDays == -1 && item.DaysRemaining >= 0 || item.ThresholdDays >= 0 && item.DaysRemaining > item.ThresholdDays {
				continue
			}
			item.InventoryNumber = record.InventoryNumber
			item.Name = record.Name
			board.Reminders = append(board.Reminders, item)
		}
		data, err := json.Marshal(board)
		if err != nil || ctx.Err() != nil {
			unavailable()
			return
		}
		if len(data) > 8<<20 {
			apiError(w, 413, "reminders_too_large", "Erinnerungen zu groß; keine Teilübersicht erstellt.")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(data)
	})
}
