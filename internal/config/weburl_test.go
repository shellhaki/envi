package config

import "testing"

// A production instance on the localhost default would mail invitation links
// that resolve to the recipient's own machine.
func TestReadRejectsLocalWebURLInProduction(t *testing.T) {
	base := func(t *testing.T) {
		t.Setenv("DATABASE_URL", "postgres://x")
		t.Setenv("REDIS_URL", "redis://x")
		t.Setenv("RESEND_API_KEY", "key")
		t.Setenv("RESEND_FROM", "Envi <no@reply>")
		t.Setenv("ENVI_ENCRYPTION_KEY", "01234567890123456789012345678901")
	}
	t.Run("localhost default is rejected", func(t *testing.T) {
		base(t)
		t.Setenv("ENVIRONMENT", "production")
		t.Setenv("ENVI_WEB_URL", "")
		if _, err := Read(); err == nil {
			t.Fatal("production accepted the localhost default for ENVI_WEB_URL")
		}
	})
	// The case that was actually live: a beta deployment serving real testers
	// while building localhost links.
	t.Run("beta is rejected too", func(t *testing.T) {
		base(t)
		t.Setenv("ENVIRONMENT", "beta")
		t.Setenv("ENVI_WEB_URL", "http://localhost:3000")
		if _, err := Read(); err == nil {
			t.Fatal("a beta deployment accepted localhost links")
		}
	})
	t.Run("an unfamiliar environment name is rejected", func(t *testing.T) {
		base(t)
		t.Setenv("ENVIRONMENT", "staging")
		t.Setenv("ENVI_WEB_URL", "http://localhost:3000")
		if _, err := Read(); err == nil {
			t.Fatal("a named environment accepted localhost links")
		}
	})
	t.Run("explicit 127.0.0.1 is rejected", func(t *testing.T) {
		base(t)
		t.Setenv("ENVIRONMENT", "production")
		t.Setenv("ENVI_WEB_URL", "http://127.0.0.1:3000")
		if _, err := Read(); err == nil {
			t.Fatal("production accepted a loopback ENVI_WEB_URL")
		}
	})
	t.Run("public URL is accepted", func(t *testing.T) {
		base(t)
		t.Setenv("ENVIRONMENT", "production")
		t.Setenv("ENVI_WEB_URL", "https://envisecrets.com")
		if _, err := Read(); err != nil {
			t.Fatalf("public ENVI_WEB_URL rejected: %v", err)
		}
	})
	// The case that actually shipped broken links: a developer's instance
	// pointed at the shared database while still building localhost links.
	t.Run("remote database with local links is flagged", func(t *testing.T) {
		base(t)
		t.Setenv("DATABASE_URL", "postgresql://user@ep-something.us-east-2.aws.neon.tech/envi")
		t.Setenv("ENVIRONMENT", "")
		t.Setenv("ENVI_WEB_URL", "http://localhost:3000")
		c, err := Read()
		if err != nil {
			t.Fatalf("local development against a remote database should still boot: %v", err)
		}
		if !c.WritesToRemoteDatabaseWithLocalLinks() {
			t.Fatal("expected a remote database with localhost links to be flagged")
		}
	})
	t.Run("local database with local links is not flagged", func(t *testing.T) {
		base(t)
		t.Setenv("DATABASE_URL", "postgresql://shellhaki@localhost:5432/envi")
		t.Setenv("ENVIRONMENT", "")
		t.Setenv("ENVI_WEB_URL", "http://localhost:3000")
		c, err := Read()
		if err != nil {
			t.Fatal(err)
		}
		if c.WritesToRemoteDatabaseWithLocalLinks() {
			t.Fatal("an entirely local setup should not be flagged")
		}
	})
	t.Run("local development is still allowed", func(t *testing.T) {
		base(t)
		t.Setenv("ENVIRONMENT", "")
		t.Setenv("ENVI_WEB_URL", "")
		c, err := Read()
		if err != nil {
			t.Fatalf("local development rejected: %v", err)
		}
		if !c.IsLocalWebURL() {
			t.Fatal("expected the localhost default to be reported as local")
		}
	})
}
