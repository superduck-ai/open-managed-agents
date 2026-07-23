package ids

import (
	"errors"
	"io"
	"strings"
	"testing"
)

func TestNewRejectsBiasedBytes(t *testing.T) {
	// First read starts with the reject range; remaining zeros should fill the ID.
	randomBytes := append(
		[]byte{248, 249, 250, 251, 252, 253, 254, 255},
		make([]byte, 2*randomReadSize)...,
	)

	got, err := generate("test_", strings.NewReader(string(randomBytes)))
	if err != nil {
		t.Fatalf("generate() error = %v", err)
	}
	want := "test_" + strings.Repeat("0", randomIDLength)
	if got != want {
		t.Fatalf("generate() = %q, want %q", got, want)
	}
}

func TestNewReturnsRandomSourceError(t *testing.T) {
	wantErr := errors.New("random source unavailable")
	_, err := generate("test_", errorReader{err: wantErr})
	if !errors.Is(err, wantErr) {
		t.Fatalf("generate() error = %v, want wrapped %v", err, wantErr)
	}
}

func TestNewFormat(t *testing.T) {
	got, err := New("claude_chat_")
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if len(got) != len("claude_chat_")+randomIDLength {
		t.Fatalf("New() length = %d, want %d", len(got), len("claude_chat_")+randomIDLength)
	}
	for _, character := range strings.TrimPrefix(got, "claude_chat_") {
		if !strings.ContainsRune(alphabet, character) {
			t.Fatalf("New() contains non-Base62 character %q", character)
		}
	}
}

type errorReader struct {
	err error
}

func (r errorReader) Read([]byte) (int, error) {
	return 0, r.err
}

var _ io.Reader = errorReader{}
