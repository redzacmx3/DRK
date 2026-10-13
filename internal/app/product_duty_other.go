package app

import (
	"context"
	"database/sql"
	"time"
)

// Explicit externally assessed obligations. Neither the list nor its acceptance
// is inferred from product names or substitutes for the dedicated duty modules.
type dutyOtherRequirement struct {
	Code        string `json:"code"`
	Description string `json:"description"`
	Status      string `json:"status"`
	Evidence    string `json:"evidence"`
	Reason      string `json:"reason"`
}

type dutyOtherAcceptance struct {
	dutyFunctionAcceptance
	ExpectedSnapshot     string                 `json:"expected_snapshot"`
	DocumentID           string                 `json:"document_id"`
	Scope                string                 `json:"scope"`
	CompletenessEvidence string                 `json:"completeness_evidence"`
	ScopeComplete        *bool                  `json:"scope_complete"`
	Requirements         []dutyOtherRequirement `json:"requirements"`
}

func otherRequirementsState(a *dutyOtherAcceptance) string {
	if a == nil || len(a.Requirements) < 1 || len(a.Requirements) > 12 {
		return "unknown"
	}
	state := "accepted"
	seen := map[string]bool{}
	for _, requirement := range a.Requirements {
		if !usageCode.MatchString(requirement.Code) || seen[requirement.Code] || !validReason(requirement.Description) || !validReason(requirement.Evidence) || !validReason(requirement.Reason) {
			return "unknown"
		}
		seen[requirement.Code] = true
		switch requirement.Status {
		case "met", "not_applicable":
		case "not_met":
			state = "blocked"
		case "unknown":
			if state != "blocked" {
				state = "unknown"
			}
		default:
			return "unknown"
		}
	}
	return state
}

func validateDutyOther(ctx context.Context, tx *sql.Tx, record deviceRecord, rule productDutyRule, source string, now time.Time) (*dutyBookBasis, error) {
	a := rule.OtherAcceptance
	if a == nil {
		return nil, nil
	}
	if rule.Kind != "other" || rule.Applicability != "required" {
		return nil, adminInvalid("Einzelpflichtenabnahme nur für ausdrücklich erforderliche weitere Produktpflichten erfassen.")
	}
	day, err := time.Parse("2006-01-02", a.PerformedOn)
	if err != nil || day.Format("2006-01-02") != a.PerformedOn || a.PerformedOn > now.UTC().Format("2006-01-02") || !a.Reviewed || a.ScopeComplete == nil || (a.Decision != "accepted" && a.Decision != "rejected") {
		return nil, adminInvalid("Entscheidung, Datum bis heute, Fachprüfung und Vollständigkeit des Umfangs ausdrücklich bestätigen.")
	}
	for _, value := range []string{a.Scope, a.CompletenessEvidence, a.Qualification, a.Assignment, a.Evidence, a.Note} {
		if !validReason(value) {
			return nil, adminInvalid("Pflichtumfang, Vollständigkeitsgrundlage, Abnahmekompetenz, Beauftragung, Nachweis und Begründung vollständig belegen.")
		}
	}
	if len(a.Requirements) < 1 || len(a.Requirements) > 12 {
		return nil, adminInvalid("Eine bis zwölf weitere Pflichten ausdrücklich erfassen.")
	}
	seen := map[string]bool{}
	for _, requirement := range a.Requirements {
		if !usageCode.MatchString(requirement.Code) || seen[requirement.Code] || !validReason(requirement.Description) || !validReason(requirement.Evidence) || !validReason(requirement.Reason) || (requirement.Status != "met" && requirement.Status != "not_met" && requirement.Status != "unknown" && requirement.Status != "not_applicable") {
			return nil, adminInvalid("Jede Einzelpflicht mit eindeutigem Code, Beschreibung, ausdrücklichem Erfüllungsstatus, Nachweis und Begründung erfassen; Ausnahmen fachlich belegen.")
		}
		seen[requirement.Code] = true
	}
	if a.Decision == "accepted" {
		if !*a.ScopeComplete || otherRequirementsState(a) != "accepted" {
			return nil, adminInvalid("Offene, unerfüllte oder unvollständig erfasste Pflichten erlauben keine positive Teilabnahme.")
		}
		if len(record.ActiveBlocks) > 0 || record.Decommission != nil {
			return nil, conflict("other_acceptance_not_current", "Gerät gesperrt oder außer Betrieb; keine positive Einzelpflichtenabnahme.")
		}
	}
	return captureDutyDocumentSnapshot(ctx, tx, record, source, a.DocumentID, a.ExpectedSnapshot, "other")
}

func checkDutyOther(ctx context.Context, tx *sql.Tx, record deviceRecord, rule productDutyRule, source string, basis *dutyBookBasis, check *productDutyCheck) error {
	a := rule.OtherAcceptance
	if a == nil {
		check.Reason = "Weitere produktspezifische Einzelpflichten, vollständiger Umfang und fachliche Teilabnahme offen."
		return nil
	}
	if a.Decision == "rejected" || otherRequirementsState(a) == "blocked" {
		check.Status = "blocked"
		check.Reason = "Weitere Einzelpflichten nicht erfüllt oder fachliche Teilabnahme ausdrücklich abgelehnt."
		return nil
	}
	if len(record.ActiveBlocks) > 0 || record.Decommission != nil {
		check.Status = "blocked"
		check.Reason = "Gerät gesperrt oder außer Betrieb; Einzelpflichtenabnahme hebt keine Sperre auf."
		return nil
	}
	if !a.Reviewed || a.Decision != "accepted" || a.ScopeComplete == nil || !*a.ScopeComplete || otherRequirementsState(a) != "accepted" || basis == nil {
		return nil
	}
	candidate := productDutyRule{BookAcceptance: &dutyBookAcceptance{Reviewed: true, Decision: "accepted", DocumentID: a.DocumentID}}
	if err := checkDutyBookAcceptance(ctx, tx, record, source, candidate, basis, check); err != nil {
		return err
	}
	if check.Status == "accepted" {
		check.Reason = "Explizit erfasste weitere Einzelpflichten und vollständiger Umfang fachlich teilabgenommen; elektrische Sicherheit und Gesamtfreigabe bleiben getrennt."
	} else if check.Status == "unknown" {
		check.Reason = "Aktenstand oder Dokumentprüfung seit Einzelpflichtenabnahme geändert; erneut prüfen."
	}
	return nil
}
