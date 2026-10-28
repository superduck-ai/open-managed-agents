package networkpolicy

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"time"
)

var ErrNonPublicAddress = errors.New("destination resolved to a non-public address")

type PublicDialer struct {
	AllowInsecure bool
	Lookup        func(context.Context, string) ([]net.IPAddr, error)
	Dial          func(context.Context, string, string) (net.Conn, error)
}

func NewPublicDialer(allowInsecure bool) PublicDialer {
	dialer := &net.Dialer{}
	return PublicDialer{AllowInsecure: allowInsecure, Lookup: net.DefaultResolver.LookupIPAddr, Dial: dialer.DialContext}
}

func NewPublicTransport(workerCtx context.Context, dial func(context.Context, string, string) (net.Conn, error), timeout time.Duration, concurrency int) *http.Transport {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.MaxIdleConns = concurrency
	transport.MaxIdleConnsPerHost = concurrency
	transport.MaxConnsPerHost = concurrency
	transport.Proxy = nil
	transport.DialTLSContext = nil
	transport.DialTLS = nil
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		ctx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()
		stop := context.AfterFunc(workerCtx, cancel)
		defer stop()
		return dial(ctx, network, address)
	}
	return transport
}

func (d PublicDialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	if d.AllowInsecure {
		return d.Dial(ctx, network, address)
	}
	if port != "443" {
		return nil, ErrNonPublicAddress
	}
	var candidates []net.IPAddr
	if ip, err := netip.ParseAddr(host); err == nil {
		candidates = []net.IPAddr{{IP: net.IP(ip.AsSlice()), Zone: ip.Zone()}}
	} else {
		candidates, err = d.Lookup(ctx, host)
		if err != nil {
			return nil, err
		}
	}
	var public []netip.Addr
	for _, candidate := range candidates {
		ip, ok := netip.AddrFromSlice(candidate.IP)
		if ok && candidate.Zone == "" && PublicAddress(ip) {
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
		conn, err := d.Dial(attemptCtx, network, net.JoinHostPort(ip.String(), port))
		cancel()
		if err == nil {
			return conn, nil
		}
		failures = append(failures, err)
	}
	if len(failures) > 0 {
		return nil, errors.Join(failures...)
	}
	return nil, ErrNonPublicAddress
}
