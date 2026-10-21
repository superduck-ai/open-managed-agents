package main

import "testing"

func TestParseInitArgs(t *testing.T) {
	cases := []struct {
		name       string
		args       []string
		wantFile   string
		wantUpdate bool
	}{
		{name: "default init file", wantFile: ".run/dev-init.yaml"},
		{name: "default init update", args: []string{"--update"}, wantFile: ".run/dev-init.yaml", wantUpdate: true},
		{name: "init file", args: []string{"dev-init.yaml"}, wantFile: "dev-init.yaml"},
		{name: "init file update", args: []string{"dev-init.yaml", "--update"}, wantFile: "dev-init.yaml", wantUpdate: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			file, update, err := parseInitArgs(tc.args)
			if err != nil {
				t.Fatal(err)
			}
			if file != tc.wantFile || update != tc.wantUpdate {
				t.Fatalf("parseInitArgs() = %q, %v, want %q, %v", file, update, tc.wantFile, tc.wantUpdate)
			}
		})
	}
}

func TestParseInitArgsRejectsBadInput(t *testing.T) {
	cases := map[string][]string{
		"too many args": {"a.yaml", "b.yaml"},
		"legacy slot":   {"1", "a.yaml"},
	}
	for name, args := range cases {
		t.Run(name, func(t *testing.T) {
			if _, _, err := parseInitArgs(args); err == nil {
				t.Fatalf("parseInitArgs(%v) error = nil, want failure", args)
			}
		})
	}
}
