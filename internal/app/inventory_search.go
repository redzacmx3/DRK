package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

type inventoryPage struct {
	Items     []deviceRecord `json:"items"`
	Total     int            `json:"total"`
	Equipment int            `json:"equipment"`
	Limit     int            `json:"limit"`
	Offset    int            `json:"offset"`
}

func inventorySearchRoutes(mux *http.ServeMux, a *authService) {
	mux.HandleFunc("GET /api/v1/inventory/devices", func(w http.ResponseWriter, r *http.Request) {
		user, err := a.session(r)
		if err != nil {
			a.sessionError(w, err)
			return
		}
		page, err := searchInventory(r.Context(), a, r, user.ID)
		if err != nil {
			sendStockError(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(page)
	})
}

func searchInventory(ctx context.Context, a *authService, r *http.Request, user string) (inventoryPage, error) {
	page := inventoryPage{Items: []deviceRecord{}, Limit: 50}
	bad := func() (inventoryPage, error) {
		return page, &stockError{400, "invalid_inventory_filter", "Suche höchstens 160 Zeichen; gültige Art, Sortierung und Seitengröße (1–100) erforderlich."}
	}
	query, queryErr := url.ParseQuery(r.URL.RawQuery)
	if queryErr != nil {
		return bad()
	}
	q := strings.TrimSpace(query.Get("q"))
	kind := query.Get("kind")
	location := query.Get("location")
	if !intakeValue(q, 160) || !intakeValue(location, 160) {
		return bad()
	}
	if kind != "" && kind != "all" && kind != "device" && kind != "equipment" && kind != "unknown" {
		return bad()
	}
	for name, target := range map[string]*int{"limit": &page.Limit, "offset": &page.Offset} {
		if value := query.Get(name); value != "" {
			n, e := strconv.Atoi(value)
			if e != nil {
				return bad()
			}
			*target = n
		}
	}
	if page.Limit < 1 || page.Limit > 100 || page.Offset < 0 || page.Offset > 1_000_000 {
		return bad()
	}
	sortBy := map[string]string{"": "d.inventory_number", "number": "d.inventory_number", "name": "current_name", "location": "l.name", "created": "d.created_at"}[query.Get("sort")]
	if sortBy == "" {
		return bad()
	}
	direction := " ASC"
	if query.Get("direction") == "desc" {
		direction = " DESC"
	} else if query.Get("direction") != "" && query.Get("direction") != "asc" {
		return bad()
	}
	tx, err := a.db.BeginTx(ctx, nil)
	if err != nil {
		return page, err
	}
	defer tx.Rollback()
	// Latest identity corrections participate in search and sorting. SQL fragments
	// above are fixed allowlisted strings; all user values remain bound parameters.
	rows, err := tx.QueryContext(ctx, `SELECT d.id,d.location_id,d.inventory_number,
 COALESCE(json_extract(e.payload_json,'$.details.name'),d.name) AS current_name,
 COALESCE(json_extract(e.payload_json,'$.details.manufacturer'),d.manufacturer),
 COALESCE(json_extract(e.payload_json,'$.details.model'),d.model),
 COALESCE(json_extract(e.payload_json,'$.details.serial_or_lot'),d.serial_or_lot),
 d.created_by,d.created_at,d.note,COALESCE(i.asset_kind,'unknown'),COALESCE(i.external_code,''),l.name
 FROM devices d JOIN locations l ON l.id=d.location_id LEFT JOIN device_intake i ON i.device_id=d.id
 LEFT JOIN device_control_events e ON e.id=(SELECT x.id FROM device_control_events x WHERE x.device_id=d.id AND x.action='details' ORDER BY x.version DESC LIMIT 1)
 WHERE EXISTS(SELECT 1 FROM role_assignments a WHERE a.site_id=l.site_id AND a.user_id=? AND a.role IN ('viewer','helper','material_manager','device_manager'))
 AND (?='' OR d.location_id=?) AND (? IN ('','all') OR COALESCE(i.asset_kind,'unknown')=?)
 ORDER BY `+sortBy+` COLLATE NOCASE`+direction+`,d.id`, user, location, location, kind, kind)
	if err != nil {
		return page, err
	}
	// Streaming through identities gives Unicode-aware search (SQLite's default
	// LOWER/LIKE only folds ASCII) without projecting every device's full history.
	q = strings.ToLower(q)
	for rows.Next() {
		var in deviceInput
		var id, actor, created, assetKind, external, place string
		if err = rows.Scan(&id, &in.LocationID, &in.InventoryNumber, &in.Name, &in.Manufacturer, &in.Model, &in.SerialOrLot, &actor, &created, &in.Note, &assetKind, &external, &place); err != nil {
			rows.Close()
			return page, err
		}
		text := strings.ToLower(strings.Join([]string{in.InventoryNumber, in.Name, in.Manufacturer, in.Model, in.SerialOrLot, external, place}, " "))
		if !strings.Contains(text, q) {
			continue
		}
		if page.Total >= page.Offset && len(page.Items) < page.Limit {
			page.Items = append(page.Items, registeredDevice(in, id, actor, created))
		}
		page.Total++
		if assetKind == "equipment" {
			page.Equipment++
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return page, err
	}
	for i := range page.Items {
		if err = projectDevice(ctx, tx, &page.Items[i], a.now()); err != nil {
			return page, err
		}
	}
	return page, nil
}
