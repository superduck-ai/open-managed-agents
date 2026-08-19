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

// SetPublicEventSink 在服务组装阶段注入事件接收端。
// 必须在 Service 开始处理请求或 worker 事件前调用，运行期不得替换。
func (s *Service) SetPublicEventSink(sink PublicEventSink) {
	if s == nil {
		return
	}
	s.sink = sink
}
