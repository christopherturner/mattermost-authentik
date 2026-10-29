#!/usr/bin/env python3
"""
Automated End-to-End Test for Mattermost Authentik Authentication Plugin.
Simulates a full browser login session through the OAuth2 / OIDC code flow.
"""

import sys
import json
import urllib.request
import urllib.parse
import urllib.error
import http.cookiejar

class LocalhostCookiePolicy(http.cookiejar.DefaultCookiePolicy):
    def return_ok_secure(self, cookie, request):
        return True

def run_test(user_type="admin"):
    print("\n=======================================================")
    print(f"Testing Login for user type: {user_type}")
    print("=======================================================")

    cj = http.cookiejar.CookieJar(LocalhostCookiePolicy())
    opener = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(cj))

    # 1. Connect
    print("1. Requesting /oauth2/connect from Mattermost plugin...")
    req = urllib.request.Request("http://localhost:8065/plugins/io.christopherturner.mattermost-authentik/oauth2/connect")
    auth_url = ""
    try:
        resp = opener.open(req)
        auth_url = resp.geturl()
    except urllib.error.HTTPError as e:
        auth_url = e.headers.get("Location") or ""

    if not auth_url:
        print("FAILED: Did not receive authorization redirect URL.")
        sys.exit(1)

    print(f"   -> Redirected to Authentik: {auth_url[:70]}...")

    # 2. Authorize in Authentik
    print("2. Authorizing in Authentik...")
    data = urllib.parse.urlencode({"user_type": user_type}).encode("utf-8")
    post_req = urllib.request.Request(auth_url, data=data, method="POST")
    cb_url = ""
    try:
        resp = opener.open(post_req)
        cb_url = resp.geturl()
    except urllib.error.HTTPError as e:
        cb_url = e.headers.get("Location") or ""

    if not cb_url:
        print("FAILED: Did not receive callback redirect URL.")
        sys.exit(1)

    print(f"   -> Redirected back to callback: {cb_url[:70]}...")

    # 3. Callback
    print("3. Executing callback in Mattermost plugin...")
    cb_req = urllib.request.Request(cb_url)
    opener.open(cb_req)

    # 4. Check cookies
    cookies: dict[str, str] = {c.name: c.value or "" for c in cj}
    token = cookies.get("MMAUTHTOKEN")
    if not token:
        print("FAILED: MMAUTHTOKEN cookie not issued.")
        sys.exit(1)

    print(f"   -> Received MMAUTHTOKEN: {token[:16]}...")

    cookie_header = "; ".join(f"{k}={v}" for k, v in cookies.items())
    opener.addheaders = [("Cookie", cookie_header)]

    # 5. Verify /api/v4/users/me
    print("4. Fetching authenticated user profile from /api/v4/users/me...")
    user_req = urllib.request.Request("http://localhost:8065/api/v4/users/me")
    user_resp = opener.open(user_req)
    user_data = json.loads(user_resp.read().decode("utf-8"))
    roles = user_data.get('roles', '')

    print("   [SUCCESS] User authenticated successfully:")
    print(f"   - User ID:  {user_data.get('id')}")
    print(f"   - Username: {user_data.get('username')}")
    print(f"   - Email:    {user_data.get('email')}")
    print(f"   - Roles:    {roles}")
    print(f"   - Service:  {user_data.get('auth_service')}")

    # 6. Verify Admin Privileges & Endpoints
    print("5. Verifying Admin authorization & endpoints...")
    csrf_token = cookies.get("MMCSRF", "")
    is_admin = "system_admin" in roles

    if user_type == "admin":
        if not is_admin:
            print("   [FAILED] Expected system_admin role for admin user!")
            sys.exit(1)
        print("   [PASS] User correctly granted system_admin role.")

        # Test Admin Diagnose endpoint
        diag_req = urllib.request.Request(
            "http://localhost:8065/plugins/io.christopherturner.mattermost-authentik/api/v1/admin/diagnose",
            data=b"{}",
            headers={
                "Content-Type": "application/json",
                "X-Requested-With": "XMLHttpRequest",
                "X-CSRF-Token": csrf_token
            },
            method="POST"
        )
        diag_resp = opener.open(diag_req)
        diag_body = json.loads(diag_resp.read().decode("utf-8"))
        if not diag_body.get("success"):
            print("   [FAILED] Admin diagnosis returned failure:", diag_body)
            sys.exit(1)
        print("   [PASS] Admin diagnose endpoint returned 200 OK with all checks passed.")

        # Test Admin LDAP Users endpoint
        ldap_req = urllib.request.Request("http://localhost:8065/plugins/io.christopherturner.mattermost-authentik/api/v1/admin/ldap-users")
        opener.open(ldap_req)
        print("   [PASS] Admin LDAP endpoint returned 200 OK.")

    else:
        if is_admin:
            print("   [FAILED] Standard user unexpectedly has system_admin role!")
            sys.exit(1)
        print("   [PASS] User correctly restricted to non-admin roles.")

        # Verify non-admin is blocked from diagnose (HTTP 403)
        try:
            diag_req = urllib.request.Request(
                "http://localhost:8065/plugins/io.christopherturner.mattermost-authentik/api/v1/admin/diagnose",
                data=b"{}",
                headers={
                    "Content-Type": "application/json",
                    "X-Requested-With": "XMLHttpRequest",
                    "X-CSRF-Token": csrf_token
                },
                method="POST"
            )
            opener.open(diag_req)
            print("   [FAILED] Non-admin unexpectedly accessed admin diagnose endpoint!")
            sys.exit(1)
        except urllib.error.HTTPError as e:
            if e.code == 403:
                print("   [PASS] Non-admin correctly rejected from admin diagnose endpoint (HTTP 403).")
            else:
                print(f"   [FAILED] Unexpected status code {e.code}")
                sys.exit(1)

        # Verify non-admin is blocked from ldap-users (HTTP 403)
        try:
            ldap_req = urllib.request.Request("http://localhost:8065/plugins/io.christopherturner.mattermost-authentik/api/v1/admin/ldap-users")
            opener.open(ldap_req)
            print("   [FAILED] Non-admin unexpectedly accessed admin LDAP endpoint!")
            sys.exit(1)
        except urllib.error.HTTPError as e:
            if e.code == 403:
                print("   [PASS] Non-admin correctly rejected from admin LDAP endpoint (HTTP 403).")
            else:
                print(f"   [FAILED] Unexpected status code {e.code}")
                sys.exit(1)

if __name__ == "__main__":
    user_arg = sys.argv[1] if len(sys.argv) > 1 else "all"
    if user_arg == "admin":
        run_test("admin")
    elif user_arg == "user":
        run_test("user")
    elif user_arg == "demoted":
        run_test("demoted")
    else:
        # Full lifecycle: promote -> demote -> re-promote -> standard user
        print("\n>>> Phase 1: Test Admin Promotion & Endpoint Privileges")
        run_test("admin")
        print("\n>>> Phase 2: Test Admin Revocation & Privilege Stripping")
        run_test("demoted")
        print("\n>>> Phase 3: Test Admin Re-Promotion")
        run_test("admin")
        print("\n>>> Phase 4: Test Standard Non-Admin User")
        run_test("user")
    print("\nAll end-to-end authentication & authorization tests passed successfully!\n")
