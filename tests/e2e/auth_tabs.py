"""Exercise shared-browser session rotation across two real tabs."""
import asyncio
import os
import re
import subprocess

from playwright.async_api import async_playwright

BASE = os.environ.get("QA_BASE_URL", "http://127.0.0.1:8000")
CONTAINER = os.environ.get("QA_CONTAINER", "filebrowser-filebrowser-1")


async def main():
    assert BASE.startswith("http://127.0.0.1:")
    logs = subprocess.check_output(["docker", "logs", CONTAINER], stderr=subprocess.STDOUT).decode()
    password = (open(os.environ["QA_PASSWORD_FILE"]).read().strip()
                if os.environ.get("QA_PASSWORD_FILE") else
                re.search(r"randomly generated password: (\S+)", logs).group(1))
    async with async_playwright() as p:
        browser = await p.chromium.launch(executable_path="/usr/bin/chromium", args=["--no-sandbox"])
        context = await browser.new_context()
        first = await context.new_page()
        await first.goto(BASE)
        await first.get_by_role("textbox", name="Username").fill("admin")
        await first.get_by_role("textbox", name="Password").fill(password)
        await first.get_by_role("button", name="Login", exact=True).click()
        await first.wait_for_url("**/files/**")
        await first.locator("#upload-input").wait_for(state="attached")
        old = await first.evaluate("localStorage.getItem('jwt')")
        second = await context.new_page()
        third = await context.new_page()
        statuses = []
        for page in (second, third):
            page.on("response", lambda r: statuses.append(r.status) if "/api/renew" in r.url else None)
        await asyncio.gather(second.goto(BASE), third.goto(BASE))
        await asyncio.gather(*(page.locator("#upload-input").wait_for(state="attached") for page in (second, third)))
        assert 401 not in statuses, statuses
        current = await first.evaluate("localStorage.getItem('jwt')")
        assert current != old
        r = await context.request.get(BASE + "/api/resources/", headers={"X-Auth": current})
        assert r.status == 200
        print("Two concurrent tab initializations/renewals: PASS", statuses)
        await first.get_by_role("button", name="Logout", exact=True).click()
        await asyncio.gather(*(page.wait_for_url("**/login**") for page in (first, second, third)))
        r = await context.request.get(BASE + "/api/resources/", headers={"X-Auth": current})
        assert r.status == 401
        print("Cross-tab logout propagation + revoked latest JWT: PASS")
        await browser.close()


if __name__ == "__main__":
    asyncio.run(main())
