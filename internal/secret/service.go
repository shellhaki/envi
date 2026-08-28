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

var ErrConflict = errors.New("stale secret revision")

type Snapshot struct {
	Values   map[string]string `json:"values"`
	Revision int64             `json:"revision"`
}

type Service struct {
	DB     *pgxpool.Pool
	Access access.Service
	Cipher *crypt.Cipher
}

func (s Service) Put(ctx context.Context, user, env, key, value string) error {
	if e := s.Access.Allow(ctx, user, env, "write"); e != nil {
		return e
	}
	tx, e := s.DB.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	if _, e = tx.Exec(ctx, `UPDATE environments SET revision=revision+1 WHERE id=$1`, env); e != nil {
		return e
	}
	if e = s.putTx(ctx, tx, user, env, key, value); e != nil {
		return e
	}
	return tx.Commit(ctx)
}
func (s Service) Get(ctx context.Context, user, env string) (map[string]string, error) {
	x, e := s.Snapshot(ctx, user, env)
	return x.Values, e
}

func (s Service) Snapshot(ctx context.Context, user, env string) (Snapshot, error) {
	if e := s.Access.Allow(ctx, user, env, "read"); e != nil {
		return Snapshot{}, e
	}
	tx, e := s.DB.Begin(ctx)
	if e != nil {
		return Snapshot{}, e
	}
	defer tx.Rollback(ctx)
	var revision int64
	if e = tx.QueryRow(ctx, `SELECT revision FROM environments WHERE id=$1 FOR SHARE`, env).Scan(&revision); e != nil {
		return Snapshot{}, e
	}
	rows, e := tx.Query(ctx, `SELECT s.id,s.key_name,v.ciphertext FROM secrets s JOIN secret_versions v ON v.id=s.current_version_id WHERE s.environment_id=$1 AND s.deleted_at IS NULL`, env)
	if e != nil {
		return Snapshot{}, e
	}
	defer rows.Close()
	out := map[string]string{}
	var ids []string
	for rows.Next() {
		var id, key string
		var b []byte
		if e = rows.Scan(&id, &key, &b); e != nil {
			return Snapshot{}, e
		}
		plain, x := s.Cipher.Open(b)
		if x != nil {
			return Snapshot{}, x
		}
		out[key] = string(plain)
		ids = append(ids, id)
	}
	if e = rows.Err(); e != nil {
		return Snapshot{}, e
	}
	rows.Close()
	for _, id := range ids {
		if _, e = tx.Exec(ctx, `INSERT INTO audit_events(org_id,actor_id,action,target_type,target_id)SELECT p.org_id,$1,'secret.read','secret',$2 FROM environments e JOIN projects p ON p.id=e.project_id WHERE e.id=$3`, user, id, env); e != nil {
			return Snapshot{}, e
		}
	}
	if e = tx.Commit(ctx); e != nil {
		return Snapshot{}, e
	}
	return Snapshot{Values: out, Revision: revision}, nil
}

func (s Service) PutAll(ctx context.Context, user, env string, values map[string]string, expected int64) (int64, error) {
	if e := s.Access.Allow(ctx, user, env, "write"); e != nil {
		return 0, e
	}
	return s.putAll(ctx, user, env, values, expected)
}

func (s Service) PutAllService(ctx context.Context, env string, values map[string]string, expected int64) (int64, error) {
	return s.putAll(ctx, "", env, values, expected)
}

func (s Service) putAll(ctx context.Context, user, env string, values map[string]string, expected int64) (int64, error) {
	tx, e := s.DB.Begin(ctx)
	if e != nil {
		return 0, e
	}
	defer tx.Rollback(ctx)
	var revision int64
	e = tx.QueryRow(ctx, `UPDATE environments SET revision=revision+1 WHERE id=$1 AND revision=$2 RETURNING revision`, env, expected).Scan(&revision)
	if errors.Is(e, pgx.ErrNoRows) {
		return 0, ErrConflict
	}
	if e != nil {
		return 0, e
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if e = s.putTx(ctx, tx, user, env, key, values[key]); e != nil {
			return 0, e
		}
	}
	return revision, tx.Commit(ctx)
}

func (s Service) Revision(ctx context.Context, env string) (int64, error) {
	var revision int64
	e := s.DB.QueryRow(ctx, `SELECT revision FROM environments WHERE id=$1`, env).Scan(&revision)
	return revision, e
}

func (s Service) putTx(ctx context.Context, tx pgx.Tx, user, env, key, value string) error {
	blob, e := s.Cipher.Seal([]byte(value))
	if e != nil {
		return e
	}
	var id string
	if e = tx.QueryRow(ctx, `INSERT INTO secrets(environment_id,key_name)VALUES($1,$2) ON CONFLICT(environment_id,key_name)DO UPDATE SET deleted_at=NULL RETURNING id`, env, key).Scan(&id); e != nil {
		return e
	}
	var n int
	if e = tx.QueryRow(ctx, `SELECT COALESCE(MAX(version_number),0)+1 FROM secret_versions WHERE secret_id=$1`, id).Scan(&n); e != nil {
		return e
	}
	var version string
	if e = tx.QueryRow(ctx, `INSERT INTO secret_versions(secret_id,ciphertext,nonce,version_number,created_by)VALUES($1,$2,$3,$4,$5)RETURNING id`, id, blob, []byte{}, n, user).Scan(&version); e != nil {
		return e
	}
	if _, e = tx.Exec(ctx, `UPDATE secrets SET current_version_id=$1 WHERE id=$2`, version, id); e != nil {
		return e
	}
	_, e = tx.Exec(ctx, `INSERT INTO audit_events(org_id,actor_id,action,target_type,target_id)SELECT p.org_id,NULLIF($1,'')::uuid,'secret.write','secret',$2 FROM environments e JOIN projects p ON p.id=e.project_id WHERE e.id=$3`, user, id, env)
	return e
}

func (s Service) Delete(ctx context.Context, user, env, key string) error {
	if e := s.Access.Allow(ctx, user, env, "write"); e != nil {
		return e
	}
	tx, e := s.DB.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	var id string
	e = tx.QueryRow(ctx, `UPDATE secrets SET deleted_at=now() WHERE environment_id=$1 AND key_name=$2 AND deleted_at IS NULL RETURNING id`, env, key).Scan(&id)
	if e != nil {
		return e
	}
	if _, e = tx.Exec(ctx, `INSERT INTO audit_events(org_id,actor_id,action,target_type,target_id)SELECT p.org_id,$1,'secret.delete','secret',$2 FROM environments e JOIN projects p ON p.id=e.project_id WHERE e.id=$3`, user, id, env); e != nil {
		return e
	}
	return tx.Commit(ctx)
}

func (s Service) GetService(ctx context.Context, serviceID, env string) (map[string]string, error) {
	rows, e := s.DB.Query(ctx, `SELECT s.key_name,v.ciphertext FROM secrets s JOIN secret_versions v ON v.id=s.current_version_id WHERE s.environment_id=$1 AND s.deleted_at IS NULL`, env)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var k string
		var b []byte
		if e = rows.Scan(&k, &b); e != nil {
			return nil, e
		}
		p, x := s.Cipher.Open(b)
		if x != nil {
			return nil, x
		}
		out[k] = string(p)
	}
	_, _ = s.DB.Exec(ctx, `INSERT INTO audit_events(org_id,action,target_type,target_id,metadata)SELECT p.org_id,'secret.read','service_identity',$1,jsonb_build_object('environment_id',$2) FROM environments e JOIN projects p ON p.id=e.project_id WHERE e.id=$2`, serviceID, env)
	return out, rows.Err()
}
func (s Service) PutService(ctx context.Context, env, key, value string) error {
	tx, e := s.DB.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	if _, e = tx.Exec(ctx, `UPDATE environments SET revision=revision+1 WHERE id=$1`, env); e != nil {
		return e
	}
	if e = s.putTx(ctx, tx, "", env, key, value); e != nil {
		return e
	}
	return tx.Commit(ctx)
}
