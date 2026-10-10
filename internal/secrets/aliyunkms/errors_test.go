package aliyunkms

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"strings"
	"testing"

	openapi "github.com/alibabacloud-go/darabonba-openapi/v2/client"
	"github.com/alibabacloud-go/tea/dara"
	"github.com/alibabacloud-go/tea/tea"
)

func TestOperationErrorDiscardsUnsafeDetails(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		code string
	}{
		{"request URL", &url.Error{Op: "Post", URL: "https://kms.example?Plaintext=secret", Err: errors.New("secret")}, "NetworkError"},
		{"unknown failure", errors.New("secret"), "RequestFailed"},
		{"invalid fields", &openapi.ClientError{Code: dara.String("bad\nsecret"), RequestId: dara.String("bad\nsecret"), StatusCode: dara.Int(999), Message: dara.String("secret")}, "RequestFailed"},
		{"overlong fields", &openapi.ClientError{Code: dara.String(strings.Repeat("x", 129)), RequestId: dara.String(strings.Repeat("x", 129))}, "RequestFailed"},
		{"malformed data", &tea.SDKError{Code: dara.String("Throttling"), Data: dara.String("secret")}, "Throttling"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := operationError(t.Context(), "encrypt", tc.err)
			var failure *KMSError
			if !errors.As(err, &failure) || failure.Code != tc.code || failure.StatusCode != 0 || failure.RequestID != "" {
				t.Fatalf("unexpected metadata: %v", err)
			}
			if strings.Contains(fmt.Sprintf("%+v", err), "secret") || errors.Unwrap(err) != nil {
				t.Fatal("raw SDK error retained")
			}
		})
	}
}

func TestOperationErrorPreservesCancellation(t *testing.T) {
	for _, cause := range []error{context.Canceled, context.DeadlineExceeded, &net.DNSError{IsTimeout: true, Name: "secret"}} {
		err := operationError(t.Context(), "decrypt", &url.Error{Op: "Post", URL: "https://kms.example?Plaintext=secret", Err: cause})
		want := context.DeadlineExceeded
		if errors.Is(cause, context.Canceled) {
			want = context.Canceled
		}
		if !errors.Is(err, want) || strings.Contains(err.Error(), "secret") || errors.Unwrap(err) != want {
			t.Fatalf("unsafe or lost cancellation: %v", err)
		}
	}
}

func TestOperationErrorMetadataAndStructuredLog(t *testing.T) {
	for _, source := range []error{
		&openapi.ClientError{Code: dara.String("Throttling"), StatusCode: dara.Int(429), RequestId: dara.String("request-123"), Message: dara.String("secret")},
		&dara.SDKError{Code: dara.String("Throttling"), StatusCode: dara.Int(429), Data: dara.String(`{"RequestId":"request-123","Message":"secret"}`)},
		&tea.SDKError{Code: dara.String("Throttling"), StatusCode: dara.Int(429), Data: dara.String(`{"RequestId":"request-123","Message":"secret"}`)},
	} {
		err := operationError(t.Context(), "decrypt", fmt.Errorf("secret: %w", source))
		var failure *KMSError
		if !errors.As(err, &failure) || failure.RequestID != "request-123" || failure.StatusCode != 429 || failure.Code != "Throttling" {
			t.Fatalf("diagnostics lost: %v", err)
		}
		var output bytes.Buffer
		slog.New(slog.NewJSONHandler(&output, nil)).ErrorContext(t.Context(), "KMS failed", "error", err)
		var record struct {
			Error struct {
				Code      string `json:"code"`
				Status    int    `json:"status"`
				RequestID string `json:"request_id"`
			} `json:"error"`
		}
		if json.Unmarshal(output.Bytes(), &record) != nil || record.Error.Code != "Throttling" || record.Error.Status != 429 || record.Error.RequestID != "request-123" || strings.Contains(output.String(), "secret") {
			t.Fatal("incorrect or unsafe structured diagnostics")
		}
	}
}

func TestSDKWireLoggingFlags(t *testing.T) {
	for _, tc := range []struct {
		value   string
		blocked bool
	}{
		{"dara", true}, {"tea", true}, {"credential", true}, {"app,dara", true},
		{"", false}, {"1", false}, {"*", false}, {"app", false},
		{"DARA", false}, {" dara", false},
	} {
		t.Run(tc.value, func(t *testing.T) {
			t.Setenv("DEBUG", tc.value)
			if sdkWireLogging(tc.value) != tc.blocked {
				t.Fatal("DEBUG matching differs from the SDK")
			}
			_, err := New(Config{Endpoint: "kms.example", KeyID: "key", AccessKeyID: "offline", AccessKeySecret: "offline"})
			if (err != nil) != (tc.blocked || initialSDKWireLogging) {
				t.Fatalf("unexpected constructor result: %v", err)
			}
		})
	}
}
