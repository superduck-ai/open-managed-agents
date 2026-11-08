//go:build integration

package tests

import (
	"context"
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/nats-io/nats.go"
	"github.com/superduck-ai/open-managed-agents/internal/api"
	"github.com/superduck-ai/open-managed-agents/internal/redisclient"
	"github.com/superduck-ai/open-managed-agents/internal/tunnels"
)

func testEgressTunnelSessionHeader(t *testing.T) {
	runEgressTunnelSessionHeader(t, "")
}

func runEgressTunnelSessionHeader(t *testing.T, binary string) {
	app := newTestAppWithStore(t, nil, newFakeStore("egress-tunnel-header"))
	t.Cleanup(app.close)
	startEgressTunnelBroker(t, app)
	client := anthropic.NewClient(option.WithBaseURL(app.baseURL), option.WithAPIKey(defaultTestKey))
	tunnel, err := client.Beta.Tunnels.New(t.Context(), anthropic.BetaTunnelNewParams{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, err := client.Beta.Tunnels.Archive(context.Background(), tunnel.ID, anthropic.BetaTunnelArchiveParams{})
		if err != nil {
			t.Error(err)
		}
	})
	token, err := client.Beta.Tunnels.RevealToken(t.Context(), tunnel.ID, anthropic.BetaTunnelRevealTokenParams{})
	if err != nil {
		t.Fatal(err)
	}
	target := app.cfg.Tunnel.PublicBaseURL + "/v1/mcp/" + tunnel.ID
	agent := createAgent(t, app, `{"model":"claude-opus-4-6","name":"tunnel-header","mcp_servers":[{"name":"local","type":"url","url":`+quoteJSON(target)+`}],"tools":[{"type":"mcp_toolset","mcp_server_name":"local"}]}`)
	t.Cleanup(func() { cleanupAgentRows(t, app.pool, agent.ID) })
	environment := createEnvironment(t, app, `{"name":"tunnel-header"}`)
	t.Cleanup(func() { cleanupEnvironmentRows(t, app.pool, environment.ID) })
	session := createSession(t, app, `{"agent":`+quoteJSON(agent.ID)+`,"environment_id":`+quoteJSON(environment.ID)+`}`)
	t.Cleanup(func() { cleanupSession(t, app, session.ID) })
	codeID := launchLocalCodeSession(t, app, session.ID)
	ingressToken := codeSessionIngressToken(t, app, codeID)
	exchange := exchangeEgressTunnelRequest
	if binary != "" {
		exchange = startEgressConnectorReceiver(t, app, binary, tunnel.ID, token.TunnelToken)
	}
	for _, mode := range []string{"direct discovery", "forged", "connection", "absent", "jsonrpc", "termination"} {
		t.Run(mode, func(t *testing.T) {
			method, path, body := http.MethodGet, "/.well-known/oauth-protected-resource/v2/ccr-sessions/"+codeID+"/mcp/local", ""
			wantSession, commandType := session.ID, tunnels.CommandTypeOAuthDiscovery
			switch mode {
			case "direct discovery":
				path = "/.well-known/oauth-protected-resource/v1/mcp/" + tunnel.ID
				wantSession = ""
			case "jsonrpc", "termination":
				path = "/v2/ccr-sessions/" + codeID + "/mcp/local"
				method, body, commandType = http.MethodPost, egressHeaderBody, tunnels.CommandTypeJSONRPC
				if mode == "termination" {
					method, body, commandType = http.MethodDelete, "", tunnels.CommandTypeSessionTermination
				}
			}
			request, err := http.NewRequestWithContext(t.Context(), method, app.baseURL+path, strings.NewReader(body))
			if err != nil {
				t.Fatal(err)
			}
			request.Header.Set("Authorization", "Bearer "+ingressToken)
			request.Header.Set("X-Api-Key", ingressToken)
			if mode == "direct discovery" {
				request.Header.Set("X-Api-Key", defaultTestKey)
			}
			request.Header.Set("Cookie", "private=value")
			request.Header.Set("X-Untrusted", "discard-in-discovery")
			request.Header.Set("Mcp-Session-Id", "protocol-session")
			request.Header.Set("Content-Type", "application/json")
			if mode != "absent" {
				request.Header.Add("X-OMA-Session-ID", "forged")
				request.Header.Add("x-oma-session-id", "another-session")
			}
			if mode == "connection" {
				request.Header.Set("Connection", "X-OMA-Session-ID")
			}
			command := exchange(t, app, request, tunnel.ID, token.TunnelToken)
			if command.CommandType != commandType || command.Headers.Get("X-OMA-Session-ID") != wantSession {
				t.Fatalf("command=%s session=%q want=%s/%q", command.CommandType, command.Headers.Get("X-OMA-Session-ID"), commandType, wantSession)
			}
			if wantSession != "" && len(command.Headers.Values("X-OMA-Session-ID")) != 1 {
				t.Fatal("connector received multiple Session IDs")
			}
			for _, name := range []string{"Authorization", "X-Api-Key", "Cookie", "Connection", "Proxy-Authorization"} {
				if command.Headers.Get(name) != "" {
					t.Errorf("connector received forbidden header %s", name)
				}
			}
			if commandType == tunnels.CommandTypeOAuthDiscovery {
				wantCount := 1
				if wantSession == "" {
					wantCount = 0
				}
				if binary == "" && len(command.Headers) != wantCount {
					t.Errorf("discovery header count=%d want=%d", len(command.Headers), wantCount)
				}
			} else if command.Headers.Get("Mcp-Session-Id") != "protocol-session" || string(command.JSONRPC) != body {
				t.Fatal("MCP protocol session or body changed")
			}
		})
	}
}

func startEgressTunnelBroker(t *testing.T, app *testApp) {
	t.Helper()
	connection, err := nats.Connect(app.cfg.NATS.URL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(connection.Close)
	bindings, err := redisclient.Open(t.Context(), app.cfg.Redis.URL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = bindings.Close() })
	broker, err := tunnels.NewBroker(t.Context(), connection, app.cfg.Tunnel, nil, tunnels.NewRequestBindings(bindings, app.cfg.Tunnel.RequestTimeout+app.cfg.Tunnel.TombstoneTTL))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(broker.Close)
	handler := api.NewServer(api.ServerDeps{
		Config: app.cfg, DB: app.db, ObjectStore: app.store, Deployments: app.deployments,
		Logger:                 slog.New(slog.NewTextHandler(io.Discard, nil)),
		CodeSessionCredentials: app.credentials, FilestoreCredentials: app.filestoreCredentials,
		VaultSecrets: app.vaultSecrets, TunnelBroker: broker, WorkerEventBroker: app.workerEvents,
		SandboxTimeoutExtender: app.sandboxTimeouts,
	})
	app.server.Close()
	app.server = httptest.NewServer(handler)
	app.baseURL, app.client = app.server.URL, app.server.Client()
}

type egressTunnelCommand struct {
	RequestID   string              `json:"request_id"`
	CommandType tunnels.CommandType `json:"command_type"`
	Channel     string              `json:"channel"`
	Headers     http.Header         `json:"headers"`
	JSONRPC     json.RawMessage     `json:"jsonrpc"`
}

func exchangeEgressTunnelRequest(t *testing.T, app *testApp, request *http.Request, tunnelID, token string) egressTunnelCommand {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		response, err := app.client.Do(request.Clone(ctx))
		if err == nil {
			defer response.Body.Close()
			_, err = io.Copy(io.Discard, response.Body)
			if response.StatusCode != http.StatusOK {
				t.Errorf("ingress status=%d", response.StatusCode)
			}
		}
		done <- err
	}()
	endpoint := app.baseURL + "/connector/v1/tunnels/" + tunnelID
	poll, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"/poll", nil)
	if err != nil {
		t.Fatal(err)
	}
	poll.Header.Set("Authorization", "Bearer "+token)
	poll.Header.Set("X-Tunnel-Client-Instance-Id", "egress-header-test")
	response, err := app.client.Do(poll)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var envelope struct {
		Commands []egressTunnelCommand `json:"commands"`
	}
	if err := jsonv2.UnmarshalRead(response.Body, &envelope); err != nil || response.StatusCode != http.StatusOK || len(envelope.Commands) != 1 {
		t.Fatalf("connector poll status=%d commands=%d error=%v", response.StatusCode, len(envelope.Commands), err)
	}
	command := envelope.Commands[0]
	responseType, payload := tunnels.ResponseTypeJSONRPC, `{"jsonrpc":"2.0","id":1,"result":{}}`
	if command.CommandType == tunnels.CommandTypeOAuthDiscovery {
		responseType, payload = tunnels.ResponseTypeOAuth, `{"resource":"https://private.example/mcp","authorization_servers":["https://auth.example"]}`
	} else if command.CommandType == tunnels.CommandTypeSessionTermination {
		responseType, payload = tunnels.ResponseTypeSessionTermination, "null"
	}
	result := tunnels.TunnelResponse{RequestID: command.RequestID, Channel: command.Channel, ResponseType: responseType, ResponseCode: http.StatusOK, JSONResponse: json.RawMessage(payload)}
	if command.CommandType == tunnels.CommandTypeSessionTermination {
		result.JSONResponse = nil
	}
	encoded, err := jsonv2.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	reply, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint+"/response", strings.NewReader(string(encoded)))
	if err != nil {
		t.Fatal(err)
	}
	reply.Header.Set("Authorization", "Bearer "+token)
	reply.Header.Set("Content-Type", "application/json")
	reply.Header.Set("X-Tunnel-Shard-Token", command.RequestID)
	ack, err := app.client.Do(reply)
	if err != nil {
		t.Fatal(err)
	}
	defer ack.Body.Close()
	if ack.StatusCode != http.StatusOK {
		t.Fatalf("connector response status=%d", ack.StatusCode)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	return command
}
