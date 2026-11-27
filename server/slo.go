package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"

	oidc "github.com/coreos/go-oidc/v3/oidc"
	"github.com/mattermost/mattermost/server/public/model"
)

const (
	// KVUserSessionsPrefix stores active Mattermost session IDs for a user to support SLO.
	KVUserSessionsPrefix = "authentik_sessions_"

	// KVSIDPrefix maps an OIDC session ID (sid) to Mattermost session IDs.
	KVSIDPrefix = "authentik_sid_"

	backChannelLogoutEvent = "http://schemas.openid.net/event/backchannel-logout"
	maxTrackedSessions     = 20
)

type trackedSession struct {
	SessionID string `json:"session_id"`
	SID       string `json:"sid,omitempty"`
	ExpiresAt int64  `json:"expires_at"`
}

// trackUserSession records a Mattermost session ID in the KV store for Single Logout revocation.
func (p *Plugin) trackUserSession(userID, sessionID, sid string, expiresAt int64) {
	if p.API == nil || userID == "" || sessionID == "" {
		return
	}

	now := model.GetMillis()
	kvKey := KVUserSessionsPrefix + userID

	// shortcut: non-atomic read-modify-write; simultaneous multi-device logins for one user may drop one tracked session
	var sessions []trackedSession
	if data, appErr := p.API.KVGet(kvKey); appErr == nil && len(data) > 0 {
		_ = json.Unmarshal(data, &sessions)
	}

	active := make([]trackedSession, 0, len(sessions)+1)
	for _, s := range sessions {
		if s.ExpiresAt > now && s.SessionID != sessionID {
			active = append(active, s)
		}
	}
	active = append(active, trackedSession{
		SessionID: sessionID,
		SID:       sid,
		ExpiresAt: expiresAt,
	})
	for len(active) > maxTrackedSessions {
		evicted := active[0]
		active = active[1:]
		if err := p.API.RevokeSession(evicted.SessionID); err != nil {
			p.API.LogWarn("Failed to revoke evicted oldest SSO session", "session_id", evicted.SessionID, "error", err.Error())
		}
	}

	maxExpiresAt := expiresAt
	for _, s := range active {
		if s.ExpiresAt > maxExpiresAt {
			maxExpiresAt = s.ExpiresAt
		}
	}
	ttl := max((maxExpiresAt-now)/1000, 60)
	if encoded, err := json.Marshal(active); err == nil {
		_ = p.API.KVSetWithExpiry(kvKey, encoded, ttl)
	}

	if sid != "" {
		sidKey := KVSIDPrefix + sid
		var sidSessions []string
		if data, appErr := p.API.KVGet(sidKey); appErr == nil && len(data) > 0 {
			if json.Unmarshal(data, &sidSessions) != nil {
				sidSessions = []string{string(data)}
			}
		}
		found := false
		for _, id := range sidSessions {
			if id == sessionID {
				found = true
				break
			}
		}
		if !found {
			sidSessions = append(sidSessions, sessionID)
		}
		if encoded, err := json.Marshal(sidSessions); err == nil {
			_ = p.API.KVSetWithExpiry(sidKey, encoded, ttl)
		}
	}
}

func (p *Plugin) revokeUserSessions(userID string) (int, error) {
	if p.API == nil || userID == "" {
		return 0, nil
	}

	kvKey := KVUserSessionsPrefix + userID
	data, appErr := p.API.KVGet(kvKey)
	if appErr != nil || len(data) == 0 {
		return 0, nil
	}

	var sessions []trackedSession
	if err := json.Unmarshal(data, &sessions); err != nil {
		return 0, nil
	}

	now := model.GetMillis()
	revoked := 0
	var remaining []trackedSession
	var firstErr error

	for _, s := range sessions {
		if s.ExpiresAt <= now {
			continue
		}
		if err := p.API.RevokeSession(s.SessionID); err != nil {
			// If the session is already gone, treat it as revoked; otherwise retain for retry.
			if _, getErr := p.API.GetSession(s.SessionID); getErr == nil {
				p.API.LogWarn("Failed to revoke Mattermost session during SLO", "session_id", s.SessionID, "error", err.Error())
				remaining = append(remaining, s)
				if firstErr == nil {
					firstErr = fmt.Errorf("failed to revoke session %s: %s", s.SessionID, err.Error())
				}
				continue
			}
		}
		revoked++
		if s.SID != "" {
			_ = p.API.KVDelete(KVSIDPrefix + s.SID)
		}
	}

	if len(remaining) > 0 {
		maxExpiresAt := now
		for _, s := range remaining {
			if s.ExpiresAt > maxExpiresAt {
				maxExpiresAt = s.ExpiresAt
			}
		}
		if encoded, err := json.Marshal(remaining); err == nil {
			_ = p.API.KVSetWithExpiry(kvKey, encoded, max((maxExpiresAt-now)/1000, 60))
		}
		return revoked, firstErr
	}

	_ = p.API.KVDelete(kvKey)
	return revoked, nil
}

func (p *Plugin) revokeSessionBySID(sid string) (bool, error) {
	if p.API == nil || sid == "" {
		return false, nil
	}

	kvKey := KVSIDPrefix + sid
	data, appErr := p.API.KVGet(kvKey)
	if appErr != nil || len(data) == 0 {
		return false, nil
	}

	var sessionIDs []string
	if err := json.Unmarshal(data, &sessionIDs); err != nil {
		sessionIDs = []string{string(data)}
	}

	var remaining []string
	var firstErr error
	for _, sessionID := range sessionIDs {
		if sessionID == "" {
			continue
		}
		if err := p.API.RevokeSession(sessionID); err != nil {
			if _, getErr := p.API.GetSession(sessionID); getErr == nil {
				p.API.LogWarn("Failed to revoke Mattermost session by OIDC sid", "sid", sid, "session_id", sessionID, "error", err.Error())
				remaining = append(remaining, sessionID)
				if firstErr == nil {
					firstErr = fmt.Errorf("failed to revoke session %s: %s", sessionID, err.Error())
				}
			}
		}
	}

	if len(remaining) > 0 {
		if encoded, err := json.Marshal(remaining); err == nil {
			_ = p.API.KVSetWithExpiry(kvKey, encoded, 3600)
		}
		return true, firstErr
	}

	_ = p.API.KVDelete(kvKey)
	return true, nil
}

// handleOAuth2Logout handles RP-initiated Single Logout, revoking local tracked sessions
// and redirecting to Authentik's end_session_endpoint when EnableSLO is active.
func (p *Plugin) handleOAuth2Logout(w http.ResponseWriter, r *http.Request) {
	config := p.getConfiguration()
	siteURL := p.getSiteURL()
	fallbackRedirect := siteURL + "/login?no_sso=1"

	if userID := r.Header.Get("Mattermost-User-Id"); userID != "" {
		_, _ = p.revokeUserSessions(userID)
	}

	if !config.Enable || !config.EnableSLO {
		http.Redirect(w, r, fallbackRedirect, http.StatusFound)
		return
	}

	p.retryOIDCProvider()
	endSession := p.getEndSessionEndpoint()
	if endSession == "" {
		http.Redirect(w, r, fallbackRedirect, http.StatusFound)
		return
	}

	u, err := url.Parse(endSession)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") {
		http.Redirect(w, r, fallbackRedirect, http.StatusFound)
		return
	}

	q := u.Query()
	if config.ClientID != "" {
		q.Set("client_id", config.ClientID)
	}
	if siteURL != "" {
		q.Set("post_logout_redirect_uri", fallbackRedirect)
	}
	u.RawQuery = q.Encode()

	http.Redirect(w, r, u.String(), http.StatusFound)
}

// handleBackChannelLogout implements OpenID Connect Back-Channel Logout 1.0 (POST /oauth2/backchannel-logout).
func (p *Plugin) handleBackChannelLogout(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")

	config := p.getConfiguration()
	if !config.Enable || !config.EnableSLO {
		http.Error(w, "Single Logout is disabled", http.StatusBadRequest)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 64*1024)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form payload", http.StatusBadRequest)
		return
	}

	rawLogoutToken := r.PostFormValue("logout_token")
	if rawLogoutToken == "" {
		http.Error(w, "Missing logout_token", http.StatusBadRequest)
		return
	}

	p.retryOIDCProvider()
	verifier := p.getOIDCVerifier()
	if verifier == nil {
		http.Error(w, "OIDC verifier not initialized", http.StatusServiceUnavailable)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	ctx = oidc.ClientContext(ctx, p.getHTTPClient(config))

	idToken, err := verifier.Verify(ctx, rawLogoutToken)
	if err != nil {
		p.API.LogWarn("Invalid back-channel logout_token", "error", err.Error())
		http.Error(w, "Invalid logout_token", http.StatusBadRequest)
		return
	}

	var claims struct {
		Subject string                 `json:"sub"`
		SID     string                 `json:"sid"`
		Nonce   string                 `json:"nonce"`
		Events  map[string]interface{} `json:"events"`
	}
	if err := idToken.Claims(&claims); err != nil {
		http.Error(w, "Failed to parse logout_token claims", http.StatusBadRequest)
		return
	}

	// OIDC Back-Channel Logout 1.0 Section 2.4: nonce MUST NOT be present; events MUST contain backchannel-logout.
	if claims.Nonce != "" {
		http.Error(w, "logout_token must not contain nonce", http.StatusBadRequest)
		return
	}
	if _, ok := claims.Events[backChannelLogoutEvent]; !ok {
		http.Error(w, "Missing backchannel-logout event claim", http.StatusBadRequest)
		return
	}
	if claims.Subject == "" && claims.SID == "" {
		http.Error(w, "logout_token must contain sub or sid", http.StatusBadRequest)
		return
	}

	var revokeErr error
	revokedBySID := false
	if claims.SID != "" {
		revokedBySID, revokeErr = p.revokeSessionBySID(claims.SID)
	}
	if !revokedBySID && claims.Subject != "" {
		if userIDBytes, appErr := p.API.KVGet(KVUserPrefix + claims.Subject); appErr == nil && len(userIDBytes) > 0 {
			_, revokeErr = p.revokeUserSessions(string(userIDBytes))
		}
	}
	if revokeErr != nil {
		http.Error(w, "Failed to revoke all sessions", http.StatusServiceUnavailable)
		return
	}

	p.API.LogInfo("Processed OIDC back-channel logout", "sub", claims.Subject, "sid", claims.SID)
	w.WriteHeader(http.StatusOK)
}
