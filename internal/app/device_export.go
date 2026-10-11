package app

import (
	"bytes"
	"context"
	"encoding/csv"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Spreadsheet applications may execute formula-like CSV cells, even when quoted.
func csvText(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed != "" && strings.ContainsRune("=+-@", rune(trimmed[0])) {
		return "'" + value
	}
	if strings.HasPrefix(value, "\t") || strings.HasPrefix(value, "\r") || strings.HasPrefix(value, "\n") {
		return "'" + value
	}
	return value
}

func csvIdentifier(value string) string {
	if value == "" {
		return ""
	}
	return "'" + value
}

func sendExportError(w http.ResponseWriter, err error) {
	var problem *stockError
	if errors.As(err, &problem) {
		apiError(w, problem.Status, problem.Code, problem.Message)
		return
	}
	apiError(w, 503, "export_unavailable", "Export nicht erstellt; später erneut herunterladen.")
}

func deviceExportRoutes(mux *http.ServeMux, a *authService) {
	mux.HandleFunc("GET /api/v1/devices/export.csv", func(w http.ResponseWriter, r *http.Request) {
		user, err := a.session(r)
		if err != nil {
			a.sessionError(w, err)
			return
		}
		select {
		case a.exportSlots <- struct{}{}:
			defer func() { <-a.exportSlots }()
		default:
			apiError(w, 429, "export_busy", "Export ausgelastet; später erneut versuchen.")
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		tx, err := a.db.BeginTx(ctx, nil)
		if err != nil {
			sendExportError(w, err)
			return
		}
		defer tx.Rollback()
		now := a.now()
		rows, err := tx.QueryContext(ctx, `SELECT d.id,d.location_id,d.inventory_number,d.name,d.manufacturer,d.model,d.serial_or_lot,d.created_by,d.created_at,d.note,o.name,s.name,l.name FROM devices d JOIN locations l ON l.id=d.location_id JOIN sites s ON s.id=l.site_id JOIN operators o ON o.id=s.operator_id WHERE EXISTS(SELECT 1 FROM role_assignments a WHERE a.site_id=l.site_id AND a.user_id=? AND a.role IN ('viewer','helper','material_manager','device_manager')) ORDER BY d.inventory_number,d.id LIMIT 1001`, user.ID)
		if err != nil {
			sendExportError(w, err)
			return
		}
		type entry struct {
			record                   deviceRecord
			operator, site, location string
		}
		entries := []entry{}
		for rows.Next() {
			var in deviceInput
			var id, actor, created string
			var e entry
			if err = rows.Scan(&id, &in.LocationID, &in.InventoryNumber, &in.Name, &in.Manufacturer, &in.Model, &in.SerialOrLot, &actor, &created, &in.Note, &e.operator, &e.site, &e.location); err != nil {
				rows.Close()
				sendExportError(w, err)
				return
			}
			e.record = registeredDevice(in, id, actor, created)
			entries = append(entries, e)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			sendExportError(w, err)
			return
		}
		if len(entries) > 1000 {
			apiError(w, 413, "export_too_large", "Mehr als 1000 Geräte im Bereich; kein unvollständiger Export erstellt.")
			return
		}
		var data bytes.Buffer
		data.WriteString("\xef\xbb\xbf")
		writer := csv.NewWriter(&data)
		writer.Comma = ';'
		writer.UseCRLF = true
		writer.Write([]string{"Datenstand UTC", "Betreiber", "Betriebsstätte", "Lagerort", "Geräte-ID", "Inventarnummer", "Bezeichnung", "Hersteller", "Modell", "Serien-/Losnummer", "Geräteversion", "Inbetriebnahme", "Lebenszyklus", "STK Status", "STK Frist", "MTK Status", "MTK Frist", "Elektrisch Status", "Elektrisch Frist", "Aktive Sperren", "Einweisungsnachweise", "Bewertung", "Hinweise", "Geräteart", "Anschaffungsjahr", "Wirtschaftsakteur Funktion", "Wirtschaftsakteur Firma", "Wirtschaftsakteur Anschrift", "Betriebliche Zuordnung", "Instandhaltungsnachweise", "Außerbetriebnahme", "Mindestaufbewahrung einschließlich", "Richtigstellungen", "Störungen", "Offene Meldebewertungen"})
		for i := range entries {
			e := &entries[i]
			d := &e.record
			if err = projectDevice(ctx, tx, d, now); err != nil {
				sendExportError(w, err)
				return
			}
			blocks := []string{}
			for _, b := range d.ActiveBlocks {
				blocks = append(blocks, b.Reason+": "+b.Note)
			}
			record := []string{now.UTC().Format(time.RFC3339), e.operator, e.site, e.location, d.ID, csvIdentifier(d.InventoryNumber), d.Name, d.Manufacturer, d.Model, csvIdentifier(d.SerialOrLot), strconv.Itoa(d.Version), d.CommissionedOn, d.Lifecycle, d.Controls["STK"].State, d.Controls["STK"].DueOn, d.Controls["MTK"].State, d.Controls["MTK"].DueOn, d.Controls["electrical"].State, d.Controls["electrical"].DueOn, strings.Join(blocks, " | "), strconv.Itoa(len(d.Instructions)), "Vollständige Einsatzfreigabe ungeklärt", strings.Join(d.Reasons, " | ")}
			master := currentMasterData(*d)
			record = append(record, master.AssetType, master.AcquisitionYear, master.ActorRole, master.ActorName, master.ActorAddress, master.OperationalAssignment, strconv.Itoa(len(d.Maintenances)))
			for n := range record {
				record[n] = csvText(record[n])
			}
			retired, retain := "", ""
			if d.Decommission != nil {
				retired, retain = d.Decommission.PerformedOn, d.Decommission.RetainThrough
			}
			open := 0
			for _, incident := range d.Incidents {
				if incident.Report != nil && incident.Report.Status != "CLOSED" {
					open++
				}
			}
			record = append(record, retired, retain, strconv.Itoa(len(d.Corrections)), strconv.Itoa(len(d.Incidents)), strconv.Itoa(open))
			writer.Write(record)
			writer.Flush()
			if data.Len() > 8<<20 {
				apiError(w, 413, "export_too_large", "Export größer als 8 MiB; kein unvollständiger Export erstellt.")
				return
			}
		}
		writer.Flush()
		if err = writer.Error(); err != nil {
			sendExportError(w, err)
			return
		}
		if err = ctx.Err(); err != nil {
			sendExportError(w, err)
			return
		}
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		w.Header().Set("Content-Disposition", `attachment; filename="geraete-uebersicht.csv"`)
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Data-As-Of", now.UTC().Format(time.RFC3339))
		w.Header().Set("X-Export-Rows", strconv.Itoa(len(entries)))
		w.Write(data.Bytes())
	})
}
