package app

import (
	"context"
	"database/sql"
	"strconv"
	"time"
)

func validateDeviceAction(ctx context.Context, tx *sql.Tx, user, action string, input *deviceControlInput, record deviceRecord, now time.Time) error {
	bad := func(message string) error { return &stockError{400, "invalid_device_proof", message} }
	if len(input.Rules) != 0 {
		return bad("Prüfung verwendet ausschließlich die gespeicherte Kontrollregel.")
	}
	switch action {
	case "instruction":
		if input.ControlType != "" || input.Result != "" || input.Examiner != "" || input.Method != "" || input.NextDueOn != "" || input.BlockReason != "" || input.BlockID != "" {
			return bad("Einweisung enthält keine Prüf-/Sperrfelder.")
		}
		date, err := civilDate(input.PerformedOn)
		if err != nil {
			return bad("Gültiges Einweisungsdatum erforderlich.")
		}
		berlin, err := time.LoadLocation("Europe/Berlin")
		if err != nil {
			return err
		}
		if date.Format("2006-01-02") > now.In(berlin).Format("2006-01-02") {
			return bad("Einweisung darf nicht zukünftig sein.")
		}
		if !validReason(input.Instructor) || !validReason(input.Trainee) || !validReason(input.DocumentVersion) {
			return bad("Einweisende/eingewiesene Person und Dokumentversion erforderlich.")
		}
		if input.InstructionScope != "device" && input.InstructionScope != "model" {
			return bad("Einweisung für dieses Gerät oder diese Bauart ausdrücklich angeben.")
		}
		if (input.InstructionScope == "model" && !validReason(input.TransferEvidence)) || (input.InstructionScope == "device" && input.TransferEvidence != "") {
			return bad("Bauartübertragbarkeit gesondert nachweisen.")
		}
		var exists bool
		if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM device_documents WHERE id=? AND device_id=?)`, input.DocumentID, record.ID).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return bad("Dokument muss an genau dieser Geräteakte hinterlegt sein.")
		}

	case "inspection":
		if input.BlockReason != "" || input.BlockID != "" {
			return bad("Sperrfelder gehören nicht zum Prüfergebnis.")
		}
		rule, ok := record.EffectiveRules[input.ControlType]
		if !ok || rule.Applicability != "required" {
			return conflict("control_not_required", "Kontrollpflicht muss zuerst ausdrücklich festgelegt werden.")
		}
		date, err := civilDate(input.PerformedOn)
		if err != nil {
			return bad("Gültiges Prüfdatum erforderlich.")
		}
		berlin, err := time.LoadLocation("Europe/Berlin")
		if err != nil {
			return err
		}
		if date.Format("2006-01-02") > now.In(berlin).Format("2006-01-02") || input.PerformedOn < rule.BasisDate || input.PerformedOn < record.InspectionDates[input.ControlType] {
			return bad("Prüfdatum darf weder zukünftig noch vor der aktuellen Fristbasis liegen.")
		}
		if !validReason(input.Examiner) || !validReason(input.Method) || (input.Result != "passed" && input.Result != "failed") {
			return bad("Prüfer/Firma, Methode und ausdrückliches Prüfergebnis erforderlich.")
		}
		if input.Result == "failed" {
			if input.NextDueOn != "" {
				return bad("Fehlgeschlagene Prüfung erhält keine neue Frist.")
			}
			return nil
		}
		rule.BasisDate = input.PerformedOn
		// The governing rule version stays unchanged. The event records the new basis.
		if rule.Mode == "exact_date" {
			if input.NextDueOn == "" {
				return bad("Exakte Folgefrist ausdrücklich anhand des Nachweises eintragen.")
			}
			rule.ExactDate = input.NextDueOn
		} else if input.NextDueOn != "" {
			return bad("Folgefrist wird aus der gespeicherten Intervallregel berechnet.")
		}
		if _, err = CalculateDeadline(rule, now, 30); err != nil {
			return bad("Ungültige Folgefrist: " + err.Error())
		}
		input.Rules = []DeadlineRule{rule}
	case "defect":
		if input.PerformedOn != "" || input.ControlType != "" || input.Result != "" || input.Examiner != "" || input.Method != "" || input.NextDueOn != "" || input.BlockID != "" {
			return bad("Defektmeldung enthält keine Prüf- oder Freigabefelder.")
		}
		if input.BlockReason != "defect" && input.BlockReason != "recall" && input.BlockReason != "quarantine" {
			return bad("Defekt, Rückruf oder Quarantäne ausdrücklich wählen.")
		}
		if input.BlockReason != "defect" {
			var manager bool
			if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM devices d JOIN locations l ON l.id=d.location_id JOIN role_assignments a ON a.site_id=l.site_id WHERE d.id=? AND a.user_id=? AND a.role='device_manager')`, record.ID, user).Scan(&manager); err != nil {
				return err
			}
			if !manager {
				return scopeError()
			}
		}
	case "release":
		if input.PerformedOn != "" || input.ControlType != "" || input.Result != "" || input.Examiner != "" || input.Method != "" || input.NextDueOn != "" || input.BlockReason != "" {
			return bad("Freigabe muss genau eine bestehende Sperre benennen.")
		}
		found := false
		for _, block := range record.ActiveBlocks {
			if block.ID == input.BlockID {
				found = true
				if block.Reason == "inspection_failed" && (record.PassedVersions[block.ControlType] <= block.Version || (record.Controls[block.ControlType].State != "CURRENT" && record.Controls[block.ControlType].State != "DUE_SOON")) {
					return conflict("passed_inspection_required", "Prüfversagenssperre benötigt eine spätere bestandene, noch gültige Kontrolle.")
				}
			}
		}
		if !found {
			return conflict("block_not_active", "Benannte Sperre ist nicht aktiv.")
		}
	default:
		return bad("Unbekannter Vorgang " + strconv.Quote(action))
	}
	return nil
}
