package app

import (
	"strings"
	"time"
)

type maintenanceProof struct {
	EventID        string `json:"event_id"`
	Version        int    `json:"version"`
	Kind           string `json:"kind"`
	PerformedOn    string `json:"performed_on"`
	Executor       string `json:"executor"`
	Measure        string `json:"measure"`
	Result         string `json:"result"`
	Evidence       string `json:"evidence"`
	DocumentID     string `json:"document_id,omitempty"`
	DocumentSHA256 string `json:"document_sha256,omitempty"`
}

func validateMaintenance(input deviceControlInput, now time.Time) error {
	bad := func(message string) error { return &stockError{400, "invalid_maintenance", message} }
	if input.MaintenanceKind != "maintenance" && input.MaintenanceKind != "repair" && input.MaintenanceKind != "servicing" {
		return bad("Instandhaltungsart ausdrücklich wählen.")
	}
	date, err := civilDate(input.PerformedOn)
	if err != nil {
		return bad("Gültiges Datum der ausgeführten Maßnahme erforderlich.")
	}
	berlin, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		return err
	}
	if date.Format("2006-01-02") > now.In(berlin).Format("2006-01-02") {
		return bad("Ausgeführte Maßnahme darf nicht in der Zukunft liegen.")
	}
	if !validReason(input.Examiner) || !validReason(input.Method) || strings.TrimSpace(input.Examiner) != input.Examiner || strings.TrimSpace(input.Method) != input.Method || !intakeValue(input.Examiner, 500) || !intakeValue(input.Method, 500) {
		return bad("Ausführende Person/Firma und Maßnahme mit jeweils höchstens 500 Zeichen erforderlich.")
	}
	if input.Result != "completed" && input.Result != "follow_up_required" && input.Result != "failed" {
		return bad("Ergebnis ausdrücklich wählen.")
	}
	if input.ControlType != "" || input.NextDueOn != "" || input.BlockReason != "" || input.BlockID != "" || len(input.Rules) > 0 || input.MasterData != nil || input.Details != nil {
		return bad("Instandhaltung separat von Kontrollen, Freigaben und Stammdaten erfassen.")
	}
	return nil
}
