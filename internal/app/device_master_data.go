package app

import (
	"strconv"
	"strings"
	"time"
)

type deviceMasterData struct {
	AcquisitionYear       string `json:"acquisition_year"`
	AssetType             string `json:"asset_type"`
	ActorRole             string `json:"actor_role"`
	ActorName             string `json:"actor_name"`
	ActorAddress          string `json:"actor_address"`
	OperationalAssignment string `json:"operational_assignment"`
}

func currentMasterData(record deviceRecord) deviceMasterData {
	if record.MasterData == nil {
		return deviceMasterData{}
	}
	return *record.MasterData
}
func validateMasterData(input deviceControlInput, record deviceRecord, now time.Time) error {
	bad := func(message string) error { return &stockError{400, "invalid_master_data", message} }
	if input.MasterData == nil || input.Details != nil {
		return bad("Stammdaten separat erfassen.")
	}
	in := input.MasterData
	berlin, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		return err
	}
	for _, value := range []string{in.AssetType, in.ActorName, in.OperationalAssignment} {
		if strings.TrimSpace(value) != value || !intakeValue(value, 160) {
			return bad("Stammdaten auf 160 Zeichen begrenzen, ohne Rand-Leerzeichen oder Steuerzeichen.")
		}
	}
	if strings.TrimSpace(in.ActorAddress) != in.ActorAddress || !intakeValue(in.ActorAddress, 500) {
		return bad("Anschrift als Text bis 500 Zeichen ohne Steuerzeichen erfassen.")
	}
	if in.AcquisitionYear != "" {
		year, err := strconv.Atoi(in.AcquisitionYear)
		if err != nil || len(in.AcquisitionYear) != 4 || strconv.Itoa(year) != in.AcquisitionYear || year < 1000 || year > now.In(berlin).Year() {
			return bad("Anschaffungsjahr vierstellig und nicht in der Zukunft; ungeklärt leer lassen.")
		}
	}
	if in.ActorRole != "" && in.ActorRole != "manufacturer" && in.ActorRole != "authorized_representative" && in.ActorRole != "importer" {
		return bad("Funktion des Wirtschaftsakteurs ausdrücklich wählen.")
	}
	if (in.ActorName != "" || in.ActorAddress != "") && in.ActorRole == "" {
		return bad("Name oder Anschrift benötigt eine ausdrücklich gewählte Akteursfunktion.")
	}
	if hasTechnicalControlFields(input) {
		return bad("Stammdaten getrennt von Prüfungen, Einweisungen und Freigaben erfassen.")
	}
	if *in == currentMasterData(record) {
		return conflict("master_data_unchanged", "Keine Stammdaten geändert.")
	}
	return nil
}

func masterDataGaps(record deviceRecord) []string {
	in := currentMasterData(record)
	gaps := []string{}
	for _, field := range []struct{ value, label string }{{in.AssetType, "Geräteart"}, {in.AcquisitionYear, "Anschaffungsjahr"}, {in.ActorRole, "Wirtschaftsakteur-Funktion"}, {in.ActorName, "Wirtschaftsakteur-Name"}, {in.ActorAddress, "Wirtschaftsakteur-Anschrift"}, {in.OperationalAssignment, "Betriebliche Zuordnung"}} {
		if field.value == "" {
			gaps = append(gaps, field.label)
		}
	}
	return gaps
}
