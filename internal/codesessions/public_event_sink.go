package codesessions

import (
	"context"
	"encoding/json"

	"github.com/superduck-ai/open-managed-agents/internal/db"
)

// PublicEventSink 隔离 code-session 领域逻辑与公开 session 事件投递实现。
// 接口保留在 codesessions 包内，避免 Service 反向依赖具体 API/传输层。
type PublicEventSink interface {
	PublishCodeSessionEvents(ctx context.Context, codeSession db.CodeSession, payloads []json.RawMessage) error
}

// SetPublicEventSink 在服务组装阶段注入事件接收端。Service 发布事件时会在锁内取得 sink 快照、
// 在锁外执行 I/O；这既避免装配或测试替换 sink 时与 worker 发布发生 data race，也不会让慢投递长期占锁。
func (s *Service) SetPublicEventSink(sink PublicEventSink) {
	if s == nil {
		return
	}
	s.sinkMu.Lock()
	defer s.sinkMu.Unlock()
	s.sink = sink
}
