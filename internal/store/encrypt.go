package store

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
)

// Keyring provides the 32-byte AES-256 key for field-level encryption (D2).
// In production the key comes from the node's keyring / Cryptomator volume;
// this minimal implementation holds it in memory.
type Keyring struct {
	key [32]byte
}

// NewKeyringFromSeed builds a Keyring from a 32-byte seed (e.g. derived from
// the node identity). A zero seed is rejected.
func NewKeyringFromSeed(seed []byte) (*Keyring, error) {
	if len(seed) < 32 {
		return nil, errors.New("seed must be at least 32 bytes")
	}
	k := &Keyring{}
	copy(k.key[:], seed[:32])
	return k, nil
}

// EncryptedValue is the JSON-wrappable at-rest form (BUILD_SPEC §4.5).
type EncryptedValue struct {
	Enc  bool   `json:"enc"`
	Alg  string `json:"alg"`
	Data string `json:"data"` // base64 ciphertext = nonce || aes-gcm(plaintext)
}

// Encrypt wraps plaintext as an EncryptedValue. Encrypted fields are never
// indexed by RAG (D17).
func (k *Keyring) Encrypt(plaintext string) (*EncryptedValue, error) {
	block, err := aes.NewCipher(k.key[:])
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	sealed := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return &EncryptedValue{Enc: true, Alg: "AES-256-GCM", Data: base64.StdEncoding.EncodeToString(sealed)}, nil
}

// Decrypt reverses Encrypt.
func (k *Keyring) Decrypt(v *EncryptedValue) (string, error) {
	if v == nil || !v.Enc {
		return "", errors.New("not an encrypted value")
	}
	sealed, err := base64.StdEncoding.DecodeString(v.Data)
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(k.key[:])
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(sealed) < gcm.NonceSize() {
		return "", fmt.Errorf("ciphertext too short")
	}
	nonce, ct := sealed[:gcm.NonceSize()], sealed[gcm.NonceSize():]
	plain, err := gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}
