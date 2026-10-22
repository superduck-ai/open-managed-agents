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

type importBundle struct {
	cases      []normalizedCase
	companions []zipCompanionBytes
}

type zipCompanionBytes struct {
	kind string
	rel  string
	body []byte
}

func readImportedCases(w http.ResponseWriter, r *http.Request) (importBundle, error) {
	body, err := readLimitedBody(w, r, maxImportBodyBytes)
	if err != nil {
		return importBundle{}, err
	}
	if len(bytes.TrimSpace(body)) == 0 {
		return importBundle{}, importRequired()
	}
	payload := body
	var companions []zipCompanionBytes
	if isZip(r) {
		payload, companions, err = readZipImport(body)
		if err != nil {
			return importBundle{}, err
		}
	}
	inputs, err := parseCaseJSONL(payload)
	if err != nil {
		return importBundle{}, err
	}
	cases, err := normalizeCases(inputs)
	if err != nil {
		return importBundle{}, err
	}
	return importBundle{cases: cases, companions: companions}, nil
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

func readZipImport(body []byte) ([]byte, []zipCompanionBytes, error) {
	reader, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		return nil, nil, invalidImportZip(err)
	}
	named, err := zipNamedFiles(reader.File)
	if err != nil {
		return nil, nil, err
	}
	return zipImportMembers(named)
}

func zipImportMembers(named []zipNamedFile) ([]byte, []zipCompanionBytes, error) {
	root := sharedZipRoot(named)
	var member *zip.File
	companions := make([]zipCompanionBytes, 0)
	seen := make(map[string]struct{})
	for _, item := range named {
		name := stripZipRoot(item.name, root)
		kind, rel, companion := splitCompanion(name)
		if companion {
			key := kind + "/" + strings.ToLower(rel)
			if _, ok := seen[key]; ok {
				return nil, nil, invalidImportZip(errZipMember)
			}
			seen[key] = struct{}{}
			payload, readErr := readZipMember(item.file)
			if readErr != nil {
				return nil, nil, readErr
			}
			companions = append(companions, zipCompanionBytes{kind: kind, rel: rel, body: payload})
			continue
		}
		if strings.Contains(name, "/") || !strings.HasSuffix(strings.ToLower(name), ".jsonl") {
			return nil, nil, invalidImportZip(errZipMember)
		}
		if member != nil {
			return nil, nil, invalidImportZip(errZipMember)
		}
		member = item.file
	}
	if member == nil {
		return nil, nil, invalidImportZip(errZipMember)
	}
	payload, err := readZipMember(member)
	if err != nil {
		return nil, nil, err
	}
	return payload, companions, nil
}

func stripZipRoot(name, root string) string {
	if root == "" {
		return name
	}
	return strings.TrimPrefix(name, root+"/")
}

func splitCompanion(name string) (string, string, bool) {
	lower := strings.ToLower(name)
	for _, prefix := range []struct {
		path string
		kind string
	}{
		{path: "input_files/", kind: "input"},
		{path: "reference_files/", kind: "reference"},
	} {
		if strings.HasPrefix(lower, prefix.path) {
			rel := name[len(prefix.path):]
			if rel == "" {
				return "", "", false
			}
			return prefix.kind, rel, true
		}
	}
	return "", "", false
}

type zipNamedFile struct {
	name string
	file *zip.File
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
