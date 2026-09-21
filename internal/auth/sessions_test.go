package auth

import (
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// testDB connects to the real database. These tests only run when you ask for
// them, with ENVI_INTEGRATION=1 and DATABASE_URL set, like the repo's other
// database tests.
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

// testUser creates a throwaway user and deletes it when the test ends. Deleting
// the user also deletes its sessions.
func testUser(t *testing.T, db *pgxpool.Pool) string {
	t.Helper()
	var userID string
	email := fmt.Sprintf("auth-test-%d@example.test", time.Now().UnixNano())
	err := db.QueryRow(t.Context(), `INSERT INTO users (email) VALUES ($1) RETURNING id`, email).Scan(&userID)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Exec(t.Context(), `DELETE FROM users WHERE id = $1`, userID) })
	return userID
}

func TestSessionLifecycle(t *testing.T) {
	db := testDB(t)
	user := testUser(t, db)

	access, refresh, err := StartSession(db, user)
	if err != nil {
		t.Fatal(err)
	}
	if got, _, err := UserForAccessToken(db, access); err != nil || got != user {
		t.Fatalf("new access token: got %q, %v", got, err)
	}

	newAccess, newRefresh, err := RefreshSession(db, refresh)
	if err != nil {
		t.Fatal(err)
	}
	if newAccess == access || newRefresh == refresh {
		t.Fatal("refreshing must hand out new tokens, not the old ones")
	}
	if _, _, err := UserForAccessToken(db, access); err == nil {
		t.Fatal("the old access token still works after a refresh")
	}
	if got, _, err := UserForAccessToken(db, newAccess); err != nil || got != user {
		t.Fatalf("refreshed access token: got %q, %v", got, err)
	}
	if _, _, err := RefreshSession(db, refresh); err == nil {
		t.Fatal("a refresh token worked twice")
	}

	if err := EndSession(db, newRefresh); err != nil {
		t.Fatal(err)
	}
	if _, _, err := RefreshSession(db, newRefresh); err == nil {
		t.Fatal("a logged-out refresh token still works")
	}
	if _, _, err := UserForAccessToken(db, newAccess); err == nil {
		t.Fatal("logging out left the access token working")
	}
}

// An expired access token is rejected, but the session can still be refreshed:
// the two tokens run out separately.
func TestAccessTokenExpiresBeforeTheSession(t *testing.T) {
	db := testDB(t)
	user := testUser(t, db)

	saved := AccessTokenLifetime
	AccessTokenLifetime = -time.Minute
	t.Cleanup(func() { AccessTokenLifetime = saved })

	access, refresh, err := StartSession(db, user)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := UserForAccessToken(db, access); err == nil {
		t.Fatal("an expired access token was accepted")
	}
	if _, _, err := RefreshSession(db, refresh); err != nil {
		t.Fatalf("an expired access token blocked refreshing: %v", err)
	}
}

// Once RefreshTokenLifetime passes with no refresh, the session is over.
func TestInactiveSessionEnds(t *testing.T) {
	db := testDB(t)
	user := testUser(t, db)

	saved := RefreshTokenLifetime
	RefreshTokenLifetime = -time.Minute
	t.Cleanup(func() { RefreshTokenLifetime = saved })

	_, refresh, err := StartSession(db, user)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := RefreshSession(db, refresh); err == nil {
		t.Fatal("a session past its lifetime could still be refreshed")
	}
}
