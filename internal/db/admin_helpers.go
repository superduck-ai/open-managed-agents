package db

import (
	"time"
)

type pagePosition struct {
	CreatedAt time.Time `db:"created_at"`
	UUID      string    `db:"uuid"`
}

func trimAdminPage[T any](items []T, limit int) []T {
	if limit > 0 && len(items) > limit {
		return items[:limit]
	}
	return items
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
