package tunnels

import (
	"errors"
	"fmt"
	"testing"
)

func TestPurgedControlCannotBeRecreatedByLateWork(t *testing.T) {
	b := testNATSBroker(t, brokerTestConfig())
	if err := b.purgeControl(t.Context(), "tunnel"); err != nil {
		t.Fatal(err)
	}
	if err := b.ActivateTokenVersion(t.Context(), "archived", 1); err != nil {
		t.Fatal(err)
	}
	var state tunnelControl
	stored, err := b.control.read(t.Context(), brokerKey("archived"), &state)
	if err != nil {
		t.Fatal(err)
	}
	if err := b.purgeControl(t.Context(), "archived"); err != nil {
		t.Fatal(err)
	}
	if err := b.control.update(t.Context(), brokerKey("archived"), state, stored.revision, maxControlValueBytes); !brokerCASConflict(err) {
		t.Fatalf("late CAS: %v", err)
	}
	if err := b.RegisterConnector(t.Context(), "archived", "late", 1, []ChannelDeclaration{{Name: "main"}}); !errors.Is(err, ErrControlNotFound) {
		t.Fatalf("late poll: %v", err)
	}
	b.releaseCommand(t.Context(), "archived", "late")
	if err := b.SuspendTokenVersion(t.Context(), "archived", 1); err != nil {
		t.Fatal(err)
	}
	if _, err := b.control.read(t.Context(), brokerKey("archived"), &state); !errors.Is(err, ErrRequestNotFound) {
		t.Fatalf("control recreated: %v", err)
	}
}

func TestControlCapacityIsReusableAfterCleanup(t *testing.T) {
	b := testNATSBroker(t, brokerTestConfig())
	if err := b.purgeControl(t.Context(), "tunnel"); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < maxControlRecords; i++ {
		if err := b.ActivateTokenVersion(t.Context(), fmt.Sprint(i), 1); err != nil {
			t.Fatal(err)
		}
	}
	if err := b.ActivateTokenVersion(t.Context(), "overflow", 1); !errors.Is(err, ErrQueueLimit) {
		t.Fatalf("capacity: %v", err)
	}
	for i := 0; i < maxControlRecords+1; i++ {
		old := fmt.Sprint(i)
		if err := b.SuspendTokenVersion(t.Context(), old, 1); err != nil {
			t.Fatal(err)
		}
		if err := b.purgeControl(t.Context(), old); err != nil {
			t.Fatal(err)
		}
		if err := b.purgeControl(t.Context(), old); err != nil {
			t.Fatal(err)
		}
		if err := b.ActivateTokenVersion(t.Context(), fmt.Sprint(i+maxControlRecords), 1); err != nil {
			t.Fatalf("replacement %d: %v", i, err)
		}
	}
	info, err := b.control.stream.Info(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if info.State.Msgs != maxControlRecords {
		t.Fatalf("retained records=%d", info.State.Msgs)
	}
}
