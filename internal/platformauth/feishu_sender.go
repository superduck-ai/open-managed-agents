package platformauth

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/config"
)

type FeishuSender struct {
	cfg       config.FeishuConfig
	client    *http.Client
	mu        sync.Mutex
	token     string
	expiresAt time.Time
}

type feishuCredentials struct {
	AppID     string `json:"app_id"`
	AppSecret string `json:"app_secret"`
}

type feishuMessage struct {
	ReceiveID string `json:"receive_id"`
	MsgType   string `json:"msg_type"`
	Content   string `json:"content"`
}

type feishuResponse struct {
	Code              *int   `json:"code"`
	TenantAccessToken string `json:"tenant_access_token"`
	Expire            int    `json:"expire"`
	Data              struct {
		MessageID string `json:"message_id"`
	} `json:"data"`
}

func NewFeishuSender(cfg config.FeishuConfig) *FeishuSender {
	return &FeishuSender{cfg: cfg, client: &http.Client{
		Timeout: 5 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}}
}

func (s *FeishuSender) SendLoginCode(ctx context.Context, recipient, code string) error {
	card, err := feishuLoginCodeCard(code)
	if err != nil {
		return err
	}
	return s.SendCard(ctx, recipient, card)
}

func (s *FeishuSender) SendCard(ctx context.Context, recipient string, card []byte) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	token, err := s.accessToken(ctx)
	if err != nil {
		return err
	}
	message := feishuMessage{ReceiveID: recipient, MsgType: "interactive", Content: string(card)}
	result, err := s.post(ctx, "/im/v1/messages?receive_id_type=email", token, message)
	if err != nil {
		return err
	}
	if result.Data.MessageID == "" {
		return errNotificationDelivery
	}
	return nil
}

func (s *FeishuSender) accessToken(ctx context.Context) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if time.Now().Before(s.expiresAt) {
		return s.token, nil
	}
	credentials := feishuCredentials{AppID: s.cfg.AppID, AppSecret: s.cfg.AppSecret}
	result, err := s.post(ctx, "/auth/v3/tenant_access_token/internal", "", credentials)
	if err != nil {
		return "", err
	}
	if result.TenantAccessToken == "" || result.Expire <= 0 {
		return "", errNotificationDelivery
	}
	s.token = result.TenantAccessToken
	s.expiresAt = time.Now().Add(time.Duration(result.Expire)*time.Second - time.Minute)
	return s.token, nil
}

func (s *FeishuSender) post(ctx context.Context, path, token string, payload any) (feishuResponse, error) {
	var result feishuResponse
	body, err := json.Marshal(payload)
	if err != nil {
		return result, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://open.feishu.cn/open-apis"+path, bytes.NewReader(body))
	if err != nil {
		return result, err
	}
	request.Header.Set("Content-Type", "application/json; charset=utf-8")
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response, err := s.client.Do(request)
	if err != nil {
		return result, errNotificationDelivery
	}
	defer response.Body.Close()
	body, err = io.ReadAll(io.LimitReader(response.Body, 64*1024))
	if err != nil || json.Unmarshal(body, &result) != nil {
		return result, &feishuDeliveryError{httpStatus: response.StatusCode}
	}
	if response.StatusCode != http.StatusOK || result.Code == nil || *result.Code != 0 {
		return result, &feishuDeliveryError{httpStatus: response.StatusCode, apiCode: result.Code}
	}
	return result, nil
}
