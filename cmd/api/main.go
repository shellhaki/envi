// Command api is the Envi backend. It reads configuration, connects to
// Postgres and Redis, wires the services together and serves HTTP. The rules
// live in internal/; this file only assembles them.
package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"
	"github.com/redis/go-redis/v9"

	"shellhaki/envi/internal/access"
	"shellhaki/envi/internal/admin"
	"shellhaki/envi/internal/announce"
	"shellhaki/envi/internal/api"
	"shellhaki/envi/internal/audit"
	"shellhaki/envi/internal/auth"
	"shellhaki/envi/internal/beta"
	"shellhaki/envi/internal/billing"
	"shellhaki/envi/internal/config"
	crypt "shellhaki/envi/internal/crypto"
	"shellhaki/envi/internal/invitation"
	"shellhaki/envi/internal/kv"
	"shellhaki/envi/internal/mailer"
	"shellhaki/envi/internal/otp"
	"shellhaki/envi/internal/project"
	"shellhaki/envi/internal/secret"
	"shellhaki/envi/internal/service_token"
	"shellhaki/envi/internal/welcome"
	"shellhaki/envi/internal/workspace"
)

func main() {
	// .env is for local development; deployed, the environment is already set.
	if err := godotenv.Load(); err != nil && !errors.Is(err, os.ErrNotExist) {
		log.Fatal(err)
	}
	c, err := config.Read()
	if err != nil {
		log.Fatal(err)
	}
	warnAboutLinks(c)

	ctx := context.Background()
	db := connectPostgres(ctx, c.DatabaseURL)
	defer db.Close()
	rc := connectRedis(ctx, c.RedisURL)
	defer rc.Close()

	cipher, err := crypt.New([]byte(c.EncryptionKey))
	if err != nil {
		log.Fatal(err)
	}
	mail := mailer.Resend{APIKey: c.ResendAPIKey, From: c.ResendFrom}

	accounts := workspace.Service{DB: db}
	accounts.OnNewUser = welcome.Service{Mailer: mail, WebURL: c.WebURL, DocsURL: c.DocsURL()}.Greet

	isBeta := c.Environment == "beta"
	login := auth.LoginSettings{
		Codes:               otp.Service{Store: otp.Redis{Client: rc}, TTL: 10 * time.Minute, MaxAttempts: 10, RequestLimit: 20},
		Mailer:              otp.Resend{Client: mail},
		FindOrCreateAccount: accounts.Identity,
	}
	if isBeta {
		allowlist, err := beta.Load("beta_testers.json")
		if err != nil {
			log.Fatal(err)
		}
		login.AllowEmail = allowlist.Allowed
		log.Printf("beta mode: sign-in restricted to beta_testers.json")
	}

	deployment := config.LoadDeployment(config.DeploymentPath())
	log.Printf("deployment mode: %s (billing %v) — from %s", deployment.Mode, deployment.Billing(), deployment.Source)

	bill, err := billing.New(db, c, deployment)
	if err != nil {
		log.Fatal(err)
	}
	invitations := invitation.Service{DB: db, Mailer: mail, WebURL: c.WebURL}
	if bill.Enabled() {
		invitations.Billing = bill
		go bill.Run(ctx, time.Hour)
	}

	handler := api.Build(db, login,
		project.Service{DB: db},
		secret.Service{DB: db, Access: access.Service{DB: db}, Cipher: cipher},
		kv.Store{DB: db, Cipher: cipher},
		audit.Service{DB: db},
		service_token.Service{DB: db},
		invitations, c.WebURL, isBeta, deployment,
		announce.Service{Admin: admin.Service{DB: db}, Mailer: mail, WebURL: c.WebURL},
		bill,
	)
	serve(&http.Server{Addr: c.Address, Handler: handler, ReadHeaderTimeout: 5 * time.Second})
}

func connectPostgres(ctx context.Context, url string) *pgxpool.Pool {
	db, err := pgxpool.New(ctx, url)
	if err != nil {
		log.Fatal(err)
	}
	if err := db.Ping(ctx); err != nil {
		log.Fatal(err)
	}
	return db
}

func connectRedis(ctx context.Context, url string) *redis.Client {
	options, err := redis.ParseURL(url)
	if err != nil {
		log.Fatal(err)
	}
	rc := redis.NewClient(options)
	if err := rc.Ping(ctx).Err(); err != nil {
		log.Fatal(err)
	}
	return rc
}

// warnAboutLinks catches the instance that writes to a shared database but
// emails links only this machine can open.
func warnAboutLinks(c config.Config) {
	if c.WritesToRemoteDatabaseWithLocalLinks() {
		log.Printf("WARNING: ENVI_WEB_URL is %s but DATABASE_URL is not local — invitations land in the shared database with links only you can open", c.WebURL)
	} else if c.IsLocalWebURL() {
		log.Printf("WARNING: ENVI_WEB_URL is %s — invitation and device links will only work on this machine", c.WebURL)
	}
}

// serve runs the server until SIGINT or SIGTERM, then gives in-flight
// requests ten seconds to finish.
func serve(s *http.Server) {
	go func() {
		log.Printf("API listening on %s", s.Addr)
		if err := s.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal(err)
		}
	}()
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := s.Shutdown(ctx); err != nil {
		log.Print(err)
	}
}
