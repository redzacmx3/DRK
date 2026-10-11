package app

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"strconv"
	"time"
)

type attentionReason struct {
	Kind          string `json:"kind"`
	ControlType   string `json:"control_type,omitempty"`
	DueOn         string `json:"due_on,omitempty"`
	DaysRemaining *int   `json:"days_remaining,omitempty"`
	SourceVersion int    `json:"source_version,omitempty"`
	Description   string `json:"description"`
}
type attentionDevice struct {
	ID              string            `json:"id"`
	InventoryNumber string            `json:"inventory_number"`
	Name            string            `json:"name"`
	LocationID      string            `json:"location_id"`
	Version         int               `json:"version"`
	Lifecycle       string            `json:"lifecycle"`
	Reasons         []attentionReason `json:"reasons"`
}
type attentionBoard struct {
	AsOf           string            `json:"as_of"`
	Day            string            `json:"day"`
	HorizonDays    int               `json:"horizon_days"`
	ScannedDevices int               `json:"scanned_devices"`
	Counts         map[string]int    `json:"counts"`
	Devices        []attentionDevice `json:"devices"`
}

func deviceAttention(record deviceRecord, today time.Time, horizon int) attentionDevice {
	item := attentionDevice{record.ID, record.InventoryNumber, record.Name, record.LocationID, record.Version, record.Lifecycle, []attentionReason{}}
	if record.Decommission == nil {
		if record.CommissionedOn == "" {
			item.Reasons = append(item.Reasons, attentionReason{Kind: "not_commissioned", Description: "Inbetriebnahmenachweis fehlt; Anwendbarkeit klären."})
		}
		for _, kind := range []string{"STK", "MTK", "electrical"} {
			control := record.Controls[kind]
			if control.State == "UNKNOWN" {
				item.Reasons = append(item.Reasons, attentionReason{Kind: "unknown", ControlType: kind, Description: "Kontrollpflicht noch nicht geklärt."})
				continue
			}
			if control.DueOn == "" || control.State == "NOT_APPLICABLE" {
				continue
			}
			due, err := civilDate(control.DueOn)
			if err != nil {
				continue
			} // projected dates are validated by the deadline engine
			days := int(due.Sub(today).Hours() / 24)
			if days < 0 {
				item.Reasons = append(item.Reasons, attentionReason{Kind: "overdue", ControlType: kind, DueOn: control.DueOn, DaysRemaining: &days, Description: "Kontrollfrist überschritten."})
			} else if days <= horizon {
				item.Reasons = append(item.Reasons, attentionReason{Kind: "due", ControlType: kind, DueOn: control.DueOn, DaysRemaining: &days, Description: "Kontrolle innerhalb des gewählten Zeitraums fällig."})
			}
		}
	}
	for _, block := range record.ActiveBlocks {
		item.Reasons = append(item.Reasons, attentionReason{Kind: "block", SourceVersion: block.Version, Description: block.Reason + ": " + block.Note})
	}
	for _, incident := range record.Incidents {
		if incident.Report != nil && incident.Report.Status != "CLOSED" {
			item.Reasons = append(item.Reasons, attentionReason{Kind: "report", SourceVersion: incident.Version, Description: "Meldebewertung/-bearbeitung offen: " + incident.Description})
		}
	}
	return item
}
func attentionPriority(item attentionDevice) int {
	rank := 5
	for _, reason := range item.Reasons {
		current := map[string]int{"report": 0, "block": 1, "overdue": 2, "due": 3, "unknown": 4, "not_commissioned": 4}[reason.Kind]
		if current < rank {
			rank = current
		}
	}
	return rank
}
func deviceAttentionRoutes(mux *http.ServeMux, a *authService) {
	mux.HandleFunc("GET /api/v1/devices/attention", func(w http.ResponseWriter, r *http.Request) {
		user, err := a.session(r)
		if err != nil {
			a.sessionError(w, err)
			return
		}
		horizon := 60
		if r.URL.RawQuery != "" {
			values := r.URL.Query()
			if len(values) != 1 || len(values["days"]) != 1 {
				apiError(w, 400, "invalid_attention_window", "Genau einen Zeitraum days von 0 bis 365 wählen.")
				return
			}
			horizon, err = strconv.Atoi(values.Get("days"))
			if err != nil || horizon < 0 || horizon > 365 {
				apiError(w, 400, "invalid_attention_window", "Zeitraum von 0 bis 365 Tagen erforderlich.")
				return
			}
		}
		select {
		case a.exportSlots <- struct{}{}:
			defer func() { <-a.exportSlots }()
		default:
			apiError(w, 429, "attention_busy", "Arbeitsübersicht ausgelastet; später erneut laden.")
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		unavailable := func() {
			apiError(w, 503, "attention_unavailable", "Arbeitsübersicht nicht geladen; erneut versuchen.")
		}
		tx, err := a.db.BeginTx(ctx, nil)
		if err != nil {
			unavailable()
			return
		}
		defer tx.Rollback()
		now := a.now()
		berlin, err := time.LoadLocation("Europe/Berlin")
		if err != nil {
			unavailable()
			return
		}
		day := now.In(berlin).Format("2006-01-02")
		today, err := civilDate(day)
		if err != nil {
			unavailable()
			return
		}
		rows, err := tx.QueryContext(ctx, `SELECT d.id FROM devices d JOIN locations l ON l.id=d.location_id WHERE EXISTS(SELECT 1 FROM role_assignments a WHERE a.site_id=l.site_id AND a.user_id=? AND a.role IN ('viewer','helper','material_manager','device_manager')) ORDER BY d.inventory_number,d.id LIMIT 1001`, user.ID)
		if err != nil {
			unavailable()
			return
		}
		ids := []string{}
		for rows.Next() {
			var id string
			if err = rows.Scan(&id); err != nil {
				rows.Close()
				unavailable()
				return
			}
			ids = append(ids, id)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			unavailable()
			return
		}
		if len(ids) > 1000 {
			apiError(w, 413, "attention_too_large", "Mehr als 1000 Geräte im Bereich; keine unvollständige Arbeitsübersicht erstellt.")
			return
		}
		board := attentionBoard{now.UTC().Format(time.RFC3339Nano), day, horizon, len(ids), map[string]int{"report": 0, "block": 0, "overdue": 0, "due": 0, "unknown": 0, "not_commissioned": 0}, []attentionDevice{}}
		for _, id := range ids {
			record, err := readDevice(ctx, tx, user.ID, id, now)
			if err != nil {
				unavailable()
				return
			}
			item := deviceAttention(record, today, horizon)
			if len(item.Reasons) == 0 {
				continue
			}
			board.Devices = append(board.Devices, item)
			for _, reason := range item.Reasons {
				board.Counts[reason.Kind]++
			}
		}
		sort.SliceStable(board.Devices, func(i, j int) bool { return attentionPriority(board.Devices[i]) < attentionPriority(board.Devices[j]) })
		data, err := json.Marshal(board)
		if err != nil || ctx.Err() != nil {
			unavailable()
			return
		}
		if len(data) > 8<<20 {
			apiError(w, 413, "attention_too_large", "Arbeitsübersicht zu groß; keine gekürzte Übersicht erstellt.")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Write(data)
	})
}
