package main

import (
	"bytes"
	"encoding/json"
	"slices"
	"testing"
)

func TestSuiteRejectsIncompleteOrFailedEvidence(t *testing.T) {
	for _, tc := range []struct {
		name  string
		extra testEvent
		code  int
	}{
		{name: "nonzero exit", code: 1},
		{name: "package failure", extra: testEvent{Package: "broken", Action: "fail"}},
		{name: "unfinished package", extra: testEvent{Package: "broken", Action: "start"}},
		{name: "required S3 skipped", extra: testEvent{Package: "github.com/superduck-ai/open-managed-agents/internal/storage", Test: "TestS3CompatibleIntegration", Action: "skip"}},
		{name: "required Redis skipped", extra: testEvent{Package: "github.com/superduck-ai/open-managed-agents/internal/tunnels", Test: "TestPresenceRedis8", Action: "skip"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := suiteEvidence(t)
			if err := json.NewEncoder(data).Encode(tc.extra); err != nil {
				t.Fatal(err)
			}
			result, err := evaluateSuite(data, tc.code)
			if err != nil || result.Status != "fail" {
				t.Fatalf("result=%+v err=%v", result, err)
			}
		})
	}
	result, err := evaluateSuite(bytes.NewBufferString(""), 0)
	if err != nil || result.Status != "fail" {
		t.Fatal("empty evidence passed")
	}
}

func TestSuiteKeepsSkippedTestsUnverified(t *testing.T) {
	for _, skipped := range []bool{false, true} {
		data := suiteEvidence(t)
		want := "pass"
		if skipped {
			want = "passed_with_skips"
			if err := json.NewEncoder(data).Encode(testEvent{Package: "live", Test: "TestCloud", Action: "skip"}); err != nil {
				t.Fatal(err)
			}
		}
		result, err := evaluateSuite(data, 0)
		if err != nil || result.Status != want || (skipped && !slices.Contains(result.Skipped, "live/TestCloud")) {
			t.Fatalf("result=%+v err=%v", result, err)
		}
	}
}

func TestSuiteDoesNotInheritExternalTestTargets(t *testing.T) {
	values := isolatedSuiteEnvironment([]string{"PATH=/usr/bin", "CONFIG_FILE=shared", "REDIS_URL=shared", "TEST_API_BASE_URL=external", "TEST_MANAGED_TUNNEL_E2E=1", "LIVE_WORKER_API_URL=external", "VERIFY_BE_API_URL=external", "OMA_S3_INTEGRATION_ENDPOINT=external", "OMA_WORKER_CONTROL_IMAGE=private", "OMA_TEST_STDIO_FIXTURE=1"})
	if !slices.Equal(values, []string{"PATH=/usr/bin"}) {
		t.Fatalf("inherited external targets: %v", values)
	}
}

func suiteEvidence(t *testing.T) *bytes.Buffer {
	t.Helper()
	data := &bytes.Buffer{}
	for _, event := range []testEvent{
		{Package: "github.com/superduck-ai/open-managed-agents/internal/db", Test: "TestSessionInputIndexMigration", Action: "pass"},
		{Package: "github.com/superduck-ai/open-managed-agents/internal/storage", Test: "TestS3CompatibleIntegration", Action: "pass"},
		{Package: "github.com/superduck-ai/open-managed-agents/internal/platformauth", Test: "TestRedisEmailCodeStoreIntegration", Action: "pass"},
		{Package: "github.com/superduck-ai/open-managed-agents/internal/tunnels", Test: "TestPresenceRedis8", Action: "pass"},
		{Package: "github.com/superduck-ai/open-managed-agents/internal/tunnels", Test: "TestRequestBindingsRedis8", Action: "pass"},
		{Package: "github.com/superduck-ai/open-managed-agents/internal/tunnels", Test: "TestRequestBindingsRedis8CrossInstanceResponse", Action: "pass"},
		{Package: "github.com/superduck-ai/open-managed-agents/tests", Test: "TestEventPayloadIntegrationRealS3", Action: "pass"},
		{Package: "github.com/superduck-ai/open-managed-agents/tests", Action: "pass"},
	} {
		if err := json.NewEncoder(data).Encode(event); err != nil {
			t.Fatal(err)
		}
	}
	return data
}
