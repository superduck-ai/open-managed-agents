package evaluations

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
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
	var member *zip.File
	for _, file := range reader.File {
		if file.FileInfo().IsDir() {
			continue
		}
		if !strings.HasSuffix(strings.ToLower(file.Name), ".jsonl") {
			return nil, invalidImportZip(errZipMember)
		}
		if member != nil {
			return nil, invalidImportZip(errZipMember)
		}
		member = file
	}
	if member == nil || member.UncompressedSize64 > uint64(maxImportBodyBytes) {
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
