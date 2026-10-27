package observability

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestDashboardProjectionOmitsSQLAndStream(t *testing.T) {
	payload, err := json.Marshal(projectDashboard())
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	lower := strings.ToLower(string(payload))
	for _, leaked := range []string{"sql", "stream"} {
		if strings.Contains(lower, leaked) {
			t.Fatalf("dashboard projection leaked %q: %s", leaked, payload)
		}
	}
	var projected DashboardProjection
	if err := json.Unmarshal(payload, &projected); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if len(projected.Tabs) != 3 {
		t.Fatalf("tabs = %d, want 3", len(projected.Tabs))
	}
	counts := map[string]int{}
	for _, tab := range projected.Tabs {
		counts[tab.ID] = len(tab.Panels)
	}
	if counts["overview"] != 14 || counts["model"] != 13 || counts["tool"] != 7 {
		t.Fatalf("panel counts = %#v", counts)
	}
	for _, query := range projected.Queries {
		if query.QueryRef == traceListRef || query.QueryRef == traceDetailRef {
			t.Fatalf("projection included fixed trace ref %q", query.QueryRef)
		}
	}
}

func TestAllQueriesDeclareOptionalAgentVersion(t *testing.T) {
	for _, query := range loadedDashboard.Queries {
		found := false
		for _, spec := range query.Variables {
			if spec.Name != "agent_version" {
				continue
			}
			found = true
			if spec.Type != variableIntList || spec.Required {
				t.Fatalf("%s agent_version = %+v, want optional int_list", query.QueryRef, spec)
			}
		}
		if !found {
			t.Fatalf("%s missing agent_version variable", query.QueryRef)
		}
	}
}
