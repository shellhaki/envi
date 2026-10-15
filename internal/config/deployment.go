package config

import (
	"encoding/json"
	"os"
	"strings"
)

/*
Deployment mode, read from envi.config.json beside the binary, or from the
ENVI_MODE and ENVI_CURRENCY environment variables, which win.

	{
	  "mode": "hosted",
	  "currency": "NGN"
	}

Two modes:

  - "self-hosted" — somebody running their own instance. No billing, no quotas,
    no revenue to report. This is the default, deliberately: the repository is
    public and the whole stack is self-hostable, so a missing or unreadable
    config file must never switch billing on in somebody else's
    infrastructure.

  - "hosted" — the commercial deployment. Billing applies, revenue is reported.

The mode is a product decision, not a security boundary, which is why a file is
fine for it. Who may read the operator screen is a security boundary, and that
stays in ENVI_ADMINS, where only whoever can deploy can change it.
*/

type Mode string

const (
	SelfHosted Mode = "self-hosted"
	Hosted     Mode = "hosted"
)

// Deployment is what envi.config.json says, after defaults are applied.
type Deployment struct {
	Mode Mode `json:"mode"`
	// Currency the operator screen reports in, and the default for a new
	// expense. Per-expense currency is still recorded on the row.
	Currency string `json:"currency"`
}

// Billing reports whether this deployment charges anybody.
func (d Deployment) Billing() bool { return d.Mode == Hosted }

// DefaultDeployment is what an instance gets with no config file: somebody
// else's, running for free.
func DefaultDeployment() Deployment {
	return Deployment{Mode: SelfHosted, Currency: "NGN"}
}

/*
LoadDeployment reads envi.config.json from path.

A missing file is not an error — it is the ordinary case for a fresh clone, and
it means self-hosted. A malformed file is also not an error for the same
reason: refusing to start because somebody mistyped a currency would turn a
cosmetic setting into an outage. Either way the safe default applies.
*/
func LoadDeployment(path string) Deployment {
	d := DefaultDeployment()

	var parsed Deployment
	if raw, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(raw, &parsed); err != nil {
			parsed = Deployment{}
		}
	}

	// The environment wins over the file. A container image built from this
	// repository does not carry envi.config.json — the final stage copies
	// only the binaries — so on Coolify or any other container platform the
	// mode has to arrive as a variable, the way every other setting does.
	if v := Setting("ENVI_MODE"); v != "" {
		parsed.Mode = Mode(v)
	}
	if v := Setting("ENVI_CURRENCY"); v != "" {
		parsed.Currency = v
	}

	switch Mode(strings.ToLower(strings.TrimSpace(string(parsed.Mode)))) {
	case Hosted:
		d.Mode = Hosted
	case SelfHosted:
		d.Mode = SelfHosted
	}
	switch strings.ToUpper(strings.TrimSpace(parsed.Currency)) {
	case "NGN":
		d.Currency = "NGN"
	case "USD":
		d.Currency = "USD"
	}
	return d
}

// DeploymentPath is where the config file lives, overridable for tests and for
// deployments that keep configuration somewhere other than the working
// directory.
func DeploymentPath() string {
	if p := strings.TrimSpace(os.Getenv("ENVI_CONFIG_FILE")); p != "" {
		return p
	}
	return "envi.config.json"
}

/*
Setting reads an environment variable the way a person typed it into a
deployment dashboard: surrounding spaces and one pair of matching quotes are
dropped.

Hosting panels differ on whether quotes are part of the value. Typed as
"hosted", some pass the quote marks through, and the mode then fails to match
and billing silently stays off; a key that starts with a quote fails its
sk_live_ check the same way. No value Envi reads is meant to begin and end
with a quote, so stripping them is always safe.
*/
func Setting(name string) string {
	v := strings.TrimSpace(os.Getenv(name))
	if len(v) >= 2 && (v[0] == '"' || v[0] == '\'') && v[len(v)-1] == v[0] {
		v = strings.TrimSpace(v[1 : len(v)-1])
	}
	return v
}
