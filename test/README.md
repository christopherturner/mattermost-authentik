# Integration Testing Environment

> **Disclaimer:** This is an independent, third-party project developed by Christopher Turner (https://christopherturner.io). It is not affiliated with, sponsored by, or endorsed by Mattermost or Authentik.

This directory provides automated end-to-end integration test suites for the Authentik for Mattermost plugin across two testing modes:
1. **Mock Authentik (Fast & Lightweight):** Standalone Go mock server running in seconds without external dependencies.
2. **Real Authentik Instance (Full Production Stack):** Official Authentik server, worker, Redis, and PostgreSQL running live flows against Mattermost.

Both stacks mount the repository's `./dist` directory directly into the Mattermost container at `/dist:ro`, allowing locally compiled plugin bundles to be installed and tested immediately.

---

## Option A: Mock Authentik (Fast & Lightweight)

Ideal for quick sanity checks, CI pipelines, and rapid development iteration.

### 1. Build the Plugin Bundle
Compile the server binaries, webapp assets, and `.tar.gz` bundle:
```bash
make bundle
```

### 2. Launch the Test Containers
```bash
docker compose -f test/docker-compose.yml up --build -d
```
Wait for Mattermost to become responsive:
```bash
curl --retry 15 --retry-delay 2 --retry-connrefused -s http://localhost:8065/api/v4/system/ping
```

### 3. Deploy the Plugin to the Test Instance
Mattermost containers run with local mode enabled (`MM_SERVICESETTINGS_ENABLELOCALMODE=true`), allowing direct management via `mmctl --local`:

1. **Create a bootstrap System Admin:**
   *(Mattermost's core safety guard prevents demoting the "last System Admin". Creating a separate bootstrap admin ensures lifecycle demotion/promotion tests succeed without conflict.)*
   ```bash
   docker compose -f test/docker-compose.yml exec mattermost \
     mmctl --local user create --email root@example.com --username rootadmin --password "RootPassword123!" --system-admin
   ```

2. **Install the plugin bundle from the mounted `/dist` directory:**
   ```bash
   docker compose -f test/docker-compose.yml exec mattermost \
     mmctl --local plugin add /dist/io.christopherturner.mattermost-authentik-1.1.0.tar.gz
   ```

3. **Apply the mock test configuration and enable the plugin:**
   ```bash
   docker cp test/plugin-config.json test-mattermost-1:/tmp/config.json
   docker compose -f test/docker-compose.yml exec mattermost mmctl --local config patch /tmp/config.json
   docker compose -f test/docker-compose.yml exec mattermost mmctl --local plugin enable io.christopherturner.mattermost-authentik
   ```

### 4. Execute the Test Suite
```bash
# Run full 4-phase lifecycle (admin promotion, demotion, re-promotion, standard user):
python3 test/test-login.py

# Or test a specific persona:
python3 test/test-login.py admin
python3 test/test-login.py demoted
python3 test/test-login.py user
```

### 5. Tear Down
```bash
docker compose -f test/docker-compose.yml down -v
```

---

## Option B: Real Authentik Instance (Complete Production Stack)

Runs against the official `ghcr.io/goauthentik/server:2026.8.4` image with PostgreSQL and Redis. Tests against live Authentik authorization flows, OIDC auto-discovery, PKCE verification, RSA JWKS key signatures, and live group synchronization.

### 1. Build the Plugin Bundle
```bash
make bundle
```

### 2. Launch the Full Stack
```bash
docker compose -f test/docker-compose.real-authentik.yml up -d
```
Wait for Authentik and Mattermost services to finish bootstrapping:
```bash
# Authentik takes ~45-60s on first run to apply Django schema migrations
until curl -sf http://localhost:9000/api/v3/root/config/ > /dev/null && curl -sf http://localhost:8065/api/v4/system/ping > /dev/null; do
  echo "Waiting for Authentik and Mattermost to be ready..."
  sleep 5
done
```

### 3. Deploy the Plugin Bundle
Install the plugin and create the bootstrap admin:
```bash
# 1. Create bootstrap admin
docker compose -f test/docker-compose.real-authentik.yml exec mattermost \
  mmctl --local user create --email root@example.com --username rootadmin --password "RootPassword123!" --system-admin

# 2. Install plugin bundle
docker compose -f test/docker-compose.real-authentik.yml exec mattermost \
  mmctl --local plugin add /dist/io.christopherturner.mattermost-authentik-1.1.0.tar.gz
```
*(Note: `test/test-real-authentik.py` will automatically configure the Authentik provider/application, patch Mattermost's configuration, and enable the plugin during test execution).*

### 4. Execute the End-to-End Test Suite
```bash
python3 test/test-real-authentik.py
```

### 5. Tear Down
```bash
docker compose -f test/docker-compose.real-authentik.yml down -v
```

---

## Rapid Development Iteration (Re-deploying Changes)

When modifying plugin server or webapp code during active development, you do not need to restart the containers:

```bash
# 1. Rebuild bundle
make bundle

# 2. Overwrite plugin in the running Mattermost instance and reload
docker compose -f test/docker-compose.yml exec mattermost \
  mmctl --local plugin add /dist/io.christopherturner.mattermost-authentik-1.1.0.tar.gz
docker compose -f test/docker-compose.yml exec mattermost \
  mmctl --local plugin disable io.christopherturner.mattermost-authentik
docker compose -f test/docker-compose.yml exec mattermost \
  mmctl --local plugin enable io.christopherturner.mattermost-authentik

# 3. View live plugin logs
docker compose -f test/docker-compose.yml logs -f mattermost
```
