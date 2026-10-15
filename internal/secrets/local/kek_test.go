package local_test

import (
	"bytes"
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"

	localkeys "github.com/superduck-ai/open-managed-agents/internal/secrets/local"
)

func TestResolveKEKFromBase64(t *testing.T) {
	kek := make([]byte, 32)
	for i := range kek {
		kek[i] = byte(i)
	}
	got, err := localkeys.ResolveKEK(base64.StdEncoding.EncodeToString(kek), "")
	if err != nil {
		t.Fatalf("ResolveKEK: %v", err)
	}
	if !bytes.Equal(got, kek) {
		t.Fatalf("got %x want %x", got, kek)
	}
}

func TestResolveKEKFromFile(t *testing.T) {
	kek := make([]byte, 32)
	for i := range kek {
		kek[i] = byte(255 - i)
	}
	path := filepath.Join(t.TempDir(), "vault.kek")
	if err := os.WriteFile(path, []byte(base64.StdEncoding.EncodeToString(kek)+"\n"), 0o600); err != nil {
		t.Fatalf("write kek file: %v", err)
	}

	got, err := localkeys.ResolveKEK("", path)
	if err != nil {
		t.Fatalf("ResolveKEK from file: %v", err)
	}
	if !bytes.Equal(got, kek) {
		t.Fatalf("got %x want %x", got, kek)
	}
}

func TestResolveKEKErrors(t *testing.T) {
	t.Run("both set", func(t *testing.T) {
		if _, err := localkeys.ResolveKEK("AAAA", "/tmp/x"); err == nil {
			t.Fatal("configuring both kek and kek_file must fail")
		}
	})
	t.Run("neither set", func(t *testing.T) {
		if _, err := localkeys.ResolveKEK("  ", ""); err == nil {
			t.Fatal("configuring neither kek nor kek_file must fail")
		}
	})
	t.Run("wrong length", func(t *testing.T) {
		short := base64.StdEncoding.EncodeToString([]byte("short"))
		if _, err := localkeys.ResolveKEK(short, ""); err == nil {
			t.Fatal("non-32-byte KEK must fail")
		}
	})
	t.Run("invalid base64", func(t *testing.T) {
		if _, err := localkeys.ResolveKEK("not!!!base64", ""); err == nil {
			t.Fatal("invalid base64 must fail")
		}
	})
	t.Run("missing file", func(t *testing.T) {
		if _, err := localkeys.ResolveKEK("", filepath.Join(t.TempDir(), "missing")); err == nil {
			t.Fatal("missing kek_file must fail")
		}
	})
}

func TestGenerateKEK(t *testing.T) {
	a, err := localkeys.GenerateKEK()
	if err != nil {
		t.Fatalf("GenerateKEK: %v", err)
	}
	if len(a) != 32 {
		t.Fatalf("got %d bytes, want 32", len(a))
	}
	b, err := localkeys.GenerateKEK()
	if err != nil {
		t.Fatalf("GenerateKEK second: %v", err)
	}
	if bytes.Equal(a, b) {
		t.Fatal("two generated KEKs must differ")
	}
}
