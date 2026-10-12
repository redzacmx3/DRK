package app

import (
	"context"
	"database/sql"
)

// A resolved record is a prerequisite for qualified acceptance, never acceptance itself.
type productDutyCheck struct {
	Kind         string `json:"kind"`
	Status       string `json:"status"`
	EventVersion int    `json:"event_version,omitempty"`
	Reason       string `json:"reason"`
}

func validateProductDutyReference(rule productDutyRule, record deviceRecord) error {
	if rule.EventVersion < 0 || rule.EventVersion > 0 && (rule.Kind != "manufacturer_maintenance" || rule.Applicability != "required") {
		return adminInvalid("Eine Geräteereignisversion wird derzeit nur für erforderliche Herstellerwartung unterstützt.")
	}
	if rule.EventVersion == 0 {
		return nil
	}
	for _, proof := range record.Maintenances {
		if proof.Version == rule.EventVersion && proof.Kind == "maintenance" {
			return nil
		}
	}
	return adminInvalid("Wartungsereignis aus derselben Geräteakte ausdrücklich auswählen; eine Reparatur ist kein Wartungsnachweis.")
}
func checkProductDutyProofs(ctx context.Context, tx *sql.Tx, record deviceRecord, rules []productDutyRule, functionSHA string, instructionBasis *dutyInstructionBasis, maintenanceBases ...*dutyMaintenanceBasis) ([]productDutyCheck, error) {
	checks := make([]productDutyCheck, 0, len(rules))
	for _, rule := range rules {
		check := productDutyCheck{Kind: rule.Kind, Status: "unknown", EventVersion: rule.EventVersion, Reason: "Automatische Nachweisauflösung für diesen Pflichtbereich noch nicht umgesetzt; fachliche Abnahme offen."}
		if rule.Applicability == "exempt" {
			check.Status = "not_applicable"
			check.Reason = "Begründete Nichtanwendbarkeit dokumentiert; keine automatische Bestätigung der Ausnahme."
		} else if rule.Applicability == "unknown" {
			check.Reason = "Anwendbarkeit noch ungeklärt."
		} else if rule.Kind == "manufacturer_maintenance" {
			check.Reason = "Kein konkretes Wartungsereignis aus dieser Geräteakte zugeordnet; Freitextreferenz reicht nicht zur Auflösung."
			if rule.EventVersion > 0 {
				var selected *maintenanceProof
				latest := 0
				for i := range record.Maintenances {
					proof := &record.Maintenances[i]
					if proof.Kind == "maintenance" && proof.Version > latest {
						latest = proof.Version
					}
					if proof.Kind == "maintenance" && proof.Version == rule.EventVersion {
						selected = proof
					}
				}
				if selected == nil {
					check.Reason = "Zugeordnetes Wartungsereignis fehlt in dieser Geräteakte."
				} else {
					check.Reason = "Wartungsereignis aufgelöst; Herstellerintervall, Umfang und fachliche Erfüllungsabnahme bleiben separat zu prüfen."
					check.Status = "record_found"
					if selected.Result != "completed" {
						check.Status = "blocked"
						check.Reason = "Zugeordnete Wartung fehlgeschlagen oder mit offenem Nacharbeitsbedarf."
					} else if latest != selected.Version {
						check.Status = "unknown"
						check.Reason = "Neuere Wartung vorhanden; Zuordnung neu prüfen."
					}
					for _, correction := range record.Corrections {
						if correction.Target.Version == selected.Version && check.Status != "blocked" {
							check.Status = "unknown"
							check.Reason = "Zugeordneter Wartungsnachweis wurde richtiggestellt; fachlich abgleichen und neuen Nachweis erfassen."
						}
					}
					if check.Status == "record_found" && (len(record.ActiveBlocks) > 0 || record.Decommission != nil) {
						check.Status = "blocked"
						check.Reason = "Gerät gesperrt oder außer Betrieb; Wartungsreferenz ermöglicht keine positive Vorprüfung."
					}
					if check.Status == "record_found" {
						if selected.DocumentID == "" {
							check.Status = "unknown"
							check.Reason = "Wartungsereignis hat keine gebundene freigegebene Nachweisunterlage."
						} else {
							var status, sha string
							err := tx.QueryRowContext(ctx, `SELECT status,sha256 FROM effective_documents WHERE id=? AND device_id=?`, selected.DocumentID, record.ID).Scan(&status, &sha)
							if err != nil {
								return nil, err
							}
							if status != "released" || sha != selected.DocumentSHA256 {
								check.Status = "unknown"
								check.Reason = "Gebundene Wartungsunterlage aktuell nicht freigegeben oder Prüfsumme abweichend."
							}
						}
					}
				}
			}
		}
		if rule.MaintenanceAcceptance != nil {
			var basis *dutyMaintenanceBasis
			if len(maintenanceBases) > 0 {
				basis = maintenanceBases[0]
			}
			if err := applyDutyMaintenanceAcceptance(ctx, tx, record, rule, basis, &check); err != nil {
				return nil, err
			}
		}
		if rule.Kind == "annex_1" && rule.Applicability == "required" {
			details, err := checkAnnex1Proofs(ctx, tx, record, rule, instructionBasis)
			if err != nil {
				return nil, err
			}
			functional, err := checkDutyFunction(ctx, tx, record, rule.FunctionCheck, functionSHA)
			if err != nil {
				return nil, err
			}
			details = append(details, functional)
			check.Reason = "Inbetriebnahme, Einweisung und besondere Funktionsprüfung separat vorgeprüft; fachliche Eignung und Endanwenderanforderungen bleiben abzunehmen."
			for _, detail := range details {
				if detail.Status == "blocked" {
					check.Status = "blocked"
				}
			}
			checks = append(checks, details...)
		}
		checks = append(checks, check)
	}
	return checks, nil
}
