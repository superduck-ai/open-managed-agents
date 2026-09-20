package transcriptretention

import (
	"errors"
	"testing"
	"time"
)

func TestNewRejectsUnsafeArchiveMinAge(t *testing.T) {
	for _, age := range []time.Duration{-time.Hour, 0, time.Hour, 7*24*time.Hour - time.Nanosecond} {
		for _, mode := range []struct {
			name   string
			policy Policy
		}{
			{"enabled", Policy{Enabled: true, BoundarySweepEnabled: true}},
			{"dry_run", Policy{Enabled: true, BoundarySweepEnabled: true, DryRun: true}},
			{"disabled", Policy{}},
			{"terminal_only", Policy{Enabled: true, TerminalSweepEnabled: true}},
		} {
			t.Run(mode.name+"/"+age.String(), func(t *testing.T) {
				policy := mode.policy
				policy.ArchiveMinAge = age
				service, err := New(nil, nil, policy, nil)
				if !errors.Is(err, errArchiveMinAge) || service != nil {
					t.Fatalf("New returned %v, %v; want nil service and minimum-age error", service, err)
				}
			})
		}
	}
}

func TestNewAcceptsSafeArchiveMinAge(t *testing.T) {
	for _, age := range []time.Duration{7 * 24 * time.Hour, 7*24*time.Hour + time.Nanosecond, 14 * 24 * time.Hour} {
		t.Run(age.String(), func(t *testing.T) {
			policy := Policy{Enabled: true, BoundarySweepEnabled: true, ArchiveMinAge: age}
			service, err := New(nil, nil, policy, nil)
			if err != nil {
				t.Fatal(err)
			}
			if service == nil || service.policy != policy {
				t.Fatal("New must preserve the supplied valid policy")
			}
		})
	}
}
