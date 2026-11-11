package aliyunkms

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"strings"
	"syscall"

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
	Attempts   int
}

func (e *KMSError) Error() string {
	message := fmt.Sprintf("aliyun_kms: %s: %s", e.Operation, e.Code)
	if e.StatusCode != 0 {
		message += fmt.Sprintf(" (status: %d)", e.StatusCode)
	}
	if e.RequestID != "" {
		message += " (request_id: " + e.RequestID + ")"
	}
	if e.Attempts > 1 {
		message += fmt.Sprintf(" (attempts: %d)", e.Attempts)
	}
	return message
}

// LogValue keeps metadata structured when callers log this error with slog.
func (e *KMSError) LogValue() slog.Value {
	return slog.GroupValue(slog.String("provider", providerName),
		slog.String("operation", e.Operation), slog.String("code", e.Code),
		slog.Int("status", e.StatusCode), slog.String("request_id", e.RequestID),
		slog.Int("attempts", e.Attempts))
}

func operationError(ctx context.Context, operation string, err error, attempts int) error {
	if ctx.Err() != nil {
		return fmt.Errorf("aliyun_kms: %s: %w", operation, ctx.Err())
	}
	for _, sentinel := range []error{context.Canceled, context.DeadlineExceeded} {
		if errors.Is(err, sentinel) {
			return fmt.Errorf("aliyun_kms: %s: %w", operation, sentinel)
		}
	}
	var networkError net.Error
	if errors.As(err, &networkError) && networkError.Timeout() {
		return fmt.Errorf("aliyun_kms: %s: %w", operation, context.DeadlineExceeded)
	}
	failure, _ := classifyFailure(err)
	failure.Operation = operation
	failure.Attempts = attempts
	return failure
}

func classifyFailure(err error) (*KMSError, bool) {
	failure := &KMSError{}
	readSDKMetadata(err, failure)
	failure.Code = safeIdentifier(failure.Code)
	failure.RequestID = safeIdentifier(failure.RequestID)
	if failure.StatusCode < 100 || failure.StatusCode > 599 {
		failure.StatusCode = 0
	}
	if failure.Code == "" && failure.StatusCode == 0 {
		code, retryable := transportFailure(err)
		failure.Code = code
		return failure, retryable
	}
	retryable := failure.StatusCode >= 500 || strings.Contains(failure.Code, "Throttling")
	if failure.Code == "" {
		failure.Code = "RequestFailed"
	}
	return failure, retryable
}

func transportFailure(err error) (string, bool) {
	var dnsError *net.DNSError
	var networkError net.Error
	switch {
	case certificateFailure(err):
		return "NetworkError.TLSCertificate", false
	case errors.As(err, &networkError) && networkError.Timeout():
		return "NetworkError.Timeout", false
	case errors.As(err, &dnsError):
		return "NetworkError.DNS", true
	case errors.Is(err, syscall.ECONNREFUSED):
		return "NetworkError.ConnectionRefused", true
	case errors.Is(err, syscall.ECONNRESET), errors.Is(err, syscall.EPIPE), errors.Is(err, io.EOF), errors.Is(err, io.ErrUnexpectedEOF):
		return "NetworkError.ConnectionReset", true
	case tlsFailure(err):
		return "NetworkError.TLS", true
	case errors.As(err, &networkError):
		return "NetworkError", true
	default:
		return "RequestFailed", false
	}
}

func certificateFailure(err error) bool {
	var verification *tls.CertificateVerificationError
	var unknownAuthority x509.UnknownAuthorityError
	var hostname x509.HostnameError
	var invalid x509.CertificateInvalidError
	return errors.As(err, &verification) || errors.As(err, &unknownAuthority) || errors.As(err, &hostname) || errors.As(err, &invalid)
}

func tlsFailure(err error) bool {
	var recordHeader tls.RecordHeaderError
	var alert tls.AlertError
	return errors.As(err, &recordHeader) || errors.As(err, &alert)
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
