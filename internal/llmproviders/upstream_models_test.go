package llmproviders

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestListUpstreamModelsRejectsMissingClientAndNonSuccess(t *testing.T) {
	if _, err := ListUpstreamModels(context.Background(), nil, Upstream{BaseURL: "https://example.com"}); !errors.Is(err, ErrUpstreamModels) {
		t.Fatalf("ListUpstreamModels() error = %v, want ErrUpstreamModels", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(server.Close)
	_, err := ListUpstreamModels(context.Background(), server.Client(), Upstream{BaseURL: server.URL, APIKey: "secret"})
	if !errors.Is(err, ErrUpstreamModels) {
		t.Fatalf("ListUpstreamModels() error = %v, want ErrUpstreamModels", err)
	}
}

func TestListUpstreamModelsReadsAnthropicModelIDs(t *testing.T) {
	var gotPath, gotKey, gotBearer, gotVersion string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotKey = r.Header.Get("X-Api-Key")
		gotBearer = r.Header.Get("Authorization")
		gotVersion = r.Header.Get("Anthropic-Version")
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"data":[{"type":"model","id":"glm-4.7"},{"id":"kimi-k2.5"},{"id":"glm-4.7"}]}`)
	}))
	t.Cleanup(server.Close)

	modelIDs, err := ListUpstreamModels(context.Background(), NewHTTPClient(time.Second), Upstream{
		BaseURL: server.URL + "/anthropic",
		APIKey:  "live-secret",
	})
	if err != nil {
		t.Fatalf("ListUpstreamModels() error = %v", err)
	}
	if gotPath != "/anthropic/v1/models" || gotKey != "live-secret" || gotBearer != "Bearer live-secret" || gotVersion != anthropicVersion {
		t.Fatalf("upstream request path=%q key=%q bearer=%q version=%q", gotPath, gotKey, gotBearer, gotVersion)
	}
	if len(modelIDs) != 2 || modelIDs[0] != "glm-4.7" || modelIDs[1] != "kimi-k2.5" {
		t.Fatalf("model IDs = %#v", modelIDs)
	}
}

func TestApplyAPIKeySetsAnthropicAndBearerHeaders(t *testing.T) {
	headers := http.Header{
		"X-Api-Key":     {"caller-key"},
		"Authorization": {"Bearer caller-key"},
	}

	ApplyAPIKey(headers, "provider-key")

	if headers.Get("X-Api-Key") != "provider-key" || headers.Get("Authorization") != "Bearer provider-key" {
		t.Fatalf("provider authentication headers = %#v", headers)
	}
}

func TestMessageRequestModelRejectsDuplicateTopLevelModel(t *testing.T) {
	_, err := MessageRequestModel([]byte(`{"model":"configured","model":"not-configured"}`))
	if !errors.Is(err, ErrDuplicateRequestModel) {
		t.Fatalf("MessageRequestModel() error = %v, want ErrDuplicateRequestModel", err)
	}
}

func TestMessageRequestModelRejectsSurroundingWhitespace(t *testing.T) {
	_, err := MessageRequestModel([]byte(`{"model":" configured "}`))
	if !errors.Is(err, ErrRequestModelWhitespace) {
		t.Fatalf("MessageRequestModel() error = %v, want ErrRequestModelWhitespace", err)
	}
}

func TestListUpstreamModelsAcceptsBearerAndRejectsErrorEnvelope(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Header.Get("Authorization") != "Bearer live-secret" {
			_, _ = io.WriteString(w, `{"code":1001,"msg":"missing Authorization","success":false}`)
			return
		}
		_, _ = io.WriteString(w, `{"object":"list","data":[{"id":"glm-4.7"},{"id":"glm-5.3"}]}`)
	}))
	t.Cleanup(server.Close)

	if _, err := parseUpstreamModelIDs([]byte(`{"code":1001,"msg":"missing Authorization","success":false}`)); !errors.Is(err, ErrUpstreamModels) {
		t.Fatalf("parseUpstreamModelIDs(error envelope) error = %v, want ErrUpstreamModels", err)
	}

	modelIDs, err := ListUpstreamModels(context.Background(), NewHTTPClient(time.Second), Upstream{
		BaseURL: server.URL + "/api/anthropic",
		APIKey:  "live-secret",
	})
	if err != nil {
		t.Fatalf("ListUpstreamModels() error = %v", err)
	}
	if len(modelIDs) != 2 || modelIDs[0] != "glm-4.7" || modelIDs[1] != "glm-5.3" {
		t.Fatalf("model IDs = %#v", modelIDs)
	}
}

func TestParseUpstreamModelIDsDistinguishesEmptyAndMalformedPages(t *testing.T) {
	modelIDs, err := parseUpstreamModelIDs([]byte(`{"data":[]}`))
	if err != nil || len(modelIDs) != 0 {
		t.Fatalf("parseUpstreamModelIDs(empty) = (%#v, %v), want empty success", modelIDs, err)
	}
	if _, err := parseUpstreamModelIDs([]byte(`{"object":"list"}`)); !errors.Is(err, ErrUpstreamModels) {
		t.Fatalf("parseUpstreamModelIDs(missing data) error = %v, want ErrUpstreamModels", err)
	}
}

func TestMergeModelIDsKeepsExistingOrderAndCaps(t *testing.T) {
	merged := MergeModelIDs([]string{"kimi-k2.5", ""}, []string{"glm-4.7", "kimi-k2.5", "qwen-max"}, 2)
	if len(merged) != 2 || merged[0] != "kimi-k2.5" || merged[1] != "glm-4.7" {
		t.Fatalf("MergeModelIDs() = %#v", merged)
	}
}
