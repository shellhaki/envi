package main

import (
	"context"
	"errors"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"shellhaki/envi/internal/access"
	"shellhaki/envi/internal/admin"
	"shellhaki/envi/internal/announce"
	"shellhaki/envi/internal/api"
	"shellhaki/envi/internal/audit"
	"shellhaki/envi/internal/auth"
	"shellhaki/envi/internal/bachs"
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
	"strings"
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
	// Somebody signing up gets one note, once. Sending is asynchronous and
	// its failure is logged rather than returned: a sign-up that worked must
	// not be reported as failed because an email did not go out.
	w.OnNewUser = welcome.Service{Mailer: mail, WebURL: c.WebURL, DocsURL: c.DocsURL()}.Greet
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
	announcer := announce.Service{Admin: admin.Service{DB: db}, Mailer: mail, WebURL: c.WebURL}
	deployment := config.LoadDeployment(config.DeploymentPath())
	log.Printf("deployment mode: %s (billing %v)", deployment.Mode, deployment.Billing())

	// Billing runs only on the hosted deployment, and only with a Bachs key.
	// Hosted with no key is a warning rather than a crash: it would otherwise
	// take sign-in down along with billing.
	bill := billing.Service{DB: db, WebURL: c.WebURL, ReturnURL: os.Getenv("BACHS_RETURN_URL"), TeamProductID: os.Getenv("BACHS_TEAM_PRODUCT_ID")}
	webhookSecret := os.Getenv("BACHS_WEBHOOK_SECRET")
	if deployment.Billing() {
		client, err := bachs.New(os.Getenv("BACHS_SECRET_KEY"))
		switch {
		case err != nil:
			log.Printf("WARNING: billing is OFF: %v (hosted mode needs BACHS_SECRET_KEY)", err)
		case client.Live() && (c.Environment == "" || isLocalURL(c.DatabaseURL) || isLocalURL(c.WebURL)):
			// A live key on a laptop charges real cards from a developer's
			// machine against a development database. ENVIRONMENT alone is
			// not enough to tell: a .env copied from production carries
			// ENVIRONMENT=production with it. A local database or dashboard
			// is something production never has, so either one refuses.
			log.Fatal("refusing to start: a live Bachs key (sk_live_) on a local instance (empty ENVIRONMENT, or a localhost DATABASE_URL or ENVI_WEB_URL); use an sk_sandbox_ key locally")
		default:
			if !client.Live() && c.Environment == "production" {
				log.Printf("WARNING: production is using a sandbox Bachs key — no real money moves")
			}
			if webhookSecret == "" {
				log.Printf("WARNING: BACHS_WEBHOOK_SECRET is unset — payments will be taken but never recorded")
			}
			if returnTo := firstSet(bill.ReturnURL, bill.WebURL); isLocalURL(returnTo) {
				log.Printf("WARNING: checkout would return to %s, which Bachs refuses — every upgrade will fail. Set BACHS_RETURN_URL to a public URL", returnTo)
			}
			bill.Bachs = client
			invitations.Billing = bill
			go bill.Run(ctx, time.Hour)
			log.Printf("billing on (%s)", map[bool]string{true: "live", false: "sandbox"}[client.Live()])
		}
	}

	s := &http.Server{Addr: c.Address, Handler: api.Build(db, login, project.Service{DB: db}, secret.Service{DB: db, Access: ac, Cipher: cipher}, kv.Store{DB: db, Cipher: cipher}, audit.Service{DB: db}, service_token.Service{DB: db}, invitations, c.WebURL, isBeta, deployment, announcer, bill, webhookSecret), ReadHeaderTimeout: 5 * time.Second}
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

func firstSet(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// isLocalURL reports an address only this machine can reach: a web URL
// Bachs would refuse as a return address, or a database URL that marks this
// as a development instance. It parses the host rather than searching the
// string, because a database URL puts credentials in front of it
// (postgres://user:pass@localhost/envi).
func isLocalURL(raw string) bool {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return false
	}
	host := parsed.Hostname()
	if host == "" {
		// A DSN with no host connects over the local Unix socket.
		return parsed.Scheme == "postgres" || parsed.Scheme == "postgresql"
	}
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && (ip.IsLoopback() || ip.IsUnspecified())
}
