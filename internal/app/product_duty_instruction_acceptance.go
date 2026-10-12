package app

import (
	"context"
	"database/sql"
	"time"
)

// Organizational suitability attestation, without publishing private usage history.
type dutyInstructionAcceptance struct {
	Reviewed        bool   `json:"reviewed"`
	Decision        string `json:"decision"`
	PerformedOn     string `json:"performed_on"`
	Qualification   string `json:"qualification"`
	Assignment      string `json:"assignment"`
	Evidence        string `json:"evidence"`
	Note            string `json:"note"`
	TrainerEvidence string `json:"trainer_evidence"`
	PersonEvidence  string `json:"person_evidence"`
	ScopeEvidence   string `json:"scope_evidence"`
}
type dutyInstructionBasis struct {
	BindingVersion        int `json:"binding_version"`
	DocumentReviewVersion int `json:"document_review_version"`
}

func validateDutyInstructionAcceptance(ctx context.Context, tx *sql.Tx, rule productDutyRule, record deviceRecord, now time.Time) (*dutyInstructionBasis, error) {
	acceptance := rule.InstructionAcceptance
	if acceptance == nil {
		return nil, nil
	}
	if rule.Kind != "annex_1" || rule.Applicability != "required" || rule.InstructionVersion <= 0 || rule.DesignatedUserID == "" || !validReason(rule.DesignatedAssignment) {
		return nil, adminInvalid("Einweisungsabnahme nur mit konkreter Einweisung und ausdrücklich benanntem Konto für erforderliche Anlage 1 erfassen.")
	}
	var selected *instructionProof
	for i := range record.Instructions {
		if record.Instructions[i].Version == rule.InstructionVersion {
			selected = &record.Instructions[i]
		}
	}
	if selected == nil {
		return nil, adminInvalid("Konkrete Einweisung derselben Geräteakte auswählen.")
	}
	date, err := time.Parse("2006-01-02", acceptance.PerformedOn)
	if err != nil || date.Format("2006-01-02") != acceptance.PerformedOn || acceptance.PerformedOn < selected.PerformedOn || acceptance.PerformedOn > now.UTC().Format("2006-01-02") || !acceptance.Reviewed || (acceptance.Decision != "accepted" && acceptance.Decision != "rejected") {
		return nil, adminInvalid("Einweisungsabnahme ausdrücklich bestätigen: Entscheidung und Datum ab Einweisungstag, nicht in der Zukunft erforderlich.")
	}
	for _, value := range []string{acceptance.Qualification, acceptance.Assignment, acceptance.Evidence, acceptance.Note, acceptance.TrainerEvidence, acceptance.PersonEvidence, acceptance.ScopeEvidence} {
		if !validReason(value) {
			return nil, adminInvalid("Abnahmekompetenz, Beauftragung, Nachweis, Begründung sowie Ausbilderbefähigung, Eignung der benannten Person und Einweisungsumfang vollständig erfassen.")
		}
	}
	if acceptance.Decision == "accepted" {
		candidate := rule
		candidate.InstructionAcceptance = nil
		checks, err := checkAnnex1Proofs(ctx, tx, record, candidate, nil)
		if err != nil {
			return nil, err
		}
		if checks[1].Status != "record_found" {
			return nil, conflict("instruction_acceptance_not_current", "Einweisung, benannte Person, Kontozuordnung oder Unterlage nicht aktuell; keine positive Teilabnahme möglich.")
		}
	}
	basis := &dutyInstructionBasis{}
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(version),0) FROM usage_events WHERE device_id=? AND subject_id=? AND action IN ('bind','unbind')`, record.ID, rule.DesignatedUserID).Scan(&basis.BindingVersion); err != nil {
		return nil, err
	}
	if err := tx.QueryRowContext(ctx, `SELECT review_version FROM effective_documents WHERE device_id=? AND id=?`, record.ID, selected.DocumentID).Scan(&basis.DocumentReviewVersion); err != nil {
		return nil, err
	}
	return basis, nil
}
