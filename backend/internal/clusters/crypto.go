package clusters

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
)

// Roadmap Phase 34: the registry of added clusters is the most valuable object
// Vinaire holds — admin kubeconfigs for every connected cluster in one place.
// So it is sealed with AES-256-GCM before it touches a Secret or a file, the
// key living somewhere other than the data it protects (an env var, a separate
// Secret, or a 0600 key file). That doesn't make a compromised Vinaire safe —
// the process must be able to decrypt — but it does mean a leaked Secret, a
// backup or a stray `cat` of the data file are not a leak of cluster-admin.

const sealedPrefix = "enc:v1:"

// Cipher seals and opens registry payloads.
type Cipher struct{ aead cipher.AEAD }

// NewCipher builds an AES-256-GCM cipher from a 32-byte key.
func NewCipher(key []byte) (*Cipher, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("cluster store key must be 32 bytes, got %d", len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Cipher{aead: aead}, nil
}

// GenerateKey returns 32 random bytes.
func GenerateKey() ([]byte, error) {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	return key, nil
}

// ParseKey decodes a base64 key (standard, URL-safe, padded or not) that must
// be exactly 32 bytes — e.g. `openssl rand -base64 32`.
func ParseKey(s string) ([]byte, error) {
	s = strings.TrimSpace(s)
	for _, enc := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
		if b, err := enc.DecodeString(s); err == nil && len(b) == 32 {
			return b, nil
		}
	}
	return nil, errors.New("CLUSTER_STORE_KEY must be 32 random bytes, base64-encoded (e.g. `openssl rand -base64 32`)")
}

// Seal encrypts plain with a fresh random nonce. The output is text, safe to
// store in a Secret's data or a file.
func (c *Cipher) Seal(plain []byte) []byte {
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		panic("crypto/rand failed: " + err.Error())
	}
	sealed := c.aead.Seal(nonce, nonce, plain, nil)
	return []byte(sealedPrefix + base64.StdEncoding.EncodeToString(sealed))
}

// Open decrypts data produced by Seal. Anything without the sealed prefix is
// treated as the legacy plaintext JSON Phase 24 wrote and returned as-is, so
// an upgrade keeps working; the next Save re-writes it encrypted.
func (c *Cipher) Open(data []byte) ([]byte, error) {
	if !bytes.HasPrefix(data, []byte(sealedPrefix)) {
		return data, nil
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(data[len(sealedPrefix):])))
	if err != nil {
		return nil, errors.New("stored cluster data is corrupt")
	}
	ns := c.aead.NonceSize()
	if len(raw) < ns {
		return nil, errors.New("stored cluster data is corrupt")
	}
	plain, err := c.aead.Open(nil, raw[:ns], raw[ns:], nil)
	if err != nil {
		return nil, errors.New("couldn't decrypt the stored clusters — is CLUSTER_STORE_KEY the key they were saved with?")
	}
	return plain, nil
}

// IsSealed reports whether data is in the encrypted format.
func IsSealed(data []byte) bool { return bytes.HasPrefix(data, []byte(sealedPrefix)) }
