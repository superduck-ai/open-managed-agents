package dsh

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"unicode/utf8"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func readRecords(w http.ResponseWriter, r *http.Request, from int64) ([]db.DSHTranscriptRecord, error) {
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxRequestBytes))
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			return nil, payloadTooLarge()
		}
		return nil, invalidRequest()
	}
	if len(raw) == 0 || raw[len(raw)-1] != '\n' {
		return nil, invalidRequest()
	}
	lines := bytes.Split(raw[:len(raw)-1], []byte{'\n'})
	if len(lines) > maxPageEvents {
		return nil, invalidRequest()
	}
	records := make([]db.DSHTranscriptRecord, 0, len(lines))
	for index, line := range lines {
		if len(line) > maxEventBytes {
			return nil, payloadTooLarge()
		}
		offset, err := recordOffset(line)
		if err != nil || offset != from+int64(index) {
			return nil, invalidRequest()
		}
		records = append(records, db.DSHTranscriptRecord{Offset: offset, Payload: line})
	}
	return records, nil
}

func recordOffset(raw []byte) (int64, error) {
	if !utf8.Valid(raw) || !json.Valid(raw) {
		return 0, invalidRequest()
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return 0, invalidRequest()
	}
	var sequence json.RawMessage
	for decoder.More() {
		key, err := decoder.Token()
		if err != nil {
			return 0, invalidRequest()
		}
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return 0, invalidRequest()
		}
		if key == "seq" {
			if sequence != nil {
				return 0, invalidRequest()
			}
			sequence = value
		}
	}
	if sequence == nil {
		return 0, nil
	}
	var seq int64
	if bytes.Equal(sequence, []byte("null")) || json.Unmarshal(sequence, &seq) != nil || seq < 0 || seq > 9007199254740991 {
		return 0, invalidRequest()
	}
	return seq + 1, nil
}
