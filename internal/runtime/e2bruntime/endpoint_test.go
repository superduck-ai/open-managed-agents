package e2bruntime

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/runtime/sandboxruntime"
)

func TestLocalServiceEndpointConnectsBeforeLookup(t *testing.T) {
	for _, scenario := range []struct {
		name          string
		connectStatus int
		lookupStatus  int
		missing       bool
		wantError     bool
	}{
		{"missing sandbox", 404, 200, true, true},
		{"provider failure", 503, 200, false, true},
		{"removed after connect", 200, 404, true, true},
		{"lookup failure", 200, 503, false, true},
		{"connected sandbox", 200, 200, false, false},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			connected := false
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case "/sandboxes/local/connect":
					connected = true
					w.WriteHeader(scenario.connectStatus)
					_, _ = fmt.Fprint(w, `{"sandboxID":"local","envdURL":"http://127.0.0.1:1"}`)
				case "/sandboxes/local/ports/3001":
					if !connected {
						t.Error("port lookup preceded sandbox connect")
					}
					w.WriteHeader(scenario.lookupStatus)
					_, _ = fmt.Fprint(w, `{"containerPort":3001,"hostPort":40000,"url":"http://127.0.0.1:40000"}`)
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			provider := NewProvider(config.E2BConfig{LocalPortLookup: true, APIURL: server.URL, SandboxURL: server.URL, APIKey: "e2b_0000000000000000000000000000000000000000", RequestTimeout: time.Second})
			endpoint, err := provider.ServiceEndpoint(t.Context(), "local", 3001, "/mcp")
			if (err != nil) != scenario.wantError || errors.Is(err, sandboxruntime.ErrSandboxNotFound) != scenario.missing {
				t.Fatalf("endpoint error = %v, wantError=%t, missing=%t", err, scenario.wantError, scenario.missing)
			}
			if !scenario.wantError && endpoint != "http://127.0.0.1:40000/mcp" {
				t.Fatalf("endpoint = %q", endpoint)
			}
		})
	}
}
