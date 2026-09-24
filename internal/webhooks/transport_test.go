package webhooks

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestDeliveryDialerRejectsNonPublicAddresses(t *testing.T) {
	for _, address := range []string{"127.0.0.1", "10.0.0.1", "169.254.169.254", "100.64.0.1", "198.18.0.1", "192.0.2.1", "224.0.0.1", "::1", "fe80::1", "fc00::1", "::ffff:127.0.0.1", "2001:db8::1"} {
		t.Run(address, func(t *testing.T) {
			dialer := deliveryDialer{
				lookup: func(context.Context, string) ([]net.IPAddr, error) {
					return []net.IPAddr{{IP: net.ParseIP(address)}}, nil
				},
				dial: func(context.Context, string, string) (net.Conn, error) {
					t.Error("dialed rejected address")
					return nil, errors.New("unexpected dial")
				},
			}
			_, err := dialer.dialContext(t.Context(), "tcp", "hook.example:443")
			var failure deliveryFailure
			if !errors.As(err, &failure) || !failure.immediateDisable || failure.reason != invalidAddressReason {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestDeliveryDialerTransientFailures(t *testing.T) {
	for _, lookupFailure := range []bool{true, false} {
		t.Run(map[bool]string{true: "dns", false: "connection"}[lookupFailure], func(t *testing.T) {
			transient := errors.New("temporary failure")
			d := deliveryDialer{
				lookup: func(context.Context, string) ([]net.IPAddr, error) {
					if lookupFailure {
						return nil, transient
					}
					return []net.IPAddr{{IP: net.ParseIP("8.8.8.8")}}, nil
				},
				dial: func(context.Context, string, string) (net.Conn, error) { return nil, transient },
			}
			_, err := d.dialContext(t.Context(), "tcp", "hook.example:443")
			var failure deliveryFailure
			if !errors.Is(err, transient) || errors.As(err, &failure) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestDeliveryDialerPinsCandidatesAndSharesDeadline(t *testing.T) {
	lookups := 0
	var dialed []string
	left, right := net.Pipe()
	defer left.Close()
	defer right.Close()
	d := deliveryDialer{
		lookup: func(context.Context, string) ([]net.IPAddr, error) {
			lookups++
			return []net.IPAddr{{IP: net.ParseIP("127.0.0.1")}, {IP: net.ParseIP("2606:4700:4700::1111")}, {IP: net.ParseIP("::ffff:8.8.8.8")}}, nil
		},
		dial: func(ctx context.Context, network, address string) (net.Conn, error) {
			dialed = append(dialed, address)
			if len(dialed) == 1 {
				<-ctx.Done()
				return nil, ctx.Err()
			}
			if ctx.Err() != nil {
				t.Error("first address exhausted entire deadline")
			}
			return left, nil
		},
	}
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	conn, err := d.dialContext(ctx, "tcp", "hook.example:443")
	if err != nil || conn != left || lookups != 1 || !reflect.DeepEqual(dialed, []string{"[2606:4700:4700::1111]:443", "8.8.8.8:443"}) {
		t.Fatalf("conn=%v err=%v lookups=%d dialed=%v", conn, err, lookups, dialed)
	}
}

func TestDeliveryTransportDirectTLS(t *testing.T) {
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:1")
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host != "example.com" || r.TLS.ServerName != "example.com" {
			t.Errorf("host=%s sni=%s", r.Host, r.TLS.ServerName)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	transport := newDeliveryTransport(t.Context(), false, time.Second)
	defer transport.CloseIdleConnections()
	if transport.Proxy != nil || transport.TLSClientConfig != nil && transport.TLSClientConfig.InsecureSkipVerify {
		t.Fatal("unsafe transport")
	}
	roots := x509.NewCertPool()
	roots.AddCert(server.Certificate())
	transport.TLSClientConfig = &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}
	d := deliveryDialer{
		lookup: func(context.Context, string) ([]net.IPAddr, error) {
			return []net.IPAddr{{IP: net.ParseIP("8.8.8.8")}}, nil
		},
		dial: func(ctx context.Context, network, address string) (net.Conn, error) {
			if address != "8.8.8.8:443" {
				t.Errorf("dial address=%s", address)
			}
			return (&net.Dialer{}).DialContext(ctx, network, server.Listener.Addr().String())
		},
	}
	transport.DialContext = d.dialContext
	client := &http.Client{Transport: transport, Timeout: time.Second}
	resp, err := client.Get("https://example.com/")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatal(resp.StatusCode)
	}
	// The local server's certificate is not valid for this host.
	if resp, err := client.Get("https://wrong.example/"); err == nil {
		resp.Body.Close()
		t.Fatal("accepted wrong certificate hostname")
	}
}

func TestDeliveryDialerCancellation(t *testing.T) {
	started := make(chan struct{})
	exited := make(chan error, 1)
	d := deliveryDialer{
		lookup: func(ctx context.Context, _ string) ([]net.IPAddr, error) {
			close(started)
			<-ctx.Done()
			return nil, ctx.Err()
		},
		dial: func(context.Context, string, string) (net.Conn, error) {
			t.Error("unexpected dial")
			return nil, errors.New("unexpected")
		},
	}
	ctx, cancel := context.WithCancel(t.Context())
	go func() { _, err := d.dialContext(ctx, "tcp", "hook.example:443"); exited <- err }()
	<-started
	cancel()
	select {
	case err := <-exited:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("resolver did not stop")
	}
}

func TestDeliverPreservesAddressRejection(t *testing.T) {
	transport := newDeliveryTransport(t.Context(), false, time.Second)
	defer transport.CloseIdleConnections()
	d := deliveryDialer{lookup: func(context.Context, string) ([]net.IPAddr, error) {
		return []net.IPAddr{{IP: net.ParseIP("10.1.2.3")}}, nil
	}}
	transport.DialContext = d.dialContext
	key, err := newSigningSecret()
	if err != nil {
		t.Fatal(err)
	}
	err = deliver(t.Context(), &http.Client{Transport: transport, Timeout: time.Second}, deliveryTarget{URL: "https://hook.example/?private=query-secret", SigningKey: key}, []byte(`{"id":"wevt_dns"}`))
	var failure deliveryFailure
	if !errors.As(err, &failure) || !failure.immediateDisable || failure.reason != invalidAddressReason {
		t.Fatalf("error = %v", err)
	}
	if strings.Contains(err.Error(), "query-secret") {
		t.Fatal("URL query leaked in delivery failure")
	}
}
