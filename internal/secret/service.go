package secret

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5/pgxpool"
	"shellhaki/envi/internal/access"
	crypt "shellhaki/envi/internal/crypto"
)

type Service struct {
	DB     *pgxpool.Pool
	Access access.Service
	Cipher *crypt.Cipher
}

func (s Service) Put(ctx context.Context, user, env, key, value string) error {
	if e := s.Access.Allow(ctx, user, env, "write"); e != nil {
		return e
	}
	blob, e := s.Cipher.Seal([]byte(value))
	if e != nil {
		return e
	}
	tx, e := s.DB.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	var id string
	e = tx.QueryRow(ctx, `INSERT INTO secrets(environment_id,key_name)VALUES($1,$2) ON CONFLICT(environment_id,key_name)DO UPDATE SET deleted_at=NULL RETURNING id`, env, key).Scan(&id)
	if e != nil {
		return e
	}
	var n int
	e = tx.QueryRow(ctx, `SELECT COALESCE(MAX(version_number),0)+1 FROM secret_versions WHERE secret_id=$1`, id).Scan(&n)
	if e != nil {
		return e
	}
	var version string
	e = tx.QueryRow(ctx, `INSERT INTO secret_versions(secret_id,ciphertext,nonce,version_number,created_by)VALUES($1,$2,$3,$4,$5)RETURNING id`, id, blob, []byte{}, n, user).Scan(&version)
	if e != nil {
		return e
	}
	if _, e = tx.Exec(ctx, `UPDATE secrets SET current_version_id=$1 WHERE id=$2`, version, id); e != nil {
		return e
	}
	_, e = tx.Exec(ctx, `INSERT INTO audit_events(org_id,actor_id,action,target_type,target_id)SELECT p.org_id,$1,'secret.write','secret',$2 FROM environments e JOIN projects p ON p.id=e.project_id WHERE e.id=$3`, user, id, env)
	if e != nil {
		return e
	}
	return tx.Commit(ctx)
}
func (s Service) Get(ctx context.Context, user, env string) (map[string]string, error) {
	if e := s.Access.Allow(ctx, user, env, "read"); e != nil {
		return nil, e
	}
	tx, e := s.DB.Begin(ctx)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback(ctx)
	rows, e := tx.Query(ctx, `SELECT s.id,s.key_name,v.ciphertext FROM secrets s JOIN secret_versions v ON v.id=s.current_version_id WHERE s.environment_id=$1 AND s.deleted_at IS NULL`, env)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := map[string]string{}
	var ids []string
	for rows.Next() {
		var id, key string
		var b []byte
		if e = rows.Scan(&id, &key, &b); e != nil {
			return nil, e
		}
		plain, x := s.Cipher.Open(b)
		if x != nil {
			return nil, x
		}
		out[key] = string(plain)
		ids = append(ids, id)
	}
	if e = rows.Err(); e != nil {
		return nil, e
	}
	rows.Close()
	for _, id := range ids {
		if _, e = tx.Exec(ctx, `INSERT INTO audit_events(org_id,actor_id,action,target_type,target_id)SELECT p.org_id,$1,'secret.read','secret',$2 FROM environments e JOIN projects p ON p.id=e.project_id WHERE e.id=$3`, user, id, env); e != nil {
			return nil, e
		}
	}
	if e = tx.Commit(ctx); e != nil {
		return nil, e
	}
	return out, nil
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

var _ = errors.New
