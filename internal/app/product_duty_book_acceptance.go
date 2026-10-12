package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"
)

// Professional attestation of book completeness, access and retention; not operational release.
type dutyBookAcceptance struct {
	ExpectedSnapshot     string `json:"expected_snapshot"`
	Reviewed             bool   `json:"reviewed"`
	Decision             string `json:"decision"`
	PerformedOn          string `json:"performed_on"`
	DocumentID           string `json:"document_id"`
	Qualification        string `json:"qualification"`
	Assignment           string `json:"assignment"`
	Evidence             string `json:"evidence"`
	Note                 string `json:"note"`
	CompletenessEvidence string `json:"completeness_evidence"`
	AccessEvidence       string `json:"access_evidence"`
	RetentionEvidence    string `json:"retention_evidence"`
}
type dutyBookBasis struct {
	Snapshot              string `json:"snapshot"`
	SourceReviewVersion   int    `json:"source_review_version"`
	DocumentReviewVersion int    `json:"document_review_version"`
	DocumentSHA256        string `json:"document_sha256"`
}

func currentDutyBookSnapshot(ctx context.Context, tx *sql.Tx, record deviceRecord) (string, error) {
	// Bind all current book records without exposing private person event identifiers.
	versions := []int{record.Version}
	for _, table := range []string{"usage_events", "manual_access_events", "end_user_acceptance_events"} {
		var version int
		if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(version),0) FROM `+table+` WHERE device_id=?`, record.ID).Scan(&version); err != nil {
			return "", err
		}
		versions = append(versions, version)
	}
	type documentBasis struct {
		ID      string
		Version int
		Status  string
		SHA     string
	}
	documents := []documentBasis{}
	rows, err := tx.QueryContext(ctx, `SELECT id,review_version,status,sha256 FROM effective_documents WHERE device_id=? ORDER BY id`, record.ID)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	for rows.Next() {
		var d documentBasis
		if err := rows.Scan(&d.ID, &d.Version, &d.Status, &d.SHA); err != nil {
			return "", err
		}
		documents = append(documents, d)
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	b, err := json.Marshal(struct {
		Versions  []int
		Documents []documentBasis
	}{versions, documents})
	if err != nil {
		return "", err
	}
	return digest("device_book:" + record.ID + ":" + string(b)), nil
}
func validateDutyBookAcceptance(ctx context.Context, tx *sql.Tx, rule productDutyRule, record deviceRecord, source string, now time.Time) (*dutyBookBasis, error) {
	a := rule.BookAcceptance
	if a == nil {
		return nil, nil
	}
	if rule.Kind != "device_book" || rule.Applicability != "required" {
		return nil, adminInvalid("Buchabnahme nur für erforderliches Medizinproduktebuch erfassen.")
	}
	day, err := time.Parse("2006-01-02", a.PerformedOn)
	if err != nil || day.Format("2006-01-02") != a.PerformedOn || a.PerformedOn > now.UTC().Format("2006-01-02") || !a.Reviewed || (a.Decision != "accepted" && a.Decision != "rejected") {
		return nil, adminInvalid("Buchabnahme ausdrücklich bestätigen, Entscheidung und gültiges Datum bis heute angeben.")
	}
	for _, value := range []string{a.Qualification, a.Assignment, a.Evidence, a.Note, a.CompletenessEvidence, a.AccessEvidence, a.RetentionEvidence} {
		if !validReason(value) {
			return nil, adminInvalid("Abnahmekompetenz, Beauftragung, Nachweis, Begründung sowie Vollständigkeit, Benutzerzugang und Aufbewahrung vollständig belegen.")
		}
	}
	basis := &dutyBookBasis{}
	var status string
	err = tx.QueryRowContext(ctx, `SELECT status,review_version,sha256 FROM effective_documents WHERE device_id=? AND id=?`, record.ID, a.DocumentID).Scan(&status, &basis.DocumentReviewVersion, &basis.DocumentSHA256)
	if err == sql.ErrNoRows {
		return nil, scopeError()
	}
	if err != nil {
		return nil, err
	}
	if status != "released" {
		return nil, conflict("book_proof_not_released", "Buchabnahmeunterlage nicht freigegeben.")
	}
	err = tx.QueryRowContext(ctx, `SELECT status,review_version FROM effective_documents WHERE device_id=? AND id=?`, record.ID, source).Scan(&status, &basis.SourceReviewVersion)
	if err == sql.ErrNoRows {
		return nil, scopeError()
	}
	if err != nil {
		return nil, err
	}
	if status != "released" {
		return nil, conflict("product_source_not_released", "Produktgrundlage nicht freigegeben.")
	}
	basis.Snapshot, err = currentDutyBookSnapshot(ctx, tx, record)
	if err != nil {
		return nil, err
	}
	if a.ExpectedSnapshot == "" || a.ExpectedSnapshot != basis.Snapshot {
		return nil, conflict("book_snapshot_changed", "Aktenstand seit Laden geändert; Buchabnahme erneut prüfen.")
	}
	return basis, nil
}
func checkDutyBookAcceptance(ctx context.Context, tx *sql.Tx, record deviceRecord, source string, rule productDutyRule, basis *dutyBookBasis, check *productDutyCheck) error {
	a := rule.BookAcceptance
	if a == nil {
		return nil
	}
	if a.Decision == "rejected" {
		check.Status = "blocked"
		check.Reason = "Vollständigkeit, Zugänglichkeit oder Aufbewahrung des Medizinproduktebuchs ausdrücklich nicht abgenommen."
		return nil
	}
	if !a.Reviewed || a.Decision != "accepted" || basis == nil {
		return nil
	}
	snapshot, err := currentDutyBookSnapshot(ctx, tx, record)
	if err != nil {
		return err
	}
	if snapshot != basis.Snapshot {
		check.Status = "unknown"
		check.Reason = "Geräte-, Personen- oder Dokumentnachweise seit Buchabnahme geändert; Vollständigkeit und Zugang erneut prüfen."
		return nil
	}
	var status, sha string
	var version int
	err = tx.QueryRowContext(ctx, `SELECT status,review_version,sha256 FROM effective_documents WHERE device_id=? AND id=?`, record.ID, a.DocumentID).Scan(&status, &version, &sha)
	if err != nil {
		return err
	}
	if status != "released" || version != basis.DocumentReviewVersion || sha != basis.DocumentSHA256 {
		return nil
	}
	err = tx.QueryRowContext(ctx, `SELECT status,review_version FROM effective_documents WHERE device_id=? AND id=?`, record.ID, source).Scan(&status, &version)
	if err != nil {
		return err
	}
	if status != "released" || version != basis.SourceReviewVersion {
		return nil
	}
	check.Status = "accepted"
	check.Reason = "Buchvollständigkeit, Benutzerzugang und Aufbewahrung für den gebundenen Aktenstand ausdrücklich fachlich abgenommen; Gesamtfreigabe offen."
	return nil
}
