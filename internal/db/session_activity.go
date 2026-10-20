package db

import (
	"bytes"
	"encoding/json"
	"fmt"
)

type sessionActivityProjection struct {
	Usage json.RawMessage `json:"usage"`
	Stats json.RawMessage `json:"stats"`
}

func (p *sessionActivityProjection) Scan(source any) error {
	var raw []byte
	switch value := source.(type) {
	case nil:
		*p = sessionActivityProjection{}
		return nil
	case []byte:
		raw = value
	case string:
		raw = []byte(value)
	default:
		return fmt.Errorf("scan session activity: unsupported type %T", source)
	}
	var decoded sessionActivityProjection
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return fmt.Errorf("scan session activity: %w", err)
	}
	if len(decoded.Usage) == 0 || len(decoded.Stats) == 0 {
		return fmt.Errorf("scan session activity: incomplete projection")
	}
	*p = decoded
	return nil
}
func (p sessionActivityProjection) usage(stored []byte) json.RawMessage {
	if p.Usage != nil {
		return bytes.Clone(p.Usage)
	}
	return bytes.Clone(stored)
}
func (p sessionActivityProjection) stats(stored []byte) json.RawMessage {
	if p.Stats != nil {
		return bytes.Clone(p.Stats)
	}
	return bytes.Clone(stored)
}
