package main

import (
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mattermost/mattermost/server/public/model"
	"github.com/mattermost/mattermost/server/public/plugin/plugintest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func setupTestPluginWithAPI() (*Plugin, *plugintest.API) {
	p := &Plugin{}
	api := &plugintest.API{}
	siteURL := "http://mattermost.example.com"
	api.On("GetConfig").Return(&model.Config{
		ServiceSettings: model.ServiceSettings{
			SiteURL: &siteURL,
		},
	}).Maybe()
	p.SetAPI(api)
	p.setupRouter()
	return p, api
}

func TestAdminEndpoints_AuthorizationCheck(t *testing.T) {
	p, api := setupTestPluginWithAPI()

	// Non-admin user does not have system manage permission
	api.On("HasPermissionTo", "non-admin-id", model.PermissionManageSystem).Return(false)

	endpoints := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api/v1/admin/ldap-users"},
		{http.MethodPost, "/api/v1/admin/unlock-ldap"},
		{http.MethodPost, "/api/v1/admin/deactivate-ldap"},
		{http.MethodPost, "/api/v1/admin/diagnose"},
	}

	for _, ep := range endpoints {
		t.Run("missing user ID on "+ep.path, func(t *testing.T) {
			req := httptest.NewRequest(ep.method, ep.path, strings.NewReader("{}"))
			w := httptest.NewRecorder()
			p.router.ServeHTTP(w, req)
			assert.Equal(t, http.StatusUnauthorized, w.Code)
		})

		t.Run("non-admin user on "+ep.path, func(t *testing.T) {
			req := httptest.NewRequest(ep.method, ep.path, strings.NewReader("{}"))
			req.Header.Set("Mattermost-User-Id", "non-admin-id")
			w := httptest.NewRecorder()
			p.router.ServeHTTP(w, req)
			assert.Equal(t, http.StatusForbidden, w.Code)
		})
	}
}

func TestHandleGetLDAPUsers(t *testing.T) {
	p, api := setupTestPluginWithAPI()

	api.On("HasPermissionTo", "admin-user-id", model.PermissionManageSystem).Return(true)

	ldapUser1 := &model.User{
		Id:          "u1",
		Username:    "ldap.one",
		Email:       "ldap.one@example.com",
		FirstName:   "LDAP",
		LastName:    "One",
		AuthService: "ldap",
		DeleteAt:    0,
	}
	ldapUserDeleted := &model.User{
		Id:          "u2",
		Username:    "ldap.deleted",
		Email:       "ldap.deleted@example.com",
		AuthService: "ldap",
		DeleteAt:    1234567, // deactivated
	}
	nativeUser := &model.User{
		Id:          "u3",
		Username:    "native.user",
		Email:       "native@example.com",
		AuthService: "",
		DeleteAt:    0,
	}

	api.On("GetUsers", mock.MatchedBy(func(opts *model.UserGetOptions) bool {
		return opts.Page == 0
	})).Return([]*model.User{ldapUser1, ldapUserDeleted, nativeUser}, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/ldap-users", nil)
	req.Header.Set("Mattermost-User-Id", "admin-user-id")
	w := httptest.NewRecorder()

	p.router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp struct {
		Count int                      `json:"count"`
		Users []map[string]interface{} `json:"users"`
	}
	err := json.NewDecoder(w.Body).Decode(&resp)
	assert.NoError(t, err)
	assert.Equal(t, 1, resp.Count)
	assert.Equal(t, "ldap.one", resp.Users[0]["username"])
}

func TestHandleUnlockLDAPUsers(t *testing.T) {
	p, api := setupTestPluginWithAPI()

	api.On("HasPermissionTo", "admin-user-id", model.PermissionManageSystem).Return(true)
	api.On("LogInfo", mock.Anything, mock.Anything, mock.Anything).Maybe()

	api.On("UpdateUserAuth", "target-ldap-uid", &model.UserAuth{
		AuthService: "",
		AuthData:    nil,
	}).Return(&model.UserAuth{}, nil)

	body := `{"user_ids": ["target-ldap-uid"]}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/unlock-ldap", strings.NewReader(body))
	req.Header.Set("Mattermost-User-Id", "admin-user-id")
	w := httptest.NewRecorder()

	p.router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp struct {
		Success  bool     `json:"success"`
		Unlocked int      `json:"unlocked"`
		Failed   []string `json:"failed"`
	}
	err := json.NewDecoder(w.Body).Decode(&resp)
	assert.NoError(t, err)
	assert.True(t, resp.Success)
	assert.Equal(t, 1, resp.Unlocked)
	assert.Empty(t, resp.Failed)
}

func TestCheckTLSCertificate(t *testing.T) {
	p := &Plugin{}

	t.Run("plain HTTP does not need certificate file", func(t *testing.T) {
		step := p.checkTLSCertificate("http://authentik.local:9000", "", false)
		assert.Equal(t, "TLS / SSL Certificate", step.Name)
		assert.Equal(t, "ok", step.Status)
		assert.Contains(t, step.Details, "No TLS certificate file is needed")
	})

	t.Run("HTTPS with self-signed certificate", func(t *testing.T) {
		// Launch a local TLS server with a self-signed certificate
		tlsServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		defer tlsServer.Close()

		certBytes := pem.EncodeToMemory(&pem.Block{
			Type:  "CERTIFICATE",
			Bytes: tlsServer.Certificate().Raw,
		})
		certPEM := string(certBytes)

		t.Run("without custom cert or skipTLS detects certificate file is needed", func(t *testing.T) {
			step := p.checkTLSCertificate(tlsServer.URL, "", false)
			assert.Equal(t, "error", step.Status)
			assert.Contains(t, step.Details, "A custom certificate file IS needed")
		})

		t.Run("with skipTLS reports certificate file needed for secure verification", func(t *testing.T) {
			step := p.checkTLSCertificate(tlsServer.URL, "", true)
			assert.Equal(t, "warning", step.Status)
			assert.Contains(t, step.Details, "A certificate file IS needed for secure verification")
		})

		t.Run("with valid custom cert PEM string succeeds", func(t *testing.T) {
			step := p.checkTLSCertificate(tlsServer.URL, certPEM, false)
			assert.Equal(t, "ok", step.Status)
			assert.Contains(t, step.Details, "successfully verified the connection")
		})

		t.Run("with valid custom cert file path succeeds", func(t *testing.T) {
			tmpDir := t.TempDir()
			certPath := filepath.Join(tmpDir, "server-ca.crt")
			err := os.WriteFile(certPath, certBytes, 0600)
			require.NoError(t, err)

			step := p.checkTLSCertificate(tlsServer.URL, certPath, false)
			assert.Equal(t, "ok", step.Status)
			assert.Contains(t, step.Details, "successfully verified the connection")
		})

		t.Run("with wrong custom cert fails", func(t *testing.T) {
			wrongPEM := generateTestCertPEM(t)
			step := p.checkTLSCertificate(tlsServer.URL, wrongPEM, false)
			assert.Equal(t, "error", step.Status)
			assert.Contains(t, step.Details, "verification still failed")
		})
	})
}

func TestHandleDiagnoseConnection(t *testing.T) {
	t.Run("empty configuration returns error step", func(t *testing.T) {
		p, api := setupTestPluginWithAPI()
		api.On("HasPermissionTo", "admin-user-id", model.PermissionManageSystem).Return(true)
		p.setConfiguration(&Configuration{})

		req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/diagnose", strings.NewReader("{}"))
		req.Header.Set("Mattermost-User-Id", "admin-user-id")
		w := httptest.NewRecorder()

		p.router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		var resp DiagnosticResponse
		err := json.NewDecoder(w.Body).Decode(&resp)
		require.NoError(t, err)
		assert.False(t, resp.Success)
		require.NotEmpty(t, resp.Steps)
		assert.Equal(t, "Configuration Check", resp.Steps[0].Name)
		assert.Equal(t, "error", resp.Steps[0].Status)
	})

	t.Run("invalid issuer URL returns error step", func(t *testing.T) {
		p, api := setupTestPluginWithAPI()
		api.On("HasPermissionTo", "admin-user-id", model.PermissionManageSystem).Return(true)
		p.setConfiguration(&Configuration{
			IssuerURL: "ftp://invalid-scheme.example.com",
		})

		req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/diagnose", strings.NewReader("{}"))
		req.Header.Set("Mattermost-User-Id", "admin-user-id")
		w := httptest.NewRecorder()

		p.router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		var resp DiagnosticResponse
		err := json.NewDecoder(w.Body).Decode(&resp)
		require.NoError(t, err)
		assert.False(t, resp.Success)
		assert.Equal(t, "Configuration Check", resp.Steps[0].Name)
		assert.Equal(t, "error", resp.Steps[0].Status)
	})

	t.Run("rejects mismatched JWKS host (SSRF prevention)", func(t *testing.T) {
		mockOIDC := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/.well-known/openid-configuration" {
				_ = json.NewEncoder(w).Encode(map[string]interface{}{
					"issuer":         "http://example.com",
					"jwks_uri":       "http://169.254.169.254/latest/meta-data/",
					"token_endpoint": "http://169.254.169.254/token",
				})
				return
			}
			w.WriteHeader(http.StatusNotFound)
		}))
		defer mockOIDC.Close()

		p, api := setupTestPluginWithAPI()
		api.On("HasPermissionTo", "admin-user-id", model.PermissionManageSystem).Return(true)
		p.setConfiguration(&Configuration{
			IssuerURL:    mockOIDC.URL,
			ClientID:     "client-id",
			ClientSecret: "client-secret",
		})

		req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/diagnose", strings.NewReader("{}"))
		req.Header.Set("Mattermost-User-Id", "admin-user-id")
		w := httptest.NewRecorder()

		p.router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		var resp DiagnosticResponse
		err := json.NewDecoder(w.Body).Decode(&resp)
		require.NoError(t, err)
		assert.False(t, resp.Success)

		var jwksStep *DiagnosticStep
		for i := range resp.Steps {
			if resp.Steps[i].Name == "JWKS Cryptographic Keys" {
				jwksStep = &resp.Steps[i]
				break
			}
		}
		require.NotNil(t, jwksStep)
		assert.Equal(t, "error", jwksStep.Status)
		assert.Contains(t, jwksStep.Details, "does not match issuer host")
	})

	t.Run("successful full diagnostic", func(t *testing.T) {
		var mockOIDC *httptest.Server
		mockOIDC = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/application/o/mattermost/.well-known/openid-configuration":
				_ = json.NewEncoder(w).Encode(map[string]interface{}{
					"issuer":           mockOIDC.URL + "/application/o/mattermost",
					"jwks_uri":         mockOIDC.URL + "/application/o/mattermost/jwks/",
					"token_endpoint":   mockOIDC.URL + "/application/o/mattermost/token/",
					"scopes_supported": []string{"openid", "profile", "email"},
				})
			case "/application/o/mattermost/jwks/":
				_ = json.NewEncoder(w).Encode(map[string]interface{}{
					"keys": []map[string]interface{}{
						{"kty": "RSA", "kid": "key-1"},
					},
				})
			case "/application/o/mattermost/token/":
				w.WriteHeader(http.StatusOK)
				_ = json.NewEncoder(w).Encode(map[string]interface{}{
					"access_token": "mock-token",
					"token_type":   "Bearer",
				})
			default:
				w.WriteHeader(http.StatusNotFound)
			}
		}))
		defer mockOIDC.Close()

		p, api := setupTestPluginWithAPI()
		api.On("HasPermissionTo", "admin-user-id", model.PermissionManageSystem).Return(true)
		p.setConfiguration(&Configuration{
			AuthentikURL:    mockOIDC.URL,
			ApplicationSlug: "mattermost",
			ClientID:        "client-id",
			ClientSecret:    "client-secret",
		})

		req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/diagnose", strings.NewReader("{}"))
		req.Header.Set("Mattermost-User-Id", "admin-user-id")
		w := httptest.NewRecorder()

		p.router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		var resp DiagnosticResponse
		err := json.NewDecoder(w.Body).Decode(&resp)
		require.NoError(t, err)
		assert.True(t, resp.Success)
		assert.Len(t, resp.Steps, 4) // TLS, Discovery, JWKS, Token
		for _, s := range resp.Steps {
			assert.Equal(t, "ok", s.Status)
		}
	})
}
