package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"sort"
	"time"
)

type packAccessory struct {
	ProductID string `json:"product_id"`
	Quantity  int64  `json:"quantity"`
}
type packDevice struct {
	DeviceID    string          `json:"device_id"`
	Accessories []packAccessory `json:"accessories"`
}
type deviceAssessment struct {
	UserUsage   *usageSummary `json:"user_usage,omitempty"`
	Requirement packDevice    `json:"requirement"`
	Device      deviceRecord  `json:"device"`
	Status      string        `json:"status"`
	Reasons     []string      `json:"reasons"`
}
type packAssessment struct {
	Basis    string             `json:"device_basis_hash"`
	Check    *deviceCheckProof  `json:"last_device_check"`
	Scope    string             `json:"scope"`
	Status   string             `json:"status"`
	Overall  string             `json:"overall_readiness"`
	AsOf     string             `json:"data_as_of"`
	Material materialReadiness  `json:"material"`
	Devices  []deviceAssessment `json:"devices"`
	Reasons  []string           `json:"reasons"`
}

func validatePackDevices(input *publishPackRequest) error {
	bad := func() error {
		return &stockError{400, "invalid_device_requirements", "Geräte eindeutig angeben; Zubehörmengen müssen durch die Material-Sollpositionen abgedeckt sein."}
	}
	if len(input.Devices) > 25 {
		return bad()
	}
	sort.Slice(input.Devices, func(i, j int) bool { return input.Devices[i].DeviceID < input.Devices[j].DeviceID })
	quantities := map[string]int64{}
	assigned := map[string]int64{}
	for _, item := range input.Items {
		quantities[item.ProductID] = item.Quantity
	}
	for i := range input.Devices {
		device := &input.Devices[i]
		if device.DeviceID == "" || len(device.DeviceID) > 100 || len(device.Accessories) > 25 || i > 0 && input.Devices[i-1].DeviceID == device.DeviceID {
			return bad()
		}
		sort.Slice(device.Accessories, func(i, j int) bool { return device.Accessories[i].ProductID < device.Accessories[j].ProductID })
		for j, accessory := range device.Accessories {
			if accessory.ProductID == "" || len(accessory.ProductID) > 100 || accessory.Quantity < 1 || accessory.Quantity > 1_000_000_000 || j > 0 && device.Accessories[j-1].ProductID == accessory.ProductID {
				return bad()
			}
			assigned[accessory.ProductID] += accessory.Quantity
			if assigned[accessory.ProductID] > quantities[accessory.ProductID] {
				return bad()
			}
		}
	}
	return nil
}
func savePackDevices(ctx context.Context, tx *sql.Tx, p packVersion) error {
	for _, device := range p.Devices {
		var exists bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM devices WHERE id=? AND location_id=?)`, device.DeviceID, p.Location).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return scopeError()
		}
		accessories, _ := json.Marshal(device.Accessories)
		if _, err := tx.ExecContext(ctx, `INSERT INTO packlist_devices VALUES(?,?,?)`, p.ID, device.DeviceID, string(accessories)); err != nil {
			return err
		}
	}
	return nil
}
func loadPackDevices(ctx context.Context, tx *sql.Tx, id string) ([]packDevice, error) {
	rows, err := tx.QueryContext(ctx, `SELECT device_id,accessories_json FROM packlist_devices WHERE packlist_id=? ORDER BY device_id`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var devices []packDevice
	for rows.Next() {
		var device packDevice
		var accessories string
		if err = rows.Scan(&device.DeviceID, &accessories); err != nil {
			return nil, err
		}
		if err = json.Unmarshal([]byte(accessories), &device.Accessories); err != nil {
			return nil, err
		}
		devices = append(devices, device)
	}
	return devices, rows.Err()
}
func assessPack(ctx context.Context, tx *sql.Tx, user string, p packVersion, now time.Time) (packAssessment, error) {
	material, _, err := calculateMaterial(ctx, tx, p, now)
	if err != nil {
		return packAssessment{}, err
	}
	result := packAssessment{Scope: "material_and_devices", Status: "needs_device_check", Overall: "not_assessed", AsOf: now.UTC().Format(time.RFC3339Nano), Material: material, Devices: []deviceAssessment{}, Reasons: []string{"Vollständige Einsatzfreigabe einschließlich Nutzerqualifikation noch nicht bewertet."}}
	if material.Status == "shortfall" || material.Status == "physical_failed" {
		result.Status = "blocked"
	}
	if material.Status == "unknown" {
		result.Status = "unknown"
	}
	for _, reason := range material.Reasons {
		result.Reasons = append(result.Reasons, "Material: "+reason)
	}
	if len(p.Devices) == 0 {
		result.Reasons = append(result.Reasons, "Geräte-Soll und dessen Anwendbarkeit nicht konfiguriert.")
		if result.Status != "blocked" {
			result.Status = "unknown"
		}
	}
	for _, required := range p.Devices {
		record, err := readDevice(ctx, tx, user, required.DeviceID, now)
		if err != nil {
			return result, err
		}
		assessed := deviceAssessment{Requirement: required, Device: record, Status: "needs_physical_check", Reasons: []string{"Physische Anwesenheit, Zubehörzuordnung und Funktion noch nicht bestätigt."}}
		if record.LocationID != p.Location {
			assessed.Status = "blocked"
			assessed.Reasons = append(assessed.Reasons, "Gerät befindet sich nicht am Sollort.")
		}
		for _, incident := range record.Incidents {
			if incident.Report != nil && incident.Report.Status != "CLOSED" {
				assessed.Status = "blocked"
				assessed.Reasons = append(assessed.Reasons, "Störung: Meldebewertung/-bearbeitung noch offen.")
			}
		}
		if record.Decommission != nil {
			assessed.Status = "blocked"
			assessed.Reasons = append(assessed.Reasons, "Gerät außer Betrieb seit "+record.Decommission.PerformedOn)
		}
		if record.CommissionedOn == "" {
			if assessed.Status != "blocked" {
				assessed.Status = "unknown"
			}
			assessed.Reasons = append(assessed.Reasons, "Inbetriebnahme fehlt.")
		}
		for _, kind := range []string{"STK", "MTK", "electrical"} {
			deadline := record.Controls[kind]
			if deadline.State == "UNKNOWN" {
				if assessed.Status != "blocked" {
					assessed.Status = "unknown"
				}
				assessed.Reasons = append(assessed.Reasons, kind+": Kontrollpflicht ungeklärt.")
			}
			if deadline.State == "OVERDUE" {
				assessed.Status = "blocked"
				assessed.Reasons = append(assessed.Reasons, kind+": Kontrolltermin "+deadline.DueOn+" überschritten.")
			}
		}
		if len(record.ActiveBlocks) > 0 {
			assessed.Status = "blocked"
			for _, block := range record.ActiveBlocks {
				assessed.Reasons = append(assessed.Reasons, "Aktive Sperre: "+block.Reason)
			}
		}
		assessed.Reasons = append(assessed.Reasons, "Einweisungspflicht und konkrete Nutzerqualifikation noch nicht vollständig bewertet.")
		for _, instruction := range record.Instructions {
			if instruction.DocumentStatus == "quarantined" {
				if assessed.Status != "blocked" {
					assessed.Status = "unknown"
				}
				assessed.Reasons = append(assessed.Reasons, "Einweisungsanhang noch in Quarantäne.")
				break
			}
		}
		for _, accessory := range required.Accessories {
			for _, item := range material.Items {
				if item.ProductID == accessory.ProductID {
					if item.Shortfall > 0 {
						assessed.Status = "blocked"
						assessed.Reasons = append(assessed.Reasons, item.Product+": nutzbarer Zubehör-Sollbestand unterschritten.")
					}
					if item.Unknown > 0 {
						if assessed.Status != "blocked" {
							assessed.Status = "unknown"
						}
						assessed.Reasons = append(assessed.Reasons, item.Product+": Zubehör-Verfallsdaten ungeklärt.")
					}
				}
			}
		}
		if assessed.Status == "blocked" {
			result.Status = "blocked"
		} else if assessed.Status == "unknown" && result.Status != "blocked" {
			result.Status = "unknown"
		}
		result.Devices = append(result.Devices, assessed)
	}
	basis, _, err := physicalDeviceBasis(ctx, tx, user, p, now)
	if err != nil {
		return result, err
	}
	result.Basis = basis
	proof, err := loadDeviceCheck(ctx, tx, p, basis, material.CurrentVersion, now)
	if err != nil {
		return result, err
	}
	result.Check = proof
	if proof != nil {
		if !proof.Current {
			result.Reasons = append(result.Reasons, "Physischer Gerätenachweis veraltet; erneut prüfen.")
		} else if !proof.Passed {
			result.Status = "blocked"
			for i, physical := range proof.Results {
				device := &result.Devices[i]
				device.Reasons[0] = "Physische Geräteprüfung aktuell dokumentiert."
				if !*physical.Present || !*physical.Functional || !*physical.AccessoriesComplete {
					device.Status = "blocked"
					if !*physical.Present {
						device.Reasons = append(device.Reasons, "Gerät am Sollort fehlt.")
					}
					if !*physical.Functional {
						device.Reasons = append(device.Reasons, "Funktionsprüfung hat eine Abweichung festgestellt.")
					}
					if !*physical.AccessoriesComplete {
						device.Reasons = append(device.Reasons, "Zugeordnetes Zubehör unvollständig.")
					}
				} else if device.Status == "needs_physical_check" {
					device.Status = "physically_checked"
				}
			}
			result.Reasons = append(result.Reasons, "Physische Geräteprüfung hat eine Abweichung festgestellt.")
		} else {
			result.Reasons = append(result.Reasons, "Physische Geräte-/Zubehörprüfung aktuell dokumentiert; technische Sperren und Nutzerqualifikation bleiben gesondert zu bewerten.")
			for i := range result.Devices {
				result.Devices[i].Reasons[0] = "Physische Anwesenheit, Funktion und Zubehör aktuell bestätigt."
				if result.Devices[i].Status == "needs_physical_check" {
					result.Devices[i].Status = "physically_checked"
				}
			}
			if result.Status == "needs_device_check" {
				result.Status = "physically_checked"
			}
		}
	}
	for i := range result.Devices {
		assessment, err := assessUsage(ctx, tx, user, user, result.Devices[i].Device, now)
		if err != nil {
			return result, err
		}
		result.Devices[i].UserUsage = &usageSummary{assessment.UserID, assessment.Version, assessment.Status, assessment.Reasons}
		for _, reason := range assessment.Reasons {
			result.Reasons = append(result.Reasons, result.Devices[i].Device.InventoryNumber+" · Nutzer: "+reason)
		}
	}
	for _, device := range result.Devices {
		for _, reason := range device.Reasons {
			result.Reasons = append(result.Reasons, device.Device.InventoryNumber+": "+reason)
		}
	}
	if !material.CurrentVersion {
		result.Status = "obsolete"
	}
	return result, nil
}
func packAssessmentRoutes(mux *http.ServeMux, a *authService) {
	packDeviceCheckRoutes(mux, a)
	mux.HandleFunc("GET /api/v1/packlists/{id}/assessment", func(w http.ResponseWriter, r *http.Request) {
		user, err := a.session(r)
		if err != nil {
			a.sessionError(w, err)
			return
		}
		tx, err := a.db.BeginTx(r.Context(), nil)
		if err != nil {
			sendStockError(w, err)
			return
		}
		defer tx.Rollback()
		p, err := loadPack(r.Context(), tx, user.ID, r.PathValue("id"), "")
		if err != nil {
			sendStockError(w, err)
			return
		}
		result, err := assessPack(r.Context(), tx, user.ID, p, a.now())
		if err != nil {
			sendStockError(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(result)
	})
}
