package networkpolicy

import "testing"

func TestAllowsHost(t *testing.T) {
	t.Parallel()
	ok, err := AllowsHost([]string{"api.notion.com", "*.example.com"}, "sub.example.com", "443")
	if err != nil || !ok {
		t.Fatalf("AllowsHost = %v, %v", ok, err)
	}
}
