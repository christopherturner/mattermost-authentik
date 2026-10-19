#!/usr/bin/env python3
"""
Automated End-to-End Test for Mattermost Authentik Authentication Plugin.
Simulates a full browser login session through the OAuth2 / OIDC code flow.
"""

import sys
import json
import urllib.request
import urllib.parse
import http.cookiejar

def run_test(user_type="admin"):
    print(f"\n=======================================================")
    print(f"Testing Login for user type: {user_type}")
    print(f"=======================================================")

    cj = http.cookiejar.CookieJar()
    opener = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(cj))

    # 1. Connect
    print("1. Requesting /oauth2/connect from Mattermost plugin...")
    req = urllib.request.Request("http://localhost:8065/plugins/io.christopherturner.mattermost-authentik/oauth2/connect")
    try:
        resp = opener.open(req)
        auth_url = resp.geturl()
    except urllib.error.HTTPError as e:
        auth_url = e.headers.get("Location")

    print(f"   -> Redirected to Authentik: {auth_url[:70]}...")

    # 2. Authorize in Authentik
    print("2. Authorizing in Authentik...")
    data = urllib.parse.urlencode({"user_type": user_type}).encode("utf-8")
    post_req = urllib.request.Request(auth_url, data=data, method="POST")
    try:
        resp = opener.open(post_req)
        cb_url = resp.geturl()
    except urllib.error.HTTPError as e:
        cb_url = e.headers.get("Location")

    print(f"   -> Redirected back to callback: {cb_url[:70]}...")

    # 3. Callback
    print("3. Executing callback in Mattermost plugin...")
    cb_req = urllib.request.Request(cb_url)
    opener.open(cb_req)

    # 4. Check cookies
    cookies = {c.name: c.value for c in cj}
    if "MMAUTHTOKEN" not in cookies:
        print("FAILED: MMAUTHTOKEN cookie not issued.")
        sys.exit(1)

    print(f"   -> Received MMAUTHTOKEN: {cookies['MMAUTHTOKEN'][:16]}...")

    # 5. Verify /api/v4/users/me
    print("4. Fetching authenticated user profile from /api/v4/users/me...")
    user_req = urllib.request.Request("http://localhost:8065/api/v4/users/me")
    user_resp = opener.open(user_req)
    user_data = json.loads(user_resp.read().decode("utf-8"))

    print(f"   [SUCCESS] User authenticated successfully:")
    print(f"   - User ID:  {user_data.get('id')}")
    print(f"   - Username: {user_data.get('username')}")
    print(f"   - Email:    {user_data.get('email')}")
    print(f"   - Roles:    {user_data.get('roles')}")
    print(f"   - Service:  {user_data.get('auth_service')}")

if __name__ == "__main__":
    run_test("admin")
    run_test("user")
    print("\nAll end-to-end authentication tests passed successfully!\n")
