#!/usr/bin/env python3
"""
End-to-End Integration Test Suite against a Real Authentik Instance.
Verifies OIDC Discovery, PKCE authorization code exchange, user creation,
session issuance, and the complete 4-phase admin role synchronization lifecycle.

Third-Party Disclaimer:
This is an independent, third-party project developed by Christopher Turner (https://christopherturner.io).
It is not affiliated with, sponsored by, or endorsed by Mattermost or Authentik.
"""

import os
import time
import json
import subprocess
import urllib.request
import urllib.parse
import urllib.error
import http.cookiejar

AUTHENTIK_URL = "http://localhost:9000"
MATTERMOST_URL = "http://localhost:8065"
BOOTSTRAP_TOKEN = "ak-bootstrap-token-1234567890"
CLIENT_ID = "mattermost-real-client-id"
CLIENT_SECRET = "mattermost-real-client-secret"
CALLBACK_URL = f"{MATTERMOST_URL}/plugins/io.christopherturner.mattermost-authentik/oauth2/callback"
ADMIN_GROUP_NAME = "authentik Admins"

def ak_api(endpoint, data=None, method="GET"):
    url = f"{AUTHENTIK_URL}/api/v3{endpoint}"
    headers = {
        "Authorization": f"Bearer {BOOTSTRAP_TOKEN}",
        "Content-Type": "application/json",
        "Accept": "application/json"
    }
    payload = json.dumps(data).encode("utf-8") if data is not None else None
    req = urllib.request.Request(url, data=payload, headers=headers, method=method)
    try:
        with urllib.request.urlopen(req) as resp:
            body = resp.read()
            return json.loads(body.decode("utf-8")) if body else {}
    except urllib.error.HTTPError as e:
        err_msg = e.read().decode("utf-8")
        raise RuntimeError(f"Authentik API {method} {endpoint} failed (HTTP {e.code}): {err_msg}")

def wait_for_services():
    print("Waiting for Authentik and Mattermost services to be ready...")
    for _ in range(60):
        try:
            with urllib.request.urlopen(f"{AUTHENTIK_URL}/api/v3/root/config/"):
                with urllib.request.urlopen(f"{MATTERMOST_URL}/api/v4/system/ping"):
                    print("Services are healthy and responsive.")
                    return
        except Exception:
            time.sleep(2)
    raise TimeoutError("Timed out waiting for services to become ready.")

def setup_authentik():
    print("\n--- Configuring Real Authentik Provider & Application ---")
    
    # 1. Fetch Flows
    flows = ak_api("/flows/instances/")["results"]
    auth_flow = next((f for f in flows if f["slug"] == "default-provider-authorization-implicit-consent"), None)
    inval_flow = next((f for f in flows if f["slug"] == "default-provider-invalidation-flow"), None)
    if not auth_flow or not inval_flow:
        raise RuntimeError("Required Authentik flows not found.")

    # 2. Fetch Signing Key
    keys = ak_api("/crypto/certificatekeypairs/")["results"]
    signing_key = next((k for k in keys if "self-signed" in k["name"].lower()), keys[0] if keys else None)
    if not signing_key:
        raise RuntimeError("No signing certificate keypair found in Authentik.")

    # 3. Fetch Scope Mappings
    mappings = ak_api("/propertymappings/provider/scope/")["results"]
    scope_pks = [m["pk"] for m in mappings if m["scope_name"] in ["openid", "email", "profile"]]

    # 4. Create or Update OAuth2 Provider
    providers = ak_api("/providers/oauth2/")["results"]
    provider = next((p for p in providers if p["client_id"] == CLIENT_ID), None)
    provider_data = {
        "name": "Mattermost Real Provider",
        "client_id": CLIENT_ID,
        "client_secret": CLIENT_SECRET,
        "client_type": "confidential",
        "grant_types": ["authorization_code"],
        "authorization_flow": auth_flow["pk"],
        "invalidation_flow": inval_flow["pk"],
        "signing_key": signing_key["pk"],
        "property_mappings": scope_pks,
        "include_claims_in_id_token": True,
        "redirect_uris": [
            {"matching_mode": "strict", "url": CALLBACK_URL, "redirect_uri_type": "authorization"}
        ]
    }

    if provider:
        provider_pk = provider["pk"]
        ak_api(f"/providers/oauth2/{provider_pk}/", provider_data, method="PATCH")
        print(f"Updated existing OAuth2 Provider (PK: {provider_pk})")
    else:
        created = ak_api("/providers/oauth2/", provider_data, method="POST")
        provider_pk = created["pk"]
        print(f"Created OAuth2 Provider (PK: {provider_pk})")

    # 5. Create or Update Application
    apps = ak_api("/core/applications/")["results"]
    app = next((a for a in apps if a["slug"] == "mattermost"), None)
    if not app:
        ak_api("/core/applications/", {"name": "Mattermost", "slug": "mattermost", "provider": provider_pk}, method="POST")
        print("Created Application 'mattermost'")
    elif app.get("provider") != provider_pk:
        ak_api("/core/applications/mattermost/", {"provider": provider_pk}, method="PATCH")
        print("Updated Application 'mattermost' provider link")

    # 6. Ensure Users and Passwords
    users = ak_api("/core/users/")["results"]
    akadmin = next((u for u in users if u["username"] == "akadmin"), None)
    if akadmin:
        ak_api(f"/core/users/{akadmin['pk']}/set_password/", {"password": "AkAdminPassword123!"}, method="POST")
        print("Configured akadmin password.")

    stduser = next((u for u in users if u["username"] == "standarduser"), None)
    if not stduser:
        created_user = ak_api("/core/users/", {
            "username": "standarduser",
            "name": "Standard User",
            "email": "standarduser@example.com",
            "is_active": True,
            "path": "users",
            "type": "internal"
        }, method="POST")
        ak_api(f"/core/users/{created_user['pk']}/set_password/", {"password": "StandardUserPassword123!"}, method="POST")
        stduser_pk = created_user["pk"]
        print("Created user 'standarduser'.")
    else:
        stduser_pk = stduser["pk"]
        ak_api(f"/core/users/{stduser_pk}/set_password/", {"password": "StandardUserPassword123!"}, method="POST")

    # 7. Find Admin Group PK
    groups = ak_api("/core/groups/")["results"]
    admin_group = next((g for g in groups if g["name"] == ADMIN_GROUP_NAME), None)
    if not admin_group:
        raise RuntimeError(f"Group '{ADMIN_GROUP_NAME}' not found in Authentik.")

    return {
        "admin_user_pk": akadmin["pk"] if akadmin else None,
        "std_user_pk": stduser_pk,
        "admin_group_pk": admin_group["pk"]
    }

def configure_mattermost():
    print("\n--- Applying Plugin Configuration to Mattermost ---")
    config_file = os.path.join(os.path.dirname(__file__), "plugin-real-authentik-config.json")
    subprocess.run(["docker", "cp", config_file, "test-mattermost-1:/tmp/patch.json"], check=True)
    subprocess.run(["docker", "exec", "test-mattermost-1", "mmctl", "--local", "config", "patch", "/tmp/patch.json"], check=True)
    subprocess.run(["docker", "exec", "test-mattermost-1", "mmctl", "--local", "plugin", "disable", "io.christopherturner.mattermost-authentik"], check=True, stdout=subprocess.DEVNULL)
    subprocess.run(["docker", "exec", "test-mattermost-1", "mmctl", "--local", "plugin", "enable", "io.christopherturner.mattermost-authentik"], check=True, stdout=subprocess.DEVNULL)
    time.sleep(1)
    print("Mattermost plugin configured and reloaded.")

class LocalhostCookiePolicy(http.cookiejar.DefaultCookiePolicy):
    def return_ok_secure(self, cookie, request):
        return True

def execute_login(username, password):
    cj = http.cookiejar.CookieJar(LocalhostCookiePolicy())
    class NoRedirect(urllib.request.HTTPRedirectHandler):
        def redirect_request(self, req, fp, code, msg, headers, newurl):
            return None

    opener = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(cj), NoRedirect())

    # Step 1: Connect
    try:
        resp = opener.open(f"{MATTERMOST_URL}/plugins/io.christopherturner.mattermost-authentik/oauth2/connect")
        auth_url = resp.geturl()
    except urllib.error.HTTPError as e:
        auth_url = e.headers.get("Location")
        if not auth_url:
            raise RuntimeError(f"/oauth2/connect failed with HTTP {e.code}: {e.read().decode('utf-8', errors='ignore')}")

    # Step 2: Request authorize from Authentik -> redirects to authentication flow
    try:
        resp = opener.open(auth_url)
        flow_url = resp.geturl()
    except urllib.error.HTTPError as e:
        flow_url = e.headers.get("Location")
        if not flow_url:
            raise RuntimeError(f"Authentik authorize failed with HTTP {e.code}: {e.read().decode('utf-8', errors='ignore')}")

    parsed = urllib.parse.urlparse(flow_url)
    executor_url = f"{AUTHENTIK_URL}/api/v3/flows/executor/default-authentication-flow/?{parsed.query}"
    headers = {"Content-Type": "application/json", "Accept": "application/json"}

    # Stage 1: Identification
    opener.open(executor_url)
    post1 = json.dumps({"uid_field": username}).encode("utf-8")
    req1 = urllib.request.Request(executor_url, data=post1, headers=headers, method="POST")
    try:
        resp1 = opener.open(req1)
        stage2_url = resp1.geturl()
    except urllib.error.HTTPError as e:
        stage2_url = urllib.parse.urljoin(AUTHENTIK_URL, e.headers.get("Location") or "")

    # Stage 2: Password
    post2 = json.dumps({"password": password}).encode("utf-8")
    req2 = urllib.request.Request(stage2_url, data=post2, headers=headers, method="POST")
    try:
        resp2 = opener.open(req2)
        stage3_url = resp2.geturl()
    except urllib.error.HTTPError as e:
        stage3_url = urllib.parse.urljoin(AUTHENTIK_URL, e.headers.get("Location") or "")

    try:
        opener.open(stage3_url)
    except urllib.error.HTTPError as e:
        stage3_url = urllib.parse.urljoin(AUTHENTIK_URL, e.headers.get("Location") or "")
        opener.open(stage3_url)

    # Step 3: Request auth_url with session cookie -> callback code
    try:
        auth_resp = opener.open(auth_url)
        cb_url = auth_resp.geturl()
    except urllib.error.HTTPError as e:
        cb_url = urllib.parse.urljoin(AUTHENTIK_URL, e.headers.get("Location") or "")

    # Step 4: Mattermost callback
    try:
        opener.open(cb_url)
    except urllib.error.HTTPError:
        pass

    cookies: dict[str, str] = {c.name: c.value or "" for c in cj}
    if not cookies.get("MMAUTHTOKEN"):
        raise RuntimeError(f"Authentication failed: MMAUTHTOKEN cookie was not issued for {username}.")

    cookie_header = "; ".join(f"{k}={v}" for k, v in cookies.items())
    opener.addheaders = [("Cookie", cookie_header)]

    user_req = urllib.request.Request(f"{MATTERMOST_URL}/api/v4/users/me")
    user_resp = opener.open(user_req)
    user_data = json.loads(user_resp.read().decode("utf-8"))

    return opener, cookies, user_data

def run_tests():
    wait_for_services()
    meta = setup_authentik()
    configure_mattermost()

    print("\n=======================================================")
    print("Executing End-to-End Test Suite against Real Authentik")
    print("=======================================================")

    # Phase 1: Test Authentik Admin User
    print("\n>>> Phase 1: Authenticating Authentik Admin ('akadmin')")
    opener, cookies, user = execute_login("akadmin", "AkAdminPassword123!")
    roles = user.get("roles", "")
    print(f"   [PASS] User ID:    {user['id']}")
    print(f"   [PASS] Username:   {user['username']}")
    print(f"   [PASS] Roles:      {roles}")
    assert "system_admin" in roles, "Admin user must have system_admin role!"

    # Verify Admin Diagnose Endpoint
    diag_req = urllib.request.Request(
        f"{MATTERMOST_URL}/plugins/io.christopherturner.mattermost-authentik/api/v1/admin/diagnose",
        data=b"{}",
        headers={
            "Content-Type": "application/json",
            "X-Requested-With": "XMLHttpRequest",
            "X-CSRF-Token": cookies.get("MMCSRF", "")
        },
        method="POST"
    )
    diag_resp = opener.open(diag_req)
    diag_data = json.loads(diag_resp.read().decode("utf-8"))
    assert diag_data.get("success"), f"Admin diagnosis failed: {diag_data}"
    print("   [PASS] Live Admin Diagnosis endpoint succeeded:")
    for step in diag_data.get("steps", []):
        lat = f" ({step['latency_ms']}ms)" if "latency_ms" in step else ""
        print(f"          - {step['name']}: {step['status']}{lat} -> {step.get('details', '')}")

    # Phase 2: Test Standard User Isolation
    print("\n>>> Phase 2: Authenticating Standard Non-Admin User ('standarduser')")
    ak_api(f"/core/users/{meta['std_user_pk']}/", {"groups": []}, method="PATCH")
    opener, cookies, user = execute_login("standarduser", "StandardUserPassword123!")
    roles = user.get("roles", "")
    print(f"   [PASS] User ID:    {user['id']}")
    print(f"   [PASS] Roles:      {roles}")
    assert "system_admin" not in roles, "Standard user must not have system_admin role!"

    # Verify Non-Admin Access Rejected (HTTP 403)
    try:
        diag_req = urllib.request.Request(
            f"{MATTERMOST_URL}/plugins/io.christopherturner.mattermost-authentik/api/v1/admin/diagnose",
            data=b"{}",
            headers={
                "Content-Type": "application/json",
                "X-Requested-With": "XMLHttpRequest",
                "X-CSRF-Token": cookies.get("MMCSRF", "")
            },
            method="POST"
        )
        opener.open(diag_req)
        raise AssertionError("Non-admin unexpectedly accessed admin diagnose endpoint!")
    except urllib.error.HTTPError as e:
        assert e.code == 403, f"Expected HTTP 403, got {e.code}"
        print("   [PASS] Non-admin correctly rejected from admin endpoint (HTTP 403).")

    # Phase 3: Dynamic Role Promotion
    print("\n>>> Phase 3: Dynamic Promotion (Adding 'standarduser' to 'authentik Admins')")
    ak_api(f"/core/users/{meta['std_user_pk']}/", {"groups": [meta["admin_group_pk"]]}, method="PATCH")
    _, _, user = execute_login("standarduser", "StandardUserPassword123!")
    roles = user.get("roles", "")
    print(f"   [PASS] Roles after promotion: {roles}")
    assert "system_admin" in roles, "Expected system_admin role after adding to admin group!"

    # Phase 4: Dynamic Role Revocation
    print("\n>>> Phase 4: Dynamic Demotion (Removing 'standarduser' from 'authentik Admins')")
    ak_api(f"/core/users/{meta['std_user_pk']}/", {"groups": []}, method="PATCH")
    _, _, user = execute_login("standarduser", "StandardUserPassword123!")
    roles = user.get("roles", "")
    print(f"   [PASS] Roles after demotion: {roles}")
    assert "system_admin" not in roles, "Expected system_admin to be removed after removal from group!"

    print("\n=======================================================")
    print("ALL TESTS PASSED: 100% verified against real Authentik!")
    print("=======================================================\n")

if __name__ == "__main__":
    run_tests()
