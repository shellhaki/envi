package otp

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

func TestRedisIntegration(t *testing.T) {
	if os.Getenv("ENVI_INTEGRATION") != "1" {
		t.Skip("set ENVI_INTEGRATION=1")
	}
	opts, err := redis.ParseURL(os.Getenv("REDIS_URL"))
	if err != nil {
		t.Fatal(err)
	}
	c := redis.NewClient(opts)
	defer c.Close()
	ctx := context.Background()
	if err = c.Ping(ctx).Err(); err != nil {
		t.Fatal(err)
	}
	_ = c.FlushDB(ctx).Err()
	s := Service{Store: Redis{Client: c}, TTL: time.Minute, MaxAttempts: 2, RequestLimit: 5}
	code, err := s.Issue(ctx, "live@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Verify(ctx, "live@example.com", code); err != nil {
		t.Fatal(err)
	}
	if err = s.Verify(ctx, "live@example.com", code); err == nil {
		t.Fatal("reused OTP accepted")
	}
	for i := 0; i < 5; i++ {
		_, _ = s.Issue(ctx, "limit@example.com")
	}
	if _, err = s.Issue(ctx, "limit@example.com"); err == nil {
		t.Fatal("request limit not enforced")
	}
}
