package vaults

import (
	"testing"
)

func TestRequireNonEmptyStringReturnsTrimmed(t *testing.T) {
	t.Parallel()

	got, err := requireNonEmptyString("  name  ", "field")
	if err != nil {
		t.Fatalf("requireNonEmptyString: %v", err)
	}
	if got != "name" {
		t.Fatalf("got %q, want trimmed identifier", got)
	}
}

func TestRequireNonEmptyStringRejectsBlank(t *testing.T) {
	t.Parallel()

	if _, err := requireNonEmptyString(" \n\t ", "field"); err == nil {
		t.Fatal("expected blank rejection")
	}
}

func TestRequireNonBlankVerbatimPreservesWhitespace(t *testing.T) {
	t.Parallel()

	const want = "  line-one\nline-two\n"
	got, err := requireNonBlankVerbatim(want, "auth.secret_value")
	if err != nil {
		t.Fatalf("requireNonBlankVerbatim: %v", err)
	}
	if got != want {
		t.Fatalf("got %q, want verbatim %q", got, want)
	}
}

func TestRequireNonBlankVerbatimRejectsBlank(t *testing.T) {
	t.Parallel()

	if _, err := requireNonBlankVerbatim(" \n\t ", "auth.secret_value"); err == nil {
		t.Fatal("expected blank rejection")
	}
}
