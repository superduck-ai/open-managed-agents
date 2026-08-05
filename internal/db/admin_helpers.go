package db

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

type pagePosition struct {
	CreatedAt time.Time `db:"created_at"`
	UUID      uuid.UUID `db:"uuid"`
}

func appendCursorFilter(
	query string,
	arguments map[string]any,
	column, afterID, beforeID string,
	cursor *pagePosition,
) string {
	if afterID == "" && beforeID == "" {
		return query
	}
	if cursor == nil {
		return query
	}
	uuidColumn := "uuid"
	if dot := strings.LastIndex(column, "."); dot > 0 {
		uuidColumn = column[:dot] + ".uuid"
	}
	if afterID != "" {
		query += fmt.Sprintf(
			" and (%s < :cursor_created_at or (%s = :cursor_created_at and %s < :cursor_uuid))",
			column,
			column,
			uuidColumn,
		)
	} else {
		query += fmt.Sprintf(
			" and (%s > :cursor_created_at or (%s = :cursor_created_at and %s > :cursor_uuid))",
			column,
			column,
			uuidColumn,
		)
	}
	arguments["cursor_created_at"] = cursor.CreatedAt
	arguments["cursor_uuid"] = cursor.UUID
	return query
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
