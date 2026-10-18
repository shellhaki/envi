package config

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// Deployment mode comes from ENVI_MODE and ENVI_CURRENCY, or from envi.config.json if those are unset.

type Mode string

const (
	SelfHosted Mode = "self-hosted"
	Hosted     Mode = "hosted"
)

// Deployment is what envi.config.json says, after defaults are applied.
type Deployment struct {
	Mode Mode `json:"mode"`
	// Currency the operator screen reports in, and the default for a new expense.
	Currency string `json:"currency"`
	// Source says where the mode came from, for the startup log.
	Source string `json:"-"`
}

// Billing reports whether this deployment charges anybody.
func (d Deployment) Billing() bool { return d.Mode == Hosted }

// DefaultDeployment is self-hosted, with no billing.
func DefaultDeployment() Deployment {
	return Deployment{Mode: SelfHosted, Currency: "NGN"}
}

// LoadDeployment reads envi.config.json from path.
func LoadDeployment(path string) Deployment {
	d := DefaultDeployment()

	var parsed Deployment
	source := "default: no ENVI_MODE and no " + path
	if raw, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(raw, &parsed); err != nil {
			parsed = Deployment{}
			source = path + " is not valid JSON"
		} else {
			source = path
		}
	}

	// The environment wins over the file.
	if v := Setting("ENVI_MODE"); v != "" {
		parsed.Mode = Mode(v)
		source = "ENVI_MODE"
	}
	if v := Setting("ENVI_CURRENCY"); v != "" {
		parsed.Currency = v
	}

	switch Mode(strings.ToLower(strings.TrimSpace(string(parsed.Mode)))) {
	case Hosted:
		d.Mode = Hosted
	case SelfHosted:
		d.Mode = SelfHosted
	case "":
	default:
		// Named, but not a mode.
		source = fmt.Sprintf("%s=%q is not hosted or self-hosted", source, string(parsed.Mode))
	}
	d.Source = source
	switch strings.ToUpper(strings.TrimSpace(parsed.Currency)) {
	case "NGN":
		d.Currency = "NGN"
	case "USD":
		d.Currency = "USD"
	}
	return d
}

// DeploymentPath is envi.config.json, or the path in ENVI_CONFIG_FILE.
func DeploymentPath() string {
	if p := strings.TrimSpace(os.Getenv("ENVI_CONFIG_FILE")); p != "" {
		return p
	}
	return "envi.config.json"
}

// Setting reads an environment variable, dropping surrounding spaces and one pair of quotes.
func Setting(name string) string {
	v := strings.TrimSpace(os.Getenv(name))
	if len(v) >= 2 && (v[0] == '"' || v[0] == '\'') && v[len(v)-1] == v[0] {
		v = strings.TrimSpace(v[1 : len(v)-1])
	}
	return v
}
