package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"shellhaki/envi/internal/access"
	"shellhaki/envi/internal/api"
	"shellhaki/envi/internal/audit"
	"shellhaki/envi/internal/auth"
	"shellhaki/envi/internal/config"
	crypt "shellhaki/envi/internal/crypto"
	"shellhaki/envi/internal/invitation"
	"shellhaki/envi/internal/otp"
	"shellhaki/envi/internal/project"
	"shellhaki/envi/internal/secret"
	"shellhaki/envi/internal/service_token"
	"shellhaki/envi/internal/workspace"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"
	"github.com/redis/go-redis/v9"
)

func main() {
	err := godotenv.Load()
	if err != nil {
		log.Fatal(err)
	}
	c, e := config.Read()
	if e != nil {
		log.Fatal(e)
	}
	ctx := context.Background()
	db, e := pgxpool.New(ctx, c.DatabaseURL)
	if e != nil {
		log.Fatal(e)
	}
	defer db.Close()
	if e = db.Ping(ctx); e != nil {
		log.Fatal(e)
	}
	o, e := redis.ParseURL(c.RedisURL)
	if e != nil {
		log.Fatal(e)
	}
	rc := redis.NewClient(o)
	defer rc.Close()
	if e = rc.Ping(ctx).Err(); e != nil {
		log.Fatal(e)
	}
	w := workspace.Service{DB: db}
	const (
		accessTTL  = 15 * time.Minute
		refreshTTL = 30 * 24 * time.Hour
	)
	tokens := &auth.PostgresTokens{DB: db, AccessTTL: accessTTL, RefreshTTL: refreshTTL}
	a := auth.Service{OTP: otp.Service{Store: otp.Redis{Client: rc}, TTL: 10 * time.Minute, MaxAttempts: 10, RequestLimit: 20}, Mailer: otp.Gmail{Email: c.SMTPEmail, Password: c.SMTPPassword}, Provision: w.Identity, AccessTTL: accessTTL, RefreshTTL: refreshTTL}
	deviceSvc := auth.DeviceService{Store: &auth.PostgresDeviceStore{DB: db}, Tokens: tokens, TTL: 10 * time.Minute, Interval: 5 * time.Second}
	cipher, e := crypt.New([]byte(c.EncryptionKey))
	if e != nil {
		log.Fatal(e)
	}
	ac := access.Service{DB: db}
	s := &http.Server{Addr: c.Address, Handler: api.Build(a, tokens, project.Service{DB: db}, secret.Service{DB: db, Access: ac, Cipher: cipher}, audit.Service{DB: db}, service_token.Service{DB: db}, invitation.Service{DB: db}, db, deviceSvc, c.WebURL, accessTTL), ReadHeaderTimeout: 5 * time.Second}
	go func() {
		log.Printf("API listening on %s", c.Address)
		if e := s.ListenAndServe(); e != nil && e != http.ErrServerClosed {
			log.Fatal(e)
		}
	}()
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if e = s.Shutdown(shutdown); e != nil {
		log.Print(e)
	}
}
