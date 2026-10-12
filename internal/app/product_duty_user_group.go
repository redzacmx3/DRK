package app

import (
	"context"
	"database/sql"
	"time"
)

// Organizational scope attestation; never grants roles or individual eligibility.
type dutyUserGroupAcceptance struct {
	dutyFunctionAcceptance
	ExpectedSnapshot   string `json:"expected_snapshot"`
	DocumentID         string `json:"document_id"`
	GroupDescription   string `json:"group_description"`
	UseScope           string `json:"use_scope"`
	ConditionsEvidence string `json:"conditions_evidence"`
	ExceptionEvidence  string `json:"exception_evidence"`
	RequirementsMet    *bool  `json:"requirements_met"`
	ExceptionUsed      *bool  `json:"exception_used"`
}

func validateDutyUserGroup(ctx context.Context, tx *sql.Tx, record deviceRecord, rule productDutyRule, source string, now time.Time) (*dutyBookBasis, error) {
	a := rule.UserGroupAcceptance
	if a == nil {
		return nil, nil
	}
	if rule.Kind != "special_user_group" || rule.Applicability != "required" {
		return nil, adminInvalid("Anwendergruppenabnahme nur für ausdrücklich erforderliche besondere Anwendergruppen erfassen.")
	}
	day, err := time.Parse("2006-01-02", a.PerformedOn)
	if err != nil || day.Format("2006-01-02") != a.PerformedOn || a.PerformedOn > now.UTC().Format("2006-01-02") || !a.Reviewed || a.RequirementsMet == nil || a.ExceptionUsed == nil || (a.Decision != "accepted" && a.Decision != "rejected") {
		return nil, adminInvalid("Anwendergruppenbewertung ausdrücklich bestätigen: Entscheidung, gültiges Datum bis heute, erfüllte Bedingungen und Verwendung einer Ausnahme jeweils ausdrücklich angeben.")
	}
	for _, value := range []string{a.Qualification, a.Assignment, a.Evidence, a.Note, a.GroupDescription, a.UseScope, a.ConditionsEvidence, a.ExceptionEvidence} {
		if !validReason(value) {
			return nil, adminInvalid("Abnahmekompetenz, Beauftragung, Nachweis, Begründung, Personenkreis, Nutzungsumfang, Bedingungen und Ausnahmegrundlage beziehungsweise begründeten Nichtbedarf vollständig belegen.")
		}
	}
	if a.Decision == "accepted" {
		if !*a.RequirementsMet {
			return nil, adminInvalid("Nicht erfüllte Bedingungen erlauben keine positive Anwendergruppenabnahme.")
		}
		if len(record.ActiveBlocks) > 0 || record.Decommission != nil {
			return nil, conflict("user_group_acceptance_not_current", "Gerät gesperrt oder außer Betrieb; keine positive Anwendergruppenabnahme.")
		}
	}
	return captureDutyDocumentSnapshot(ctx, tx, record, source, a.DocumentID, a.ExpectedSnapshot, "user_group")
}
func checkDutyUserGroup(ctx context.Context, tx *sql.Tx, record deviceRecord, rule productDutyRule, source string, basis *dutyBookBasis, check *productDutyCheck) error {
	a := rule.UserGroupAcceptance
	if a == nil {
		check.Reason = "Besonderer Personenkreis, Nutzungsumfang, Bedingungen und mögliche Ausnahmen noch nicht ausdrücklich fachlich abgenommen."
		return nil
	}
	if a.Decision == "rejected" || a.RequirementsMet != nil && !*a.RequirementsMet {
		check.Status = "blocked"
		check.Reason = "Anwendergruppenbedingungen nicht erfüllt oder fachliche Abnahme abgelehnt."
		return nil
	}
	if len(record.ActiveBlocks) > 0 || record.Decommission != nil {
		check.Status = "blocked"
		check.Reason = "Gerät gesperrt oder außer Betrieb; Gruppenbewertung hebt keine Sperre auf."
		return nil
	}
	if !a.Reviewed || a.Decision != "accepted" || a.RequirementsMet == nil || a.ExceptionUsed == nil || basis == nil {
		return nil
	}
	candidate := productDutyRule{BookAcceptance: &dutyBookAcceptance{Reviewed: true, Decision: "accepted", DocumentID: a.DocumentID}}
	if err := checkDutyBookAcceptance(ctx, tx, record, source, candidate, basis, check); err != nil {
		return err
	}
	if check.Status == "accepted" {
		check.Reason = "Besonderer Personenkreis, Nutzungsumfang, Bedingungen und Ausnahmegrundlage ausdrücklich fachlich bewertet; individuelle Eignung, Kontorechte und Gesamtfreigabe bleiben getrennt."
	} else if check.Status == "unknown" {
		check.Reason = "Aktenstand oder Dokumentprüfung seit Anwendergruppenbewertung geändert; erneut fachlich prüfen."
	}
	return nil
}
