package aliyunkms

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestWorkloadCredentialConcurrentRefresh(t *testing.T) {
	for _, key := range []string{"ALIBABA_CLOUD_OIDC_TOKEN_FILE", "ALIBABA_CLOUD_OIDC_PROVIDER_ARN", "ALIBABA_CLOUD_ROLE_ARN", "ALIBABA_CLOUD_ECS_METADATA_DISABLED"} {
		t.Setenv(key, "")
	}
	t.Setenv("ALIBABA_CLOUD_ECS_METADATA", "test-role")
	var active, maximum atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			fmt.Fprint(w, "metadata-token")
			return
		}
		count := active.Add(1)
		defer active.Add(-1)
		for previous := maximum.Load(); count > previous; previous = maximum.Load() {
			if maximum.CompareAndSwap(previous, count) {
				break
			}
		}
		time.Sleep(10 * time.Millisecond)
		fmt.Fprint(w, `{"Code":"Success","AccessKeyId":"fake-ak","AccessKeySecret":"fake-sk","SecurityToken":"fake-sts","Expiration":"2099-01-01T00:00:00Z"}`)
	}))
	defer server.Close()
	proxyURL, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	original := http.DefaultTransport
	transport := original.(*http.Transport).Clone()
	transport.Proxy = http.ProxyURL(proxyURL)
	http.DefaultTransport = transport
	defer func() {
		http.DefaultTransport = original
		transport.CloseIdleConnections()
	}()
	credential, err := newCredential(Config{})
	if err != nil {
		t.Fatal(err)
	}
	var workers sync.WaitGroup
	start := make(chan struct{})
	for range 16 {
		workers.Go(func() {
			<-start
			value, err := credential.GetCredential()
			if err != nil {
				t.Error(err)
				return
			}
			if value.AccessKeyId == nil || *value.AccessKeyId != "fake-ak" || value.SecurityToken == nil || *value.SecurityToken != "fake-sts" {
				t.Error("unexpected workload credentials")
			}
		})
	}
	close(start)
	workers.Wait()
	if maximum.Load() != 1 {
		t.Fatalf("concurrent metadata refreshes = %d, want 1", maximum.Load())
	}
}
