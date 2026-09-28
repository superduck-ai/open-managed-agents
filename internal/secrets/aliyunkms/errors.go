package aliyunkms

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"strings"

	"github.com/alibabacloud-go/tea/dara"
	"github.com/alibabacloud-go/tea/tea"
)

// KMSError retains diagnostic metadata, never the original SDK error, URL or
// response body. Those may contain plaintext DEKs or identity credentials.
type KMSError struct {
	Operation  string
	Code       string
	RequestID  string
	StatusCode int
}

func (e *KMSError) Error() string {
	message := fmt.Sprintf("aliyun_kms: %s: %s", e.Operation, e.Code)
	if e.StatusCode != 0 {
		message += fmt.Sprintf(" (status: %d)", e.StatusCode)
	}
	if e.RequestID != "" {
		message += " (request_id: " + e.RequestID + ")"
	}
	return message
}

// LogValue keeps metadata structured when callers log this error with slog.
func (e *KMSError) LogValue() slog.Value {
	return slog.GroupValue(slog.String("provider", providerName),
		slog.String("operation", e.Operation), slog.String("code", e.Code),
		slog.Int("status", e.StatusCode), slog.String("request_id", e.RequestID))
}

func operationError(ctx context.Context, operation string, err error) error {
	if ctx.Err() != nil {
		return fmt.Errorf("aliyun_kms: %s: %w", operation, ctx.Err())
	}
	// Preserve safe sentinels without retaining an SDK/url.Error in the chain.
	for _, sentinel := range []error{context.Canceled, context.DeadlineExceeded} {
		if errors.Is(err, sentinel) {
			return fmt.Errorf("aliyun_kms: %s: %w", operation, sentinel)
		}
	}
	var networkError net.Error
	if errors.As(err, &networkError) && networkError.Timeout() {
		return fmt.Errorf("aliyun_kms: %s: %w", operation, context.DeadlineExceeded)
	}
	failure := &KMSError{Operation: operation}
	readSDKMetadata(err, failure)
	failure.Code = safeIdentifier(failure.Code)
	failure.RequestID = safeIdentifier(failure.RequestID)
	if failure.StatusCode < 100 || failure.StatusCode > 599 {
		failure.StatusCode = 0
	}
	if failure.Code == "" {
		failure.Code = "RequestFailed"
		if networkError != nil {
			failure.Code = "NetworkError"
		}
	}
	return failure
}

func readSDKMetadata(err error, failure *KMSError) {
	var response interface {
		GetCode() *string
		GetRequestId() *string
		GetStatusCode() *int
	}
	var modern *dara.SDKError
	var legacy *tea.SDKError
	var data string
	switch {
	case errors.As(err, &response):
		failure.Code = dara.StringValue(response.GetCode())
		failure.RequestID = dara.StringValue(response.GetRequestId())
		failure.StatusCode = dara.IntValue(response.GetStatusCode())
	case errors.As(err, &modern):
		failure.Code, failure.StatusCode = dara.StringValue(modern.Code), dara.IntValue(modern.StatusCode)
		data = dara.StringValue(modern.Data)
	case errors.As(err, &legacy):
		failure.Code, failure.StatusCode = dara.StringValue(legacy.Code), dara.IntValue(legacy.StatusCode)
		data = dara.StringValue(legacy.Data)
	}
	// Older SDK errors keep RequestId in JSON Data. Decode only that field;
	// ignore malformed or oversized diagnostic payloads instead of logging them.
	if data != "" && len(data) <= 64*1024 {
		var metadata struct {
			RequestID string `json:"RequestId"`
		}
		if json.Unmarshal([]byte(data), &metadata) == nil {
			failure.RequestID = metadata.RequestID
		}
	}
}

func safeIdentifier(value string) string {
	if len(value) > 128 || strings.ContainsFunc(value, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '.' || r == '_' || r == '-')
	}) {
		return ""
	}
	return value
}
