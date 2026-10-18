package secret

import (
	"context"
	"errors"
	"sort"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"shellhaki/envi/internal/access"
	crypt "shellhaki/envi/internal/crypto"
)

// ErrConflict means someone else changed the environment since the caller last read it.
var ErrConflict = errors.New("stale secret revision")

type Snapshot struct {
	Values   map[string]string `json:"values"`
	Revision int64             `json:"revision"`
}

// Every value is stored encrypted. Each change adds a row to secret_versions and
// bumps the environment's revision, which is how concurrent writers are detected.
type Service struct {
	DB     *pgxpool.Pool
	Access access.Service
	Cipher *crypt.Cipher
}

// Get returns all of an environment's values.
func (s Service) Get(ctx context.Context, userID, envID string) (map[string]string, error) {
	snapshot, err := s.Snapshot(ctx, userID, envID)
	return snapshot.Values, err
}

// Snapshot returns all values and the revision, read together, and records one audit event.
func (s Service) Snapshot(ctx context.Context, userID, envID string) (Snapshot, error) {
	if err := s.Access.Allow(ctx, userID, envID, "read"); err != nil {
		return Snapshot{}, err
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return Snapshot{}, err
	}
	defer tx.Rollback(ctx)

	var revision int64
	if err := tx.QueryRow(ctx, `SELECT revision FROM environments WHERE id = $1 FOR SHARE`, envID).Scan(&revision); err != nil {
		return Snapshot{}, err
	}
	values, err := s.readValues(ctx, tx, envID)
	if err != nil {
		return Snapshot{}, err
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO audit_events (org_id, actor_id, action, target_type, target_id, metadata)
		SELECT p.org_id, $1, 'secret.read', 'environment', $2, jsonb_build_object('secrets', $3::int)
		  FROM environments e JOIN projects p ON p.id = e.project_id
		 WHERE e.id = $2`, userID, envID, len(values))
	if err != nil {
		return Snapshot{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Snapshot{}, err
	}
	return Snapshot{Values: values, Revision: revision}, nil
}

// GetService is Get for a service token, which is already bound to this environment.
func (s Service) GetService(ctx context.Context, serviceID, envID string) (map[string]string, error) {
	values, err := s.readValues(ctx, s.DB, envID)
	if err != nil {
		return nil, err
	}
	_, _ = s.DB.Exec(ctx, `
		INSERT INTO audit_events (org_id, action, target_type, target_id, metadata)
		SELECT p.org_id, 'secret.read', 'service_identity', $1, jsonb_build_object('environment_id', $2)
		  FROM environments e JOIN projects p ON p.id = e.project_id
		 WHERE e.id = $2`, serviceID, envID)
	return values, nil
}

// Revision returns the environment's current revision number.
func (s Service) Revision(ctx context.Context, envID string) (int64, error) {
	var revision int64
	err := s.DB.QueryRow(ctx, `SELECT revision FROM environments WHERE id = $1`, envID).Scan(&revision)
	return revision, err
}

// Put sets one secret.
func (s Service) Put(ctx context.Context, userID, envID, key, value string) error {
	if err := s.Access.Allow(ctx, userID, envID, "write"); err != nil {
		return err
	}
	return s.putOne(ctx, userID, envID, key, value)
}

// PutService is Put for a service token.
func (s Service) PutService(ctx context.Context, envID, key, value string) error {
	return s.putOne(ctx, "", envID, key, value)
}

// PutAll writes many secrets at once, only if the revision is still `expected`. Otherwise ErrConflict.
func (s Service) PutAll(ctx context.Context, userID, envID string, values map[string]string, expected int64) (int64, error) {
	if err := s.Access.Allow(ctx, userID, envID, "write"); err != nil {
		return 0, err
	}
	return s.putAll(ctx, userID, envID, values, expected)
}

// PutAllService is PutAll for a service token.
func (s Service) PutAllService(ctx context.Context, envID string, values map[string]string, expected int64) (int64, error) {
	return s.putAll(ctx, "", envID, values, expected)
}

// Delete marks one secret deleted. Its versions are kept.
func (s Service) Delete(ctx context.Context, userID, envID, key string) error {
	if err := s.Access.Allow(ctx, userID, envID, "write"); err != nil {
		return err
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var secretID string
	err = tx.QueryRow(ctx, `
		UPDATE secrets SET deleted_at = now()
		 WHERE environment_id = $1 AND key_name = $2 AND deleted_at IS NULL
		RETURNING id`, envID, key).Scan(&secretID)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO audit_events (org_id, actor_id, action, target_type, target_id)
		SELECT p.org_id, $1, 'secret.delete', 'secret', $2
		  FROM environments e JOIN projects p ON p.id = e.project_id
		 WHERE e.id = $3`, userID, secretID, envID)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s Service) putOne(ctx context.Context, userID, envID, key, value string) error {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `UPDATE environments SET revision = revision + 1 WHERE id = $1`, envID); err != nil {
		return err
	}
	if err := s.writeVersion(ctx, tx, userID, envID, key, value); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s Service) putAll(ctx context.Context, userID, envID string, values map[string]string, expected int64) (int64, error) {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)

	var revision int64
	err = tx.QueryRow(ctx, `
		UPDATE environments SET revision = revision + 1
		 WHERE id = $1 AND revision = $2
		RETURNING revision`, envID, expected).Scan(&revision)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, ErrConflict
	}
	if err != nil {
		return 0, err
	}

	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if err := s.writeVersion(ctx, tx, userID, envID, key, values[key]); err != nil {
			return 0, err
		}
	}
	return revision, tx.Commit(ctx)
}

// writeVersion encrypts a value, stores it as the secret's next version, and records an audit event.
func (s Service) writeVersion(ctx context.Context, tx pgx.Tx, userID, envID, key, value string) error {
	sealed, err := s.Cipher.Seal([]byte(value))
	if err != nil {
		return err
	}
	var secretID string
	err = tx.QueryRow(ctx, `
		INSERT INTO secrets (environment_id, key_name) VALUES ($1, $2)
		    ON CONFLICT (environment_id, key_name) DO UPDATE SET deleted_at = NULL
		RETURNING id`, envID, key).Scan(&secretID)
	if err != nil {
		return err
	}
	var next int
	err = tx.QueryRow(ctx, `SELECT COALESCE(MAX(version_number), 0) + 1 FROM secret_versions WHERE secret_id = $1`,
		secretID).Scan(&next)
	if err != nil {
		return err
	}
	var versionID string
	err = tx.QueryRow(ctx, `
		INSERT INTO secret_versions (secret_id, ciphertext, nonce, version_number, created_by)
		VALUES ($1, $2, $3, $4, $5) RETURNING id`,
		secretID, sealed, []byte{}, next, userID).Scan(&versionID)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE secrets SET current_version_id = $1 WHERE id = $2`, versionID, secretID); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO audit_events (org_id, actor_id, action, target_type, target_id)
		SELECT p.org_id, NULLIF($1, '')::uuid, 'secret.write', 'secret', $2
		  FROM environments e JOIN projects p ON p.id = e.project_id
		 WHERE e.id = $3`, userID, secretID, envID)
	return err
}

// querier is anything that can run a query: the pool or a transaction.
type querier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

// readValues decrypts every current secret in an environment.
func (s Service) readValues(ctx context.Context, q querier, envID string) (map[string]string, error) {
	rows, err := q.Query(ctx, `
		SELECT s.key_name, v.ciphertext
		  FROM secrets s
		  JOIN secret_versions v ON v.id = s.current_version_id
		 WHERE s.environment_id = $1 AND s.deleted_at IS NULL`, envID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	values := map[string]string{}
	for rows.Next() {
		var key string
		var sealed []byte
		if err := rows.Scan(&key, &sealed); err != nil {
			return nil, err
		}
		plain, err := s.Cipher.Open(sealed)
		if err != nil {
			return nil, err
		}
		values[key] = string(plain)
	}
	return values, rows.Err()
}
