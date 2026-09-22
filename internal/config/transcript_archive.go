package config

import (
	"fmt"
	"time"
)

func ValidateTranscriptArchive(cfg TranscriptArchiveConfig) error {
	if cfg.TerminalDwell <= 0 || cfg.ArchiveMinAge < 168*time.Hour || cfg.SoftDeleteWindow < 0 {
		return fmt.Errorf("transcript_archive requires positive terminal_dwell, archive_min_age >= 168h and soft_delete_window >= 0")
	}
	if cfg.TargetSegmentRawBytes < 1 || cfg.TargetSegmentRawBytes > 32*1024*1024 || cfg.DeleteBatchRows < 1 || cfg.DeleteBatchRows > 500 || cfg.MaxRowsPerJob < 1 || cfg.MaxRowsPerJob > 50000 {
		return fmt.Errorf("transcript_archive requires target_segment_raw_bytes 1..33554432, delete_batch_rows 1..500 and max_rows_per_job 1..50000")
	}
	return nil
}
