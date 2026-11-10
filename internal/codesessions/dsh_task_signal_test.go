package codesessions

import (
	"net/http/httptest"
	"testing"
)

func TestDSHTaskAvailableIsATransportHintWithoutDeliveryID(t *testing.T) {
	response := httptest.NewRecorder()
	if err := writeDSHTaskAvailable(response, response); err != nil {
		t.Fatal(err)
	}
	if response.Body.String() != "event: task.available\ndata: {}\n\n" || !response.Flushed {
		t.Fatalf("response=%q", response.Body.String())
	}
}
