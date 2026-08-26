package otp

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"
)

type Redis struct{ Client *redis.Client }

func (r Redis) Set(ctx context.Context, key, value string, ttl time.Duration) error {
	return r.Client.Set(ctx, key, value, ttl).Err()
}
func (r Redis) GetDel(ctx context.Context, key string) (string, error) {
	return r.Client.GetDel(ctx, key).Result()
}
func (r Redis) Increment(ctx context.Context, key string, ttl time.Duration) (int64, error) {
	n, err := r.Client.Incr(ctx, key).Result()
	if err == nil && n == 1 {
		err = r.Client.Expire(ctx, key, ttl).Err()
	}
	return n, err
}
