package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"
)

type packReleaseInput struct {
	ExpectedVersion    int    `json:"expected_version"`
	ExpectedBasis      string `json:"expected_basis,omitempty"`
	UserID             string `json:"user_id"`
	Action             string `json:"action"`
	Reviewed           *bool  `json:"reviewed,omitempty"`
	PerformedOn        string `json:"performed_on,omitempty"`
	ValidUntil         string `json:"valid_until,omitempty"`
	MissionReference   string `json:"mission_reference,omitempty"`
	OperatingScope     string `json:"operating_scope,omitempty"`
	ConditionsEvidence string `json:"conditions_evidence,omitempty"`
	HandoverEvidence   string `json:"handover_evidence,omitempty"`
	Evidence           string `json:"evidence"`
	Qualification      string `json:"qualification"`
	Assignment         string `json:"assignment"`
	Note               string `json:"note"`
}
type packReleaseEvent struct {
	ID           string              `json:"id"`
	Version      int                 `json:"version"`
	ActorID      string              `json:"actor_id"`
	ActorVersion int                 `json:"actor_administrative_version"`
	CreatedAt    string              `json:"created_at"`
	Input        packReleaseInput    `json:"input"`
	Basis        string              `json:"evidence_basis,omitempty"`
	Components   []evidenceComponent `json:"components,omitempty"`
}
type packReleaseAssessment struct {
	Current       *packReleaseEvent  `json:"current_decision,omitempty"`
	UserID        string             `json:"user_id"`
	Version       int                `json:"version"`
	Status        string             `json:"status"`
	Overall       string             `json:"overall_readiness"`
	Reasons       []string           `json:"reasons"`
	Basis         string             `json:"basis_hash,omitempty"`
	Prerequisites operatingEvidence  `json:"prerequisites"`
	Events        []packReleaseEvent `json:"events,omitempty"`
}

func packReleaseManager(ctx context.Context, tx *sql.Tx, user, location string) (bool, error) {
	var ok bool
	err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM locations l JOIN users u ON u.id=? AND u.active=1 WHERE l.id=? AND (SELECT COUNT(DISTINCT role) FROM role_assignments a WHERE a.site_id=l.site_id AND a.user_id=u.id AND a.role IN ('material_manager','device_manager'))=2)`, user, location).Scan(&ok)
	return ok, err
}
func releaseUserExists(ctx context.Context, tx *sql.Tx, subject string, p packVersion) (bool, error) {
	var ok bool
	err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM users u WHERE u.id=? AND (EXISTS(SELECT 1 FROM role_assignments a JOIN locations l ON l.site_id=a.site_id WHERE a.user_id=u.id AND l.id=?) OR EXISTS(SELECT 1 FROM pack_release_events e WHERE e.subject_id=u.id AND e.packlist_id=?)))`, subject, p.Location, p.ID).Scan(&ok)
	return ok, err
}
func administrativeVersion(ctx context.Context, tx *sql.Tx, kind, id string) (int, error) {
	var version int
	err := tx.QueryRowContext(ctx, `SELECT version FROM administrative_versions WHERE kind=? AND object_id=?`, kind, id).Scan(&version)
	return version, err
}
func packReleaseBasis(ctx context.Context, tx *sql.Tx, subject string, p packVersion, result packAssessment) (string, error) {
	type deviceSource struct {
		ID, Snapshot, EndUserBasis                                                  string
		DutyVersion, FulfilmentVersion, UsageVersion, ManualVersion, EndUserVersion int
	}
	sources := []deviceSource{}
	for _, d := range result.Devices {
		snapshot, err := currentDutyPreparationSnapshot(ctx, tx, d.Device)
		if err != nil {
			return "", err
		}
		source := deviceSource{ID: d.Device.ID, Snapshot: snapshot}
		if d.ProductDuties != nil {
			source.DutyVersion = d.ProductDuties.Version
		}
		if d.DutyFulfilment != nil {
			source.FulfilmentVersion = d.DutyFulfilment.Version
		}
		if d.UserUsage != nil {
			source.UsageVersion = d.UserUsage.Version
		}
		if d.ManualAccess != nil {
			source.ManualVersion = d.ManualAccess.Version
		}
		if d.EndUser != nil {
			source.EndUserVersion = d.EndUser.Version
			source.EndUserBasis = d.EndUser.Basis
		}
		sources = append(sources, source)
	}
	var site, operator string
	if err := tx.QueryRowContext(ctx, `SELECT l.site_id,s.operator_id FROM locations l JOIN sites s ON s.id=l.site_id WHERE l.id=?`, p.Location).Scan(&site, &operator); err != nil {
		return "", err
	}
	versions := []int{}
	for _, object := range []struct{ Kind, ID string }{{"user", subject}, {"location", p.Location}, {"site", site}, {"operator", operator}} {
		version, err := administrativeVersion(ctx, tx, object.Kind, object.ID)
		if err != nil {
			return "", err
		}
		versions = append(versions, version)
	}
	materialCheck, deviceCheck := "", ""
	if result.Material.Check != nil {
		materialCheck = result.Material.Check.ID
	}
	if result.Check != nil {
		deviceCheck = result.Check.ID
	}
	_, moduleRevision, err := currentModules(ctx, tx)
	if err != nil {
		return "", err
	}
	value, err := json.Marshal(struct {
		Pack, Subject, Site, Operator, Material, Devices, MaterialCheck, DeviceCheck string
		ModuleRevision                                                               int
		Versions                                                                     []int
		Sources                                                                      []deviceSource
		Evidence                                                                     operatingEvidence
	}{p.ID, subject, site, operator, result.Material.Basis, result.Basis, materialCheck, deviceCheck, moduleRevision, versions, sources, result.Evidence})
	if err != nil {
		return "", err
	}
	return digest("pack_release:" + string(value)), nil
}
func assessPackReleaseWith(ctx context.Context, tx *sql.Tx, subject string, p packVersion, result packAssessment, now time.Time) (packReleaseAssessment, error) {
	assessment := packReleaseAssessment{UserID: subject, Status: "unknown", Overall: "not_assessed", Reasons: []string{}, Events: []packReleaseEvent{}, Prerequisites: result.Evidence}
	var err error
	assessment.Basis, err = packReleaseBasis(ctx, tx, subject, p, result)
	if err != nil {
		return assessment, err
	}
	var count int
	if err = tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(version),0),COUNT(*) FROM pack_release_events WHERE packlist_id=?`, p.ID).Scan(&assessment.Version, &count); err != nil {
		return assessment, err
	}
	if count > 2000 {
		return assessment, &stockError{413, "pack_release_limit", "Mehr als 2000 Einsatzfreigaben; keine Teilbewertung."}
	}
	rows, err := tx.QueryContext(ctx, `SELECT payload_json FROM pack_release_events WHERE packlist_id=? AND subject_id=? ORDER BY version DESC LIMIT 100`, p.ID, subject)
	if err != nil {
		return assessment, err
	}
	for rows.Next() {
		var raw string
		var event packReleaseEvent
		if err = rows.Scan(&raw); err != nil {
			rows.Close()
			return assessment, err
		}
		if err = json.Unmarshal([]byte(raw), &event); err != nil {
			rows.Close()
			return assessment, err
		}
		assessment.Events = append(assessment.Events, event)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return assessment, err
	}
	if result.Evidence.Status == "blocked" {
		assessment.Status = "blocked"
		assessment.Reasons = append(assessment.Reasons, "Eine Einsatzvoraussetzung ist aktuell blockiert.")
	}
	if len(assessment.Events) == 0 {
		assessment.Reasons = append(assessment.Reasons, "Noch keine ausdrückliche Einsatzfreigabe für diese Person und Sollversion.")
		return assessment, nil
	}
	assessment.Overall = "not_released"
	last := assessment.Events[0]
	assessment.Current = &last
	if last.Input.Action == "reject" || last.Input.Action == "revoke" {
		assessment.Status = "blocked"
		assessment.Reasons = append(assessment.Reasons, "Einsatzfreigabe ausdrücklich abgelehnt oder widerrufen.")
		return assessment, nil
	}
	configuration, _, err := currentModules(ctx, tx)
	if err != nil {
		return assessment, err
	}
	if !configuration.Enabled("release") {
		assessment.Reasons = append(assessment.Reasons, "Einsatzfreigabe-Modul deaktiviert. Historische Freigaben gelten nicht als aktuelle Freigabe.")
		return assessment, nil
	}
	stamp, err := time.Parse(time.RFC3339Nano, last.CreatedAt)
	if err != nil {
		return assessment, err
	}
	until, err := time.Parse(time.RFC3339, last.Input.ValidUntil)
	if err != nil {
		return assessment, err
	}
	if now.Before(stamp) || !now.Before(until) {
		assessment.Reasons = append(assessment.Reasons, "Einsatzfreigabe abgelaufen oder noch nicht aktuell; neu prüfen.")
		return assessment, nil
	}
	eligible, err := packReleaseManager(ctx, tx, last.ActorID, p.Location)
	if err != nil {
		return assessment, err
	}
	if !eligible {
		assessment.Status = "blocked"
		assessment.Reasons = append(assessment.Reasons, "Freigebende Person nicht mehr aktiv mit beiden Verantwortungsrechten am Betriebsort.")
		return assessment, nil
	}
	actorVersion, err := administrativeVersion(ctx, tx, "user", last.ActorID)
	if err != nil {
		return assessment, err
	}
	if result.Evidence.Status != "recorded_prerequisites_met" || last.Basis != assessment.Basis || last.ActorVersion != actorVersion {
		assessment.Reasons = append(assessment.Reasons, "Sollversion, Bestände, Prüfungen, Personen-, Produkt-, Verwaltungsnachweise oder Modulkonfiguration geändert / unvollständig; keine automatische Wiederfreigabe.")
		return assessment, nil
	}
	assessment.Status = "released"
	assessment.Overall = "released_for_user"
	assessment.Reasons = append(assessment.Reasons, "Ausdrückliche betriebliche Einsatzfreigabe für diese Person, Sollversion und dokumentierten Einsatzumfang; alle gebundenen Voraussetzungen aktuell.")
	return assessment, nil
}
