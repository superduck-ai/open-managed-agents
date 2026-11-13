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
	LabelLength = 26
	keyBytes    = 32
)

var labelEncoding = base32.StdEncoding.WithPadding(base32.NoPadding)

type LabelCodec struct {
	masterKey []byte
}

func NewLabelCodec(key []byte) (*LabelCodec, error) {
	if len(key) != keyBytes {
		return nil, errInvalidKey
	}
	return &LabelCodec{masterKey: append([]byte(nil), key...)}, nil
}

func (c *LabelCodec) portCipher(port int) (cipher.Block, error) {
	mac := hmac.New(sha256.New, c.masterKey)
	mac.Write([]byte("session-port:" + strconv.Itoa(port)))
	return aes.NewCipher(mac.Sum(nil))
}

func (c *LabelCodec) ForwardKey() []byte {
	mac := hmac.New(sha256.New, c.masterKey)
	mac.Write([]byte("session-port-internal-forward"))
	return mac.Sum(nil)
}

func LoadLabelKey(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read session port label key: %w", err)
	}
	key, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(data)))
	if err != nil || len(key) != keyBytes {
		return nil, errInvalidKey
	}
	return key, nil
}

func ephemeralLabelKey() ([]byte, error) {
	key := make([]byte, keyBytes)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	return key, nil
}

func (c *LabelCodec) Encode(sessionUUID string, port int) (string, error) {
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
	return strings.ToLower(labelEncoding.EncodeToString(sealed[:])), nil
}

func (c *LabelCodec) Decode(label string, port int) (string, error) {
	if len(label) != LabelLength || label != strings.ToLower(label) {
		return "", ErrInvalidLabel
	}
	sealed, err := labelEncoding.DecodeString(strings.ToUpper(label))
	if err != nil || len(sealed) != aes.BlockSize {
		return "", ErrInvalidLabel
	}
	if strings.ToLower(labelEncoding.EncodeToString(sealed)) != label {
		return "", ErrInvalidLabel
	}
	block, err := c.portCipher(port)
	if err != nil {
		return "", err
	}
	var plain uuid.UUID
	block.Decrypt(plain[:], sealed)
	return plain.String(), nil
}
