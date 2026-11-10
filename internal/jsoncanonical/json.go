package jsoncanonical

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/json/jsontext"
)

const digestPrefix = "exact-json-sha256-v1:"

func Canonicalize(value []byte) ([]byte, error) {
	validated := jsontext.Value(bytes.Clone(value))
	if err := validated.Format(); err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(validated))
	decoder.UseNumber()
	var decoded any
	if err := decoder.Decode(&decoded); err != nil {
		return nil, err
	}
	return json.Marshal(normalizeNumbers(decoded))
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
	return digestPrefix + hex.EncodeToString(digest[:]), nil
}
