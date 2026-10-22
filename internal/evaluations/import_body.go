package evaluations

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"path"
	"strings"
)

func readImportedCases(w http.ResponseWriter, r *http.Request) ([]normalizedCase, error) {
	body, err := readLimitedBody(w, r, maxImportBodyBytes)
	if err != nil {
		return nil, err
	}
	if len(bytes.TrimSpace(body)) == 0 {
		return nil, importRequired()
	}
	payload, err := importPayload(r, body)
	if err != nil {
		return nil, err
	}
	inputs, err := parseCaseJSONL(payload)
	if err != nil {
		return nil, err
	}
	return normalizeCases(inputs)
}

func importPayload(r *http.Request, body []byte) ([]byte, error) {
	if isZip(r) {
		return jsonlFromZip(body)
	}
	return body, nil
}

func isZip(r *http.Request) bool {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	return err == nil && (mediaType == "application/zip" || mediaType == "application/x-zip-compressed")
}

func readLimitedBody(w http.ResponseWriter, r *http.Request, limit int64) ([]byte, error) {
	reader := http.MaxBytesReader(w, r.Body, limit)
	body, err := io.ReadAll(reader)
	if err == nil {
		return body, nil
	}
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		return nil, importBodyTooLarge()
	}
	return nil, invalidImportJSONL(err)
}

func parseCaseJSONL(body []byte) ([]caseInput, error) {
	body = bytes.TrimPrefix(body, []byte{0xEF, 0xBB, 0xBF})
	lines := bytes.Split(body, []byte{'\n'})
	inputs := make([]caseInput, 0, len(lines))
	for _, line := range lines {
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		if len(inputs) >= maxImportedCases {
			return nil, importTooLarge()
		}
		decoder := json.NewDecoder(bytes.NewReader(line))
		decoder.DisallowUnknownFields()
		var input caseInput
		if err := decoder.Decode(&input); err != nil {
			return nil, invalidImportJSONL(err)
		}
		if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
			return nil, invalidImportJSONL(errTrailingJSON)
		}
		inputs = append(inputs, input)
	}
	return inputs, nil
}

func jsonlFromZip(body []byte) ([]byte, error) {
	reader, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		return nil, invalidImportZip(err)
	}
	member, err := zipJSONLMember(reader.File)
	if err != nil {
		return nil, err
	}
	return readZipMember(member)
}

type zipNamedFile struct {
	name string
	file *zip.File
}

func zipJSONLMember(files []*zip.File) (*zip.File, error) {
	named, err := zipNamedFiles(files)
	if err != nil {
		return nil, err
	}
	root := sharedZipRoot(named)
	var member *zip.File
	for _, item := range named {
		name := item.name
		if root != "" {
			name = strings.TrimPrefix(name, root+"/")
		}
		if zipCompanion(name) {
			continue
		}
		if strings.Contains(name, "/") || !strings.HasSuffix(strings.ToLower(name), ".jsonl") {
			return nil, invalidImportZip(errZipMember)
		}
		if member != nil {
			return nil, invalidImportZip(errZipMember)
		}
		member = item.file
	}
	if member == nil {
		return nil, invalidImportZip(errZipMember)
	}
	return member, nil
}

func zipNamedFiles(files []*zip.File) ([]zipNamedFile, error) {
	named := make([]zipNamedFile, 0, len(files))
	for _, file := range files {
		if file.FileInfo().IsDir() {
			continue
		}
		name, ok := zipMemberPath(file.Name)
		if !ok {
			return nil, invalidImportZip(errZipMember)
		}
		if zipIgnored(name) {
			continue
		}
		named = append(named, zipNamedFile{name: name, file: file})
	}
	return named, nil
}

func zipMemberPath(name string) (string, bool) {
	slash := strings.TrimPrefix(strings.ReplaceAll(name, "\\", "/"), "/")
	if slash == "" || slash == "." {
		return "", false
	}
	for _, part := range strings.Split(slash, "/") {
		if part == "" || part == "." || part == ".." {
			return "", false
		}
	}
	cleaned := path.Clean(slash)
	if cleaned != slash {
		return "", false
	}
	return slash, true
}

func zipIgnored(name string) bool {
	if path.Base(name) == ".DS_Store" {
		return true
	}
	for _, part := range strings.Split(name, "/") {
		if part == "__MACOSX" {
			return true
		}
	}
	return false
}

func zipCompanion(name string) bool {
	lower := strings.ToLower(name)
	return strings.HasPrefix(lower, "input_files/") || strings.HasPrefix(lower, "reference_files/")
}

func sharedZipRoot(files []zipNamedFile) string {
	root := ""
	for _, item := range files {
		slash := strings.Index(item.name, "/")
		if slash <= 0 {
			return ""
		}
		segment := item.name[:slash]
		if root == "" {
			root = segment
			continue
		}
		if segment != root {
			return ""
		}
	}
	return root
}

func readZipMember(member *zip.File) ([]byte, error) {
	if member.UncompressedSize64 > uint64(maxImportBodyBytes) {
		return nil, invalidImportZip(errZipMember)
	}
	opened, err := member.Open()
	if err != nil {
		return nil, invalidImportZip(err)
	}
	defer opened.Close()
	payload, err := io.ReadAll(io.LimitReader(opened, maxImportBodyBytes+1))
	if err != nil {
		return nil, invalidImportZip(err)
	}
	if int64(len(payload)) > maxImportBodyBytes {
		return nil, importBodyTooLarge()
	}
	return payload, nil
}
