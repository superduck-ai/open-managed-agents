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
	body, err := io.ReadAll(io.LimitReader(response.Body, maxSize+1))
	if err != nil {
		return nil, fmt.Errorf("read %s search response: %w", provider, err)
	}
	if int64(len(body)) > maxSize {
		return nil, errors.New(provider + " search response exceeds maximum size")
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("%s search provider returned HTTP %d", provider, response.StatusCode)
	}
	return body, nil
}
