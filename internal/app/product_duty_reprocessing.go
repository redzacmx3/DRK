package app

import (
	"context"
	"database/sql"
	"time"
)

// Professional assessment of an external reprocessing process, never a cycle release.
type dutyReprocessingAcceptance struct {
	dutyFunctionAcceptance
	ExpectedSnapshot         string `json:"expected_snapshot"`
	DocumentID               string `json:"document_id"`
	ProcessDescription       string `json:"process_description"`
	ProcessVersion           string `json:"process_version"`
	ManufacturerEvidence     string `json:"manufacturer_evidence"`
	ClassificationEvidence   string `json:"classification_evidence"`
	ValidationEvidence       string `json:"validation_evidence"`
	ExecutorEvidence         string `json:"executor_evidence"`
	ReleaseProcedureEvidence string `json:"release_procedure_evidence"`
	StorageEvidence          string `json:"storage_evidence"`
	RequirementsMet          *bool  `json:"requirements_met"`
	ProcessValidated         *bool  `json:"process_validated"`
}

func validateDutyReprocessing(ctx context.Context, tx *sql.Tx, record deviceRecord, rule productDutyRule, source string, now time.Time) (*dutyBookBasis, error) {
	a := rule.ReprocessingAcceptance
	if a == nil {
		return nil, nil
	}
	if rule.Kind != "reprocessing" || rule.Applicability != "required" {
		return nil, adminInvalid("Prozessabnahme nur für ausdrücklich erforderliche Aufbereitung erfassen.")
	}
	day, err := time.Parse("2006-01-02", a.PerformedOn)
	if err != nil || day.Format("2006-01-02") != a.PerformedOn || a.PerformedOn > now.UTC().Format("2006-01-02") || !a.Reviewed || a.RequirementsMet == nil || a.ProcessValidated == nil || (a.Decision != "accepted" && a.Decision != "rejected") {
		return nil, adminInvalid("Aufbereitungsprozessbewertung ausdrücklich bestätigen: Entscheidung, gültiges Datum bis heute, erfüllte Bedingungen und Validierung des Verfahrens jeweils ausdrücklich angeben.")
	}
	for _, value := range []string{a.Qualification, a.Assignment, a.Evidence, a.Note, a.ProcessDescription, a.ProcessVersion, a.ManufacturerEvidence, a.ClassificationEvidence, a.ValidationEvidence, a.ExecutorEvidence, a.ReleaseProcedureEvidence, a.StorageEvidence} {
		if !validReason(value) {
			return nil, adminInvalid("Abnahmekompetenz, Beauftragung, Nachweis, Begründung, Prozess, Verfahrensversion, Herstellervorgaben, Klassifizierung, Validierung, Ausführendenbefähigung, Freigabeverfahren und Lagerung vollständig belegen.")
		}
	}
	if a.Decision == "accepted" {
		if !*a.RequirementsMet || !*a.ProcessValidated {
			return nil, adminInvalid("Nicht erfüllte Bedingungen oder fehlende Prozessvalidierung erlauben keine positive Aufbereitungsprozessabnahme.")
		}
		if len(record.ActiveBlocks) > 0 || record.Decommission != nil {
			return nil, conflict("reprocessing_acceptance_not_current", "Gerät gesperrt oder außer Betrieb; keine positive Aufbereitungsprozessabnahme.")
		}
	}
	return captureDutyDocumentSnapshot(ctx, tx, record, source, a.DocumentID, a.ExpectedSnapshot, "reprocessing")
}
func checkDutyReprocessing(ctx context.Context, tx *sql.Tx, record deviceRecord, rule productDutyRule, source string, basis *dutyBookBasis, check *productDutyCheck) error {
	a := rule.ReprocessingAcceptance
	if a == nil {
		check.Reason = "Aufbereitungsprozess, Validierung und fachliche Eignungsbewertung offen."
		return nil
	}
	if a.Decision == "rejected" || a.RequirementsMet != nil && !*a.RequirementsMet || a.ProcessValidated != nil && !*a.ProcessValidated {
		check.Status = "blocked"
		check.Reason = "Aufbereitungsbedingungen nicht erfüllt, Prozess nicht validiert oder fachliche Abnahme abgelehnt."
		return nil
	}
	if len(record.ActiveBlocks) > 0 || record.Decommission != nil {
		check.Status = "blocked"
		check.Reason = "Gerät gesperrt oder außer Betrieb; Prozessbewertung hebt keine Sperre auf."
		return nil
	}
	if !a.Reviewed || a.Decision != "accepted" || a.RequirementsMet == nil || a.ProcessValidated == nil || basis == nil {
		return nil
	}
	candidate := productDutyRule{BookAcceptance: &dutyBookAcceptance{Reviewed: true, Decision: "accepted", DocumentID: a.DocumentID}}
	if err := checkDutyBookAcceptance(ctx, tx, record, source, candidate, basis, check); err != nil {
		return err
	}
	if check.Status == "accepted" {
		check.Reason = "Aufbereitungsprozess und Validierung ausdrücklich fachlich bewertet; konkrete Zyklen, Chargen-/Sterilgutstatus und Freigabe jedes Aufbereitungsergebnisses bleiben offen."
	} else if check.Status == "unknown" {
		check.Reason = "Aktenstand oder Dokumentprüfung seit Aufbereitungsprozessbewertung geändert; erneut fachlich prüfen."
	}
	return nil
}
