package messagemedia

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"net/url"
	"path"
	"slices"
	"strconv"
	"unicode/utf8"

	"github.com/superduck-ai/open-managed-agents/internal/sandboxmount"
)

const (
	BlockImage    = "image"
	BlockDocument = "document"

	SourceBase64 = "base64"
	SourceText   = "text"
	SourceURL    = "url"
	SourceFile   = "file"

	MediaTypePDF  = "application/pdf"
	MediaTypeText = "text/plain"
)

var imageExtensions = map[string]string{
	"image/jpeg": ".jpg",
	"image/png":  ".png",
	"image/gif":  ".gif",
	"image/webp": ".webp",
}

var documentExtensions = map[string]string{
	MediaTypePDF:  ".pdf",
	MediaTypeText: ".txt",
}

type Source struct {
	Type      string `json:"type"`
	MediaType string `json:"media_type,omitempty"`
	Data      string `json:"data,omitempty"`
	URL       string `json:"url,omitempty"`
	FileID    string `json:"file_id,omitempty"`
}

type Block struct {
	Type    string  `json:"type"`
	Source  Source  `json:"source"`
	Title   *string `json:"title,omitempty"`
	Context *string `json:"context,omitempty"`
}

func IsMediaBlock(blockType string) bool {
	return blockType == BlockImage || blockType == BlockDocument
}

func ParseBlock(raw json.RawMessage) (Block, error) {
	var block Block
	if err := json.Unmarshal(raw, &block); err != nil {
		return Block{}, errors.New("media content block is invalid")
	}
	if !IsMediaBlock(block.Type) {
		return Block{}, fmt.Errorf("content block type %q is not a media block", block.Type)
	}
	return block, nil
}

func (b Block) Stored() bool {
	switch b.Source.Type {
	case SourceBase64, SourceText, SourceFile:
		return true
	default:
		return false
	}
}

func (b Block) InlineMediaType() string {
	if b.Source.Type == SourceText {
		return MediaTypeText
	}
	return b.Source.MediaType
}

func (b Block) InlineContent() ([]byte, error) {
	switch b.Source.Type {
	case SourceBase64:
		data, err := base64.StdEncoding.DecodeString(b.Source.Data)
		if err != nil {
			return nil, errors.New("source data must be valid base64")
		}
		if len(data) == 0 {
			return nil, errors.New("source data must not be empty")
		}
		return data, nil
	case SourceText:
		return []byte(b.Source.Data), nil
	default:
		return nil, fmt.Errorf("source type %q has no inline content", b.Source.Type)
	}
}

func ValidateInline(b Block) error {
	switch b.Source.Type {
	case SourceURL:
		parsed, err := url.Parse(b.Source.URL)
		if err != nil || !parsed.IsAbs() || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
			return errors.New("source url must be an absolute http or https URL")
		}
		return nil
	case SourceFile:
		return nil
	case SourceText:
		if b.Type != BlockDocument {
			return errors.New("text sources are only accepted for document blocks")
		}
	case SourceBase64:
	default:
		return fmt.Errorf("source type %q is not supported", b.Source.Type)
	}
	if !SupportedMediaType(b.Type, b.InlineMediaType()) {
		return fmt.Errorf("%s media_type %q is not supported", b.Type, b.InlineMediaType())
	}
	if b.Type == BlockDocument && b.Source.Type == SourceBase64 && b.Source.MediaType != MediaTypePDF {
		return fmt.Errorf("base64 document media_type must be %q", MediaTypePDF)
	}
	data, err := b.InlineContent()
	if err != nil {
		return err
	}
	if !ContentMatches(b.InlineMediaType(), data) {
		return fmt.Errorf("source data does not match media_type %q", b.InlineMediaType())
	}
	return nil
}

func ContentMatches(mediaType string, head []byte) bool {
	if mediaType == MediaTypeText {
		return true
	}
	return http.DetectContentType(head) == mediaType
}

func CandidateMediaTypes(blockType string) []string {
	if blockType == BlockDocument {
		return slices.Sorted(maps.Keys(documentExtensions))
	}
	return slices.Sorted(maps.Keys(imageExtensions))
}

func SupportedMediaType(blockType, mediaType string) bool {
	switch blockType {
	case BlockImage:
		_, ok := imageExtensions[mediaType]
		return ok
	case BlockDocument:
		_, ok := documentExtensions[mediaType]
		return ok
	default:
		return false
	}
}

func AttachmentPath(eventExternalID string, index int, blockType, mediaType string) string {
	extension := imageExtensions[mediaType]
	if blockType == BlockDocument {
		extension = documentExtensions[mediaType]
	}
	return path.Join(sandboxmount.MessageAttachmentsRoot, eventExternalID, strconv.Itoa(index)+extension)
}

func (b Block) WithFileReference(fileID string) Block {
	b.Source = Source{Type: SourceFile, FileID: fileID}
	return b
}

func (b Block) WithContent(mediaType string, data []byte) (Block, error) {
	if !SupportedMediaType(b.Type, mediaType) {
		return Block{}, fmt.Errorf("%s media_type %q is not supported", b.Type, mediaType)
	}
	if b.Type == BlockDocument && mediaType == MediaTypeText {
		if !utf8.Valid(data) {
			return Block{}, errors.New("text document is not valid UTF-8")
		}
		b.Source = Source{Type: SourceText, MediaType: MediaTypeText, Data: string(data)}
		return b, nil
	}
	b.Source = Source{Type: SourceBase64, MediaType: mediaType, Data: base64.StdEncoding.EncodeToString(data)}
	return b, nil
}
