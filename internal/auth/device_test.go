package auth

import (
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// forgetDeviceCode deletes a device login row when the test ends, since a
// pending one isn't linked to any user that would clean it up.
func forgetDeviceCode(t *testing.T, db *pgxpool.Pool, deviceCode string) {
	t.Cleanup(func() {
		db.Exec(t.Context(), `DELETE FROM device_authorizations WHERE device_code_hash = $1`, HashToken(deviceCode))
	})
}

func TestDeviceLoginApproved(t *testing.T) {
	db := testDB(t)
	user := testUser(t, db)

	deviceCode, userCode, expiresIn, pollEvery, err := StartDeviceLogin(db)
	if err != nil {
		t.Fatal(err)
	}
	forgetDeviceCode(t, db, deviceCode)
	if expiresIn != 600 || pollEvery != 5 {
		t.Fatalf("expiresIn=%d pollEvery=%d, want 600 and 5", expiresIn, pollEvery)
	}
	if !regexp.MustCompile(`^[A-HJ-NP-Z2-9]{4}-[A-HJ-NP-Z2-9]{4}$`).MatchString(userCode) {
		t.Fatalf("user code %q isn't in XXXX-XXXX form", userCode)
	}

	// Before approval, the CLI is told to keep waiting.
	if _, _, err := FinishDeviceLogin(db, deviceCode); !isPending(err, "authorization_pending") {
		t.Fatalf("before approval: got %v, want authorization_pending", err)
	}

	// The website sends the code as displayed, with the dash.
	if err := ApproveDeviceLogin(db, userCode, user); err != nil {
		t.Fatal(err)
	}
	access, _, err := FinishDeviceLogin(db, deviceCode)
	if err != nil {
		t.Fatal(err)
	}
	if got, _, err := UserForAccessToken(db, access); err != nil || got != user {
		t.Fatalf("the CLI's new session belongs to %q (%v), want %q", got, err, user)
	}

	// A code only logs someone in once.
	if _, _, err := FinishDeviceLogin(db, deviceCode); !isPending(err, "expired_token") {
		t.Fatalf("second use: got %v, want expired_token", err)
	}
}

func TestDeviceLoginDenied(t *testing.T) {
	db := testDB(t)
	deviceCode, userCode, _, _, err := StartDeviceLogin(db)
	if err != nil {
		t.Fatal(err)
	}
	forgetDeviceCode(t, db, deviceCode)

	if err := DenyDeviceLogin(db, userCode); err != nil {
		t.Fatal(err)
	}
	if _, _, err := FinishDeviceLogin(db, deviceCode); !isPending(err, "access_denied") {
		t.Fatalf("got %v, want access_denied", err)
	}
}

func TestDeviceLoginUnknownCodes(t *testing.T) {
	db := testDB(t)
	user := testUser(t, db)

	if err := ApproveDeviceLogin(db, "ZZZZ-ZZZZ", user); !errors.Is(err, ErrDeviceCode) {
		t.Fatalf("approving an unknown code: got %v, want ErrDeviceCode", err)
	}
	if _, _, err := FinishDeviceLogin(db, "not-a-real-device-code"); !errors.Is(err, ErrDeviceNotFound) {
		t.Fatalf("an unknown device code: got %v, want ErrDeviceNotFound", err)
	}
}

func TestDeviceLoginExpires(t *testing.T) {
	db := testDB(t)
	user := testUser(t, db)

	saved := DeviceCodeLifetime
	DeviceCodeLifetime = -time.Minute
	t.Cleanup(func() { DeviceCodeLifetime = saved })

	deviceCode, userCode, _, _, err := StartDeviceLogin(db)
	if err != nil {
		t.Fatal(err)
	}
	forgetDeviceCode(t, db, deviceCode)

	if err := ApproveDeviceLogin(db, userCode, user); !errors.Is(err, ErrDeviceCode) {
		t.Fatalf("approving an expired code: got %v, want ErrDeviceCode", err)
	}
	if _, _, err := FinishDeviceLogin(db, deviceCode); !isPending(err, "expired_token") {
		t.Fatalf("got %v, want expired_token", err)
	}
}

func TestNormalizeUserCode(t *testing.T) {
	for _, tc := range []struct{ typed, want string }{
		{"wxyz-abcd", "WXYZABCD"},
		{"WXYZ ABCD", "WXYZABCD"},
		{"WXYZABCD", "WXYZABCD"},
		{"  wx yz-ab cd  ", "WXYZABCD"},
	} {
		if got := NormalizeUserCode(tc.typed); got != tc.want {
			t.Errorf("NormalizeUserCode(%q) = %q, want %q", tc.typed, got, tc.want)
		}
	}
}

func isPending(err error, reason string) bool {
	var pending DevicePending
	return errors.As(err, &pending) && pending.Reason == reason
}
