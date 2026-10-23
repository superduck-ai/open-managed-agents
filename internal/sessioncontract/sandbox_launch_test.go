package sessioncontract

import (
	"testing"
	"time"
)

func TestSandboxLaunchReadsStringAndObjectMetadata(t *testing.T) {
	if SandboxTimeout(nil) != 0 || SandboxEnv(nil) != nil {
		t.Fatal("empty metadata applied a launch override")
	}
	if SandboxTimeout([]byte(`{"timeout_seconds":"0"}`)) != 0 || SandboxTimeout([]byte(`{"timeout_seconds":86401}`)) != 0 {
		t.Fatal("timeout outside 1-86400 was accepted")
	}
	if SandboxEnv([]byte(`{"env":"not-json"}`)) != nil || SandboxEnv([]byte(`{"env":{}}`)) != nil {
		t.Fatal("invalid env was accepted")
	}
	stringMetadata := []byte(`{"timeout_seconds":"60","env":"{\"TRACE\":\"1\"}","experiment_id":"evalexp_test"}`)
	if SandboxTimeout(stringMetadata) != 60*time.Second || SandboxEnv(stringMetadata)["TRACE"] != "1" {
		t.Fatalf("string metadata timeout=%s env=%v", SandboxTimeout(stringMetadata), SandboxEnv(stringMetadata))
	}
	objectMetadata := []byte(`{"timeout_seconds":60,"env":{"TRACE":"1"},"mcp_allowed_hosts":[]}`)
	if SandboxTimeout(objectMetadata) != 60*time.Second || SandboxEnv(objectMetadata)["TRACE"] != "1" {
		t.Fatalf("object metadata timeout=%s env=%v", SandboxTimeout(objectMetadata), SandboxEnv(objectMetadata))
	}
	if SandboxTimeout([]byte(`{"experiment_id":"evalexp_test"}`)) != 0 || SandboxEnv([]byte(`{"experiment_id":"evalexp_test"}`)) != nil {
		t.Fatal("metadata without timeout or env applied a launch override")
	}
}
