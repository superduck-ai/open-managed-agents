package jsoncanonical

import "testing"

func TestRejectInvalidCanonicalJSON(t *testing.T) {
	for _, raw := range []string{`{"key":1,"key":2}`, `{"x":NaN}`, `{"x":"\ud800"}`, `[] trailing`} {
		if _, err := Canonicalize([]byte(raw)); err == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
}

func TestCanonicalWireEquality(t *testing.T) {
	tests := []struct {
		left, right string
		equal       bool
	}{
		{`{"a":1,"b":100}`, `{"b":1e2,"a":1.0}`, true},
		{`{"value":"\u4e2d"}`, `{"value":"中"}`, true},
		{`{"value":-0}`, `{"value":0}`, true},
		{`{"a":[]}`, `{"a":{}}`, false},
		{`{"value":1}`, `{"value":"1"}`, false},
	}
	for _, test := range tests {
		equal, err := Equal([]byte(test.left), []byte(test.right))
		if err != nil || equal != test.equal {
			t.Fatalf("Equal(%s,%s) = %v,%v", test.left, test.right, equal, err)
		}
	}
}
