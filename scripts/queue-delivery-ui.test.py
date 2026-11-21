"""設定頁送出方式與佇列附件標示。假 HTTP／假 WebSocket，不啟動 Go MiniApp。"""
import json
import os
import re
import subprocess
import threading
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from urllib.parse import urlparse

from playwright.sync_api import expect, sync_playwright

repo = Path(__file__).resolve().parents[1]
static = repo / "internal" / "static"
out = Path(os.environ.get("QUEUE_DELIVERY_UI_OUT", r"D:/Temp/miniapp-queue-delivery-mode-20261121/revision-160/ui"))
out.mkdir(parents=True, exist_ok=True)
index_html = (static / "index.html").read_text(encoding="utf-8")
styles = "\n".join(re.findall(r"<style[^>]*>(.*?)</style>", index_html, re.S))
script_names = re.findall(r'<script[^>]*src="\./(js/[^"]+)"[^>]*></script>', index_html.split("<body", 1)[1])
QUEUE = [
    {"id": 1, "content": "one", "author": "", "source_session_id": "", "attachments": [
        {"id": "img-a", "name": "same.png", "mime_type": "image/png"},
        {"id": "img-b", "name": "same.png", "mime_type": "image/png"},
    ]},
    {"id": 2, "content": "no photo", "author": "", "source_session_id": "", "attachments": []},
    {"id": 3, "content": "three", "author": "", "source_session_id": "", "attachments": [
        {"id": "file-d", "name": "note.txt", "mime_type": "text/plain"},
        {"id": "img-c", "name": "same.png", "mime_type": "image/png"},
    ]},
]
SESSIONS = [{
    "id": "s-pm", "name": "PM會話", "status": "idle", "unread_count": 0, "agent_type": "claude",
    "work_dir": "D:/work", "pinned": False, "category_id": None, "last_active": "2020-01-02 03:04:05",
}]
ROOMS = [{"id": "room-1", "name": "甲工作室", "member_count": 1, "pm_id": "s-pm"}]


class State:
    def __init__(self):
        self.http = []
        self.general = {
            "vscodeNoAdmin": False, "claudeAutoAllowAsk": False, "autoResumeOnQuota": False,
            "quotaGuardEnabled": True, "quotaGuardPercent": 90,
        }
        self.hold = threading.Event()
        self.hold.set()
        self.arm_hold = False
        self.fail_put = False


state = State()
sockets = []


def compile_bundle():
    chunks = [(static / name).read_text(encoding="utf-8") for name in script_names]
    proc = subprocess.run(
        ["go", "run", "github.com/evanw/esbuild/cmd/esbuild", "--loader=jsx", "--target=es2020"],
        input="\n".join(chunks), encoding="utf-8", capture_output=True, cwd=repo, check=True,
    )
    (out / "app.bundle.js").write_text(proc.stdout, encoding="utf-8")


def page_html():
    return f"""<!doctype html><html lang="zh-TW"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<link rel="stylesheet" href="/js/vendor/tailwind.css"><style>{styles}</style></head>
<body class="h-app ra-body"><div id="root" class="h-app"></div>
<script src="/js/vendor/react.production.min.js"></script>
<script src="/js/vendor/react-dom.production.min.js"></script>
<script src="/js/vendor/marked.min.js"></script>
<script src="/js/vendor/purify.min.js"></script>
<script src="/js/vendor/highlight.min.js"></script>
<script src="/app.bundle.js"></script></body></html>"""


def json_response(handler, code, payload):
    body = json.dumps(payload).encode("utf-8")
    handler.close_connection = True
    handler.send_response(code)
    handler.send_header("Content-Type", "application/json")
    handler.send_header("Content-Length", str(len(body)))
    handler.send_header("Connection", "close")
    handler.end_headers()
    try:
        handler.wfile.write(body)
    except (ConnectionAbortedError, ConnectionResetError, BrokenPipeError):
        return


def read_body(handler):
    n = int(handler.headers.get("Content-Length") or 0)
    raw = handler.rfile.read(n) if n else b""
    return json.loads(raw.decode("utf-8")) if raw else {}


class Handler(BaseHTTPRequestHandler):
    def log_message(self, fmt, *args):
        return

    def do_GET(self):
        self.serve()

    def do_POST(self):
        self.serve()

    def do_PUT(self):
        self.serve()

    def do_PATCH(self):
        self.serve()

    def do_DELETE(self):
        self.serve()

    def serve(self):
        parsed = urlparse(self.path)
        path, method = parsed.path, self.command
        if path in ("/", "/index.html"):
            body = page_html().encode("utf-8")
            self.send_response(200)
            self.send_header("Content-Type", "text/html; charset=utf-8")
            self.send_header("Content-Length", str(len(body)))
            self.end_headers()
            self.wfile.write(body)
            return
        if path == "/app.bundle.js":
            data = (out / "app.bundle.js").read_bytes()
            self.send_response(200)
            self.send_header("Content-Type", "text/javascript")
            self.send_header("Content-Length", str(len(data)))
            self.end_headers()
            self.wfile.write(data)
            return
        file_path = static / path.lstrip("/")
        if method == "GET" and file_path.is_file():
            data = file_path.read_bytes()
            self.send_response(200)
            kind = "text/css" if path.endswith(".css") else "application/javascript"
            self.send_header("Content-Type", kind)
            self.send_header("Content-Length", str(len(data)))
            self.end_headers()
            self.wfile.write(data)
            return
        body = read_body(self) if method in ("POST", "PUT", "PATCH", "DELETE") else None
        if not path.startswith("/js/"):
            state.http.append({"method": method, "path": path, "body": body})
        if path == "/setup/status":
            json_response(self, 200, {"needs_setup": False})
            return
        if path == "/config":
            json_response(self, 200, {"version": "0.0.0", "sha": "test"})
            return
        if path == "/settings/appearance":
            json_response(self, 200, {"stored": False})
            return
        if path == "/settings/global-instruction":
            json_response(self, 200, {"text": "", "mode": "off"})
            return
        if path == "/settings/general":
            if method == "GET" and state.arm_hold:
                state.arm_hold = False
                state.hold.clear()
                state.hold.wait(8)
            if method == "PUT":
                if state.fail_put:
                    json_response(self, 400, {"error": "拒絕寫入"})
                    return
                state.general = body
                json_response(self, 200, state.general)
                return
            json_response(self, 200, state.general)
            return
        if path == "/work-dirs":
            json_response(self, 200, ["D:/work"])
            return
        if path == "/session-categories":
            json_response(self, 200, [])
            return
        if path == "/sessions" and method == "GET":
            json_response(self, 200, SESSIONS)
            return
        if path == "/sessions/s-pm" and method == "GET":
            json_response(self, 200, SESSIONS[0])
            return
        if path.startswith("/sessions/s-pm/messages"):
            json_response(self, 200, [])
            return
        if path == "/rooms" and method == "GET":
            json_response(self, 200, ROOMS)
            return
        if path == "/rooms/room-1" and method == "GET":
            json_response(self, 200, {
                "id": "room-1", "name": "甲工作室", "pm_id": "s-pm",
                "members": [{"session_id": "s-pm", "session_name": "PM會話", "role": "pm", "agent_type": "claude", "status": "idle"}],
            })
            return
        if path.endswith("/timeline"):
            json_response(self, 200, {"messages": [], "has_more": False, "next_cursor": ""})
            return
        if path.endswith("/queue") and method == "GET":
            json_response(self, 200, {"pm_id": "s-pm", "batch_ready": True, "queue": QUEUE, "has_more": False, "next_after_id": 0})
            return
        json_response(self, 200, {})


def puts():
    return [row for row in state.http if row["method"] == "PUT" and row["path"] == "/settings/general"]


def pump(page, pred, turns=50):
    for _ in range(turns):
        if pred():
            return True
        page.evaluate("() => 0")
    return bool(pred())


def open_settings(page):
    page.get_by_role("button", name="設定").click()
    dialog = page.locator("dialog")
    expect(dialog).to_be_visible()
    if dialog.locator("[data-settings-section='general']").count() and page.locator("#queue-delivery-each").count() == 0:
        dialog.locator("[data-settings-section='general']").click()
    return dialog


def measure(page):
    return page.evaluate("""() => {
      const label = document.querySelector('label[for="queue-delivery-each"]');
      const box = label.getBoundingClientRect();
      const root = document.documentElement;
      const dialog = document.querySelector('dialog');
      return {
        labelH: box.height, labelW: box.width,
        pageOverflow: root.scrollWidth > root.clientWidth + 1,
        dialogOverflow: dialog ? dialog.scrollWidth > dialog.clientWidth + 1 : false,
      };
    }""")


def assert_queue(page, root):
    text = root.inner_text()
    assert "照片1-1 img-a" in text and "照片1-2 img-b" in text, text
    assert text.index("照片1-1 img-a") < text.index("照片1-2 img-b"), text
    assert "照片2-" not in text, text
    assert "附件3-1 file-d" in text and "照片3-1 img-c" in text, text
    assert text.index("附件3-1 file-d") < text.index("照片3-1 img-c"), text


def main():
    compile_bundle()
    server = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
    thread = threading.Thread(target=server.serve_forever, daemon=True)
    thread.start()
    origin = f"http://127.0.0.1:{server.server_port}"
    errors = []
    evidence = {"origin": origin, "stopped": False}
    try:
        with sync_playwright() as p:
            browser = p.chromium.launch(channel="msedge", headless=True)
            try:
                ctx = browser.new_context(viewport={"width": 1280, "height": 900})
                ctx.add_init_script("Object.defineProperty(Document.prototype, 'hidden', { configurable: true, get() { return true; } });")
                expected = ("http", "127.0.0.1", server.server_port)

                def allow(route):
                    u = urlparse(route.request.url)
                    if (u.scheme, u.hostname, u.port) == expected:
                        route.continue_()
                    else:
                        route.abort()

                def route_ws(ws):
                    sockets.append(ws)
                    payload = json.dumps({"type": "sync", "value": "IDLE", "messages": [], "queue": QUEUE, "model": {"display_text": "gpt-6.1-sol"}})
                    if "/sessions/" in ws.url and "/ws" in ws.url:
                        ws.send(payload)

                    def on_sent(_frame):
                        if "/sessions/" in ws.url and "/ws" in ws.url:
                            ws.send(payload)

                    ws.on_message(on_sent)

                ctx.route("**/*", allow)
                ctx.route_web_socket(re.compile(r".*"), route_ws)
                page = ctx.new_page()
                page.on("pageerror", lambda err: errors.append(str(err)))
                page.goto(origin, wait_until="domcontentloaded")
                expect(page.locator("[aria-label='PM會話']")).to_be_visible(timeout=15000)
                page.locator("[aria-label='PM會話']").click()
                chat = page.locator("[aria-label='排隊中的訊息']")
                expect(chat).to_be_visible(timeout=10000)
                assert_queue(page, chat)
                page.screenshot(path=str(out / "queue-chat.png"))
                page.locator("button.room-list-open", has_text="甲工作室").click()
                room = page.locator(".room-queue-panel")
                expect(room).to_be_visible()
                assert_queue(page, room)
                page.screenshot(path=str(out / "queue-room.png"))

                state.arm_hold = True
                page.get_by_role("button", name="設定").click()
                assert pump(page, lambda: any(row["path"] == "/settings/general" and row["method"] == "GET" for row in state.http))
                dialog = page.locator("dialog")
                each = dialog.get_by_role("radio", name="逐筆送出")
                batch = dialog.get_by_role("radio", name="一起送")
                expect(each).to_be_disabled()
                state.hold.set()
                expect(each).to_be_enabled()
                expect(each).to_be_checked()
                before = len(puts())
                batch.check()
                page.evaluate("() => 0")
                assert len(puts()) == before, puts()
                dialog.get_by_role("button", name="儲存佇列送出方式").click()
                assert pump(page, lambda: len(puts()) == before + 1)
                assert len(puts()) == before + 1
                assert puts()[-1]["body"]["queueDelivery"] == "batch"
                page.keyboard.press("Escape")
                expect(dialog).to_be_hidden()
                open_settings(page)
                expect(batch).to_be_checked()
                state.fail_put = True
                marked = len(puts())
                each.check()
                dialog.get_by_role("button", name="儲存佇列送出方式").click()
                assert pump(page, lambda: len(puts()) == marked + 1)
                expect(dialog.get_by_role("alert")).to_contain_text("拒絕寫入")
                expect(batch).to_be_checked()
                assert state.general.get("queueDelivery") == "batch"
                page.screenshot(path=str(out / "settings-desktop.png"))

                def check_viewport(width, height, name, touch):
                    box = browser.new_context(viewport={"width": width, "height": height}, has_touch=touch)
                    box.add_init_script("Object.defineProperty(Document.prototype, 'hidden', { configurable: true, get() { return true; } });")
                    box.route("**/*", allow)
                    box.route_web_socket(re.compile(r".*"), route_ws)
                    view = box.new_page()
                    view.on("pageerror", lambda err: errors.append(str(err)))
                    view.goto(origin, wait_until="domcontentloaded")
                    expect(view.get_by_role("button", name="設定")).to_be_visible(timeout=15000)
                    open_settings(view)
                    expect(view.get_by_role("radio", name="一起送")).to_be_checked()
                    metrics = measure(view)
                    assert metrics["labelH"] >= 44 and metrics["labelW"] >= 44, metrics
                    assert not metrics["pageOverflow"] and not metrics["dialogOverflow"], metrics
                    view.screenshot(path=str(out / name))
                    (out / (name + ".json")).write_text(json.dumps(metrics), encoding="utf-8")
                    box.close()

                check_viewport(390, 844, "settings-390.png", True)
                check_viewport(768, 1024, "settings-768.png", True)
            finally:
                browser.close()
    finally:
        server.shutdown()
        thread.join(5)
        evidence["stopped"] = not thread.is_alive()
        evidence["joined"] = not thread.is_alive()
        evidence["pageerrors"] = errors
        evidence["puts"] = puts()
        (out / "requests.json").write_text(json.dumps({"http": state.http, "evidence": evidence}, ensure_ascii=False, indent=2), encoding="utf-8")
        print("server stopped", evidence["stopped"], "pageerrors", errors)
    if errors:
        raise SystemExit(1)


if __name__ == "__main__":
    main()
