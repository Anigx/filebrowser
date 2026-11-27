"""Exercise a disposable File Browser with Chromium's virtual FIDO2 authenticator."""

import json
import os
from pathlib import Path
import secrets
import shutil
import socket
import subprocess
import tempfile
import time
import urllib.request

from playwright.sync_api import expect, sync_playwright


def wait_for_health(url, process):
    for _ in range(100):
        if process.poll() is not None:
            raise RuntimeError("Disposable File Browser exited")
        try:
            with urllib.request.urlopen(url, timeout=1) as response:
                if response.status == 200:
                    return
        except OSError:
            time.sleep(0.1)
    raise RuntimeError("Disposable File Browser did not start")


def run():
    binary = str(Path(os.environ["QA_FILEBROWSER_BIN"]).resolve(strict=True))
    admin_password, user_password, new_password = (secrets.token_urlsafe(24) for _ in range(3))
    with tempfile.TemporaryDirectory() as directory:
        tmp = Path(directory)
        root = tmp / "files"
        root.mkdir()
        (root / "test.txt").write_text("virtual key acceptance")
        with socket.socket() as sock:
            sock.bind(("127.0.0.1", 0))
            port = sock.getsockname()[1]
        origin = f"http://localhost:{port}"
        prefix = os.environ.get("QA_BASE_PATH", "/qa").rstrip("/")
        base = origin + prefix
        database = str(tmp / "db")
        opt = [binary, "--database", database]

        def cli(*args):
            # These generated fixture credentials are not real user passwords.
            result = subprocess.run([*opt, *args], cwd=tmp, capture_output=True, check=False)
            if result.returncode:
                raise RuntimeError(f"Fixture command failed: {args[:2]}")

        cli("config", "init")
        cli("users", "add", "--perm.admin", "--", "fixture-admin", admin_password)
        cli("users", "add", "--", "fixture-user", user_password)

        def start(*, configure_origin=False):
            env = os.environ.copy()
            if configure_origin and not env.get("QA_WEBAUTHN_BASELINE"):
                env["FB_WEBAUTHN_ORIGIN"] = origin
            else:
                env.pop("FB_WEBAUTHN_ORIGIN", None)
            proc = subprocess.Popen([*opt, "--root", str(root), "--address", "127.0.0.1",
                                     "--port", str(port), "--baseURL", prefix],
                                    cwd=tmp, env=env, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
            wait_for_health(base + "/health", proc)
            return proc

        def stop(proc):
            proc.terminate()
            proc.wait(timeout=10)

        process = start(configure_origin=True)
        try:
            with sync_playwright() as playwright:
                browser_path = os.environ.get("QA_CHROMIUM")
                kwargs = {"executable_path": browser_path} if browser_path else {}
                browser = playwright.chromium.launch(**kwargs)
                try:
                    user = browser.new_page(locale="en-US")
                    admin = browser.new_page(locale="en-US")
                    cdp = user.context.new_cdp_session(user)
                    cdp.send("WebAuthn.enable")
                    authenticator = {"protocol": "ctap2", "transport": "usb",
                                     "hasResidentKey": False, "hasUserVerification": True,
                                     "isUserVerified": True, "automaticPresenceSimulation": True}
                    primary = cdp.send("WebAuthn.addVirtualAuthenticator", {"options": authenticator})["authenticatorId"]

                    def login(page, name, password=None):
                        page.goto(base + "/login")
                        page.get_by_placeholder("Username", exact=True).fill(name)
                        if password is None:
                            page.get_by_role("button", name="Sign in with a security key").click()
                        else:
                            page.locator("input[type=password]").fill(password)
                            page.locator("input[type=submit]").click()
                        page.wait_for_url("**/files/**")

                    def add_key(page, name, password=None):
                        page.goto(base + "/settings/profile")
                        page.get_by_label("Key name").fill(name)
                        if password:
                            section = page.locator("section").filter(has=page.get_by_role("heading", name="Security keys"))
                            section.get_by_label("Your Current Password", exact=True).fill(password)
                        statuses = []
                        def record(response):
                            if "/api/webauthn/" in response.url:
                                statuses.append((response.url.rsplit("/api/webauthn/", 1)[1], response.status))
                        page.on("response", record)
                        try:
                            page.get_by_role("button", name="Register a security key").click()
                            expect(page.get_by_role("button", name="Sign in with a security key")).to_be_visible(timeout=15000)
                        except AssertionError as err:
                            raise AssertionError(f"Security-key registration did not complete: {statuses}") from err
                        finally:
                            page.remove_listener("response", record)

                    login(user, "fixture-user", user_password)
                    add_key(user, "Primary key", user_password)
                    password_body = {"username": "fixture-user", "password": user_password}
                    assert user.request.post(base + "/api/login", data=password_body).status == 403

                    login(user, "fixture-user")
                    # The first key confirms backup enrollment. Swap devices before
                    # the new registration options reach navigator.credentials.create.
                    def use_backup(route):
                        cdp.send("WebAuthn.removeVirtualAuthenticator", {"authenticatorId": primary})
                        cdp.send("WebAuthn.addVirtualAuthenticator", {"options": authenticator})
                        route.continue_()

                    user.route("**/api/webauthn/register/start", use_backup)
                    add_key(user, "Backup key")
                    user.unroute("**/api/webauthn/register/start", use_backup)
                    stop(process)
                    # Cold-copy the database to a new file and open only the copy.
                    restored = str(tmp / "restored.db")
                    shutil.copyfile(database, restored)
                    opt[-1] = restored
                    process = start()
                    login(user, "fixture-user")
                    user.goto(base + "/settings/profile")
                    expect(user.get_by_role("listitem").filter(has_text="Primary key")).to_be_visible()
                    expect(user.get_by_role("listitem").filter(has_text="Backup key")).to_be_visible()

                    login(admin, "fixture-admin", admin_password)
                    admin_token = admin.evaluate("localStorage.getItem('jwt')")
                    lock = admin.request.put(base + "/api/users/2", headers={"X-Auth": admin_token}, data={
                        "what": "user", "which": ["lockPassword"], "current_password": admin_password,
                        "data": {"id": 2, "lockPassword": True},
                    })
                    assert lock.ok
                    admin.goto(base + "/settings/users/2")
                    expect(admin.locator("#password")).to_have_count(0)
                    row = admin.get_by_role("listitem").filter(has_text="Primary key")
                    row.get_by_role("button", name="Remove key").click()
                    admin.get_by_label("Your Current Password", exact=True).fill(admin_password)
                    admin.locator("section").get_by_role("button", name="Remove key", exact=True).last.click()
                    expect(row).to_have_count(0)
                    assert user.request.post(base + "/api/login", data=password_body).status == 403

                    admin_cdp = admin.context.new_cdp_session(admin)
                    admin_cdp.send("WebAuthn.enable")
                    admin_cdp.send("WebAuthn.addVirtualAuthenticator", {"options": authenticator})
                    add_key(admin, "Admin key", admin_password)
                    login(admin, "fixture-admin")
                    user.evaluate("localStorage.removeItem('jwt')")
                    login(user, "fixture-user")
                    stale_token = user.evaluate("localStorage.getItem('jwt')")
                    admin.goto(base + "/settings/users/2")
                    admin.get_by_role("button", name="Reset security keys").click()
                    expect(admin.get_by_label("Your Current Password", exact=True)).to_have_count(0)
                    admin.get_by_label("Your new password", exact=True).fill(new_password)
                    admin.get_by_label("Confirm your new password", exact=True).fill(new_password)
                    admin.get_by_role("button", name="Set new password and remove keys").click()
                    expect(admin.get_by_text("No security keys are registered.")).to_be_visible()
                    assert user.request.get(base + "/api/users/2", headers={"X-Auth": stale_token}).status == 401
                    assert user.request.post(base + "/api/login", data=password_body).status == 403
                    assert user.request.post(base + "/api/login", data={
                        "username": "fixture-user", "password": new_password
                    }).ok
                    print(json.dumps({"verified": "WebAuthn registration, key login, restart, backup and admin reset",
                                      "basePath": prefix or "/"}))
                finally:
                    browser.close()
        finally:
            if process.poll() is None:
                stop(process)


if __name__ == "__main__":
    run()
