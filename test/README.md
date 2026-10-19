# Integration Testing Environment

> **Disclaimer:** This is an independent, third-party project developed by Christopher Turner (https://christopherturner.io). It is not affiliated with, sponsored by, or endorsed by Mattermost or Authentik.

This directory provides automated end-to-end integration test suites for the Authentik for Mattermost plugin across two testing modes:
1. **Mock Authentik (Fast & Lightweight):** Standalone Go mock server running in seconds without external dependencies.
2. **Real Authentik Instance (Full Production Stack):** Official Authentik server, worker, Redis, and PostgreSQL running live flows against Mattermost.

---

## Option A: Mock Authentik (Fast)

Ideal for quick sanity checks and rapid development iteration.

1. **Build the plugin bundle:**
   ```bash
   make bundle
   ```

2. **Launch the mock stack:**
   ```bash
   docker compose -f test/docker-compose.yml up --build -d
   ```

3. **Execute the test suite:**
   ```bash
   # Run full 4-phase lifecycle (admin, demotion, re-promotion, non-admin)
   python3 test/test-login.py

   # Or test specific persona:
   python3 test/test-login.py admin
   python3 test/test-login.py user
   ```

4. **Tear down:**
   ```bash
   docker compose -f test/docker-compose.yml down -v
   ```

---

## Option B: Real Authentik Instance (Complete E2E Stack)

Runs against the official `ghcr.io/goauthentik/server` image. Automatically bootstraps an OAuth2/OIDC provider, provisions test users, executes multi-stage authentication flows, verifies RSA JWKS signatures, and exercises the 4-phase admin role synchronization lifecycle.

1. **Build the plugin bundle:**
   ```bash
   make bundle
   ```

2. **Launch the real Authentik stack:**
   ```bash
   docker compose -f test/docker-compose.real-authentik.yml up -d
   ```

3. **Execute the end-to-end test suite:**
   ```bash
   python3 test/test-real-authentik.py
   ```

4. **Tear down:**
   ```bash
   docker compose -f test/docker-compose.real-authentik.yml down -v
   ```
