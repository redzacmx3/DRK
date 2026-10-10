package app

import "strings"

// Identity and location stay fixed; corrections are appended to the common
// device event sequence and never alter the immutable registration or proofs.
type deviceDetails struct {
	Name         string `json:"name"`
	Manufacturer string `json:"manufacturer"`
	Model        string `json:"model"`
	SerialOrLot  string `json:"serial_or_lot"`
}

func currentDeviceDetails(record deviceRecord) deviceDetails {
	return deviceDetails{record.Name, record.Manufacturer, record.Model, record.SerialOrLot}
}
func validateDeviceDetails(input deviceControlInput, record deviceRecord) error {
	bad := func(message string) error { return &stockError{400, "invalid_device_details", message} }
	if input.Details == nil || input.Details.Name == "" {
		return bad("Bezeichnung erforderlich; unbekannte Hersteller-/Modell-/Serienangaben leer lassen.")
	}
	for _, value := range []string{input.Details.Name, input.Details.Manufacturer, input.Details.Model, input.Details.SerialOrLot} {
		if strings.TrimSpace(value) != value || !intakeValue(value, 160) {
			return bad("Grunddaten auf 160 Zeichen begrenzen, ohne Rand-Leerzeichen oder Steuerzeichen.")
		}
	}
	if input.PerformedOn != "" || input.ControlType != "" || input.Result != "" || input.Examiner != "" || input.Method != "" || input.NextDueOn != "" || input.BlockReason != "" || input.BlockID != "" || len(input.Rules) > 0 || input.Instructor != "" || input.Trainee != "" || input.InstructionScope != "" || input.TransferEvidence != "" || input.DocumentVersion != "" || input.DocumentID != "" || input.Qualification != "" || input.Assignment != "" {
		return bad("Grunddatenänderung getrennt von Prüfungen, Einweisungen und Freigaben erfassen.")
	}
	if *input.Details == currentDeviceDetails(record) {
		return conflict("device_details_unchanged", "Keine Grunddaten geändert.")
	}
	return nil
}
