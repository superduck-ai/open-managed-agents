package telemetryexport

import (
	"compress/gzip"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

type recordedRequest struct {
	encoding, contentType, authorization string
	body                                 []byte
}

func deliveryServer(t *testing.T, statuses ...int) (*httptest.Server, *[]recordedRequest) {
	t.Helper()
	var mutex sync.Mutex
	var requests []recordedRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body []byte
		if r.Header.Get("Content-Encoding") == "gzip" {
			if reader, err := gzip.NewReader(r.Body); err == nil {
				body, _ = io.ReadAll(reader)
			}
		} else {
			body, _ = io.ReadAll(r.Body)
		}
		mutex.Lock()
		index := len(requests)
		requests = append(requests, recordedRequest{encoding: r.Header.Get("Content-Encoding"), contentType: r.Header.Get("Content-Type"), authorization: r.Header.Get("Authorization"), body: body})
		mutex.Unlock()
		status := statuses[min(index, len(statuses)-1)]
		if status >= 300 && status < 400 {
			w.Header().Set("Location", "https://elsewhere.example.com")
		}
		w.WriteHeader(status)
	}))
	t.Cleanup(server.Close)
	return server, &requests
}

func TestDeliveryFailureClassification(t *testing.T) {
	client, transport := newDeliveryClient(context.Background(), true, 2*time.Second, 2)
	defer transport.CloseIdleConnections()
	for _, test := range []struct {
		name      string
		statuses  []int
		requests  int
		retryable bool
		reason    string
	}{
		{"redirect disables immediately", []int{http.StatusFound}, 1, false, reasonRedirect},
		{"rate limit retries", []int{http.StatusTooManyRequests}, 1, true, ""},
		{"server error retries", []int{http.StatusBadGateway}, 1, true, ""},
		{"request timeout retries", []int{http.StatusRequestTimeout}, 1, true, ""},
		{"authentication failure is final", []int{http.StatusUnauthorized}, 1, false, ""},
		{"rejected compression falls back once and stays final", []int{http.StatusUnsupportedMediaType, http.StatusBadRequest}, 2, false, ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			server, requests := deliveryServer(t, test.statuses...)
			result := deliver(context.Background(), client, server.URL, nil, []byte("spans"))
			if !result.failed() || result.retryable != test.retryable || result.disableReason != test.reason || len(*requests) != test.requests {
				t.Fatalf("result = %+v requests = %d", result, len(*requests))
			}
		})
	}
	unreachable := deliver(context.Background(), client, "http://127.0.0.1:1/v1/traces", nil, []byte("spans"))
	if !unreachable.failed() || !unreachable.retryable {
		t.Fatalf("connection failure = %+v", unreachable)
	}
	strictClient, strictTransport := newDeliveryClient(context.Background(), false, 2*time.Second, 1)
	defer strictTransport.CloseIdleConnections()
	private := deliver(context.Background(), strictClient, "https://127.0.0.1/v1/traces", nil, []byte("spans"))
	if !private.failed() || private.retryable || private.disableReason != reasonInvalidAddress {
		t.Fatalf("private destination = %+v", private)
	}
}

func TestDeliverySendsCompressedProtobufWithDestinationHeaders(t *testing.T) {
	client, transport := newDeliveryClient(context.Background(), true, 2*time.Second, 1)
	defer transport.CloseIdleConnections()
	server, requests := deliveryServer(t, http.StatusUnprocessableEntity, http.StatusOK)
	result := deliver(context.Background(), client, server.URL, map[string]string{"Authorization": "Bearer secret", "Content-Type": "text/plain"}, []byte("spans"))
	if result.failed() || result.status != http.StatusOK || len(*requests) != 2 {
		t.Fatalf("result = %+v requests = %d", result, len(*requests))
	}
	first, second := (*requests)[0], (*requests)[1]
	if first.encoding != "gzip" || second.encoding != "" || string(first.body) != "spans" || string(second.body) != "spans" {
		t.Fatalf("requests = %+v", *requests)
	}
	if first.contentType != "application/x-protobuf" || first.authorization != "Bearer secret" {
		t.Fatalf("headers = %+v", first)
	}
}
