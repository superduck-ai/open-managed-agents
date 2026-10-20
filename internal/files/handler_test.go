package files

import (
	"bytes"
	"io"
	"mime/multipart"
	"strings"
	"testing"
)

func TestValidateFilename(t *testing.T) {
	for _, filename := range []string{"", strings.Repeat("n", 256), "line\nbreak.txt", "null\x00.txt", "invalid\xff.txt", "a<b", "a>b", "a:b", "a|b", "a?b", "a*b", "a\\b", "a/b"} {
		t.Run("reject "+filename, func(t *testing.T) {
			if err := validateFilename(filename); err == nil {
				t.Fatalf("validateFilename(%q) accepted invalid filename", filename)
			}
		})
	}
	for _, filename := range []string{`quote"name.txt`, "percent%25.txt", "中文 文件🙂.txt", strings.Repeat("n", 255)} {
		t.Run("accept "+filename, func(t *testing.T) {
			if err := validateFilename(filename); err != nil {
				t.Fatalf("validateFilename(%q): %v", filename, err)
			}
		})
	}
}

func TestMultipartQuotedFilename(t *testing.T) {
	const filename = `quote"name.txt`
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", filename)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(part, "quoted filename content"); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	form, err := multipart.NewReader(&body, writer.Boundary()).ReadForm(1024)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := form.RemoveAll(); err != nil {
			t.Error(err)
		}
	})
	parsed := form.File["file"][0].Filename
	if parsed != filename {
		t.Fatalf("parsed filename = %q, want %q", parsed, filename)
	}
	if err := validateFilename(parsed); err != nil {
		t.Fatalf("multipart filename rejected: %v", err)
	}
	if key := sanitizeForKey(parsed); key != "quote_name.txt" {
		t.Fatalf("object key filename = %q, want quote_name.txt", key)
	}
}
