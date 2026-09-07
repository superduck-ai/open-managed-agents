package workerevents

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

type AckRef struct {
	AckSubject   string `json:"ack_subject"`
	CleanupJobID string `json:"cleanup_job_id,omitempty"`
}

// AckStore 保存业务事件与 JetStream ACK subject 之间的短期映射。
//
// SSE 和 delivery API 是两个独立请求，甚至可能由不同服务实例处理。SSE 从
// JetStream 拉到消息时持有完整 Msg，但 worker 稍后上报处理状态时只携带 event_id，
// 无法仅凭 event_id 推导出本次投递的 ACK subject。因此，服务必须在 SSE flush
// 前保存下面的映射：
//
//	(code_session_id, worker_epoch, event_id) -> {ack_subject, cleanup_job_id}
//
// 例如，实例 A 通过 SSE 发送事件 evt_123，并保存：
//
//	(cse_abc, 7, evt_123) -> {$JS.ACK.OMA_WORKER_INBOUND..., job_xyz}
//
// 随后实例 B 收到 worker 的 processed 上报，先用 Get 找回 ACK subject，再向
// JetStream 发送最终 ACK；成功后用 Delete 移除映射。received 和 processing
// 上报则发送 InProgress，并用 Refresh 延长映射的 TTL。
//
// 这里的数据不是消息事实源，可以安全丢失。Redis 重启或映射过期时，delivery
// API 会忽略本次上报且不 ACK；JetStream 随后重新投递消息，SSE 会写入新的映射。
type AckStore interface {
	Put(context.Context, string, int64, string, AckRef) error
	Get(context.Context, string, int64, string) (AckRef, bool, error)
	// GetMany 一次取回同一 (session, epoch) 下多个 event 的映射，返回按 eventID
	// 索引的结果；不存在的 event 不出现在返回 map 中。
	GetMany(context.Context, string, int64, []string) (map[string]AckRef, error)
	Refresh(context.Context, string, int64, string) error
	Delete(context.Context, string, int64, string) error
}

type RedisAckStore struct{ client *redis.Client }

func NewRedisAckStore(client *redis.Client) *RedisAckStore {
	return &RedisAckStore{client: client}
}

func (s *RedisAckStore) Put(ctx context.Context, sessionID string, epoch int64, eventID string, reference AckRef) error {
	body, err := json.Marshal(reference)
	if err != nil {
		return err
	}
	return s.client.Set(ctx, acknowledgementKey(sessionID, epoch, eventID), body, AcknowledgementStoreTTL).Err()
}

func (s *RedisAckStore) Get(ctx context.Context, sessionID string, epoch int64, eventID string) (AckRef, bool, error) {
	body, err := s.client.Get(ctx, acknowledgementKey(sessionID, epoch, eventID)).Bytes()
	if errors.Is(err, redis.Nil) {
		return AckRef{}, false, nil
	}
	if err != nil {
		return AckRef{}, false, err
	}
	var reference AckRef
	if err := json.Unmarshal(body, &reference); err != nil {
		return AckRef{}, false, err
	}
	return reference, true, nil
}

// GetMany 用 pipeline 一次取回全部映射，替代逐 event 的串行 round trip。
func (s *RedisAckStore) GetMany(ctx context.Context, sessionID string, epoch int64, eventIDs []string) (map[string]AckRef, error) {
	references := make(map[string]AckRef, len(eventIDs))
	if len(eventIDs) == 0 {
		return references, nil
	}
	commands := make([]redis.Cmder, 0, len(eventIDs))
	pipe := s.client.Pipeline()
	for _, eventID := range eventIDs {
		commands = append(commands, pipe.Get(ctx, acknowledgementKey(sessionID, epoch, eventID)))
	}
	if _, err := pipe.Exec(ctx); err != nil && !errors.Is(err, redis.Nil) {
		return nil, err
	}
	for index, command := range commands {
		eventID := eventIDs[index]
		body, err := command.(*redis.StringCmd).Bytes()
		if err != nil {
			if errors.Is(err, redis.Nil) {
				continue
			}
			return nil, err
		}
		var reference AckRef
		if err := json.Unmarshal(body, &reference); err != nil {
			return nil, err
		}
		references[eventID] = reference
	}
	return references, nil
}

func (s *RedisAckStore) Refresh(ctx context.Context, sessionID string, epoch int64, eventID string) error {
	return s.client.Expire(ctx, acknowledgementKey(sessionID, epoch, eventID), AcknowledgementStoreTTL).Err()
}

func (s *RedisAckStore) Delete(ctx context.Context, sessionID string, epoch int64, eventID string) error {
	return s.client.Del(ctx, acknowledgementKey(sessionID, epoch, eventID)).Err()
}

func acknowledgementKey(sessionID string, epoch int64, eventID string) string {
	digest := sha256.Sum256([]byte(eventID))
	return "oma:worker-event-ack:v2:" + sessionID + ":" + strconv.FormatInt(epoch, 10) + ":" + hex.EncodeToString(digest[:])
}

type memoryAcknowledgementEntry struct {
	reference AckRef
	expiresAt time.Time
}

// memoryAcknowledgementEvictionThreshold 触发进程内 store 惰性清理的条目数阈值。
const memoryAcknowledgementEvictionThreshold = 1024

type MemoryAcknowledgementStore struct {
	mu      sync.Mutex
	entries map[string]memoryAcknowledgementEntry
}

func NewMemoryAcknowledgementStore() *MemoryAcknowledgementStore {
	return &MemoryAcknowledgementStore{entries: make(map[string]memoryAcknowledgementEntry)}
}

func (s *MemoryAcknowledgementStore) Put(_ context.Context, sessionID string, epoch int64, eventID string, reference AckRef) error {
	now := time.Now()
	s.mu.Lock()
	if len(s.entries) >= memoryAcknowledgementEvictionThreshold {
		s.evictExpiredLocked(now)
	}
	s.entries[acknowledgementKey(sessionID, epoch, eventID)] = memoryAcknowledgementEntry{reference: reference, expiresAt: now.Add(AcknowledgementStoreTTL)}
	s.mu.Unlock()
	return nil
}

// evictExpiredLocked 清理已过期的映射条目。无 Redis 的进程内 store 没有外部
// TTL 机制，靠 Put 时的阈值触发驱逐，避免长期运行时 map 只增不减。
func (s *MemoryAcknowledgementStore) evictExpiredLocked(now time.Time) {
	for key, entry := range s.entries {
		if now.After(entry.expiresAt) {
			delete(s.entries, key)
		}
	}
}

func (s *MemoryAcknowledgementStore) Get(_ context.Context, sessionID string, epoch int64, eventID string) (AckRef, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := acknowledgementKey(sessionID, epoch, eventID)
	entry, found := s.entries[key]
	if !found || time.Now().After(entry.expiresAt) {
		delete(s.entries, key)
		return AckRef{}, false, nil
	}
	return entry.reference, true, nil
}

// GetMany 逐项复用 Get 的查找语义，返回按 eventID 索引的映射。
func (s *MemoryAcknowledgementStore) GetMany(ctx context.Context, sessionID string, epoch int64, eventIDs []string) (map[string]AckRef, error) {
	references := make(map[string]AckRef, len(eventIDs))
	for _, eventID := range eventIDs {
		reference, found, err := s.Get(ctx, sessionID, epoch, eventID)
		if err != nil {
			return nil, err
		}
		if found {
			references[eventID] = reference
		}
	}
	return references, nil
}

func (s *MemoryAcknowledgementStore) Refresh(_ context.Context, sessionID string, epoch int64, eventID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := acknowledgementKey(sessionID, epoch, eventID)
	entry, found := s.entries[key]
	if found {
		entry.expiresAt = time.Now().Add(AcknowledgementStoreTTL)
		s.entries[key] = entry
	}
	return nil
}

func (s *MemoryAcknowledgementStore) Delete(_ context.Context, sessionID string, epoch int64, eventID string) error {
	s.mu.Lock()
	delete(s.entries, acknowledgementKey(sessionID, epoch, eventID))
	s.mu.Unlock()
	return nil
}
