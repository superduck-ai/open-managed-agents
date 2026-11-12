package channels

import (
	"net/http"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func routePatterns(t *testing.T, router http.Handler) map[string]struct{} {
	t.Helper()
	patterns := make(map[string]struct{})
	if err := chi.Walk(router.(chi.Routes), func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		patterns[method+" "+route] = struct{}{}
		return nil
	}); err != nil {
		t.Fatalf("walk routes: %v", err)
	}
	return patterns
}

func TestWorkspaceRouterServesChannelQr(t *testing.T) {
	handler := newLocalHandler(newLocalService(&fakeRegistrationStore{records: map[string]db.ChannelRegistrationRecord{}}, testCipher(t), nil, nil), nil)
	patterns := routePatterns(t, handler.workspaceRouter())
	for _, route := range []string{"GET /{channel_id}/qr", "POST /{channel_id}/qr", "GET /authorizations/{authorization_id}", "POST /authorizations"} {
		if _, ok := patterns[route]; !ok {
			t.Fatalf("工作区路由缺少 %s：%v", route, patterns)
		}
	}
}

func TestAgentRouterNoLongerServesChannelQr(t *testing.T) {
	handler := newLocalHandler(newLocalService(&fakeRegistrationStore{records: map[string]db.ChannelRegistrationRecord{}}, testCipher(t), nil, nil), nil)
	patterns := routePatterns(t, handler.agentRouter())
	if _, ok := patterns["GET /{channel_id}/qr"]; ok {
		t.Fatalf("扫码不应再挂在 agent 路由：%v", patterns)
	}
}
