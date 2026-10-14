package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"strconv"
	"time"
)

type cycleInput struct {
	ExpectedVersion       int    `json:"expected_version"`
	ExpectedDeviceVersion int    `json:"expected_device_version"`
	ExpectedBasis         string `json:"expected_basis,omitempty"`
	Action                string `json:"action"`
	CycleVersion          int    `json:"cycle_version,omitempty"`
	ProcessVersion        int    `json:"process_version,omitempty"`
	Reviewed              *bool  `json:"reviewed,omitempty"`
	PerformedAt           string `json:"performed_at,omitempty"`
	PerformedOn           string `json:"performed_on,omitempty"`
	CycleReference        string `json:"cycle_reference,omitempty"`
	Executor              string `json:"executor,omitempty"`
	Result                string `json:"result,omitempty"`
	ParametersEvidence    string `json:"parameters_evidence,omitempty"`
	ReleaseEvidence       string `json:"release_evidence,omitempty"`
	ValidUntil            string `json:"valid_until,omitempty"`
	DocumentID            string `json:"document_id,omitempty"`
	Evidence              string `json:"evidence"`
	Qualification         string `json:"qualification"`
	Assignment            string `json:"assignment"`
	Note                  string `json:"note"`
}
type cycleEvent struct {
	ID                    string     `json:"id"`
	Version               int        `json:"version"`
	ActorID               string     `json:"actor_id"`
	CreatedAt             string     `json:"created_at"`
	Input                 cycleInput `json:"input"`
	Basis                 string     `json:"basis,omitempty"`
	DocumentSHA256        string     `json:"document_sha256,omitempty"`
	DocumentReviewVersion int        `json:"document_review_version,omitempty"`
}
type cycleAssessment struct {
	ProcessReviewedOn string       `json:"process_reviewed_on,omitempty"`
	Version           int          `json:"version"`
	DeviceVersion     int          `json:"device_version"`
	Status            string       `json:"status"`
	Reason            string       `json:"reason"`
	CycleVersion      int          `json:"cycle_version,omitempty"`
	ProcessVersion    int          `json:"process_version,omitempty"`
	Basis             string       `json:"basis_hash,omitempty"`
	Events            []cycleEvent `json:"events,omitempty"`
	Overall           string       `json:"overall_readiness"`
}

func currentProcessPrerequisites(ctx context.Context, tx *sql.Tx, record deviceRecord, now time.Time) (cycleAssessment, error) {
	result := cycleAssessment{DeviceVersion: record.Version, Status: "unknown", Reason: "Aktuelle fachliche Prozessbewertung für Zyklusführung erforderlich.", Overall: "not_assessed"}
	raw := record
	raw.Reprocessing = nil
	raw.ActiveBlocks = nil
	for _, b := range record.ActiveBlocks {
		if b.Reason != "reprocessing_required" {
			raw.ActiveBlocks = append(raw.ActiveBlocks, b)
		}
	}
	assessment, err := assessProductDuty(ctx, tx, "", raw, now)
	if err != nil {
		return result, err
	}
	result.ProcessVersion = assessment.Version
	if len(raw.ActiveBlocks) > 0 || raw.Decommission != nil {
		result.Status = "blocked"
		result.Reason = "Unabhängige Gerätesperre oder Außerbetriebnahme aktiv."
		return result, nil
	}
	if assessment.Status != "reviewed" || len(assessment.Events) == 0 {
		return result, nil
	}
	last := assessment.Events[0]
	if last.ReprocessingBasis == nil || last.ReprocessingBasis.Domain != "reprocessing_v1" {
		result.Reason = "Prozessbewertung für neue Zyklusführung erneut bestätigen."
		return result, nil
	}
	for _, check := range assessment.Checks {
		if check.Kind == "reprocessing_process" && check.Status == "accepted" {
			result.Status = "met"
			result.Reason = "Aktueller fachlich bewerteter Prozess; Zyklus und Ergebnisfreigabe separat erforderlich."
			identity, err := manualIdentity(ctx, tx, raw)
			if err != nil {
				return result, err
			}
			var process *dutyReprocessingAcceptance
			for _, r := range last.Input.Rules {
				if r.Kind == "reprocessing" {
					process = r.ReprocessingAcceptance
				}
			}
			if process == nil {
				return result, nil
			}
			result.ProcessReviewedOn = process.PerformedOn
			copy := *process
			copy.ExpectedSnapshot = ""
			approval, _ := json.Marshal(copy)
			var epoch int
			err = tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(e.version),0) FROM product_duty_events e WHERE e.device_id=? AND (e.action='revoke' OR EXISTS(SELECT 1 FROM json_each(e.payload_json,'$.input.rules') r WHERE json_extract(r.value,'$.kind')='reprocessing' AND (json_extract(r.value,'$.applicability')!='required' OR json_extract(r.value,'$.reprocessing_acceptance.decision') IS NULL OR json_extract(r.value,'$.reprocessing_acceptance.decision')!='accepted' OR json_extract(r.value,'$.reprocessing_acceptance.requirements_met')!=1 OR json_extract(r.value,'$.reprocessing_acceptance.process_validated')!=1)))`, record.ID).Scan(&epoch)
			if err != nil {
				return result, err
			}
			result.Basis = digest("cycle_process:" + record.ID + ":" + identity + ":" + string(approval) + ":" + strconv.Itoa(epoch) + ":" + last.ReprocessingBasis.Snapshot)
		}
	}
	return result, nil
}
func cycleDocumentCurrent(ctx context.Context, tx *sql.Tx, id string, event cycleEvent) (bool, error) {
	var status, sha string
	var version int
	err := tx.QueryRowContext(ctx, `SELECT status,sha256,review_version FROM effective_documents WHERE device_id=? AND id=?`, id, event.Input.DocumentID).Scan(&status, &sha, &version)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return status == "released" && sha == event.DocumentSHA256 && version == event.DocumentReviewVersion && version > 0, nil
}
func assessCycles(ctx context.Context, tx *sql.Tx, record deviceRecord, now time.Time) (result cycleAssessment, assessmentErr error) {
	result = cycleAssessment{DeviceVersion: record.Version, Status: "unknown", Reason: "Kein Aufbereitungszyklus erfasst.", Overall: "not_assessed"}
	defer func() {
		if assessmentErr == nil {
			reasons := []string{}
			var inactive bool
			inactive, assessmentErr = maskModuleAssessment(ctx, tx, "reprocessing", &result.Status, &reasons)
			if inactive {
				result.Reason += " " + reasons[0]
			}
		}
	}()
	var count int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*),COALESCE(MAX(version),0) FROM reprocessing_events WHERE device_id=?`, record.ID).Scan(&count, &result.Version); err != nil {
		return result, err
	}
	if count > 2000 {
		return result, &stockError{413, "reprocessing_limit", "Mehr als 2000 Aufbereitungsereignisse; Bewertung abgebrochen."}
	}
	if count == 0 {
		return result, nil
	}
	rows, err := tx.QueryContext(ctx, `SELECT payload_json FROM reprocessing_events WHERE device_id=? ORDER BY version DESC LIMIT 100`, record.ID)
	if err != nil {
		return result, err
	}
	for rows.Next() {
		var payload string
		var event cycleEvent
		if err := rows.Scan(&payload); err != nil {
			rows.Close()
			return result, err
		}
		if err := json.Unmarshal([]byte(payload), &event); err != nil {
			rows.Close()
			return result, err
		}
		result.Events = append(result.Events, event)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return result, err
	}
	last := result.Events[0]
	result.Status = "blocked"
	result.Reason = "Aufbereitungsbedarf, offener Zyklus oder fehlende Ergebnisfreigabe."
	if last.Input.Action == "cycle" {
		result.CycleVersion = last.Version
		result.ProcessVersion = last.Input.ProcessVersion
	} else {
		result.CycleVersion = last.Input.CycleVersion
		result.ProcessVersion = last.Input.ProcessVersion
	}
	if last.Input.Action != "release" {
		return result, nil
	}
	result.CycleVersion = last.Input.CycleVersion
	result.ProcessVersion = last.Input.ProcessVersion
	var payload string
	err = tx.QueryRowContext(ctx, `SELECT payload_json FROM reprocessing_events WHERE device_id=? AND version=? AND action='cycle'`, record.ID, last.Input.CycleVersion).Scan(&payload)
	if err == sql.ErrNoRows {
		return result, nil
	}
	if err != nil {
		return result, err
	}
	var cycle cycleEvent
	if err := json.Unmarshal([]byte(payload), &cycle); err != nil {
		return result, err
	}
	if cycle.Input.Result != "passed" || last.Input.Reviewed == nil || !*last.Input.Reviewed {
		return result, nil
	}
	until, err := time.Parse(time.RFC3339, last.Input.ValidUntil)
	created, clockErr := time.Parse(time.RFC3339Nano, last.CreatedAt)
	if err != nil || !until.After(now) || clockErr != nil || created.After(now) {
		result.Reason = "Zyklusfreigabe abgelaufen oder Zeitbasis ungeklärt."
		return result, nil
	}
	prereq, err := currentProcessPrerequisites(ctx, tx, record, now)
	if err != nil {
		return result, err
	}
	if prereq.Status != "met" || prereq.Basis != last.Basis || cycle.Basis != last.Basis || cycle.Input.ProcessVersion != last.Input.ProcessVersion {
		result.Reason = "Prozess-, Geräte- oder Dokumentbasis geändert; keine aktuelle Zyklusfreigabe."
		return result, nil
	}
	for _, proof := range []cycleEvent{cycle, last} {
		ok, err := cycleDocumentCurrent(ctx, tx, record.ID, proof)
		if err != nil {
			return result, err
		}
		if !ok {
			result.Reason = "Zyklus- oder Ergebnisfreigabeunterlage entzogen beziehungsweise neu geprüft."
			return result, nil
		}
	}
	result.Status = "released"
	result.Reason = "Konkretes Aufbereitungsergebnis ausdrücklich freigegeben; übrige Einsatzvoraussetzungen bleiben getrennt."
	result.Basis = last.Basis
	return result, nil
}
func projectReprocessing(ctx context.Context, tx *sql.Tx, record *deviceRecord, now time.Time) error {
	assessment, err := assessCycles(ctx, tx, *record, now)
	if err != nil {
		return err
	}
	if assessment.Version == 0 {
		return nil
	}
	assessment.Events = nil
	record.Reprocessing = &assessment
	if assessment.Status != "released" {
		var id string
		if err := tx.QueryRowContext(ctx, `SELECT id FROM reprocessing_events WHERE device_id=? ORDER BY version DESC LIMIT 1`, record.ID).Scan(&id); err != nil {
			return err
		}
		record.ActiveBlocks = append(record.ActiveBlocks, deviceBlock{ID: "reprocessing:" + id, Reason: "reprocessing_required", Version: assessment.Version, Note: assessment.Reason})
		record.Reasons = append(record.Reasons, "Aufbereitung: "+assessment.Reason)
	}
	return nil
}
