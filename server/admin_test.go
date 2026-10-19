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
	p.SetAPI(api)
	p.setupRouter()
	return p, api
}

func TestAdminEndpoints_AuthorizationCheck(t *testing.T) {
	p, api := setupTestPluginWithAPI()

	// Non-admin user
	api.On("GetUser", "non-admin-id").Return(&model.User{
		Id:    "non-admin-id",
		Roles: model.SystemUserRoleId,
	}, nil)

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
			assert.Equal(t, http.StatusForbidden, w.Code)
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

	adminUser := &model.User{
		Id:    "admin-user-id",
		Roles: model.SystemAdminRoleId + " " + model.SystemUserRoleId,
	}
	api.On("GetUser", "admin-user-id").Return(adminUser, nil)

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

	adminUser := &model.User{
		Id:    "admin-user-id",
		Roles: model.SystemAdminRoleId,
	}
	api.On("GetUser", "admin-user-id").Return(adminUser, nil)
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
