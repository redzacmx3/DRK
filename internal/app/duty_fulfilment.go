package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"
)

type dutyFulfilmentArea struct {
	Kind          string `json:"kind"`
	ScopeComplete *bool  `json:"scope_complete"`
	Evidence      string `json:"evidence"`
	Reason        string `json:"reason"`
}
type dutyFulfilmentInput struct {
	ExpectedVersion       int                  `json:"expected_version"`
	ExpectedDeviceVersion int                  `json:"expected_device_version"`
	ExpectedBasis         string               `json:"expected_basis,omitempty"`
	Action                string               `json:"action"`
	Reviewed              *bool                `json:"reviewed,omitempty"`
	PerformedOn           string               `json:"performed_on,omitempty"`
	ValidUntil            string               `json:"valid_until,omitempty"`
	DocumentID            string               `json:"document_id,omitempty"`
	Areas                 []dutyFulfilmentArea `json:"areas,omitempty"`
	Evidence              string               `json:"evidence"`
	Qualification         string               `json:"qualification"`
	Assignment            string               `json:"assignment"`
	Note                  string               `json:"note"`
}
type dutyFulfilmentEvent struct {
	ID                    string              `json:"id"`
	Version               int                 `json:"version"`
	ActorID               string              `json:"actor_id"`
	CreatedAt             string              `json:"created_at"`
	Input                 dutyFulfilmentInput `json:"input"`
	Basis                 string              `json:"evidence_basis,omitempty"`
	ProductDutyVersion    int                 `json:"product_duty_version,omitempty"`
	Sources               []productDutyCheck  `json:"sources,omitempty"`
	DocumentSHA256        string              `json:"document_sha256,omitempty"`
	DocumentReviewVersion int                 `json:"document_review_version,omitempty"`
}
type dutyFulfilmentAssessment struct {
	Version       int                   `json:"version"`
	DeviceVersion int                   `json:"device_version"`
	Status        string                `json:"status"`
	Reasons       []string              `json:"reasons"`
	Basis         string                `json:"basis_hash,omitempty"`
	Prerequisites []productDutyCheck    `json:"prerequisites"`
	Events        []dutyFulfilmentEvent `json:"events,omitempty"`
	Overall       string                `json:"overall_readiness"`
}

// Aggregate recorded technical prerequisites only. The reviewer must separately
// attest completeness for every area; a preview never constitutes acceptance.
func dutyFulfilmentPrerequisites(record deviceRecord, duties productDutyAssessment) (string, []productDutyCheck) {
	state := "met"
	sources := []productDutyCheck{}
	checks := map[string]string{}
	for _, c := range duties.Checks {
		checks[c.Kind] = c.Status
	}
	control := func(kind, proof string) string {
		switch record.Controls[kind].State {
		case "NOT_APPLICABLE":
			return "not_applicable"
		case "OVERDUE":
			return "blocked"
		case "CURRENT", "DUE_SOON":
			return checks[proof]
		default:
			return "unknown"
		}
	}
	merge := func(status *string, value string) {
		if value == "blocked" {
			*status = "blocked"
		} else if value != "accepted" && value != "not_applicable" && *status != "blocked" {
			*status = "unknown"
		}
	}
	for _, kind := range productDutyKinds {
		status := "unknown"
		if duties.Status == "reviewed" {
			status = checks[kind]
			if status == "" {
				status = "unknown"
			}
			if status != "not_applicable" {
				switch kind {
				case "annex_1":
					status = "accepted"
					commission := checks["annex_1_commission"]
					if commission == "record_found" {
						commission = "accepted"
					}
					for _, v := range []string{commission, checks["annex_1_function"], checks["annex_1_instruction"], control("STK", "annex_1_stk")} {
						merge(&status, v)
					}
				case "annex_2":
					status = "accepted"
					merge(&status, control("MTK", "annex_2_mtk"))
				case "other":
					status = "accepted"
					merge(&status, checks["other_requirements"])
					merge(&status, control("electrical", "electrical_safety"))
				}
			}
		}
		if duties.Status == "blocked" || len(record.ActiveBlocks) > 0 || record.Decommission != nil {
			status = "blocked"
		}
		// A subordinate failed proof must survive aggregation even if the parent is unknown.
		if checks[kind] == "blocked" {
			status = "blocked"
		}
		reason := "Aktuelle Teilnachweise fehlen; vollständigen Pflichtumfang gesondert prüfen."
		if status == "accepted" {
			reason = "Erfasste erforderliche Teilnachweise aktuell; vollständigen Bereich ausdrücklich fachlich abnehmen."
		}
		if status == "not_applicable" {
			reason = "Begründete Ausnahme dokumentiert; Vollständigkeit und Tragfähigkeit ausdrücklich fachlich prüfen."
		}
		if status == "blocked" {
			reason = "Pflichtnachweis oder Gerät aktuell blockiert; keine positive Erfüllungsabnahme."
		}
		sources = append(sources, productDutyCheck{Kind: kind, Status: status, Reason: reason})
		if status == "blocked" {
			state = "blocked"
		} else if status != "accepted" && status != "not_applicable" && state != "blocked" {
			state = "unknown"
		}
	}
	return state, sources
}

func dutyFulfilmentBasis(ctx context.Context, tx *sql.Tx, record deviceRecord, duties productDutyAssessment, sources []productDutyCheck) (string, error) {
	snapshot, err := currentDutyPreparationSnapshot(ctx, tx, record)
	if err != nil {
		return "", err
	}
	value, err := json.Marshal(struct {
		Snapshot string
		Version  int
		Sources  []productDutyCheck
	}{snapshot, duties.Version, sources})
	if err != nil {
		return "", err
	}
	return digest("duty_fulfilment:" + record.ID + ":" + string(value)), nil
}
func assessDutyFulfilmentWith(ctx context.Context, tx *sql.Tx, record deviceRecord, duties productDutyAssessment, now time.Time) (result dutyFulfilmentAssessment, assessmentErr error) {
	result = dutyFulfilmentAssessment{DeviceVersion: record.Version, Status: "unknown", Reasons: []string{}, Events: []dutyFulfilmentEvent{}, Overall: "not_assessed"}
	defer func() {
		if assessmentErr == nil {
			var inactive bool
			inactive, assessmentErr = maskModuleAssessment(ctx, tx, "compliance", &result.Status, &result.Reasons)
			if inactive {
				for i := range result.Prerequisites {
					if result.Prerequisites[i].Status != "blocked" {
						result.Prerequisites[i].Status = "unknown"
					}
				}
			}
		}
	}()
	state, sources := dutyFulfilmentPrerequisites(record, duties)
	result.Prerequisites = sources
	var err error
	result.Basis, err = dutyFulfilmentBasis(ctx, tx, record, duties, sources)
	if err != nil {
		return result, err
	}
	var count int
	if err = tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(version),0),COUNT(*) FROM duty_fulfilment_events WHERE device_id=?`, record.ID).Scan(&result.Version, &count); err != nil {
		return result, err
	}
	if count > 2000 {
		return result, &stockError{413, "duty_fulfilment_limit", "Mehr als 2000 Erfüllungsabnahmen; keine Teilbewertung."}
	}
	rows, err := tx.QueryContext(ctx, `SELECT payload_json FROM duty_fulfilment_events WHERE device_id=? ORDER BY version DESC LIMIT 100`, record.ID)
	if err != nil {
		return result, err
	}
	for rows.Next() {
		var raw string
		var event dutyFulfilmentEvent
		if err = rows.Scan(&raw); err != nil {
			rows.Close()
			return result, err
		}
		if err = json.Unmarshal([]byte(raw), &event); err != nil {
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
	if state == "blocked" {
		result.Status = "blocked"
		result.Reasons = append(result.Reasons, "Pflichtnachweis oder Gerät aktuell blockiert.")
	}
	if len(result.Events) == 0 {
		result.Reasons = append(result.Reasons, "Noch keine ausdrückliche fachliche Erfüllungsabnahme der acht Produktpflichtbereiche.")
		return result, nil
	}
	last := result.Events[0]
	if last.Input.Action == "reject" || last.Input.Action == "revoke" {
		result.Status = "blocked"
		result.Reasons = append(result.Reasons, "Erfüllungsabnahme ausdrücklich abgelehnt oder widerrufen.")
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
		result.Reasons = append(result.Reasons, "Erfüllungsabnahme abgelaufen oder noch nicht aktuell; ausdrücklich erneuern.")
		return result, nil
	}
	if state != "met" || last.Basis != result.Basis {
		result.Reasons = append(result.Reasons, "Gebundene Produkt-, Geräte-, Personen-, Unterlagen- oder Aufbereitungsnachweise geändert / unvollständig; erneut abnehmen.")
		return result, nil
	}
	var status, sha string
	var version int
	if err = tx.QueryRowContext(ctx, `SELECT status,sha256,review_version FROM effective_documents WHERE device_id=? AND id=?`, record.ID, last.Input.DocumentID).Scan(&status, &sha, &version); err != nil {
		return result, err
	}
	if status != "released" || sha != last.DocumentSHA256 || version != last.DocumentReviewVersion {
		result.Reasons = append(result.Reasons, "Gebundene Erfüllungsunterlage nicht mehr im bestätigten Freigabestand.")
		return result, nil
	}
	result.Status = "accepted"
	result.Reasons = append(result.Reasons, "Acht Produktpflichtbereiche ausdrücklich fachlich abgenommen und aktuell gebunden; Material, konkrete Endanwender und gesamte Einsatzfreigabe bleiben getrennt.")
	return result, nil
}
func assessDutyFulfilment(ctx context.Context, tx *sql.Tx, actor string, record deviceRecord, now time.Time) (dutyFulfilmentAssessment, error) {
	duties, err := assessProductDuty(ctx, tx, actor, record, now)
	if err != nil {
		return dutyFulfilmentAssessment{}, err
	}
	return assessDutyFulfilmentWith(ctx, tx, record, duties, now)
}
