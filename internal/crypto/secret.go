package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"io"
)

type Cipher struct{ gcm cipher.AEAD }

// New returns an AES-256-GCM cipher. The key must be exactly 32 bytes.
func New(key []byte) (*Cipher, error) {
	if len(key) != 32 {
		return nil, errors.New("encryption key must be 32 bytes")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Cipher{gcm}, nil
}

// Seal encrypts plain and returns the random nonce followed by the ciphertext.
func (c *Cipher) Seal(plain []byte) ([]byte, error) {
	nonce := make([]byte, c.gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	return c.gcm.Seal(nonce, nonce, plain, nil), nil
}

// Open decrypts what Seal produced.
func (c *Cipher) Open(sealed []byte) ([]byte, error) {
	size := c.gcm.NonceSize()
	if len(sealed) < size {
		return nil, errors.New("invalid ciphertext")
	}
	nonce, ciphertext := sealed[:size], sealed[size:]
	return c.gcm.Open(nil, nonce, ciphertext, nil)
}
