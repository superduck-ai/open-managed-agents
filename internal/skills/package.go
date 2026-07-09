package skills

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"path"
	"sort"
	"strings"
)

const MaxSkillPackageBytes int64 = 8 * 1024 * 1024

type packageError struct {
	Status  int
	Message string
}

func (e packageError) Error() string {
	return e.Message
}

type skillPackage struct {
	Zip         []byte
	Directory   string
	Name        string
	Description string
	Size        int64
	SHA256      string
}

func readSkillPackage(w http.ResponseWriter, r *http.Request, maxBytes int64) (skillPackage, error) {
	r.Body = http.MaxBytesReader(w, r.Body, maxBytes+1024*1024)
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			return skillPackage{}, packageError{Status: http.StatusRequestEntityTooLarge, Message: "Skill package exceeds maximum size"}
		}
		return skillPackage{}, packageError{Status: http.StatusBadRequest, Message: "Expected multipart form data with files[] fields"}
	}
	defer cleanupMultipart(r.MultipartForm)

	headers := collectSkillFileHeaders(r.MultipartForm)
	if len(headers) == 0 {
		return skillPackage{}, packageError{Status: http.StatusBadRequest, Message: "Missing required multipart field: files[]"}
	}

	if len(headers) == 1 && isSkillArchiveFilename(originalFilename(headers[0])) {
		file, err := headers[0].Open()
		if err != nil {
			return skillPackage{}, err
		}
		data, err := io.ReadAll(io.LimitReader(file, maxBytes+1))
		closeErr := file.Close()
		if err != nil {
			return skillPackage{}, err
		}
		if closeErr != nil {
			return skillPackage{}, closeErr
		}
		if int64(len(data)) > maxBytes {
			return skillPackage{}, packageError{Status: http.StatusRequestEntityTooLarge, Message: "Skill package exceeds maximum size"}
		}
		return skillPackageFromArchive(data, maxBytes)
	}

	var files []normalizedSkillFile
	var total int64
	for _, header := range headers {
		normalized, top, err := validateArchivePath(originalFilename(header))
		if err != nil {
			return skillPackage{}, packageError{Status: http.StatusBadRequest, Message: err.Error()}
		}
		file, err := header.Open()
		if err != nil {
			return skillPackage{}, err
		}
		data, err := io.ReadAll(io.LimitReader(file, maxBytes+1))
		closeErr := file.Close()
		if err != nil {
			return skillPackage{}, err
		}
		if closeErr != nil {
			return skillPackage{}, closeErr
		}
		total += int64(len(data))
		if total > maxBytes {
			return skillPackage{}, packageError{Status: http.StatusRequestEntityTooLarge, Message: "Skill package exceeds maximum size"}
		}
		_ = top
		files = append(files, normalizedSkillFile{Name: normalized, Data: data})
	}
	return skillPackageFromFiles(files, maxBytes)
}

type normalizedSkillFile struct {
	Name string
	Data []byte
}

func skillPackageFromArchive(data []byte, maxBytes int64) (skillPackage, error) {
	if len(data) == 0 {
		return skillPackage{}, packageError{Status: http.StatusBadRequest, Message: "Skill package must contain files"}
	}
	if int64(len(data)) > maxBytes {
		return skillPackage{}, packageError{Status: http.StatusRequestEntityTooLarge, Message: "Skill package exceeds maximum size"}
	}
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return skillPackage{}, packageError{Status: http.StatusBadRequest, Message: "Skill package archive is not a valid zip file"}
	}

	var files []normalizedSkillFile
	var total int64
	for _, file := range reader.File {
		name := strings.ReplaceAll(file.Name, "\\", "/")
		if shouldSkipArchiveEntry(name) || file.FileInfo().IsDir() {
			continue
		}
		normalized, _, err := validateArchivePath(name)
		if err != nil {
			return skillPackage{}, packageError{Status: http.StatusBadRequest, Message: err.Error()}
		}
		data, err := readZipFile(file, maxBytes-total)
		if err != nil {
			return skillPackage{}, err
		}
		total += int64(len(data))
		if total > maxBytes {
			return skillPackage{}, packageError{Status: http.StatusRequestEntityTooLarge, Message: "Skill package exceeds maximum size"}
		}
		files = append(files, normalizedSkillFile{Name: normalized, Data: data})
	}
	return skillPackageFromFiles(files, maxBytes)
}

func skillPackageFromFiles(files []normalizedSkillFile, maxBytes int64) (skillPackage, error) {
	sort.Slice(files, func(i, j int) bool {
		return files[i].Name < files[j].Name
	})

	var zipBuf bytes.Buffer
	writer := zip.NewWriter(&zipBuf)
	var directory string
	var skillMD []byte
	var total int64
	seen := map[string]struct{}{}

	for _, file := range files {
		normalized, top, err := validateArchivePath(file.Name)
		if err != nil {
			writer.Close()
			return skillPackage{}, packageError{Status: http.StatusBadRequest, Message: err.Error()}
		}
		if directory == "" {
			directory = top
		} else if directory != top {
			writer.Close()
			return skillPackage{}, packageError{Status: http.StatusBadRequest, Message: "All skill files must be under a single top-level directory"}
		}
		if _, ok := seen[normalized]; ok {
			writer.Close()
			return skillPackage{}, packageError{Status: http.StatusBadRequest, Message: "Duplicate skill file path: " + normalized}
		}
		seen[normalized] = struct{}{}

		total += int64(len(file.Data))
		if total > maxBytes {
			writer.Close()
			return skillPackage{}, packageError{Status: http.StatusRequestEntityTooLarge, Message: "Skill package exceeds maximum size"}
		}
		if normalized == directory+"/SKILL.md" {
			skillMD = file.Data
		}

		entry, err := writer.CreateHeader(&zip.FileHeader{Name: normalized, Method: zip.Deflate})
		if err != nil {
			writer.Close()
			return skillPackage{}, err
		}
		if _, err := entry.Write(file.Data); err != nil {
			writer.Close()
			return skillPackage{}, err
		}
	}
	if err := writer.Close(); err != nil {
		return skillPackage{}, err
	}
	if directory == "" {
		return skillPackage{}, packageError{Status: http.StatusBadRequest, Message: "Skill package must contain files"}
	}
	if len(skillMD) == 0 {
		return skillPackage{}, packageError{Status: http.StatusBadRequest, Message: "Skill package must contain SKILL.md at the top level"}
	}

	sum := sha256.Sum256(zipBuf.Bytes())
	meta := parseSkillMetadata(skillMD, directory)
	return skillPackage{
		Zip:         zipBuf.Bytes(),
		Directory:   directory,
		Name:        meta.Name,
		Description: meta.Description,
		Size:        int64(zipBuf.Len()),
		SHA256:      hex.EncodeToString(sum[:]),
	}, nil
}

func isSkillArchiveFilename(filename string) bool {
	name := strings.ToLower(strings.TrimSpace(filename))
	return strings.HasSuffix(name, ".zip") || strings.HasSuffix(name, ".skill")
}

func shouldSkipArchiveEntry(name string) bool {
	name = strings.TrimSpace(strings.ReplaceAll(name, "\\", "/"))
	if name == "" {
		return true
	}
	parts := strings.Split(name, "/")
	if len(parts) == 0 {
		return true
	}
	if parts[0] == "__MACOSX" {
		return true
	}
	for _, part := range parts {
		if part == ".DS_Store" || strings.HasPrefix(part, "._") {
			return true
		}
	}
	return false
}

func readZipFile(file *zip.File, remaining int64) ([]byte, error) {
	if remaining < 0 {
		remaining = 0
	}
	rc, err := file.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	data, err := io.ReadAll(io.LimitReader(rc, remaining+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > remaining {
		return nil, packageError{Status: http.StatusRequestEntityTooLarge, Message: "Skill package exceeds maximum size"}
	}
	return data, nil
}

func collectSkillFileHeaders(form *multipart.Form) []*multipart.FileHeader {
	if form == nil || form.File == nil {
		return nil
	}
	var headers []*multipart.FileHeader
	headers = append(headers, form.File["files[]"]...)
	headers = append(headers, form.File["files"]...)
	return headers
}

func originalFilename(header *multipart.FileHeader) string {
	disposition := header.Header.Get("Content-Disposition")
	if disposition != "" {
		if _, params, err := mime.ParseMediaType(disposition); err == nil {
			if filename := params["filename"]; filename != "" {
				return filename
			}
		}
	}
	return header.Filename
}

func validateArchivePath(filename string) (string, string, error) {
	filename = strings.ReplaceAll(strings.TrimSpace(filename), "\\", "/")
	if filename == "" {
		filename = "anonymous_file"
	}
	if strings.HasPrefix(filename, "/") || strings.Contains(filename, "\x00") {
		return "", "", fmt.Errorf("Invalid skill file path: %s", filename)
	}
	cleaned := path.Clean(filename)
	if cleaned == "." || cleaned == "/" || strings.HasPrefix(cleaned, "../") || cleaned == ".." || path.IsAbs(cleaned) {
		return "", "", fmt.Errorf("Invalid skill file path: %s", filename)
	}
	parts := strings.Split(cleaned, "/")
	if len(parts) < 2 || parts[0] == "" || parts[0] == "." || parts[0] == ".." {
		return "", "", fmt.Errorf("Skill files must be under a top-level directory")
	}
	for _, part := range parts {
		if part == "" || part == "." || part == ".." {
			return "", "", fmt.Errorf("Invalid skill file path: %s", filename)
		}
	}
	return cleaned, parts[0], nil
}

func cleanupMultipart(form *multipart.Form) {
	if form != nil {
		_ = form.RemoveAll()
	}
}
