package app

import "time"

// A conservative minimum for the retained book, never an automatic deletion date.
type decommissionProof struct {
	EventID       string `json:"event_id"`
	Version       int    `json:"version"`
	PerformedOn   string `json:"performed_on"`
	RetainThrough string `json:"retain_through"`
	Evidence      string `json:"evidence"`
}

func validateDecommission(input deviceControlInput, record deviceRecord, now time.Time) error {
	bad := func(message string) error { return &stockError{400, "invalid_decommission", message} }
	if record.Decommission != nil {
		return conflict("already_decommissioned", "Außerbetriebnahme ist bereits dokumentiert.")
	}
	date, err := civilDate(input.PerformedOn)
	if err != nil {
		return bad("Gültiges Datum der Außerbetriebnahme erforderlich.")
	}
	berlin, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		return err
	}
	if date.Format("2006-01-02") > now.In(berlin).Format("2006-01-02") {
		return bad("Außerbetriebnahme darf nicht in der Zukunft liegen.")
	}
	if input.PerformedOn < record.CommissionedOn {
		return bad("Außerbetriebnahme darf nicht vor der Inbetriebnahme liegen.")
	}
	for _, proof := range record.Maintenances {
		if input.PerformedOn < proof.PerformedOn {
			return bad("Außerbetriebnahme darf nicht vor dokumentierten Maßnahmen liegen.")
		}
	}
	for _, proof := range record.Instructions {
		if input.PerformedOn < proof.PerformedOn {
			return bad("Außerbetriebnahme darf nicht vor dokumentierten Einweisungen liegen.")
		}
	}
	for _, performed := range record.InspectionDates {
		if input.PerformedOn < performed {
			return bad("Außerbetriebnahme darf nicht vor dokumentierten Prüfungen liegen.")
		}
	}
	other := input
	other.PerformedOn = ""
	if hasTechnicalControlFields(other) || input.Details != nil || input.MasterData != nil {
		return bad("Außerbetriebnahme getrennt von technischen Nachweisen und Stammdaten erfassen.")
	}
	return nil
}
