package app

import (
	"context"
	"database/sql"
	"time"
)

// Records a product-specific external inspection and its professional acceptance.
// Software identity is explicitly attested; the application does not scan installed software.
type dutySecurityAcceptance struct {
	MeasuresCompleted *bool `json:"measures_completed"`
	dutyFunctionAcceptance
	ExpectedSnapshot string `json:"expected_snapshot"`
	DocumentID       string `json:"document_id"`
	CheckPerformedOn string `json:"check_performed_on"`
	NextDueOn        string `json:"next_due_on"`
	SoftwareVersion  string `json:"software_version"`
	SystemScope      string `json:"system_scope"`
	Method           string `json:"method"`
	Examiner         string `json:"examiner"`
	Result           string `json:"result"`
	ExaminerEvidence string `json:"examiner_evidence"`
	FindingsEvidence string `json:"findings_evidence"`
	MeasuresEvidence string `json:"measures_evidence"`
}

func validateDutySecurity(ctx context.Context, tx *sql.Tx, record deviceRecord, rule productDutyRule, source string, now time.Time) (*dutyBookBasis, error) {
	a := rule.SecurityAcceptance
	if a == nil {
		return nil, nil
	}
	if rule.Kind != "it_security" || rule.Applicability != "required" {
		return nil, adminInvalid("IT-Sicherheitsabnahme nur bei ausdrücklich erforderlicher produktspezifischer IT-Sicherheitsprüfung erfassen.")
	}
	for _, value := range []string{a.Qualification, a.Assignment, a.Evidence, a.Note, a.SoftwareVersion, a.SystemScope, a.Method, a.Examiner, a.ExaminerEvidence, a.FindingsEvidence, a.MeasuresEvidence} {
		if !validReason(value) {
			return nil, adminInvalid("Abnahme, geprüften Softwarestand / Systemumfang, Verfahren, Prüferbefähigung, Befunde und Maßnahmen vollständig belegen; auch fehlende Befunde ausdrücklich begründen.")
		}
	}
	today := now.UTC().Format("2006-01-02")
	for _, value := range []string{a.PerformedOn, a.CheckPerformedOn, a.NextDueOn} {
		day, err := time.Parse("2006-01-02", value)
		if err != nil || day.Format("2006-01-02") != value {
			return nil, adminInvalid("Gültige Prüf-, Abnahme- und nächste Fälligkeitsdaten erforderlich.")
		}
	}
	if a.CheckPerformedOn > today || a.PerformedOn > today || a.PerformedOn < a.CheckPerformedOn || a.NextDueOn < a.PerformedOn || !a.Reviewed || a.MeasuresCompleted == nil || (a.Decision != "accepted" && a.Decision != "rejected") || (a.Result != "passed" && a.Result != "failed") {
		return nil, adminInvalid("Prüfung und Abnahme ausdrücklich mit Entscheidung bestätigen; Daten nicht zukünftig, Abnahme ab Prüftag und Folgefrist ab Abnahmetag.")
	}
	if a.Decision == "accepted" {
		if a.Result != "passed" || !*a.MeasuresCompleted {
			return nil, adminInvalid("Nicht bestandene IT-Sicherheitsprüfung oder offene Maßnahmen können nicht positiv abgenommen werden.")
		}
		if a.NextDueOn < today || len(record.ActiveBlocks) > 0 || record.Decommission != nil {
			return nil, conflict("security_acceptance_not_current", "Gerät gesperrt, außer Betrieb oder bestätigte IT-Sicherheitsfrist überschritten; keine positive Abnahme.")
		}
	}
	return captureDutyDocumentSnapshot(ctx, tx, record, source, a.DocumentID, a.ExpectedSnapshot, "security")
}
func checkDutySecurity(ctx context.Context, tx *sql.Tx, record deviceRecord, rule productDutyRule, source string, basis *dutyBookBasis, now time.Time, check *productDutyCheck) error {
	a := rule.SecurityAcceptance
	if a == nil {
		check.Reason = "Konkrete IT-Sicherheitsprüfung, Protokoll und fachliche Abnahme offen."
		return nil
	}
	if a.Decision == "rejected" || a.Result == "failed" || a.MeasuresCompleted != nil && !*a.MeasuresCompleted {
		check.Status = "blocked"
		check.Reason = "IT-Sicherheitsprüfung nicht bestanden, Maßnahmen offen oder fachliche Abnahme abgelehnt."
		return nil
	}
	if len(record.ActiveBlocks) > 0 || record.Decommission != nil {
		check.Status = "blocked"
		check.Reason = "Gerät gesperrt oder außer Betrieb; IT-Teilabnahme ermöglicht keine positive Bewertung."
		return nil
	}
	if a.NextDueOn < now.UTC().Format("2006-01-02") {
		check.Status = "blocked"
		check.Reason = "Bestätigte produktspezifische IT-Sicherheitsfrist überschritten."
		return nil
	}
	if !a.Reviewed || a.Decision != "accepted" || a.Result != "passed" || a.MeasuresCompleted == nil || basis == nil {
		return nil
	}
	candidate := productDutyRule{BookAcceptance: &dutyBookAcceptance{Reviewed: true, Decision: "accepted", DocumentID: a.DocumentID}}
	if err := checkDutyBookAcceptance(ctx, tx, record, source, candidate, basis, check); err != nil {
		return err
	}
	if check.Status == "accepted" {
		check.Reason = "IT-Sicherheitsprüfung für ausdrücklich dokumentierten Softwarestand und Systemumfang fachlich abgenommen; Gesamtfreigabe offen."
	} else if check.Status == "unknown" {
		check.Reason = "Aktenstand oder Dokumentprüfung seit IT-Sicherheitsabnahme geändert; erneut prüfen."
	}
	return nil
}
