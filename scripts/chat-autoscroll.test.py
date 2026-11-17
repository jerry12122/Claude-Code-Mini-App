"""Chat auto-scroll against the real server + frontend: python scripts/chat-autoscroll.test.py
Needs Go, Node, Playwright and Edge. A fake `claude` (scripts/fake-claude.cjs) streams long output and then
asks for permission; the real app must keep following, show the permission panel, and still honor scroll-up."""
import json
import os
import socket
import subprocess
import tempfile
import time
import urllib.request
from pathlib import Path

from playwright.sync_api import expect, sync_playwright

repo = Path(__file__).resolve().parents[1]
PASSWORD = "verify-pass-12345"
SCROLLER = "[style*='overflow-anchor']"  # the chat message scroller
with socket.socket() as s:
    s.bind(("127.0.0.1", 0))
    port = s.getsockname()[1]
base = f"http://127.0.0.1:{port}"


def api(path, body=None, token=None):
    headers = {"Content-Type": "application/json", **({"Authorization": "Bearer " + token} if token else {})}
    req = urllib.request.Request(base + path, data=json.dumps(body).encode() if body is not None else None, headers=headers)
    return json.load(urllib.request.urlopen(req))


with tempfile.TemporaryDirectory(prefix="autoscroll-", ignore_cleanup_errors=True) as tmp, sync_playwright() as p:
    tmp = Path(tmp)
    (tmp / "work").mkdir()
    (tmp / "bin").mkdir()
    (tmp / "bin" / "claude.cmd").write_text(f'@echo off\r\nnode "{repo / "scripts/fake-claude.cjs"}" %*\r\n')
    (tmp / "config.yaml").write_text(f'web:\n  password: "{PASSWORD}"\nserver:\n  port: {port}\ndb:\n  path: "./app.db"\n')
    subprocess.run(["go", "build", "-tags", "production", "-o", str(tmp / "server.exe"), "./cmd/server"], cwd=repo, check=True)
    server = subprocess.Popen([str(tmp / "server.exe")], cwd=tmp, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL,
                              env={**os.environ, "PATH": str(tmp / "bin") + ";" + os.environ["PATH"]})
    browser = None
    try:
        for _ in range(100):
            try:
                urllib.request.urlopen(base + "/")
                break
            except OSError:
                time.sleep(0.1)
        token = api("/auth/login", {"password": PASSWORD})["token"]
        browser = p.chromium.launch(channel="msedge", headless=True)

        def open_chat(name):
            api("/sessions", {"name": name, "agent_type": "claude", "input_mode": "chat", "work_dir": str(tmp / "work")}, token)
            page = browser.new_page(viewport={"width": 1280, "height": 800})
            page.goto(base + "/")
            page.get_by_role("textbox", name="密碼").fill(PASSWORD)
            page.get_by_role("button", name="進入").click()
            page.wait_for_timeout(1000)
            page.keyboard.press("Control+P")
            page.keyboard.type(name)
            page.wait_for_timeout(300)
            page.keyboard.press("Enter")
            expect(page.locator("textarea[data-chat-input]")).to_be_enabled()
            return page

        def send(page, text):
            field = page.locator("textarea[data-chat-input]")
            field.fill(text)
            field.press("Enter")

        gap = lambda page: page.evaluate("() => { const e = document.querySelector(\"" + SCROLLER + "\"); return [Math.round(e.scrollHeight - e.scrollTop - e.clientHeight), Math.round(e.scrollTop)]; }")

        # 1) Long stream, then the permission panel appears: the view must end at the bottom (panel visible).
        #    The old logic lost the bottom in roughly 4 of 10 runs, so repeat. One foreground tab at a time:
        #    background tabs pause requestAnimationFrame, which the follow logic relies on.
        for i in range(5):
            page = open_chat(f"perm{i}")
            send(page, "STREAM PERM please")
            expect(page.get_by_role("button", name="允許").first).to_be_visible(timeout=30000)
            page.wait_for_timeout(1500)
            assert gap(page)[0] <= 40, f"run {i}: permission panel left below the fold, gap={gap(page)}"
            page.close()

        # 2) Scrolling up mid-stream (wheel, and a gesture-less programmatic scroll) stops following; going back resumes.
        for mode in ("wheel", "programmatic"):
            page = open_chat("up-" + mode)
            send(page, "STREAM please")
            page.wait_for_timeout(2500)
            box = page.locator(SCROLLER).bounding_box()
            page.mouse.move(box["x"] + box["width"] / 2, box["y"] + box["height"] / 2)
            if mode == "wheel":
                page.mouse.wheel(0, -900)
            else:
                page.evaluate("() => { document.querySelector(\"" + SCROLLER + "\").scrollTop -= 900; }")
            page.wait_for_timeout(400)
            top = gap(page)[1]
            page.wait_for_timeout(3000)
            now = gap(page)
            assert abs(now[1] - top) <= 5 and now[0] > 200, f"{mode}: view was pulled back while reading, {now}"
            for _ in range(40):  # keep scrolling down like a user until the bottom is reached
                page.mouse.wheel(0, 800)
                page.wait_for_timeout(100)
                if gap(page)[0] <= 2:
                    break
            page.wait_for_timeout(2500)
            assert gap(page)[0] <= 2, f"{mode}: following did not resume at the bottom, gap={gap(page)}"
            page.close()
        print("chat auto-scroll checks passed: stream follow, permission panel, scroll-up pause and resume")
    finally:
        if browser:
            browser.close()
        # kill the whole tree: the fake claude children keep tmp/work open on Windows
        subprocess.run(["taskkill", "/F", "/T", "/PID", str(server.pid)], capture_output=True)
        server.wait(timeout=10)
