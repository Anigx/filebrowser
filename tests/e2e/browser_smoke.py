"""Local-only real-browser acceptance checks; never prints credentials or JWTs."""
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
import tempfile
import uuid

from playwright.sync_api import sync_playwright


BASE = os.environ.get("QA_BASE_URL", "http://127.0.0.1:8000")
CONTAINER = os.environ.get("QA_CONTAINER", "filebrowser-filebrowser-1")
assert BASE.startswith("http://127.0.0.1:"), "Only loopback testing permitted"


def main():
    logs = subprocess.check_output(
        ["docker", "logs", CONTAINER], stderr=subprocess.STDOUT
    ).decode()
    password = (open(os.environ["QA_PASSWORD_FILE"]).read().strip()
                if os.environ.get("QA_PASSWORD_FILE") else
                re.search(r"randomly generated password: (\S+)", logs).group(1))
    with sync_playwright() as p:
        browser = p.chromium.launch(
            executable_path="/usr/bin/chromium", args=["--no-sandbox"]
        )
        context = browser.new_context()
        page = context.new_page()
        errors = []
        page.on("pageerror", lambda error: errors.append(str(error)))
        page.goto(BASE)
        page.get_by_role("textbox", name="Username").fill("admin")
        page.get_by_role("textbox", name="Password").fill(password)
        page.get_by_role("button", name="Login", exact=True).click()
        page.wait_for_url("**/files/**")
        page.locator("#upload-input").wait_for(state="attached")
        print("Browser login and file listing: PASS")
        token = page.evaluate("localStorage.getItem('jwt')")
        assert token
        headers = {"X-Auth": token}
        name = "qa-browser-" + uuid.uuid4().hex + ".bin"
        patches = []
        rotated = []
        if os.environ.get("QA_ROTATE_DURING_UPLOAD") == "1":
            other = context.new_page()

            def rotate_after_first_chunk(route):
                if route.request.method == "PATCH" and not rotated:
                    rotated.append(True)
                    result = route.fetch()
                    assert result.status == 204
                    other.goto(BASE)
                    other.locator("#upload-input").wait_for(state="attached")
                    route.fulfill(response=result)
                else:
                    route.continue_()

            page.route("**/api/tus/**", rotate_after_first_chunk)
        page.on("request", lambda r: patches.append(int(r.all_headers().get("content-length", "0")) or len(r.post_data_buffer or b""))
                if r.method == "PATCH" and "/api/tus/" in r.url else None)
        try:
            with tempfile.TemporaryDirectory(prefix="fb-browser-qa-") as tmp:
                path = Path(tmp) / name
                block = bytes(range(256)) * 40960
                digest = hashlib.sha256()
                with path.open("wb") as f:
                    for _ in range(11):
                        f.write(block)
                        digest.update(block)
                page.locator("#upload-input").set_input_files(str(path))
                page.get_by_text(name, exact=True).first.wait_for(timeout=180000)
                token = page.evaluate("localStorage.getItem('jwt')")
                headers = {"X-Auth": token}
                response = context.request.get(BASE + "/api/raw/" + name, headers=headers)
                assert response.status == 200, response.status
                assert hashlib.sha256(response.body()).digest() == digest.digest()
                assert len(patches) >= 2, patches
                assert all(0 < size <= 95 * 1024 * 1024 for size in patches), patches
                print("Browser 110 MiB upload, request bounds, download SHA256: PASS", patches)
        finally:
            response = context.request.delete(BASE + "/api/resources/" + name, headers=headers)
            assert response.status in (204, 200, 404), response.status
        output_dir = Path(os.environ.get("QA_OUTPUT_DIR", "/tmp/filebrowser-qa"))
        output_dir.mkdir(parents=True, exist_ok=True)
        page.screenshot(path=str(output_dir / "authenticated.png"), full_page=True)
        page.get_by_role("button", name="Logout", exact=True).click()
        page.wait_for_url("**/login**")
        response = context.request.get(BASE + "/api/resources/", headers=headers)
        assert response.status == 401, f"UI logout leaves JWT usable: {response.status}"
        print("Browser logout revokes copied JWT: PASS")
        assert not errors, errors
        print("Browser JS errors: NONE")
        browser.close()


if __name__ == "__main__":
    main()
