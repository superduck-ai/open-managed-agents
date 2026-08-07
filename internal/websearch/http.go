package websearch

import (
	"errors"
	"fmt"
	"io"
	"net/http"
)

func fetchLimitedBody(client *http.Client, request *http.Request, maxSize int64, provider string) ([]byte, error) {
	response, err := client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("%s search request failed: %w", provider, err)
	}
	defer response.Body.Close()
	// 先判定状态码：一个超限的错误页不应该把 429/503 这类可用于重试决策的信息
	// 换成一条无状态的大小错误。
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("%s search provider returned HTTP %d", provider, response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxSize+1))
	if err != nil {
		return nil, fmt.Errorf("read %s search response: %w", provider, err)
	}
	if int64(len(body)) > maxSize {
		return nil, errors.New(provider + " search response exceeds maximum size")
	}
	return body, nil
}
