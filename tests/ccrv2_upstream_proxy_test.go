package tests

import (
	"context"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/auth"
	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/ids"

	"golang.org/x/net/websocket"
)

type capturedRuntimeModelRequest struct {
	path          string
	apiKey        string
	authorization string
	version       string
	body          string
	contentLength int64
}

type repeatedRuntimeModelByteReader struct{}

func (repeatedRuntimeModelByteReader) Read(buffer []byte) (int, error) {
	clear(buffer)
	return len(buffer), nil
}

func TestCCRV2RuntimeEndpoints(t *testing.T) {
	upstreamRequests := make(chan capturedRuntimeModelRequest, 1)
	upstreamStartedReading := make(chan struct{}, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body []byte
		if r.Header.Get("X-Test-Streaming") == "true" {
			firstByte := make([]byte, 1)
			if _, err := io.ReadFull(r.Body, firstByte); err == nil {
				upstreamStartedReading <- struct{}{}
			}
			rest, _ := io.ReadAll(r.Body)
			body = append(body, firstByte...)
			body = append(body, rest...)
		} else {
			body, _ = io.ReadAll(r.Body)
		}
		upstreamRequests <- capturedRuntimeModelRequest{
			path:          r.URL.Path,
			apiKey:        r.Header.Get("X-Api-Key"),
			authorization: r.Header.Get("Authorization"),
			version:       r.Header.Get("anthropic-version"),
			body:          string(body),
			contentLength: r.ContentLength,
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Upstream-Test", "reached")
		_, _ = w.Write([]byte(`{"id":"msg_test","type":"message"}`))
	}))
	defer upstream.Close()

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	// 真实上游配置只存在于 API server；测试通过捕获请求验证 sandbox token 会被替换。
	cfg.AnthropicUpstream.BaseURL = upstream.URL + "/coding"
	cfg.AnthropicUpstream.APIKey = "upstream-secret"
	app := newTestAppWithStore(t, &cfg, newFakeStore("ccrv2-upstream-proxy-bucket"))
	defer app.close()

	agent := createAgent(t, app, `{"model":"claude-opus-4-6","name":"ccrv2-upstream-proxy-agent"}`)
	defer cleanupAgentRows(t, app.db, agent.ID)
	environment := createEnvironment(t, app, `{"name":"ccrv2-upstream-proxy-env"}`)
	defer cleanupEnvironmentRows(t, app.db, environment.ID)
	session := createSession(t, app, `{"agent":`+quoteJSON(agent.ID)+`,"environment_id":`+quoteJSON(environment.ID)+`}`)
	codeSessionID := launchLocalCodeSession(t, app, session.ID)
	messagesToken, err := ids.New("sk-ant-oat01-ccrv2-")
	if err != nil {
		t.Fatalf("generate code session Messages token: %v", err)
	}
	if _, err := app.db.Pool.Exec(context.Background(), `
		update code_sessions
		set oauth_access_token_hash = $2
		where external_id = $1
	`, codeSessionID, auth.HashAPIKey(messagesToken)); err != nil {
		t.Fatalf("set code session Messages token: %v", err)
	}
	sessionIngressToken := codeSessionIngressToken(t, app, codeSessionID)
	if !strings.HasPrefix(sessionIngressToken, "sk-ant-si-") {
		t.Fatalf("unexpected session ingress credential: %q", sessionIngressToken)
	}

	t.Run("failure model proxy requires code session token", func(t *testing.T) {
		response := postRuntimeModelRequest(t, app, "", "")
		assertError(t, response, http.StatusUnauthorized, "authentication_error")

		response = postRuntimeModelRequest(t, app, "invalid-code-session", "")
		assertError(t, response, http.StatusUnauthorized, "authentication_error")
	})

	t.Run("failure websocket requires code session token", func(t *testing.T) {
		request, err := http.NewRequest(http.MethodGet, app.baseURL+"/v1/code/upstreamproxy/ws", nil)
		if err != nil {
			t.Fatalf("new websocket probe request: %v", err)
		}
		response, err := app.client.Do(request)
		if err != nil {
			t.Fatalf("websocket probe request: %v", err)
		}
		assertError(t, response, http.StatusUnauthorized, "authentication_error")
	})

	t.Run("failure websocket blocks private CONNECT targets", func(t *testing.T) {
		// 默认配置必须拒绝 loopback，即使 WebSocket Bearer 和 CONNECT Basic 都合法。
		connection := dialCCRV2UpstreamProxy(t, app, sessionIngressToken)
		defer connection.Close()
		authorization := base64.StdEncoding.EncodeToString([]byte(codeSessionID + ":" + sessionIngressToken))
		connectHead := "CONNECT 127.0.0.1:443 HTTP/1.1\r\nProxy-Authorization: Basic " + authorization + "\r\n\r\n"
		if err := websocket.Message.Send(connection, encodeCCRV2TestChunk([]byte(connectHead))); err != nil {
			t.Fatalf("send private CONNECT request: %v", err)
		}
		status := receiveCCRV2TestChunk(t, connection)
		if !strings.HasPrefix(string(status), "HTTP/1.1 403 Forbidden") {
			t.Fatalf("private CONNECT status = %q", status)
		}
	})

	t.Run("failure websocket requires matching Basic token", func(t *testing.T) {
		connection := dialCCRV2UpstreamProxy(t, app, sessionIngressToken)
		defer connection.Close()
		authorization := base64.StdEncoding.EncodeToString([]byte(codeSessionID + ":sk-ant-si-other"))
		connectHead := "CONNECT 1.1.1.1:443 HTTP/1.1\r\nProxy-Authorization: Basic " + authorization + "\r\n\r\n"
		if err := websocket.Message.Send(connection, encodeCCRV2TestChunk([]byte(connectHead))); err != nil {
			t.Fatalf("send mismatched CONNECT request: %v", err)
		}
		status := receiveCCRV2TestChunk(t, connection)
		if !strings.HasPrefix(string(status), "HTTP/1.1 407 Proxy Authentication Required") {
			t.Fatalf("mismatched CONNECT status = %q", status)
		}
	})
	registerCodeSessionWorker(t, app, codeSessionID)

	t.Run("failure model proxy rejects declared oversized body", func(t *testing.T) {
		const declaredSize = (32 << 20) + 1
		body := io.LimitReader(repeatedRuntimeModelByteReader{}, declaredSize)
		request, err := http.NewRequest(http.MethodPost, app.baseURL+"/v1/messages", body)
		if err != nil {
			t.Fatalf("new oversized model request: %v", err)
		}
		request.ContentLength = declaredSize
		request.Header.Set("X-Api-Key", messagesToken)
		response, err := app.client.Do(request)
		if err != nil {
			t.Fatalf("oversized model request: %v", err)
		}
		assertError(t, response, http.StatusRequestEntityTooLarge, "request_too_large")
	})

	t.Run("success CA certificate is publicly downloadable", func(t *testing.T) {
		response, err := app.client.Get(app.baseURL + "/v1/code/upstreamproxy/ca-cert")
		if err != nil {
			t.Fatalf("download CA certificate: %v", err)
		}
		defer response.Body.Close()
		if response.StatusCode != http.StatusOK {
			t.Fatalf("CA status = %d, want 200: %s", response.StatusCode, readAll(t, response.Body))
		}
		certificatePEM, err := io.ReadAll(response.Body)
		if err != nil {
			t.Fatalf("read CA certificate: %v", err)
		}
		block, rest := pem.Decode(certificatePEM)
		if block == nil || len(rest) != 0 {
			t.Fatalf("invalid CA PEM: block=%v rest=%q", block, rest)
		}
		certificate, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			t.Fatalf("parse CA certificate: %v", err)
		}
		if !certificate.IsCA {
			t.Fatal("downloaded certificate is not a CA")
		}
	})

	t.Run("success model proxy streams request body", func(t *testing.T) {
		firstChunk := strings.Repeat("x", 64<<10)
		secondChunk := "tail"
		bodyReader, bodyWriter := io.Pipe()
		writeResult := make(chan error, 1)
		go func() {
			if _, err := bodyWriter.Write([]byte(firstChunk)); err != nil {
				writeResult <- err
				return
			}
			select {
			case <-upstreamStartedReading:
				_, err := bodyWriter.Write([]byte(secondChunk))
				if closeErr := bodyWriter.Close(); err == nil {
					err = closeErr
				}
				writeResult <- err
			case <-time.After(3 * time.Second):
				err := errors.New("upstream did not start reading streamed request body")
				_ = bodyWriter.CloseWithError(err)
				writeResult <- err
			}
		}()

		request, err := http.NewRequest(http.MethodPost, app.baseURL+"/v1/messages", bodyReader)
		if err != nil {
			t.Fatalf("new streaming model request: %v", err)
		}
		request.ContentLength = int64(len(firstChunk) + len(secondChunk))
		request.Header.Set("X-Api-Key", messagesToken)
		request.Header.Set("X-Test-Streaming", "true")
		response, err := app.client.Do(request)
		if err != nil {
			t.Fatalf("streaming model request: %v", err)
		}
		defer response.Body.Close()
		if err := <-writeResult; err != nil {
			t.Fatal(err)
		}
		if response.StatusCode != http.StatusOK {
			t.Fatalf("streaming model proxy status = %d, want 200: %s", response.StatusCode, readAll(t, response.Body))
		}
		captured := <-upstreamRequests
		if captured.body != firstChunk+secondChunk || captured.contentLength != request.ContentLength {
			t.Fatalf("unexpected streamed request: body bytes=%d content-length=%d", len(captured.body), captured.contentLength)
		}
	})

	t.Run("success model proxy keeps upstream secret server-side", func(t *testing.T) {
		body := `{"model":"claude-opus-4-6","max_tokens":16,"messages":[{"role":"user","content":"hello"}]}`
		response := postRuntimeModelRequest(t, app, messagesToken, body)
		defer response.Body.Close()
		if response.StatusCode != http.StatusOK {
			t.Fatalf("model proxy status = %d, want 200: %s", response.StatusCode, readAll(t, response.Body))
		}
		if response.Header.Get("X-Upstream-Test") != "reached" {
			t.Fatalf("missing upstream response header: %#v", response.Header)
		}
		captured := <-upstreamRequests
		if captured.path != "/coding/v1/messages" || captured.apiKey != "upstream-secret" {
			t.Fatalf("unexpected upstream target/auth: %+v", captured)
		}
		if captured.authorization != "" || captured.version != "2023-06-01" || captured.body != body || captured.contentLength != int64(len(body)) {
			t.Fatalf("unexpected forwarded request: %+v", captured)
		}
	})
}

func postRuntimeModelRequest(t *testing.T, app *testApp, messagesToken string, body string) *http.Response {
	t.Helper()
	request, err := http.NewRequest(http.MethodPost, app.baseURL+"/v1/messages", strings.NewReader(body))
	if err != nil {
		t.Fatalf("new runtime model request: %v", err)
	}
	if messagesToken != "" {
		request.Header.Set("X-Api-Key", messagesToken)
		request.Header.Set("Authorization", "Bearer "+messagesToken)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("anthropic-version", "2023-06-01")
	response, err := app.client.Do(request)
	if err != nil {
		t.Fatalf("runtime model request: %v", err)
	}
	return response
}

func dialCCRV2UpstreamProxy(t *testing.T, app *testApp, sessionIngressToken string) *websocket.Conn {
	t.Helper()
	location := "ws" + strings.TrimPrefix(app.baseURL, "http") + "/v1/code/upstreamproxy/ws"
	config, err := websocket.NewConfig(location, app.baseURL)
	if err != nil {
		t.Fatalf("new upstream proxy websocket config: %v", err)
	}
	config.Header.Set("Authorization", "Bearer "+sessionIngressToken)
	config.Header.Set("Content-Type", "application/proto")
	connection, err := websocket.DialConfig(config)
	if err != nil {
		t.Fatalf("dial upstream proxy websocket: %v", err)
	}
	return connection
}

func encodeCCRV2TestChunk(data []byte) []byte {
	length := len(data)
	encoded := []byte{0x0a}
	for length >= 0x80 {
		encoded = append(encoded, byte(length)|0x80)
		length >>= 7
	}
	encoded = append(encoded, byte(length))
	return append(encoded, data...)
}

func receiveCCRV2TestChunk(t *testing.T, connection *websocket.Conn) []byte {
	t.Helper()
	var encoded []byte
	if err := websocket.Message.Receive(connection, &encoded); err != nil {
		t.Fatalf("receive upstream proxy chunk: %v", err)
	}
	if len(encoded) < 2 || encoded[0] != 0x0a {
		t.Fatalf("invalid upstream proxy chunk: %v", encoded)
	}
	length := 0
	shift := 0
	index := 1
	for ; index < len(encoded); index++ {
		value := encoded[index]
		length |= int(value&0x7f) << shift
		if value&0x80 == 0 {
			index++
			break
		}
		shift += 7
	}
	if index+length != len(encoded) {
		t.Fatalf("invalid upstream proxy chunk length: %v", encoded)
	}
	return encoded[index:]
}
