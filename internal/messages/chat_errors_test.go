package messages

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/codesessions"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
)

func TestChatSessionLookupError(t *testing.T) {
	for _, test := range []struct {
		name    string
		cause   error
		status  int
		message string
	}{
		{"missing session", db.ErrNotFound, http.StatusUnauthorized, "Invalid API key"},
		{"wrapped missing session", fmt.Errorf("load transport: %w", db.ErrNotFound), http.StatusUnauthorized, "Invalid API key"},
		{"database failure", errors.New("private database failure"), http.StatusInternalServerError, "Workspace model configuration is unavailable"},
	} {
		t.Run(test.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
			httpapi.WriteError(response, request, chatSessionLookupError(test.cause))
			if response.Code != test.status || !strings.Contains(response.Body.String(), test.message) {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
			if strings.Contains(response.Body.String(), "private database failure") {
				t.Fatal("response exposes private cause")
			}
		})
	}
}

func TestModelRequestStartBudgetError(t *testing.T) {
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	httpapi.WriteError(response, request, modelRequestStartError(fmt.Errorf("persist start: %w", db.ErrSessionBudgetReached)))
	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "session budget reached:") {
		t.Fatalf("预算拒绝被当成上游故障：status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestModelRequestPriceUnavailableError(t *testing.T) {
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	httpapi.WriteError(response, request, modelRequestStartError(fmt.Errorf("pricing: %w", codesessions.ErrModelPriceUnavailable)))
	if response.Code != http.StatusServiceUnavailable || !strings.Contains(response.Body.String(), `"code":"model_price_unavailable"`) {
		t.Fatalf("错误价格门禁响应: %s", response.Body.String())
	}
}
