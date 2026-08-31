package jsonx

import (
	"encoding/json"
	"testing"
)

type testValue struct {
	Name string `json:"name"`
}

func TestDecodeRejectsInvalidJSON(t *testing.T) {
	if _, err := Decode[testValue](json.RawMessage(`{"name":`)); err == nil {
		t.Fatal("Decode() error = nil")
	}
}

func TestEncodeDecode(t *testing.T) {
	raw, err := Encode(testValue{Name: "deployment"})
	if err != nil {
		t.Fatalf("Encode(): %v", err)
	}
	value, err := Decode[testValue](raw)
	if err != nil {
		t.Fatalf("Decode(): %v", err)
	}
	if value.Name != "deployment" {
		t.Fatalf("Decode().Name = %q", value.Name)
	}
}

func TestIsNull(t *testing.T) {
	for _, test := range []struct {
		name string
		raw  json.RawMessage
		want bool
	}{
		{name: "empty", want: false},
		{name: "value", raw: json.RawMessage(`{}`), want: false},
		{name: "null", raw: json.RawMessage(" \nnull\t"), want: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := IsNull(test.raw); got != test.want {
				t.Fatalf("IsNull() = %t, want %t", got, test.want)
			}
		})
	}
}

func TestDefaultOnlyReplacesAbsentJSON(t *testing.T) {
	if got := string(Default(nil, `{}`)); got != `{}` {
		t.Fatalf("Default(nil) = %q", got)
	}
	if got := string(Default(json.RawMessage(`null`), `{}`)); got != `null` {
		t.Fatalf("Default(null) = %q", got)
	}
}
