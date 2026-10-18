package vaults

import (
	"encoding/json"
	"testing"
)

func TestEnvironmentCredentialCreateInjectionDefaults(t *testing.T) {
	for _, tc := range []struct {
		name   string
		input  string
		header bool
		body   bool
		fail   bool
	}{
		{name: "omitted", header: true, body: true},
		{name: "header only", input: `,"injection_location":{"header":true}`, header: true},
		{name: "body only", input: `,"injection_location":{"body":true}`, body: true},
		{name: "both", input: `,"injection_location":{"header":true,"body":true}`, header: true, body: true},
		{name: "empty", input: `,"injection_location":{}`, fail: true},
		{name: "disabled", input: `,"injection_location":{"header":false,"body":false}`, fail: true},
		{name: "null", input: `,"injection_location":null`, fail: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw := json.RawMessage(`{"type":"environment_variable","secret_name":"TEST_TOKEN","secret_value":"synthetic-value","networking":{"type":"unrestricted"}` + tc.input + `}`)
			state, err := normalizeCredentialAuthForCreate(raw)
			if tc.fail {
				if err == nil {
					t.Fatal("invalid location accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			auth, err := decodeCredentialAuth(state.PublicAuth)
			if err != nil {
				t.Fatal(err)
			}
			got := auth.value.(*environmentVariableCredentialAuth).InjectionLocation
			if got.Header != tc.header || got.Body != tc.body {
				t.Fatalf("location=%+v, want header=%v body=%v", got, tc.header, tc.body)
			}
		})
	}
}
