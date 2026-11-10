package jsoncanonical

import "testing"

func TestDigestUsesCanonicalJSONAndVersionedSHA256(t *testing.T) {
	if _, err := Digest([]byte(`{"a":1,"a":2}`)); err == nil {
		t.Fatal("accepted duplicate keys")
	}
	a, err := Digest([]byte(`{"a":1.0,"b":"\u4e2d"}`))
	if err != nil {
		t.Fatal(err)
	}
	b, err := Digest([]byte(`{ "b":"中", "a":1 }`))
	if err != nil || a != b || len(a) != len("exact-json-sha256-v1:")+64 {
		t.Fatalf("digests=%q,%q,%v", a, b, err)
	}
	changed, err := Digest([]byte(`{"a":2,"b":"中"}`))
	if err != nil || changed == a {
		t.Fatalf("changed digest=%q,%v", changed, err)
	}
}
