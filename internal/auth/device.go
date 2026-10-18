package auth

// Device login: the CLI gets a secret device code and a short user code, the user approves
// the user code on the website, and the CLI polls with its device code until it's approved.

import (
	"context"
	"crypto/rand"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	DeviceCodeLifetime = 10 * time.Minute
	DevicePollInterval = 5 * time.Second
)

// DevicePending means the CLI isn't signed in yet. Reason is
// "authorization_pending", "access_denied" or "expired_token".
type DevicePending struct{ Reason string }

func (p DevicePending) Error() string { return p.Reason }

var (
	ErrDeviceNotFound = errors.New("device code not found")
	ErrDeviceCode     = errors.New("invalid or expired code")
)

// 32 letters, without I, O, 0 and 1, which are easy to misread.
const userCodeLetters = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"

// StartDeviceLogin creates a pending login and returns its codes, lifetime and poll interval in seconds.
func StartDeviceLogin(db *pgxpool.Pool) (deviceCode, userCode string, expiresIn, interval int, err error) {
	deviceCode, err = newRandomToken()
	if err != nil {
		return "", "", 0, 0, err
	}
	expiresAt := time.Now().Add(DeviceCodeLifetime)

	// User codes must be unique among live logins; on the rare clash, try another.
	for attempt := 0; attempt < 5; attempt++ {
		var code string
		if code, err = newUserCode(); err != nil {
			return "", "", 0, 0, err
		}
		_, err = db.Exec(context.Background(),
			`INSERT INTO device_authorizations (device_code_hash, user_code, expires_at) VALUES ($1, $2, $3)`,
			HashToken(deviceCode), code, expiresAt)
		if err == nil {
			return deviceCode, code[:4] + "-" + code[4:], int(DeviceCodeLifetime.Seconds()), int(DevicePollInterval.Seconds()), nil
		}
	}
	return "", "", 0, 0, err
}

// ApproveDeviceLogin is the user clicking Approve on the website.
func ApproveDeviceLogin(db *pgxpool.Pool, userCode, userID string) error {
	if userID == "" {
		return errors.New("user is required")
	}
	result, err := db.Exec(context.Background(), `
		UPDATE device_authorizations SET status = 'approved', user_id = $2, approved_at = now()
		 WHERE user_code = $1 AND status = 'pending' AND expires_at > now()`,
		NormalizeUserCode(userCode), userID)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return ErrDeviceCode
	}
	return nil
}

// DenyDeviceLogin is the user clicking Deny on the website.
func DenyDeviceLogin(db *pgxpool.Pool, userCode string) error {
	result, err := db.Exec(context.Background(), `
		UPDATE device_authorizations SET status = 'denied'
		 WHERE user_code = $1 AND status = 'pending' AND expires_at > now()`,
		NormalizeUserCode(userCode))
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return ErrDeviceCode
	}
	return nil
}

// FinishDeviceLogin returns tokens once the login is approved, or a DevicePending saying why not.
func FinishDeviceLogin(db *pgxpool.Pool, deviceCode string) (access, refresh string, err error) {
	userID, err := redeemDeviceCode(db, deviceCode)
	if err != nil {
		return "", "", err
	}
	return StartSession(db, userID)
}

// redeemDeviceCode returns the approving user and marks the code used, so it signs in only once.
func redeemDeviceCode(db *pgxpool.Pool, deviceCode string) (string, error) {
	ctx := context.Background()
	tx, err := db.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)

	var id, status string
	var approvedBy *string
	var expiresAt time.Time
	err = tx.QueryRow(ctx, `
		SELECT id, status, user_id, expires_at FROM device_authorizations
		 WHERE device_code_hash = $1 FOR UPDATE`,
		HashToken(deviceCode)).Scan(&id, &status, &approvedBy, &expiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrDeviceNotFound
	}
	if err != nil {
		return "", err
	}

	switch {
	case time.Now().After(expiresAt):
		return "", DevicePending{"expired_token"}
	case status == "pending" || (status == "approved" && approvedBy == nil):
		return "", DevicePending{"authorization_pending"}
	case status == "denied":
		return "", DevicePending{"access_denied"}
	case status != "approved":
		return "", DevicePending{"expired_token"} // already redeemed
	}

	if _, err = tx.Exec(ctx, `UPDATE device_authorizations SET status = 'redeemed' WHERE id = $1`, id); err != nil {
		return "", err
	}
	return *approvedBy, tx.Commit(ctx)
}

// newUserCode makes 8 random characters from userCodeLetters.
func newUserCode() (string, error) {
	random := make([]byte, 8)
	if _, err := rand.Read(random); err != nil {
		return "", err
	}
	code := make([]byte, 8)
	for i, b := range random {
		code[i] = userCodeLetters[int(b)%len(userCodeLetters)] // 256 divides by 32, so every letter is equally likely
	}
	return string(code), nil
}

// NormalizeUserCode turns "wxyz-abcd" or "WXYZ ABCD" into "WXYZABCD".
func NormalizeUserCode(typed string) string {
	var cleaned strings.Builder
	for _, r := range strings.ToUpper(typed) {
		if strings.ContainsRune(userCodeLetters, r) {
			cleaned.WriteRune(r)
		}
	}
	return cleaned.String()
}
