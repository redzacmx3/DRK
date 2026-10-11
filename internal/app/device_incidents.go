package app

import (
	"strings"
	"time"
)

type deviceIncident struct {
	Kind         string `json:"kind"`
	Description  string `json:"description"`
	Consequences string `json:"consequences"`
}
type incidentReport struct {
	AssessmentReference    string `json:"assessment_reference"`
	IncidentVersion        int    `json:"incident_version"`
	Status                 string `json:"status"`
	Responsible            string `json:"responsible"`
	Authority              string `json:"authority"`
	AuthorityReference     string `json:"authority_reference"`
	AuthorityReportedAt    string `json:"authority_reported_at"`
	ManufacturerContact    string `json:"manufacturer_contact"`
	ManufacturerReference  string `json:"manufacturer_reference"`
	ManufacturerReportedAt string `json:"manufacturer_reported_at"`
}
type incidentProof struct {
	EventID    string `json:"event_id"`
	Version    int    `json:"version"`
	OccurredOn string `json:"occurred_on"`
	deviceIncident
	Evidence       string          `json:"evidence"`
	DocumentID     string          `json:"document_id,omitempty"`
	DocumentSHA256 string          `json:"document_sha256,omitempty"`
	Report         *incidentReport `json:"report,omitempty"`
}
type reportProof struct {
	EventID string `json:"event_id"`
	Version int    `json:"version"`
	incidentReport
	Evidence       string `json:"evidence"`
	DocumentID     string `json:"document_id,omitempty"`
	DocumentSHA256 string `json:"document_sha256,omitempty"`
}

func validateIncident(input deviceControlInput, now time.Time) error {
	bad := func(message string) error { return &stockError{400, "invalid_device_incident", message} }
	if input.Incident == nil {
		return bad("Art, Beschreibung und Folgen der Störung erforderlich.")
	}
	if input.Incident.Kind != "malfunction" && input.Incident.Kind != "repeated_operating_error" && input.Incident.Kind != "suspected_serious_incident" {
		return bad("Art der Störung ausdrücklich wählen.")
	}
	for _, value := range []string{input.Incident.Description, input.Incident.Consequences} {
		if !validReason(value) || strings.TrimSpace(value) != value || !intakeValue(value, 500) {
			return bad("Beschreibung und Folgen mit jeweils höchstens 500 UTF-8-Bytes erforderlich. Keine Patientendaten erfassen.")
		}
	}
	date, err := civilDate(input.PerformedOn)
	if err != nil {
		return bad("Gültiges Datum der Störung erforderlich.")
	}
	berlin, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		return err
	}
	if date.Format("2006-01-02") > now.In(berlin).Format("2006-01-02") {
		return bad("Störung darf nicht zukünftig sein.")
	}
	other := input
	other.Incident = nil
	other.PerformedOn = ""
	other.DocumentID = ""
	if hasTechnicalControlFields(other) || input.Details != nil || input.MasterData != nil {
		return bad("Störung getrennt von technischen Nachweisen und Meldestatus erfassen.")
	}
	return nil
}
func validateIncidentReport(input deviceControlInput, record deviceRecord, now time.Time) error {
	bad := func(message string) error { return &stockError{400, "invalid_incident_report", message} }
	if input.Report == nil {
		return bad("Störungsbezug und Meldestatus erforderlich.")
	}
	report := input.Report
	switch report.Status {
	case "ASSESSMENT_REQUIRED", "REPORT_PENDING", "REPORTED", "FOLLOW_UP", "CLOSED":
	default:
		return bad("Meldestatus ausdrücklich wählen.")
	}
	if report.Status == "CLOSED" && !validReason(report.AssessmentReference) {
		return bad("Abschluss benötigt eine ausdrückliche Bewertungs-/Entscheidungsreferenz.")
	}
	var previous *incidentReport
	found := false
	occurred := ""
	for _, incident := range record.Incidents {
		if incident.Version == report.IncidentVersion {
			found = true
			occurred = incident.OccurredOn
			previous = incident.Report
			break
		}
	}
	if !found {
		return bad("Störung gehört nicht zu dieser Geräteakte.")
	}
	if !validReason(report.Responsible) || strings.TrimSpace(report.Responsible) != report.Responsible || !intakeValue(report.Responsible, 500) {
		return bad("Verantwortliche Person oder Funktion erforderlich.")
	}
	for _, value := range []string{report.AssessmentReference, report.Authority, report.AuthorityReference, report.ManufacturerContact, report.ManufacturerReference} {
		if strings.TrimSpace(value) != value || !intakeValue(value, 500) {
			return bad("Gültige Meldebezüge erforderlich.")
		}
	}
	validateContact := func(name, ref, date string) bool {
		if name == "" && ref == "" && date == "" {
			return true
		}
		if name == "" || ref == "" || date == "" {
			return false
		}
		parsed, err := time.Parse(time.RFC3339, date)
		berlin, zoneErr := time.LoadLocation("Europe/Berlin")
		return err == nil && zoneErr == nil && !parsed.After(now) && parsed.In(berlin).Format("2006-01-02") >= occurred
	}
	if !validateContact(report.Authority, report.AuthorityReference, report.AuthorityReportedAt) || !validateContact(report.ManufacturerContact, report.ManufacturerReference, report.ManufacturerReportedAt) {
		return bad("Eine dokumentierte Meldung benötigt Empfänger, Referenz und gültigen nicht zukünftigen Zeitpunkt mit Zeitzone.")
	}
	if report.Status == "REPORTED" && report.AuthorityReference == "" {
		return bad("Gemeldet benötigt eine ausdrücklich dokumentierte Behördenmeldung; Herstellerkontakt genügt nicht.")
	}
	if previous != nil && ((previous.AuthorityReference != "" && report.AuthorityReference == "") || (previous.ManufacturerReference != "" && report.ManufacturerReference == "")) {
		return bad("Bereits dokumentierte Meldebezüge dürfen nicht entfernt werden; Richtigstellung separat ergänzen.")
	}
	other := input
	other.Report = nil
	other.DocumentID = ""
	other.Qualification = ""
	other.Assignment = ""
	if hasTechnicalControlFields(other) || input.Details != nil || input.MasterData != nil {
		return bad("Meldestatus getrennt von Störung und technischen Nachweisen erfassen.")
	}
	return nil
}
