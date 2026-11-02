package codesessions

import (
	"bytes"
	"encoding/json"
	"net/http"

	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
	"github.com/superduck-ai/open-managed-agents/internal/telemetry"
)

const (
	sandboxMCPResponseLimit = 8 << 20
	sandboxMCPInspectLimit  = maxSandboxMCPRequestBytes
)

var sandboxMCPToolNames = telemetry.NewEnum("bash", "read", "write", "edit", "grep", "glob")

type sandboxMCPReply struct {
	Method string `json:"method"`
	Result *struct {
		IsError bool `json:"isError"`
	} `json:"result"`
	Error *json.RawMessage `json:"error"`
}

func sandboxMCPToolName(call sandboxRPCRequest) string {
	if call.Method != "tools/call" {
		return ""
	}
	return sandboxMCPToolNames.Normalize(call.Params.Name)
}

func sandboxMCPRequestID(call sandboxRPCRequest) string {
	id := string(call.ID)
	if len(id) > 64 {
		return id[:64]
	}
	return id
}

func sandboxMCPOutcome(status int, reply []byte) string {
	if status < http.StatusOK || status >= http.StatusMultipleChoices {
		return "upstream_status"
	}
	if outcome, ok := sandboxMCPReplyOutcome(reply); ok {
		return outcome
	}
	return telemetry.OutcomeOK
}

func sandboxMCPTransportOutcome(r *http.Request) string {
	if httpapi.ClientGone(r) {
		return telemetry.OutcomeCanceled
	}
	return "upstream_error"
}

func sandboxMCPReplyOutcome(payload []byte) (string, bool) {
	var reply sandboxMCPReply
	if json.Unmarshal(payload, &reply) != nil || reply.Method != "" {
		return "", false
	}
	switch {
	case reply.Error != nil:
		return "rpc_error", true
	case reply.Result == nil:
		return "", false
	case reply.Result.IsError:
		return "tool_error", true
	default:
		return telemetry.OutcomeOK, true
	}
}

func sandboxMCPSpanFailed(outcome string) bool {
	return outcome != telemetry.OutcomeOK && outcome != telemetry.OutcomeCanceled
}

type sandboxMCPStreamInspector struct {
	pending      []byte
	data         []byte
	outcome      string
	overflow     bool
	lineOverflow bool
}

func (i *sandboxMCPStreamInspector) observe(chunk []byte) {
	for i.outcome == "" && len(chunk) > 0 {
		index := bytes.IndexByte(chunk, '\n')
		if index < 0 {
			i.buffer(chunk)
			return
		}
		i.buffer(chunk[:index])
		if !i.lineOverflow {
			i.line(bytes.TrimSuffix(i.pending, []byte("\r")))
		}
		i.pending, i.lineOverflow = i.pending[:0], false
		chunk = chunk[index+1:]
	}
	if i.outcome != "" {
		i.pending, i.data = nil, nil
	}
}

func (i *sandboxMCPStreamInspector) buffer(part []byte) {
	if i.lineOverflow {
		return
	}
	if len(i.pending)+len(part) > sandboxMCPInspectLimit {
		i.pending, i.lineOverflow, i.overflow = i.pending[:0], true, true
		return
	}
	i.pending = append(i.pending, part...)
}

func (i *sandboxMCPStreamInspector) line(line []byte) {
	if len(line) == 0 {
		if !i.overflow && len(i.data) > 0 {
			if outcome, ok := sandboxMCPReplyOutcome(i.data); ok {
				i.outcome = outcome
			}
		}
		i.data, i.overflow = i.data[:0], false
		return
	}
	value, ok := bytes.CutPrefix(line, []byte("data:"))
	if !ok || i.overflow {
		return
	}
	value = bytes.TrimPrefix(value, []byte(" "))
	if len(i.data)+len(value) > sandboxMCPInspectLimit {
		i.data, i.overflow = i.data[:0], true
		return
	}
	if len(i.data) > 0 {
		i.data = append(i.data, '\n')
	}
	i.data = append(i.data, value...)
}
