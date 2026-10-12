package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"
)

type dutyMTKAcceptance struct {
	dutyFunctionAcceptance
	InspectionVersion int    `json:"inspection_version"`
	DocumentID        string `json:"document_id"`
	ExpectedSnapshot  string `json:"expected_snapshot"`
	ScopeEvidence     string `json:"scope_evidence"`
	ExecutorEvidence  string `json:"executor_evidence"`
	MarkingEvidence   string `json:"marking_evidence"`
}
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
func checkDutyMTK(ctx context.Context, tx *sql.Tx, record deviceRecord, rule productDutyRule, source string, basis *dutyBookBasis) (productDutyCheck, error) {
	check := productDutyCheck{Kind: "annex_2_mtk", Status: "unknown", Reason: "Konkrete MTK-Prüfung, Protokoll und fachliche Teilabnahme offen."}
	a := rule.MTKAcceptance
	if a == nil {
		return check, nil
	}
	check.EventVersion = a.InspectionVersion
	if a.Decision == "rejected" {
		check.Status = "blocked"
		check.Reason = "Messtechnische Kontrolle ausdrücklich fachlich nicht abgenommen."
		return check, nil
	}
	if len(record.ActiveBlocks) > 0 || record.Decommission != nil {
		check.Status = "blocked"
		check.Reason = "Gerät gesperrt oder außer Betrieb; keine positive MTK-Teilabnahme."
		return check, nil
	}
	choices, err := dutyInspectionChoices(ctx, tx, record.ID)
	if err != nil {
		return check, err
	}
	var selected *dutyInspectionChoice
	for i := range choices {
		if choices[i].Version == a.InspectionVersion && choices[i].ControlType == "MTK" {
			selected = &choices[i]
		}
	}
	if selected == nil {
		return check, nil
	}
	if selected.Result != "passed" {
		check.Status = "blocked"
		check.Reason = "Zugeordnete messtechnische Kontrolle nicht bestanden."
		return check, nil
	}
	control, ok := record.EffectiveRules["MTK"]
	if !ok || control.Applicability != "required" || record.PassedVersions["MTK"] != a.InspectionVersion {
		check.Reason = "MTK-Pflicht oder neueste bestandene MTK nicht aktuell zugeordnet."
		return check, nil
	}
	state := record.Controls["MTK"].State
	if state == "OVERDUE" {
		check.Status = "blocked"
		check.Reason = "Messtechnische Kontrollfrist überschritten."
		return check, nil
	}
	if state != "CURRENT" && state != "DUE_SOON" {
		check.Reason = "Messtechnische Kontrollfrist ungeklärt."
		return check, nil
	}
	for _, correction := range record.Corrections {
		if correction.Target.Version == a.InspectionVersion {
			check.Reason = "MTK-Prüfung richtiggestellt; neuen Nachweis und Abnahme erfassen."
			return check, nil
		}
	}
	if basis == nil {
		check.Status = "record_found"
		check.Reason = "Aktuelle bestandene MTK gefunden; qualifizierte Abnahme offen."
		return check, nil
	}
	// Reuse the opaque device-book snapshot and document epoch binding, without asserting book completeness.
	bookRule := productDutyRule{BookAcceptance: &dutyBookAcceptance{Reviewed: a.Reviewed, Decision: a.Decision, DocumentID: a.DocumentID}}
	if err := checkDutyBookAcceptance(ctx, tx, record, source, bookRule, basis, &check); err != nil {
		return check, err
	}
	if check.Status == "unknown" {
		check.Reason = "Aktenstand oder Dokumentprüfstand seit MTK-Abnahme geändert; erneut fachlich prüfen."
	}
	if check.Status == "accepted" {
		check.Reason = "Messtechnische Kontrolle mit Protokoll, Prüfumfang, Prüferbefähigung und Kennzeichnung ausdrücklich fachlich abgenommen; Gesamtfreigabe offen."
	}
	return check, nil
}
func validateDutyMTK(ctx context.Context, tx *sql.Tx, record deviceRecord, rule productDutyRule, source string, now time.Time) (*dutyBookBasis, error) {
	a := rule.MTKAcceptance
	if a == nil {
		return nil, nil
	}
	if rule.Kind != "annex_2" || rule.Applicability != "required" || a.InspectionVersion <= 0 {
		return nil, adminInvalid("MTK-Teilabnahme nur für erforderliche Anlage 2 mit konkreter MTK-Prüfung erfassen.")
	}
	choices, err := dutyInspectionChoices(ctx, tx, record.ID)
	if err != nil {
		return nil, err
	}
	var selected *dutyInspectionChoice
	for i := range choices {
		if choices[i].Version == a.InspectionVersion && choices[i].ControlType == "MTK" {
			selected = &choices[i]
		}
	}
	if selected == nil {
		return nil, adminInvalid("MTK-Prüfung derselben Geräteakte auswählen; STK oder elektrische Prüfung ersetzt sie nicht.")
	}
	day, err := time.Parse("2006-01-02", a.PerformedOn)
	if err != nil || day.Format("2006-01-02") != a.PerformedOn || a.PerformedOn < selected.PerformedOn || a.PerformedOn > now.UTC().Format("2006-01-02") || !a.Reviewed || (a.Decision != "accepted" && a.Decision != "rejected") {
		return nil, adminInvalid("MTK-Abnahme ausdrücklich bestätigen: Entscheidung und Datum ab Prüftag bis heute erforderlich.")
	}
	for _, value := range []string{a.Qualification, a.Assignment, a.Evidence, a.Note, a.ScopeEvidence, a.ExecutorEvidence, a.MarkingEvidence} {
		if !validReason(value) {
			return nil, adminInvalid("Abnahmekompetenz, Beauftragung, Nachweis, Begründung, Prüfumfang, Prüferbefähigung und Kennzeichnung vollständig belegen.")
		}
	}
	if a.Decision == "accepted" {
		check, err := checkDutyMTK(ctx, tx, record, rule, source, nil)
		if err != nil {
			return nil, err
		}
		if check.Status != "record_found" {
			return nil, conflict("mtk_acceptance_not_current", "Keine positive MTK-Teilabnahme bei fehlgeschlagener, veralteter, gesperrter oder überfälliger Prüfung.")
		}
	}
	return captureDutyDocumentSnapshot(ctx, tx, record, source, a.DocumentID, a.ExpectedSnapshot, "mtk")
}
