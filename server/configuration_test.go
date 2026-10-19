package main

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

	t.Run("invalid custom certificate string fails", func(t *testing.T) {
		cfg := &Configuration{
			Enable:       true,
			AuthentikURL: "https://auth.example.com",
			ClientID:     "client-123",
			ClientSecret: "secret-456",
			CustomCert:   "not-a-valid-pem",
		}
		assert.Error(t, cfg.IsValid())
	})

	t.Run("valid custom certificate PEM string passes", func(t *testing.T) {
		certPEM := generateTestCertPEM(t)
		cfg := &Configuration{
			Enable:       true,
			AuthentikURL: "https://auth.example.com",
			ClientID:     "client-123",
			ClientSecret: "secret-456",
			CustomCert:   certPEM,
		}
		assert.NoError(t, cfg.IsValid())
	})

	t.Run("valid custom certificate file path passes", func(t *testing.T) {
		certPEM := generateTestCertPEM(t)
		tmpDir := t.TempDir()
		certPath := filepath.Join(tmpDir, "authentik-ca.crt")
		err := os.WriteFile(certPath, []byte(certPEM), 0600)
		require.NoError(t, err)

		cfg := &Configuration{
			Enable:       true,
			AuthentikURL: "https://auth.example.com",
			ClientID:     "client-123",
			ClientSecret: "secret-456",
			CustomCert:   certPath,
		}
		assert.NoError(t, cfg.IsValid())
	})

	t.Run("custom certificate file with non-PEM contents fails", func(t *testing.T) {
		tmpDir := t.TempDir()
		certPath := filepath.Join(tmpDir, "corrupted.crt")
		err := os.WriteFile(certPath, []byte("random non-pem bytes"), 0600)
		require.NoError(t, err)

		cfg := &Configuration{
			Enable:       true,
			AuthentikURL: "https://auth.example.com",
			ClientID:     "client-123",
			ClientSecret: "secret-456",
			CustomCert:   certPath,
		}
		assert.Error(t, cfg.IsValid())
	})

	t.Run("non-existent certificate file fails", func(t *testing.T) {
		cfg := &Configuration{
			Enable:       true,
			AuthentikURL: "https://auth.example.com",
			ClientID:     "client-123",
			ClientSecret: "secret-456",
			CustomCert:   "/path/does/not/exist/ca.crt",
		}
		assert.Error(t, cfg.IsValid())
	})

	t.Run("empty custom cert is allowed", func(t *testing.T) {
		cfg := &Configuration{
			Enable:        true,
			AuthentikURL:  "https://auth.example.com",
			ClientID:      "client-123",
			ClientSecret:  "secret-456",
			CustomCert:    "",
			SkipTLSVerify: true,
		}
		assert.NoError(t, cfg.IsValid())
	})

	t.Run("scope missing openid fails", func(t *testing.T) {
		cfg := &Configuration{
			Enable:          true,
			AuthentikURL:    "https://auth.example.com",
			ApplicationSlug: "mattermost",
			ClientID:        "client-123",
			ClientSecret:    "secret-456",
			Scope:           "profile email groups",
		}
		assert.Error(t, cfg.IsValid())
	})

	t.Run("scope missing profile fails", func(t *testing.T) {
		cfg := &Configuration{
			Enable:          true,
			AuthentikURL:    "https://auth.example.com",
			ApplicationSlug: "mattermost",
			ClientID:        "client-123",
			ClientSecret:    "secret-456",
			Scope:           "openid email groups",
		}
		assert.Error(t, cfg.IsValid())
	})

	t.Run("scope missing email fails", func(t *testing.T) {
		cfg := &Configuration{
			Enable:          true,
			AuthentikURL:    "https://auth.example.com",
			ApplicationSlug: "mattermost",
			ClientID:        "client-123",
			ClientSecret:    "secret-456",
			Scope:           "openid profile groups",
		}
		assert.Error(t, cfg.IsValid())
	})

	t.Run("scope missing groups fails", func(t *testing.T) {
		cfg := &Configuration{
			Enable:          true,
			AuthentikURL:    "https://auth.example.com",
			ApplicationSlug: "mattermost",
			ClientID:        "client-123",
			ClientSecret:    "secret-456",
			Scope:           "openid profile email",
		}
		assert.Error(t, cfg.IsValid())
	})

	t.Run("scope with openid profile email groups passes", func(t *testing.T) {
		cfg := &Configuration{
			Enable:          true,
			AuthentikURL:    "https://auth.example.com",
			ApplicationSlug: "mattermost",
			ClientID:        "client-123",
			ClientSecret:    "secret-456",
			Scope:           "openid profile email groups",
		}
		assert.NoError(t, cfg.IsValid())
	})

	t.Run("setDefaults sets Scope to openid profile email groups", func(t *testing.T) {
		cfg := &Configuration{}
		cfg.setDefaults()
		assert.Equal(t, "openid profile email groups", cfg.Scope)
	})
}

func generateTestCertPEM(t *testing.T) string {
	t.Helper()
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			CommonName: "authentik.local",
		},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}

	derBytes, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	require.NoError(t, err)

	var certPEM bytes.Buffer
	err = pem.Encode(&certPEM, &pem.Block{Type: "CERTIFICATE", Bytes: derBytes})
	require.NoError(t, err)

	return certPEM.String()
}
