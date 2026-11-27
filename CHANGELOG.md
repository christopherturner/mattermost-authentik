# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [1.3.0] - 2026-11-27

### Added
- OIDC Single Logout (SLO): RP-initiated logout via discovered `end_session_endpoint` (`GET /oauth2/logout`) and OIDC Back-Channel Logout (`POST /oauth2/backchannel-logout`) session revocation (`EnableSLO` setting).
- Auto-Redirect SSO (`AutoRedirectSSO` setting) to automatically redirect browser users from `/login` to Authentik when `EnforceSSO` is active (bypassable via `?no_sso=1`).
- System Console **Authentik Integration URLs** helper (`RedirectURIHelper`) to copy the OAuth2 Callback and Back-Channel Logout URLs, plus `secret: true` masking on `ClientSecret`.
- Per-IP rate limiting on `/oauth2/connect` to protect the KV store from unauthenticated floods.
- Webapp ESLint + Jest unit test suite and automated Mock & Real Authentik E2E integration jobs in CI.

### Changed
- Default `EmailVerified` on newly provisioned accounts to `false` unless explicitly confirmed (`email_verified: true`) by Authentik.
- Tracked `webapp/package-lock.json` for reproducible `npm ci` builds and CI caching.

## [1.2.1] - 2026-11-21

### Security & Dependency Updates
- Upgraded `github.com/mattermost/mattermost/server/public` to `v0.6.1` and `github.com/russellhaering/goxmldsig` to `v1.6.1`, eliminating 74 upstream CVE alerts in dependencies (including CVE-2026-13426 / GHSA-72v2-v53v-4qmm and CVE-2026-33186) and removing the legacy `server/v8` indirect dependency.

### Fixed
- Decoupled GitHub Pages deployment workflow into separate `build` and `deploy` jobs to eliminate artifact indexing race conditions.

### Added
- Added repository security policy in `SECURITY.md` detailing GitHub Private Vulnerability Reporting procedures.
- Updated `support_url` in `plugin.json` to canonical issue tracker endpoint.

## [1.2.0] - 2026-11-19

### Added
- Verified email enforcement on account auto-linking (`RequireEmailVerified` setting, enabled by default) to ensure unverified third-party emails cannot claim existing accounts.
- Server-side SSO enforcement via `UserWillLogIn` plugin hook blocking standard password logins when `EnforceSSO` is active.
- Strict configuration validation in `IsValid()` rejecting non-loopback cleartext HTTP issuer URLs.
- Tested and verified compatibility with Mattermost v12.1.0 and Authentik 2026.8.4.

### Fixed
- Resolved CodeQL security alerts (`go/cookie-httponly-not-set`) by applying literal `HttpOnly: true` on `MMAUTHTOKEN` session cookies and outputting client-accessible non-sensitive cookies (`MMUSERID`, `MMCSRF`) via explicit headers without suppression comments.

## [1.1.1] - 2026-10-31

### Added
- Added 512x512 transparent icon assets (`assets/authentik-icon.svg` and `assets/authentik-icon.png`) featuring the Authentik orange logo per Mattermost Marketplace submission guidelines.
- Added curated screenshots in `assets/screenshots/` showcasing the SSO login experience, System Console configuration, real-time connection diagnostics with dynamic scopes, and plugin management.

### Fixed
- Fixed user login redirection failure by enforcing default team auto-join during OAuth callback for all authenticating users (both newly provisioned and existing users) whenever a default team is configured.
- Corrected session cookie attributes: removed `HttpOnly` from `MMUSERID` and `MMCSRF` so the Mattermost single-page application frontend can access CSRF tokens and active session user ID, and dynamically assigned `Secure` based on Site URL protocol (`https://` vs `http://`) to support local development environments without TLS.
- Removed duplicate icon asset `assets/icon.png` in favor of canonical `assets/authentik-icon.png` and `assets/authentik-icon.svg`.

## [1.1.0] - 2026-10-28

### Security
- Upgraded Go dependencies (`oauth2`, `grpc`, and `go-jose` patch for GHSA-78h2-9frx-2jm8).
- Hardened admin endpoint authorization to use canonical `PermissionManageSystem` checks and differentiated HTTP 401 (unauthenticated) vs. HTTP 403 (insufficient permissions).
- Remediated SSRF vulnerabilities (CWE-918) in the connection diagnostic endpoint by enforcing strict server-side configuration retrieval and host verification for discovery, JWKS, and token endpoints.
- Hardened webapp redirect handling against DOM XSS and unvalidated client-side redirection (CWE-79, CWE-601) via strict origin and protocol parsing.
- Prevented path traversal in custom certificate loading (CWE-22, CWE-73) with normalized path checks and certificate extension whitelisting.
- Removed disabled TLS certificate verification (CWE-295) in diagnostic connectivity checks.
- Eliminated clear-text credential file storage (CWE-312) in integration test tooling by using pre-configured static configuration assets.
- Enforced `Secure` attribute on session and state cookies (CWE-614).
- Validated redirect URIs against authorized callback endpoints in mock Authentik test server (CWE-601).

### Added
- Added project repository homepage, issue tracker, and changelog resource links to README.

### Fixed
- Fixed missing email address and edit locking in native Profile modal for Authentik users.
- Enhanced Security modal: added Authentik SSO details, themed badges with configured brand color, and removed hover effect on non-clickable sign-in row.
- Prevented password field text overflow by removing redundant badge.
- Corrected default scopes description to `openid profile email groups` and removed `offline_access`.
- Corrected installation and configuration instructions in `README.md` to direct users to locate **Authentik for Mattermost** in Plugin Management and the System Console sidebar navigation under **Plugins** > **Authentik for Mattermost**.
- Synchronized configuration settings table in `README.md` with exact display names from `plugin.json` and added entries for `Enforce Authentik SSO`, `Test Connection & Credentials`, and `Manage Legacy LDAP Accounts`.

## [1.0.0] - 2026-10-18

### Added
- **Native OIDC SSO:** Complete OpenID Connect Authorization Code Flow with PKCE (`S256`) and nonce verification, bringing SSO across Team and Enterprise editions.
- **Verified Platform Compatibility:** Empirically tested and verified end-to-end on Mattermost v10.5, v11.11, and v12.0+ with Authentik 2026.8+.
- **Role & Group Synchronization:** Automatic synchronization of Authentik groups (e.g. `authentik Admins`) to Mattermost System Administrator privileges upon sign-in.
- **User Provisioning & Account Linking:** Automatic account creation for new Authentik users and seamless email-based linking for existing local or GitLab accounts.
- **Desktop & Mobile Support:** Popup login support with cross-window `localStorage` synchronization for Mattermost Desktop (Electron) and custom URI callback support (`mmauth://callback`) for mobile.
- **Hardened Security Architecture:** HMAC-SHA256 signed state tokens stored in KV store with strict 10-minute expiry and atomic consumption (`KVCompareAndDelete`), combined with browser-bound state cookies.
- **Admin Tools & Diagnostics:** System Console connection diagnostics and administrative endpoints for unlocking or migrating legacy LDAP accounts.
- **Turnkey Integration Testing:** Multi-stage mock Authentik provider (`test-login.py`) and live Authentik production stack test suite (`test-real-authentik.py`) verified against Authentik 2026.8+.
- **Automated CI/CD:** GitHub Pages documentation pipeline and tag-triggered multi-architecture release workflow.
