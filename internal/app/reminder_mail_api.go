package app

import (
	"encoding/json"
	"net/http"
)

type reminderMailStatus struct {
	ID              string `json:"id"`
	DeviceID        string `json:"device_id"`
	InventoryNumber string `json:"inventory_number"`
	Name            string `json:"name"`
	ControlType     string `json:"control_type"`
	DueOn           string `json:"due_on"`
	MessageID       string `json:"message_id"`
	State           string `json:"state"`
	Attempts        int    `json:"attempts"`
	CreatedAt       string `json:"created_at"`
	LastAttempt     string `json:"last_attempt_at"`
	NextAttempt     string `json:"next_attempt_at"`
	Code            string `json:"code"`
}

func reminderMailRoutes(mux *http.ServeMux, a *authService) {
	mux.HandleFunc("GET /api/v1/devices/reminder-mail", func(w http.ResponseWriter, r *http.Request) {
		user, err := a.session(r)
		if err != nil {
			a.sessionError(w, err)
			return
		}
		if r.URL.RawQuery != "" {
			apiError(w, 400, "invalid_mail_query", "Keine Abfrageparameter vorgesehen.")
			return
		}
		rows, err := a.db.QueryContext(r.Context(), `SELECT x.id,m.device_id,d.inventory_number,d.name,m.control_type,m.due_on,x.message_id,x.state,x.attempts,x.created_at,COALESCE(x.last_attempt_at,''),x.next_attempt_at,x.last_code FROM device_reminder_deliveries x JOIN device_reminders m ON m.id=x.reminder_id JOIN devices d ON d.id=m.device_id JOIN locations l ON l.id=d.location_id WHERE EXISTS(SELECT 1 FROM role_assignments a WHERE a.site_id=l.site_id AND a.user_id=? AND a.role IN ('viewer','helper','material_manager','device_manager')) ORDER BY x.created_at DESC,x.id LIMIT 201`, user.ID)
		if err != nil {
			apiError(w, 503, "mail_status_unavailable", "Versandstatus nicht verfügbar.")
			return
		}
		defer rows.Close()
		result := []reminderMailStatus{}
		for rows.Next() {
			var item reminderMailStatus
			if err = rows.Scan(&item.ID, &item.DeviceID, &item.InventoryNumber, &item.Name, &item.ControlType, &item.DueOn, &item.MessageID, &item.State, &item.Attempts, &item.CreatedAt, &item.LastAttempt, &item.NextAttempt, &item.Code); err != nil {
				apiError(w, 503, "mail_status_unavailable", "Versandstatus nicht verfügbar.")
				return
			}
			result = append(result, item)
		}
		if rows.Err() != nil {
			apiError(w, 503, "mail_status_unavailable", "Versandstatus nicht verfügbar.")
			return
		}
		w.Header().Set("X-Has-More", "false")
		if len(result) > 200 {
			result = result[:200]
			w.Header().Set("X-Has-More", "true")
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(result)
	})
}
