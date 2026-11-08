//go:build integration

package tests

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/net/websocket"
)

func TestEgressPublicSessionHeader(t *testing.T) {
	started := time.Now()
	var calls atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Received-Session", strings.Join(r.Header.Values("X-OMA-Session-ID"), ","))
		w.Header().Set("Received-Method", r.Method)
		w.Header().Set("Received-URI", r.URL.RequestURI())
		w.Header().Set("Received-Auth", r.Header.Get("Authorization"))
		w.Header().Set("Received-Proxy-Auth", r.Header.Get("Proxy-Authorization"))
		w.Header().Set("Received-MCP-Session", r.Header.Get("Mcp-Session-Id"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.Copy(w, r.Body)
	}))
	t.Cleanup(upstream.Close)
	app := newSSRFDisabledTestApp(t, "egress-session-header")
	t.Cleanup(app.close)
	target := upstream.URL + "/mcp?tenant=one"
	agent := createAgent(t, app, `{"model":"claude-opus-4-6","name":"egress-header","mcp_servers":[{"name":"echo","type":"url","url":`+quoteJSON(target)+`}],"tools":[{"type":"mcp_toolset","mcp_server_name":"echo"}]}`)
	t.Cleanup(func() { cleanupAgentRows(t, app.pool, agent.ID) })
	environment := createEnvironment(t, app, `{"name":"egress-header"}`)
	t.Cleanup(func() { cleanupEnvironmentRows(t, app.pool, environment.ID) })
	type sessionIdentity struct {
		publicID, codeID, token string
	}
	identities := make([]sessionIdentity, 2)
	for i := range identities {
		session := createSession(t, app, `{"agent":`+quoteJSON(agent.ID)+`,"environment_id":`+quoteJSON(environment.ID)+`}`)
		t.Cleanup(func() { cleanupSession(t, app, session.ID) })
		codeID := launchLocalCodeSession(t, app, session.ID)
		identities[i] = sessionIdentity{session.ID, codeID, codeSessionIngressToken(t, app, codeID)}
	}
	t.Run("concurrent session forwarding", func(t *testing.T) {
		for i, identity := range identities {
			for _, mode := range []string{"http", "mcp"} {
				for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodDelete} {
					for _, forged := range []bool{false, true} {
						t.Run(strconv.Itoa(i)+"/"+mode+"/"+method+"/forged="+strconv.FormatBool(forged), func(t *testing.T) {
							t.Parallel()
							request := newEgressMCPRequest(t, app, identity.codeID, identity.token, target, method)
							wantAuth := ""
							if mode == "http" {
								request.URL, _ = url.Parse(target)
								request.Host = request.URL.Host
								request.Header.Set("Proxy-Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(identity.codeID+":"+identity.token)))
								wantAuth = "Bearer business-test-key"
								request.Header.Set("Authorization", wantAuth)
							}
							if forged {
								request.Header.Add("x-oma-session-id", identities[1-i].publicID)
								request.Header.Add("X-OMA-Session-ID", "forged")
								request.Header.Set("Connection", "X-OMA-Session-ID")
							}
							var response *http.Response
							if mode == "http" {
								response = sendEgressHTTP(t, app, identity.token, request)
							} else {
								var err error
								response, err = app.client.Do(request)
								if err != nil {
									t.Fatal(err)
								}
							}
							defer response.Body.Close()
							body := readAll(t, response.Body)
							if response.StatusCode != http.StatusOK || string(body) != egressHeaderBody {
								t.Fatalf("status=%d body=%q", response.StatusCode, body)
							}
							for name, want := range map[string]string{
								"Received-Session": identity.publicID, "Received-Method": method,
								"Received-URI": "/mcp?tenant=one", "Received-Auth": wantAuth,
								"Received-Proxy-Auth": "", "Received-MCP-Session": "protocol-session",
							} {
								if got := response.Header.Get(name); got != want {
									t.Errorf("%s=%q want=%q", name, got, want)
								}
							}
						})
					}
				}
			}
		}
	})
	if got := calls.Load(); got != 24 {
		t.Fatalf("upstream calls=%d want=24", got)
	}
	t.Run("tunnel connector headers", testEgressTunnelSessionHeader)
	if !t.Failed() {
		t.Logf("BE_PROOF {\"stage\":\"egress_headers_forwarded\",\"elapsed_ms\":%d}", time.Since(started).Milliseconds())
	}
}

const egressHeaderBody = `{"jsonrpc":"2.0","id":1,"method":"initialize"}`

func newEgressMCPRequest(t *testing.T, app *testApp, codeID, token, target, method string) *http.Request {
	t.Helper()
	endpoint := app.baseURL + "/v2/ccr-sessions/" + codeID + "/mcp?" + url.Values{"mcp_url": {target}}.Encode()
	request, err := http.NewRequestWithContext(t.Context(), method, endpoint, strings.NewReader(egressHeaderBody))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Mcp-Session-Id", "protocol-session")
	return request
}

func sendEgressHTTP(t *testing.T, app *testApp, token string, request *http.Request) *http.Response {
	t.Helper()
	connection := dialCCRV2UpstreamProxy(t, app, token)
	t.Cleanup(func() { _ = connection.Close() })
	if err := connection.SetDeadline(time.Now().Add(10 * time.Second)); err != nil {
		t.Fatal(err)
	}
	var wire bytes.Buffer
	if err := request.WriteProxy(&wire); err != nil {
		t.Fatal(err)
	}
	if err := websocket.Message.Send(connection, encodeCCRV2TestChunk(wire.Bytes())); err != nil {
		t.Fatal(err)
	}
	response, err := http.ReadResponse(bufio.NewReader(&egressChunkReader{t: t, connection: connection}), request)
	if err != nil {
		t.Fatal(err)
	}
	return response
}

type egressChunkReader struct {
	t          *testing.T
	connection *websocket.Conn
	remaining  []byte
}

func (r *egressChunkReader) Read(p []byte) (int, error) {
	for len(r.remaining) == 0 {
		r.remaining = receiveCCRV2TestChunk(r.t, r.connection)
	}
	n := copy(p, r.remaining)
	r.remaining = r.remaining[n:]
	return n, nil
}
