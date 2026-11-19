"""隔離的正式版 server；本機與 BrowserStack 共用案例。"""
import os
from pathlib import Path
import secrets
import shutil
import socket
import subprocess
import time
import urllib.request

import pytest

REPO = Path(__file__).resolve().parents[2]


@pytest.hookimpl(hookwrapper=True)
def pytest_runtest_makereport():
    outcome = yield
    report = outcome.get_result()
    key = os.environ.get("BROWSERSTACK_ACCESS_KEY")
    if key:
        if report.longrepr:
            report.longrepr = str(report.longrepr).replace(key, "[redacted]")
        report.sections = [(name, text.replace(key, "[redacted]")) for name, text in report.sections]


@pytest.fixture(scope="session", autouse=True)
def app(tmp_path_factory):
    root = tmp_path_factory.mktemp("miniapp-browser")
    (root / "work").mkdir()
    (root / "bin").mkdir()
    node = shutil.which("node")
    if not node:
        pytest.fail("需要 Node.js 執行 fake-claude.cjs")
    fake = REPO / "scripts/fake-claude.cjs"
    if os.name == "nt":
        (root / "bin/claude.cmd").write_text(f'@echo off\n"{node}" "{fake}" %*\n')
    else:
        import shlex
        shim = root / "bin/claude"
        shim.write_text(f'#!/bin/sh\nexec {shlex.quote(node)} {shlex.quote(str(fake))} "$@"\n')
        shim.chmod(0o700)
    with socket.socket() as sock:
        sock.bind(("127.0.0.1", 0))
        port = sock.getsockname()[1]
    password = secrets.token_urlsafe(24)
    (root / "config.yaml").write_text(
        f'web:\n  password: "{password}"\nserver:\n  port: {port}\ndb:\n  path: "./app.db"\n',
        encoding="utf-8",
    )
    binary = root / ("server.exe" if os.name == "nt" else "server")
    subprocess.run(
        ["go", "build", "-tags", "production", "-o", str(binary), "./cmd/server"],
        cwd=REPO, check=True, timeout=300,
    )
    # 不把個人 CLI 加入測試 PATH，避免動用真實 agent 帳號。
    env = {**os.environ, "PATH": str(root / "bin") + os.pathsep + os.defpath}
    base = f"http://127.0.0.1:{port}"
    with (root / "server.log").open("w", encoding="utf-8") as log:
        process = subprocess.Popen([str(binary)], cwd=root, env=env, stdout=log, stderr=log)
        processes = [process]
        try:
            deadline = time.monotonic() + 30
            while True:
                if process.poll() is not None:
                    pytest.fail(f"測試 server 提前結束：{root / 'server.log'}")
                try:
                    with urllib.request.urlopen(base + "/", timeout=1) as response:
                        assert response.status == 200
                    break
                except OSError:
                    if time.monotonic() >= deadline:
                        pytest.fail(f"測試 server 啟動逾時：{root / 'server.log'}")
                    time.sleep(0.1)
            if os.environ.get("UI_PROFILE") == "webkit":
                # WebKit's remote HTTP proxy fails for ws://; exercise real HTTPS/WSS instead.
                proxy_binary = root / ("tls-proxy.exe" if os.name == "nt" else "tls-proxy")
                subprocess.run(["go", "build", "-o", str(proxy_binary), str(REPO / "scripts/browser/tls_proxy.go")],
                               cwd=REPO, check=True, timeout=120)
                proxy = subprocess.Popen([str(proxy_binary), base], stdout=subprocess.PIPE, stderr=log, text=True)
                processes.append(proxy)
                base = proxy.stdout.readline().strip()
                assert base.startswith("https://127.0.0.1:"), "TLS fixture failed to start"
            yield {"url": base, "password": password}
        finally:
            for child in reversed(processes):
                child.terminate()
                try:
                    child.wait(timeout=10)
                except subprocess.TimeoutExpired:
                    child.kill()
                    child.wait(timeout=10)


@pytest.fixture
def login(page, app):
    def enter():
        page.goto(app["url"])
        page.get_by_role("textbox", name="密碼").fill(app["password"])
        page.get_by_role("button", name="進入", exact=True).click()
        page.get_by_role("button", name="建立新 Session", exact=True).wait_for(state="visible")
    return enter


@pytest.fixture(autouse=True)
def check_browser_errors(page):
    errors = []
    page.on("pageerror", lambda error: errors.append(str(error)))
    yield
    assert not errors, errors
