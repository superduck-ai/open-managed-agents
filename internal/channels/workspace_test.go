package channels

import (
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/config"
)

func TestBridgeWorkspaceRouterServesChannelQr(t *testing.T) {
	handler := newChannelHandler(config.ChannelBridgeConfig{}, nil, nil, nil)
	patterns := routePatterns(t, handler.workspaceRouter())
	for _, route := range []string{"GET /{channel_id}/qr", "POST /{channel_id}/qr"} {
		if _, ok := patterns[route]; !ok {
			t.Fatalf("桥接工作区路由缺少 %s：%v", route, patterns)
		}
	}
}
