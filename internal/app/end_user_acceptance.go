package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"
)

type endUserInput struct {
	ExpectedVersion       int    `json:"expected_version"`
	ExpectedDeviceVersion int    `json:"expected_device_version"`
	ExpectedBasis         string `json:"expected_basis,omitempty"`
	UserID                string `json:"user_id"`
	Action                string `json:"action"`
	Reviewed              *bool  `json:"reviewed,omitempty"`
	PerformedOn           string `json:"performed_on,omitempty"`
	ValidUntil            string `json:"valid_until,omitempty"`
	CompetenceEvidence    string `json:"competence_evidence,omitempty"`
	InstructionEvidence   string `json:"instruction_evidence,omitempty"`
	ManualEvidence        string `json:"manual_evidence,omitempty"`
	Evidence              string `json:"evidence"`
	Qualification         string `json:"qualification"`
	Assignment            string `json:"assignment"`
	Note                  string `json:"note"`
}
type endUserEvent struct {
	Sources   []endUserSource `json:"sources"`
	ID        string          `json:"id"`
	Version   int             `json:"version"`
	ActorID   string          `json:"actor_id"`
	CreatedAt string          `json:"created_at"`
	Input     endUserInput    `json:"input"`
	Identity  string          `json:"identity_basis"`
	Basis     string          `json:"evidence_basis,omitempty"`
}
type endUserSource struct {
	DocumentID            string `json:"document_id,omitempty"`
	DocumentReviewVersion int    `json:"document_review_version,omitempty"`
	Kind                  string `json:"kind"`
	Version               int    `json:"version"`
	Reference             string `json:"reference"`
	Evidence              string `json:"evidence"`
	Qualification         string `json:"qualification,omitempty"`
	ValidFrom             string `json:"valid_from,omitempty"`
	ValidUntil            string `json:"valid_until,omitempty"`
}
type endUserAssessment struct {
	Sources       []endUserSource `json:"sources"`
	UserID        string          `json:"user_id"`
	Version       int             `json:"version"`
	DeviceVersion int             `json:"device_version"`
	Status        string          `json:"status"`
	Reasons       []string        `json:"reasons"`
	Basis         string          `json:"basis_hash"`
	Events        []endUserEvent  `json:"events"`
	Overall       string          `json:"overall_readiness"`
}
type endUserPrerequisites struct {
	Sources                     []endUserSource
	Status, Basis, EarliestDate string
	Reasons                     []string
}

// Only the selected person's current required evidence affects this fingerprint.
// Renewing unchanged manual access preserves approval; explicit revocation does not.
func assessEndUserPrerequisites(ctx context.Context, tx *sql.Tx, actor, subject string, record deviceRecord, now time.Time) (endUserPrerequisites, error) {
	result := endUserPrerequisites{Status: "met", Reasons: []string{}, Sources: []endUserSource{}}
	usage, err := assessUsage(ctx, tx, actor, subject, record, now)
	if err != nil {
		return result, err
	}
	access, err := assessManualAccess(ctx, tx, actor, subject, record, now)
	if err != nil {
		return result, err
	}
	merge := func(status, reason string) {
		if status == "blocked" || result.Status != "blocked" {
			result.Status = status
		}
		result.Reasons = append(result.Reasons, reason)
	}
	if usage.Status != "recorded_requirements_met" {
		status := "unknown"
		if usage.Status == "blocked" {
			status = "blocked"
		}
		merge(status, "Personenqualifikation oder konkrete Einweisungszuordnung nicht aktuell.")
	}
	if access.Status != "confirmed" {
		status := "unknown"
		if access.Status == "blocked" {
			status = "blocked"
		}
		merge(status, "Verständliche Gebrauchsanweisung für diese Person am Geräteort nicht aktuell zugänglich bestätigt.")
	}
	if len(record.ActiveBlocks) > 0 || record.Decommission != nil {
		merge("blocked", "Gerät gesperrt oder außer Betrieb; Teilabnahme hebt keine Sperre auf.")
	}
	var policy, binding *usageEvent
	qualifications := map[string]usageEvent{}
	for i := range usage.Events {
		e := &usage.Events[i]
		if e.Input.Action == "policy" {
			policy = e
		}
		if e.Input.Action == "bind" || e.Input.Action == "unbind" {
			binding = e
		}
		if e.Input.Action == "qualify" || e.Input.Action == "revoke" {
			qualifications[e.Input.Code] = *e
		}
	}
	type proofVersion struct {
		ID      string
		Version int
	}
	versions := []proofVersion{}
	if policy != nil {
		result.Sources = append(result.Sources, endUserSource{Kind: "policy", Version: policy.Version, Reference: policy.Input.InstructionMode, Evidence: policy.Input.Evidence, Qualification: policy.Input.Qualification})
		versions = append(versions, proofVersion{policy.ID, policy.Version})
	}
	for _, code := range usage.Codes {
		if proof, ok := qualifications[code]; ok {
			result.Sources = append(result.Sources, endUserSource{Kind: "qualification", Version: proof.Version, Reference: code + " · " + proof.Input.Action, Evidence: proof.Input.Evidence, Qualification: proof.Input.Qualification, ValidFrom: proof.Input.ValidFrom, ValidUntil: proof.Input.ValidUntil})
			versions = append(versions, proofVersion{proof.ID, proof.Version})
			if proof.Input.ValidFrom > result.EarliestDate {
				result.EarliestDate = proof.Input.ValidFrom
			}
		}
	}
	instructionReview := 0
	if usage.InstructionMode == "required" && binding != nil {
		versions = append(versions, proofVersion{binding.ID, binding.Version})
		result.Sources = append(result.Sources, endUserSource{Kind: "instruction", Version: binding.Version, Reference: binding.Input.Action, Evidence: binding.Input.Evidence, Qualification: binding.Input.Qualification})
		if binding.Input.Action == "unbind" {
			merge("blocked", "Einweisungszuordnung ausdrücklich widerrufen.")
		}
		if binding.Input.Action == "bind" {
			for _, proof := range record.Instructions {
				if proof.Version != binding.Input.InstructionVersion {
					continue
				}
				result.Sources = append(result.Sources, endUserSource{Kind: "instruction", Version: proof.Version, Reference: proof.Instructor + " · " + proof.Scope + " · " + proof.DocumentVersion + " · SHA-256 " + proof.DocumentSHA256, Evidence: proof.DocumentID, ValidFrom: proof.PerformedOn})
				if proof.PerformedOn > result.EarliestDate {
					result.EarliestDate = proof.PerformedOn
				}
				if correctedDutyEvent(record, proof.Version) {
					merge("unknown", "Zugeordnete Einweisung wurde richtiggestellt; passenden neuen Nachweis prüfen.")
				}
				var status, sha string
				if err := tx.QueryRowContext(ctx, `SELECT status,sha256,review_version FROM effective_documents WHERE id=? AND device_id=?`, proof.DocumentID, record.ID).Scan(&status, &sha, &instructionReview); err != nil {
					return result, err
				}
				result.Sources[len(result.Sources)-1].DocumentID = proof.DocumentID
				result.Sources[len(result.Sources)-1].DocumentReviewVersion = instructionReview
				if status != "released" || sha != proof.DocumentSHA256 {
					merge("unknown", "Gebundene Einweisungsunterlage aktuell nicht freigegeben oder Prüfsumme abweichend.")
				}
			}
		}
	}
	manualDocument, manualSHA, manualVersion, language := "", "", "", ""
	manualReview, manualRevoke := 0, 0
	if len(access.Events) > 0 && access.Events[0].Input.Action == "confirm" {
		last := access.Events[0]
		result.Sources = append(result.Sources, endUserSource{Kind: "manual", Version: last.Version, Reference: last.Input.ManualVersion + " · " + last.Input.Language + " · SHA-256 " + last.DocumentSHA256, Evidence: last.Input.Evidence, Qualification: last.Input.Qualification, ValidUntil: last.Input.ValidUntil})
		manualDocument, manualSHA, manualVersion, language = last.Input.DocumentID, last.DocumentSHA256, last.Input.ManualVersion, last.Input.Language
		if err := tx.QueryRowContext(ctx, `SELECT review_version FROM effective_documents WHERE id=? AND device_id=?`, manualDocument, record.ID).Scan(&manualReview); err != nil {
			return result, err
		}
		result.Sources[len(result.Sources)-1].DocumentID = manualDocument
		result.Sources[len(result.Sources)-1].DocumentReviewVersion = manualReview
	}
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(version),0) FROM manual_access_events WHERE device_id=? AND subject_id=? AND action='revoke'`, record.ID, subject).Scan(&manualRevoke); err != nil {
		return result, err
	}
	identity, err := manualIdentity(ctx, tx, record)
	if err != nil {
		return result, err
	}
	value, err := json.Marshal(struct {
		Identity, Subject                                  string
		Versions                                           []proofVersion
		InstructionReview                                  int
		ManualDocument, ManualSHA, ManualVersion, Language string
		ManualReview, ManualRevoke                         int
	}{identity, subject, versions, instructionReview, manualDocument, manualSHA, manualVersion, language, manualReview, manualRevoke})
	if err != nil {
		return result, err
	}
	result.Basis = digest(string(value))
	return result, nil
}

func assessEndUser(ctx context.Context, tx *sql.Tx, actor, subject string, record deviceRecord, now time.Time) (endUserAssessment, error) {
	result := endUserAssessment{UserID: subject, DeviceVersion: record.Version, Status: "unknown", Reasons: []string{}, Events: []endUserEvent{}, Overall: "not_assessed"}
	prerequisites, err := assessEndUserPrerequisites(ctx, tx, actor, subject, record, now)
	if err != nil {
		return result, err
	}
	result.Basis = prerequisites.Basis
	result.Sources = prerequisites.Sources
	var count int
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(version),0),COUNT(*) FROM end_user_acceptance_events WHERE device_id=?`, record.ID).Scan(&result.Version, &count); err != nil {
		return result, err
	}
	if count > 2000 {
		return result, &stockError{413, "end_user_limit", "Mehr als 2000 Endanwenderbewertungen; keine Teilbewertung erstellt."}
	}
	rows, err := tx.QueryContext(ctx, `SELECT payload_json FROM end_user_acceptance_events WHERE device_id=? AND subject_id=? ORDER BY version DESC LIMIT 100`, record.ID, subject)
	if err != nil {
		return result, err
	}
	for rows.Next() {
		var raw string
		var event endUserEvent
		if err := rows.Scan(&raw); err != nil {
			rows.Close()
			return result, err
		}
		if err := json.Unmarshal([]byte(raw), &event); err != nil {
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
	if prerequisites.Status == "blocked" {
		result.Status = "blocked"
		result.Reasons = append(result.Reasons, prerequisites.Reasons...)
	}
	if len(result.Events) == 0 {
		result.Reasons = append(result.Reasons, "Noch keine fachliche Endanwenderabnahme für diese Person.")
		return result, nil
	}
	last := result.Events[0]
	if last.Input.Action == "revoke" {
		result.Status = "blocked"
		result.Reasons = append(result.Reasons, "Endanwenderabnahme ausdrücklich widerrufen.")
		return result, nil
	}
	stamp, err := time.Parse(time.RFC3339Nano, last.CreatedAt)
	if err != nil {
		return result, err
	}
	until, err := time.Parse(time.RFC3339, last.Input.ValidUntil)
	if err != nil {
		return result, err
	}
	if now.Before(stamp) || !now.Before(until) {
		result.Reasons = append(result.Reasons, "Endanwenderbewertung nicht aktuell; ausdrücklich erneuern.")
		return result, nil
	}
	if last.Input.Action == "reject" {
		result.Status = "blocked"
		result.Reasons = append(result.Reasons, "Fachliche Endanwenderabnahme ausdrücklich abgelehnt.")
		return result, nil
	}
	if prerequisites.Status != "met" {
		if result.Status != "blocked" {
			result.Reasons = append(result.Reasons, prerequisites.Reasons...)
		}
		return result, nil
	}
	if last.Basis != prerequisites.Basis {
		result.Reasons = append(result.Reasons, "Gebundene Qualifikations-, Einweisungs-, Unterlagen- oder Standortnachweise geändert; keine automatische Reaktivierung, ausdrücklich neu abnehmen.")
		return result, nil
	}
	result.Status = "accepted"
	result.Reasons = append(result.Reasons, "Personenbezogene Endanwendernachweise ausdrücklich fachlich abgenommen und aktuell gebunden; besondere Produktpflichten und gesamte Einsatzfreigabe bleiben offen.")
	return result, nil
}
