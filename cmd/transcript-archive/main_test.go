package main

import (
	"bytes"
	"testing"
)

func TestRejectInvalidArchiveCommand(t *testing.T) {
	for _, args := range [][]string{{"-mode", "delete"}, {"-mode", "restore"}, {"-workspace", "not-a-uuid"}} {
		var output bytes.Buffer
		if err := run(args, &output, &output); err == nil {
			t.Fatal("invalid command accepted")
		}
	}
}
