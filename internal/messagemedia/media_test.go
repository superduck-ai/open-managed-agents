package messagemedia

import (
	"encoding/json"
	"testing"
)

func TestValidateInlineRejectsUnsupportedSources(t *testing.T) {
	tests := []struct {
		name  string
		block Block
	}{
		{name: "invalid base64", block: Block{Type: BlockImage, Source: Source{Type: SourceBase64, MediaType: "image/png", Data: "%%%"}}},
		{name: "empty base64", block: Block{Type: BlockImage, Source: Source{Type: SourceBase64, MediaType: "image/png", Data: ""}}},
		{name: "unsupported image type", block: Block{Type: BlockImage, Source: Source{Type: SourceBase64, MediaType: "image/tiff", Data: "AA=="}}},
		{name: "base64 text document", block: Block{Type: BlockDocument, Source: Source{Type: SourceBase64, MediaType: MediaTypeText, Data: "AA=="}}},
		{name: "text image", block: Block{Type: BlockImage, Source: Source{Type: SourceText, MediaType: MediaTypeText, Data: "x"}}},
		{name: "unknown source", block: Block{Type: BlockImage, Source: Source{Type: "ftp"}}},
		{name: "relative url", block: Block{Type: BlockImage, Source: Source{Type: SourceURL, URL: "not a url"}}},
		{name: "non http url", block: Block{Type: BlockDocument, Source: Source{Type: SourceURL, URL: "ftp://example.com/a.pdf"}}},
		{name: "host-less url", block: Block{Type: BlockImage, Source: Source{Type: SourceURL, URL: "https:///a.png"}}},
		{name: "mismatched image bytes", block: Block{Type: BlockImage, Source: Source{Type: SourceBase64, MediaType: "image/jpeg", Data: "iVBORw0KGgo="}}},
		{name: "mismatched pdf bytes", block: Block{Type: BlockDocument, Source: Source{Type: SourceBase64, MediaType: MediaTypePDF, Data: "AA=="}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := ValidateInline(test.block); err == nil {
				t.Fatal("ValidateInline() error = nil")
			}
		})
	}
}

func TestValidateInlineAcceptsContractSources(t *testing.T) {
	blocks := []Block{
		{Type: BlockImage, Source: Source{Type: SourceBase64, MediaType: "image/webp", Data: "UklGRgAAAABXRUJQVlA4IA=="}},
		{Type: BlockImage, Source: Source{Type: SourceBase64, MediaType: "image/png", Data: "iVBORw0KGgo="}},
		{Type: BlockImage, Source: Source{Type: SourceBase64, MediaType: "image/jpeg", Data: "/9j/4A=="}},
		{Type: BlockImage, Source: Source{Type: SourceBase64, MediaType: "image/gif", Data: "R0lGODlh"}},
		{Type: BlockImage, Source: Source{Type: SourceURL, URL: "https://example.com/a.png"}},
		{Type: BlockImage, Source: Source{Type: SourceFile, FileID: "file_1"}},
		{Type: BlockDocument, Source: Source{Type: SourceBase64, MediaType: MediaTypePDF, Data: "JVBERi0xLjQK"}},
		{Type: BlockDocument, Source: Source{Type: SourceText, MediaType: MediaTypeText, Data: "notes"}},
	}
	for _, block := range blocks {
		if err := ValidateInline(block); err != nil {
			t.Fatalf("ValidateInline(%+v) error = %v", block, err)
		}
	}
}

func TestParseBlockRejectsNonMediaBlocks(t *testing.T) {
	if _, err := ParseBlock(json.RawMessage(`{"type":"text","text":"hi"}`)); err == nil {
		t.Fatal("ParseBlock(text) error = nil")
	}
	if _, err := ParseBlock(json.RawMessage(`[]`)); err == nil {
		t.Fatal("ParseBlock(array) error = nil")
	}
}

func TestAttachmentPathAndReferencesPreserveDocumentFields(t *testing.T) {
	if got := AttachmentPath("sevt_1", 2, BlockImage, "image/png"); got != "/uploads/message_attachments/sevt_1/2.png" {
		t.Fatalf("AttachmentPath(image) = %q", got)
	}
	if got := AttachmentPath("sevt_1", 0, BlockDocument, MediaTypeText); got != "/uploads/message_attachments/sevt_1/0.txt" {
		t.Fatalf("AttachmentPath(text) = %q", got)
	}
	title := "Report"
	block := Block{Type: BlockDocument, Title: &title, Source: Source{Type: SourceBase64, MediaType: MediaTypePDF, Data: "JVBERi0xLjQK"}}
	raw, err := json.Marshal(block.WithFileReference("file_9"))
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != `{"type":"document","source":{"type":"file","file_id":"file_9"},"title":"Report"}` {
		t.Fatalf("WithFileReference() = %s", raw)
	}
}

func TestWithContentRestoresProviderSources(t *testing.T) {
	text, err := Block{Type: BlockDocument, Source: Source{Type: SourceFile, FileID: "file_1"}}.WithContent(MediaTypeText, []byte("notes"))
	if err != nil {
		t.Fatal(err)
	}
	if text.Source != (Source{Type: SourceText, MediaType: MediaTypeText, Data: "notes"}) {
		t.Fatalf("text source = %+v", text.Source)
	}
	image, err := Block{Type: BlockImage, Source: Source{Type: SourceFile, FileID: "file_2"}}.WithContent("image/png", []byte{0})
	if err != nil {
		t.Fatal(err)
	}
	if image.Source != (Source{Type: SourceBase64, MediaType: "image/png", Data: "AA=="}) {
		t.Fatalf("image source = %+v", image.Source)
	}
	if _, err := (Block{Type: BlockImage}).WithContent("application/zip", []byte{0}); err == nil {
		t.Fatal("WithContent(zip) error = nil")
	}
	if _, err := (Block{Type: BlockDocument}).WithContent(MediaTypeText, []byte{0xff}); err == nil {
		t.Fatal("WithContent(invalid utf8) error = nil")
	}
}

func TestCandidateMediaTypesCoverAttachmentExtensions(t *testing.T) {
	if got := CandidateMediaTypes(BlockDocument); len(got) != 2 || got[0] != MediaTypePDF || got[1] != MediaTypeText {
		t.Fatalf("CandidateMediaTypes(document) = %v", got)
	}
	if got := CandidateMediaTypes(BlockImage); len(got) != 4 || got[0] != "image/gif" || got[3] != "image/webp" {
		t.Fatalf("CandidateMediaTypes(image) = %v", got)
	}
}
