package sessionresource

import (
	"errors"
	"strings"
	"testing"
)

func TestMemoryInstructionsPublic4096Boundary(t *testing.T) {
	for _, text := range []string{strings.Repeat("a", 4097), strings.Repeat("中", 4097), strings.Repeat("😀", 4097), strings.Repeat("e\u0301", 2048) + "x"} {
		_, err := ParseMemoryInstructions(mustJSONString(t, text))
		if !errors.Is(err, ErrMemoryStoreInstructionsTooLong) {
			t.Fatalf("over4096 codepoints error=%v", err)
		}
	}
	for _, text := range []string{strings.Repeat("a", 4095), strings.Repeat("a", 4096), strings.Repeat("中", 4096), strings.Repeat("😀", 4096), strings.Repeat("e\u0301", 2048)} {
		got, err := ParseMemoryInstructions(mustJSONString(t, text))
		if err != nil || got != text {
			t.Fatalf("valid public4096 boundary rejected or changed: bytes=%d error=%v", len(text), err)
		}
	}
}
