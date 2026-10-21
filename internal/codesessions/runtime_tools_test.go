package codesessions

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/config"
	"github.com/superduck-ai/open-managed-agents/internal/db"
)

func TestSessionContextRuntimeTools(t *testing.T) {
	for _, test := range []struct {
		name  string
		tools string
		want  []string
	}{
		{"empty", `[]`, []string{}},
		{"disabled", `[{"type":"agent_toolset_20260401","default_config":{"enabled":false}}]`, []string{}},
		{"mcp only", `[{"type":"mcp_toolset"}]`, []string{}},
		{"read only", `[{"type":"agent_toolset_20260401","default_config":{"enabled":false},"configs":[{"name":"read","enabled":true}]}]`, []string{"Read"}},
		{"approval required", `[{"type":"agent_toolset_20260401","default_config":{"enabled":false},"configs":[{"name":"bash","enabled":true,"permission_policy":{"type":"always_ask"}}]}]`, []string{"Bash"}},
		{"default enabled", `[{"type":"agent_toolset_20260401","configs":[{"name":"bash","enabled":false}]}]`, []string{"Read", "Write", "Edit", "Grep", "Glob", "web_search", "web_fetch"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			record := db.CodeSession{Metadata: json.RawMessage(`{"config":{"tools":` + test.tools + `}}`)}
			context, err := sessionContextFromCodeSession(record, MCPRuntimeIdentity{}, config.Config{})
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(context["runtime_tools"], test.want) {
				t.Fatalf("runtime_tools = %#v, want %#v", context["runtime_tools"], test.want)
			}
		})
	}
}
