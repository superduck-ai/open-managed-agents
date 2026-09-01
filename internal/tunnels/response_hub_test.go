package tunnels

import "testing"

func TestOfferResponseDeliveryKeepsTerminalAfterNotificationBacklog(t *testing.T) {
	t.Parallel()
	deliveries := make(chan responseDelivery, responseSubscriptionBuffer)
	for _, payload := range []string{"notification-1", "notification-2", "notification-3", "terminal"} {
		offerResponseDelivery(deliveries, responseDelivery{payload: payload})
	}
	first := <-deliveries
	second := <-deliveries
	if first.payload != "notification-3" || second.payload != "terminal" {
		t.Fatalf("buffered deliveries = (%q, %q), want newest notification and terminal", first.payload, second.payload)
	}
}
