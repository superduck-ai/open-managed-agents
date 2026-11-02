package tracecontext

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestParseRejectsInvalidTraceParents(t *testing.T) {
	for _, value := range []string{
		"",
		"00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7",
		"00-00000000000000000000000000000000-00f067aa0ba902b7-01",
		"00-4bf92f3577b34da6a3ce929d0e0e4736-0000000000000000-01",
		"ff-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01",
		"00-4BF92F3577B34DA6A3CE929D0E0E4736-00f067aa0ba902b7-01",
		"00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01-extra",
		"0-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01",
	} {
		if _, ok := Parse(value, ""); ok {
			t.Fatalf("accepted %q", value)
		}
	}
}

func TestParseKeepsValidContexts(t *testing.T) {
	parsed, ok := Parse(" 00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-00 ", " vendor=a ")
	if !ok || parsed.TraceParent != "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-00" || parsed.TraceState != "vendor=a" {
		t.Fatalf("parsed = %+v %v", parsed, ok)
	}
	future, ok := Parse("01-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01-future", strings.Repeat("x", 513))
	if !ok || future.TraceParent != "01-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01" || future.TraceState != "" {
		t.Fatalf("future version = %+v %v", future, ok)
	}
	if parsed, _ := Parse("00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01", "bad\nstate"); parsed.TraceState != "" {
		t.Fatal("accepted control characters in tracestate")
	}
}

func TestFromRequestRequiresOneTraceParent(t *testing.T) {
	request := httptest.NewRequest("POST", "/", nil)
	if _, ok := FromRequest(request); ok {
		t.Fatal("accepted missing header")
	}
	request.Header.Add("traceparent", "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")
	request.Header.Add("tracestate", "a=1")
	request.Header.Add("tracestate", "b=2")
	parsed, ok := FromRequest(request)
	if !ok || parsed.TraceState != "a=1,b=2" {
		t.Fatalf("parsed = %+v", parsed)
	}
	request.Header.Add("traceparent", "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b8-01")
	if _, ok := FromRequest(request); ok {
		t.Fatal("accepted duplicate traceparent")
	}
}
