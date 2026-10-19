package main

import (
	"crypto/x509"
	"fmt"
	"net/url"
	"strings"
)

// Configuration captures the plugin's external configuration as set in the System Console.
type Configuration struct {
	Enable               bool   `json:"Enable"`
	EnforceSSO           bool   `json:"EnforceSSO"`
	AuthentikURL         string `json:"AuthentikURL"`
	ApplicationSlug      string `json:"ApplicationSlug"`
	IssuerURL            string `json:"IssuerURL"`
	ClientID             string `json:"ClientID"`
	ClientSecret         string `json:"ClientSecret"`
	Scope                string `json:"Scope"`
	AdminGroup           string `json:"AdminGroup"`
	SyncAdminRole        bool   `json:"SyncAdminRole"`
	DefaultTeam          string `json:"DefaultTeam"`
	AutoCreateAccounts   bool   `json:"AutoCreateAccounts"`
	AutoLinkByEmail      bool   `json:"AutoLinkByEmail"`
	RequireEmailVerified bool   `json:"RequireEmailVerified"`
	ButtonText           string `json:"ButtonText"`
	ButtonColor          string `json:"ButtonColor"`
	CustomCert           string `json:"CustomCert"`
	SkipTLSVerify        bool   `json:"SkipTLSVerify"`
}

// Clone shallow copies the configuration.
func (c *Configuration) Clone() *Configuration {
	var clone = *c
	return &clone
}

// GetEffectiveIssuerURL returns the OIDC issuer URL.
// If IssuerURL is explicitly configured, it is used directly.
// Otherwise, it is derived from AuthentikURL and ApplicationSlug.
func (c *Configuration) GetEffectiveIssuerURL() string {
	if c.IssuerURL != "" {
		return strings.TrimRight(strings.TrimSpace(c.IssuerURL), "/")
	}

	baseURL := strings.TrimRight(strings.TrimSpace(c.AuthentikURL), "/")
	if baseURL == "" {
		return ""
	}

	slug := strings.TrimSpace(c.ApplicationSlug)
	if slug == "" {
		slug = "mattermost"
	}

	return fmt.Sprintf("%s/application/o/%s", baseURL, slug)
}

// IsValid validates the configuration settings.
func (c *Configuration) IsValid() error {
	if !c.Enable {
		return nil
	}

	if c.ClientID == "" {
		return fmt.Errorf("client ID must not be empty")
	}

	if c.ClientSecret == "" {
		return fmt.Errorf("client secret must not be empty")
	}

	effectiveIssuer := c.GetEffectiveIssuerURL()
	if effectiveIssuer == "" {
		return fmt.Errorf("Authentik URL or Issuer URL must be specified")
	}

	parsed, err := url.Parse(effectiveIssuer)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return fmt.Errorf("invalid issuer URL: %s", effectiveIssuer)
	}

	if strings.TrimSpace(c.CustomCert) != "" {
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM([]byte(c.CustomCert)) {
			return fmt.Errorf("failed to parse custom certificate(s): ensure valid PEM format with BEGIN and END CERTIFICATE markers")
		}
	}

	return nil
}

// setDefaults applies sensible fallback values.
func (c *Configuration) setDefaults() {
	if c.Scope == "" {
		c.Scope = "openid profile email"
	}
	if c.ButtonText == "" {
		c.ButtonText = "Log in with Authentik"
	}
	if c.ButtonColor == "" {
		c.ButtonColor = "#FD4B2D"
	}
	if c.ApplicationSlug == "" {
		c.ApplicationSlug = "mattermost"
	}
}
