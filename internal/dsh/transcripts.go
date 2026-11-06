package dsh

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

type nativeHeader struct {
	Version       int     `json:"version"`
	ID            string  `json:"id"`
	ParentSession *string `json:"parentSession"`
	CreatedAt     *int64  `json:"createdAt"`
	IsSeeded      *bool   `json:"isSeeded"`
}

type headerRequest struct {
	ParentDSHSessionID *string         `json:"parent_dsh_session_id"`
	Header             json.RawMessage `json:"header"`
}

type watermark struct {
	DSHSessionID string `json:"dsh_session_id,omitempty"`
	NextSeq      string `json:"next_seq"`
}

func (h *Handler) putHeader(w http.ResponseWriter, r *http.Request) {
	request, err := decodeBody[headerRequest](w, r)
	if err != nil {
		h.fail(w, r, err, nil)
		return
	}
	id := chi.URLParam(r, "dsh_session_id")
	if err := validateHeader(id, chi.URLParam(r, "code_session_id"), request); err != nil {
		h.fail(w, r, err, nil)
		return
	}
	result, err := h.database.PutDSHTranscript(r.Context(), scopeFrom(r), id, request.ParentDSHSessionID, request.Header)
	if err != nil {
		h.fail(w, r, err, nil)
		return
	}
	writeJSON(w, watermark{DSHSessionID: id, NextSeq: strconv.FormatInt(result.NextSeq, 10)})
}

func validateHeader(id, codeSessionID string, request headerRequest) error {
	var header nativeHeader
	if err := json.Unmarshal(request.Header, &header); err != nil {
		return invalidRequest()
	}
	if header.Version != 4 {
		return unsupportedTranscript()
	}
	if id == "" || header.ID != id || header.CreatedAt == nil || *header.CreatedAt < 0 || *header.CreatedAt > 9007199254740991 || header.IsSeeded == nil {
		return invalidRequest()
	}
	if !sameParent(header.ParentSession, request.ParentDSHSessionID) {
		return invalidRequest()
	}
	if id == codeSessionID && request.ParentDSHSessionID != nil {
		return invalidRequest()
	}
	if id != codeSessionID && (request.ParentDSHSessionID == nil || *request.ParentDSHSessionID == "" || *request.ParentDSHSessionID == id) {
		return invalidRequest()
	}
	return nil
}

func sameParent(left, right *string) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

type appendRequest struct {
	Events []json.RawMessage `json:"events"`
}

type nativeEvent struct {
	Seq  *int64          `json:"seq"`
	Type string          `json:"type"`
	Data json.RawMessage `json:"data"`
}

func (h *Handler) appendEvents(w http.ResponseWriter, r *http.Request) {
	request, err := decodeBody[appendRequest](w, r)
	if err != nil {
		h.fail(w, r, err, nil)
		return
	}
	events, err := parseEvents(request.Events)
	if err != nil {
		h.fail(w, r, err, nil)
		return
	}
	result, err := h.database.AppendDSHTranscript(r.Context(), scopeFrom(r), chi.URLParam(r, "dsh_session_id"), events)
	if err != nil {
		h.fail(w, r, err, &result.NextSeq)
		return
	}
	writeJSON(w, watermark{NextSeq: strconv.FormatInt(result.NextSeq, 10)})
}

func parseEvents(raw []json.RawMessage) ([]db.DSHTranscriptEvent, error) {
	if len(raw) == 0 || len(raw) > maxPageEvents {
		return nil, invalidRequest()
	}
	result := make([]db.DSHTranscriptEvent, 0, len(raw))
	var previous int64
	for index, payload := range raw {
		if len(payload) > maxEventBytes {
			return nil, payloadTooLarge()
		}
		var event nativeEvent
		if err := json.Unmarshal(payload, &event); err != nil {
			return nil, invalidRequest()
		}
		if event.Seq == nil || *event.Seq < 0 || *event.Seq >= 9007199254740991 || event.Type == "" || len(event.Data) == 0 || index > 0 && *event.Seq != previous+1 {
			return nil, invalidRequest()
		}
		previous = *event.Seq
		result = append(result, db.DSHTranscriptEvent{Seq: *event.Seq, Payload: payload})
	}
	return result, nil
}

type transcriptInfo struct {
	DSHSessionID       string  `json:"dsh_session_id"`
	ParentDSHSessionID *string `json:"parent_dsh_session_id"`
	SessionThreadID    *string `json:"session_thread_id"`
	HeaderPresent      bool    `json:"header_present"`
	NextSeq            string  `json:"next_seq"`
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
		response.Data = append(response.Data, transcriptInfo{DSHSessionID: row.DSHSessionID, ParentDSHSessionID: row.ParentDSHSessionID, SessionThreadID: row.SessionThreadID, HeaderPresent: true, NextSeq: strconv.FormatInt(row.NextSeq, 10)})
	}
	writeJSON(w, response)
}

type transcriptPage struct {
	Header      json.RawMessage   `json:"header"`
	Events      []json.RawMessage `json:"events"`
	NextFromSeq *string           `json:"next_from_seq"`
}

func (h *Handler) read(w http.ResponseWriter, r *http.Request) {
	from := int64(0)
	var err error
	if value := r.URL.Query().Get("from_seq"); value != "" {
		from, err = decimal(value)
	}
	if err != nil || from > 9007199254740991 {
		h.fail(w, r, invalidRequest(), nil)
		return
	}
	transcript, events, err := h.database.ReadDSHTranscript(r.Context(), scopeFrom(r), chi.URLParam(r, "dsh_session_id"), from, maxPageEvents)
	if err != nil {
		h.fail(w, r, err, nil)
		return
	}
	response := transcriptPage{Events: make([]json.RawMessage, 0, len(events))}
	if from == 0 {
		response.Header = transcript.Header
	}
	for _, event := range events {
		response.Events = append(response.Events, json.RawMessage(event.Payload))
	}
	next := from + int64(len(response.Events))
	if next < transcript.NextSeq {
		cursor := strconv.FormatInt(next, 10)
		response.NextFromSeq = &cursor
	}
	writeJSON(w, response)
}
