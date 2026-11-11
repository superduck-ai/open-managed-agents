package aliyunkms

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/url"
	"os"
	"strings"
	"syscall"
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
		{"DNS host", &url.Error{Op: "Post", URL: "https://secret.example", Err: &net.DNSError{Err: "no such host", Name: "secret.example"}}, "NetworkError.DNS"},
		{"dial address", &url.Error{Op: "Post", URL: "https://secret.example", Err: &net.OpError{Op: "dial", Net: "tcp", Addr: &net.TCPAddr{IP: net.IPv4(10, 0, 0, 1)}, Err: os.NewSyscallError("connect", syscall.ECONNREFUSED)}}, "NetworkError.ConnectionRefused"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := operationError(t.Context(), "encrypt", tc.err, 1)
			var failure *KMSError
			if !errors.As(err, &failure) || failure.Code != tc.code || failure.StatusCode != 0 || failure.RequestID != "" {
				t.Fatalf("unexpected metadata: %v", err)
			}
			if strings.Contains(fmt.Sprintf("%+v", err), "secret") || strings.Contains(fmt.Sprintf("%+v", err), "10.0.0.1") || errors.Unwrap(err) != nil {
				t.Fatal("raw SDK error retained")
			}
		})
	}
}

func TestClassifyFailure(t *testing.T) {
	for _, tc := range []struct {
		name      string
		err       error
		code      string
		retryable bool
	}{
		{"client rejection", &openapi.ClientError{Code: dara.String("Forbidden.NoPermission"), StatusCode: dara.Int(403)}, "Forbidden.NoPermission", false},
		{"invalid ciphertext", &dara.SDKError{Code: dara.String("InvalidCiphertext"), StatusCode: dara.Int(400)}, "InvalidCiphertext", false},
		{"unknown authority", &url.Error{Op: "Post", URL: "https://kms.example", Err: &tls.CertificateVerificationError{Err: x509.UnknownAuthorityError{}}}, "NetworkError.TLSCertificate", false},
		{"hostname mismatch", &url.Error{Op: "Post", URL: "https://kms.example", Err: x509.HostnameError{Host: "kms.example", Certificate: &x509.Certificate{}}}, "NetworkError.TLSCertificate", false},
		{"unknown failure", errors.New("decode failed"), "RequestFailed", false},
		{"throttling", error(&openapi.ThrottlingError{Code: dara.String("Throttling.User"), StatusCode: dara.Int(400), RequestId: dara.String("request-1")}), "Throttling.User", true},
		{"rejected throttling", &dara.SDKError{Code: dara.String("Rejected.Throttling"), StatusCode: dara.Int(400)}, "Rejected.Throttling", true},
		{"server error", &openapi.ServerError{Code: dara.String("InternalFailure"), StatusCode: dara.Int(503)}, "InternalFailure", true},
		{"DNS", &url.Error{Op: "Post", URL: "https://kms.example", Err: &net.OpError{Op: "dial", Err: &net.DNSError{Err: "no such host", Name: "kms.example"}}}, "NetworkError.DNS", true},
		{"connection refused", &url.Error{Op: "Post", URL: "https://kms.example", Err: &net.OpError{Op: "dial", Err: os.NewSyscallError("connect", syscall.ECONNREFUSED)}}, "NetworkError.ConnectionRefused", true},
		{"connection reset", &url.Error{Op: "Post", URL: "https://kms.example", Err: &net.OpError{Op: "read", Err: os.NewSyscallError("read", syscall.ECONNRESET)}}, "NetworkError.ConnectionReset", true},
		{"broken pipe", &net.OpError{Op: "write", Err: os.NewSyscallError("write", syscall.EPIPE)}, "NetworkError.ConnectionReset", true},
		{"bare unexpected EOF", io.ErrUnexpectedEOF, "NetworkError.ConnectionReset", true},
		{"wrapped EOF", &url.Error{Op: "Post", URL: "https://kms.example", Err: io.EOF}, "NetworkError.ConnectionReset", true},
		{"TLS record", &url.Error{Op: "Post", URL: "https://kms.example", Err: tls.RecordHeaderError{Msg: "bad record"}}, "NetworkError.TLS", true},
		{"TLS alert", &url.Error{Op: "Post", URL: "https://kms.example", Err: tls.AlertError(40)}, "NetworkError.TLS", true},
		{"DNS timeout", &url.Error{Op: "Post", URL: "https://kms.example", Err: &net.DNSError{IsTimeout: true}}, "NetworkError.Timeout", false},
		{"header timeout", &url.Error{Op: "Post", URL: "https://kms.example", Err: timeoutError{}}, "NetworkError.Timeout", false},
		{"other network", &url.Error{Op: "Post", URL: "https://kms.example", Err: errors.New("closed")}, "NetworkError", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			failure, retryable := classifyFailure(tc.err)
			if failure.Code != tc.code || retryable != tc.retryable {
				t.Fatalf("classifyFailure = %q, %t; want %q, %t", failure.Code, retryable, tc.code, tc.retryable)
			}
		})
	}
}

type timeoutError struct{}

func (timeoutError) Error() string   { return "timeout" }
func (timeoutError) Timeout() bool   { return true }
func (timeoutError) Temporary() bool { return true }

func TestOperationErrorPreservesCancellation(t *testing.T) {
	for _, cause := range []error{context.Canceled, context.DeadlineExceeded, &net.DNSError{IsTimeout: true, Name: "secret"}} {
		err := operationError(t.Context(), "decrypt", &url.Error{Op: "Post", URL: "https://kms.example?Plaintext=secret", Err: cause}, 1)
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
		err := operationError(t.Context(), "decrypt", fmt.Errorf("secret: %w", source), 3)
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
				Attempts  int    `json:"attempts"`
			} `json:"error"`
		}
		if json.Unmarshal(output.Bytes(), &record) != nil || record.Error.Code != "Throttling" || record.Error.Status != 429 || record.Error.RequestID != "request-123" || record.Error.Attempts != 3 || strings.Contains(output.String(), "secret") {
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
