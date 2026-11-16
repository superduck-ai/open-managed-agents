package devinit

import (
	"encoding/json/v2"
	"strings"
	"testing"
)

func TestProviderStateRetainsLegacyIdentitiesWithoutWorkspaceOwnership(t *testing.T) {
	var state State
	err := json.Unmarshal([]byte(`{"providers":[{"workspace_ref":"first","name":"shared","external_id":"first-id"},{"workspace_ref":"second","name":"shared","external_id":"second-id","input_slug":"local"}]}`), &state)
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Providers) != 2 || state.Providers[0].ExternalID != "first-id" || state.Providers[1].ExternalID != "second-id" {
		t.Fatalf("registered identities = %+v", state.Providers)
	}
	data, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "workspace_ref") {
		t.Fatal("provider workspace ownership serialized")
	}
	if state.Providers[0].InputSlug != nil || state.Providers[1].InputSlug == nil || *state.Providers[1].InputSlug != "local" {
		t.Fatal("input slug provenance changed")
	}
}
