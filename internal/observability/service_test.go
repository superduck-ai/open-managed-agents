package observability

import (
	"errors"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/apperr"
)

func TestTrajectoryInputErrorsHaveStableContracts(t *testing.T) {
	for _, test := range []struct {
		name    string
		run     func() error
		message string
	}{
		{"invalid time", func() error { _, err := ParseWindow("bad", ""); return err }, "invalid trajectory time range"},
		{"invalid window", func() error { _, err := ParseWindow("2026-01-02T00:00:00Z", "2026-01-01T00:00:00Z"); return err }, "trajectory time range must be positive and at most one year"},
		{"cursor", func() error { _, err := ParseCursor("-2"); return err }, "invalid trajectory cursor"},
		{"offset", func() error { _, err := ParseOffset("100001"); return err }, "invalid trajectory offset"},
		{"id", MissingTrajectoryID, "trajectory id is required"},
	} {
		t.Run(test.name, func(t *testing.T) {
			var appError *apperr.Error
			if !errors.As(test.run(), &appError) || appError.Kind != apperr.InvalidArgument || appError.PublicMessage != test.message {
				t.Fatalf("unexpected error: %+v", appError)
			}
		})
	}
}

func TestTrajectoryPositionDefaultsAndLimits(t *testing.T) {
	for _, test := range []struct {
		raw  string
		want int64
	}{{"", -1}, {"-1", -1}, {"0", 0}, {"42", 42}} {
		got, err := ParseCursor(test.raw)
		if err != nil || got != test.want {
			t.Fatalf("cursor %q: %d %v", test.raw, got, err)
		}
	}
	for _, test := range []struct {
		raw  string
		want int
	}{{"", 0}, {"0", 0}, {"100000", 100000}} {
		got, err := ParseOffset(test.raw)
		if err != nil || got != test.want {
			t.Fatalf("offset %q: %d %v", test.raw, got, err)
		}
	}
}
