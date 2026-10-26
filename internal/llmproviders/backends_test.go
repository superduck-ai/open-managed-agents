package llmproviders

import (
	"errors"
	"net/http"
	"testing"
)

func TestUnsupportedAPIBackends(t *testing.T) {
	for _, backend := range []string{"", "responses", "chat-completions", " messages"} {
		if ValidBackend(backend) {
			t.Fatalf("accepted %q", backend)
		}
	}
	if !errors.Is((Upstream{APIBackend: ChatCompletions}).RequireBackend(Messages), ErrBackendMismatch) {
		t.Fatal("accepted protocol mismatch")
	}
}

func TestBackendHeadersAndEndpoints(t *testing.T) {
	for _, backend := range []string{Messages, ChatCompletions} {
		upstream := Upstream{APIBackend: backend, APIKey: "server-key"}
		headers := http.Header{"Anthropic-Beta": []string{"private"}, "Anthropic-Version": []string{"old"}, "X-Api-Key": []string{"client-key"}}
		upstream.ApplyHeaders(headers)
		if headers.Get("Authorization") != "Bearer server-key" {
			t.Fatal("missing server credential")
		}
		if backend == ChatCompletions && (headers.Get("X-Api-Key") != "" || headers.Get("Anthropic-Version") != "" || headers.Get("Anthropic-Beta") != "") {
			t.Fatalf("leaked headers=%v", headers)
		}
		if backend == Messages && (headers.Get("X-Api-Key") != "server-key" || headers.Get("Anthropic-Version") != anthropicVersion) {
			t.Fatal("missing messages headers")
		}
	}
	if err := (Upstream{}).RequireBackend(Messages); err != nil {
		t.Fatal(err)
	}
	endpoint, err := Endpoint("https://provider.example/prefix/", "/v1/chat/completions", "")
	if err != nil || endpoint != "https://provider.example/prefix/v1/chat/completions" {
		t.Fatalf("endpoint=%s err=%v", endpoint, err)
	}
}
