package messages

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json/v2"
	"io"
	"maps"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/codesessions"
	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
	maevents "github.com/superduck-ai/open-managed-agents/internal/managedagentsevents"
)

// responseObservation forwards bytes unchanged and buffers public content until
// message_stop, so the completed messages and their end can be published together.
type responseObservation struct {
	onComplete   func()
	request      *codesessions.ModelRequest
	result       codesessions.ModelRequestResult
	streaming    bool
	buffer       []byte
	line         []byte
	malformed    bool
	complete     bool
	messageID    string
	blocks       map[int]*responseContentBlock
	contentBytes int
}

type responseMessage struct {
	ID      string                         `json:"id"`
	Type    string                         `json:"type"`
	Usage   codesessions.ModelRequestUsage `json:"usage"`
	Content []responseContentBlock         `json:"content"`
}

type responseContentBlock struct {
	ID         string `json:"id"`
	Type       string `json:"type"`
	Text       string `json:"text"`
	textBuffer strings.Builder
}

type responseStreamEvent struct {
	Type         string                         `json:"type"`
	Message      responseMessage                `json:"message"`
	Usage        codesessions.ModelRequestUsage `json:"usage"`
	Index        int                            `json:"index"`
	ContentBlock responseContentBlock           `json:"content_block"`
	Delta        responseContentBlock           `json:"delta"`
}

func (h *Handler) beginModelRequest(r *http.Request, principal auth.Principal, model string) (*codesessions.ModelRequest, error) {
	if principal.CodeSessionExternalID == "" {
		return nil, nil
	}
	return h.codeSessions.BeginModelRequest(r.Context(), principal.WorkspaceUUID, principal.PublicSessionExternalID,
		principal.CodeSessionExternalID, strings.TrimSpace(r.Header.Get("X-Claude-Code-Agent-Id")), model)
}

func (h *Handler) proxyModelRequest(w http.ResponseWriter, r, upstream *http.Request, request *codesessions.ModelRequest) {
	observation := &responseObservation{request: request}
	if request != nil {
		// Claude Code copies this header to assistant.request_id, including HTTP errors.
		w.Header().Set("Request-Id", request.StartID)
		// A forwarded Accept-Encoding disables Transport decompression and hides the frames from the observer.
		upstream.Header.Del("Accept-Encoding")
		observation.onComplete = sync.OnceFunc(func() { h.finishModelRequest(r.Context(), observation) })
		defer observation.onComplete()
	}
	response, err := h.client.Do(upstream)
	if err != nil {
		observation.result.ErrorType = "transport_error"
		h.logger.ErrorContext(r.Context(), "proxy messages upstream request", "error", err)
		httpapi.WriteError(w, r, upstreamUnavailableError())
		return
	}
	defer response.Body.Close()
	if request != nil {
		observation.result.UpstreamRequestID = response.Header.Get("Request-Id")
		w.Header().Del("Request-Id")
		response.Header.Set("Request-Id", request.StartID)
		observation.streaming = strings.Contains(strings.ToLower(response.Header.Get("Content-Type")), "text/event-stream")
		if response.StatusCode >= 400 {
			observation.result.ErrorType = "http_error"
		}
		response.Body = &observedResponseBody{Reader: io.TeeReader(response.Body, observation), Closer: response.Body}
	}
	if err := writeProxyResponse(w, response); err != nil {
		observation.result.ErrorType = "stream_error"
		if r.Context().Err() == nil {
			h.logger.ErrorContext(r.Context(), "stream Messages upstream response", "error", err)
		}
	}
}

type observedResponseBody struct {
	io.Reader
	io.Closer
}

func (h *Handler) finishModelRequest(ctx context.Context, observation *responseObservation) {
	observation.finish()
	if ctx.Err() != nil && !observation.complete {
		observation.result.ErrorType = "cancelled"
	}
	// End must survive client cancellation, but cannot hold a handler indefinitely.
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if err := h.codeSessions.EndModelRequest(cleanup, observation.request, observation.result); err != nil {
		h.logger.ErrorContext(cleanup, "persist model request end", "model_request_start_id", observation.request.StartID, "error", err)
	}
}

func (o *responseObservation) Write(data []byte) (int, error) {
	// ponytail: observe at most 4 MiB per frame (or JSON response); larger
	// payloads keep forwarding and close with observation_limit. Use a token
	// decoder if providers need larger non-streaming responses.
	const maxFrameBytes = 4 * 1024 * 1024
	if o.malformed || o.complete {
		return len(data), nil
	}
	if !o.streaming {
		if len(o.buffer)+len(data) > maxFrameBytes {
			o.malformed = true
			o.result.ErrorType = "observation_limit"
			o.buffer = nil
		} else {
			o.buffer = append(o.buffer, data...)
		}
		return len(data), nil
	}
	for _, b := range data {
		if b != '\n' {
			o.line = append(o.line, b)
			if len(o.buffer)+len(o.line) > maxFrameBytes {
				o.malformed = true
				o.result.ErrorType = "observation_limit"
				o.buffer = nil
				o.line = nil
				break
			}
			continue
		}
		line := bytes.TrimSuffix(o.line, []byte{'\r'})
		if len(line) == 0 {
			if len(o.buffer) > 0 {
				o.observeFrame(o.buffer)
				o.buffer = o.buffer[:0]
			}
		} else if value, ok := bytes.CutPrefix(line, []byte("data:")); ok {
			value = bytes.TrimPrefix(value, []byte{' '})
			o.buffer = append(o.buffer, value...)
			o.buffer = append(o.buffer, '\n')
		}
		o.line = o.line[:0]
	}
	return len(data), nil
}

func (o *responseObservation) observeFrame(data []byte) {
	var event responseStreamEvent
	if err := json.Unmarshal(data, &event); err != nil {
		o.malformed = true
		return
	}
	switch event.Type {
	case "message_start":
		o.messageID = event.Message.ID
		o.result.Usage = event.Message.Usage
		o.blocks = make(map[int]*responseContentBlock)
	case "content_block_start":
		o.addEventID(event.Index, event.ContentBlock.Type)
		if o.blocks == nil || event.Index < 0 {
			o.malformed = true
			return
		}
		block := event.ContentBlock
		block.textBuffer.WriteString(block.Text)
		o.contentBytes += len(block.Text)
		if o.contentBytes > 4*1024*1024 {
			o.malformed = true
			o.result.ErrorType = "observation_limit"
			return
		}
		o.blocks[event.Index] = &block
		if event.ContentBlock.Type == "tool_use" && event.ContentBlock.ID != "" {
			o.result.ToolUseIDs = append(o.result.ToolUseIDs, event.ContentBlock.ID)
		}
	case "content_block_delta":
		if event.Delta.Type == "text_delta" {
			o.contentBytes += len(event.Delta.Text)
			if o.contentBytes > 4*1024*1024 {
				o.malformed = true
				o.result.ErrorType = "observation_limit"
				return
			}
			block := o.blocks[event.Index]
			if block == nil {
				o.malformed = true
				return
			}
			block.textBuffer.WriteString(event.Delta.Text)
		}
	case "message_delta":
		mergeRequestUsage(&o.result.Usage, event.Usage)
	case "message_stop":
		if o.messageID == "" {
			o.malformed = true
		}
		for _, index := range slices.Sorted(maps.Keys(o.blocks)) {
			block := o.blocks[index]
			block.Text = block.textBuffer.String()
			o.addMessage(index, *block)
		}
		o.complete = true
		o.result.EndedAt = time.Now().UTC().Truncate(time.Microsecond)
		if o.onComplete != nil {
			o.onComplete()
		}
	case "error":
		o.result.ErrorType = "provider_error"
		if o.onComplete != nil {
			o.onComplete()
		}
	}
}

func (o *responseObservation) addEventID(index int, blockType string) {
	if o.messageID == "" {
		o.malformed = true
		return
	}
	if blockType == "tool_use" || blockType == "server_tool_use" {
		return
	}
	eventType := "agent.message"
	if blockType == "thinking" || blockType == "redacted_thinking" {
		eventType = "agent.thinking"
	}
	o.result.EventIDs = append(o.result.EventIDs, maevents.StableAssistantEventID(o.request.CodeSessionID, o.messageID, index, eventType))
}

func (o *responseObservation) finish() {
	if !o.streaming && !o.complete && !o.malformed && o.result.ErrorType == "" {
		var message responseMessage
		if err := json.Unmarshal(o.buffer, &message); err != nil || message.ID == "" || message.Type != "message" {
			o.malformed = true
		} else {
			o.messageID = message.ID
			o.result.Usage = message.Usage
			for index, block := range message.Content {
				o.addEventID(index, block.Type)
				o.addMessage(index, block)
				if block.Type == "tool_use" && block.ID != "" {
					o.result.ToolUseIDs = append(o.result.ToolUseIDs, block.ID)
				}
			}
			o.complete = true
		}
	}
	if o.result.EndedAt.IsZero() {
		o.result.EndedAt = time.Now().UTC().Truncate(time.Microsecond)
	}
	if o.result.ErrorType == "" {
		switch {
		case o.malformed:
			o.result.ErrorType = "invalid_response"
		case !o.complete:
			o.result.ErrorType = "incomplete_response"
		}
	}
}

func (o *responseObservation) addMessage(index int, block responseContentBlock) {
	message := codesessions.ModelRequestMessage{Type: "agent.message"}
	switch block.Type {
	case "thinking", "redacted_thinking":
		message.Type = "agent.thinking"
	case "text":
		message.Content = []codesessions.ModelRequestContent{{Type: "text", Text: &block.Text}}
	case "redacted":
		message.Content = []codesessions.ModelRequestContent{{Type: "redacted"}}
	default:
		return
	}
	message.ID = maevents.StableAssistantEventID(o.request.CodeSessionID, o.messageID, index, message.Type)
	o.result.Messages = append(o.result.Messages, message)
}

func mergeRequestUsage(target *codesessions.ModelRequestUsage, delta codesessions.ModelRequestUsage) {
	if delta.CacheCreation != nil {
		if target.CacheCreation == nil {
			target.CacheCreation = new(codesessions.ModelRequestCacheUsage)
		}
		target.CacheCreation.Ephemeral5mInputTokens = cmp.Or(delta.CacheCreation.Ephemeral5mInputTokens, target.CacheCreation.Ephemeral5mInputTokens)
		target.CacheCreation.Ephemeral1hInputTokens = cmp.Or(delta.CacheCreation.Ephemeral1hInputTokens, target.CacheCreation.Ephemeral1hInputTokens)
	}
	target.InputTokens = cmp.Or(delta.InputTokens, target.InputTokens)
	target.OutputTokens = cmp.Or(delta.OutputTokens, target.OutputTokens)
	target.CacheCreationInputTokens = cmp.Or(delta.CacheCreationInputTokens, target.CacheCreationInputTokens)
	target.CacheReadInputTokens = cmp.Or(delta.CacheReadInputTokens, target.CacheReadInputTokens)
}
