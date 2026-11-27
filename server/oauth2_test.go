package main

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/mattermost/mattermost/server/public/model"
	"github.com/mattermost/mattermost/server/public/plugin/plugintest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestSignAndVerifyState(t *testing.T) {
	p := &Plugin{
		encryptionKey: "test-secret-encryption-key-for-hmac",
	}

	t.Run("valid state roundtrip", func(t *testing.T) {
		token := "random-state-token-12345"
		signed, err := p.signState(token)
		require.NoError(t, err)

		extracted, err := p.verifyAndExtractState(signed)
		assert.NoError(t, err)
		assert.Equal(t, token, extracted)
	})

	t.Run("tampered state token is rejected", func(t *testing.T) {
		token := "random-state-token-12345"
		signed, err := p.signState(token)
		require.NoError(t, err)

		tampered := "tampered-token" + signed[len(token):]
		_, err = p.verifyAndExtractState(tampered)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "state signature mismatch")
	})

	t.Run("malformed state without separator is rejected", func(t *testing.T) {
		_, err := p.verifyAndExtractState("invalidtokenwithoutcolon")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "malformed state parameter")
	})

	t.Run("wrong key fails signature verification", func(t *testing.T) {
		token := "random-state-token-12345"
		signed, err := p.signState(token)
		require.NoError(t, err)

		pOther := &Plugin{
			encryptionKey: "different-encryption-key",
		}
		_, err = pOther.verifyAndExtractState(signed)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "state signature mismatch")
	})
}

func TestIsAllowedMobileScheme(t *testing.T) {
	testCases := []struct {
		url      string
		expected bool
	}{
		{"mmauth://callback", true},
		{"mmauthbeta://callback", true},
		{"mmauth://callback?token=123", true},
		{"mmauthbeta://callback?token=123&csrf=abc", true},
		{"mmauth://callback.evil.com", false},
		{"mmauth://other", false},
		{"https://evil.com/callback", false},
		{"javascript:alert(1)", false},
		{"", false},
	}

	for _, tc := range testCases {
		t.Run(tc.url, func(t *testing.T) {
			actual := isAllowedMobileScheme(tc.url)
			assert.Equal(t, tc.expected, actual)
		})
	}
}

func TestGetCookiePath(t *testing.T) {
	assert.Equal(t, "/", getCookiePath(""))
	assert.Equal(t, "/", getCookiePath("https://chat.example.com"))
	assert.Equal(t, "/", getCookiePath("https://chat.example.com/"))
	assert.Equal(t, "/mattermost", getCookiePath("https://chat.example.com/mattermost"))
	assert.Equal(t, "/mattermost", getCookiePath("https://chat.example.com/mattermost/"))
}

func TestUserWillLogIn(t *testing.T) {
	t.Run("allows login when SSO enforcement is disabled", func(t *testing.T) {
		p := &Plugin{
			configuration: &Configuration{
				Enable:     true,
				EnforceSSO: false,
			},
		}
		msg := p.UserWillLogIn(nil, &model.User{AuthService: ""})
		assert.Empty(t, msg)
	})

	t.Run("allows Authentik user when SSO enforcement is enabled", func(t *testing.T) {
		p := &Plugin{
			configuration: &Configuration{
				Enable:     true,
				EnforceSSO: true,
			},
		}
		msg := p.UserWillLogIn(nil, &model.User{AuthService: AuthService})
		assert.Empty(t, msg)
	})

	t.Run("rejects non-Authentik user when SSO enforcement is enabled", func(t *testing.T) {
		p := &Plugin{
			configuration: &Configuration{
				Enable:     true,
				EnforceSSO: true,
			},
		}
		msg := p.UserWillLogIn(nil, &model.User{AuthService: ""})
		assert.NotEmpty(t, msg)
		assert.Contains(t, msg, "Password login is disabled")
	})
}

func signTestIDToken(t *testing.T, priv *rsa.PrivateKey, claims map[string]interface{}) string {
	t.Helper()
	headerJSON, err := json.Marshal(map[string]string{
		"alg": "RS256",
		"typ": "JWT",
		"kid": "test-kid-1",
	})
	require.NoError(t, err)
	claimsJSON, err := json.Marshal(claims)
	require.NoError(t, err)

	signingInput := base64.RawURLEncoding.EncodeToString(headerJSON) + "." +
		base64.RawURLEncoding.EncodeToString(claimsJSON)

	sum := sha256.Sum256([]byte(signingInput))
	sig, err := rsa.SignPKCS1v15(rand.Reader, priv, crypto.SHA256, sum[:])
	require.NoError(t, err)

	return signingInput + "." + base64.RawURLEncoding.EncodeToString(sig)
}

func setupMockOIDCServer(t *testing.T, priv *rsa.PrivateKey, claimsFn func(nonce string) map[string]interface{}) (*httptest.Server, *string) {
	t.Helper()
	var currentNonce string

	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		issuer := srv.URL + "/application/o/mattermost"
		switch r.URL.Path {
		case "/application/o/mattermost/.well-known/openid-configuration":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"issuer":                 issuer,
				"authorization_endpoint": issuer + "/authorize/",
				"token_endpoint":         issuer + "/token/",
				"userinfo_endpoint":      issuer + "/userinfo/",
				"jwks_uri":               issuer + "/jwks/",
			})
		case "/application/o/mattermost/jwks/":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"keys": []map[string]interface{}{
					{
						"kty": "RSA",
						"kid": "test-kid-1",
						"use": "sig",
						"alg": "RS256",
						"n":   base64.RawURLEncoding.EncodeToString(priv.N.Bytes()),
						"e":   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(priv.E)).Bytes()),
					},
				},
			})
		case "/application/o/mattermost/token/":
			idToken := signTestIDToken(t, priv, claimsFn(currentNonce))
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"access_token": "mock-access-token",
				"token_type":   "Bearer",
				"expires_in":   3600,
				"id_token":     idToken,
			})
		case "/application/o/mattermost/userinfo/":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"sub": "sub-123",
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	return srv, &currentNonce
}

func TestHandleGetPublicConfig(t *testing.T) {
	p, _ := setupTestPluginWithAPI()
	p.setConfiguration(&Configuration{
		Enable:       true,
		EnforceSSO:   true,
		AuthentikURL: "https://auth.example.com",
		ClientID:     "cid",
		ClientSecret: "csecret",
		ButtonText:   "Sign in",
		ButtonColor:  "#123456",
	})

	req := httptest.NewRequest(http.MethodGet, "/config", nil)
	w := httptest.NewRecorder()
	p.router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var body map[string]interface{}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&body))
	assert.Equal(t, true, body["enable"])
	assert.Equal(t, true, body["enforce_sso"])
	assert.Equal(t, "Sign in", body["button_text"])
	assert.Equal(t, "#123456", body["button_color"])
}

func TestHandleOAuth2Connect(t *testing.T) {
	t.Run("returns 400 when disabled", func(t *testing.T) {
		p, _ := setupTestPluginWithAPI()
		p.setConfiguration(&Configuration{Enable: false})

		req := httptest.NewRequest(http.MethodGet, "/oauth2/connect", nil)
		w := httptest.NewRecorder()
		p.router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("enforces rate limit on connect", func(t *testing.T) {
		p, api := setupTestPluginWithAPI()
		api.On("LogWarn", mock.Anything, mock.Anything, mock.Anything).Maybe()
		p.setConfiguration(&Configuration{Enable: true})
		p.connectLimiter = newIPRateLimiter(1, time.Minute)

		// First request passes rate limit (fails later on nil oauthConfig -> 500)
		api.On("LogError", mock.Anything).Maybe()
		req1 := httptest.NewRequest(http.MethodGet, "/oauth2/connect", nil)
		req1.RemoteAddr = "198.51.100.5:1234"
		w1 := httptest.NewRecorder()
		p.router.ServeHTTP(w1, req1)
		assert.NotEqual(t, http.StatusTooManyRequests, w1.Code)

		// Second request from same IP is rate limited
		req2 := httptest.NewRequest(http.MethodGet, "/oauth2/connect", nil)
		req2.RemoteAddr = "198.51.100.5:5678"
		w2 := httptest.NewRecorder()
		p.router.ServeHTTP(w2, req2)
		assert.Equal(t, http.StatusTooManyRequests, w2.Code)
		assert.Equal(t, "60", w2.Header().Get("Retry-After"))
	})

	t.Run("redirects with PKCE, nonce, state cookie, and sanitized return_to", func(t *testing.T) {
		priv, err := rsa.GenerateKey(rand.Reader, 2048)
		require.NoError(t, err)
		srv, _ := setupMockOIDCServer(t, priv, func(nonce string) map[string]interface{} { return nil })
		defer srv.Close()

		p, api := setupTestPluginWithAPI()
		p.encryptionKey = "test-hmac-key"
		cfg := &Configuration{
			Enable:          true,
			AuthentikURL:    srv.URL,
			ApplicationSlug: "mattermost",
			ClientID:        "cid",
			ClientSecret:    "csec",
			Scope:           "openid profile email groups",
		}
		p.setConfiguration(cfg)
		require.NoError(t, p.initOIDCProvider(cfg))

		var savedStateBytes []byte
		api.On("KVSetWithExpiry", mock.MatchedBy(func(k string) bool {
			return strings.HasPrefix(k, KVOAuthStatePrefix)
		}), mock.Anything, int64(stateExpiry.Seconds())).Run(func(args mock.Arguments) {
			savedStateBytes = args.Get(1).([]byte)
		}).Return(nil)

		req := httptest.NewRequest(http.MethodGet, "/oauth2/connect?return_to=//evil.com&popup=1", nil)
		req.RemoteAddr = "198.51.100.9:1111"
		w := httptest.NewRecorder()
		p.router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusFound, w.Code)
		loc, err := url.Parse(w.Header().Get("Location"))
		require.NoError(t, err)
		assert.Equal(t, "S256", loc.Query().Get("code_challenge_method"))
		assert.NotEmpty(t, loc.Query().Get("code_challenge"))
		assert.NotEmpty(t, loc.Query().Get("nonce"))
		assert.NotEmpty(t, loc.Query().Get("state"))

		var stored OAuthState
		require.NoError(t, json.Unmarshal(savedStateBytes, &stored))
		assert.Equal(t, "/", stored.ReturnTo)
		assert.True(t, stored.Popup)

		cookies := w.Result().Cookies()
		require.NotEmpty(t, cookies)
		assert.Equal(t, stateCookieName, cookies[0].Name)
		assert.Equal(t, stored.Token, cookies[0].Value)
	})
}

func TestHandleOAuth2Callback(t *testing.T) {
	t.Run("rejects callback errors and invalid state", func(t *testing.T) {
		p, api := setupTestPluginWithAPI()
		api.On("LogError", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Maybe()
		api.On("LogError", mock.Anything, mock.Anything, mock.Anything).Maybe()
		p.setConfiguration(&Configuration{Enable: true})

		req := httptest.NewRequest(http.MethodGet, "/oauth2/callback?error=access_denied&error_description=nope", nil)
		w := httptest.NewRecorder()
		p.router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusUnauthorized, w.Code)
		assert.Contains(t, w.Body.String(), "access_denied")

		req2 := httptest.NewRequest(http.MethodGet, "/oauth2/callback?state=badstate", nil)
		w2 := httptest.NewRecorder()
		p.router.ServeHTTP(w2, req2)
		assert.Equal(t, http.StatusUnauthorized, w2.Code)
		assert.Contains(t, w2.Body.String(), "Invalid authentication state")
	})

	t.Run("full callback creates user, sets session cookies, and handles popup", func(t *testing.T) {
		priv, err := rsa.GenerateKey(rand.Reader, 2048)
		require.NoError(t, err)

		var srv *httptest.Server
		var noncePtr *string
		srv, noncePtr = setupMockOIDCServer(t, priv, func(nonce string) map[string]interface{} {
			return map[string]interface{}{
				"iss":                srv.URL + "/application/o/mattermost",
				"sub":                "sub-123",
				"aud":                "cid",
				"exp":                time.Now().Add(time.Hour).Unix(),
				"iat":                time.Now().Unix(),
				"nonce":              nonce,
				"email":              "alice@example.com",
				"email_verified":     true,
				"preferred_username": "alice",
				"given_name":         "Alice",
				"family_name":        "Smith",
				"groups":             []string{"users"},
			}
		})
		defer srv.Close()

		p := &Plugin{encryptionKey: "test-hmac-key"}
		api := &plugintest.API{}
		siteURL := "https://mattermost.example.com"
		api.On("GetConfig").Return(&model.Config{
			ServiceSettings: model.ServiceSettings{SiteURL: &siteURL},
		}).Maybe()
		api.On("LogInfo", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Maybe()
		p.SetAPI(api)
		p.setupRouter()

		cfg := &Configuration{
			Enable:               true,
			AuthentikURL:         srv.URL,
			ApplicationSlug:      "mattermost",
			ClientID:             "cid",
			ClientSecret:         "csec",
			Scope:                "openid profile email groups",
			AutoCreateAccounts:   true,
			RequireEmailVerified: true,
		}
		p.setConfiguration(cfg)
		require.NoError(t, p.initOIDCProvider(cfg))

		stateToken := "state-abc"
		*noncePtr = "nonce-xyz"
		stateBytes, _ := json.Marshal(OAuthState{
			Token:    stateToken,
			CreateAt: model.GetMillis(),
			ReturnTo: "/channels/town-square",
			Nonce:    "nonce-xyz",
			Popup:    true,
		})

		api.On("KVGet", KVOAuthStatePrefix+stateToken).Return(stateBytes, nil)
		api.On("KVCompareAndDelete", KVOAuthStatePrefix+stateToken, stateBytes).Return(true, nil)
		api.On("KVGet", KVUserPrefix+"sub-123").Return(nil, nil)
		api.On("GetUserByEmail", "alice@example.com").Return(nil, model.NewAppError("GetUserByEmail", "not_found", nil, "", http.StatusNotFound))
		api.On("CreateUser", mock.MatchedBy(func(u *model.User) bool {
			return u.Email == "alice@example.com" && u.Username == "alice" && u.EmailVerified
		})).Return(&model.User{
			Id:            "mm-user-1",
			Username:      "alice",
			Email:         "alice@example.com",
			AuthService:   AuthService,
			EmailVerified: true,
			Roles:         model.SystemUserRoleId,
		}, nil)
		api.On("KVSet", KVUserPrefix+"sub-123", []byte("mm-user-1")).Return(nil)
		api.On("KVGet", KVUserSessionsPrefix+"mm-user-1").Return(nil, nil)
		api.On("KVSetWithExpiry", KVUserSessionsPrefix+"mm-user-1", mock.Anything, mock.AnythingOfType("int64")).Return(nil)
		api.On("CreateSession", mock.AnythingOfType("*model.Session")).Return(&model.Session{
			Id:        "sess-1",
			Token:     "tok-secret",
			UserId:    "mm-user-1",
			ExpiresAt: model.GetMillis() + 3600*1000,
			Props:     model.StringMap{"csrf": "csrf-1"},
		}, nil)

		signedState, signErr := p.signState(stateToken)
		require.NoError(t, signErr)
		req := httptest.NewRequest(http.MethodGet, "/oauth2/callback?code=auth-code&state="+url.QueryEscape(signedState), nil)
		req.AddCookie(&http.Cookie{Name: stateCookieName, Value: stateToken})
		w := httptest.NewRecorder()
		p.router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), "Authenticated with Authentik")

		var hasAuthToken, hasUserCookie, hasCSRFCookie bool
		for _, c := range w.Result().Cookies() {
			switch c.Name {
			case model.SessionCookieToken:
				hasAuthToken = c.Value == "tok-secret" && c.HttpOnly && c.Secure
			case model.SessionCookieUser:
				hasUserCookie = c.Value == "mm-user-1" && !c.HttpOnly && c.Secure
			case model.SessionCookieCsrf:
				hasCSRFCookie = c.Value == "csrf-1" && !c.HttpOnly && c.Secure
			}
		}
		assert.True(t, hasAuthToken)
		assert.True(t, hasUserCookie)
		assert.True(t, hasCSRFCookie)
	})
}

func TestGetOrCreateUser_EmailVerifiedDefault(t *testing.T) {
	p, api := setupTestPluginWithAPI()
	api.On("LogInfo", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Maybe()
	api.On("KVGet", KVUserPrefix+"sub-unverified").Return(nil, nil)
	api.On("GetUserByEmail", "bob@example.com").Return(nil, model.NewAppError("GetUserByEmail", "not_found", nil, "", http.StatusNotFound))
	api.On("CreateUser", mock.MatchedBy(func(u *model.User) bool {
		return u.Email == "bob@example.com" && !u.EmailVerified
	})).Return(&model.User{Id: "mm-bob", Email: "bob@example.com", EmailVerified: false}, nil)
	api.On("KVSet", KVUserPrefix+"sub-unverified", []byte("mm-bob")).Return(nil)

	user, err := p.getOrCreateUser(&AuthentikUserInfo{
		Subject:       "sub-unverified",
		Email:         "bob@example.com",
		Username:      "bob",
		EmailVerified: nil,
	}, &Configuration{
		AutoCreateAccounts:   true,
		RequireEmailVerified: false,
	})
	require.NoError(t, err)
	assert.False(t, user.EmailVerified)
}
