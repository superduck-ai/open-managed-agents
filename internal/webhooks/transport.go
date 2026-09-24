package webhooks

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"time"

	"github.com/superduck-ai/open-managed-agents/internal/networkpolicy"
)

// deliveryDialer resolves once and dials only vetted numeric addresses. Keeping
// the request URL unchanged preserves Host, TLS SNI and certificate validation.
type deliveryDialer struct {
	allowInsecure bool
	lookup        func(context.Context, string) ([]net.IPAddr, error)
	dial          func(context.Context, string, string) (net.Conn, error)
}

func newDeliveryTransport(batchCtx context.Context, allowInsecure bool, timeout time.Duration) *http.Transport {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.DialTLSContext = nil
	transport.DialTLS = nil
	dialer := &net.Dialer{}
	guarded := deliveryDialer{allowInsecure: allowInsecure, lookup: net.DefaultResolver.LookupIPAddr, dial: dialer.DialContext}
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		ctx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()
		stop := context.AfterFunc(batchCtx, cancel)
		defer stop()
		return guarded.dialContext(ctx, network, address)
	}
	return transport
}

func (d deliveryDialer) dialContext(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	if d.allowInsecure {
		return d.dial(ctx, network, address)
	}
	if port != "443" {
		return nil, deliveryFailure{reason: invalidAddressReason, immediateDisable: true}
	}
	var candidates []net.IPAddr
	if ip, err := netip.ParseAddr(host); err == nil {
		candidates = []net.IPAddr{{IP: net.IP(ip.AsSlice()), Zone: ip.Zone()}}
	} else {
		candidates, err = d.lookup(ctx, host)
		if err != nil {
			return nil, err
		}
	}
	var public []netip.Addr
	for _, candidate := range candidates {
		ip, ok := netip.AddrFromSlice(candidate.IP)
		if ok && candidate.Zone == "" && networkpolicy.PublicAddress(ip) {
			public = append(public, ip.Unmap())
		}
	}
	var failures []error
	for index, ip := range public {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		attemptCtx := ctx
		cancel := func() {}
		if deadline, ok := ctx.Deadline(); ok {
			attemptCtx, cancel = context.WithTimeout(ctx, time.Until(deadline)/time.Duration(len(public)-index))
		}
		conn, err := d.dial(attemptCtx, network, net.JoinHostPort(ip.String(), port))
		cancel()
		if err == nil {
			return conn, nil
		}
		failures = append(failures, err)
	}
	if len(failures) > 0 {
		return nil, errors.Join(failures...)
	}
	return nil, deliveryFailure{reason: invalidAddressReason, immediateDisable: true}
}
