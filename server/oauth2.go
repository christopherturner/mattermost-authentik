package main

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"net/mail"
	"net/url"
	"strconv"
	"strings"
	"time"

	oidc "github.com/coreos/go-oidc/v3/oidc"
	"github.com/mattermost/mattermost/server/public/model"
	"golang.org/x/oauth2"
)

const (
	stateExpiry        = 10 * time.Minute
	stateCookieName    = "MMAUTHENTIKSTATE"
	KVUserPrefix       = "authentik_user_"
)

// OAuthState holds the metadata stored in KV during the OAuth authorization code flow.
type OAuthState struct {
	Token          string `json:"token"`
	CreateAt       int64  `json:"create_at"`
	ReturnTo       string `json:"return_to"`
	MobileRedirect string `json:"mobile_redirect,omitempty"`
	Popup          bool   `json:"popup,omitempty"`
	CodeVerifier   string `json:"code_verifier,omitempty"`
	Nonce          string `json:"nonce,omitempty"`
}

var allowedMobileSchemes = []string{"mmauth://callback", "mmauthbeta://callback"}

func isAllowedMobileScheme(redirect string) bool {
	for _, s := range allowedMobileSchemes {
		if redirect == s || strings.HasPrefix(redirect, s+"?") {
			return true
		}
	}
	return false
}

// handleGetPublicConfig returns publicly accessible configuration for webapp button rendering.
func (p *Plugin) handleGetPublicConfig(w http.ResponseWriter, r *http.Request) {
	config := p.getConfiguration()

	publicConfig := map[string]interface{}{
		"enable":       config.Enable && config.IsValid() == nil,
		"enforce_sso":  config.EnforceSSO,
		"button_text":  config.ButtonText,
		"button_color": config.ButtonColor,
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(publicConfig)
}

// handleOAuth2Connect initiates the Authentik OIDC login flow.
func (p *Plugin) handleOAuth2Connect(w http.ResponseWriter, r *http.Request) {
	config := p.getConfiguration()
	if !config.Enable {
		http.Error(w, "Authentik authentication is disabled", http.StatusBadRequest)
		return
	}

	if p.getSiteURL() == "" {
		p.API.LogError("SiteURL is not configured in Mattermost. Please configure ServiceSettings.SiteURL.")
		http.Error(w, "Mattermost SiteURL is not configured. Please contact your administrator.", http.StatusInternalServerError)
		return
	}

	p.retryOIDCProvider()
	oauthConfig := p.getOAuthConfig()
	if oauthConfig == nil {
		p.API.LogError("Authentik provider not initialized. Check plugin configuration.")
		http.Error(w, "Authentik provider not initialized. Check server logs.", http.StatusInternalServerError)
		return
	}

	stateToken, err := generateRandomKey(16)
	if err != nil {
		p.API.LogError("Failed to generate state token", "error", err.Error())
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	signedState := p.signState(stateToken)

	returnTo := r.URL.Query().Get("return_to")
	if returnTo == "" || !strings.HasPrefix(returnTo, "/") || strings.HasPrefix(returnTo, "//") || strings.ContainsRune(returnTo, '\\') {
		returnTo = "/"
	}

	nonce, err := generateRandomKey(16)
	if err != nil {
		p.API.LogError("Failed to generate nonce", "error", err.Error())
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	codeVerifier := oauth2.GenerateVerifier()

	state := OAuthState{
		Token:        stateToken,
		CreateAt:     model.GetMillis(),
		ReturnTo:     returnTo,
		CodeVerifier: codeVerifier,
		Nonce:        nonce,
		Popup:        r.URL.Query().Get("popup") == "1",
	}

	mobileRedirect := r.URL.Query().Get("mobile_redirect")
	if mobileRedirect != "" && isAllowedMobileScheme(mobileRedirect) {
		state.MobileRedirect = mobileRedirect
	}

	stateBytes, err := json.Marshal(state)
	if err != nil {
		p.API.LogError("Failed to serialize OAuth state", "error", err.Error())
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	kvKey := KVOAuthStatePrefix + stateToken
	if appErr := p.API.KVSetWithExpiry(kvKey, stateBytes, int64(stateExpiry.Seconds())); appErr != nil {
		p.API.LogError("Failed to store OAuth state in KV", "error", appErr.Error())
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}

	p.setStateCookie(w, stateToken)

	authOpts := []oauth2.AuthCodeOption{
		oauth2.AccessTypeOnline,
		oauth2.SetAuthURLParam("nonce", nonce),
		oauth2.S256ChallengeOption(codeVerifier),
	}

	authURL := oauthConfig.AuthCodeURL(signedState, authOpts...)
	http.Redirect(w, r, authURL, http.StatusFound)
}

// handleOAuth2Callback handles the redirect callback from Authentik.
func (p *Plugin) handleOAuth2Callback(w http.ResponseWriter, r *http.Request) {
	config := p.getConfiguration()
	if !config.Enable {
		http.Error(w, "Authentik authentication is disabled", http.StatusBadRequest)
		return
	}

	if errParam := r.URL.Query().Get("error"); errParam != "" {
		errDesc := r.URL.Query().Get("error_description")
		p.API.LogError("Authentik returned error", "error", errParam, "description", errDesc)
		p.renderError(w, "Authentik authentication failed: "+errParam)
		return
	}

	signedState := r.URL.Query().Get("state")
	stateToken, err := p.verifyAndExtractState(signedState)
	if err != nil {
		p.API.LogError("Invalid OAuth state parameter", "error", err.Error())
		p.renderError(w, "Invalid authentication state. Please try again.")
		return
	}

	kvKey := KVOAuthStatePrefix + stateToken
	stateBytes, appErr := p.API.KVGet(kvKey)
	if appErr != nil || stateBytes == nil {
		p.API.LogError("OAuth state expired or not found")
		p.renderError(w, "Authentication session expired. Please try again.")
		return
	}

	deleted, delErr := p.API.KVCompareAndDelete(kvKey, stateBytes)
	if delErr != nil || !deleted {
		p.API.LogWarn("OAuth state already consumed or replay attempt", "key", kvKey)
		p.renderError(w, "Authentication session expired. Please try again.")
		return
	}

	var state OAuthState
	if err := json.Unmarshal(stateBytes, &state); err != nil {
		p.API.LogError("Failed to deserialize OAuth state", "error", err.Error())
		p.renderError(w, "Internal error")
		return
	}

	if time.Since(time.UnixMilli(state.CreateAt)) > stateExpiry {
		p.renderError(w, "Authentication session expired. Please try again.")
		return
	}

	if state.MobileRedirect == "" {
		if !p.stateCookieMatches(r, stateToken) {
			p.API.LogWarn("OAuth state cookie mismatch (login CSRF)")
			p.clearStateCookie(w)
			p.renderError(w, "Invalid authentication session. Please try again.")
			return
		}
		p.clearStateCookie(w)
	}

	code := r.URL.Query().Get("code")
	if code == "" {
		p.renderError(w, "Missing authorization code from Authentik")
		return
	}

	oauthConfig := p.getOAuthConfig()
	verifier := p.getOIDCVerifier()
	if oauthConfig == nil || verifier == nil {
		p.API.LogError("Authentik OIDC provider or verifier not initialized")
		p.renderError(w, "Authentik provider not initialized. Check plugin configuration.")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	httpClient := p.getHTTPClient(config)
	ctx = oidc.ClientContext(ctx, httpClient)

	var exchangeOpts []oauth2.AuthCodeOption
	if state.CodeVerifier != "" {
		exchangeOpts = append(exchangeOpts, oauth2.VerifierOption(state.CodeVerifier))
	}

	token, err := oauthConfig.Exchange(ctx, code, exchangeOpts...)
	if err != nil {
		p.API.LogError("Failed to exchange code with Authentik", "error", err.Error())
		p.renderError(w, "Failed to exchange authorization code with Authentik.")
		return
	}

	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok {
		p.API.LogError("No ID token returned by Authentik")
		p.renderError(w, "No ID token received from Authentik.")
		return
	}

	idToken, err := verifier.Verify(ctx, rawIDToken)
	if err != nil {
		p.API.LogError("Failed to verify ID token", "error", err.Error())
		p.renderError(w, "Invalid ID token from Authentik.")
		return
	}

	if idToken.Nonce != state.Nonce {
		p.API.LogError("Nonce mismatch in ID token", "expected", state.Nonce, "actual", idToken.Nonce)
		p.renderError(w, "Security check failed: nonce mismatch.")
		return
	}

	userInfo, err := p.extractAuthentikUserInfo(ctx, idToken, token)
	if err != nil {
		p.API.LogError("Failed to extract user info from Authentik", "error", err.Error())
		p.renderError(w, "Failed to read user claims from Authentik.")
		return
	}

	if config.RequireEmailVerified && (userInfo.EmailVerified == nil || !*userInfo.EmailVerified) {
		p.API.LogWarn("Authentik login rejected: email not verified", "email", userInfo.Email)
		p.renderError(w, "Your email address is not verified in Authentik.")
		return
	}

	mmUser, err := p.getOrCreateUser(userInfo, config)
	if err != nil {
		p.API.LogError("Failed to create or update Mattermost user", "error", err.Error())
		p.renderError(w, "Failed to provision Mattermost user account.")
		return
	}

	if mmUser.DeleteAt > 0 {
		p.API.LogWarn("Deactivated user attempted login", "user_id", mmUser.Id)
		p.renderError(w, "Your Mattermost account is deactivated. Contact an administrator.")
		return
	}

	// Synchronize Admin Role based on Authentik groups
	if err := p.syncAdminRole(mmUser, userInfo, config); err != nil {
		p.API.LogWarn("Failed to sync admin role", "error", err.Error())
	}

	// Create Mattermost session
	mmConfig := p.API.GetConfig()
	isMobile := state.MobileRedirect != ""

	sessionLengthHours := 720 // default 30 days
	if mmConfig != nil {
		if isMobile && mmConfig.ServiceSettings.SessionLengthMobileInHours != nil {
			sessionLengthHours = *mmConfig.ServiceSettings.SessionLengthMobileInHours
		} else if mmConfig.ServiceSettings.SessionLengthWebInHours != nil {
			sessionLengthHours = *mmConfig.ServiceSettings.SessionLengthWebInHours
		}
	}

	userAgent := r.UserAgent()
	if len(userAgent) > 256 {
		userAgent = userAgent[:256]
	}

	csrfToken := model.NewId()

	session, appErr := p.API.CreateSession(&model.Session{
		UserId:        mmUser.Id,
		Roles:         mmUser.GetRawRoles(),
		IsOAuth:       false,
		ExpiresAt:     model.GetMillis() + int64(sessionLengthHours)*60*60*1000,
		ExpiredNotify: false,
		Props: model.StringMap{
			"platform":                    "authentik_plugin",
			"os":                          userAgent,
			"csrf":                        csrfToken,
			model.UserAuthServiceIsOAuth:  "true",
			model.UserAuthServiceIsMobile: strconv.FormatBool(isMobile),
		},
	})
	if appErr != nil {
		p.API.LogError("Failed to create Mattermost session", "error", appErr.Error(), "user_id", mmUser.Id)
		p.renderError(w, "Failed to create login session.")
		return
	}

	if isMobile {
		p.renderMobileAuthComplete(w, state.MobileRedirect, session.Token, csrfToken)
		return
	}

	siteURL := p.getSiteURL()
	p.setSessionCookie(w, session, siteURL)

	returnTo := state.ReturnTo
	if returnTo == "" || !strings.HasPrefix(returnTo, "/") || strings.HasPrefix(returnTo, "//") || strings.ContainsRune(returnTo, '\\') {
		returnTo = "/"
	}

	if state.Popup {
		p.renderPopupAuthComplete(w, siteURL, returnTo)
		return
	}

	http.Redirect(w, r, siteURL+returnTo, http.StatusFound)
}

// extractAuthentikUserInfo parses claims from ID token and UserInfo endpoint.
func (p *Plugin) extractAuthentikUserInfo(ctx context.Context, idToken *oidc.IDToken, oauthToken *oauth2.Token) (*AuthentikUserInfo, error) {
	var claims map[string]interface{}
	if err := idToken.Claims(&claims); err != nil {
		return nil, fmt.Errorf("failed to parse ID token claims: %w", err)
	}

	provider := p.getOIDCProvider()
	if provider != nil && oauthToken != nil && oauthToken.AccessToken != "" {
		tokenSource := oauth2.StaticTokenSource(oauthToken)
		if userInfoResp, err := provider.UserInfo(ctx, tokenSource); err == nil {
			var uiClaims map[string]interface{}
			if err := userInfoResp.Claims(&uiClaims); err == nil {
				for k, v := range uiClaims {
					if _, exists := claims[k]; !exists {
						claims[k] = v
					}
				}
			}
		}
	}

	email, _ := claims["email"].(string)
	if email == "" {
		return nil, fmt.Errorf("email claim missing in Authentik token")
	}
	if _, err := mail.ParseAddress(email); err != nil {
		return nil, fmt.Errorf("invalid email address '%s': %w", email, err)
	}

	// Username resolution from preferred_username, username, nickname, or email prefix
	username := ""
	for _, k := range []string{"preferred_username", "username", "nickname"} {
		if val, ok := claims[k].(string); ok && strings.TrimSpace(val) != "" {
			username = strings.TrimSpace(val)
			break
		}
	}
	if username == "" {
		parts := strings.SplitN(email, "@", 2)
		username = parts[0]
	}
	username = sanitizeUsername(username)

	firstName, _ := claims["given_name"].(string)
	lastName, _ := claims["family_name"].(string)
	if firstName == "" && lastName == "" {
		if name, ok := claims["name"].(string); ok && name != "" {
			parts := strings.SplitN(strings.TrimSpace(name), " ", 2)
			firstName = parts[0]
			if len(parts) > 1 {
				lastName = parts[1]
			}
		}
	}

	var emailVerified *bool
	if ev, ok := claims["email_verified"].(bool); ok {
		emailVerified = &ev
	}

	groups := parseStringSlice(claims["groups"])
	if len(groups) == 0 {
		groups = parseStringSlice(claims["roles"])
	}

	return &AuthentikUserInfo{
		Subject:       idToken.Subject,
		Email:         email,
		Username:      username,
		FirstName:     firstName,
		LastName:      lastName,
		EmailVerified: emailVerified,
		Groups:        groups,
	}, nil
}

// getOrCreateUser finds an existing user by Authentik Subject or Email, or creates a new one.
func (p *Plugin) getOrCreateUser(userInfo *AuthentikUserInfo, config *Configuration) (*model.User, error) {
	kvKey := KVUserPrefix + userInfo.Subject
	if userIDBytes, appErr := p.API.KVGet(kvKey); appErr == nil && len(userIDBytes) > 0 {
		userID := string(userIDBytes)
		user, getErr := p.API.GetUser(userID)
		if getErr == nil && user != nil {
			return p.updateUserIfChanged(user, userInfo)
		}
		_ = p.API.KVDelete(kvKey)
	}

	// Check if user exists by email
	user, appErr := p.API.GetUserByEmail(userInfo.Email)
	if appErr == nil && user != nil {
		if !config.AutoLinkByEmail {
			return nil, fmt.Errorf("user with email %s exists but auto-linking is disabled", userInfo.Email)
		}

		p.API.LogInfo("Linking existing Mattermost user to Authentik", "user_id", user.Id, "email", user.Email)
		_, updateErr := p.API.UpdateUserAuth(user.Id, &model.UserAuth{
			AuthService: AuthService,
			AuthData:    model.NewPointer(userInfo.Subject),
		})
		if updateErr != nil {
			return nil, fmt.Errorf("failed to link user auth: %s", updateErr.Error())
		}

		_ = p.API.KVSet(kvKey, []byte(user.Id))
		return p.updateUserIfChanged(user, userInfo)
	}

	// Create new user if enabled
	if !config.AutoCreateAccounts {
		return nil, fmt.Errorf("user %s not found and auto-creation is disabled", userInfo.Email)
	}

	newUser := &model.User{
		Email:         userInfo.Email,
		Username:      userInfo.Username,
		FirstName:     userInfo.FirstName,
		LastName:      userInfo.LastName,
		AuthService:   AuthService,
		AuthData:      model.NewPointer(userInfo.Subject),
		EmailVerified: userInfo.EmailVerified == nil || *userInfo.EmailVerified,
	}

	createdUser, appErr := p.API.CreateUser(newUser)
	if appErr != nil {
		// If username already taken, add a unique suffix
		if strings.Contains(appErr.Message, "username") || appErr.Id == "app.user.save.username_exists.app_error" {
			suffix, _ := generateRandomKey(3)
			newUser.Username = sanitizeUsername(newUser.Username + "_" + suffix)
			createdUser, appErr = p.API.CreateUser(newUser)
			if appErr != nil {
				return nil, fmt.Errorf("failed to create user with suffix: %s", appErr.Error())
			}
		} else {
			return nil, fmt.Errorf("failed to create user: %s", appErr.Error())
		}
	}

	p.API.LogInfo("Created new Mattermost user via Authentik", "user_id", createdUser.Id, "username", createdUser.Username)
	_ = p.API.KVSet(kvKey, []byte(createdUser.Id))

	// Auto-join the default team for newly created users
	if config.DefaultTeam != "" {
		if err := p.ensureDefaultTeamMember(createdUser.Id, config.DefaultTeam); err != nil {
			p.API.LogWarn("Failed to add new user to default team", "user_id", createdUser.Id, "team", config.DefaultTeam, "error", err.Error())
		}
	}

	return createdUser, nil
}

// updateUserIfChanged updates user fields if profile information changed in Authentik.
func (p *Plugin) updateUserIfChanged(user *model.User, info *AuthentikUserInfo) (*model.User, error) {
	changed := false

	if info.Email != "" && user.Email != info.Email {
		user.Email = info.Email
		changed = true
	}
	if user.FirstName != info.FirstName {
		user.FirstName = info.FirstName
		changed = true
	}
	if user.LastName != info.LastName {
		user.LastName = info.LastName
		changed = true
	}
	if info.EmailVerified != nil && user.EmailVerified != *info.EmailVerified {
		user.EmailVerified = *info.EmailVerified
		changed = true
	}

	if !changed {
		return user, nil
	}

	updatedUser, appErr := p.API.UpdateUser(user)
	if appErr != nil {
		return nil, fmt.Errorf("failed to update user profile: %s", appErr.Error())
	}
	return updatedUser, nil
}

func (p *Plugin) signState(token string) string {
	mac := hmac.New(sha256.New, []byte(p.getEncryptionKey()))
	mac.Write([]byte(token))
	return token + ":" + hex.EncodeToString(mac.Sum(nil))
}

func (p *Plugin) verifyAndExtractState(signedState string) (string, error) {
	parts := strings.SplitN(signedState, ":", 2)
	if len(parts) != 2 {
		return "", fmt.Errorf("malformed state parameter")
	}

	token := parts[0]
	expected := p.signState(token)
	if !hmac.Equal([]byte(signedState), []byte(expected)) {
		return "", fmt.Errorf("state signature mismatch")
	}
	return token, nil
}

func (p *Plugin) setStateCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     stateCookieName,
		Value:    token,
		Path:     "/",
		MaxAge:   int(stateExpiry.Seconds()),
		HttpOnly: true,
		Secure:   strings.HasPrefix(p.getSiteURL(), "https"),
		SameSite: http.SameSiteLaxMode,
	})
}

func (p *Plugin) clearStateCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     stateCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   strings.HasPrefix(p.getSiteURL(), "https"),
		SameSite: http.SameSiteLaxMode,
	})
}

func (p *Plugin) stateCookieMatches(r *http.Request, token string) bool {
	c, err := r.Cookie(stateCookieName)
	if err != nil || c.Value == "" {
		return false
	}
	return hmac.Equal([]byte(c.Value), []byte(token))
}

func (p *Plugin) setSessionCookie(w http.ResponseWriter, session *model.Session, siteURL string) {
	secure := strings.HasPrefix(siteURL, "https")
	domain := p.getCookieDomain()
	cookiePath := getCookiePath(siteURL)

	maxAge := max(int((session.ExpiresAt-model.GetMillis())/1000), 1)
	expires := time.UnixMilli(session.ExpiresAt)

	csrf := session.GetCSRF()
	if csrf == "" {
		if propCsrf, ok := session.Props["csrf"]; ok && propCsrf != "" {
			csrf = propCsrf
		} else {
			csrf = model.NewId()
		}
	}

	cookies := []*http.Cookie{
		{Name: model.SessionCookieToken, Value: session.Token, HttpOnly: true, Path: cookiePath, Domain: domain, MaxAge: maxAge, Expires: expires, Secure: secure, SameSite: http.SameSiteLaxMode},
		{Name: model.SessionCookieUser, Value: session.UserId, Path: cookiePath, Domain: domain, MaxAge: maxAge, Expires: expires, Secure: secure, SameSite: http.SameSiteLaxMode},
		{Name: model.SessionCookieCsrf, Value: csrf, Path: cookiePath, Domain: domain, MaxAge: maxAge, Expires: expires, Secure: secure, SameSite: http.SameSiteLaxMode},
	}
	for _, c := range cookies {
		if domain != "" {
			http.SetCookie(w, &http.Cookie{Name: c.Name, Value: "", Path: cookiePath, MaxAge: -1, Secure: c.Secure, HttpOnly: c.HttpOnly, SameSite: c.SameSite})
		}
		http.SetCookie(w, c)
	}
}

func (p *Plugin) getCookieDomain() string {
	cfg := p.API.GetConfig()
	if cfg == nil || cfg.ServiceSettings.AllowCookiesForSubdomains == nil || !*cfg.ServiceSettings.AllowCookiesForSubdomains {
		return ""
	}
	u, err := url.Parse(p.getSiteURL())
	if err != nil {
		return ""
	}
	return u.Hostname()
}

func (p *Plugin) renderError(w http.ResponseWriter, message string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusUnauthorized)
	_, _ = fmt.Fprintf(w, `<!DOCTYPE html>
<html>
<head>
	<meta charset="utf-8">
	<title>Authentication Error</title>
	<style>
		body { font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif; background: #f8f9fa; display: flex; align-items: center; justify-content: center; height: 100vh; margin: 0; }
		.card { background: #fff; padding: 32px; border-radius: 8px; box-shadow: 0 4px 12px rgba(0,0,0,0.1); max-width: 450px; text-align: center; }
		h2 { color: #d9534f; margin-top: 0; }
		p { color: #555; line-height: 1.5; }
		a.btn { display: inline-block; margin-top: 20px; padding: 10px 20px; background: #fd4b2d; color: #fff; text-decoration: none; border-radius: 4px; font-weight: 600; }
	</style>
</head>
<body>
	<div class="card">
		<h2>Authentication Failed</h2>
		<p>%s</p>
		<a class="btn" href="%s/login">Return to Login</a>
	</div>
</body>
</html>`, html.EscapeString(message), html.EscapeString(p.getSiteURL()))
}

func (p *Plugin) renderPopupAuthComplete(w http.ResponseWriter, siteURL, returnTo string) {
	target := siteURL + returnTo
	targetJS, _ := json.Marshal(target)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate, private")
	w.Header().Set("Pragma", "no-cache")
	w.WriteHeader(http.StatusOK)
	_, _ = fmt.Fprintf(w, `<!DOCTYPE html>
<html>
<head>
	<meta charset="utf-8">
	<title>Authentik Authentication Complete</title>
</head>
<body style="font-family: sans-serif; max-width: 500px; margin: 50px auto; text-align: center;">
	<h3>Authenticated with Authentik</h3>
	<p>You are logged in. You can close this window.</p>
	<p><a href="%s">Click here if not redirected</a></p>
	<script>
	(function() {
		var target = %s;
		try { window.localStorage.setItem('mattermost_authentik_login', String(Date.now())); } catch(e) {}
		try {
			if (window.opener && !window.opener.closed) {
				window.opener.location = target;
			}
		} catch(e) {}
		try { window.close(); } catch(e) {}
	})();
	</script>
</body>
</html>`, html.EscapeString(target), targetJS)
}

func (p *Plugin) renderMobileAuthComplete(w http.ResponseWriter, redirectTo, token, csrf string) {
	u, err := url.Parse(redirectTo)
	if err != nil {
		p.renderError(w, "Invalid mobile redirect URL")
		return
	}

	q := u.Query()
	q.Set(model.SessionCookieToken, token)
	q.Set(model.SessionCookieCsrf, csrf)
	u.RawQuery = q.Encode()
	link := u.String()

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate, private")
	w.WriteHeader(http.StatusOK)
	_, _ = fmt.Fprintf(w, `<!DOCTYPE html>
<html>
<head>
	<meta charset="utf-8">
	<meta http-equiv="refresh" content="0; url=%s">
	<title>Redirecting to Mattermost App</title>
</head>
<body style="font-family: sans-serif; max-width: 500px; margin: 50px auto; text-align: center;">
	<h3>Authentication Complete</h3>
	<p>Redirecting you back to Mattermost&hellip;</p>
	<p><a href="%s">Tap here if not redirected automatically</a></p>
</body>
</html>`, html.EscapeString(link), html.EscapeString(link))
}
