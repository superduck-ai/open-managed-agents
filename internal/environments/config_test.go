package environments

import (
	"encoding/json"
	"testing"
)

func TestNormalizeEnvironmentConfigForCreateRejectsInvalidConfig(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{name: "non-object config", raw: `[]`, want: "config must be an object"},
		{name: "unknown config type", raw: `{"type":"local"}`, want: "config.type must be cloud or self_hosted"},
		{name: "non-object packages", raw: `{"packages":[]}`, want: "config.packages must be an object or null"},
		{name: "invalid package list", raw: `{"packages":{"pip":"numpy"}}`, want: "config.packages.pip must be an array of strings"},
		{name: "unknown network type", raw: `{"networking":{"type":"private"}}`, want: "config.networking.type must be unrestricted or limited"},
		{name: "host URL", raw: `{"networking":{"type":"limited","allowed_hosts":["https://example.com"]}}`, want: "config.networking.allowed_hosts entries must be hostnames without URL schemes"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := normalizeEnvironmentConfigForCreate(json.RawMessage(test.raw))
			if err == nil || err.Error() != test.want {
				t.Fatalf("NormalizeEnvironmentConfigForCreate() error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestNormalizeEnvironmentConfigForCreatePreservesCurrentContract(t *testing.T) {
	tests := []struct {
		name string
		raw  json.RawMessage
		want string
	}{
		{
			name: "omitted config uses unrestricted cloud defaults",
			want: `{
				"type":"cloud",
				"packages":{"type":"packages","apt":[],"cargo":[],"gem":[],"go":[],"npm":[],"pip":[]},
				"networking":{"type":"unrestricted"}
			}`,
		},
		{
			name: "cloud config normalizes packages and limited networking",
			raw: json.RawMessage(`{
				"packages":{"pip":["numpy"],"npm":["typescript"]},
				"networking":{"type":"limited","allowed_hosts":["*.example.com"],"allow_package_managers":true}
			}`),
			want: `{
				"type":"cloud",
				"packages":{"type":"packages","apt":[],"cargo":[],"gem":[],"go":[],"npm":["typescript"],"pip":["numpy"]},
				"networking":{"type":"limited","allowed_hosts":["*.example.com"],"allow_mcp_servers":false,"allow_package_managers":true}
			}`,
		},
		{
			name: "self-hosted config drops unrelated fields",
			raw:  json.RawMessage(`{"type":"self_hosted","packages":{"pip":["numpy"]}}`),
			want: `{"type":"self_hosted"}`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := normalizeEnvironmentConfigForCreate(test.raw)
			if err != nil {
				t.Fatalf("NormalizeEnvironmentConfigForCreate() error = %v", err)
			}
			assertJSONEqual(t, got, test.want)
		})
	}
}

func TestNormalizeEnvironmentConfigForUpdatePreservesPatchContract(t *testing.T) {
	current := json.RawMessage(`{
		"type":"cloud",
		"packages":{"type":"packages","pip":["numpy"]},
		"networking":{"type":"limited","allowed_hosts":["example.com"],"allow_mcp_servers":true,"allow_package_managers":false},
		"init_script":"legacy setup",
		"environment":{"LEGACY":"value"}
	}`)

	tests := []struct {
		name string
		raw  string
		want string
	}{
		{
			name: "null resets to cloud defaults",
			raw:  `null`,
			want: `{"type":"cloud","packages":{"type":"packages","apt":[],"cargo":[],"gem":[],"go":[],"npm":[],"pip":[]},"networking":{"type":"unrestricted"}}`,
		},
		{
			name: "package patch retains networking and legacy response fields",
			raw:  `{"packages":{"apt":["git"]}}`,
			want: `{
				"type":"cloud",
				"packages":{"type":"packages","apt":["git"],"cargo":[],"gem":[],"go":[],"npm":[],"pip":[]},
				"networking":{"type":"limited","allowed_hosts":["example.com"],"allow_mcp_servers":true,"allow_package_managers":false},
				"init_script":"legacy setup",
				"environment":{"LEGACY":"value"}
			}`,
		},
		{
			name: "networking null resets only networking",
			raw:  `{"type":"cloud","networking":null}`,
			want: `{
				"type":"cloud",
				"packages":{"type":"packages","pip":["numpy"]},
				"networking":{"type":"unrestricted"},
				"init_script":"legacy setup",
				"environment":{"LEGACY":"value"}
			}`,
		},
		{
			name: "type switch replaces config",
			raw:  `{"type":"self_hosted"}`,
			want: `{"type":"self_hosted"}`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := normalizeEnvironmentConfigForUpdate(current, json.RawMessage(test.raw))
			if err != nil {
				t.Fatalf("NormalizeEnvironmentConfigForUpdate() error = %v", err)
			}
			assertJSONEqual(t, got, test.want)
		})
	}
}

func TestEnvironmentConfigForResponsePreservesPlatformShape(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{
			name: "invalid stored config uses limited response defaults",
			raw:  `{`,
			want: `{
				"type":"cloud",
				"packages":{"type":"packages","apt":[],"cargo":[],"gem":[],"go":[],"npm":[],"pip":[]},
				"networking":{"type":"limited","allowed_hosts":[],"allow_mcp_servers":false,"allow_package_managers":false},
				"init_script":"",
				"environment":{}
			}`,
		},
		{
			name: "stored cloud config gets compatibility fields",
			raw:  `{"type":"cloud","packages":{"pip":["numpy"]},"networking":{"type":"unrestricted"}}`,
			want: `{
				"type":"cloud",
				"packages":{"type":"packages","apt":[],"cargo":[],"gem":[],"go":[],"npm":[],"pip":["numpy"]},
				"networking":{"type":"unrestricted","allowed_hosts":[],"allow_mcp_servers":false,"allow_package_managers":false},
				"init_script":"",
				"environment":{}
			}`,
		},
		{name: "self-hosted response remains minimal", raw: `{"type":"self_hosted","unexpected":true}`, want: `{"type":"self_hosted"}`},
		{name: "unknown config type passes through", raw: `{"type":"future","value":1}`, want: `{"type":"future","value":1}`},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assertJSONEqual(t, environmentConfigForResponse(json.RawMessage(test.raw)), test.want)
		})
	}
}

func assertJSONEqual(t *testing.T, got json.RawMessage, want string) {
	t.Helper()
	var gotValue any
	if err := json.Unmarshal(got, &gotValue); err != nil {
		t.Fatalf("unmarshal actual JSON %q: %v", got, err)
	}
	var wantValue any
	if err := json.Unmarshal([]byte(want), &wantValue); err != nil {
		t.Fatalf("unmarshal expected JSON %q: %v", want, err)
	}
	gotCanonical, _ := json.Marshal(gotValue)
	wantCanonical, _ := json.Marshal(wantValue)
	if string(gotCanonical) != string(wantCanonical) {
		t.Fatalf("JSON = %s, want %s", gotCanonical, wantCanonical)
	}
}
