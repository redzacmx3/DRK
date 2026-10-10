package app

import (
	"fmt"
	"time"
)

// DeadlineRule is an explicitly confirmed product-specific rule, not a
// classification inferred from a device name. Dates are civil Berlin dates.
type DeadlineRule struct {
	ControlType    string `json:"control_type"` // STK, MTK, electrical, other
	Version        string `json:"version"`
	Evidence       string `json:"evidence"`
	Applicability  string `json:"applicability"` // required, exempt, unknown
	Mode           string `json:"mode"`          // month_end, year_end, exact_date
	IntervalMonths int    `json:"interval_months"`
	BasisDate      string `json:"basis_date"`
	ExactDate      string `json:"exact_date"`
	// EarlierDate records a shorter or occasion-related deadline. It cannot
	// extend the calculated deadline. Its evidence is independently required.
	EarlierDate     string `json:"earlier_date"`
	EarlierEvidence string `json:"earlier_evidence"`
}

type Deadline struct {
	State           string `json:"state"`
	DueOn           string `json:"due_on,omitempty"`
	BasisDate       string `json:"basis_date,omitempty"`
	RuleVersion     string `json:"rule_version,omitempty"`
	Evidence        string `json:"evidence,omitempty"`
	EarlierEvidence string `json:"earlier_evidence,omitempty"`
}

func civilDate(value string) (time.Time, error) {
	date, err := time.Parse("2006-01-02", value)
	if err != nil || date.Format("2006-01-02") != value || date.Year() < 1 {
		return time.Time{}, fmt.Errorf("invalid civil date")
	}
	return date, nil
}

// CalculateDeadline only assesses a single control deadline. It never releases
// a device, closes a block, or asserts overall operational readiness.
func CalculateDeadline(rule DeadlineRule, now time.Time, warningDays int) (Deadline, error) {
	result := Deadline{State: "UNKNOWN", RuleVersion: rule.Version, Evidence: rule.Evidence}
	if warningDays < 0 || warningDays > 365 {
		return result, fmt.Errorf("invalid warning period")
	}
	if rule.Applicability == "unknown" {
		return result, nil
	}
	if rule.Version == "" || rule.Evidence == "" {
		return result, fmt.Errorf("confirmed rule version and evidence required")
	}
	if rule.Applicability == "exempt" {
		if rule.BasisDate != "" || rule.ExactDate != "" || rule.EarlierDate != "" || rule.Mode != "" || rule.IntervalMonths != 0 {
			return result, fmt.Errorf("exemption cannot contain a deadline")
		}
		result.State = "NOT_APPLICABLE"
		return result, nil
	}
	if rule.Applicability != "required" {
		return result, fmt.Errorf("invalid applicability")
	}
	if rule.BasisDate == "" {
		return result, nil
	}
	basis, err := civilDate(rule.BasisDate)
	if err != nil {
		return result, err
	}
	result.BasisDate = rule.BasisDate
	var due time.Time
	switch rule.Mode {
	case "month_end", "year_end":
		if rule.IntervalMonths < 1 || rule.IntervalMonths > 120 || rule.ExactDate != "" {
			return result, fmt.Errorf("invalid interval")
		}
		if rule.ControlType == "STK" && (rule.Mode != "month_end" || rule.IntervalMonths > 24) {
			return result, fmt.Errorf("STK requires month-end calculation and at most 24 months")
		}
		if rule.Mode == "year_end" && rule.IntervalMonths%12 != 0 {
			return result, fmt.Errorf("year-end interval must use whole years")
		}
		// Start on day one to avoid Go's March normalization of February 29.
		month := time.Date(basis.Year(), basis.Month(), 1, 0, 0, 0, 0, time.UTC).AddDate(0, rule.IntervalMonths, 0)
		due = month.AddDate(0, 1, 0).AddDate(0, 0, -1)
		if rule.Mode == "year_end" {
			due = time.Date(month.Year(), 12, 31, 0, 0, 0, 0, time.UTC)
		}
	case "exact_date":
		if rule.ControlType == "STK" {
			return result, fmt.Errorf("STK requires a calculated maximum; use earlier_date for shorter deadlines")
		}
		if rule.IntervalMonths != 0 {
			return result, fmt.Errorf("exact deadline cannot contain an interval")
		}
		due, err = civilDate(rule.ExactDate)
		if err != nil {
			return result, err
		}
	default:
		return result, fmt.Errorf("invalid calculation mode")
	}
	if due.Before(basis) || due.Year() > 9999 {
		return result, fmt.Errorf("deadline outside supported range")
	}
	if rule.EarlierDate != "" {
		earlier, err := civilDate(rule.EarlierDate)
		if err != nil {
			return result, err
		}
		if rule.EarlierEvidence == "" || earlier.After(due) {
			return result, fmt.Errorf("shorter deadline requires evidence and cannot extend interval")
		}
		due = earlier
		result.EarlierEvidence = rule.EarlierEvidence
	} else if rule.EarlierEvidence != "" {
		return result, fmt.Errorf("earlier evidence requires a date")
	}
	berlin, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		return result, err
	}
	today, err := civilDate(now.In(berlin).Format("2006-01-02"))
	if err != nil {
		return result, err
	}
	result.DueOn = due.Format("2006-01-02")
	result.State = "CURRENT"
	if due.Before(today) {
		result.State = "OVERDUE"
	} else if !due.After(today.AddDate(0, 0, warningDays)) {
		result.State = "DUE_SOON"
	}
	return result, nil
}
