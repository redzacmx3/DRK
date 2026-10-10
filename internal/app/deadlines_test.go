package app

import (
	"testing"
	"time"
)

func TestDeadlineCalendar(t *testing.T) {
	for _, tc := range []struct {
		mode, basis, want string
		months            int
	}{
		{"month_end", "2026-03-15", "2028-03-31", 24},
		{"year_end", "2026-03-15", "2028-12-31", 24},
		{"month_end", "2024-02-29", "2026-02-28", 24},
		{"month_end", "2023-01-31", "2023-02-28", 1},
		{"month_end", "2023-02-28", "2024-02-29", 12},
	} {
		rule := DeadlineRule{Version: "v1", Evidence: "confirmed test rule", Applicability: "required", Mode: tc.mode, BasisDate: tc.basis, IntervalMonths: tc.months}
		got, err := CalculateDeadline(rule, time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC), 30)
		if err != nil || got.DueOn != tc.want {
			t.Fatalf("%+v: %+v %v", tc, got, err)
		}
	}
}

func TestDeadlineStatesAndBerlinDay(t *testing.T) {
	rule := DeadlineRule{Version: "1", Evidence: "protocol", Applicability: "required", Mode: "exact_date", BasisDate: "2026-03-01", ExactDate: "2026-03-31"}
	for _, tc := range []struct{ now, state string }{
		{"2026-02-28T12:00:00Z", "CURRENT"},
		{"2026-03-01T12:00:00Z", "DUE_SOON"},
		{"2026-03-31T21:59:59Z", "DUE_SOON"},
		{"2026-03-31T22:00:00Z", "OVERDUE"},
	} {
		now, _ := time.Parse(time.RFC3339, tc.now)
		got, err := CalculateDeadline(rule, now, 30)
		if err != nil || got.State != tc.state {
			t.Fatalf("%s: %+v %v", tc.now, got, err)
		}
	}
	rule.EarlierDate = "2026-03-15"
	rule.EarlierEvidence = "occasion control"
	got, err := CalculateDeadline(rule, time.Date(2026, 3, 16, 0, 0, 0, 0, time.UTC), 7)
	if err != nil || got.DueOn != "2026-03-15" || got.State != "OVERDUE" || got.EarlierEvidence == "" {
		t.Fatalf("shorter: %+v %v", got, err)
	}
	rule.EarlierDate = "2026-04-01"
	if _, err := CalculateDeadline(rule, time.Now(), 7); err == nil {
		t.Fatal("accepted extension")
	}
}

func TestDeadlineUnknownExemptionAndInvalidInput(t *testing.T) {
	now := time.Now()
	for _, r := range []DeadlineRule{{Applicability: "unknown"}, {Applicability: "required", Version: "1", Evidence: "rule"}} {
		got, err := CalculateDeadline(r, now, 30)
		if err != nil || got.State != "UNKNOWN" || got.DueOn != "" {
			t.Fatalf("unknown: %+v %v", got, err)
		}
	}
	exempt := DeadlineRule{Applicability: "exempt", Version: "1", Evidence: "documented product exemption"}
	got, err := CalculateDeadline(exempt, now, 30)
	if err != nil || got.State != "NOT_APPLICABLE" {
		t.Fatal(got, err)
	}
	for _, r := range []DeadlineRule{
		{Applicability: "exempt"},
		{Applicability: "exempt", Version: "1", Evidence: "proof", ExactDate: "2026-01-01"},
		{Applicability: "required", Version: "1", Evidence: "rule", Mode: "month_end", BasisDate: "2026-02-29", IntervalMonths: 24},
		{Applicability: "required", Version: "1", Evidence: "rule", Mode: "year_end", BasisDate: "2026-01-01", IntervalMonths: 18},
		{Applicability: "required", Version: "1", Evidence: "rule", Mode: "month_end", BasisDate: "9999-12-01", IntervalMonths: 24},
		{Applicability: "required", Version: "1", Evidence: "rule", Mode: "exact_date", BasisDate: "2026-01-01", ExactDate: "2025-12-31"},
	} {
		if _, err := CalculateDeadline(r, now, 30); err == nil {
			t.Fatalf("accepted %+v", r)
		}
	}
}

func TestSTKMaximumCannotBeExtended(t *testing.T) {
	rule := DeadlineRule{ControlType: "STK", Version: "1", Evidence: "confirmed rule", Applicability: "required", Mode: "month_end", BasisDate: "2026-03-15", IntervalMonths: 24}
	for _, months := range []int{1, 12, 24, 25, 36} {
		rule.IntervalMonths = months
		got, err := CalculateDeadline(rule, time.Now(), 30)
		if months <= 24 && err != nil {
			t.Fatal(got, err)
		}
		if months > 24 && err == nil {
			t.Fatal("STK maximum extended")
		}
	}
	rule.Mode = "exact_date"
	rule.IntervalMonths = 0
	rule.ExactDate = "2030-03-15"
	if _, err := CalculateDeadline(rule, time.Now(), 30); err == nil {
		t.Fatal("STK maximum bypassed")
	}
}
