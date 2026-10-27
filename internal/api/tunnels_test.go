package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/codesessions"
	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
	"github.com/superduck-ai/open-managed-agents/internal/filestore"
	"github.com/superduck-ai/open-managed-agents/internal/tunnels"
)

func TestTunnelRouteAssemblyWithOptionalTestDependencies(t *testing.T) {
	credentials, err := codesessions.NewSessionCredentials(config.Config{})
	if err != nil {
		t.Fatal(err)
	}
	filestoreCredentials, err := filestore.NewTokenCredentials(config.Config{})
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name     string
		database *db.DB
		broker   *tunnels.Broker
		status   int
	}{
		{name: "no database", broker: &tunnels.Broker{}, status: http.StatusNotFound},
		{name: "no broker", database: &db.DB{}, status: http.StatusNotFound},
		{name: "configured routes require authentication", database: &db.DB{}, broker: &tunnels.Broker{}, status: http.StatusUnauthorized},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := NewServer(ServerDeps{
				DB: test.database, TunnelBroker: test.broker,
				CodeSessionCredentials: credentials, FilestoreCredentials: filestoreCredentials,
			})
			if (server.tunnels != nil) != (test.database != nil) || (server.consoleTunnels != nil) != (test.database != nil) {
				t.Fatal("Tunnel management must remain available with DB alone")
			}
			for _, path := range []string{
				"/connector/v1/tunnels/tunnel_0123456789abcdef0123456789abcdef",
				"/.well-known/oauth-protected-resource/v1/mcp/tunnel_0123456789abcdef0123456789abcdef",
				"/.well-known/oauth-protected-resource/v1/mcp/tunnel_0123456789abcdef0123456789abcdef/stdio",
			} {
				response := httptest.NewRecorder()
				server.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
				if response.Code != test.status {
					t.Fatalf("%s returned %d, want %d: %s", path, response.Code, test.status, response.Body.String())
				}
			}
		})
	}
}
