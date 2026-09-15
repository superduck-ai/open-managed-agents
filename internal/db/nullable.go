package db

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
