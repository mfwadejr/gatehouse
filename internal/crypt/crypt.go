// Package crypt seals small secrets (TOTP seeds, PPPoE passwords) with a machine key.
package crypt

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"os"
	"sync"
)

// Box encrypts and decrypts strings with AES-256-GCM.
type Box struct {
	mu   sync.RWMutex
	aead cipher.AEAD
}

func (b *Box) get() cipher.AEAD {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.aead
}

// SetKey switches the box to another key, for restoring a backup made with it.
func (b *Box) SetKey(key []byte) error {
	n, err := New(key)
	if err != nil {
		return err
	}
	b.mu.Lock()
	b.aead = n.aead
	b.mu.Unlock()
	return nil
}

// LoadOrCreate reads the key file, creating it (mode 0600) on first use.
func LoadOrCreate(path string) (*Box, error) {
	key, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		key = make([]byte, 32)
		if _, err := rand.Read(key); err != nil {
			return nil, err
		}
		if err := os.WriteFile(path, key, 0o600); err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	}
	if len(key) != 32 {
		return nil, errors.New("crypt: key file must be 32 bytes")
	}
	return New(key)
}

// New builds a Box from a 32 byte key.
func New(key []byte) (*Box, error) {
	blk, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	a, err := cipher.NewGCM(blk)
	if err != nil {
		return nil, err
	}
	b := &Box{}
	b.aead = a
	return b, nil
}

// Seal returns base64(nonce||ciphertext).
func (b *Box) Seal(plain string) string {
	aead := b.get()
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		panic(err)
	}
	return base64.RawStdEncoding.EncodeToString(aead.Seal(nonce, nonce, []byte(plain), nil))
}

// Open reverses Seal.
func (b *Box) Open(s string) (string, error) {
	raw, err := base64.RawStdEncoding.DecodeString(s)
	if err != nil {
		return "", err
	}
	aead := b.get()
	n := aead.NonceSize()
	if len(raw) < n {
		return "", errors.New("crypt: short ciphertext")
	}
	pt, err := aead.Open(nil, raw[:n], raw[n:], nil)
	if err != nil {
		return "", err
	}
	return string(pt), nil
}
