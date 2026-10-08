package config

import (
	"go.yaml.in/yaml/v3"
	"testing"
	"time"
)

func TestTranscriptArchiveConfiguration(t *testing.T) {
	original := newYAMLConfig().resolve().TranscriptArchive
	for _, mutate := range []func(*TranscriptArchiveConfig){
		func(c *TranscriptArchiveConfig) { c.TerminalDwell = 0 }, func(c *TranscriptArchiveConfig) { c.ArchiveMinAge = time.Hour }, func(c *TranscriptArchiveConfig) { c.SoftDeleteWindow = -1 }, func(c *TranscriptArchiveConfig) { c.TargetSegmentRawBytes = 0 }, func(c *TranscriptArchiveConfig) { c.TargetSegmentRawBytes = 33554433 }, func(c *TranscriptArchiveConfig) { c.DeleteBatchRows = 0 }, func(c *TranscriptArchiveConfig) { c.DeleteBatchRows = 501 }, func(c *TranscriptArchiveConfig) { c.MaxRowsPerJob = 0 }, func(c *TranscriptArchiveConfig) { c.MaxRowsPerJob = 50001 },
	} {
		cfg := original
		mutate(&cfg)
		if ValidateTranscriptArchive(cfg) == nil {
			t.Fatal("unsafe configuration accepted")
		}
	}
	if original.Enabled || !original.DryRun || !original.TerminalSweepEnabled || original.BoundarySweepEnabled || original.HardDeleteEnabled {
		t.Fatalf("unsafe defaults: %+v", original)
	}
	if original.TerminalDwell != 24*time.Hour || original.ArchiveMinAge != 168*time.Hour || original.SoftDeleteWindow != 336*time.Hour || original.TargetSegmentRawBytes != 8388608 || original.DeleteBatchRows != 500 || original.MaxRowsPerJob != 50000 {
		t.Fatalf("incorrect defaults: %+v", original)
	}
	input := newYAMLConfig()
	if err := yaml.Unmarshal([]byte("transcript_archive:\n  enabled: true\n  soft_delete_window: 0s\n  delete_batch_rows: 13\n"), &input); err != nil {
		t.Fatal(err)
	}
	cfg := input.resolve().TranscriptArchive
	if !cfg.Enabled || cfg.SoftDeleteWindow != 0 || cfg.DeleteBatchRows != 13 || !cfg.DryRun {
		t.Fatalf("configuration mapping: %+v", cfg)
	}
	if err := ValidateTranscriptArchive(cfg); err != nil {
		t.Fatal(err)
	}
}
