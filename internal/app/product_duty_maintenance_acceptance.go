package app

import (
	"context"
	"database/sql"
	"time"
)

// Explicit manufacturer requirements and qualified acceptance; no inferred interval.
type dutyMaintenanceAcceptance struct {
	Reviewed         bool   `json:"reviewed"`
	Decision         string `json:"decision"`
	PerformedOn      string `json:"performed_on"`
	NextDueOn        string `json:"next_due_on"`
	Qualification    string `json:"qualification"`
	Assignment       string `json:"assignment"`
	Evidence         string `json:"evidence"`
	Note             string `json:"note"`
	IntervalEvidence string `json:"interval_evidence"`
	ScopeEvidence    string `json:"scope_evidence"`
	ExecutorEvidence string `json:"executor_evidence"`
}
type dutyMaintenanceBasis struct {
	SourceDocumentID      string `json:"source_document_id"`
	SourceReviewVersion   int    `json:"source_review_version"`
	ProtocolReviewVersion int    `json:"protocol_review_version"`
}

func validateDutyMaintenanceAcceptance(ctx context.Context, tx *sql.Tx, rule productDutyRule, record deviceRecord, source string, now time.Time) (*dutyMaintenanceBasis, error) {
	a := rule.MaintenanceAcceptance
	if a == nil {
		return nil, nil
	}
	if rule.Kind != "manufacturer_maintenance" || rule.Applicability != "required" || rule.EventVersion <= 0 {
		return nil, adminInvalid("Wartungsabnahme benötigt eine erforderliche Herstellerwartung mit konkretem Wartungsereignis.")
	}
	var proof *maintenanceProof
	for i := range record.Maintenances {
		if record.Maintenances[i].Version == rule.EventVersion && record.Maintenances[i].Kind == "maintenance" {
			proof = &record.Maintenances[i]
		}
	}
	if proof == nil {
		return nil, adminInvalid("Wartungsereignis derselben Geräteakte auswählen.")
	}
	day, err := time.Parse("2006-01-02", a.PerformedOn)
	if err != nil || day.Format("2006-01-02") != a.PerformedOn || a.PerformedOn < proof.PerformedOn || a.PerformedOn > now.UTC().Format("2006-01-02") || !a.Reviewed || (a.Decision != "accepted" && a.Decision != "rejected") {
		return nil, adminInvalid("Wartungsabnahme ausdrücklich bestätigen und gültiges Datum ab Wartungstag bis heute angeben.")
	}
	for _, value := range []string{a.Qualification, a.Assignment, a.Evidence, a.Note, a.IntervalEvidence, a.ScopeEvidence, a.ExecutorEvidence} {
		if !validReason(value) {
			return nil, adminInvalid("Abnahmekompetenz, Beauftragung, Nachweis, Begründung sowie Herstellerintervall, Wartungsumfang und Ausführendenbefähigung vollständig belegen.")
		}
	}
	due, err := time.Parse("2006-01-02", a.NextDueOn)
	if err != nil || due.Format("2006-01-02") != a.NextDueOn || a.NextDueOn < a.PerformedOn {
		return nil, adminInvalid("Nächste Fälligkeit aus bestätigten Herstellervorgaben ausdrücklich als Datum ab Abnahmetag angeben.")
	}
	if a.Decision == "accepted" {
		candidate := rule
		candidate.MaintenanceAcceptance = nil
		checks, err := checkProductDutyProofs(ctx, tx, record, []productDutyRule{candidate}, "", nil)
		if err != nil {
			return nil, err
		}
		if checks[0].Status != "record_found" || a.NextDueOn < now.UTC().Format("2006-01-02") {
			return nil, conflict("maintenance_acceptance_not_current", "Keine positive Wartungsabnahme bei offenem, veraltetem, gesperrtem oder überfälligem Nachweis.")
		}
	}
	basis := &dutyMaintenanceBasis{SourceDocumentID: source}
	var status string
	if err := tx.QueryRowContext(ctx, `SELECT status,review_version FROM effective_documents WHERE id=? AND device_id=?`, source, record.ID).Scan(&status, &basis.SourceReviewVersion); err != nil {
		return nil, err
	}
	if status != "released" {
		return nil, conflict("product_source_not_released", "Herstellervorgaben nicht freigegeben.")
	}
	if proof.DocumentID != "" {
		if err := tx.QueryRowContext(ctx, `SELECT review_version FROM effective_documents WHERE id=? AND device_id=?`, proof.DocumentID, record.ID).Scan(&basis.ProtocolReviewVersion); err != nil {
			return nil, err
		}
	}
	return basis, nil
}
func applyDutyMaintenanceAcceptance(ctx context.Context, tx *sql.Tx, record deviceRecord, rule productDutyRule, basis *dutyMaintenanceBasis, check *productDutyCheck) error {
	a := rule.MaintenanceAcceptance
	if a.Decision == "rejected" {
		check.Status = "blocked"
		check.Reason = "Fachliche Wartungsabnahme ausdrücklich abgelehnt."
		return nil
	}
	if check.Status != "record_found" {
		return nil
	}
	if !a.Reviewed || a.Decision != "accepted" || basis == nil || basis.SourceReviewVersion <= 0 || basis.ProtocolReviewVersion <= 0 {
		check.Status = "unknown"
		check.Reason = "Aktueller Nachweisschnappschuss der Wartungsabnahme fehlt."
		return nil
	}
	if a.NextDueOn < time.Now().UTC().Format("2006-01-02") {
		check.Status = "blocked"
		check.Reason = "Bestätigte Herstellerwartungsfälligkeit überschritten."
		return nil
	}
	var protocol string
	for _, proof := range record.Maintenances {
		if proof.Version == rule.EventVersion {
			protocol = proof.DocumentID
		}
	}
	for _, source := range []struct {
		id      string
		version int
	}{{basis.SourceDocumentID, basis.SourceReviewVersion}, {protocol, basis.ProtocolReviewVersion}} {
		var status string
		var version int
		err := tx.QueryRowContext(ctx, `SELECT status,review_version FROM effective_documents WHERE id=? AND device_id=?`, source.id, record.ID).Scan(&status, &version)
		if err != nil && err != sql.ErrNoRows {
			return err
		}
		if err == sql.ErrNoRows || status != "released" || version != source.version {
			check.Status = "unknown"
			check.Reason = "Herstellervorgabe oder Wartungsprotokoll neu geprüft beziehungsweise entzogen; erneute fachliche Abnahme erforderlich."
			return nil
		}
	}
	check.Status = "accepted"
	check.Reason = "Herstellerintervall, bestätigte nächste Fälligkeit, Wartungsumfang und Ausführendenbefähigung ausdrücklich fachlich abgenommen; Gesamtfreigabe offen."
	return nil
}
