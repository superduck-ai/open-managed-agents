package db

import (
	"context"
	"testing"
)

func TestSessionUsesDSHCache(t *testing.T) {
	t.Run("plain session values carry no transaction cache", func(t *testing.T) {
		if (Session{}).protocol != nil {
			t.Fatal("plain session should not carry a protocol cache")
		}
	})

	t.Run("known non DSR protocol is answered without a query", func(t *testing.T) {
		session := Session{protocol: &sessionProtocol{known: true}}
		dsr, err := sessionUsesDSH(context.Background(), nil, session)
		if err != nil || dsr {
			t.Fatalf("dsr=%v err=%v", dsr, err)
		}
	})

	t.Run("session copies share the transaction cache", func(t *testing.T) {
		session := Session{protocol: &sessionProtocol{known: true, dsr: true}}
		copied := session
		copied.protocol.dsr = false
		dsr, err := sessionUsesDSH(context.Background(), nil, session)
		if err != nil || dsr {
			t.Fatalf("dsr=%v err=%v", dsr, err)
		}
	})
}
