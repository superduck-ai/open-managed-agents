package workerevents

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

const (
	// StreamName 是所有 Code Session 共用的 worker 入站事件 JetStream Stream 名称。
	StreamName = "OMA_WORKER_INBOUND"
	// subjectPrefix 后拼接 Code Session ID，组成当前 v2 协议的会话级 subject。
	subjectPrefix = "oma.worker.inbound.v2."
	// streamSubject 使用 > 通配符，让共享 Stream 收集所有会话的入站事件。
	streamSubject = subjectPrefix + ">"
	// MaxMessageBytes 是编码后单条 envelope 的大小上限：1 MiB。
	MaxMessageBytes = 1 << 20
	// LargePayloadThreshold 是编码后 envelope 的外置阈值：超过 900 KiB 时，
	// 将 payload 存入对象存储，envelope 只保留引用，再检查完整 envelope 的大小上限。
	LargePayloadThreshold = 900 << 10
	// envelopeOverheadAllowance 是 envelope 固定字段编码长度的估算上界，
	// 供按 payload 长度估算 envelope 大小时使用。
	envelopeOverheadAllowance = 512
	// MaxOffloadedPayloadBytes 是允许外置到对象存储的单个 payload 大小上限。
	// payload 不只来自 ingress 请求体（activation 历史来自 session_events），
	// 因此该契约属于 worker event 传输层，而不是 HTTP body 限制。
	MaxOffloadedPayloadBytes = 16 << 20
	// LogicalRetention 是入站事件的逻辑有效期：30 天，通过 expires_at 和应用过期清理执行，
	// 不使用 JetStream MaxAge 自动删除未处理消息。
	LogicalRetention = 30 * 24 * time.Hour
	// AcknowledgementStoreTTL 是事件到 ACK subject 临时映射的有效期：20 分钟。
	// received/processing 回执会续期；映射过期后依靠消息重投重建，不代表消息已完成。
	AcknowledgementStoreTTL = 20 * time.Minute
	// duplicateWindow 是消息 ID 去重窗口，JetStream Duplicates 与内存实现保持一致。
	duplicateWindow = 24 * time.Hour
)

type PayloadReference struct {
	Key          string `json:"key"`
	Size         int64  `json:"size"`
	SHA256       string `json:"sha256"`
	CleanupJobID string `json:"cleanup_job_id"`
}

// EnvelopeV1 retains the Go type name used by callers while version 2 changes
// transport ownership from PostgreSQL rows to JetStream messages.
type EnvelopeV1 struct {
	Version        int               `json:"version"`
	CodeSessionID  string            `json:"code_session_id"`
	EventID        string            `json:"event_id"`
	PayloadEventID string            `json:"payload_event_id,omitempty"`
	SequenceNum    int64             `json:"sequence_num"`
	EventType      string            `json:"event_type"`
	EventSubtype   string            `json:"event_subtype,omitempty"`
	Payload        json.RawMessage   `json:"payload,omitempty"`
	PayloadRef     *PayloadReference `json:"payload_ref,omitempty"`
	ExpiresAt      time.Time         `json:"expires_at"`
}

type Delivery struct {
	Envelope   EnvelopeV1
	AckSubject string
}

// IsExpired 报告 now 是否已越过 envelope 的逻辑有效期。过期消息不允许静默跳过：
// 消费方必须终止对应 Code Session，该判定因此收敛在 envelope 自身。
func (e EnvelopeV1) IsExpired(now time.Time) bool {
	return !e.ExpiresAt.IsZero() && !now.Before(e.ExpiresAt)
}

// LikelyExceedsLargePayload 按未编码 payload 的长度估算 envelope 是否可能超过
// LargePayloadThreshold。payload 内嵌 RawMessage，编码只会 compact 而不会变长，
// 长度加固定字段余量的判定方向保守（宁可多外置也不漏判），调用方因此不必为了
// 判大小先做一次全量 JSON 编码。
func LikelyExceedsLargePayload(payloadSize int) bool {
	return payloadSize+envelopeOverheadAllowance > LargePayloadThreshold
}

type ExpiredEvent struct {
	StreamSequence uint64
	Envelope       EnvelopeV1
}

type Subscription interface {
	Messages() <-chan Delivery
	Errors() <-chan error
	Close() error
}

type Broker interface {
	Publish(context.Context, string, EnvelopeV1) error
	Subscribe(context.Context, string) (Subscription, error)
	InProgress(ctx context.Context, ackSubject string) error
	DoubleAck(ctx context.Context, ackSubject string) error
	Term(ctx context.Context, ackSubject string) error
	ScanExpired(context.Context, uint64, int, time.Time) ([]ExpiredEvent, uint64, error)
	PurgeSession(context.Context, string) error
}

func EventEnvelope(codeSessionID, eventID, payloadEventID string, eventType, eventSubtype string, payload json.RawMessage, expiresAt time.Time) EnvelopeV1 {
	return EnvelopeV1{
		Version:        2,
		CodeSessionID:  codeSessionID,
		EventID:        eventID,
		PayloadEventID: payloadEventID,
		EventType:      eventType,
		EventSubtype:   eventSubtype,
		Payload:        payload,
		ExpiresAt:      expiresAt,
	}
}

func Subject(codeSessionID string) (string, error) {
	if codeSessionID == "" || strings.ContainsAny(codeSessionID, ".*> \t\r\n") {
		return "", errors.New("invalid code session ID for worker event subject")
	}
	return subjectPrefix + codeSessionID, nil
}

func consumerName(codeSessionID string) string { return "oma_worker_" + codeSessionID }

type JetStreamBroker struct {
	connection *nats.Conn
	js         jetstream.JetStream
}

func NewJetStream(ctx context.Context, connection *nats.Conn) (*JetStreamBroker, error) {
	if connection == nil || !connection.IsConnected() {
		return nil, nats.ErrDisconnected
	}
	js, err := jetstream.New(connection)
	if err != nil {
		return nil, fmt.Errorf("create worker event JetStream client: %w", err)
	}
	_, err = js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name:       StreamName,
		Subjects:   []string{streamSubject},
		Retention:  jetstream.WorkQueuePolicy,
		Discard:    jetstream.DiscardNew,
		MaxAge:     0,
		MaxBytes:   10 << 30,
		MaxMsgSize: MaxMessageBytes,
		Storage:    jetstream.FileStorage,
		Replicas:   3,
		Duplicates: duplicateWindow,
	})
	if err != nil {
		return nil, fmt.Errorf("ensure worker event stream: %w", err)
	}
	return &JetStreamBroker{connection: connection, js: js}, nil
}

func (b *JetStreamBroker) Publish(ctx context.Context, messageID string, envelope EnvelopeV1) error {
	subjectName, err := Subject(envelope.CodeSessionID)
	if err != nil {
		return err
	}
	body, err := json.Marshal(envelope)
	if err != nil {
		return fmt.Errorf("marshal worker event envelope: %w", err)
	}
	if len(body) > MaxMessageBytes {
		return fmt.Errorf("worker event envelope is %d bytes, limit is %d", len(body), MaxMessageBytes)
	}
	message := nats.NewMsg(subjectName)
	message.Data = body
	message.Header.Set(nats.MsgIdHdr, messageID)
	if _, err := b.js.PublishMsg(ctx, message); err != nil {
		return fmt.Errorf("publish worker event: %w", err)
	}
	return nil
}

func (b *JetStreamBroker) Subscribe(ctx context.Context, codeSessionID string) (Subscription, error) {
	filter, err := Subject(codeSessionID)
	if err != nil {
		return nil, err
	}
	name := consumerName(codeSessionID)
	consumer, err := b.js.CreateOrUpdateConsumer(ctx, StreamName, jetstream.ConsumerConfig{
		Name:            name,
		Durable:         name,
		DeliverPolicy:   jetstream.DeliverAllPolicy,
		AckPolicy:       jetstream.AckExplicitPolicy,
		FilterSubject:   filter,
		MaxDeliver:      -1,
		BackOff:         []time.Duration{time.Minute, 5 * time.Minute, 15 * time.Minute},
		MaxAckPending:   1,
		MaxRequestBatch: 1,
	})
	if err != nil {
		return nil, fmt.Errorf("ensure worker event consumer: %w", err)
	}
	subCtx, cancel := context.WithCancel(ctx)
	s := &jetStreamSubscription{consumer: consumer, ctx: subCtx, cancel: cancel, messages: make(chan Delivery), errors: make(chan error, 1)}
	go s.receive()
	return s, nil
}

func (b *JetStreamBroker) InProgress(_ context.Context, ackSubject string) error {
	if ackSubject == "" {
		return errAckSubjectEmpty
	}
	return b.connection.Publish(ackSubject, []byte("+WPI"))
}

func (b *JetStreamBroker) DoubleAck(ctx context.Context, ackSubject string) error {
	if ackSubject == "" {
		return errAckSubjectEmpty
	}
	if _, err := b.connection.RequestWithContext(ctx, ackSubject, []byte("+ACK")); err != nil {
		return fmt.Errorf("confirm worker event ACK: %w", err)
	}
	return nil
}

func (b *JetStreamBroker) Term(_ context.Context, ackSubject string) error {
	if ackSubject == "" {
		return errAckSubjectEmpty
	}
	return b.connection.Publish(ackSubject, []byte("+TERM"))
}

func (b *JetStreamBroker) ScanExpired(ctx context.Context, cursor uint64, limit int, now time.Time) ([]ExpiredEvent, uint64, error) {
	stream, err := b.js.Stream(ctx, StreamName)
	if err != nil {
		return nil, cursor, err
	}
	info, err := stream.Info(ctx)
	if err != nil {
		return nil, cursor, err
	}
	if info.State.Msgs == 0 {
		return nil, 0, nil
	}
	sequence := cursor
	if sequence < info.State.FirstSeq || sequence > info.State.LastSeq {
		sequence = info.State.FirstSeq
	}
	expired := make([]ExpiredEvent, 0)
	for inspected := 0; inspected < limit && sequence <= info.State.LastSeq; inspected++ {
		message, getErr := stream.GetMsg(ctx, sequence)
		if getErr == nil {
			var envelope EnvelopeV1
			if decodeErr := json.Unmarshal(message.Data, &envelope); decodeErr != nil {
				return nil, sequence, fmt.Errorf("decode stored worker event envelope at sequence %d: %w", sequence, decodeErr)
			}
			if envelope.IsExpired(now) {
				expired = append(expired, ExpiredEvent{StreamSequence: message.Sequence, Envelope: envelope})
			}
		} else if !errors.Is(getErr, jetstream.ErrMsgNotFound) {
			return nil, sequence, getErr
		}
		sequence++
	}
	if sequence > info.State.LastSeq {
		sequence = 0
	}
	return expired, sequence, nil
}

func (b *JetStreamBroker) PurgeSession(ctx context.Context, codeSessionID string) error {
	filter, err := Subject(codeSessionID)
	if err != nil {
		return err
	}
	stream, err := b.js.Stream(ctx, StreamName)
	if err != nil {
		return err
	}
	if err := stream.Purge(ctx, jetstream.WithPurgeSubject(filter)); err != nil {
		return err
	}
	err = b.js.DeleteConsumer(ctx, StreamName, consumerName(codeSessionID))
	if errors.Is(err, jetstream.ErrConsumerNotFound) {
		return nil
	}
	return err
}

type jetStreamSubscription struct {
	consumer  jetstream.Consumer
	ctx       context.Context
	cancel    context.CancelFunc
	messages  chan Delivery
	errors    chan error
	closeOnce sync.Once
}

func (s *jetStreamSubscription) Messages() <-chan Delivery { return s.messages }
func (s *jetStreamSubscription) Errors() <-chan error      { return s.errors }

func (s *jetStreamSubscription) receive() {
	defer close(s.messages)
	defer close(s.errors)
	for s.ctx.Err() == nil {
		batch, err := s.consumer.Fetch(1, jetstream.FetchContext(s.ctx))
		if err != nil {
			if s.ctx.Err() == nil {
				s.report(err)
			}
			return
		}
		for message := range batch.Messages() {
			var envelope EnvelopeV1
			if err := json.Unmarshal(message.Data(), &envelope); err != nil {
				s.report(fmt.Errorf("decode worker event envelope: %w", err))
				return
			}
			metadata, err := message.Metadata()
			if err != nil {
				s.report(fmt.Errorf("read worker event metadata: %w", err))
				return
			}
			envelope.SequenceNum = int64(metadata.Sequence.Stream)
			delivery := Delivery{Envelope: envelope, AckSubject: message.Reply()}
			select {
			case s.messages <- delivery:
			case <-s.ctx.Done():
				return
			}
		}
		if err := batch.Error(); err != nil && !errors.Is(err, context.Canceled) {
			s.report(err)
			return
		}
	}
}

func (s *jetStreamSubscription) report(err error) {
	select {
	case s.errors <- err:
	default:
	}
}

func (s *jetStreamSubscription) Close() error {
	s.closeOnce.Do(s.cancel)
	return nil
}

type memoryMessage struct {
	id        uint64
	envelope  EnvelopeV1
	delivered bool
}

type MemoryBroker struct {
	mu            sync.Mutex
	nextID        uint64
	queues        map[string][]*memoryMessage
	subs          map[string]*memorySubscription
	ackSubjectMap map[string]*memoryMessage
	messageIDs    map[string]time.Time
}

func NewMemory() *MemoryBroker {
	return &MemoryBroker{
		queues: make(map[string][]*memoryMessage), subs: make(map[string]*memorySubscription),
		ackSubjectMap: make(map[string]*memoryMessage), messageIDs: make(map[string]time.Time),
	}
}

// Pending returns a point-in-time copy of unacknowledged envelopes. It is used
// by in-process diagnostics and tests without changing durable delivery state.
func (b *MemoryBroker) Pending(codeSessionID string) []EnvelopeV1 {
	b.mu.Lock()
	defer b.mu.Unlock()
	queue := b.queues[codeSessionID]
	result := make([]EnvelopeV1, len(queue))
	for i, message := range queue {
		result[i] = message.envelope
		result[i].Payload = append(json.RawMessage(nil), message.envelope.Payload...)
		if message.envelope.PayloadRef != nil {
			reference := *message.envelope.PayloadRef
			result[i].PayloadRef = &reference
		}
	}
	return result
}

func (b *MemoryBroker) Publish(ctx context.Context, messageID string, envelope EnvelopeV1) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, exists := b.messageIDs[messageID]; messageID != "" && exists {
		return nil
	}
	b.nextID++
	envelope.SequenceNum = int64(b.nextID)
	message := &memoryMessage{id: b.nextID, envelope: envelope}
	b.queues[envelope.CodeSessionID] = append(b.queues[envelope.CodeSessionID], message)
	if messageID != "" {
		now := time.Now()
		b.evictStaleMessageIDsLocked(now)
		b.messageIDs[messageID] = now
	}
	b.dispatchLocked(envelope.CodeSessionID)
	return nil
}

// evictStaleMessageIDsLocked 在去重集合增长到阈值后清理超出 duplicateWindow 的消息 ID，
// 防止长驻内存 broker 的去重 map 只增不减；窗口语义与 JetStream Duplicates 对齐。
func (b *MemoryBroker) evictStaleMessageIDsLocked(now time.Time) {
	if len(b.messageIDs) < 1024 {
		return
	}
	cutoff := now.Add(-duplicateWindow)
	for id, publishedAt := range b.messageIDs {
		if publishedAt.Before(cutoff) {
			delete(b.messageIDs, id)
		}
	}
}

func (b *MemoryBroker) Subscribe(ctx context.Context, codeSessionID string) (Subscription, error) {
	if _, err := Subject(codeSessionID); err != nil {
		return nil, err
	}
	b.mu.Lock()
	if existing := b.subs[codeSessionID]; existing != nil {
		b.undeliverHeadLocked(codeSessionID)
		select {
		case existing.errors <- errors.New("worker event consumer was superseded"):
		default:
		}
	}
	s := &memorySubscription{broker: b, codeSessionID: codeSessionID, messages: make(chan Delivery, 1), errors: make(chan error, 1), done: make(chan struct{})}
	b.subs[codeSessionID] = s
	b.dispatchLocked(codeSessionID)
	b.mu.Unlock()
	go func() { <-ctx.Done(); _ = s.Close() }()
	return s, nil
}

func (b *MemoryBroker) InProgress(context.Context, string) error { return nil }

func (b *MemoryBroker) DoubleAck(_ context.Context, ackSubject string) error {
	return b.finishDelivery(ackSubject)
}

func (b *MemoryBroker) Term(_ context.Context, ackSubject string) error {
	return b.finishDelivery(ackSubject)
}

func (b *MemoryBroker) finishDelivery(ackSubject string) error {
	if ackSubject == "" {
		return errAckSubjectEmpty
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	message := b.ackSubjectMap[ackSubject]
	if message == nil {
		return errMemoryAckNotFound
	}
	sessionID := message.envelope.CodeSessionID
	queue := b.queues[sessionID]
	if len(queue) == 0 || queue[0] != message {
		return errMemoryAckNotQueueHead
	}
	delete(b.ackSubjectMap, ackSubject)
	queue[0] = nil
	b.queues[sessionID] = queue[1:]
	b.dispatchLocked(sessionID)
	return nil
}

// undeliverHeadLocked 将队头消息重置回未投递状态并清除其 ACK subject 映射，
// 供订阅被抢占或关闭后重新投递使用。
func (b *MemoryBroker) undeliverHeadLocked(codeSessionID string) {
	queue := b.queues[codeSessionID]
	if len(queue) == 0 {
		return
	}
	queue[0].delivered = false
	delete(b.ackSubjectMap, b.ackSubject(queue[0]))
}

func (b *MemoryBroker) ScanExpired(_ context.Context, cursor uint64, limit int, now time.Time) ([]ExpiredEvent, uint64, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	messages := make([]*memoryMessage, 0)
	for _, queue := range b.queues {
		messages = append(messages, queue...)
	}
	sort.Slice(messages, func(i, j int) bool { return messages[i].id < messages[j].id })
	expired := make([]ExpiredEvent, 0)
	next := uint64(0)
	inspected := 0
	for _, message := range messages {
		if message.id < cursor {
			continue
		}
		if inspected >= limit {
			return expired, next, nil
		}
		inspected++
		next = message.id + 1
		if message.envelope.IsExpired(now) {
			expired = append(expired, ExpiredEvent{StreamSequence: message.id, Envelope: message.envelope})
		}
	}
	return expired, 0, nil
}

func (b *MemoryBroker) PurgeSession(_ context.Context, codeSessionID string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, message := range b.queues[codeSessionID] {
		delete(b.ackSubjectMap, b.ackSubject(message))
	}
	delete(b.queues, codeSessionID)
	return nil
}

func (b *MemoryBroker) dispatchLocked(codeSessionID string) {
	subscription := b.subs[codeSessionID]
	queue := b.queues[codeSessionID]
	if subscription == nil || len(queue) == 0 || queue[0].delivered {
		return
	}
	message := queue[0]
	message.delivered = true
	ackSubject := b.ackSubject(message)
	b.ackSubjectMap[ackSubject] = message
	delivery := Delivery{Envelope: message.envelope, AckSubject: ackSubject}
	select {
	case subscription.messages <- delivery:
	default:
		message.delivered = false
		delete(b.ackSubjectMap, ackSubject)
	}
}

func (b *MemoryBroker) ackSubject(message *memoryMessage) string {
	return "memory-worker-event-ack-" + strconv.FormatUint(message.id, 10)
}

type memorySubscription struct {
	broker        *MemoryBroker
	codeSessionID string
	messages      chan Delivery
	errors        chan error
	done          chan struct{}
	closeOnce     sync.Once
}

func (s *memorySubscription) Messages() <-chan Delivery { return s.messages }
func (s *memorySubscription) Errors() <-chan error      { return s.errors }
func (s *memorySubscription) Close() error {
	s.closeOnce.Do(func() {
		s.broker.mu.Lock()
		if s.broker.subs[s.codeSessionID] == s {
			delete(s.broker.subs, s.codeSessionID)
			s.broker.undeliverHeadLocked(s.codeSessionID)
		}
		close(s.done)
		s.broker.mu.Unlock()
	})
	return nil
}
