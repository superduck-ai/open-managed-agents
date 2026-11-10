package jsoncanonical

import "testing"

func TestExactNumberEquality(t *testing.T) {
	for _, test := range []struct {
		left, right string
		equal       bool
	}{
		{`9007199254740993`, `9007199254740992`, false},
		{`0.123456789012345678901`, `0.123456789012345678902`, false},
		{`1e-10000`, `0`, false},
		{`9007199254740993`, `9007199254740993.0`, true},
		{`12300e-4`, `1.2300`, true},
		{`-0e999999999999999999999`, `0`, true},
		{`10e999999999999999999999`, `1e1000000000000000000000`, true},
	} {
		equal, err := Equal([]byte(`{"value":`+test.left+`}`), []byte(`{"value":`+test.right+`}`))
		if err != nil || equal != test.equal {
			t.Errorf("Equal(%s,%s)=%v,%v", test.left, test.right, equal, err)
		}
	}
}

func TestExactDigestDistinguishesLargeIntegers(t *testing.T) {
	a, err := Digest([]byte(`{"id":9007199254740993}`))
	if err != nil {
		t.Fatal(err)
	}
	b, err := Digest([]byte(`{"id":9007199254740992}`))
	if err != nil || a == b {
		t.Fatalf("digests=%s,%s err=%v", a, b, err)
	}
}

func TestLegacyDigestRequiresLosslessInput(t *testing.T) {
	legacy := "jcs-sha256-v1:24bb430971eb50f964e63784a7ad4f3411bc7cdb1659188e371150793e872da1"
	for _, test := range []struct {
		raw, digest string
		match       bool
	}{
		{`{"id":9007199254740993}`, legacy, false},
		{`{"id":9007199254740992}`, "unknown:hash", false},
		{`{"id":9007199254740992}`, legacy, true},
	} {
		match, err := MatchesLegacyDigest([]byte(test.raw), test.digest)
		if err != nil || match != test.match {
			t.Fatalf("match=%v err=%v", match, err)
		}
	}
}

func TestExactCanonicalizationIsStable(t *testing.T) {
	for _, raw := range []string{`{"b":"\u4e2d","a":1.230000e+002}`, `{"n":-0.000}`, `{"n":1e10000000000000000000000}`} {
		first, err := Canonicalize([]byte(raw))
		if err != nil {
			t.Fatal(err)
		}
		second, err := Canonicalize(first)
		if err != nil || string(first) != string(second) {
			t.Fatalf("first=%s second=%s err=%v", first, second, err)
		}
	}
}

func TestExactCanonicalWireFormat(t *testing.T) {
	raw := []byte(`{"z":100.00,"a":[1.2300,-0,9007199254740993,"\u4e2d"]}`)
	want := `{"a":[123e-2,0,9007199254740993,"中"],"z":1e2}`
	got, err := Canonicalize(raw)
	if err != nil || string(got) != want {
		t.Fatalf("canonical=%s err=%v", got, err)
	}
}
