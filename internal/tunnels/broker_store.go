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

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

const (
	commandStreamName    = "OMA_TUNNEL_COMMANDS_V1"
	commandSubjectPrefix = "oma.tunnel.command.v1."
	requestBucketName    = "OMA_TUNNEL_REQUESTS_V1"
	controlBucketName    = "OMA_TUNNEL_CONTROL_V1"
	maxBrokerValueBytes  = 2 << 20
	maxControlRecords    = 4096
	maxControlValueBytes = 256 << 10
	brokerCASAttempts    = 64
)

// brokerStore uses normal stream GET requests, which are served by the stream
// leader. Missing is still not sufficient evidence to discard a live command.
type brokerStore struct {
	bucket jetstream.KeyValue
	name   string
	stream jetstream.Stream
}

type storedBrokerValue struct {
	revision uint64
}

func openBrokerStore(ctx context.Context, js jetstream.JetStream, name string, maxRecords int64, maxValueBytes int32, ttl time.Duration, replicas int) (*brokerStore, error) {
	stream, err := js.CreateStream(ctx, jetstream.StreamConfig{
		Name: "KV_" + name, Subjects: []string{"$KV." + name + ".>"},
		Storage: jetstream.FileStorage, Replicas: replicas,
		MaxAge: ttl, MaxMsgSize: maxValueBytes, MaxMsgs: maxRecords,
		MaxMsgsPerSubject: 1, Discard: jetstream.DiscardNew,
		AllowRollup: true, DenyDelete: true, AllowDirect: false,
		// Include storage/header overhead and room for replacing a full record.
		MaxBytes: 2 * (maxRecords + 1) * (int64(maxValueBytes) + 4096),
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

func (s *brokerStore) read(ctx context.Context, key string, target any) (storedBrokerValue, error) {
	msg, err := s.stream.GetLastMsgForSubject(ctx, "$KV."+s.name+"."+key)
	if errors.Is(err, jetstream.ErrMsgNotFound) {
		return storedBrokerValue{}, ErrRequestNotFound
	}
	if err != nil {
		return storedBrokerValue{}, err
	}
	if msg.Header.Get("KV-Operation") != "" {
		return storedBrokerValue{}, ErrRequestNotFound
	}
	if err := json.Unmarshal(msg.Data, target); err != nil {
		return storedBrokerValue{}, fmt.Errorf("decode tunnel KV: %w", err)
	}
	return storedBrokerValue{revision: msg.Sequence}, nil
}

func encodeTunnelJSON(value any, limit int) ([]byte, error) {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return nil, err
	}
	if buffer.Len() > limit {
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

func brokerCASConflict(err error) bool {
	return errors.Is(err, jetstream.ErrKeyExists)
}

func brokerPause(ctx context.Context, attempt int) error {
	timer := time.NewTimer(time.Duration(1+attempt%8) * time.Millisecond)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
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

func (s *brokerStore) update(ctx context.Context, key string, value any, revision uint64, limit int) error {
	data, err := encodeTunnelJSON(value, limit)
	if err != nil {
		return err
	}
	_, err = s.bucket.Update(ctx, key, data, revision)
	return err
}

func publishBrokerSignal(connection *nats.Conn, subject, key string) {
	// The durable record is authoritative; this signal only accelerates reads.
	data, err := json.Marshal(responseEnvelope{Key: key})
	if err == nil {
		_ = connection.Publish(subject, data)
	}
}
