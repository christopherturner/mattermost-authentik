# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Security
- Upgraded server Go dependencies including `golang.org/x/oauth2` (v0.36.0), `google.golang.org/grpc` (v1.83.2), and patched `github.com/go-jose/go-jose/v4` (v4.1.4) addressing JWE decryption vulnerability GHSA-78h2-9frx-2jm8.

### Fixed
- Fixed default scopes description in the System Console scope selector component to reflect `groups` (`openid profile email groups`).
- Removed unused `offline_access` scope suggestion from the System Console scope selector.

## [1.0.0] - 2026-10-18

### Added
- **OIDC SSO for Team Edition:** Complete OpenID Connect Authorization Code Flow with PKCE (`S256`) and nonce verification, providing SSO without requiring an Enterprise license.
- **Verified Platform Compatibility:** Empirically tested and verified end-to-end on Mattermost v10.5, v11.11, and v12.0+ with Authentik 2026.8+.
- **Role & Group Synchronization:** Automatic synchronization of Authentik groups (e.g. `authentik Admins`) to Mattermost System Administrator privileges upon sign-in.
- **User Provisioning & Account Linking:** Automatic account creation for new Authentik users and seamless email-based linking for existing local or GitLab accounts.
- **Desktop & Mobile Support:** Popup login support with cross-window `localStorage` synchronization for Mattermost Desktop (Electron) and custom URI callback support (`mmauth://callback`) for mobile.
- **Hardened Security Architecture:** HMAC-SHA256 signed state tokens stored in KV store with strict 10-minute expiry and atomic consumption (`KVCompareAndDelete`), combined with browser-bound state cookies.
- **Admin Tools & Diagnostics:** System Console connection diagnostics and administrative endpoints for unlocking or migrating legacy LDAP accounts.
- **Turnkey Integration Testing:** Multi-stage mock Authentik provider (`test-login.py`) and live Authentik production stack test suite (`test-real-authentik.py`) verified against Authentik 2026.8+.
- **Automated CI/CD:** GitHub Pages documentation pipeline and tag-triggered multi-architecture release workflow.
