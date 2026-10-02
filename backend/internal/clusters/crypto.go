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

// Cipher seals and opens registry payloads. It always seals with its current key
// but can also open data sealed with earlier keys, which is how the key is
// rotated: start with the new key as current and the old one as previous, and
// the first load re-encrypts everything in place (see Store.Load).
type Cipher struct {
	aead     cipher.AEAD
	previous []cipher.AEAD
}

func newAEAD(key []byte) (cipher.AEAD, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("cluster store key must be 32 bytes, got %d", len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

// NewCipher builds an AES-256-GCM cipher from a 32-byte key, plus any previous
// keys data may still be sealed with.
func NewCipher(key []byte, previous ...[]byte) (*Cipher, error) {
	aead, err := newAEAD(key)
	if err != nil {
		return nil, err
	}
	c := &Cipher{aead: aead}
	for _, p := range previous {
		old, err := newAEAD(p)
		if err != nil {
			return nil, fmt.Errorf("previous key: %w", err)
		}
		c.previous = append(c.previous, old)
	}
	return c, nil
}

// ParseKeys decodes a comma-separated list of base64 keys (CLUSTER_STORE_KEY_PREVIOUS).
func ParseKeys(list string) ([][]byte, error) {
	var out [][]byte
	for _, s := range strings.Split(list, ",") {
		if strings.TrimSpace(s) == "" {
			continue
		}
		k, err := ParseKey(s)
		if err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, nil
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
	plain, _, err := c.OpenReport(data)
	return plain, err
}

// OpenReport is Open that also says whether the data should be re-sealed with
// the current key: it was sealed with a previous key, or was never sealed.
func (c *Cipher) OpenReport(data []byte) (plain []byte, reseal bool, err error) {
	if !bytes.HasPrefix(data, []byte(sealedPrefix)) {
		return data, true, nil
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(data[len(sealedPrefix):])))
	if err != nil {
		return nil, false, errors.New("stored cluster data is corrupt")
	}
	ns := c.aead.NonceSize()
	if len(raw) < ns {
		return nil, false, errors.New("stored cluster data is corrupt")
	}
	if plain, err := c.aead.Open(nil, raw[:ns], raw[ns:], nil); err == nil {
		return plain, false, nil
	}
	for _, old := range c.previous {
		if plain, err := old.Open(nil, raw[:ns], raw[ns:], nil); err == nil {
			return plain, true, nil
		}
	}
	return nil, false, errors.New("couldn't decrypt the stored clusters — is CLUSTER_STORE_KEY the key they were saved with? (after changing the key, put the old one in CLUSTER_STORE_KEY_PREVIOUS)")
}

// IsSealed reports whether data is in the encrypted format.
func IsSealed(data []byte) bool { return bytes.HasPrefix(data, []byte(sealedPrefix)) }
