package webhooks

import (
	"context"
	"errors"
	"net"
	"net/http"
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

func newDeliveryTransport(workerCtx context.Context, allowInsecure bool, timeout time.Duration, concurrency int) *http.Transport {
	public := networkpolicy.NewPublicDialer(allowInsecure)
	guarded := deliveryDialer{allowInsecure: allowInsecure, lookup: public.Lookup, dial: public.Dial}
	return networkpolicy.NewPublicTransport(workerCtx, guarded.dialContext, timeout, concurrency)
}

func (d deliveryDialer) dialContext(ctx context.Context, network, address string) (net.Conn, error) {
	conn, err := networkpolicy.PublicDialer{AllowInsecure: d.allowInsecure, Lookup: d.lookup, Dial: d.dial}.DialContext(ctx, network, address)
	if errors.Is(err, networkpolicy.ErrNonPublicAddress) {
		return nil, deliveryFailure{reason: invalidAddressReason, immediateDisable: true}
	}
	return conn, err
}
