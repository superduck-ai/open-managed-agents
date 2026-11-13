package sessionports

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"encoding/base64"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/google/uuid"
)

const (
	SlugLength = 26
	keyBytes   = 32
)

var slugEncoding = base32.StdEncoding.WithPadding(base32.NoPadding)

type SlugCodec struct {
	masterKey []byte
}

func NewSlugCodec(key []byte) (*SlugCodec, error) {
	if len(key) != keyBytes {
		return nil, errInvalidKey
	}
	return &SlugCodec{masterKey: append([]byte(nil), key...)}, nil
}

func (c *SlugCodec) portCipher(port int) (cipher.Block, error) {
	mac := hmac.New(sha256.New, c.masterKey)
	mac.Write([]byte("session-port:" + strconv.Itoa(port)))
	return aes.NewCipher(mac.Sum(nil))
}

func (c *SlugCodec) ForwardKey() []byte {
	mac := hmac.New(sha256.New, c.masterKey)
	mac.Write([]byte("session-port-internal-forward"))
	return mac.Sum(nil)
}

func LoadSlugKey(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read session port slug key: %w", err)
	}
	key, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(data)))
	if err != nil || len(key) != keyBytes {
		return nil, errInvalidKey
	}
	return key, nil
}

func ephemeralSlugKey() ([]byte, error) {
	key := make([]byte, keyBytes)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	return key, nil
}

func (c *SlugCodec) Encode(sessionUUID string, port int) (string, error) {
	parsed, err := uuid.Parse(sessionUUID)
	if err != nil {
		return "", errInvalidUUID
	}
	block, err := c.portCipher(port)
	if err != nil {
		return "", err
	}
	var sealed [aes.BlockSize]byte
	block.Encrypt(sealed[:], parsed[:])
	return strings.ToLower(slugEncoding.EncodeToString(sealed[:])), nil
}

func (c *SlugCodec) Decode(slug string, port int) (string, error) {
	if len(slug) != SlugLength || slug != strings.ToLower(slug) {
		return "", ErrInvalidSlug
	}
	sealed, err := slugEncoding.DecodeString(strings.ToUpper(slug))
	if err != nil || len(sealed) != aes.BlockSize {
		return "", ErrInvalidSlug
	}
	if strings.ToLower(slugEncoding.EncodeToString(sealed)) != slug {
		return "", ErrInvalidSlug
	}
	block, err := c.portCipher(port)
	if err != nil {
		return "", err
	}
	var plain uuid.UUID
	block.Decrypt(plain[:], sealed)
	return plain.String(), nil
}
