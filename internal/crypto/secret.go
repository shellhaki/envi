package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"io"
)

type Cipher struct{ gcm cipher.AEAD }

func New(key []byte) (*Cipher, error) {
	if len(key) != 32 {
		return nil, errors.New("encryption key must be 32 bytes")
	}
	b, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	g, err := cipher.NewGCM(b)
	if err != nil {
		return nil, err
	}
	return &Cipher{g}, nil
}

func (c *Cipher) Seal(plain []byte) ([]byte, error) {
	n := make([]byte, c.gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, n); err != nil {
		return nil, err
	}
	return c.gcm.Seal(n, n, plain, nil), nil
}

func (c *Cipher) Open(data []byte) ([]byte, error) {
	n := c.gcm.NonceSize()
	if len(data) < n {
		return nil, errors.New("invalid ciphertext")
	}
	return c.gcm.Open(nil, data[:n], data[n:], nil)
}
