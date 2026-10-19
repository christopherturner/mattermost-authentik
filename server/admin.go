package main

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/mattermost/mattermost/server/public/model"
)

// handleGetLDAPUsers lists all active users whose auth_service is set to "ldap".
func (p *Plugin) handleGetLDAPUsers(w http.ResponseWriter, r *http.Request) {
	userID := r.Header.Get("Mattermost-User-Id")
	if userID == "" || !p.isAdmin(userID) {
		http.Error(w, "Unauthorized: System Administrator permissions required", http.StatusForbidden)
		return
	}

	var ldapUsers []map[string]interface{}
	page := 0
	perPage := 200

	for {
		users, appErr := p.API.GetUsers(&model.UserGetOptions{
			Page:    page,
			PerPage: perPage,
		})
		if appErr != nil || len(users) == 0 {
			break
		}

		for _, u := range users {
			if u.AuthService == "ldap" && u.DeleteAt == 0 {
				ldapUsers = append(ldapUsers, map[string]interface{}{
					"id":         u.Id,
					"username":   u.Username,
					"email":      u.Email,
					"first_name": u.FirstName,
					"last_name":  u.LastName,
					"roles":      u.Roles,
				})
			}
		}

		if len(users) < perPage {
			break
		}
		page++
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"count": len(ldapUsers),
		"users": ldapUsers,
	})
}

// handleUnlockLDAPUsers removes the "ldap" auth_service lock from specified or all LDAP users.
// This sets auth_service = "" and auth_data = nil, restoring normal email status so they can
// be edited/deactivated in the GUI or seamlessly linked to Authentik on next login.
func (p *Plugin) handleUnlockLDAPUsers(w http.ResponseWriter, r *http.Request) {
	userID := r.Header.Get("Mattermost-User-Id")
	if userID == "" || !p.isAdmin(userID) {
		http.Error(w, "Unauthorized", http.StatusForbidden)
		return
	}

	var req struct {
		UserIDs []string `json:"user_ids"` // optional: if empty, unlocks all active ldap users
	}
	_ = json.NewDecoder(r.Body).Decode(&req)

	targetIDs := req.UserIDs
	if len(targetIDs) == 0 {
		// Unlock all LDAP users
		page := 0
		perPage := 200
		for {
			users, appErr := p.API.GetUsers(&model.UserGetOptions{Page: page, PerPage: perPage})
			if appErr != nil || len(users) == 0 {
				break
			}
			for _, u := range users {
				if u.AuthService == "ldap" && u.DeleteAt == 0 {
					targetIDs = append(targetIDs, u.Id)
				}
			}
			if len(users) < perPage {
				break
			}
			page++
		}
	}

	unlockedCount := 0
	var failedUsers []string

	for _, uid := range targetIDs {
		_, appErr := p.API.UpdateUserAuth(uid, &model.UserAuth{
			AuthService: "",
			AuthData:    nil,
		})
		if appErr != nil {
			p.API.LogError("Failed to unlock LDAP user", "user_id", uid, "error", appErr.Error())
			failedUsers = append(failedUsers, uid)
		} else {
			unlockedCount++
		}
	}

	p.API.LogInfo("Unlocked LDAP accounts", "count", unlockedCount)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"success":  true,
		"unlocked": unlockedCount,
		"failed":   failedUsers,
	})
}

// handleDeactivateLDAPUsers deactivates LDAP accounts.
func (p *Plugin) handleDeactivateLDAPUsers(w http.ResponseWriter, r *http.Request) {
	userID := r.Header.Get("Mattermost-User-Id")
	if userID == "" || !p.isAdmin(userID) {
		http.Error(w, "Unauthorized", http.StatusForbidden)
		return
	}

	var req struct {
		UserIDs []string `json:"user_ids"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)

	targetIDs := req.UserIDs
	if len(targetIDs) == 0 {
		page := 0
		perPage := 200
		for {
			users, appErr := p.API.GetUsers(&model.UserGetOptions{Page: page, PerPage: perPage})
			if appErr != nil || len(users) == 0 {
				break
			}
			for _, u := range users {
				if u.AuthService == "ldap" && u.DeleteAt == 0 {
					targetIDs = append(targetIDs, u.Id)
				}
			}
			if len(users) < perPage {
				break
			}
			page++
		}
	}

	deactivatedCount := 0
	for _, uid := range targetIDs {
		if appErr := p.API.UpdateUserActive(uid, false); appErr != nil {
			p.API.LogError("Failed to deactivate LDAP user", "user_id", uid, "error", appErr.Error())
		} else {
			deactivatedCount++
		}
	}

	p.API.LogInfo("Deactivated LDAP accounts", "count", deactivatedCount)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"success":     true,
		"deactivated": deactivatedCount,
	})
}

// isAdmin checks if the user has System Admin role.
func (p *Plugin) isAdmin(userID string) bool {
	user, err := p.API.GetUser(userID)
	if err != nil || user == nil {
		return false
	}
	return user.IsInRole(model.SystemAdminRoleId)
}

// DiagnosticStep records a step in the connectivity diagnosis.
type DiagnosticStep struct {
	Name      string `json:"name"`
	Status    string `json:"status"` // "ok", "error", "warning"
	URL       string `json:"url,omitempty"`
	LatencyMs int64  `json:"latency_ms,omitempty"`
	Details   string `json:"details"`
}

// DiagnosticResponse contains the full diagnostic results.
type DiagnosticResponse struct {
	Success         bool             `json:"success"`
	EffectiveIssuer string           `json:"effective_issuer"`
	CallbackURL     string           `json:"callback_url"`
	ScopesSupported []string         `json:"scopes_supported,omitempty"`
	Steps           []DiagnosticStep `json:"steps"`
}

// handleDiagnoseConnection tests connectivity, OIDC discovery, JWKS retrieval, and credentials.
func (p *Plugin) handleDiagnoseConnection(w http.ResponseWriter, r *http.Request) {
	userID := r.Header.Get("Mattermost-User-Id")
	if userID == "" || !p.isAdmin(userID) {
		http.Error(w, "Unauthorized", http.StatusForbidden)
		return
	}

	var req struct {
		AuthentikURL    string `json:"authentik_url"`
		ApplicationSlug string `json:"application_slug"`
		IssuerURL       string `json:"issuer_url"`
		ClientID        string `json:"client_id"`
		ClientSecret    string `json:"client_secret"`
		CustomCert      string `json:"custom_cert"`
		SkipTLSVerify   *bool  `json:"skip_tls_verify"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)

	savedConfig := p.getConfiguration()

	authentikURL := strings.TrimSpace(req.AuthentikURL)
	if authentikURL == "" {
		authentikURL = savedConfig.AuthentikURL
	}
	slug := strings.TrimSpace(req.ApplicationSlug)
	if slug == "" {
		slug = savedConfig.ApplicationSlug
	}
	if slug == "" {
		slug = "mattermost"
	}
	issuerURL := strings.TrimSpace(req.IssuerURL)
	if issuerURL == "" {
		issuerURL = savedConfig.IssuerURL
	}
	clientID := strings.TrimSpace(req.ClientID)
	if clientID == "" {
		clientID = savedConfig.ClientID
	}
	clientSecret := strings.TrimSpace(req.ClientSecret)
	if clientSecret == "" {
		clientSecret = savedConfig.ClientSecret
	}
	customCert := strings.TrimSpace(req.CustomCert)
	if customCert == "" {
		customCert = savedConfig.CustomCert
	}
	skipTLSVerify := savedConfig.SkipTLSVerify
	if req.SkipTLSVerify != nil {
		skipTLSVerify = *req.SkipTLSVerify
	}

	var effectiveIssuer string
	if issuerURL != "" {
		effectiveIssuer = strings.TrimRight(issuerURL, "/")
	} else if authentikURL != "" {
		effectiveIssuer = fmt.Sprintf("%s/application/o/%s", strings.TrimRight(authentikURL, "/"), slug)
	}

	callbackURL := p.getCallbackURL()

	var steps []DiagnosticStep
	overallSuccess := true

	if effectiveIssuer == "" {
		steps = append(steps, DiagnosticStep{
			Name:    "Configuration Check",
			Status:  "error",
			Details: "Authentik URL or Issuer URL is empty. Please enter your Authentik Base URL or custom Issuer URL.",
		})
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(DiagnosticResponse{
			Success:     false,
			CallbackURL: callbackURL,
			Steps:       steps,
		})
		return
	}

	diagConfig := &Configuration{
		CustomCert:    customCert,
		SkipTLSVerify: skipTLSVerify,
	}
	httpClient := p.getHTTPClient(diagConfig)
	httpClient.Timeout = 10 * time.Second

	// Step 1: TLS / Certificate Check (tests if a certificate file is needed)
	certStep := p.checkTLSCertificate(effectiveIssuer, customCert, skipTLSVerify)
	steps = append(steps, certStep)
	if certStep.Status == "error" {
		overallSuccess = false
	}

	// Step 2: OpenID Connect Discovery
	discoveryURL := effectiveIssuer + "/.well-known/openid-configuration"
	t0 := time.Now()
	resp, err := httpClient.Get(discoveryURL)
	latency := time.Since(t0).Milliseconds()

	var discoveryDoc struct {
		Issuer                string   `json:"issuer"`
		AuthorizationEndpoint string   `json:"authorization_endpoint"`
		TokenEndpoint         string   `json:"token_endpoint"`
		UserinfoEndpoint      string   `json:"userinfo_endpoint"`
		JwksURI               string   `json:"jwks_uri"`
		ScopesSupported       []string `json:"scopes_supported"`
	}

	if err != nil {
		overallSuccess = false
		steps = append(steps, DiagnosticStep{
			Name:      "OIDC Discovery",
			Status:    "error",
			URL:       discoveryURL,
			LatencyMs: latency,
			Details:   fmt.Sprintf("Failed to reach discovery endpoint: %v. Check network connectivity, DNS, or firewall between Mattermost and Authentik.", err),
		})
	} else {
		defer resp.Body.Close()
		bodyBytes, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != http.StatusOK {
			overallSuccess = false
			steps = append(steps, DiagnosticStep{
				Name:      "OIDC Discovery",
				Status:    "error",
				URL:       discoveryURL,
				LatencyMs: latency,
				Details:   fmt.Sprintf("Authentik returned HTTP %d on discovery endpoint. Response: %s", resp.StatusCode, string(bodyBytes)),
			})
		} else if parseErr := json.Unmarshal(bodyBytes, &discoveryDoc); parseErr != nil {
			overallSuccess = false
			steps = append(steps, DiagnosticStep{
				Name:      "OIDC Discovery",
				Status:    "error",
				URL:       discoveryURL,
				LatencyMs: latency,
				Details:   fmt.Sprintf("Discovery response was not valid JSON: %v", parseErr),
			})
		} else {
			steps = append(steps, DiagnosticStep{
				Name:      "OIDC Discovery",
				Status:    "ok",
				URL:       discoveryURL,
				LatencyMs: latency,
				Details:   fmt.Sprintf("Valid OIDC configuration found. Issuer: '%s'", discoveryDoc.Issuer),
			})
		}
	}

	// Step 3: JWKS Keys Verification
	jwksURL := discoveryDoc.JwksURI
	if jwksURL == "" && effectiveIssuer != "" {
		jwksURL = effectiveIssuer + "/jwks/"
	}

	if jwksURL != "" && overallSuccess {
		t0 = time.Now()
		jwksResp, jwksErr := httpClient.Get(jwksURL)
		jwksLatency := time.Since(t0).Milliseconds()

		if jwksErr != nil {
			overallSuccess = false
			steps = append(steps, DiagnosticStep{
				Name:      "JWKS Cryptographic Keys",
				Status:    "error",
				URL:       jwksURL,
				LatencyMs: jwksLatency,
				Details:   fmt.Sprintf("Failed to fetch signing keys from %s: %v", jwksURL, jwksErr),
			})
		} else {
			defer jwksResp.Body.Close()
			bodyBytes, _ := io.ReadAll(jwksResp.Body)
			var jwksDoc struct {
				Keys []interface{} `json:"keys"`
			}
			if jwksResp.StatusCode != http.StatusOK {
				overallSuccess = false
				steps = append(steps, DiagnosticStep{
					Name:      "JWKS Cryptographic Keys",
					Status:    "error",
					URL:       jwksURL,
					LatencyMs: jwksLatency,
					Details:   fmt.Sprintf("HTTP %d when fetching JWKS keys: %s", jwksResp.StatusCode, string(bodyBytes)),
				})
			} else if parseErr := json.Unmarshal(bodyBytes, &jwksDoc); parseErr != nil {
				overallSuccess = false
				steps = append(steps, DiagnosticStep{
					Name:      "JWKS Cryptographic Keys",
					Status:    "error",
					URL:       jwksURL,
					LatencyMs: jwksLatency,
					Details:   fmt.Sprintf("Invalid JSON in JWKS keys response: %v", parseErr),
				})
			} else {
				steps = append(steps, DiagnosticStep{
					Name:      "JWKS Cryptographic Keys",
					Status:    "ok",
					URL:       jwksURL,
					LatencyMs: jwksLatency,
					Details:   fmt.Sprintf("Retrieved %d public signing key(s) from Authentik.", len(jwksDoc.Keys)),
				})
			}
		}
	}

	// Step 4: Client Credentials & Token Endpoint Validation
	tokenURL := discoveryDoc.TokenEndpoint
	if tokenURL == "" && effectiveIssuer != "" {
		tokenURL = effectiveIssuer + "/token/"
	}

	if clientID == "" || clientSecret == "" {
		steps = append(steps, DiagnosticStep{
			Name:    "Client ID & Secret Credentials",
			Status:  "warning",
			Details: "Client ID or Client Secret not provided. Skipped credentials verification.",
		})
	} else if tokenURL != "" {
		t0 = time.Now()
		form := url.Values{}
		form.Set("grant_type", "client_credentials")

		tokenReq, _ := http.NewRequest(http.MethodPost, tokenURL, strings.NewReader(form.Encode()))
		tokenReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		tokenReq.SetBasicAuth(clientID, clientSecret)

		tokenResp, tokenErr := httpClient.Do(tokenReq)
		tokenLatency := time.Since(t0).Milliseconds()

		if tokenErr != nil {
			overallSuccess = false
			steps = append(steps, DiagnosticStep{
				Name:      "Client ID & Secret Credentials",
				Status:    "error",
				URL:       tokenURL,
				LatencyMs: tokenLatency,
				Details:   fmt.Sprintf("Failed to contact token endpoint at %s: %v", tokenURL, tokenErr),
			})
		} else {
			defer tokenResp.Body.Close()
			bodyBytes, _ := io.ReadAll(tokenResp.Body)
			bodyStr := string(bodyBytes)

			if tokenResp.StatusCode == http.StatusOK {
				steps = append(steps, DiagnosticStep{
					Name:      "Client ID & Secret Credentials",
					Status:    "ok",
					URL:       tokenURL,
					LatencyMs: tokenLatency,
					Details:   "Client ID and Client Secret successfully verified against Authentik token endpoint.",
				})
			} else if tokenResp.StatusCode == http.StatusUnauthorized {
				overallSuccess = false
				steps = append(steps, DiagnosticStep{
					Name:      "Client ID & Secret Credentials",
					Status:    "error",
					URL:       tokenURL,
					LatencyMs: tokenLatency,
					Details:   "Authentication failed (HTTP 401): Client ID or Client Secret rejected by Authentik. Please double check credentials.",
				})
			} else if strings.Contains(bodyStr, "invalid_client") {
				overallSuccess = false
				steps = append(steps, DiagnosticStep{
					Name:      "Client ID & Secret Credentials",
					Status:    "error",
					URL:       tokenURL,
					LatencyMs: tokenLatency,
					Details:   "Authentik rejected client credentials: 'invalid_client'. Check Client ID and Secret in your Authentik Provider.",
				})
			} else {
				// Token endpoint is reachable and Authentik processed the request without rejecting credentials
				steps = append(steps, DiagnosticStep{
					Name:      "Client ID & Secret Credentials",
					Status:    "ok",
					URL:       tokenURL,
					LatencyMs: tokenLatency,
					Details:   "Authentik token endpoint reachable and responsive.",
				})
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(DiagnosticResponse{
		Success:         overallSuccess,
		EffectiveIssuer: effectiveIssuer,
		CallbackURL:     callbackURL,
		ScopesSupported: discoveryDoc.ScopesSupported,
		Steps:           steps,
	})
}

// checkTLSCertificate tests whether a TLS connection requires a custom certificate file/Root CA,
// validates any configured custom certificate or SkipTLSVerify setting, and returns a DiagnosticStep.
func (p *Plugin) checkTLSCertificate(targetURL string, customCert string, skipTLSVerify bool) DiagnosticStep {
	u, err := url.Parse(targetURL)
	if err != nil {
		return DiagnosticStep{
			Name:    "TLS / SSL Certificate",
			Status:  "error",
			Details: fmt.Sprintf("Invalid URL '%s': %v", targetURL, err),
		}
	}

	if u.Scheme != "https" {
		return DiagnosticStep{
			Name:    "TLS / SSL Certificate",
			Status:  "ok",
			Details: "Connection uses plain HTTP. No TLS certificate file is needed.",
		}
	}

	host := u.Host
	if !strings.Contains(host, ":") {
		host = host + ":443"
	}

	dialer := &net.Dialer{
		Timeout: 5 * time.Second,
	}

	// 1. Try standard TLS handshake using system Root CA pool (without custom cert or skipTLSVerify)
	t0 := time.Now()
	sysConn, sysErr := tls.DialWithDialer(dialer, "tcp", host, &tls.Config{
		ServerName: u.Hostname(),
	})
	latency := time.Since(t0).Milliseconds()

	if sysErr == nil {
		defer sysConn.Close()
		state := sysConn.ConnectionState()
		var certInfo string
		if len(state.PeerCertificates) > 0 {
			peer := state.PeerCertificates[0]
			certInfo = fmt.Sprintf(" (Subject: '%s', Issuer: '%s', Expires: %s)",
				peer.Subject.CommonName, peer.Issuer.CommonName, peer.NotAfter.Format("2006-01-02"))
		}
		return DiagnosticStep{
			Name:      "TLS / SSL Certificate",
			Status:    "ok",
			URL:       targetURL,
			LatencyMs: latency,
			Details:   "Server certificate is trusted by system Root CA pool" + certInfo + ". No custom certificate file is needed.",
		}
	}

	// 2. System verification failed. Connect insecurely to inspect peer certificates for diagnostic details.
	var peerInfo string
	insecureConn, dialErr := tls.DialWithDialer(dialer, "tcp", host, &tls.Config{
		ServerName:         u.Hostname(),
		InsecureSkipVerify: true, //nolint:gosec
	})
	if dialErr != nil {
		return DiagnosticStep{
			Name:      "TLS / SSL Certificate",
			Status:    "error",
			URL:       targetURL,
			LatencyMs: latency,
			Details:   fmt.Sprintf("Failed to establish TCP/TLS connection to %s: %v. Check network connectivity and port.", host, dialErr),
		}
	}
	defer insecureConn.Close()
	state := insecureConn.ConnectionState()
	if len(state.PeerCertificates) > 0 {
		peer := state.PeerCertificates[0]
		peerInfo = fmt.Sprintf(" (Subject: '%s', Issuer: '%s')", peer.Subject.CommonName, peer.Issuer.CommonName)
	}

	// 3. If custom certificate is configured, test if it resolves the verification failure
	if strings.TrimSpace(customCert) != "" {
		pemBytes, loadErr := loadCertPEM(customCert)
		if loadErr != nil {
			return DiagnosticStep{
				Name:      "TLS / SSL Certificate",
				Status:    "error",
				URL:       targetURL,
				LatencyMs: latency,
				Details:   fmt.Sprintf("Custom certificate could not be loaded: %v", loadErr),
			}
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pemBytes) {
			return DiagnosticStep{
				Name:      "TLS / SSL Certificate",
				Status:    "error",
				URL:       targetURL,
				LatencyMs: latency,
				Details:   "Custom certificate is not valid PEM format. Ensure valid BEGIN and END CERTIFICATE blocks.",
			}
		}

		customConn, customErr := tls.DialWithDialer(dialer, "tcp", host, &tls.Config{
			ServerName: u.Hostname(),
			RootCAs:    pool,
		})
		if customErr == nil {
			defer customConn.Close()
			return DiagnosticStep{
				Name:      "TLS / SSL Certificate",
				Status:    "ok",
				URL:       targetURL,
				LatencyMs: latency,
				Details:   "Server uses a private or self-signed certificate" + peerInfo + ". Custom certificate file is configured and successfully verified the connection.",
			}
		}

		return DiagnosticStep{
			Name:      "TLS / SSL Certificate",
			Status:    "error",
			URL:       targetURL,
			LatencyMs: latency,
			Details:   fmt.Sprintf("A custom certificate is configured, but verification still failed: %v%s. Check that the certificate file matches Authentik's CA.", customErr, peerInfo),
		}
	}

	// 4. No custom certificate provided. Check if SkipTLSVerify is bypassing the error.
	if skipTLSVerify {
		return DiagnosticStep{
			Name:      "TLS / SSL Certificate",
			Status:    "warning",
			URL:       targetURL,
			LatencyMs: latency,
			Details:   fmt.Sprintf("A certificate file IS needed for secure verification: server certificate is untrusted%s (%v). 'Skip TLS Certificate Verification' is currently bypassing validation.", peerInfo, sysErr),
		}
	}

	return DiagnosticStep{
		Name:      "TLS / SSL Certificate",
		Status:    "error",
		URL:       targetURL,
		LatencyMs: latency,
		Details:   fmt.Sprintf("A custom certificate file IS needed! The server presented an untrusted certificate%s (%v). Upload or paste the Root CA certificate file into 'Custom Root CA' or enable 'Skip TLS Verification'.", peerInfo, sysErr),
	}
}
