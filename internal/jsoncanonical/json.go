package jsoncanonical

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"

	"github.com/cyberphone/json-canonicalization/go/src/webpki.org/jsoncanonicalizer"
)

func Canonicalize(value []byte) ([]byte, error) {
	return jsoncanonicalizer.Transform(value)
}

func Equal(left, right []byte) (bool, error) {
	a, err := Canonicalize(left)
	if err != nil {
		return false, err
	}
	b, err := Canonicalize(right)
	if err != nil {
		return false, err
	}
	return bytes.Equal(a, b), nil
}

func Digest(value []byte) (string, error) {
	canonical, err := Canonicalize(value)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(canonical)
	return "jcs-sha256-v1:" + hex.EncodeToString(digest[:]), nil
}
