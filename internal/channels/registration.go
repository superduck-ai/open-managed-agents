package channels

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	registrationFeishuHost = "accounts.feishu.cn"
	registrationLarkHost   = "accounts.larksuite.com"
	registrationTimeout    = 10 * time.Second
	registrationNameLimit  = 40
)

type registrationChallenge struct {
	DeviceCode string
	URL        string
	ExpiresIn  time.Duration
	Interval   time.Duration
}

type registrationPoll struct {
	Status    string
	Domain    string
	AppID     string
	AppSecret string
}

type registrationClient struct {
	client  *http.Client
	baseURL map[string]string
}

func newRegistrationClient() *registrationClient {
	return &registrationClient{client: &http.Client{Timeout: registrationTimeout}}
}

func (c *registrationClient) begin(ctx context.Context, name string) (registrationChallenge, error) {
	trimmed := strings.TrimSpace(name)
	response, err := c.post(ctx, "feishu", map[string]string{
		"action":            "begin",
		"archetype":         "PersonalAgent",
		"auth_method":       "client_secret",
		"request_user_info": "open_id",
	})
	if err != nil {
		return registrationChallenge{}, err
	}
	deviceCode, _ := response["device_code"].(string)
	verification, _ := response["verification_uri_complete"].(string)
	if deviceCode == "" || verification == "" {
		return registrationChallenge{}, errors.New("飞书注册响应缺少设备码")
	}
	target, err := url.Parse(verification)
	if err != nil || target.Scheme != "https" || target.User != nil {
		return registrationChallenge{}, errors.New("飞书注册确认地址无效")
	}
	if target.Hostname() != "feishu.cn" && !strings.HasSuffix(target.Hostname(), ".feishu.cn") {
		return registrationChallenge{}, errors.New("飞书注册确认地址无效")
	}
	query := target.Query()
	query.Set("from", "sdk")
	query.Set("source", "open-managed-agents")
	query.Set("tp", "sdk")
	if trimmed != "" {
		query.Set("name", trimmed)
	}
	target.RawQuery = query.Encode()
	expiresIn, err := registrationInt(response["expires_in"], 600, 86400)
	if err != nil {
		return registrationChallenge{}, err
	}
	interval, err := registrationInt(response["interval"], 5, 3600)
	if err != nil {
		return registrationChallenge{}, err
	}
	return registrationChallenge{
		DeviceCode: deviceCode,
		URL:        target.String(),
		ExpiresIn:  time.Duration(expiresIn) * time.Second,
		Interval:   time.Duration(interval) * time.Second,
	}, nil
}

func (c *registrationClient) poll(ctx context.Context, domain, deviceCode string) (registrationPoll, error) {
	if deviceCode == "" {
		return registrationPoll{}, errors.New("飞书注册设备码缺失")
	}
	response, err := c.post(ctx, domain, map[string]string{"action": "poll", "device_code": deviceCode})
	if err != nil {
		return registrationPoll{}, err
	}
	if user, ok := response["user_info"].(map[string]any); ok {
		if brand, _ := user["tenant_brand"].(string); brand == "lark" && domain != "lark" {
			return registrationPoll{Status: "domain_switched", Domain: "lark"}, nil
		}
	}
	clientID, _ := response["client_id"].(string)
	clientSecret, _ := response["client_secret"].(string)
	if clientID != "" && clientSecret != "" {
		return registrationPoll{Status: "authorized", AppID: clientID, AppSecret: clientSecret}, nil
	}
	switch response["error"] {
	case "authorization_pending":
		return registrationPoll{Status: "pending"}, nil
	case "slow_down":
		return registrationPoll{Status: "slow_down"}, nil
	case "access_denied":
		return registrationPoll{Status: "denied"}, nil
	case "expired_token":
		return registrationPoll{Status: "expired"}, nil
	default:
		return registrationPoll{}, errors.New("飞书注册轮询响应无法确认")
	}
}

func (c *registrationClient) endpoint(domain string) string {
	if base, ok := c.baseURL[domain]; ok {
		return base + "/oauth/v1/app/registration"
	}
	host := registrationFeishuHost
	if domain == "lark" {
		host = registrationLarkHost
	}
	return "https://" + host + "/oauth/v1/app/registration"
}

func (c *registrationClient) post(ctx context.Context, domain string, body map[string]string) (map[string]any, error) {
	form := url.Values{}
	for key, value := range body {
		form.Set(key, value)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint(domain), strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err := c.client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK && response.StatusCode != http.StatusBadRequest {
		return nil, fmt.Errorf("飞书注册请求失败：%d", response.StatusCode)
	}
	var payload map[string]any
	if err := json.NewDecoder(io.LimitReader(response.Body, 64<<10)).Decode(&payload); err != nil {
		return nil, errors.New("飞书注册响应无法确认")
	}
	return payload, nil
}

func registrationInt(value any, fallback, max int) (int, error) {
	if value == nil {
		return fallback, nil
	}
	number, ok := value.(float64)
	if !ok || number <= 0 || number > float64(max) || number != float64(int(number)) {
		return 0, errors.New("飞书注册有效期无效")
	}
	return int(number), nil
}
