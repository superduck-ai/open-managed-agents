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
	"strings"
)

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

	var zipBuf bytes.Buffer
	writer := zip.NewWriter(&zipBuf)
	var directory string
	var skillMD []byte
	var total int64
	seen := map[string]struct{}{}

	for _, header := range headers {
		normalized, top, err := validateArchivePath(originalFilename(header))
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

		file, err := header.Open()
		if err != nil {
			writer.Close()
			return skillPackage{}, err
		}
		data, err := io.ReadAll(io.LimitReader(file, maxBytes+1))
		closeErr := file.Close()
		if err != nil {
			writer.Close()
			return skillPackage{}, err
		}
		if closeErr != nil {
			writer.Close()
			return skillPackage{}, closeErr
		}
		total += int64(len(data))
		if total > maxBytes {
			writer.Close()
			return skillPackage{}, packageError{Status: http.StatusRequestEntityTooLarge, Message: "Skill package exceeds maximum size"}
		}
		if normalized == directory+"/SKILL.md" {
			skillMD = data
		}

		entry, err := writer.CreateHeader(&zip.FileHeader{Name: normalized, Method: zip.Deflate})
		if err != nil {
			writer.Close()
			return skillPackage{}, err
		}
		if _, err := entry.Write(data); err != nil {
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
