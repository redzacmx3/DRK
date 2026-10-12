package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"
)

type dutyControlAcceptance struct {
	dutyFunctionAcceptance
	InspectionVersion int    `json:"inspection_version"`
	DocumentID        string `json:"document_id"`
	ExpectedSnapshot  string `json:"expected_snapshot"`
	ScopeEvidence     string `json:"scope_evidence"`
	ExecutorEvidence  string `json:"executor_evidence"`
	MarkingEvidence   string `json:"marking_evidence"`
}
type dutyMTKAcceptance = dutyControlAcceptance
type dutySTKAcceptance = dutyControlAcceptance

type dutyInspectionChoice struct {
	Version     int    `json:"version"`
	ControlType string `json:"control_type"`
	PerformedOn string `json:"performed_on"`
	Result      string `json:"result"`
	Examiner    string `json:"examiner"`
	Method      string `json:"method"`
}

func dutyInspectionChoices(ctx context.Context, tx *sql.Tx, id string) ([]dutyInspectionChoice, error) {
	choices := []dutyInspectionChoice{}
	rows, err := tx.QueryContext(ctx, `SELECT version,payload_json FROM device_control_events WHERE device_id=? AND action='inspection' ORDER BY version DESC`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var p dutyInspectionChoice
		var payload string
		if err := rows.Scan(&p.Version, &payload); err != nil {
			return nil, err
		}
		var in deviceControlInput
		if err := json.Unmarshal([]byte(payload), &in); err != nil {
			return nil, err
		}
		p.ControlType = in.ControlType
		p.PerformedOn = in.PerformedOn
		p.Result = in.Result
		p.Examiner = in.Examiner
		p.Method = in.Method
		choices = append(choices, p)
	}
	return choices, rows.Err()
}
func checkDutyControlAcceptance(ctx context.Context, tx *sql.Tx, record deviceRecord, a *dutyControlAcceptance, source string, basis *dutyBookBasis, controlType, detailKind string) (productDutyCheck, error) {
	check := productDutyCheck{Kind: detailKind, Status: "unknown", Reason: controlType + "-Prüfung, Protokoll und fachliche Teilabnahme offen."}
	if a == nil {
		return check, nil
	}
	check.EventVersion = a.InspectionVersion
	if a.Decision == "rejected" {
		check.Status = "blocked"
		check.Reason = controlType + " ausdrücklich fachlich nicht abgenommen."
		return check, nil
	}
	if len(record.ActiveBlocks) > 0 || record.Decommission != nil {
		check.Status = "blocked"
		check.Reason = "Gerät gesperrt oder außer Betrieb; keine positive " + controlType + "-Teilabnahme."
		return check, nil
	}
	choices, err := dutyInspectionChoices(ctx, tx, record.ID)
	if err != nil {
		return check, err
	}
	var selected *dutyInspectionChoice
	for i := range choices {
		if choices[i].Version == a.InspectionVersion && choices[i].ControlType == controlType {
			selected = &choices[i]
		}
	}
	if selected == nil {
		return check, nil
	}
	if selected.Result != "passed" {
		check.Status = "blocked"
		check.Reason = "Zugeordnete " + controlType + " nicht bestanden."
		return check, nil
	}
	control, ok := record.EffectiveRules[controlType]
	if !ok || control.Applicability != "required" || record.PassedVersions[controlType] != a.InspectionVersion {
		check.Reason = controlType + "-Pflicht oder neueste bestandene Prüfung nicht aktuell zugeordnet."
		return check, nil
	}
	state := record.Controls[controlType].State
	if state == "OVERDUE" {
		check.Status = "blocked"
		check.Reason = controlType + "-Kontrollfrist überschritten."
		return check, nil
	}
	if state != "CURRENT" && state != "DUE_SOON" {
		check.Reason = controlType + "-Kontrollfrist ungeklärt."
		return check, nil
	}
	for _, correction := range record.Corrections {
		if correction.Target.Version == a.InspectionVersion {
			check.Reason = controlType + "-Prüfung richtiggestellt; neuen Nachweis und Abnahme erfassen."
			return check, nil
		}
	}
	if basis == nil {
		check.Status = "record_found"
		check.Reason = "Aktuelle bestandene " + controlType + " gefunden; qualifizierte Abnahme offen."
		return check, nil
	}
	// Reuse the opaque device-book snapshot and document epoch binding, without asserting book completeness.
	bookRule := productDutyRule{BookAcceptance: &dutyBookAcceptance{Reviewed: a.Reviewed, Decision: a.Decision, DocumentID: a.DocumentID}}
	if err := checkDutyBookAcceptance(ctx, tx, record, source, bookRule, basis, &check); err != nil {
		return check, err
	}
	if check.Status == "unknown" {
		check.Reason = "Aktenstand oder Dokumentprüfstand seit " + controlType + "-Abnahme geändert; erneut fachlich prüfen."
	}
	if check.Status == "accepted" {
		check.Reason = controlType + " mit Protokoll, Prüfumfang, Prüferbefähigung und Kennzeichnung ausdrücklich fachlich abgenommen; Gesamtfreigabe offen."
	}
	return check, nil
}
func validateDutyControlAcceptance(ctx context.Context, tx *sql.Tx, record deviceRecord, rule productDutyRule, a *dutyControlAcceptance, source string, now time.Time, controlType, detailKind, ruleKind, prefix string) (*dutyBookBasis, error) {
	if a == nil {
		return nil, nil
	}
	if rule.Kind != ruleKind || rule.Applicability != "required" || a.InspectionVersion <= 0 {
		return nil, adminInvalid(controlType + "-Teilabnahme nur im zugehörigen erforderlichen Pflichtbereich mit konkreter Prüfung erfassen.")
	}
	choices, err := dutyInspectionChoices(ctx, tx, record.ID)
	if err != nil {
		return nil, err
	}
	var selected *dutyInspectionChoice
	for i := range choices {
		if choices[i].Version == a.InspectionVersion && choices[i].ControlType == controlType {
			selected = &choices[i]
		}
	}
	if selected == nil {
		return nil, adminInvalid(controlType + "-Prüfung derselben Geräteakte auswählen; andere Kontrollarten ersetzen sie nicht.")
	}
	day, err := time.Parse("2006-01-02", a.PerformedOn)
	if err != nil || day.Format("2006-01-02") != a.PerformedOn || a.PerformedOn < selected.PerformedOn || a.PerformedOn > now.UTC().Format("2006-01-02") || !a.Reviewed || (a.Decision != "accepted" && a.Decision != "rejected") {
		return nil, adminInvalid(controlType + "-Abnahme ausdrücklich bestätigen: Entscheidung und Datum ab Prüftag bis heute erforderlich.")
	}
	for _, value := range []string{a.Qualification, a.Assignment, a.Evidence, a.Note, a.ScopeEvidence, a.ExecutorEvidence, a.MarkingEvidence} {
		if !validReason(value) {
			return nil, adminInvalid("Abnahmekompetenz, Beauftragung, Nachweis, Begründung, Prüfumfang, Prüferbefähigung und Kennzeichnung vollständig belegen.")
		}
	}
	if a.Decision == "accepted" {
		check, err := checkDutyControlAcceptance(ctx, tx, record, a, source, nil, controlType, detailKind)
		if err != nil {
			return nil, err
		}
		if check.Status != "record_found" {
			return nil, conflict(prefix+"_acceptance_not_current", "Keine positive "+controlType+"-Teilabnahme bei fehlgeschlagener, veralteter, gesperrter oder überfälliger Prüfung.")
		}
	}
	return captureDutyDocumentSnapshot(ctx, tx, record, source, a.DocumentID, a.ExpectedSnapshot, prefix)
}
func checkDutyMTK(ctx context.Context, tx *sql.Tx, record deviceRecord, rule productDutyRule, source string, basis *dutyBookBasis) (productDutyCheck, error) {
	return checkDutyControlAcceptance(ctx, tx, record, rule.MTKAcceptance, source, basis, "MTK", "annex_2_mtk")
}
func validateDutyMTK(ctx context.Context, tx *sql.Tx, record deviceRecord, rule productDutyRule, source string, now time.Time) (*dutyBookBasis, error) {
	return validateDutyControlAcceptance(ctx, tx, record, rule, rule.MTKAcceptance, source, now, "MTK", "annex_2_mtk", "annex_2", "mtk")
}
func checkDutySTK(ctx context.Context, tx *sql.Tx, record deviceRecord, rule productDutyRule, source string, basis *dutyBookBasis) (productDutyCheck, error) {
	return checkDutyControlAcceptance(ctx, tx, record, rule.STKAcceptance, source, basis, "STK", "annex_1_stk")
}
func validateDutySTK(ctx context.Context, tx *sql.Tx, record deviceRecord, rule productDutyRule, source string, now time.Time) (*dutyBookBasis, error) {
	return validateDutyControlAcceptance(ctx, tx, record, rule, rule.STKAcceptance, source, now, "STK", "annex_1_stk", "annex_1", "stk")
}
