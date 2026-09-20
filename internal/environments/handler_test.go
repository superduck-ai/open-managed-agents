package environments

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDecodeEnvironmentWorkStopForceRejectsInvalidBodies(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "malformed", body: `{`},
		{name: "non object", body: `[]`},
		{name: "oversized", body: `{"force":"` + strings.Repeat("x", maxEnvironmentBodySize) + `"}`},
		{name: "invalid force", body: `{"force":"yes"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(tt.body))

			if _, err := decodeEnvironmentWorkStopForce(httptest.NewRecorder(), request); err == nil {
				t.Fatal("decodeEnvironmentWorkStopForce() error = nil")
			}
		})
	}
}

func TestDecodeEnvironmentWorkStopForcePreservesEmptyBody(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/", nil)

	force, err := decodeEnvironmentWorkStopForce(httptest.NewRecorder(), request)
	if err != nil {
		t.Fatalf("decodeEnvironmentWorkStopForce() error = %v", err)
	}
	if force {
		t.Fatal("decodeEnvironmentWorkStopForce() = true, want false")
	}
}

func TestDecodeEnvironmentWorkStopForceDecodesFirstObject(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"force":true} trailing`))

	force, err := decodeEnvironmentWorkStopForce(httptest.NewRecorder(), request)
	if err != nil {
		t.Fatalf("decodeEnvironmentWorkStopForce() error = %v", err)
	}
	if !force {
		t.Fatal("decodeEnvironmentWorkStopForce() = false, want true")
	}
}

func TestValidateMetadataRejectsReservedInternalKind(t *testing.T) {
	if err := validateMetadata(map[string]string{"internal_kind": "dream_default_environment"}); err == nil {
		t.Fatal("validateMetadata() error = nil, want reserved internal_kind rejection")
	}
}
