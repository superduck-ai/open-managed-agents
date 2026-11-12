package sessions

import (
	"testing"
	"time"
)

func TestCommittedStreamProtocolCheck(t *testing.T) {
	now := time.Date(2026, 11, 12, 0, 0, 0, 0, time.UTC)

	t.Run("non DSR session is not rechecked before the interval", func(t *testing.T) {
		order := &committedStreamOrder{nextCheck: now.Add(streamProtocolRecheckInterval)}
		if order.needsProtocolCheck(now.Add(streamProtocolRecheckInterval - time.Millisecond)) {
			t.Fatal("protocol rechecked before interval elapsed")
		}
	})

	t.Run("DSR session is never rechecked", func(t *testing.T) {
		order := &committedStreamOrder{enabled: true}
		if order.needsProtocolCheck(now.Add(time.Hour)) {
			t.Fatal("enabled order rechecked protocol")
		}
	})

	t.Run("first notification checks protocol", func(t *testing.T) {
		if !(&committedStreamOrder{}).needsProtocolCheck(now) {
			t.Fatal("first check skipped")
		}
	})

	t.Run("non DSR session is rechecked after the interval", func(t *testing.T) {
		order := &committedStreamOrder{nextCheck: now}
		if !order.needsProtocolCheck(now) {
			t.Fatal("session that may have become DSR was not rechecked")
		}
	})
}
