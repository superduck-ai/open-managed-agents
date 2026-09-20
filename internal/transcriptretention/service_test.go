package transcriptretention

import (
	"testing"
	"time"
)

func validTestPolicy() Policy {
	return Policy{Enabled: true, TerminalSweepEnabled: true, BoundarySweepEnabled: true, TerminalDwell: 24 * time.Hour, ArchiveMinAge: 7 * 24 * time.Hour, SoftDeleteWindow: 14 * 24 * time.Hour, TargetSegmentRawBytes: 8 * 1024 * 1024, DeleteBatchRows: 500, MaxRowsPerJob: 50000}
}

func TestNewRejectsInvalidPolicy(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*Policy)
	}{
		{"negative_age", func(p *Policy) { p.ArchiveMinAge = -time.Hour }},
		{"zero_age", func(p *Policy) { p.ArchiveMinAge = 0 }},
		{"short_age", func(p *Policy) { p.ArchiveMinAge = 7*24*time.Hour - time.Nanosecond }},
		{"zero_dwell", func(p *Policy) { p.TerminalDwell = 0 }},
		{"negative_dwell", func(p *Policy) { p.TerminalDwell = -time.Hour }},
		{"negative_window", func(p *Policy) { p.SoftDeleteWindow = -time.Hour }},
		{"zero_segment", func(p *Policy) { p.TargetSegmentRawBytes = 0 }},
		{"large_segment", func(p *Policy) { p.TargetSegmentRawBytes = 32*1024*1024 + 1 }},
		{"zero_batch", func(p *Policy) { p.DeleteBatchRows = 0 }},
		{"large_batch", func(p *Policy) { p.DeleteBatchRows = 501 }},
		{"zero_budget", func(p *Policy) { p.MaxRowsPerJob = 0 }},
		{"large_budget", func(p *Policy) { p.MaxRowsPerJob = 50001 }},
	} {
		for _, mode := range []string{"enabled", "disabled", "dry_run", "terminal_only"} {
			t.Run(tc.name+"/"+mode, func(t *testing.T) {
				policy := validTestPolicy()
				policy.Enabled = mode != "disabled"
				policy.DryRun = mode == "dry_run"
				policy.BoundarySweepEnabled = mode != "terminal_only"
				tc.mutate(&policy)
				service, err := New(nil, nil, policy, nil)
				if err == nil || service != nil {
					t.Fatalf("invalid policy accepted: service=%v, error=%v", service, err)
				}
			})
		}
	}
}

func TestNewAcceptsValidPolicy(t *testing.T) {
	for _, age := range []time.Duration{7 * 24 * time.Hour, 7*24*time.Hour + time.Nanosecond, 14 * 24 * time.Hour} {
		t.Run(age.String(), func(t *testing.T) {
			policy := validTestPolicy()
			policy.ArchiveMinAge = age
			policy.SoftDeleteWindow = 0
			service, err := New(nil, nil, policy, nil)
			if err != nil {
				t.Fatal(err)
			}
			if service == nil || service.policy != policy {
				t.Fatal("constructor changed the valid policy")
			}
		})
	}
}
