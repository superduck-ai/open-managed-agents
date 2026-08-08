package db

import (
	"slices"
	"testing"
)

func TestDecodeVaultIDList(t *testing.T) {
	cases := []struct {
		name    string
		raw     string
		want    []string
		wantErr bool
	}{
		{name: "empty", raw: ``, want: nil},
		{name: "empty array", raw: `[]`, want: []string{}},
		{name: "ordered ids", raw: `["vlt_a","vlt_b"]`, want: []string{"vlt_a", "vlt_b"}},
		{name: "trim blanks", raw: `[" vlt_a ",""]`, want: []string{"vlt_a"}},
		{name: "invalid json", raw: `{`, wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := decodeVaultIDList([]byte(tc.raw))
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("decodeVaultIDList: %v", err)
			}
			if !slices.Equal(got, tc.want) {
				t.Fatalf("got %v want %v", got, tc.want)
			}
		})
	}
}
