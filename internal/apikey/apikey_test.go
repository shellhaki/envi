package apikey

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"shellhaki/envi/internal/auth"
)

func testDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	if os.Getenv("ENVI_INTEGRATION") != "1" {
		t.Skip("set ENVI_INTEGRATION=1")
	}
	db, err := pgxpool.New(t.Context(), os.Getenv("DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	return db
}

func testUser(t *testing.T, db *pgxpool.Pool) string {
	t.Helper()
	var userID string
	email := fmt.Sprintf("apikey-test-%d@example.test", time.Now().UnixNano())
	if err := db.QueryRow(t.Context(), `INSERT INTO users (email) VALUES ($1) RETURNING id`, email).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Exec(t.Context(), `DELETE FROM users WHERE id = $1`, userID) })
	return userID
}

func TestCreateAuthenticateList(t *testing.T) {
	db := testDB(t)
	user := testUser(t, db)

	key, err := Create(t.Context(), db, user, "laptop", "read", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(key.Secret, Prefix) {
		t.Fatalf("key %q does not start with %q", key.Secret, Prefix)
	}
	if key.ExpiresAt == nil {
		t.Fatal("a key created with a lifetime has no expiry")
	}

	gotUser, permission, keyID, err := Authenticate(t.Context(), db, key.Secret)
	if err != nil || gotUser != user || permission != "read" || keyID != key.ID {
		t.Fatalf("authenticate: %q %q %q %v", gotUser, permission, keyID, err)
	}

	keys, err := List(t.Context(), db, user)
	if err != nil || len(keys) != 1 {
		t.Fatalf("list returned %d keys: %v", len(keys), err)
	}
	if keys[0].Secret != "" {
		t.Fatal("listing a key exposed its secret")
	}
	if keys[0].LastUsedAt == nil {
		t.Fatal("using a key did not record last_used_at")
	}
}

// The secret is never stored, only its hash, so a stolen database is not a
// stolen set of keys.
func TestSecretIsNotStored(t *testing.T) {
	db := testDB(t)
	user := testUser(t, db)

	key, err := Create(t.Context(), db, user, "laptop", "read", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	var stored []byte
	if err = db.QueryRow(t.Context(), `SELECT token_hash FROM api_tokens WHERE id = $1`, key.ID).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(stored), key.Secret) {
		t.Fatal("the key itself was written to the database")
	}
	if string(stored) != string(auth.HashToken(key.Secret)) {
		t.Fatal("stored value is not the hash of the key")
	}
}

func TestRevokeEndsTheKeyAndItsSessions(t *testing.T) {
	db := testDB(t)
	user := testUser(t, db)

	key, err := Create(t.Context(), db, user, "ci", "write", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	access, refresh, err := auth.StartLimitedSession(db, user, key.Permission, key.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, ceiling, err := auth.UserForAccessToken(db, access); err != nil || ceiling != "write" {
		t.Fatalf("session ceiling = %q, %v; want write", ceiling, err)
	}

	if err = Revoke(t.Context(), db, user, key.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err = Authenticate(t.Context(), db, key.Secret); err == nil {
		t.Fatal("a revoked key still authenticates")
	}
	// The point of tying sessions to the key: revoking has to sign them out,
	// or a leaked key stays usable for the life of the session it made.
	if _, _, err = auth.UserForAccessToken(db, access); err == nil {
		t.Fatal("revoking the key left its session working")
	}
	if _, _, err = auth.RefreshSession(db, refresh); err == nil {
		t.Fatal("revoking the key left its session refreshable")
	}
}

// A ceiling has to survive a refresh, or waiting fifteen minutes would remove it.
func TestCeilingSurvivesRefresh(t *testing.T) {
	db := testDB(t)
	user := testUser(t, db)

	key, err := Create(t.Context(), db, user, "ci", "read", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	_, refresh, err := auth.StartLimitedSession(db, user, key.Permission, key.ID)
	if err != nil {
		t.Fatal(err)
	}
	newAccess, _, err := auth.RefreshSession(db, refresh)
	if err != nil {
		t.Fatal(err)
	}
	if _, ceiling, err := auth.UserForAccessToken(db, newAccess); err != nil || ceiling != "read" {
		t.Fatalf("after refresh the ceiling is %q, %v; want read", ceiling, err)
	}
}

func TestExpiredAndInvalidKeys(t *testing.T) {
	db := testDB(t)
	user := testUser(t, db)

	expired, err := Create(t.Context(), db, user, "old", "read", -time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err = Authenticate(t.Context(), db, expired.Secret); err == nil {
		t.Fatal("an expired key authenticated")
	}
	if _, _, _, err = Authenticate(t.Context(), db, "envi_nonsense"); err == nil {
		t.Fatal("a made-up key authenticated")
	}
	if _, err = Create(t.Context(), db, user, "bad", "superuser", time.Hour); err == nil {
		t.Fatal("an unknown permission was accepted")
	}
	if _, err = Create(t.Context(), db, user, "  ", "read", time.Hour); err == nil {
		t.Fatal("a blank name was accepted")
	}
	// Someone else's key is not yours to revoke, and a malformed id is a typo.
	other := testUser(t, db)
	key, err := Create(t.Context(), db, user, "mine", "read", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if err = Revoke(t.Context(), db, other, key.ID); err != ErrNotFound {
		t.Fatalf("revoking another user's key: %v, want ErrNotFound", err)
	}
	if err = Revoke(t.Context(), db, user, "not-a-uuid"); err != ErrNotFound {
		t.Fatalf("revoking a malformed id: %v, want ErrNotFound", err)
	}
}

// A key that never expires is allowed, but has to be asked for explicitly.
func TestNeverExpiringKey(t *testing.T) {
	db := testDB(t)
	user := testUser(t, db)

	key, err := Create(t.Context(), db, user, "forever", "manage", 0)
	if err != nil {
		t.Fatal(err)
	}
	if key.ExpiresAt != nil {
		t.Fatal("a zero lifetime produced an expiry")
	}
	if _, _, _, err = Authenticate(t.Context(), db, key.Secret); err != nil {
		t.Fatal(err)
	}
}
