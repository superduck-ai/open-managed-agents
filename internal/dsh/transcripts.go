package dsh

import (
	"mime"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
)

type watermark struct {
	NextOffset string `json:"next_offset"`
}

func (h *Handler) appendRecords(w http.ResponseWriter, r *http.Request) {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/x-ndjson" {
		h.fail(w, r, invalidRequest(), nil)
		return
	}
	from, err := decimal(r.URL.Query().Get("from_offset"))
	if err != nil || from > 9007199254740992 {
		h.fail(w, r, invalidRequest(), nil)
		return
	}
	records, err := readRecords(w, r, from)
	if err != nil {
		h.fail(w, r, err, nil)
		return
	}
	id := chi.URLParam(r, "dsh_session_id")
	var parent *string
	if values, exists := r.URL.Query()["parent_dsh_session_id"]; exists {
		if from != 0 || len(values) != 1 || values[0] == "" || values[0] == id {
			h.fail(w, r, invalidRequest(), nil)
			return
		}
		parent = &values[0]
	}
	if from == 0 && ((id == chi.URLParam(r, "code_session_id")) != (parent == nil)) {
		h.fail(w, r, invalidRequest(), nil)
		return
	}
	result, err := h.database.AppendDSHTranscript(r.Context(), scopeFrom(r), id, parent, records)
	if err != nil {
		h.fail(w, r, err, &result.NextOffset)
		return
	}
	writeJSON(w, watermark{NextOffset: strconv.FormatInt(result.NextOffset, 10)})
}

type transcriptInfo struct {
	DSHSessionID       string  `json:"dsh_session_id"`
	ParentDSHSessionID *string `json:"parent_dsh_session_id"`
	SessionThreadID    *string `json:"session_thread_id"`
	NextOffset         string  `json:"next_offset"`
}

type transcriptList struct {
	Data      []transcriptInfo `json:"data"`
	NextAfter *string          `json:"next_after"`
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	limit := int64(100)
	var err error
	if value := r.URL.Query().Get("limit"); value != "" {
		limit, err = decimal(value)
	}
	if err != nil || limit < 1 || limit > 200 {
		h.fail(w, r, invalidRequest(), nil)
		return
	}
	rows, err := h.database.ListDSHTranscripts(r.Context(), scopeFrom(r), r.URL.Query().Get("after"), int(limit)+1)
	if err != nil {
		h.fail(w, r, err, nil)
		return
	}
	response := transcriptList{Data: make([]transcriptInfo, 0, min(len(rows), int(limit)))}
	if len(rows) > int(limit) {
		rows = rows[:limit]
		id := rows[len(rows)-1].DSHSessionID
		response.NextAfter = &id
	}
	for _, row := range rows {
		response.Data = append(response.Data, transcriptInfo{DSHSessionID: row.DSHSessionID, ParentDSHSessionID: row.ParentDSHSessionID, SessionThreadID: row.SessionThreadID, NextOffset: strconv.FormatInt(row.NextOffset, 10)})
	}
	writeJSON(w, response)
}

func (h *Handler) read(w http.ResponseWriter, r *http.Request) {
	from := int64(0)
	var err error
	if value := r.URL.Query().Get("from_offset"); value != "" {
		from, err = decimal(value)
	}
	if err != nil || from > 9007199254740993 {
		h.fail(w, r, invalidRequest(), nil)
		return
	}
	transcript, records, err := h.database.ReadDSHTranscript(r.Context(), scopeFrom(r), chi.URLParam(r, "dsh_session_id"), from, maxPageEvents)
	if err != nil {
		h.fail(w, r, err, nil)
		return
	}
	next := from + int64(len(records))
	w.Header().Set("Content-Type", "application/x-ndjson")
	w.Header().Set("DSR-Next-Offset", strconv.FormatInt(transcript.NextOffset, 10))
	if next < transcript.NextOffset {
		w.Header().Set("DSR-Next-From-Offset", strconv.FormatInt(next, 10))
	}
	w.WriteHeader(http.StatusOK)
	for _, record := range records {
		if _, err := w.Write(record.Payload); err != nil {
			return
		}
		if _, err := w.Write([]byte{'\n'}); err != nil {
			return
		}
	}
}
