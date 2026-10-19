package main

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	oidc "github.com/coreos/go-oidc/v3/oidc"
	"github.com/gorilla/mux"
	"github.com/mattermost/mattermost/server/public/plugin"
	"golang.org/x/oauth2"
)

const (
	// PluginID is the unique identifier for this plugin matching plugin.json.
	PluginID = "io.christopherturner.mattermost-authentik"

	// AuthService is the service identifier stored in the user's AuthService field.
	AuthService = "authentik"

	// KVOAuthStatePrefix is the KV store key prefix for OAuth state tokens.
	KVOAuthStatePrefix = "authentik_state_"

	// KVEncryptionKey is the KV store key under which the HMAC signing key is persisted.
	KVEncryptionKey = "_authentik_encryption_key"

	// discoveryRetryInterval limits how often unauthenticated login requests can make the plugin contact the issuer.
	discoveryRetryInterval = 10 * time.Second
)

// Plugin implements the Mattermost plugin interface.
type Plugin struct {
	plugin.MattermostPlugin

	configurationLock sync.RWMutex
	configuration     *Configuration

	encryptionKey string

	oidcProvider *oidc.Provider
	oauth2Config *oauth2.Config
	oidcVerifier *oidc.IDTokenVerifier

	retryLock          sync.Mutex
	lastDiscoveryRetry time.Time

	router *mux.Router
}

// OnActivate is called when the plugin is activated.
func (p *Plugin) OnActivate() error {
	// Initialize or load HMAC signing key from KV store
	var key string
	if keyBytes, err := p.API.KVGet(KVEncryptionKey); err != nil {
		return fmt.Errorf("failed to load encryption key from KV store: %w", err)
	} else if len(keyBytes) > 0 {
		key = string(keyBytes)
	} else {
		generatedKey, genErr := generateRandomKey(32)
		if genErr != nil {
			return fmt.Errorf("failed to generate encryption key: %w", genErr)
		}
		if setErr := p.API.KVSet(KVEncryptionKey, []byte(generatedKey)); setErr != nil {
			return fmt.Errorf("failed to persist encryption key: %w", setErr)
		}
		key = generatedKey
	}

	p.configurationLock.Lock()
	p.encryptionKey = key
	p.configurationLock.Unlock()

	p.cleanExpiredOAuthStates()
	p.setupRouter()
	p.ensureClientBundleAvailable()

	config := p.getConfiguration()
	if config.Enable && config.IsValid() == nil {
		if err := p.initOIDCProvider(config); err != nil {
			p.API.LogWarn("Failed to initialize Authentik OIDC provider on activation (will retry on login)", "error", err.Error())
		}
	}

	p.API.LogInfo("Authentik Authentication plugin activated successfully")
	return nil
}

// OnConfigurationChange is called when the plugin configuration changes.
func (p *Plugin) OnConfigurationChange() error {
	var configuration = new(Configuration)
	if err := p.API.LoadPluginConfiguration(configuration); err != nil {
		return fmt.Errorf("failed to load plugin configuration: %w", err)
	}

	configuration.setDefaults()
	p.setConfiguration(configuration)
	p.ensureClientBundleAvailable()

	if configuration.Enable && configuration.IsValid() == nil {
		if err := p.initOIDCProvider(configuration); err != nil {
			p.API.LogWarn("Failed to reinitialize Authentik provider on config change", "error", err.Error())
		} else {
			p.API.LogInfo("Authentik OIDC provider initialized successfully")
		}
	} else {
		p.configurationLock.Lock()
		p.oidcProvider = nil
		p.oauth2Config = nil
		p.oidcVerifier = nil
		p.configurationLock.Unlock()
	}

	return nil
}

// ensureClientBundleAvailable ensures Mattermost's static file server can serve the webapp bundle.
// In Mattermost v10+, manifest bundle_path is computed as /static/<plugin-id>/<hash>_bundle.js,
// but the bundle is placed under ./client/plugins/<plugin-id>/.
// Creating a symlink from ./client/<plugin-id> to plugins/<plugin-id> ensures it is served at 200 OK.
func (p *Plugin) ensureClientBundleAvailable() {
	pluginClientDir := filepath.Join("client", "plugins", PluginID)
	targetDir := filepath.Join("client", PluginID)

	if _, err := os.Stat(pluginClientDir); err == nil {
		if _, err := os.Lstat(targetDir); os.IsNotExist(err) {
			if symErr := os.Symlink(filepath.Join("plugins", PluginID), targetDir); symErr != nil {
				p.API.LogWarn("Could not create client webapp symlink", "error", symErr.Error())
			} else {
				p.API.LogInfo("Created client webapp symlink for Authentik plugin bundle")
			}
		}
	}
}

// setupRouter initializes the HTTP routes.
func (p *Plugin) setupRouter() {
	r := mux.NewRouter()

	// Public configuration for webapp button rendering
	r.HandleFunc("/config", p.handleGetPublicConfig).Methods(http.MethodGet)

	// OAuth2 flow
	r.HandleFunc("/oauth2/connect", p.handleOAuth2Connect).Methods(http.MethodGet)
	r.HandleFunc("/oauth2/callback", p.handleOAuth2Callback).Methods(http.MethodGet)

	// Admin endpoints for LDAP account management
	r.HandleFunc("/api/v1/admin/ldap-users", p.handleGetLDAPUsers).Methods(http.MethodGet)
	r.HandleFunc("/api/v1/admin/unlock-ldap", p.handleUnlockLDAPUsers).Methods(http.MethodPost)
	r.HandleFunc("/api/v1/admin/deactivate-ldap", p.handleDeactivateLDAPUsers).Methods(http.MethodPost)

	// Admin endpoint for connectivity and credentials diagnosis
	r.HandleFunc("/api/v1/admin/diagnose", p.handleDiagnoseConnection).Methods(http.MethodPost)

	p.router = r
}

// ServeHTTP handles HTTP requests targeted at the plugin.
func (p *Plugin) ServeHTTP(c *plugin.Context, w http.ResponseWriter, r *http.Request) {
	if p.router == nil {
		http.Error(w, "Plugin initializing", http.StatusServiceUnavailable)
		return
	}
	p.router.ServeHTTP(w, r)
}

func (p *Plugin) getConfiguration() *Configuration {
	p.configurationLock.RLock()
	defer p.configurationLock.RUnlock()
	if p.configuration == nil {
		return &Configuration{}
	}
	return p.configuration
}

func (p *Plugin) setConfiguration(configuration *Configuration) {
	p.configurationLock.Lock()
	defer p.configurationLock.Unlock()
	p.configuration = configuration
}

func (p *Plugin) getEncryptionKey() string {
	p.configurationLock.RLock()
	key := p.encryptionKey
	p.configurationLock.RUnlock()
	if key != "" {
		return key
	}

	p.configurationLock.Lock()
	defer p.configurationLock.Unlock()
	if p.encryptionKey != "" {
		return p.encryptionKey
	}
	if p.API != nil {
		if keyBytes, err := p.API.KVGet(KVEncryptionKey); err == nil && len(keyBytes) > 0 {
			p.encryptionKey = string(keyBytes)
			return p.encryptionKey
		}
	}
	return "authentik_default_ephemeral_key"
}

// cleanExpiredOAuthStates asynchronously removes stale OAuth state tokens from the KV store.
func (p *Plugin) cleanExpiredOAuthStates() {
	go func() {
		if p.API == nil {
			return
		}
		for page := 0; ; page++ {
			keys, appErr := p.API.KVList(page, 100)
			if appErr != nil || len(keys) == 0 {
				break
			}
			for _, k := range keys {
				if strings.HasPrefix(k, KVOAuthStatePrefix) {
					if val, getErr := p.API.KVGet(k); getErr == nil && val != nil {
						var st OAuthState
						if json.Unmarshal(val, &st) == nil && time.Since(time.UnixMilli(st.CreateAt)) > stateExpiry {
							_ = p.API.KVDelete(k)
						}
					}
				}
			}
			if len(keys) < 100 {
				break
			}
		}
	}()
}

func (p *Plugin) getOAuthConfig() *oauth2.Config {
	p.configurationLock.RLock()
	defer p.configurationLock.RUnlock()
	return p.oauth2Config
}

func (p *Plugin) getOIDCVerifier() *oidc.IDTokenVerifier {
	p.configurationLock.RLock()
	defer p.configurationLock.RUnlock()
	return p.oidcVerifier
}

func (p *Plugin) getOIDCProvider() *oidc.Provider {
	p.configurationLock.RLock()
	defer p.configurationLock.RUnlock()
	return p.oidcProvider
}

// getHTTPClient returns an http.Client configured with optional custom Root CA or SkipTLSVerify.
func (p *Plugin) getHTTPClient(cfg *Configuration) *http.Client {
	tlsConfig := &tls.Config{
		InsecureSkipVerify: cfg.SkipTLSVerify, //nolint:gosec
	}

	if strings.TrimSpace(cfg.CustomCert) != "" {
		rootCAs, err := x509.SystemCertPool()
		if err != nil || rootCAs == nil {
			rootCAs = x509.NewCertPool()
		}
		if rootCAs.AppendCertsFromPEM([]byte(cfg.CustomCert)) {
			tlsConfig.RootCAs = rootCAs
		} else {
			p.API.LogWarn("Failed to append custom certificate to root CA pool")
		}
	}

	transport := &http.Transport{
		TLSClientConfig: tlsConfig,
		Proxy:           http.ProxyFromEnvironment,
	}

	return &http.Client{
		Transport: transport,
		Timeout:   30 * time.Second,
	}
}

// initOIDCProvider discovers and initializes the Authentik OIDC provider and verifier.
func (p *Plugin) initOIDCProvider(cfg *Configuration) error {
	issuerURL := cfg.GetEffectiveIssuerURL()
	if issuerURL == "" {
		return fmt.Errorf("issuer URL is empty")
	}

	if p.getSiteURL() == "" {
		p.API.LogWarn("Mattermost SiteURL is not configured. OAuth2 redirect URL may be invalid. Set ServiceSettings.SiteURL.")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	httpClient := p.getHTTPClient(cfg)
	ctx = oidc.ClientContext(ctx, httpClient)

	provider, err := oidc.NewProvider(ctx, issuerURL)
	if err != nil {
		return fmt.Errorf("OIDC discovery failed for %s: %w", issuerURL, err)
	}

	redirectURI := p.getCallbackURL()

	scopes := strings.Fields(cfg.Scope)
	if len(scopes) == 0 {
		scopes = []string{oidc.ScopeOpenID, "profile", "email"}
	}

	oauthConfig := &oauth2.Config{
		ClientID:     cfg.ClientID,
		ClientSecret: cfg.ClientSecret,
		Endpoint:     provider.Endpoint(),
		RedirectURL:  redirectURI,
		Scopes:       scopes,
	}

	verifier := provider.Verifier(&oidc.Config{
		ClientID: cfg.ClientID,
	})

	p.configurationLock.Lock()
	p.oidcProvider = provider
	p.oauth2Config = oauthConfig
	p.oidcVerifier = verifier
	p.configurationLock.Unlock()

	return nil
}

// retryOIDCProvider attempts discovery if provider is not yet loaded.
func (p *Plugin) retryOIDCProvider() {
	if p.getOAuthConfig() != nil {
		return
	}

	p.retryLock.Lock()
	defer p.retryLock.Unlock()

	if p.getOAuthConfig() != nil {
		return
	}

	if time.Since(p.lastDiscoveryRetry) < discoveryRetryInterval {
		return
	}
	p.lastDiscoveryRetry = time.Now()

	cfg := p.getConfiguration()
	if cfg.Enable && cfg.IsValid() == nil {
		if err := p.initOIDCProvider(cfg); err != nil {
			p.API.LogError("OIDC provider discovery retry failed", "error", err.Error())
		}
	}
}

// getSiteURL returns the Mattermost SiteURL with any trailing slash stripped.
func (p *Plugin) getSiteURL() string {
	siteURL := ""
	if config := p.API.GetConfig(); config != nil && config.ServiceSettings.SiteURL != nil {
		siteURL = *config.ServiceSettings.SiteURL
	}
	return strings.TrimRight(siteURL, "/")
}

// getCallbackURL returns the full redirect URL registered in Authentik.
func (p *Plugin) getCallbackURL() string {
	return fmt.Sprintf("%s/plugins/%s/oauth2/callback", p.getSiteURL(), PluginID)
}

func generateRandomKey(length int) (string, error) {
	b := make([]byte, length)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func getCookiePath(siteURL string) string {
	if u, err := url.Parse(siteURL); err == nil && u.Path != "" {
		return path.Clean(u.Path)
	}
	return "/"
}
