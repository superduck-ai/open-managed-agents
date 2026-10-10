// Package memorypath defines the path contract shared by Memory API and Filestore.
package memorypath

import (
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

const MaxBytes = 1024

func Validate(path string) error {
	if path == "" || len([]byte(path)) > MaxBytes {
		return errors.New("path must be between 1 and 1024 bytes")
	}
	if !utf8.ValidString(path) {
		return errors.New("path must be valid UTF-8")
	}
	if !strings.HasPrefix(path, "/") {
		return errors.New("path must start with /")
	}
	if path == "/" {
		return errors.New("path must contain at least one segment")
	}
	if strings.Contains(path, "//") || strings.HasSuffix(path, "/") {
		return errors.New("path must not contain empty segments")
	}
	if !norm.NFC.IsNormalString(path) {
		return errors.New("path must be NFC-normalized")
	}
	for _, r := range path {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return errors.New("path must not contain control or format characters")
		}
	}
	for _, segment := range strings.Split(strings.TrimPrefix(path, "/"), "/") {
		if segment == "." || segment == ".." {
			return errors.New("path must not contain . or .. segments")
		}
	}
	return nil
}

func ValidatePrefix(pathPrefix string) error {
	if pathPrefix == "" {
		return nil
	}
	if len([]byte(pathPrefix)) > MaxBytes {
		return errors.New("path_prefix must be at most 1024 bytes")
	}
	if !utf8.ValidString(pathPrefix) {
		return errors.New("path_prefix must be valid UTF-8")
	}
	if !strings.HasPrefix(pathPrefix, "/") {
		return errors.New("path_prefix must start with /")
	}
	if !norm.NFC.IsNormalString(pathPrefix) {
		return errors.New("path_prefix must be NFC-normalized")
	}
	if strings.Contains(pathPrefix, "//") {
		return errors.New("path_prefix must not contain empty segments")
	}
	for _, r := range pathPrefix {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return errors.New("path_prefix must not contain control or format characters")
		}
	}
	trimmed := strings.Trim(pathPrefix, "/")
	if trimmed == "" {
		return nil
	}
	for _, segment := range strings.Split(trimmed, "/") {
		if segment == "." || segment == ".." {
			return errors.New("path_prefix must not contain . or .. segments")
		}
	}
	return nil
}
