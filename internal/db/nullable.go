package db

import "time"

// nullableString translates an absent domain value to SQL NULL at the mapper boundary.
func nullableString(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func stringFromNullable(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

// nullableTime translates an unset domain time to SQL NULL at the mapper boundary.
func nullableTime(value time.Time) *time.Time {
	if value.IsZero() {
		return nil
	}
	return &value
}

func timeFromNullable(value *time.Time) time.Time {
	if value == nil {
		return time.Time{}
	}
	return *value
}
