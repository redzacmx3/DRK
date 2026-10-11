package app

import (
	"context"
	"database/sql"
	"strings"
	"unicode/utf8"
)

type deviceCorrection struct {
	TargetVersion int    `json:"target_version"`
	Statement     string `json:"statement"`
}
type correctedEvent struct {
	ID            string `json:"id"`
	Version       int    `json:"version"`
	Action        string `json:"action"`
	ActorID       string `json:"actor_id"`
	CreatedAt     string `json:"created_at"`
	PayloadSHA256 string `json:"payload_sha256"`
}
type correctionProof struct {
	EventID        string         `json:"event_id"`
	Version        int            `json:"version"`
	Target         correctedEvent `json:"target"`
	Statement      string         `json:"statement"`
	Evidence       string         `json:"evidence"`
	DocumentID     string         `json:"document_id,omitempty"`
	DocumentSHA256 string         `json:"document_sha256,omitempty"`
}

func correctableAction(action string) bool {
	switch action {
	case "commission", "rules", "inspection", "defect", "release", "instruction", "maintenance", "decommission", "incident", "report":
		return true
	}
	return false
}
func validateDeviceCorrection(ctx context.Context, tx *sql.Tx, id string, input deviceControlInput) (*correctedEvent, error) {
	bad := func(message string) (*correctedEvent, error) {
		return nil, &stockError{400, "invalid_device_correction", message}
	}
	if input.Correction == nil || input.Correction.TargetVersion < 1 || strings.TrimSpace(input.Correction.Statement) == "" || (utf8.RuneCountInString(input.Correction.Statement) > 2000 || !intakeValue(strings.ReplaceAll(input.Correction.Statement, "\n", " "), 8000)) || strings.TrimSpace(input.Correction.Statement) != input.Correction.Statement {
		return bad("Ursprungsversion und Richtigstellung mit höchstens 2000 Zeichen erforderlich.")
	}
	other := input
	other.Correction = nil
	other.DocumentID = ""
	other.Qualification = ""
	other.Assignment = ""
	if hasTechnicalControlFields(other) || input.Details != nil || input.MasterData != nil {
		return bad("Richtigstellung separat von neuen technischen Nachweisen und Stammdaten erfassen.")
	}
	target := &correctedEvent{}
	var payload string
	err := tx.QueryRowContext(ctx, `SELECT id,version,action,actor_id,created_at,payload_json FROM device_control_events WHERE device_id=? AND version=?`, id, input.Correction.TargetVersion).Scan(&target.ID, &target.Version, &target.Action, &target.ActorID, &target.CreatedAt, &payload)
	if err == sql.ErrNoRows {
		return bad("Ursprungsnachweis gehört nicht zu dieser Geräteakte.")
	}
	if err != nil {
		return nil, err
	}
	if !correctableAction(target.Action) {
		return bad("Fachnachweis wählen; Grund-/Stammdaten haben einen eigenen Änderungsablauf. Weitere Richtigstellungen beziehen sich auf den ursprünglichen Fachnachweis.")
	}
	target.PayloadSHA256 = digest(payload)
	return target, nil
}
