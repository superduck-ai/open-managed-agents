package porttunnel

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
)

type Header struct {
	Name  string `json:"name,omitempty"`
	Value string `json:"value,omitempty"`
}

type Bytes []byte

func (b Bytes) MarshalJSON() ([]byte, error) {
	return json.Marshal(base64.StdEncoding.EncodeToString(b))
}

func (b *Bytes) UnmarshalJSON(data []byte) error {
	var text string
	if err := json.Unmarshal(data, &text); err != nil {
		return err
	}
	for _, encoding := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
		if decoded, err := encoding.DecodeString(text); err == nil {
			*b = decoded
			return nil
		}
	}
	return errors.New("invalid base64 bytes field")
}

type tunnelRequest struct {
	SessionID    string           `json:"sessionId,omitempty"`
	HTTPRequest  *httpRequestMsg  `json:"httpRequest,omitempty"`
	HTTPCancel   *httpCancelMsg   `json:"httpCancel,omitempty"`
	WSOpen       *wsOpenMsg       `json:"wsOpen,omitempty"`
	WSMessage    *wsMessageMsg    `json:"wsMessage,omitempty"`
	WSClose      *wsCloseMsg      `json:"wsClose,omitempty"`
	WindowUpdate *windowUpdateMsg `json:"windowUpdate,omitempty"`
}

type tunnelResponse struct {
	HTTPHeaders  *httpHeadersMsg  `json:"httpHeaders,omitempty"`
	HTTPChunk    *httpChunkMsg    `json:"httpChunk,omitempty"`
	HTTPError    *httpErrorMsg    `json:"httpError,omitempty"`
	WSOpened     *wsOpenedMsg     `json:"wsOpened,omitempty"`
	WSMessage    *wsMessageMsg    `json:"wsMessage,omitempty"`
	WSClose      *wsCloseMsg      `json:"wsClose,omitempty"`
	WSError      *wsErrorMsg      `json:"wsError,omitempty"`
	WindowUpdate *windowUpdateMsg `json:"windowUpdate,omitempty"`
}

type windowUpdateMsg struct {
	StreamID string `json:"streamId,omitempty"`
	Bytes    uint32 `json:"bytes,omitempty"`
}

type httpRequestMsg struct {
	RequestID string   `json:"requestId,omitempty"`
	Port      int32    `json:"port,omitempty"`
	Method    string   `json:"method,omitempty"`
	Path      string   `json:"path,omitempty"`
	Headers   []Header `json:"headers,omitempty"`
	Body      Bytes    `json:"body,omitempty"`
}

type httpCancelMsg struct {
	RequestID string `json:"requestId,omitempty"`
}

type httpHeadersMsg struct {
	RequestID      string   `json:"requestId,omitempty"`
	StatusCode     int32    `json:"statusCode,omitempty"`
	StatusText     string   `json:"statusText,omitempty"`
	Headers        []Header `json:"headers,omitempty"`
	SequenceNumber uint32   `json:"sequenceNumber,omitempty"`
}

type httpChunkMsg struct {
	RequestID      string `json:"requestId,omitempty"`
	Data           Bytes  `json:"data,omitempty"`
	IsFinal        bool   `json:"isFinal,omitempty"`
	SequenceNumber uint32 `json:"sequenceNumber,omitempty"`
}

type httpErrorMsg struct {
	RequestID    string `json:"requestId,omitempty"`
	ErrorMessage string `json:"errorMessage,omitempty"`
}

type wsOpenMsg struct {
	TunnelID  string   `json:"tunnelId,omitempty"`
	Port      int32    `json:"port,omitempty"`
	Path      string   `json:"path,omitempty"`
	Headers   []Header `json:"headers,omitempty"`
	Protocols []string `json:"protocols,omitempty"`
}

type wsOpenedMsg struct {
	TunnelID string `json:"tunnelId,omitempty"`
	Protocol string `json:"protocol,omitempty"`
}

type wsMessageMsg struct {
	TunnelID string `json:"tunnelId,omitempty"`
	Data     Bytes  `json:"data,omitempty"`
	IsBinary bool   `json:"isBinary,omitempty"`
}

type wsCloseMsg struct {
	TunnelID string `json:"tunnelId,omitempty"`
	Code     int32  `json:"code,omitempty"`
	Reason   string `json:"reason,omitempty"`
}

type wsErrorMsg struct {
	TunnelID     string `json:"tunnelId,omitempty"`
	ErrorMessage string `json:"errorMessage,omitempty"`
}

func encodeRequest(request tunnelRequest) ([]byte, error) {
	return json.Marshal(request)
}

func decodeResponse(data []byte) (tunnelResponse, error) {
	if bytes.IndexByte(data, '_') >= 0 {
		normalized, err := normalizeFieldNames(json.RawMessage(data))
		if err != nil {
			return tunnelResponse{}, err
		}
		data = normalized
	}
	var response tunnelResponse
	if err := json.Unmarshal(data, &response); err != nil {
		return tunnelResponse{}, err
	}
	return response, nil
}

func normalizeFieldNames(raw json.RawMessage) (json.RawMessage, error) {
	trimmed := bytes.TrimLeft(raw, " \t\r\n")
	if len(trimmed) == 0 {
		return raw, nil
	}
	switch trimmed[0] {
	case '{':
		var object map[string]json.RawMessage
		if err := json.Unmarshal(raw, &object); err != nil {
			return nil, err
		}
		normalized := make(map[string]json.RawMessage, len(object))
		for key, value := range object {
			child, err := normalizeFieldNames(value)
			if err != nil {
				return nil, err
			}
			normalized[lowerCamel(key)] = child
		}
		return json.Marshal(normalized)
	case '[':
		var items []json.RawMessage
		if err := json.Unmarshal(raw, &items); err != nil {
			return nil, err
		}
		for index, item := range items {
			child, err := normalizeFieldNames(item)
			if err != nil {
				return nil, err
			}
			items[index] = child
		}
		return json.Marshal(items)
	default:
		return raw, nil
	}
}

func lowerCamel(name string) string {
	if !strings.Contains(name, "_") {
		return name
	}
	parts := strings.Split(name, "_")
	var builder strings.Builder
	builder.WriteString(parts[0])
	for _, part := range parts[1:] {
		if part == "" {
			continue
		}
		builder.WriteString(strings.ToUpper(part[:1]) + part[1:])
	}
	return builder.String()
}
