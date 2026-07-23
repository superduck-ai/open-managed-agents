// Package ids provides helpers for generating opaque external identifiers.
package ids

import (
	"crypto/rand"
	"fmt"
	"io"
)

const (
	alphabet       = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
	alphabetSize   = len(alphabet)
	randomIDLength = 24
	// Bytes at or above this value are rejected so byte%alphabetSize stays unbiased.
	uniformByteCeil = byte(256 - 256%alphabetSize)
	// Slightly oversized: ~3% of bytes are rejected (256%62 == 8).
	randomReadSize = randomIDLength + 4
)

// New generates an API-boundary external ID: business prefix + 24 unbiased Base62 chars.
// Results are typically stored in external_id, or used as opaque request/token identifiers.
func New(prefix string) (string, error) {
	return generate(prefix, rand.Reader)
}

func generate(prefix string, reader io.Reader) (string, error) {
	var id [randomIDLength]byte
	buf := make([]byte, randomReadSize)

	for i := 0; i < len(id); {
		if _, err := io.ReadFull(reader, buf); err != nil {
			return "", fmt.Errorf("read random bytes: %w", err)
		}
		for _, b := range buf {
			if b >= uniformByteCeil {
				continue
			}
			id[i] = alphabet[int(b)%alphabetSize]
			i++
			if i == len(id) {
				break
			}
		}
	}
	return prefix + string(id[:]), nil
}
