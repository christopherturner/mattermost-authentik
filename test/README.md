# Integration Testing Environment

This directory contains an automated local testing environment for the Authentik for Mattermost plugin.

## Components
- **Mattermost Team Edition:** Container running `mattermost/mattermost-team-edition:latest` on port `8065`.
- **PostgreSQL:** PostgreSQL 15 container backing Mattermost.
- **Mock Authentik Provider:** Lightweight, standalone OIDC identity provider built directly from source (`mock-authentik/main.go`) exposing port `9000`.
- **Test Runner:** Python 3 script (`test-login.py`) simulating an end-to-end browser OAuth2 authorization code flow with PKCE, state token verification, and Mattermost session creation.

## Running the Tests

1. **Build the plugin bundle:**
   ```bash
   make bundle
   ```

2. **Launch the test containers:**
   ```bash
   docker compose -f test/docker-compose.yml up --build -d
   ```

3. **Execute the end-to-end test:**
   ```bash
   # Test System Admin role synchronization
   python3 test/test-login.py admin

   # Test standard user provisioning
   python3 test/test-login.py user
   ```

4. **Tear down:**
   ```bash
   docker compose -f test/docker-compose.yml down -v
   ```
