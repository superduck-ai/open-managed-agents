package admin

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
)

const (
	defaultCursorLimit = 20
	maxCursorLimit     = 1000
)

type pageCursor struct {
	Offset int `json:"offset"`
}

func parseCursorLimit(r *http.Request) (int, error) {
	raw := r.URL.Query().Get("limit")
	if raw == "" {
		return defaultCursorLimit, nil
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit < 1 || limit > maxCursorLimit {
		return 0, errors.New("limit must be between 1 and 1000")
	}
	return limit, nil
}

func parseTokenLimit(r *http.Request, fallback, max int) (int, error) {
	raw := r.URL.Query().Get("limit")
	if raw == "" {
		return fallback, nil
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit < 1 || limit > max {
		return 0, errors.New("limit is out of range")
	}
	return limit, nil
}

func decodePageOffset(raw string) (int, error) {
	if raw == "" {
		return 0, nil
	}
	data, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return 0, errors.New("invalid page cursor")
	}
	var cursor pageCursor
	if err := json.Unmarshal(data, &cursor); err != nil || cursor.Offset < 0 {
		return 0, errors.New("invalid page cursor")
	}
	return cursor.Offset, nil
}

func encodePageOffset(offset int) string {
	data, _ := json.Marshal(pageCursor{Offset: offset})
	return base64.RawURLEncoding.EncodeToString(data)
}

func cursorPage[T any](data []T, hasMore bool, id func(T) string) cursorPageResponse[T] {
	var firstID, lastID *string
	if len(data) > 0 {
		first := id(data[0])
		last := id(data[len(data)-1])
		firstID = &first
		lastID = &last
	}
	return cursorPageResponse[T]{Data: data, FirstID: firstID, HasMore: hasMore, LastID: lastID}
}

func tokenPage[T any](data []T, hasMore bool, offset int) tokenPageResponse[T] {
	var nextPage *string
	if hasMore {
		next := encodePageOffset(offset + len(data))
		nextPage = &next
	}
	return tokenPageResponse[T]{Data: data, NextPage: nextPage}
}
