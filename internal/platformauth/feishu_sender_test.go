package platformauth

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/config"
)

func TestFeishuFailureLogsStatusAndCode(t *testing.T) {
	for _, status := range []int{http.StatusBadRequest, http.StatusOK} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			sender := NewFeishuSender(config.FeishuConfig{AppID: "cli_test", AppSecret: "secret"})
			sender.client.Transport = feishuTransport(func(r *http.Request) (*http.Response, error) {
				if strings.Contains(r.URL.Path, "/auth/") {
					return feishuTestResponse(200, `{"code":0,"tenant_access_token":"token","expire":7200}`), nil
				}
				return feishuTestResponse(status, `{"code":230013,"msg":"user@example.com secret 123456"}`), nil
			})
			var output bytes.Buffer
			logger := slog.New(slog.NewJSONHandler(&output, nil))
			err := Deliver(t.Context(), logger, Delivery{Channel: "feishu", Send: func(ctx context.Context) error {
				return sender.SendLoginCode(ctx, "user@example.com", "123456")
			}})
			if err == nil {
				t.Fatal("rejected notification was accepted")
			}
			var record struct {
				Channel string `json:"channel"`
				Error   struct {
					HTTPStatus int `json:"http_status"`
					APICode    int `json:"api_code"`
				} `json:"error"`
			}
			if err := json.Unmarshal(output.Bytes(), &record); err != nil || record.Channel != "feishu" || record.Error.HTTPStatus != status || record.Error.APICode != 230013 {
				t.Fatalf("failure log lost Feishu status or code: %s", output.String())
			}
			for _, sensitive := range []string{"secret", "@", "123456", "token"} {
				if strings.Contains(output.String(), sensitive) {
					t.Fatal("failure log exposed sensitive data")
				}
			}
		})
	}
}

type feishuTransport func(*http.Request) (*http.Response, error)

func (f feishuTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func feishuTestResponse(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}
}

func TestFeishuSendFailures(t *testing.T) {
	for _, tc := range []struct {
		name   string
		token  string
		status int
		body   string
	}{
		{"token rejected", `{"code":10003,"msg":"secret@example.com"}`, 200, ""},
		{"token missing", `{"code":0,"expire":7200}`, 200, ""},
		{"expiry missing", `{"code":0,"tenant_access_token":"token"}`, 200, ""},
		{"HTTP failure", `{"code":0,"tenant_access_token":"token","expire":7200}`, 503, "private response"},
		{"API failure", `{"code":0,"tenant_access_token":"token","expire":7200}`, 200, `{"code":230013,"msg":"secret@example.com"}`},
		{"missing status", `{"code":0,"tenant_access_token":"token","expire":7200}`, 200, `{"data":{"message_id":"message"}}`},
		{"missing message", `{"code":0,"tenant_access_token":"token","expire":7200}`, 200, `{"code":0}`},
		{"invalid JSON", `{"code":0,"tenant_access_token":"token","expire":7200}`, 200, `private response`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sender := NewFeishuSender(config.FeishuConfig{AppID: "cli_test", AppSecret: "secret"})
			sender.client.Transport = feishuTransport(func(r *http.Request) (*http.Response, error) {
				if strings.Contains(r.URL.Path, "/auth/") {
					return feishuTestResponse(200, tc.token), nil
				}
				return feishuTestResponse(tc.status, tc.body), nil
			})
			err := sender.SendLoginCode(t.Context(), "user@example.com", "123456")
			if err == nil {
				t.Fatal("failed submission was accepted")
			}
			for _, sensitive := range []string{"secret", "@", "123456", "private response"} {
				if strings.Contains(err.Error(), sensitive) {
					t.Fatal("delivery error exposed sensitive data")
				}
			}
		})
	}
}

func TestFeishuSendsByEmailAndReusesToken(t *testing.T) {
	sender := NewFeishuSender(config.FeishuConfig{AppID: "cli_test", AppSecret: "app-secret"})
	var tokens, messages atomic.Int32
	sender.client.Transport = feishuTransport(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodPost || r.URL.Host != "open.feishu.cn" {
			t.Error("unexpected Feishu endpoint")
		}
		deadline, ok := r.Context().Deadline()
		if !ok || time.Until(deadline) > 5*time.Second {
			t.Error("Feishu request must have a bounded deadline")
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(r.URL.Path, "/auth/") {
			tokens.Add(1)
			var credentials struct {
				AppID     string `json:"app_id"`
				AppSecret string `json:"app_secret"`
			}
			if err := json.Unmarshal(body, &credentials); err != nil || credentials.AppID != "cli_test" || credentials.AppSecret != "app-secret" || r.Header.Get("Authorization") != "" {
				t.Error("invalid app credentials request")
			}
			return feishuTestResponse(200, `{"code":0,"tenant_access_token":"tenant-token","expire":7200}`), nil
		}
		messages.Add(1)
		var message struct {
			ReceiveID string `json:"receive_id"`
			MsgType   string `json:"msg_type"`
			Content   string `json:"content"`
		}
		if err := json.Unmarshal(body, &message); err != nil || message.ReceiveID != "user@example.com" || message.MsgType != "interactive" || r.URL.Query().Get("receive_id_type") != "email" || r.Header.Get("Authorization") != "Bearer tenant-token" {
			t.Error("message must be addressed to the user's email")
		}
		var card struct {
			Schema string `json:"schema"`
			Config struct {
				WidthMode string `json:"width_mode"`
			} `json:"config"`
			Header struct {
				Title struct {
					Content string `json:"content"`
				} `json:"title"`
			} `json:"header"`
			Body struct {
				Elements []struct {
					Tag       string `json:"tag"`
					Content   string `json:"content"`
					TextSize  string `json:"text_size"`
					TextAlign string `json:"text_align"`
					Text      *struct {
						Content string `json:"content"`
					} `json:"text"`
				} `json:"elements"`
			} `json:"body"`
		}
		if err := json.Unmarshal([]byte(message.Content), &card); err != nil || card.Schema != "2.0" || card.Config.WidthMode != "compact" || card.Header.Title.Content != "登录验证码" {
			t.Error("verification code must use a clearly titled card")
		}
		foundCode, foundExpiry := false, false
		for _, element := range card.Body.Elements {
			if element.Content == "**123456**" {
				foundCode = element.Tag == "markdown" && element.TextSize == "heading-0" && element.TextAlign == "center"
			}
			if element.Text != nil && strings.Contains(element.Text.Content, "10 分钟内有效") && strings.Contains(element.Text.Content, "请勿转发") {
				foundExpiry = true
			}
		}
		if !foundCode || !foundExpiry {
			t.Error("card must emphasize the same code and retain expiration and safety instructions")
		}
		return feishuTestResponse(200, `{"code":0,"data":{"message_id":"message"}}`), nil
	})
	for range 2 {
		if err := sender.SendLoginCode(t.Context(), "user@example.com", "123456"); err != nil {
			t.Fatal(err)
		}
	}
	if tokens.Load() != 1 || messages.Load() != 2 {
		t.Fatalf("token requests = %d, messages = %d", tokens.Load(), messages.Load())
	}
	sender.expiresAt = time.Time{}
	if err := sender.SendLoginCode(t.Context(), "user@example.com", "123456"); err != nil || tokens.Load() != 2 || messages.Load() != 3 {
		t.Fatal("expired access token was not refreshed")
	}
}

func TestFeishuSendsInvitationAsCard(t *testing.T) {
	sender := NewFeishuSender(config.FeishuConfig{AppID: "cli_test", AppSecret: "secret"})
	sent := false
	card := []byte(`{"header":{"title":{"tag":"plain_text","content":"组织邀请"}},"elements":[{"tag":"action","actions":[{"tag":"button","text":{"tag":"plain_text","content":"查看邀请"},"url":"https://oma.example.com/invites","type":"primary"}]}]}`)
	sender.client.Transport = feishuTransport(func(r *http.Request) (*http.Response, error) {
		if strings.Contains(r.URL.Path, "/auth/") {
			return feishuTestResponse(200, `{"code":0,"tenant_access_token":"token","expire":7200}`), nil
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatal(err)
		}
		var message struct {
			ReceiveID string `json:"receive_id"`
			MsgType   string `json:"msg_type"`
			Content   string `json:"content"`
		}
		if err := json.Unmarshal(body, &message); err != nil || message.ReceiveID != "user@example.com" || message.MsgType != "interactive" || r.URL.Query().Get("receive_id_type") != "email" || message.Content != string(card) {
			t.Error("card must be delivered as interactive content to the invited email identity")
		}
		sent = true
		return feishuTestResponse(200, `{"code":0,"data":{"message_id":"card-message"}}`), nil
	})
	if err := sender.SendCard(t.Context(), "user@example.com", card); err != nil || !sent {
		t.Fatalf("send card: %v", err)
	}
}
