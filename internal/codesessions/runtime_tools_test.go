package codesessions

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/superduck-ai/open-managed-agents/internal/clitools"
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
			context, err := sessionContextFromCodeSession(record, MCPRuntimeIdentity{}, config.Config{}, builtinToolCatalog{})
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(context["runtime_tools"], test.want) {
				t.Fatalf("runtime_tools = %#v, want %#v", context["runtime_tools"], test.want)
			}
		})
	}
}

func TestCLIToolsFromInstalledPackages(t *testing.T) {
	dir := t.TempDir()
	manifest := func(name string) clitools.Package {
		t.Helper()
		file := name + ".json"
		definition := clitools.Definition{Name: name, Description: name, Executable: "query", Variable: strings.ToUpper(name) + "_TOKEN", TargetHost: "catalog.example.org", Subcommands: []string{"list"}}
		data, err := json.Marshal(definition)
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(filepath.Join(dir, file), data, 0600); err != nil {
			t.Fatal(err)
		}
		return clitools.Package{Manifest: file, ProviderVariable: "QUERY_PROVIDER"}
	}
	a, b := manifest("Lookup"), manifest("Inventory")
	record := db.CodeSession{Metadata: json.RawMessage(`{"config":{"tools":[{"type":"agent_toolset_20260401","default_config":{"enabled":false},"configs":[{"name":"bash","enabled":true}]}],"cli_oauth_providers":{"LOOKUP_TOKEN":"custom:a","INVENTORY_TOKEN":"custom:b"}}}`)}
	for _, tc := range []struct {
		pkgs []clitools.Package
		want []string
	}{
		{nil, []string{"Bash"}}, {[]clitools.Package{a}, []string{"Bash", "Lookup"}}, {[]clitools.Package{a, b}, []string{"Bash", "Lookup", "Inventory"}},
	} {
		installed, err := clitools.Load(tc.pkgs, dir)
		if err != nil {
			t.Fatal(err)
		}
		ctx, err := sessionContextFromCodeSession(record, MCPRuntimeIdentity{}, config.Config{CLITools: installed}, builtinToolCatalog{})
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(ctx["runtime_tools"], tc.want) {
			t.Errorf("工具目录：%v，期望 %v", ctx["runtime_tools"], tc.want)
		}
		if len(ctx["cli_tools"].([]clitools.Definition)) != len(tc.pkgs) {
			t.Fatal("工具定义与已安装的工具不一致")
		}
	}
}
