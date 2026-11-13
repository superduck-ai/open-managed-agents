package skills

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/superduck-ai/open-managed-agents/internal/httpapi"
)

const maxSkillFilePreviewBytes = 256 * 1024

type skillFilePreview struct {
	Path        string `json:"path"`
	Content     string `json:"content"`
	Previewable bool   `json:"previewable"`
	Reason      string `json:"reason"`
}

func (h *Handler) loadPackage(ctx context.Context, bucket, key string, pkg skillPackage, limit int64) (skillPackage, error) {
	if bucket != h.store.Name() {
		return pkg, errSkillStorageMismatch
	}
	object, err := h.store.Open(ctx, key, nil)
	if err != nil {
		return pkg, err
	}
	defer object.Body.Close()
	archive, err := readWithinBudget(object.Body, limit)
	if err != nil {
		return pkg, err
	}
	sum := sha256.Sum256(archive)
	if hex.EncodeToString(sum[:]) != pkg.SHA256 {
		return pkg, errSkillIntegrityFailed
	}
	pkg.Zip = archive
	return pkg, nil
}

func (h *Handler) servePackage(w http.ResponseWriter, r *http.Request, bucket, key string, pkg skillPackage, limit int64) error {
	pkg, err := h.loadPackage(r.Context(), bucket, key, pkg, limit)
	if err != nil {
		return err
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if r.URL.Query().Get("download") == "1" {
		w.Header().Set("Content-Type", skillArchiveContentType)
		w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": pkg.Directory + ".zip"}))
		w.Header().Set("Content-Length", strconv.Itoa(len(pkg.Zip)))
		_, err = w.Write(pkg.Zip)
		return err
	}
	if path := r.URL.Query().Get("path"); path != "" {
		preview, err := previewPackageFile(pkg, path)
		if err != nil {
			return err
		}
		httpapi.WriteJSON(w, http.StatusOK, preview)
		return nil
	}
	result, err := systemPackageDTO(pkg)
	if err != nil {
		return err
	}
	httpapi.WriteJSON(w, http.StatusOK, result)
	return nil
}

func previewPackageFile(pkg skillPackage, path string) (skillFilePreview, error) {
	result := skillFilePreview{Path: path}
	if !strings.HasPrefix(path, pkg.Directory+"/") || strings.Contains(path, "\\") {
		return result, skillPreviewFileNotFound()
	}
	for part := range strings.SplitSeq(path, "/") {
		if part == ".." || part == "." || part == "" {
			return result, skillPreviewFileNotFound()
		}
	}
	reader, err := zip.NewReader(bytes.NewReader(pkg.Zip), int64(len(pkg.Zip)))
	if err != nil {
		return result, err
	}
	for _, file := range reader.File {
		if file.Name != path || file.FileInfo().IsDir() {
			continue
		}
		if file.UncompressedSize64 > maxSkillFilePreviewBytes {
			result.Reason = "too_large"
			return result, nil
		}
		data, err := readZipFile(file, maxSkillFilePreviewBytes)
		if err != nil {
			return result, err
		}
		if !utf8.Valid(data) || bytes.ContainsFunc(data, func(r rune) bool {
			return unicode.IsControl(r) && r != '\n' && r != '\r' && r != '\t'
		}) {
			result.Reason = "binary"
			return result, nil
		}
		result.Content, result.Previewable = string(data), true
		return result, nil
	}
	return result, skillPreviewFileNotFound()
}
