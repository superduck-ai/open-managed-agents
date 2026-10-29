package telemetryexport

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/networkpolicy"
)

const (
	reasonInvalidAddress = "invalid_address"
	reasonRedirect       = "redirect"
	maxResponseDrain     = 64 << 10
)

var uncompressedFallbackStatuses = []int{http.StatusBadRequest, http.StatusNotAcceptable, http.StatusUnsupportedMediaType, http.StatusUnprocessableEntity}

type outcome struct {
	status        int
	err           error
	retryable     bool
	disableReason string
}

func (o outcome) failed() bool {
	return o.err != nil
}

func newDeliveryClient(ctx context.Context, allowInsecure bool, timeout time.Duration, concurrency int) (*http.Client, *http.Transport) {
	public := networkpolicy.NewPublicDialer(allowInsecure)
	transport := networkpolicy.NewPublicTransport(ctx, public.DialContext, min(timeout, 5*time.Second), concurrency)
	transport.TLSHandshakeTimeout = min(timeout, 5*time.Second)
	transport.ResponseHeaderTimeout = min(timeout, 5*time.Second)
	client := &http.Client{Transport: transport, Timeout: timeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return client, transport
}

func gzipPayload(payload []byte) ([]byte, error) {
	var buffer bytes.Buffer
	writer := gzip.NewWriter(&buffer)
	if _, err := writer.Write(payload); err != nil {
		return nil, err
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

func post(ctx context.Context, client *http.Client, url string, headers map[string]string, body []byte, compressed bool) outcome {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return outcome{err: errors.New("invalid destination URL")}
	}
	for name, value := range headers {
		request.Header.Set(name, value)
	}
	request.Header.Set("Content-Type", "application/x-protobuf")
	if compressed {
		request.Header.Set("Content-Encoding", "gzip")
	}
	response, err := client.Do(request)
	if err != nil {
		if errors.Is(err, networkpolicy.ErrNonPublicAddress) {
			return outcome{err: errors.New("destination resolved to a non-public address"), disableReason: reasonInvalidAddress}
		}
		return outcome{err: errors.New("destination request failed before a response"), retryable: true}
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, maxResponseDrain))
	status := response.StatusCode
	switch {
	case status >= 200 && status < 300:
		return outcome{status: status}
	case status >= 300 && status < 400:
		return outcome{status: status, err: fmt.Errorf("destination redirected with status %d", status), disableReason: reasonRedirect}
	case status == http.StatusRequestTimeout || status == http.StatusTooManyRequests || status >= 500:
		return outcome{status: status, err: fmt.Errorf("destination returned status %d", status), retryable: true}
	default:
		return outcome{status: status, err: fmt.Errorf("destination returned status %d", status)}
	}
}

func deliver(ctx context.Context, client *http.Client, url string, headers map[string]string, payload []byte) outcome {
	compressed, err := gzipPayload(payload)
	if err != nil {
		return outcome{err: errors.New("could not compress spans")}
	}
	result := post(ctx, client, url, headers, compressed, true)
	if slices.Contains(uncompressedFallbackStatuses, result.status) {
		result = post(ctx, client, url, headers, payload, false)
	}
	return result
}
