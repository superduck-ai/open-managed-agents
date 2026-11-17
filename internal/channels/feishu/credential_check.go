package feishu

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"regexp"
)

var TokenURL = "https://open.feishu.cn/open-apis/auth/v3/tenant_access_token/internal"

var feishuAppIDPattern = regexp.MustCompile(`^cli_[A-Za-z0-9]+$`)

func MatchCredentials(ctx context.Context, client *http.Client, endpoint, appID, appSecret string) (bool, error) {
	payload, err := json.Marshal(map[string]string{"app_id": appID, "app_secret": appSecret})
	if err != nil {
		return false, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return false, err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := client.Do(request)
	if err != nil {
		return false, err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 8<<10))
	if err != nil {
		return false, err
	}
	if response.StatusCode >= 500 {
		return false, errFeishuStatus(response.StatusCode)
	}
	var result struct {
		Code int `json:"code"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return false, err
	}
	return result.Code == 0, nil
}

type feishuStatusError int

func (status feishuStatusError) Error() string {
	return "feishu credential check failed"
}

func errFeishuStatus(status int) error {
	return feishuStatusError(status)
}

func ValidAppID(appID string) bool { return feishuAppIDPattern.MatchString(appID) }
