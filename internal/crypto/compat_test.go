package crypto

import (
	"encoding/hex"
	"encoding/json"
	"os"
	"testing"
)

// The Workers port reads the same database as this server, so the ciphertext
// layout is a contract rather than an implementation detail. Both suites pin
// to this one fixture file: workers/test/crypto.test.ts opens it with
// WebCrypto, this opens it with crypto/cipher. A change on either side that
// breaks the other fails here.
func TestWorkersFixturesOpen(t *testing.T) {
	const path = "../../workers/test/fixtures/go-sealed.json"
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("fixture not present (%v)", err)
	}
	var fixtures map[string]string
	if err = json.Unmarshal(raw, &fixtures); err != nil {
		t.Fatal(err)
	}
	if len(fixtures) == 0 {
		t.Fatal("fixture file is empty")
	}
	// The key the fixtures were sealed with; it is test-only by construction.
	c, err := New([]byte("01234567890123456789012345678901"))
	if err != nil {
		t.Fatal(err)
	}
	for want, encoded := range fixtures {
		blob, err := hex.DecodeString(encoded)
		if err != nil {
			t.Fatalf("%q: %v", want, err)
		}
		got, err := c.Open(blob)
		if err != nil {
			t.Fatalf("%q: ciphertext no longer opens — the Go and Workers formats have diverged: %v", want, err)
		}
		if string(got) != want {
			t.Fatalf("decrypted %q, want %q", got, want)
		}
	}
}
