package admin

import (
	"errors"
	"time"
)

func validateGroupType(groupType string) error {
	if groupType == "" {
		return nil
	}
	switch groupType {
	case "model_group", "batch", "token_count", "files", "skills", "web_search":
		return nil
	default:
		return errors.New("invalid group_type")
	}
}

func validateMessagesReportQuery(query reportQuery) error {
	if _, err := time.Parse(time.RFC3339, query.StartingAt); err != nil {
		return errors.New("starting_at must be an RFC 3339 timestamp")
	}
	if query.EndingAt != "" {
		if _, err := time.Parse(time.RFC3339, query.EndingAt); err != nil {
			return errors.New("ending_at must be an RFC 3339 timestamp")
		}
	}
	switch query.BucketWidth {
	case "", "1d", "1h", "1m":
		return nil
	default:
		return errors.New("invalid bucket_width")
	}
}

func validateClaudeCodeReportQuery(query reportQuery) error {
	if _, err := time.Parse("2006-01-02", query.StartingAt); err != nil {
		return errors.New("starting_at must be a UTC date in YYYY-MM-DD format")
	}
	return nil
}

func validateCostReportQuery(query reportQuery) error {
	if _, err := time.Parse(time.RFC3339, query.StartingAt); err != nil {
		return errors.New("starting_at must be an RFC 3339 timestamp")
	}
	if query.EndingAt != "" {
		if _, err := time.Parse(time.RFC3339, query.EndingAt); err != nil {
			return errors.New("ending_at must be an RFC 3339 timestamp")
		}
	}
	if query.BucketWidth != "" && query.BucketWidth != "1d" {
		return errors.New("invalid bucket_width")
	}
	return nil
}
