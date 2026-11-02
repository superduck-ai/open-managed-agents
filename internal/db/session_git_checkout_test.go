package db

import "testing"

func TestSessionGitHubCheckoutRejectsInvalidStoredJSON(t *testing.T) {
	for _, value := range []any{42, []byte(`"main"`), []byte(`{"type":42}`), []byte(`{`)} {
		var checkout sessionGitHubCheckout
		if err := checkout.Scan(value); err == nil {
			t.Fatalf("accepted invalid checkout %v", value)
		}
	}
}

func TestSessionGitHubCheckoutRoundTrip(t *testing.T) {
	for _, stored := range []any{nil, `{"type":"branch","name":"main"}`, []byte(`{"type":"commit","sha":"0123456789012345678901234567890123456789"}`)} {
		var checkout sessionGitHubCheckout
		if err := checkout.Scan(stored); err != nil {
			t.Fatal(err)
		}
		value, err := checkout.Value()
		if err != nil {
			t.Fatal(err)
		}
		var restored sessionGitHubCheckout
		if err := restored.Scan(value); err != nil {
			t.Fatal(err)
		}
		if checkout.Checkout == nil {
			if restored.Checkout != nil {
				t.Fatal("NULL checkout changed")
			}
		} else if restored.Checkout == nil || *checkout.Checkout != *restored.Checkout {
			t.Fatal("checkout changed after round trip")
		}
	}
}
