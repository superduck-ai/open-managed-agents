// Package transcriptarchive encodes immutable, independently verified transcript segments.
package transcriptarchive

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"time"

	"github.com/klauspost/compress/zstd"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

const maxSegmentBytes = 64 * 1024 * 1024

// DecodedEvent includes the original persistence identity needed for a faithful restore.
// Payload bytes are inserted directly, never passed through json.Marshal.
type DecodedEvent struct {
	UUID            string          `json:"uuid"`
	ExternalID      string          `json:"external_id"`
	SequenceNum     int64           `json:"sequence_num"`
	AgentID         *string         `json:"agent_id"`
	EventType       string          `json:"event_type"`
	IsCompaction    bool            `json:"is_compaction"`
	PayloadUUID     string          `json:"payload_uuid"`
	PayloadHash     string          `json:"payload_hash"`
	IdempotencyKey  string          `json:"idempotency_key"`
	CreatedAt       time.Time       `json:"created_at"`
	UpdatedAt       time.Time       `json:"updated_at"`
	PayloadLeading  string          `json:"payload_leading,omitempty"`
	PayloadTrailing string          `json:"payload_trailing,omitempty"`
	Payload         json.RawMessage `json:"payload"`
	EventMetadata   json.RawMessage `json:"event_metadata"`
}

type EncodedSegment struct {
	Body       []byte
	RawBytes   int64
	SHA256     string
	EventCount int
}

type SegmentExpectation struct {
	Size         int64
	RawBytes     int64
	SHA256       string
	EventCount   int
	FromSequence int64
	ToSequence   int64
}

func ObjectKey(scope db.TranscriptScope, segmentUUID string) string {
	return "transcript-archive/" + scope.OrganizationUUID + "/" + scope.WorkspaceUUID + "/" + scope.CodeSessionUUID + "/" + segmentUUID + ".jsonl.zst"
}

func entryFromEvent(event db.CodeSessionInternalEvent) DecodedEvent {
	return DecodedEvent{UUID: event.UUID, ExternalID: event.ExternalID, SequenceNum: event.SequenceNum, AgentID: event.AgentID, EventType: event.EventType, IsCompaction: event.IsCompaction, PayloadUUID: event.PayloadUUID, PayloadHash: event.PayloadHash, IdempotencyKey: event.IdempotencyKey, CreatedAt: event.CreatedAt, UpdatedAt: event.UpdatedAt, Payload: event.Payload, EventMetadata: event.EventMetadata}
}

// EncodeRecord preserves payload bytes even when the JSON contains insignificant whitespace.
func EncodeRecord(event db.CodeSessionInternalEvent) ([]byte, error) {
	if !json.Valid(event.Payload) || !json.Valid(event.EventMetadata) || event.SequenceNum <= 0 {
		return nil, errInvalidSegment
	}
	entry := entryFromEvent(event)
	trimmed := bytes.Trim(event.Payload, " \t\r\n")
	offset := bytes.Index(event.Payload, trimmed)
	entry.PayloadLeading = string(event.Payload[:offset])
	entry.PayloadTrailing = string(event.Payload[offset+len(trimmed):])
	entry.Payload = nil
	entry.EventMetadata = nil
	header, err := json.Marshal(entry)
	if err != nil {
		return nil, err
	}
	// Remove the two known final null fields and append raw JSON values verbatim.
	suffix := []byte(`,"payload":null,"event_metadata":null}`)
	header = bytes.TrimSuffix(header, suffix)
	body := append(header, []byte(`,"payload":`)...)
	body = append(body, event.Payload...)
	body = append(body, []byte(`,"event_metadata":`)...)
	body = append(body, event.EventMetadata...)
	return append(body, '}', '\n'), nil
}

func Encode(events []db.CodeSessionInternalEvent) (EncodedSegment, error) {
	if len(events) == 0 {
		return EncodedSegment{}, errInvalidSegment
	}
	var raw bytes.Buffer
	var previous int64
	for _, event := range events {
		if event.SequenceNum <= previous {
			return EncodedSegment{}, errInvalidSegment
		}
		record, err := EncodeRecord(event)
		if err != nil {
			return EncodedSegment{}, err
		}
		if raw.Len()+len(record) > maxSegmentBytes {
			return EncodedSegment{}, errInvalidSegment
		}
		raw.Write(record)
		previous = event.SequenceNum
	}
	encoder, err := zstd.NewWriter(nil, zstd.WithEncoderConcurrency(1))
	if err != nil {
		return EncodedSegment{}, err
	}
	defer encoder.Close()
	body := encoder.EncodeAll(raw.Bytes(), nil)
	digest := sha256.Sum256(body)
	return EncodedSegment{Body: body, RawBytes: int64(raw.Len()), SHA256: hex.EncodeToString(digest[:]), EventCount: len(events)}, nil
}

func Decode(body io.Reader, expect SegmentExpectation) (map[int64]DecodedEvent, error) {
	if expect.Size <= 0 || expect.Size > maxSegmentBytes || expect.RawBytes <= 0 || expect.RawBytes > maxSegmentBytes || expect.EventCount <= 0 {
		return nil, errInvalidSegment
	}
	compressed, err := io.ReadAll(io.LimitReader(body, expect.Size+1))
	if err != nil {
		return nil, err
	}
	digest := sha256.Sum256(compressed)
	if int64(len(compressed)) != expect.Size || hex.EncodeToString(digest[:]) != expect.SHA256 {
		return nil, errIntegrity
	}
	decoder, err := zstd.NewReader(bytes.NewReader(compressed), zstd.WithDecoderConcurrency(1), zstd.WithDecoderMaxMemory(maxSegmentBytes))
	if err != nil {
		return nil, err
	}
	defer decoder.Close()
	raw, err := io.ReadAll(io.LimitReader(decoder, expect.RawBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(raw)) != expect.RawBytes {
		return nil, errIntegrity
	}
	return decodeRecords(raw, expect)
}

func decodeRecords(raw []byte, expect SegmentExpectation) (map[int64]DecodedEvent, error) {
	events := make(map[int64]DecodedEvent)
	decoder := json.NewDecoder(bytes.NewReader(raw))
	var previous int64
	for {
		var event DecodedEvent
		err := decoder.Decode(&event)
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, errInvalidSegment
		}
		if event.SequenceNum <= previous || !json.Valid(event.Payload) || !json.Valid(event.EventMetadata) {
			return nil, errIntegrity
		}
		if len(events) == 0 && event.SequenceNum != expect.FromSequence {
			return nil, errIntegrity
		}
		event.Payload = append(append([]byte(event.PayloadLeading), event.Payload...), []byte(event.PayloadTrailing)...)
		if !json.Valid(event.Payload) {
			return nil, errIntegrity
		}
		events[event.SequenceNum] = event
		previous = event.SequenceNum
	}
	if len(events) != expect.EventCount || previous != expect.ToSequence {
		return nil, errIntegrity
	}
	return events, nil
}
