package auth

// This file is logging in the CLI through the browser ("envi auth").
//
// A terminal can't show a login page, so the CLI borrows the browser:
//
//  1. StartDeviceLogin: the CLI asks for a code. It gets two back:
//     - a long secret DEVICE code that only the CLI knows, and
//     - a short USER code like "WXYZ-ABCD" that it shows on screen.
//  2. The user opens the website (already logged in there), types the user
//     code, and clicks Approve: ApproveDeviceLogin (or Deny: DenyDeviceLogin).
//  3. Meanwhile the CLI keeps asking "is it approved yet?" with its device
//     code: FinishDeviceLogin. Once approved, that returns tokens and the CLI
//     is logged in as that user.
//
// Each request is a row in the device_authorizations table, with a status:
//
//	pending -> approved -> redeemed      (the normal path)
//	pending -> denied                    (the user clicked Deny)
//
// This is a standard called RFC 8628, which is where names like
// "authorization_pending" below come from.

import (
	"context"
	"crypto/rand"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// How long a code stays valid, and how often the CLI should ask whether it's
// been approved.
var (
	DeviceCodeLifetime = 10 * time.Minute
	DevicePollInterval = 5 * time.Second
)

// DevicePending means "not logged in yet", with a reason the CLI understands:
//
//	"authorization_pending"  still waiting for the user; keep asking
//	"access_denied"          the user clicked Deny; stop
//	"expired_token"          the code ran out or was already used; stop
type DevicePending struct {
	Reason string
}

// Error lets a DevicePending be returned as an error. Go requires this exact
// method for anything used as an error, which is why it's written this way.
func (pending DevicePending) Error() string {
	return pending.Reason
}

// ErrDeviceNotFound means the CLI sent a device code we've never seen.
var ErrDeviceNotFound = errors.New("device code not found")

// ErrDeviceCode means a user code couldn't be approved or denied: it's wrong,
// expired, or was already handled.
var ErrDeviceCode = errors.New("invalid or expired code")

// The letters a user code is made from. I, O, 0 and 1 are left out because
// they're easy to confuse when copying a code off a screen. There are exactly
// 32 of them, which matters in newUserCode below.
const userCodeLetters = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"

// StartDeviceLogin creates a new pending login and returns the codes the CLI
// needs, plus how long they last and how often to check back (in seconds).
func StartDeviceLogin(db *pgxpool.Pool) (deviceCode string, userCode string, expiresInSeconds int, pollEverySeconds int, err error) {
	deviceCode, err = newRandomToken()
	if err != nil {
		return "", "", 0, 0, err
	}
	expiresAt := time.Now().Add(DeviceCodeLifetime)

	// Two live requests can't share a user code (the database enforces it).
	// Clashes are very rare, so just try again with a new code a few times.
	for attempt := 1; attempt <= 5; attempt++ {
		var rawCode string
		rawCode, err = newUserCode()
		if err != nil {
			return "", "", 0, 0, err
		}

		_, err = db.Exec(context.Background(),
			`INSERT INTO device_authorizations (device_code_hash, user_code, expires_at) VALUES ($1, $2, $3)`,
			HashToken(deviceCode), rawCode, expiresAt,
		)
		if err == nil {
			return deviceCode, formatUserCode(rawCode), int(DeviceCodeLifetime.Seconds()), int(DevicePollInterval.Seconds()), nil
		}
	}
	return "", "", 0, 0, err
}

// ApproveDeviceLogin is the user clicking Approve on the website. It links the
// pending login to their account.
func ApproveDeviceLogin(db *pgxpool.Pool, userCode string, userID string) error {
	if userID == "" {
		return errors.New("user is required")
	}
	result, err := db.Exec(context.Background(),
		`UPDATE device_authorizations SET status = 'approved', user_id = $2, approved_at = now()
		 WHERE user_code = $1 AND status = 'pending' AND expires_at > now()`,
		NormalizeUserCode(userCode), userID,
	)
	if err != nil {
		return err
	}
	// If no row changed, the code was wrong, expired, or already handled.
	if result.RowsAffected() == 0 {
		return ErrDeviceCode
	}
	return nil
}

// DenyDeviceLogin is the user clicking Deny. The CLI stops waiting and fails.
func DenyDeviceLogin(db *pgxpool.Pool, userCode string) error {
	result, err := db.Exec(context.Background(),
		`UPDATE device_authorizations SET status = 'denied'
		 WHERE user_code = $1 AND status = 'pending' AND expires_at > now()`,
		NormalizeUserCode(userCode),
	)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return ErrDeviceCode
	}
	return nil
}

// FinishDeviceLogin is the CLI checking in. If the login has been approved it
// returns tokens, and the login can't be used again. Otherwise it returns a
// DevicePending saying why not.
func FinishDeviceLogin(db *pgxpool.Pool, deviceCode string) (accessToken string, refreshToken string, err error) {
	userID, err := useUpDeviceCode(db, deviceCode)
	if err != nil {
		return "", "", err
	}
	return StartSession(db, userID)
}

// useUpDeviceCode looks up a device login and, if it's approved, marks it as
// redeemed so it only ever logs someone in once. It returns the approving user.
func useUpDeviceCode(db *pgxpool.Pool, deviceCode string) (userID string, err error) {
	ctx := context.Background()
	tx, err := db.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)

	var rowID string
	var status string
	var approvedBy *string // a pointer, because it's empty (NULL) until someone approves
	var expiresAt time.Time
	// FOR UPDATE locks the row, so if the CLI checks twice at the same moment,
	// only the first check can use the code.
	err = tx.QueryRow(ctx,
		`SELECT id, status, user_id, expires_at FROM device_authorizations
		 WHERE device_code_hash = $1 FOR UPDATE`,
		HashToken(deviceCode),
	).Scan(&rowID, &status, &approvedBy, &expiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrDeviceNotFound
	}
	if err != nil {
		return "", err
	}

	if time.Now().After(expiresAt) {
		return "", DevicePending{Reason: "expired_token"}
	}
	if status == "pending" {
		return "", DevicePending{Reason: "authorization_pending"}
	}
	if status == "denied" {
		return "", DevicePending{Reason: "access_denied"}
	}
	if status != "approved" {
		// Already redeemed: each code logs someone in only once.
		return "", DevicePending{Reason: "expired_token"}
	}
	if approvedBy == nil {
		return "", DevicePending{Reason: "authorization_pending"}
	}

	_, err = tx.Exec(ctx, `UPDATE device_authorizations SET status = 'redeemed' WHERE id = $1`, rowID)
	if err != nil {
		return "", err
	}
	err = tx.Commit(ctx)
	if err != nil {
		return "", err
	}
	return *approvedBy, nil
}

// newUserCode makes 8 random characters from userCodeLetters, like "WXYZABCD".
func newUserCode() (string, error) {
	randomBytes := make([]byte, 8)
	_, err := rand.Read(randomBytes)
	if err != nil {
		return "", err
	}
	code := make([]byte, 8)
	for i, b := range randomBytes {
		// A byte is 0-255. There are 32 letters and 256 divides evenly by 32,
		// so every letter is equally likely.
		code[i] = userCodeLetters[int(b)%len(userCodeLetters)]
	}
	return string(code), nil
}

// formatUserCode adds a dash in the middle for display: "WXYZABCD" -> "WXYZ-ABCD".
func formatUserCode(raw string) string {
	if len(raw) != 8 {
		return raw
	}
	return raw[:4] + "-" + raw[4:]
}

// NormalizeUserCode cleans up what the user typed so it matches what we stored:
// "wxyz-abcd", "WXYZ ABCD" and "WXYZABCD" all become "WXYZABCD".
func NormalizeUserCode(typed string) string {
	var cleaned strings.Builder
	for _, letter := range strings.ToUpper(typed) {
		if strings.ContainsRune(userCodeLetters, letter) {
			cleaned.WriteRune(letter)
		}
	}
	return cleaned.String()
}
