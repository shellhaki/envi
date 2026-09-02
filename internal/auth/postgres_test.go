package auth

import (
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPostgresTokensIntegration(t *testing.T) {
	if os.Getenv("ENVI_INTEGRATION") != "1" {
		t.Skip("set ENVI_INTEGRATION=1")
	}
	db, err := pgxpool.New(t.Context(), os.Getenv("DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Ping(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(t.Context(), `CREATE TEMP TABLE sessions (id uuid DEFAULT gen_random_uuid(), user_id uuid, refresh_token_hash bytea UNIQUE, access_token_hash bytea UNIQUE, access_expires_at timestamptz, expires_at timestamptz, revoked_at timestamptz)`); err != nil {
		t.Fatal(err)
	}
	store := &PostgresTokens{DB: db, AccessTTL: time.Minute, RefreshTTL: time.Hour}
	var user string
	if err = db.QueryRow(t.Context(), `INSERT INTO users(email) VALUES($1) RETURNING id`, `token-test@example.com`).Scan(&user); err != nil {
		t.Fatal(err)
	}
	defer db.Exec(t.Context(), `DELETE FROM users WHERE id=$1`, user)
	if err = store.Save(user, "old", "access"); err != nil {
		t.Fatal(err)
	}
	if got, err := store.Authenticate("access"); err != nil || got != user {
		t.Fatalf("authenticate: %q %v", got, err)
	}
	if got, err := store.Take("old"); err != nil || got != user {
		t.Fatalf("take: %q %v", got, err)
	}
	// Taking a refresh token revokes the row, so its access token dies with it.
	if _, err = store.Authenticate("access"); err == nil {
		t.Fatal("access token survived rotation")
	}
	if _, err = store.Take("old"); err == nil {
		t.Fatal("consumed token accepted")
	}
	if err = store.Save(user, "next", "access2"); err != nil {
		t.Fatal(err)
	}
	if err = store.Revoke("next"); err != nil {
		t.Fatal(err)
	}
	if _, err = store.Take("next"); err == nil {
		t.Fatal("revoked token accepted")
	}
	if _, err = store.Authenticate("access2"); err == nil {
		t.Fatal("revoked session still authenticates")
	}
	// An access token past access_expires_at must be rejected while the session
	// itself is still refreshable.
	expiring := &PostgresTokens{DB: db, AccessTTL: -time.Minute, RefreshTTL: time.Hour}
	if err = expiring.Save(user, "third", "access3"); err != nil {
		t.Fatal(err)
	}
	if _, err = expiring.Authenticate("access3"); err == nil {
		t.Fatal("expired access token accepted")
	}
	if _, err = expiring.Take("third"); err != nil {
		t.Fatalf("expired access token blocked refresh: %v", err)
	}
}
