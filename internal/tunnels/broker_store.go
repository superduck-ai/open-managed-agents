package tunnels

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/nats-io/nats.go/jetstream"
)

const (
	commandStreamName      = "OMA_TUNNEL_COMMANDS_V1"
	commandSubjectPrefix   = "oma.tunnel.command.v1."
	maxBrokerValueBytes    = 2 << 20
	maxCommandConsumers    = 131072 // Existing global Stream consumer limit.
	maxRequestBindingBytes = 4096
)

func encodeTunnelJSON(value any, limit int) ([]byte, error) {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return nil, err
	}
	if limit > 0 && buffer.Len() > limit {
		return nil, ErrPayloadLimit
	}
	return buffer.Bytes(), nil
}

func brokerKey(parts ...string) string {
	hash := sha256.New()
	for _, part := range parts {
		_, _ = fmt.Fprintf(hash, "%d:", len(part))
		_, _ = hash.Write([]byte(part))
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func brokerCapacityError(err error) error {
	apiError, ok := errors.AsType[*jetstream.APIError](err)
	if ok && apiError.ErrorCode == 10077 && (apiError.Description == "maximum bytes exceeded" || apiError.Description == "maximum messages exceeded") {
		return fmt.Errorf("%w: %v", errCommandStorageFull, err)
	}
	return err
}
