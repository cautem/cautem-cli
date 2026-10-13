#!/usr/bin/env python3
"""Start isolated Gateway and console processes, then run the Playwright smoke."""

from __future__ import annotations

import os
from pathlib import Path
import socket
import subprocess
import sys
import tempfile
import time
from urllib.error import URLError
from urllib.request import urlopen

from playwright.sync_api import sync_playwright


def free_port() -> int:
    with socket.socket() as listener:
        listener.bind(("127.0.0.1", 0))
        return listener.getsockname()[1]


def wait_http(url: str, process: subprocess.Popen[bytes]) -> None:
    deadline = time.monotonic() + 90
    while time.monotonic() < deadline:
        if process.poll() is not None:
            raise RuntimeError(f"server exited before readiness: {process.args}")
        try:
            with urlopen(url, timeout=1):
                return
        except (OSError, URLError):
            time.sleep(0.2)
    raise TimeoutError(f"server did not become ready: {url}")


def main() -> None:
    root = Path(__file__).resolve().parents[4]
    gateway_port, console_port = free_port(), free_port()
    environment = os.environ.copy()
    environment["GOWORK"] = str(root / "go.work")
    processes: list[subprocess.Popen[bytes]] = []
    with tempfile.TemporaryDirectory(prefix="cautem-console-e2e-") as data_dir:
        try:
            gateway = subprocess.Popen(
                [
                    "go", "-C", str(root / "cautem-gateway"), "run", "./cmd/cautem-gateway",
                    "--listen", f"127.0.0.1:{gateway_port}", "--data-dir", str(Path(data_dir) / "gateway"), "--disable-tls",
                ],
                cwd=root,
                env=environment,
            )
            processes.append(gateway)
            console = subprocess.Popen(
                [
                    "go", "-C", str(root / "cautem-cli"), "run", "./cmd/cautem-console",
                    "--listen", f"127.0.0.1:{console_port}", "--gateway", f"http://127.0.0.1:{gateway_port}",
                ],
                cwd=root,
                env=environment,
            )
            processes.append(console)
            wait_http(f"http://127.0.0.1:{gateway_port}/v1/auth/login", gateway)
            wait_http(f"http://127.0.0.1:{console_port}/healthz", console)
            environment["CAUTEM_CONSOLE_E2E_URL"] = f"http://127.0.0.1:{console_port}"
            with sync_playwright() as playwright:
                channel = environment.get("CAUTEM_PLAYWRIGHT_CHANNEL", "").strip() or None
                browser = playwright.chromium.launch(channel=channel, headless=True)
                context = browser.new_context()
                page = context.new_page()
                errors: list[str] = []
                logout_set_cookie: list[str] = []
                signed_out_request_had_session_cookie: list[bool] = []
                page.on("pageerror", lambda error: errors.append(str(error)))
                page.on(
                    "response",
                    lambda response: logout_set_cookie.append(response.all_headers().get("set-cookie", ""))
                    if response.url.endswith("/logout")
                    else None,
                )
                page.on(
                    "request",
                    lambda request: signed_out_request_had_session_cookie.append(
                        "cautem_console_session=" in request.all_headers().get("cookie", "")
                    )
                    if request.url.endswith("/signed-out")
                    else None,
                )
                response = page.goto(environment["CAUTEM_CONSOLE_E2E_URL"], wait_until="networkidle")
                assert response is not None and response.status == 200
                assert page.title() == "cautem Console"
                assert page.get_by_role("heading", name="Sandboxes").is_visible()
                assert page.get_by_text("No sandboxes").is_visible()
                assert page.get_by_text("local-dev").is_visible()
                headers = response.all_headers()
                assert "script-src 'none'" in headers.get("content-security-policy", "")
                css = page.request.get(environment["CAUTEM_CONSOLE_E2E_URL"] + "/assets/console.css")
                assert css.status == 200 and "color-scheme:dark" in css.text()
                cookies = {item["name"]: item for item in context.cookies()}
                session = cookies.get("cautem_console_session")
                assert session is not None and session["httpOnly"] and session["sameSite"] == "Lax"
                original_session_id = session["value"]
                page.get_by_role("button", name="Sign out").click()
                signed_out_heading = page.get_by_role("heading", name="Signed out")
                try:
                    signed_out_heading.wait_for(state="visible", timeout=3000)
                except Exception as error:
                    raise AssertionError(f"logout did not render signed-out page: url={page.url} title={page.title()} html={page.content()[:1200]}") from error
                assert any(
                    "cautem_console_session=;" in header and "Max-Age=0" in header
                    for header in logout_set_cookie
                ), f"logout did not expire the session cookie: {logout_set_cookie}"
                assert signed_out_request_had_session_cookie == [False], (
                    "Chrome sent the expired session cookie after logout: "
                    f"{signed_out_request_had_session_cookie}"
                )
                request = playwright.request.new_context()
                try:
                    revoked = request.post(
                        environment["CAUTEM_CONSOLE_E2E_URL"] + "/logout",
                        headers={"Cookie": f"cautem_console_session={original_session_id}"},
                    )
                finally:
                    request.dispose()
                assert revoked.status == 401, f"logout did not revoke its session: status={revoked.status}"

                csrf_context = browser.new_context()
                csrf_page = csrf_context.new_page()
                csrf_page.goto(environment["CAUTEM_CONSOLE_E2E_URL"], wait_until="networkidle")
                csrf = csrf_page.locator('form[action="/logout"] input[name="csrf"]').input_value()
                rejected = csrf_page.request.post(
                    environment["CAUTEM_CONSOLE_E2E_URL"] + "/logout", form={"csrf": csrf}
                )
                assert rejected.status == 403, f"missing Origin should be rejected, got {rejected.status}"
                csrf_context.close()
                assert not errors, f"browser page errors: {errors}"
                browser.close()
        finally:
            for process in reversed(processes):
                process.terminate()
            for process in reversed(processes):
                try:
                    process.wait(timeout=5)
                except subprocess.TimeoutExpired:
                    process.kill()
                    process.wait()

    print("console browser E2E: PASS")


if __name__ == "__main__":
    try:
        main()
    except Exception as error:
        print(f"console browser E2E: FAIL: {error}", file=sys.stderr)
        raise
