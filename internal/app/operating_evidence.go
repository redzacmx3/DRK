package app

// A component is a recorded prerequisite, not a legal or medical release.
// Product-duty reviews document applicability; final acceptance remains separate.
type evidenceComponent struct {
	Kind   string `json:"kind"`
	Status string `json:"status"`
	Reason string `json:"reason"`
}
type operatingEvidence struct {
	Status     string              `json:"status"`
	Components []evidenceComponent `json:"components"`
}

func consolidateEvidence(result packAssessment) operatingEvidence {
	evidence := operatingEvidence{Status: "incomplete", Components: []evidenceComponent{}}
	add := func(kind, status, reason string) {
		evidence.Components = append(evidence.Components, evidenceComponent{kind, status, reason})
		if status == "blocked" {
			evidence.Status = "blocked"
		}
	}
	material := "unknown"
	switch result.Material.Status {
	case "ready":
		material = "met"
	case "shortfall", "physical_failed":
		material = "blocked"
	case "needs_check":
		material = "needs_check"
	}
	add("material", material, "Materialbestand und eigene aktuelle physische Materialprüfung.")
	physical := "needs_check"
	if len(result.Devices) == 0 {
		physical = "unknown"
	} else if result.Check != nil && result.Check.Current {
		if result.Check.Passed {
			physical = "met"
		} else {
			physical = "blocked"
		}
	}
	add("device_function", physical, "Aktuelle physische Anwesenheits-, Funktions- und Zubehörprüfung dieser Sollversion.")
	technical, usage, manual := "met", "met", "met"
	if len(result.Devices) == 0 {
		technical = "unknown"
		usage = "unknown"
		manual = "unknown"
	}
	merge := func(current *string, status string) {
		if status == "blocked" {
			*current = "blocked"
		} else if status != "met" && *current != "blocked" {
			*current = "unknown"
		}
	}
	for _, device := range result.Devices {
		status := "met"
		record := device.Device
		if record.CommissionedOn == "" {
			status = "unknown"
		}
		for _, kind := range []string{"STK", "MTK", "electrical"} {
			state := record.Controls[kind].State
			switch state {
			case "CURRENT", "DUE_SOON", "NOT_APPLICABLE":
			case "OVERDUE":
				status = "blocked"
			default:
				if status != "blocked" {
					status = "unknown"
				}
			}
		}
		if (result.Material.Pack.Location != "" && record.LocationID != result.Material.Pack.Location) || record.Decommission != nil || len(record.ActiveBlocks) > 0 {
			status = "blocked"
		}
		for _, incident := range record.Incidents {
			if incident.Report != nil && incident.Report.Status != "CLOSED" {
				status = "blocked"
			}
		}

		merge(&technical, status)
		status = "unknown"
		if device.UserUsage != nil {
			if device.UserUsage.Status == "recorded_requirements_met" {
				status = "met"
			} else if device.UserUsage.Status == "blocked" {
				status = "blocked"
			}
		}
		merge(&usage, status)
		status = "unknown"
		if device.ManualAccess != nil {
			if device.ManualAccess.Status == "confirmed" {
				status = "met"
			} else if device.ManualAccess.Status == "blocked" {
				status = "blocked"
			}
		}
		merge(&manual, status)
	}
	add("device_controls", technical, "Inbetriebnahme, bestätigte Kontrollpflichten, Fristen, Sperren und offene Meldebewertungen.")
	add("user_qualification", usage, "Ausdrückliche Einweisungszuordnung und gültige Qualifikationsnachweise der konkret bewerteten Person.")
	add("manual_access", manual, "Befristeter Zugang zur passenden freigegebenen Gebrauchsanweisung für diese Person am Geräteort.")
	endUser := "unknown"
	if len(result.Devices) > 0 {
		endUser = "met"
	}
	for _, device := range result.Devices {
		status := "unknown"
		if device.EndUser != nil {
			if device.EndUser.Status == "accepted" {
				status = "met"
			} else if device.EndUser.Status == "blocked" {
				status = "blocked"
			}
		}
		merge(&endUser, status)
	}
	add("end_user_acceptance", endUser, "Ausdrückliche fachliche Teilabnahme der aktuellen personenbezogenen Endanwendernachweise; keine gesamte Einsatzfreigabe.")
	duties := "unknown"
	if len(result.Devices) > 0 {
		duties = "met"
	}
	for _, device := range result.Devices {
		status := "unknown"
		if device.DutyFulfilment != nil {
			if device.DutyFulfilment.Status == "accepted" && device.ProductDuties != nil && device.ProductDuties.Status == "reviewed" {
				status = "met"
			}
			if device.DutyFulfilment.Status == "blocked" {
				status = "blocked"
			}
		}
		if device.ProductDuties != nil {
			if device.ProductDuties.Status == "blocked" {
				status = "blocked"
			}
			for _, check := range device.ProductDuties.Checks {
				if check.Status == "blocked" {
					status = "blocked"
				}
			}
		}
		merge(&duties, status)
	}
	add("product_duties", duties, "Aktuelle ausdrückliche fachliche Erfüllungsabnahme aller acht Produktpflichtbereiche; konkrete Endanwender und gesamte Einsatzfreigabe bleiben getrennt.")
	allMet := len(evidence.Components) > 0
	for _, component := range evidence.Components {
		if component.Status != "met" {
			allMet = false
		}
	}
	if allMet {
		evidence.Status = "recorded_prerequisites_met"
	}
	if !result.Material.CurrentVersion {
		evidence.Status = "obsolete"
	}
	return evidence
}
