//go:build e2e

package tests

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/tunnels"
	"net/http"
	"testing"
	"time"
)

func TestTunnelProbePaginationThroughNATS(t *testing.T) {
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	cfg.Database.URL = managedTunnelDatabase(t, cfg.Database.URL)
	cfg.Tunnel.PublicBaseURL = "https://oma.example"
	app := newTestAppWithStore(t, &cfg, newFakeStore("tunnel-probe-pages"))
	t.Cleanup(app.close)
	broker, err := tunnels.NewBroker(t.Context(), managedTunnelNATS(t), cfg.Tunnel)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(broker.Close)
	client := anthropic.NewClient(option.WithBaseURL(app.baseURL), option.WithAPIKey(defaultTestKey))
	ids := getDefaultDBIDs(t, app.pool)
	for _, failSecond := range []bool{true, false} {
		t.Run(fmt.Sprintf("second_page_error=%v", failSecond), func(t *testing.T) {
			created, err := client.Beta.Tunnels.New(t.Context(), anthropic.BetaTunnelNewParams{})
			if err != nil {
				t.Fatal(err)
			}
			record, err := app.db.GetMCPTunnel(t.Context(), ids.OrganizationUUID, ids.WorkspaceUUID, created.ID)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
			defer cancel()
			channels := []tunnels.ChannelDeclaration{{Name: "main"}}
			if err := broker.RegisterConnector(ctx, record.UUID, "pages", 1, channels); err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() { done <- respondToTunnelProbePages(ctx, broker, record.UUID, channels, failSecond) }()
			result, recognized, err := tunnels.NewService(cfg.Tunnel, app.db, app.vaultSecrets, broker).ProbeTarget(ctx, tunnels.ConsoleScope{OrganizationUUID: ids.OrganizationUUID, WorkspaceUUID: ids.WorkspaceUUID}, cfg.Tunnel.PublicBaseURL+"/v1/mcp/"+created.ID)
			t.Logf("probe returned tools=%d error=%v", len(result.Tools), err)
			if !recognized {
				t.Fatal("not recognized")
			}
			if failSecond {
				if err == nil || len(result.Tools) != 0 {
					t.Fatalf("partial result accepted: %v %v", result, err)
				}
			} else if err != nil || len(result.Tools) != 2 || result.Tools[1].Name != "second" {
				t.Fatalf("paginated result: %v %v", result, err)
			}
			select {
			case err := <-done:
				if err != nil {
					t.Fatal(err)
				}
			case <-ctx.Done():
				t.Fatal("probe did not clean up its session")
			}
		})
	}
}

func respondToTunnelProbePages(ctx context.Context, broker *tunnels.Broker, tunnelUUID string, channels []tunnels.ChannelDeclaration, failSecond bool) error {
	pages := 0
	for ctx.Err() == nil {
		commands, err := broker.Poll(ctx, tunnelUUID, "pages", 1, channels, 25, time.Second)
		if err != nil {
			return err
		}
		for _, command := range commands {
			response := tunnels.TunnelResponse{RequestID: command.RequestID, Channel: "main", ResponseCode: http.StatusOK, ResponseType: tunnels.ResponseTypeJSONRPC}
			finished := command.CommandType == tunnels.CommandTypeSessionTermination
			if finished {
				if pages != 2 || command.Headers.Get("Mcp-Session-Id") != "private-pages" {
					return fmt.Errorf("cleanup after %d pages has invalid session", pages)
				}
				response.ResponseType = tunnels.ResponseTypeSessionTermination
			} else {
				var rpc struct {
					ID     json.RawMessage `json:"id"`
					Method string          `json:"method"`
					Params struct {
						Cursor string `json:"cursor"`
					} `json:"params"`
				}
				if err := json.Unmarshal(command.JSONRPC, &rpc); err != nil {
					return err
				}
				result := map[string]any{}
				envelope := map[string]any{"jsonrpc": "2.0", "id": rpc.ID, "result": result}
				switch rpc.Method {
				case "initialize":
					result["protocolVersion"] = "2025-06-18"
					result["serverInfo"] = map[string]string{"name": "pages", "version": "1"}
					response.ResponseHeaders = http.Header{"Mcp-Session-Id": {"private-pages"}}
				case "notifications/initialized":
					response.ResponseType = tunnels.ResponseTypeNotifyAck
				case "tools/list":
					pages++
					if command.Headers.Get("Mcp-Session-Id") != "private-pages" {
						return fmt.Errorf("page %d lost session", pages)
					}
					if pages == 1 {
						if rpc.Params.Cursor != "" {
							return fmt.Errorf("unexpected first cursor")
						}
						result["tools"] = []map[string]string{{"name": "first"}}
						result["nextCursor"] = "opaque +/== "
					} else {
						if pages != 2 || rpc.Params.Cursor != "opaque +/== " {
							return fmt.Errorf("invalid next cursor")
						}
						result["tools"] = []map[string]string{{"name": "second"}}
						if failSecond {
							delete(envelope, "result")
							envelope["error"] = map[string]any{"code": -32603, "message": "page unavailable"}
						}
					}
				default:
					return fmt.Errorf("unexpected method %s", rpc.Method)
				}
				if response.ResponseType == tunnels.ResponseTypeJSONRPC {
					response.JSONResponse, err = json.Marshal(envelope)
					if err != nil {
						return err
					}
				}
			}
			if err := broker.SubmitResponse(ctx, tunnelUUID, "pages", 1, command.ShardToken, response); err != nil {
				return err
			}
			if finished {
				return nil
			}
		}
	}
	return ctx.Err()
}
