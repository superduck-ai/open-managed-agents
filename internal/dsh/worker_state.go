package dsh

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"math"
	"net/http"
	"strconv"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

type turnTrace struct {
	SessionID    string   `json:"sessionId"`
	TraceID      string   `json:"traceId"`
	SpanID       string   `json:"spanId"`
	ParentSpanID string   `json:"parentSpanId,omitempty"`
	TraceState   string   `json:"traceState,omitempty"`
	StartMillis  *float64 `json:"startMs"`
}

type workerStateResponse struct {
	ModelControlSequence *string     `json:"model_control_sequence"`
	Model                *string     `json:"model"`
	TraceTurns           []turnTrace `json:"trace_turns"`
}

func stateResponse(value db.DSHWorkerState) (workerStateResponse, error) {
	result := workerStateResponse{ModelControlSequence: value.ModelControlSequence, Model: value.Model}
	if len(value.TraceTurns) > 0 {
		if err := json.Unmarshal(value.TraceTurns, &result.TraceTurns); err != nil {
			return result, err
		}
	}
	return result, nil
}

func (h *ControlHandler) state(w http.ResponseWriter, r *http.Request) {
	result, err := h.database.GetDSHWorkerState(r.Context(), scopeFrom(r))
	if err != nil {
		writeError(w, r, err, nil)
		return
	}
	response, err := stateResponse(result)
	if err != nil {
		writeError(w, r, err, nil)
		return
	}
	writeJSON(w, response)
}

func (h *ControlHandler) patchState(w http.ResponseWriter, r *http.Request) {
	envelope, err := decodeBody[map[string]json.RawMessage](w, r)
	if err != nil {
		writeError(w, r, err, nil)
		return
	}
	patch, err := parseStatePatch(envelope)
	if err != nil {
		writeError(w, r, err, nil)
		return
	}
	result, err := h.database.PatchDSHWorkerState(r.Context(), scopeFrom(r), patch)
	if err != nil {
		writeError(w, r, err, nil)
		return
	}
	response, err := stateResponse(result)
	if err != nil {
		writeError(w, r, err, nil)
		return
	}
	writeJSON(w, response)
}

func parseStatePatch(envelope map[string]json.RawMessage) (db.DSHWorkerPatch, error) {
	var patch db.DSHWorkerPatch
	for key, raw := range envelope {
		switch key {
		case "control_input_id":
			if json.Unmarshal(raw, &patch.ControlInputID) != nil || patch.ControlInputID == "" {
				return patch, invalidRequest()
			}
		case "control_sequence":
			if json.Unmarshal(raw, &patch.ControlSequence) != nil {
				return patch, invalidRequest()
			}
			sequence, err := strconv.ParseUint(patch.ControlSequence, 10, 64)
			if err != nil || sequence == 0 || strconv.FormatUint(sequence, 10) != patch.ControlSequence {
				return patch, invalidRequest()
			}
		case "model":
			patch.ModelSet = true
			if json.Unmarshal(raw, &patch.Model) != nil || patch.Model != nil && *patch.Model == "" {
				return patch, invalidRequest()
			}
		case "trace_turns":
			patch.TraceTurnsSet = true
			if !bytes.Equal(raw, []byte("null")) {
				var turns []turnTrace
				if json.Unmarshal(raw, &turns) != nil || turns == nil {
					return patch, invalidRequest()
				}
				for _, turn := range turns {
					if !validTurnTrace(turn) {
						return patch, invalidRequest()
					}
				}
				patch.TraceTurns = raw
			}
		default:
			return patch, invalidRequest()
		}
	}
	if (patch.ModelSet || patch.ControlInputID != "" || patch.ControlSequence != "") && (!patch.ModelSet || patch.ControlInputID == "" || patch.ControlSequence == "") {
		return patch, invalidRequest()
	}
	return patch, nil
}

func validHex(value string, size int) bool {
	if len(value) != size {
		return false
	}
	_, err := hex.DecodeString(value)
	if err != nil {
		return false
	}
	for _, c := range value {
		if c >= 'A' && c <= 'F' {
			return false
		}
	}
	return true
}

func validTurnTrace(turn turnTrace) bool {
	return turn.SessionID != "" && validHex(turn.TraceID, 32) && validHex(turn.SpanID, 16) &&
		(turn.ParentSpanID == "" || validHex(turn.ParentSpanID, 16)) && turn.StartMillis != nil && !math.IsNaN(*turn.StartMillis) && !math.IsInf(*turn.StartMillis, 0)
}
