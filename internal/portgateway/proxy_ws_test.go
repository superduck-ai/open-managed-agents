package portgateway

import (
	"encoding/binary"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/porttunnel"
)

func TestTunnelCloseFrameNeverCarriesReservedCodes(t *testing.T) {
	for code, want := range map[int]int{1006: 1001, 1015: 1001, 0: 1001, 1004: 1001, 999: 1001, 4001: 4001, 1000: 1000} {
		frame := tunnelCloseFrame(&porttunnel.CloseError{Code: code, Reason: "r"})
		if len(frame) < 2 || int(binary.BigEndian.Uint16(frame)) != want {
			t.Fatalf("code %d produced frame %v, want %d", code, frame, want)
		}
	}
	if frame := tunnelCloseFrame(&porttunnel.CloseError{Code: 1005}); len(frame) != 0 {
		t.Fatalf("no-status close produced payload %v", frame)
	}
	if frame := tunnelCloseFrame(porttunnel.ErrTunnelDisconnected); int(binary.BigEndian.Uint16(frame)) != 1001 {
		t.Fatalf("disconnect frame = %v", frame)
	}
}
