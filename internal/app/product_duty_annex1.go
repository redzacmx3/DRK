package app

import (
	"context"
	"database/sql"
	"encoding/json"
)

type dutyCommissionChoice struct {
	Version     int    `json:"version"`
	PerformedOn string `json:"performed_on"`
	Evidence    string `json:"evidence"`
}
type dutyPersonChoice struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func dutyCommissionChoices(ctx context.Context, tx *sql.Tx, id string) ([]dutyCommissionChoice, error) {
	result := []dutyCommissionChoice{}
	rows, err := tx.QueryContext(ctx, `SELECT version,payload_json FROM device_control_events WHERE device_id=? AND action='commission' ORDER BY version LIMIT 101`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var version int
		var payload string
		var in deviceControlInput
		if err = rows.Scan(&version, &payload); err != nil {
			return nil, err
		}
		if err = json.Unmarshal([]byte(payload), &in); err != nil {
			return nil, err
		}
		result = append(result, dutyCommissionChoice{version, in.PerformedOn, in.Evidence})
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	if len(result) > 100 {
		return nil, &stockError{413, "commission_limit", "Zu viele Inbetriebnahmenachweise; keine Teilauswahl erstellt."}
	}
	return result, nil
}
func dutyPersonChoices(ctx context.Context, tx *sql.Tx, record deviceRecord, manager bool) ([]dutyPersonChoice, error) {
	result := []dutyPersonChoice{}
	if !manager {
		return result, nil
	}
	rows, err := tx.QueryContext(ctx, `SELECT u.id,u.username FROM users u WHERE u.active=1 AND EXISTS(SELECT 1 FROM role_assignments a JOIN locations l ON l.site_id=a.site_id WHERE a.user_id=u.id AND l.id=?) ORDER BY u.username,u.id LIMIT 501`, record.LocationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var person dutyPersonChoice
		if err = rows.Scan(&person.ID, &person.Name); err != nil {
			return nil, err
		}
		result = append(result, person)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	if len(result) > 500 {
		return nil, &stockError{413, "person_limit", "Mehr als 500 Konten im Gerätebereich; keine Teilauswahl erstellt."}
	}
	return result, nil
}
func validateAnnex1Reference(ctx context.Context, tx *sql.Tx, rule productDutyRule, record deviceRecord) error {
	if rule.CommissionVersion < 0 || rule.InstructionVersion < 0 {
		return adminInvalid("Nachweisversionen dürfen nicht negativ sein.")
	}
	if rule.CommissionVersion == 0 && rule.InstructionVersion == 0 && rule.DesignatedUserID == "" && rule.DesignatedAssignment == "" && rule.InstructionAcceptance == nil {
		return nil
	}
	if rule.Kind != "annex_1" || rule.Applicability != "required" {
		return adminInvalid("Inbetriebnahme- und Einweisungszuordnung nur für erforderliche Anlage-1-Pflichten erfassen.")
	}
	if rule.CommissionVersion > 0 {
		var exists bool
		err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM device_control_events WHERE device_id=? AND version=? AND action='commission')`, record.ID, rule.CommissionVersion).Scan(&exists)
		if err != nil {
			return err
		}
		if !exists {
			return adminInvalid("Inbetriebnahmeereignis aus derselben Geräteakte auswählen.")
		}
	}
	if rule.InstructionVersion > 0 {
		found := false
		for _, proof := range record.Instructions {
			if proof.Version == rule.InstructionVersion {
				found = true
			}
		}
		if !found {
			return adminInvalid("Einweisungsereignis aus derselben Geräteakte auswählen.")
		}
	}
	if rule.DesignatedUserID != "" {
		if rule.InstructionVersion == 0 || !validReason(rule.DesignatedAssignment) {
			return adminInvalid("Benannte Person mit konkretem Einweisungsereignis und ausdrücklichem Benennungsnachweis zuordnen.")
		}
		eligible, err := manualEligible(ctx, tx, rule.DesignatedUserID, record)
		if err != nil {
			return err
		}
		if !eligible {
			return adminInvalid("Benannte Person muss ein aktives Konto im Gerätebereich sein.")
		}
	} else if rule.DesignatedAssignment != "" {
		return adminInvalid("Benennungsnachweis erfordert ein konkretes Konto.")
	}
	return nil
}
func correctedDutyEvent(record deviceRecord, version int) bool {
	for _, proof := range record.Corrections {
		if proof.Target.Version == version {
			return true
		}
	}
	return false
}
func checkAnnex1Proofs(ctx context.Context, tx *sql.Tx, record deviceRecord, rule productDutyRule, basis *dutyInstructionBasis) ([]productDutyCheck, error) {
	commission := productDutyCheck{Kind: "annex_1_commission", Status: "unknown", EventVersion: rule.CommissionVersion, Reason: "Kein konkreter Inbetriebnahmenachweis zugeordnet; besondere Funktionsprüfung am Betriebsort bleibt offen."}
	instruction := productDutyCheck{Kind: "annex_1_instruction", Status: "unknown", EventVersion: rule.InstructionVersion, Reason: "Keine konkrete Einweisung und benannte Person ausdrücklich zugeordnet."}
	var bindingVersion, documentReviewVersion int
	if rule.CommissionVersion > 0 {
		var exists bool
		err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM device_control_events WHERE device_id=? AND version=? AND action='commission')`, record.ID, rule.CommissionVersion).Scan(&exists)
		if err != nil {
			return nil, err
		}
		if exists {
			commission.Status = "record_found"
			commission.Reason = "Inbetriebnahmenachweis aufgelöst; dies bestätigt noch keine besondere Funktionsprüfung am Betriebsort."
			if correctedDutyEvent(record, rule.CommissionVersion) {
				commission.Status = "unknown"
				commission.Reason = "Inbetriebnahmenachweis wurde richtiggestellt; vor fachlicher Abnahme abgleichen."
			}
		}
	}
	if rule.InstructionVersion > 0 {
		var selected *instructionProof
		for i := range record.Instructions {
			if record.Instructions[i].Version == rule.InstructionVersion {
				selected = &record.Instructions[i]
			}
		}
		if selected != nil {
			instruction.Reason = "Einweisungsereignis vorhanden; benannte Person und ausdrückliche Kontozuordnung noch ungeklärt."
			if rule.DesignatedUserID != "" && validReason(rule.DesignatedAssignment) {
				eligible, err := manualEligible(ctx, tx, rule.DesignatedUserID, record)
				if err != nil {
					return nil, err
				}
				if !eligible {
					instruction.Status = "blocked"
					instruction.Reason = "Benanntes Konto nicht mehr aktiv im Gerätebereich."
				} else {
					// Read only the latest bind/unbind needed for this organizational prerequisite.
					// Never expose another person's qualification or private usage history here.
					var payload string
					err = tx.QueryRowContext(ctx, `SELECT version,payload_json FROM usage_events WHERE device_id=? AND subject_id=? AND action IN ('bind','unbind') ORDER BY version DESC LIMIT 1`, record.ID, rule.DesignatedUserID).Scan(&bindingVersion, &payload)
					if err != nil && err != sql.ErrNoRows {
						return nil, err
					}
					if err == nil {
						var binding usageEvent
						if err = json.Unmarshal([]byte(payload), &binding); err != nil {
							return nil, err
						}
						value, _ := json.Marshal(currentDeviceDetails(record))
						if binding.Input.Action == "unbind" {
							instruction.Status = "blocked"
							instruction.Reason = "Einweisungszuordnung der benannten Person ausdrücklich widerrufen."
						} else if binding.Input.InstructionVersion != rule.InstructionVersion {
							instruction.Reason = "Aktuelle Kontozuordnung verweist auf eine andere Einweisung; bewusst neu abgleichen."
						} else if binding.Identity != digest(string(value)) {
							instruction.Reason = "Geräteidentifikation seit der Einweisungszuordnung geändert."
						} else {
							var status, sha string
							err = tx.QueryRowContext(ctx, `SELECT status,sha256,review_version FROM effective_documents WHERE id=? AND device_id=?`, selected.DocumentID, record.ID).Scan(&status, &sha, &documentReviewVersion)
							if err != nil {
								return nil, err
							}
							if status == "released" && sha == selected.DocumentSHA256 {
								instruction.Status = "record_found"
								instruction.Reason = "Konkrete Einweisung, aktive benannte Person, ausdrückliche Kontozuordnung und freigegebene Unterlage aufgelöst; fachliche Eignungsabnahme und Endanwenderbewertung bleiben offen."
							} else {
								instruction.Reason = "Gebundene Einweisungsunterlage aktuell nicht freigegeben oder Prüfsumme abweichend."
							}
						}
					}
				}
			}
			if correctedDutyEvent(record, rule.InstructionVersion) && instruction.Status != "blocked" {
				instruction.Status = "unknown"
				instruction.Reason = "Zugeordnete Einweisung wurde richtiggestellt; neuen passenden Nachweis ausdrücklich zuordnen."
			}
		}
	}
	if len(record.ActiveBlocks) > 0 || record.Decommission != nil {
		independentBlock := record.Decommission != nil
		for _, block := range record.ActiveBlocks {
			correctionBlock := false
			for _, correction := range record.Corrections {
				if correction.EventID == block.ID {
					correctionBlock = true
					break
				}
			}
			if !correctionBlock {
				independentBlock = true
			}
		}
		for _, check := range []*productDutyCheck{&commission, &instruction} {
			if check.Status == "record_found" || check.Status == "unknown" && independentBlock {
				check.Status = "blocked"
				check.Reason = "Gerät aktuell gesperrt oder außer Betrieb; keine positive Nachweisvorprüfung."
			}
		}
	}
	if acceptance := rule.InstructionAcceptance; acceptance != nil && acceptance.Reviewed {
		if acceptance.Decision == "rejected" {
			instruction.Status = "blocked"
			instruction.Reason = "Fachliche Abnahme der Einweisung der benannten Person ausdrücklich abgelehnt."
		} else if acceptance.Decision == "accepted" && instruction.Status == "record_found" {
			if basis == nil || basis.BindingVersion <= 0 || basis.BindingVersion != bindingVersion || basis.DocumentReviewVersion <= 0 || basis.DocumentReviewVersion != documentReviewVersion {
				instruction.Status = "unknown"
				instruction.Reason = "Kontozuordnung oder Unterlagenbewertung seit der Teilabnahme geändert; ausdrücklich neu abnehmen."
			} else {
				instruction.Status = "accepted"
				instruction.Reason = "Einweisung der benannten Person ausdrücklich fachlich abgenommen; konkrete Einweisung, aktive Kontozuordnung und Unterlagenbindung aktuell. Endanwenderbewertung und Gesamtfreigabe bleiben offen."
			}
		}
	}
	return []productDutyCheck{commission, instruction}, nil
}
