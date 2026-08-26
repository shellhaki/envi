package crypto

import "testing"

func TestCipher(t *testing.T) {
	a, err := New([]byte("01234567890123456789012345678901"))
	if err != nil {
		t.Fatal(err)
	}
	b, _ := New([]byte("11234567890123456789012345678901"))
	sealed, err := a.Seal([]byte("secret"))
	if err != nil {
		t.Fatal(err)
	}
	plain, err := a.Open(sealed)
	if err != nil || string(plain) != "secret" {
		t.Fatal(err)
	}
	sealed[len(sealed)-1] ^= 1
	if _, err = a.Open(sealed); err == nil {
		t.Fatal("tampering accepted")
	}
	sealed, _ = a.Seal([]byte("secret"))
	if _, err = b.Open(sealed); err == nil {
		t.Fatal("wrong key accepted")
	}
}

func TestNewRejectsBadKey(t *testing.T) {
	if _, err := New([]byte("short")); err == nil {
		t.Fatal("short key accepted")
	}
}
