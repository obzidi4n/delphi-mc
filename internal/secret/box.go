// Package secret seals small secrets at rest with a local AES-256-GCM key.
package secret

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const keySize = 32

// Box seals and opens byte slices.
type Box struct {
	key []byte
}

// New loads or creates a 32-byte key.
func New(keyPath string) (*Box, error) {
	if keyPath == "" {
		return nil, errors.New("secret: key path is required")
	}
	key, err := os.ReadFile(keyPath)
	if err == nil {
		if len(key) != keySize {
			return nil, fmt.Errorf("secret: key must be %d bytes", keySize)
		}
		return &Box{key: key}, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	key = make([]byte, keySize)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(keyPath), 0o700); err != nil {
		return nil, err
	}
	tmp := keyPath + ".tmp"
	if err := os.WriteFile(tmp, key, 0o600); err != nil {
		return nil, err
	}
	if err := os.Rename(tmp, keyPath); err != nil {
		return nil, err
	}
	return &Box{key: key}, nil
}

func (b *Box) gcm() (cipher.AEAD, error) {
	block, err := aes.NewCipher(b.key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

// Seal encrypts plaintext with a random nonce prefix.
func (b *Box) Seal(plain []byte) ([]byte, error) {
	aead, err := b.gcm()
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return append(nonce, aead.Seal(nil, nonce, plain, nil)...), nil
}

// Open decrypts sealed data.
func (b *Box) Open(sealed []byte) ([]byte, error) {
	aead, err := b.gcm()
	if err != nil {
		return nil, err
	}
	if len(sealed) < aead.NonceSize() {
		return nil, errors.New("secret: ciphertext too short")
	}
	nonce, ciphertext := sealed[:aead.NonceSize()], sealed[aead.NonceSize():]
	return aead.Open(nil, nonce, ciphertext, nil)
}
