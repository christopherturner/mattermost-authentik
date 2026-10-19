package main

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"sync"
	"time"
)

var (
	rsaPrivateKey *rsa.PrivateKey
	keyID         = "mock-authentik-key-1"
	authCodesLock sync.Mutex
	authCodes     = make(map[string]*AuthSession)
)

type AuthSession struct {
	User          MockUser
	Nonce         string
	CodeChallenge string
	RedirectURI   string
}

type MockUser struct {
	Subject   string
	Email     string
	Username  string
	FirstName string
	LastName  string
	Groups    []string
}

func init() {
	var err error
	rsaPrivateKey, err = rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		log.Fatalf("Failed to generate RSA key: %v", err)
	}
}

func main() {
	mux := http.NewServeMux()

	// OIDC Discovery
	mux.HandleFunc("/application/o/mattermost/.well-known/openid-configuration", handleDiscovery)

	// JWKS Keys
	mux.HandleFunc("/application/o/mattermost/jwks/", handleJWKS)

	// OAuth2 Authorization endpoint
	mux.HandleFunc("/application/o/authorize/", handleAuthorize)

	// OAuth2 Token exchange
	mux.HandleFunc("/application/o/token/", handleToken)

	// UserInfo endpoint
	mux.HandleFunc("/application/o/userinfo/", handleUserInfo)

	// Health check
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	})

	port := ":9000"
	log.Printf("==> Mock Authentik OIDC Server running on http://localhost%s", port)
	log.Printf("    Discovery URL: http://localhost%s/application/o/mattermost/.well-known/openid-configuration", port)
	if err := http.ListenAndServe(port, mux); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}

func handleDiscovery(w http.ResponseWriter, r *http.Request) {
	baseURL := "http://localhost:9000"
	issuer := baseURL + "/application/o/mattermost"

	resp := map[string]interface{}{
		"issuer":                                issuer,
		"authorization_endpoint":                baseURL + "/application/o/authorize/",
		"token_endpoint":                        baseURL + "/application/o/token/",
		"userinfo_endpoint":                     baseURL + "/application/o/userinfo/",
		"jwks_uri":                              issuer + "/jwks/",
		"response_types_supported":              []string{"code"},
		"subject_types_supported":               []string{"public"},
		"id_token_signing_alg_values_supported": []string{"RS256"},
		"scopes_supported":                      []string{"openid", "profile", "email"},
		"token_endpoint_auth_methods_supported": []string{"client_secret_basic", "client_secret_post"},
		"code_challenge_methods_supported":      []string{"S256"},
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func handleJWKS(w http.ResponseWriter, r *http.Request) {
	nBytes := rsaPrivateKey.PublicKey.N.Bytes()
	nStr := base64.RawURLEncoding.EncodeToString(nBytes)

	eBuf := make([]byte, 4)
	binary.BigEndian.PutUint32(eBuf, uint32(rsaPrivateKey.PublicKey.E))
	// trim leading zeros
	for len(eBuf) > 0 && eBuf[0] == 0 {
		eBuf = eBuf[1:]
	}
	eStr := base64.RawURLEncoding.EncodeToString(eBuf)

	jwks := map[string]interface{}{
		"keys": []map[string]interface{}{
			{
				"kty": "RSA",
				"use": "sig",
				"alg": "RS256",
				"kid": keyID,
				"n":   nStr,
				"e":   eStr,
			},
		},
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(jwks)
}

var loginTemplate = template.Must(template.New("login").Parse(`<!DOCTYPE html>
<html>
<head>
	<meta charset="utf-8">
	<title>Authentik - Sign In (Mock)</title>
	<style>
		body { font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif; background: #0f141c; color: #f0f0f0; margin: 0; display: flex; align-items: center; justify-content: center; height: 100vh; }
		.card { background: #192230; padding: 36px; border-radius: 8px; width: 440px; box-shadow: 0 8px 24px rgba(0,0,0,0.4); border: 1px solid #2d3748; }
		.logo { display: flex; align-items: center; margin-bottom: 24px; }
		.logo svg { width: 32px; height: 32px; margin-right: 12px; }
		.logo h2 { margin: 0; color: #fd4b2d; font-size: 24px; font-weight: 700; }
		p.sub { color: #a0aec0; margin-top: -12px; margin-bottom: 24px; font-size: 14px; }
		.btn { width: 100%; padding: 12px; border-radius: 4px; border: none; font-weight: 600; cursor: pointer; font-size: 14px; margin-bottom: 12px; text-align: left; transition: opacity 0.15s; }
		.btn:hover { opacity: 0.9; }
		.btn-admin { background: #fd4b2d; color: #fff; }
		.btn-user { background: #2b6cb0; color: #fff; }
		.btn-title { font-weight: bold; font-size: 15px; }
		.btn-desc { font-size: 12px; opacity: 0.85; margin-top: 2px; }
		.divider { border-top: 1px solid #2d3748; margin: 20px 0; }
	</style>
</head>
<body>
	<div class="card">
		<div class="logo">
			<svg viewBox="0 0 24 24" fill="#fd4b2d">
				<path d="M12 1.5L2.5 7v10l9.5 5.5 9.5-5.5V7L12 1.5zm0 3.2l6.5 3.75v7.1L12 19.3l-6.5-3.75v-7.1L12 4.7zm0 2.5L7.8 15.2h2.2l.9-2.2h2.2l.9 2.2h2.2L12 7.2zm0 3.3l.8 1.9h-1.6l.8-1.9z"/>
			</svg>
			<h2>authentik</h2>
		</div>
		<p class="sub">Mock Identity Provider &bull; Application: Mattermost</p>

		<form method="POST">
			<input type="hidden" name="user_type" value="admin">
			<button class="btn btn-admin" type="submit">
				<div class="btn-title">&#9733; Sign In as Admin (Jane Administrator)</div>
				<div class="btn-desc">Email: admin@example.com &bull; Group: authentik Admins</div>
			</button>
		</form>

		<form method="POST">
			<input type="hidden" name="user_type" value="user">
			<button class="btn btn-user" type="submit">
				<div class="btn-title">&#128100; Sign In as Standard User (John Developer)</div>
				<div class="btn-desc">Email: john.developer@example.com &bull; Group: engineering</div>
			</button>
		</form>

		<form method="POST">
			<input type="hidden" name="user_type" value="demoted">
			<button class="btn btn-user" type="submit" style="border-left-color: #E53E3E;">
				<div class="btn-title">&#9888; Sign In as Demoted Admin (Admin Group Removed)</div>
				<div class="btn-desc">Email: admin@example.com &bull; Group: engineering only</div>
			</button>
		</form>

		<div class="divider"></div>
		<p style="font-size: 12px; color: #718096; text-align: center; margin: 0;">
			Clicking either button will issue an authentic OIDC token and redirect back to Mattermost.
		</p>
	</div>
</body>
</html>`))

func handleAuthorize(w http.ResponseWriter, r *http.Request) {
	redirectURI := r.URL.Query().Get("redirect_uri")
	state := r.URL.Query().Get("state")
	nonce := r.URL.Query().Get("nonce")
	codeChallenge := r.URL.Query().Get("code_challenge")

	if r.Method == http.MethodGet {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_ = loginTemplate.Execute(w, nil)
		return
	}

	if r.Method == http.MethodPost {
		userType := r.FormValue("user_type")

		var user MockUser
		if userType == "admin" {
			user = MockUser{
				Subject:   "authentik-sub-admin-1001",
				Email:     "admin@example.com",
				Username:  "admin",
				FirstName: "Jane",
				LastName:  "Administrator",
				Groups:    []string{"authentik Admins", "engineering"},
			}
		} else if userType == "demoted" {
			user = MockUser{
				Subject:   "authentik-sub-admin-1001",
				Email:     "admin@example.com",
				Username:  "admin",
				FirstName: "Jane",
				LastName:  "Administrator",
				Groups:    []string{"engineering"}, // Admin group removed
			}
		} else {
			user = MockUser{
				Subject:   "authentik-sub-user-2002",
				Email:     "john.developer@example.com",
				Username:  "john.developer",
				FirstName: "John",
				LastName:  "Developer",
				Groups:    []string{"engineering"},
			}
		}

		code := fmt.Sprintf("code-%d", time.Now().UnixNano())

		authCodesLock.Lock()
		authCodes[code] = &AuthSession{
			User:          user,
			Nonce:         nonce,
			CodeChallenge: codeChallenge,
			RedirectURI:   redirectURI,
		}
		authCodesLock.Unlock()

		target := fmt.Sprintf("%s?code=%s&state=%s", redirectURI, code, state)
		http.Redirect(w, r, target, http.StatusFound)
	}
}

func handleToken(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	code := r.FormValue("code")
	codeVerifier := r.FormValue("code_verifier")

	authCodesLock.Lock()
	session, exists := authCodes[code]
	if exists {
		delete(authCodes, code)
	}
	authCodesLock.Unlock()

	if !exists {
		http.Error(w, "Invalid or expired authorization code", http.StatusBadRequest)
		return
	}

	// Verify PKCE if challenge was provided
	if session.CodeChallenge != "" {
		h := sha256.Sum256([]byte(codeVerifier))
		computedChallenge := base64.RawURLEncoding.EncodeToString(h[:])
		if computedChallenge != session.CodeChallenge {
			http.Error(w, "PKCE verification failed", http.StatusBadRequest)
			return
		}
	}

	// Build ID Token JWT
	headerJSON := `{"alg":"RS256","typ":"JWT","kid":"mock-authentik-key-1"}`
	headerB64 := base64.RawURLEncoding.EncodeToString([]byte(headerJSON))

	claims := map[string]interface{}{
		"iss":                "http://localhost:9000/application/o/mattermost",
		"sub":                session.User.Subject,
		"aud":                "mattermost-client-id",
		"exp":                time.Now().Add(1 * time.Hour).Unix(),
		"iat":                time.Now().Unix(),
		"email":              session.User.Email,
		"email_verified":     true,
		"preferred_username": session.User.Username,
		"name":               session.User.FirstName + " " + session.User.LastName,
		"given_name":         session.User.FirstName,
		"family_name":        session.User.LastName,
		"groups":             session.User.Groups,
	}
	if session.Nonce != "" {
		claims["nonce"] = session.Nonce
	}

	claimsBytes, _ := json.Marshal(claims)
	claimsB64 := base64.RawURLEncoding.EncodeToString(claimsBytes)

	signingInput := headerB64 + "." + claimsB64
	hashed := sha256.Sum256([]byte(signingInput))

	signature, err := rsa.SignPKCS1v15(rand.Reader, rsaPrivateKey, crypto.SHA256, hashed[:])
	if err != nil {
		http.Error(w, "Signing failed", http.StatusInternalServerError)
		return
	}

	signatureB64 := base64.RawURLEncoding.EncodeToString(signature)
	idToken := signingInput + "." + signatureB64

	resp := map[string]interface{}{
		"access_token": "mock-access-token-xyz",
		"token_type":   "Bearer",
		"expires_in":   3600,
		"id_token":     idToken,
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func handleUserInfo(w http.ResponseWriter, r *http.Request) {
	// Simple userinfo response
	resp := map[string]interface{}{
		"sub":            "authentik-sub-admin-1001",
		"email":          "admin@example.com",
		"email_verified": true,
		"name":           "Jane Administrator",
		"groups":         []string{"authentik Admins", "engineering"},
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}
