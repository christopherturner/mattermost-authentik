package main

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/mattermost/mattermost/server/public/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestHandleOAuth2Logout(t *testing.T) {
	t.Run("redirects to end_session_endpoint and revokes tracked sessions when EnableSLO is true", func(t *testing.T) {
		p, api := setupTestPluginWithAPI()
		p.setConfiguration(&Configuration{
			Enable:    true,
			EnableSLO: true,
			ClientID:  "cid",
		})
		p.endSessionEndpoint = "https://auth.example.com/application/o/mattermost/end-session/"

		sessionsData, _ := json.Marshal([]trackedSession{
			{SessionID: "sess-1", SID: "sid-1", ExpiresAt: model.GetMillis() + 60000},
		})
		api.On("KVGet", KVUserSessionsPrefix+"user-1").Return(sessionsData, nil)
		api.On("RevokeSession", "sess-1").Return(nil)
		api.On("KVDelete", KVSIDPrefix+"sid-1").Return(nil)
		api.On("KVDelete", KVUserSessionsPrefix+"user-1").Return(nil)

		req := httptest.NewRequest(http.MethodGet, "/oauth2/logout", nil)
		req.Header.Set("Mattermost-User-Id", "user-1")
		w := httptest.NewRecorder()
		p.router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusFound, w.Code)
		loc, err := url.Parse(w.Header().Get("Location"))
		require.NoError(t, err)
		assert.Equal(t, "auth.example.com", loc.Host)
		assert.Equal(t, "cid", loc.Query().Get("client_id"))
		assert.Equal(t, "http://mattermost.example.com/login?no_sso=1", loc.Query().Get("post_logout_redirect_uri"))
		api.AssertCalled(t, "RevokeSession", "sess-1")
	})

	t.Run("falls back to login?no_sso=1 when EnableSLO is false", func(t *testing.T) {
		p, _ := setupTestPluginWithAPI()
		p.setConfiguration(&Configuration{
			Enable:    true,
			EnableSLO: false,
		})
		p.endSessionEndpoint = "https://auth.example.com/end-session/"

		req := httptest.NewRequest(http.MethodGet, "/oauth2/logout", nil)
		w := httptest.NewRecorder()
		p.router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusFound, w.Code)
		assert.Equal(t, "http://mattermost.example.com/login?no_sso=1", w.Header().Get("Location"))
	})
}

func TestHandleBackChannelLogout(t *testing.T) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	srv, _ := setupMockOIDCServer(t, priv, func(nonce string) map[string]interface{} { return nil })
	defer srv.Close()

	newConfiguredPlugin := func() (*Plugin, *mock.Mock) {
		p, api := setupTestPluginWithAPI()
		api.On("LogInfo", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Maybe()
		api.On("LogWarn", mock.Anything, mock.Anything, mock.Anything).Maybe()
		cfg := &Configuration{
			Enable:          true,
			EnableSLO:       true,
			AuthentikURL:    srv.URL,
			ApplicationSlug: "mattermost",
			ClientID:        "cid",
			ClientSecret:    "csec",
			Scope:           "openid profile email groups",
		}
		p.setConfiguration(cfg)
		require.NoError(t, p.initOIDCProvider(cfg))
		return p, &api.Mock
	}

	t.Run("rejects logout_token containing nonce", func(t *testing.T) {
		p, _ := newConfiguredPlugin()
		tok := signTestIDToken(t, priv, map[string]interface{}{
			"iss":    srv.URL + "/application/o/mattermost",
			"sub":    "sub-123",
			"aud":    "cid",
			"exp":    time.Now().Add(time.Hour).Unix(),
			"iat":    time.Now().Unix(),
			"nonce":  "forbidden-nonce",
			"events": map[string]interface{}{backChannelLogoutEvent: map[string]interface{}{}},
		})

		form := url.Values{"logout_token": {tok}}.Encode()
		req := httptest.NewRequest(http.MethodPost, "/oauth2/backchannel-logout", strings.NewReader(form))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		p.router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "nonce")
	})

	t.Run("rejects logout_token missing backchannel-logout event", func(t *testing.T) {
		p, _ := newConfiguredPlugin()
		tok := signTestIDToken(t, priv, map[string]interface{}{
			"iss": srv.URL + "/application/o/mattermost",
			"sub": "sub-123",
			"aud": "cid",
			"exp": time.Now().Add(time.Hour).Unix(),
			"iat": time.Now().Unix(),
		})

		form := url.Values{"logout_token": {tok}}.Encode()
		req := httptest.NewRequest(http.MethodPost, "/oauth2/backchannel-logout", strings.NewReader(form))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		p.router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "event")
	})

	t.Run("revokes session by sid on valid logout_token", func(t *testing.T) {
		p, apiMock := newConfiguredPlugin()
		apiMock.On("KVGet", KVSIDPrefix+"sid-abc").Return([]byte("mm-sess-99"), nil)
		apiMock.On("KVDelete", KVSIDPrefix+"sid-abc").Return(nil)
		apiMock.On("RevokeSession", "mm-sess-99").Return(nil)

		tok := signTestIDToken(t, priv, map[string]interface{}{
			"iss":    srv.URL + "/application/o/mattermost",
			"sub":    "sub-123",
			"sid":    "sid-abc",
			"aud":    "cid",
			"exp":    time.Now().Add(time.Hour).Unix(),
			"iat":    time.Now().Unix(),
			"events": map[string]interface{}{backChannelLogoutEvent: map[string]interface{}{}},
		})

		form := url.Values{"logout_token": {tok}}.Encode()
		req := httptest.NewRequest(http.MethodPost, "/oauth2/backchannel-logout", strings.NewReader(form))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		p.router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "no-store", w.Header().Get("Cache-Control"))
		apiMock.AssertCalled(t, "RevokeSession", "mm-sess-99")
	})

	t.Run("revokes all tracked user sessions by sub when sid is absent", func(t *testing.T) {
		p, apiMock := newConfiguredPlugin()
		sessionsData, _ := json.Marshal([]trackedSession{
			{SessionID: "mm-sess-1", ExpiresAt: model.GetMillis() + 60000},
			{SessionID: "mm-sess-2", ExpiresAt: model.GetMillis() + 60000},
		})
		apiMock.On("KVGet", KVUserPrefix+"sub-123").Return([]byte("mm-user-123"), nil)
		apiMock.On("KVGet", KVUserSessionsPrefix+"mm-user-123").Return(sessionsData, nil)
		apiMock.On("RevokeSession", "mm-sess-1").Return(nil)
		apiMock.On("RevokeSession", "mm-sess-2").Return(nil)
		apiMock.On("KVDelete", KVUserSessionsPrefix+"mm-user-123").Return(nil)

		tok := signTestIDToken(t, priv, map[string]interface{}{
			"iss":    srv.URL + "/application/o/mattermost",
			"sub":    "sub-123",
			"aud":    "cid",
			"exp":    time.Now().Add(time.Hour).Unix(),
			"iat":    time.Now().Unix(),
			"events": map[string]interface{}{backChannelLogoutEvent: map[string]interface{}{}},
		})

		form := url.Values{"logout_token": {tok}}.Encode()
		req := httptest.NewRequest(http.MethodPost, "/oauth2/backchannel-logout", strings.NewReader(form))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		p.router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		apiMock.AssertCalled(t, "RevokeSession", "mm-sess-1")
		apiMock.AssertCalled(t, "RevokeSession", "mm-sess-2")
	})
}
