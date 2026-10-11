package app

import (
	"context"
	"database/sql"
	"time"
)

// An explicit protocol attestation, stored immutably with its applicability review.
// This is neither a pack pre-use check nor qualified acceptance of the device.
type dutyFunctionAcceptance struct {
	Reviewed      bool   `json:"reviewed"`
	Decision      string `json:"decision"`
	PerformedOn   string `json:"performed_on"`
	Qualification string `json:"qualification"`
	Assignment    string `json:"assignment"`
	Evidence      string `json:"evidence"`
	Note          string `json:"note"`
}

type dutyFunctionProof struct {
	Acceptance    *dutyFunctionAcceptance `json:"acceptance,omitempty"`
	Reviewed      bool                    `json:"reviewed"`
	PerformedOn   string                  `json:"performed_on"`
	LocationID    string                  `json:"location_id"`
	Examiner      string                  `json:"examiner"`
	Qualification string                  `json:"qualification"`
	Assignment    string                  `json:"assignment"`
	Method        string                  `json:"method"`
	Conditions    string                  `json:"conditions"`
	Result        string                  `json:"result"`
	Evidence      string                  `json:"evidence"`
	DocumentID    string                  `json:"document_id"`
}

func validateDutyFunction(ctx context.Context, tx *sql.Tx, rule productDutyRule, record deviceRecord, now time.Time) (string, error) {
	proof := rule.FunctionCheck
	if proof == nil {
		return "", nil
	}
	if rule.Kind != "annex_1" || rule.Applicability != "required" {
		return "", adminInvalid("Besondere Funktionsprüfung nur für erforderliche Anlage-1-Pflichten erfassen.")
	}
	date, err := time.Parse("2006-01-02", proof.PerformedOn)
	if err != nil || date.Format("2006-01-02") != proof.PerformedOn || proof.PerformedOn > now.UTC().Format("2006-01-02") || !proof.Reviewed || proof.LocationID != record.LocationID || (proof.Result != "passed" && proof.Result != "failed") {
		return "", adminInvalid("Prüfung ausdrücklich bestätigen: Datum nicht in der Zukunft, aktueller Betriebsort und bestanden/nicht bestanden erforderlich.")
	}
	for _, value := range []string{proof.Examiner, proof.Qualification, proof.Assignment, proof.Method, proof.Conditions, proof.Evidence} {
		if !validReason(value) {
			return "", adminInvalid("Prüfer, Qualifikation, Beauftragung, Verfahren, Bedingungen und Nachweis der Funktionsprüfung vollständig erfassen.")
		}
	}
	if acceptance := proof.Acceptance; acceptance != nil {
		date, err := time.Parse("2006-01-02", acceptance.PerformedOn)
		if err != nil || date.Format("2006-01-02") != acceptance.PerformedOn || acceptance.PerformedOn < proof.PerformedOn || acceptance.PerformedOn > now.UTC().Format("2006-01-02") || !acceptance.Reviewed || (acceptance.Decision != "accepted" && acceptance.Decision != "rejected") || (acceptance.Decision == "accepted" && proof.Result != "passed") {
			return "", adminInvalid("Abnahme ausdrücklich bestätigen: Entscheidung und Datum ab Prüfdatum, nicht in der Zukunft; fehlgeschlagene Prüfung nicht abnehmen.")
		}
		for _, value := range []string{acceptance.Qualification, acceptance.Assignment, acceptance.Evidence, acceptance.Note} {
			if !validReason(value) {
				return "", adminInvalid("Qualifikation, Beauftragung, Abnahmenachweis und Begründung vollständig erfassen.")
			}
		}
	}
	var status, sha string
	err = tx.QueryRowContext(ctx, `SELECT status,sha256 FROM effective_documents WHERE id=? AND device_id=?`, proof.DocumentID, record.ID).Scan(&status, &sha)
	if err == sql.ErrNoRows {
		return "", scopeError()
	}
	if err != nil {
		return "", err
	}
	if status != "released" {
		return "", conflict("function_protocol_not_released", "Prüfprotokoll der Funktionsprüfung nicht freigegeben.")
	}
	return sha, nil
}

func checkDutyFunction(ctx context.Context, tx *sql.Tx, record deviceRecord, proof *dutyFunctionProof, boundSHA string) (productDutyCheck, error) {
	check := productDutyCheck{Kind: "annex_1_function", Status: "unknown", Reason: "Keine besondere Funktionsprüfung am Betriebsort ausdrücklich erfasst; Inbetriebnahme und Packprüfung ersetzen sie nicht."}
	if proof == nil {
		return check, nil
	}
	if proof.Acceptance != nil && proof.Acceptance.Reviewed && proof.Acceptance.Decision == "rejected" {
		check.Status = "blocked"
		check.Reason = "Fachliche Abnahme der besonderen Funktionsprüfung ausdrücklich abgelehnt."
		return check, nil
	}
	if proof.Result == "failed" {
		check.Status = "blocked"
		check.Reason = "Besondere Funktionsprüfung nicht bestanden; keine positive Nachweisvorprüfung."
		return check, nil
	}
	if !proof.Reviewed || proof.Result != "passed" || proof.LocationID != record.LocationID || boundSHA == "" {
		check.Reason = "Funktionsnachweis unvollständig oder Betriebsort abweichend; erneut prüfen."
		return check, nil
	}
	if len(record.ActiveBlocks) > 0 || record.Decommission != nil {
		check.Status = "blocked"
		check.Reason = "Gerät aktuell gesperrt oder außer Betrieb; Funktionsnachweis hebt keine Sperre auf."
		return check, nil
	}
	var status, sha string
	err := tx.QueryRowContext(ctx, `SELECT status,sha256 FROM effective_documents WHERE id=? AND device_id=?`, proof.DocumentID, record.ID).Scan(&status, &sha)
	if err == sql.ErrNoRows {
		return check, nil
	}
	if err != nil {
		return check, err
	}
	if status != "released" || sha != boundSHA {
		check.Reason = "Gebundenes Funktionsprüfprotokoll aktuell nicht freigegeben oder Prüfsumme abweichend."
		return check, nil
	}
	check.Status = "record_found"
	check.Reason = "Ausdrücklicher Funktionsnachweis mit Betriebsort und freigegebenem Prüfprotokoll aufgelöst; Angaben zu Prüfer, Qualifikation und Verfahren sowie fachliche Abnahme bleiben zu bewerten."
	if proof.Acceptance != nil && proof.Acceptance.Reviewed && proof.Acceptance.Decision == "accepted" {
		check.Status = "accepted"
		check.Reason = "Besondere Funktionsprüfung ausdrücklich fachlich abgenommen; Standort- und Protokollbindung aktuell. Diese Teilabnahme bestätigt keine gesamte Einsatzfreigabe."
	}
	return check, nil
}
