package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"shellhaki/envi/internal/access"
	"shellhaki/envi/internal/api"
	"shellhaki/envi/internal/audit"
	"shellhaki/envi/internal/auth"
	"shellhaki/envi/internal/beta"
	"shellhaki/envi/internal/config"
	crypt "shellhaki/envi/internal/crypto"
	"shellhaki/envi/internal/invitation"
	"shellhaki/envi/internal/mailer"
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
	// A .env file is a local-development convenience. Under Docker, systemd or
	// a process manager the environment is already populated, so a missing
	// file is not an error — a malformed one still is.
	if err := godotenv.Load(); err != nil && !errors.Is(err, os.ErrNotExist) {
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
	// How long logins last lives in internal/auth/sessions.go.
	mail := mailer.Resend{APIKey: c.ResendAPIKey, From: c.ResendFrom}
	isBeta := c.Environment == "beta"
	// Stated once at boot so it is obvious which instance a CLI is talking to:
	// an invitation sent against a local API lands in the local database, with
	// a link nobody else can open.
	if c.WritesToRemoteDatabaseWithLocalLinks() {
		log.Printf("WARNING: ENVI_WEB_URL is %s but DATABASE_URL is not local — invitations created here are written to the shared database and emailed with links only you can open. Set ENVI_WEB_URL to the public dashboard URL.", c.WebURL)
	} else if c.IsLocalWebURL() {
		log.Printf("WARNING: ENVI_WEB_URL is %s — invitation and device links will only work on this machine", c.WebURL)
	}
	login := auth.LoginSettings{
		Codes:               otp.Service{Store: otp.Redis{Client: rc}, TTL: 10 * time.Minute, MaxAttempts: 10, RequestLimit: 20},
		Mailer:              otp.Resend{Client: mail},
		FindOrCreateAccount: w.Identity,
	}
	if isBeta {
		allowlist, e := beta.Load("beta_testers.json")
		if e != nil {
			log.Fatal(e)
		}
		login.AllowEmail = allowlist.Allowed
		log.Printf("beta mode: sign-in restricted to beta_testers.json")
	}
	cipher, e := crypt.New([]byte(c.EncryptionKey))
	if e != nil {
		log.Fatal(e)
	}
	ac := access.Service{DB: db}
	invitations := invitation.Service{DB: db, Mailer: mail, WebURL: c.WebURL}
	s := &http.Server{Addr: c.Address, Handler: api.Build(db, login, project.Service{DB: db}, secret.Service{DB: db, Access: ac, Cipher: cipher}, audit.Service{DB: db}, service_token.Service{DB: db}, invitations, c.WebURL, isBeta), ReadHeaderTimeout: 5 * time.Second}
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
