package tunnels

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/nats-io/nats.go/jetstream"
)

const (
	commandStreamName      = "OMA_TUNNEL_COMMANDS_V1"
	commandSubjectPrefix   = "oma.tunnel.command.v1."
	requestBucketName      = "OMA_TUNNEL_REQUESTS_V1"
	maxBrokerValueBytes    = 2 << 20
	maxCommandConsumers    = 131072 // Existing global Stream consumer limit.
	maxRequestBindingBytes = 4096
)

// brokerStore reads immutable bindings from the stream leader.
type brokerStore struct {
	bucket jetstream.KeyValue
	name   string
	stream jetstream.Stream
}

func openBrokerStore(ctx context.Context, js jetstream.JetStream, name string, maxRecords int64, maxValueBytes int32, ttl time.Duration, replicas int) (*brokerStore, error) {
	stream, err := js.CreateStream(ctx, jetstream.StreamConfig{
		Name: "KV_" + name, Subjects: []string{"$KV." + name + ".>"},
		Storage: jetstream.FileStorage, Replicas: replicas,
		MaxAge: ttl, MaxMsgSize: maxValueBytes, MaxMsgs: maxRecords,
		MaxMsgsPerSubject: 1, Discard: jetstream.DiscardNew,
		AllowRollup: true, DenyDelete: true, AllowDirect: false,
		// Include binding and stream header overhead.
		MaxBytes: maxRecords * (int64(maxValueBytes) + 4096),
	})
	if err != nil {
		return nil, fmt.Errorf("open tunnel KV %s: %w", name, err)
	}
	kv, err := js.KeyValue(ctx, name)
	if err != nil {
		return nil, err
	}
	return &brokerStore{bucket: kv, name: name, stream: stream}, nil
}

func (s *brokerStore) read(ctx context.Context, key string, target any) error {
	msg, err := s.stream.GetLastMsgForSubject(ctx, "$KV."+s.name+"."+key)
	if errors.Is(err, jetstream.ErrMsgNotFound) {
		return ErrRequestNotFound
	}
	if err != nil {
		return err
	}
	if msg.Header.Get("KV-Operation") != "" {
		return ErrRequestNotFound
	}
	if err := json.Unmarshal(msg.Data, target); err != nil {
		return fmt.Errorf("decode tunnel KV: %w", err)
	}
	return nil
}

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
	var apiError *jetstream.APIError
	if errors.As(err, &apiError) && apiError.ErrorCode == 10077 && (apiError.Description == "maximum messages exceeded" || apiError.Description == "maximum bytes exceeded") {
		return fmt.Errorf("%w: %v", ErrQueueLimit, err)
	}
	return err
}

func (s *brokerStore) create(ctx context.Context, key string, value any, limit int) error {
	data, err := encodeTunnelJSON(value, limit)
	if err != nil {
		return err
	}
	_, err = s.bucket.Create(ctx, key, data)
	return brokerCapacityError(err)
}
