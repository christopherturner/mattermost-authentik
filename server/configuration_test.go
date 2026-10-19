package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGetEffectiveIssuerURL(t *testing.T) {
	t.Run("derived from AuthentikURL and slug", func(t *testing.T) {
		cfg := &Configuration{
			AuthentikURL:    "https://auth.example.com",
			ApplicationSlug: "mattermost",
		}
		assert.Equal(t, "https://auth.example.com/application/o/mattermost", cfg.GetEffectiveIssuerURL())
	})

	t.Run("trims trailing slashes", func(t *testing.T) {
		cfg := &Configuration{
			AuthentikURL:    "https://auth.example.com///",
			ApplicationSlug: "mattermost",
		}
		assert.Equal(t, "https://auth.example.com/application/o/mattermost", cfg.GetEffectiveIssuerURL())
	})

	t.Run("uses default slug if empty", func(t *testing.T) {
		cfg := &Configuration{
			AuthentikURL:    "https://auth.example.com",
			ApplicationSlug: "",
		}
		assert.Equal(t, "https://auth.example.com/application/o/mattermost", cfg.GetEffectiveIssuerURL())
	})

	t.Run("explicit IssuerURL takes precedence", func(t *testing.T) {
		cfg := &Configuration{
			AuthentikURL:    "https://auth.example.com",
			ApplicationSlug: "mattermost",
			IssuerURL:       "https://custom-oidc.example.com/oauth/",
		}
		assert.Equal(t, "https://custom-oidc.example.com/oauth", cfg.GetEffectiveIssuerURL())
	})
}

func TestConfigurationValidation(t *testing.T) {
	t.Run("disabled configuration is always valid", func(t *testing.T) {
		cfg := &Configuration{Enable: false}
		assert.NoError(t, cfg.IsValid())
	})

	t.Run("valid configuration passes", func(t *testing.T) {
		cfg := &Configuration{
			Enable:          true,
			AuthentikURL:    "https://auth.example.com",
			ApplicationSlug: "mattermost",
			ClientID:        "client-123",
			ClientSecret:    "secret-456",
		}
		assert.NoError(t, cfg.IsValid())
	})

	t.Run("missing client ID fails", func(t *testing.T) {
		cfg := &Configuration{
			Enable:          true,
			AuthentikURL:    "https://auth.example.com",
			ClientID:        "",
			ClientSecret:    "secret-456",
		}
		assert.Error(t, cfg.IsValid())
	})

	t.Run("missing client secret fails", func(t *testing.T) {
		cfg := &Configuration{
			Enable:          true,
			AuthentikURL:    "https://auth.example.com",
			ClientID:        "client-123",
			ClientSecret:    "",
		}
		assert.Error(t, cfg.IsValid())
	})

	t.Run("invalid URL scheme fails", func(t *testing.T) {
		cfg := &Configuration{
			Enable:          true,
			AuthentikURL:    "ftp://auth.example.com",
			ClientID:        "client-123",
			ClientSecret:    "secret-456",
		}
		assert.Error(t, cfg.IsValid())
	})

	t.Run("invalid custom certificate fails", func(t *testing.T) {
		cfg := &Configuration{
			Enable:          true,
			AuthentikURL:    "https://auth.example.com",
			ClientID:        "client-123",
			ClientSecret:    "secret-456",
			CustomCert:      "not-a-valid-pem",
		}
		assert.Error(t, cfg.IsValid())
	})

	t.Run("empty custom cert is allowed", func(t *testing.T) {
		cfg := &Configuration{
			Enable:          true,
			AuthentikURL:    "https://auth.example.com",
			ClientID:        "client-123",
			ClientSecret:    "secret-456",
			CustomCert:      "",
			SkipTLSVerify:   true,
		}
		assert.NoError(t, cfg.IsValid())
	})
}
