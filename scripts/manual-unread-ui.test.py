"""手動提醒的真 App。假 HTTP 在 127.0.0.1 隨機埠，不是 Go Start，也不是 SQLite。

執行：python scripts/manual-unread-ui.test.py
"""
import copy
import json
import os
import re
import subprocess
import threading
import time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from urllib.parse import urlparse

from playwright.sync_api import expect, sync_playwright

repo = Path(__file__).resolve().parents[1]
static = repo / "internal" / "static"
out = Path(os.environ.get("MANUAL_UNREAD_UI_ARTIFACTS", r"D:/Temp/miniapp-next-four-20261121/manual-unread-ui-v3"))
out.mkdir(parents=True, exist_ok=True)
index_html = (static / "index.html").read_text(encoding="utf-8")
styles = "\n".join(re.findall(r"<style[^>]*>(.*?)</style>", index_html, re.S))
script_names = re.findall(r'<script[^>]*src="\./(js/[^"]+)"[^>]*></script>', index_html.split("<body", 1)[1])
store = {"sessions": [], "messages": {}, "raw": [], "gate": None, "fail_mark": False, "errors": [], "hold_sync": False, "held": []}


def session(sid, name, unread, marker, revision, ids, agent="claude"):
    return {
        "id": sid, "name": name, "status": "idle", "unread_count": unread, "agent_type": agent,
        "work_dir": "A", "pinned": False, "category_id": None, "last_active": "2020-01-02 03:04:05",
        "manual_unread": marker, "manual_unread_revision": revision, "read_message_ids": list(ids),
    }


def compile_bundle():
    chunks = [(static / name).read_text(encoding="utf-8") for name in script_names]
    proc = subprocess.run(
        ["go", "run", "github.com/evanw/esbuild/cmd/esbuild", "--loader=jsx", "--target=es2020"],
        input="\n".join(chunks), encoding="utf-8", capture_output=True, cwd=repo, check=True,
    )
    (out / "app.bundle.js").write_text(proc.stdout, encoding="utf-8")


def page_html():
    return f"""<!doctype html><html lang="zh-TW"><head><meta charset="utf-8">
<link rel="stylesheet" href="/js/vendor/tailwind.css">
<style>{styles}</style></head>
<body class="h-app ra-body">
<div id="root" class="h-app"></div>
<script src="/js/vendor/react.production.min.js"></script>
<script src="/js/vendor/react-dom.production.min.js"></script>
<script src="/js/vendor/marked.min.js"></script>
<script src="/js/vendor/purify.min.js"></script>
<script src="/js/vendor/highlight.min.js"></script>
<script src="/app.bundle.js"></script>
</body></html>"""


def json_response(handler, code, payload):
    body = json.dumps(payload).encode("utf-8")
    handler.send_response(code)
    handler.send_header("Content-Type", "application/json")
    handler.send_header("Content-Length", str(len(body)))
    handler.end_headers()
    try:
        handler.wfile.write(body)
    except OSError:
        return


def read_raw(handler):
    n = int(handler.headers.get("Content-Length") or 0)
    raw = handler.rfile.read(n) if n else b""
    text = raw.decode("utf-8")
    data = json.loads(text) if text.strip() else None
    return text, data


def row_by(sid):
    return next(row for row in store["sessions"] if row["id"] == sid)


def sync_ids(row):
    done = [m for m in store["messages"].get(row["id"], []) if m["status"] != "pending" and m["role"] != "user" and not m.get("is_read")]
    row["read_message_ids"] = [m["id"] for m in done]
    row["unread_count"] = len(done) if row["agent_type"] != "telegram" else 0


def ack(row, ids):
    wanted = set(ids or [])
    for msg in store["messages"].get(row["id"], []):
        if msg["id"] in wanted and msg["status"] != "pending" and msg["role"] != "user":
            msg["is_read"] = 1
    sync_ids(row)


class Handler(BaseHTTPRequestHandler):
    def log_message(self, fmt, *args):
        return

    def do_GET(self):
        self.serve()

    def do_POST(self):
        self.serve()

    def do_PATCH(self):
        self.serve()

    def do_DELETE(self):
        self.serve()

    def serve(self):
        path, method = urlparse(self.path).path, self.command
        if path in ("/", "/index.html") or path.startswith("/share/"):
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
        if path == "/guest/me":
            json_response(self, 200, {
                "nickname": "訪客", "role": "editor", "mode": "live",
                "session": {"id": "s-open", "name": "Open", "agent_type": "claude", "permission_mode": "default",
                            "manual_unread": True, "manual_unread_revision": 4, "read_message_ids": [2]},
            })
            return
        if path == "/setup/status":
            json_response(self, 200, {"needs_setup": False})
            return
        if path in ("/config", "/settings/appearance"):
            json_response(self, 200, {"version": "0.0.0", "sha": "test", "stored": False})
            return
        if path == "/rooms" or path == "/work-dirs":
            json_response(self, 200, [] if path == "/rooms" else ["A"])
            return
        if path.startswith("/session-categories"):
            json_response(self, 200, [])
            return
        if path == "/sessions" and method == "GET":
            store["raw"].append({"method": "GET", "path": path, "text": "", "body": {}})
            gate = store.get("omit_gate")
            rows = []
            for row in store["sessions"]:
                item = dict(row)
                if gate and gate.get("omit") and row["id"] == gate.get("id"):
                    item.pop("manual_unread", None)
                    item.pop("manual_unread_revision", None)
                    item.pop("read_message_ids", None)
                rows.append(item)
            json_response(self, 200, rows)
            return
        if path == "/sessions/read-all" and method == "POST":
            text, data = read_raw(self)
            store["raw"].append({"method": "POST", "path": path, "text": text, "body": data})
            gate = store.get("gate")
            if gate is not None:
                gate.wait(5)
            if not isinstance(data, dict) or "sessions" not in data:
                json_response(self, 400, {"error": "sessions 必須是觀察清單"})
                return
            states, conflict = [], False
            for entry in data["sessions"]:
                row = row_by(entry["id"])
                if entry.get("manual_unread_revision") == row["manual_unread_revision"]:
                    row["manual_unread"] = False
                    row["manual_unread_revision"] += 1
                else:
                    conflict = True
                ack(row, entry.get("read_message_ids") or [])
                states.append({"id": row["id"], "manual_unread": row["manual_unread"], "manual_unread_revision": row["manual_unread_revision"]})
            if conflict:
                json_response(self, 409, {"error": "manual_unread_revision 已變更", "sessions": states})
            else:
                json_response(self, 200, {"sessions": states})
            return
        matched = re.fullmatch(r"/sessions/([^/]+)", path)
        if matched and method == "PATCH":
            text, data = read_raw(self)
            store["raw"].append({"method": "PATCH", "path": path, "text": text, "body": data})
            row = row_by(matched.group(1))
            if store.get("fail_mark") and data.get("manual_unread") is True:
                store["fail_mark"] = False
                json_response(self, 500, {"error": "手動標記資料庫錯誤"})
                return
            if data.get("manual_unread") is True:
                row["manual_unread"] = True
                row["manual_unread_revision"] += 1
                json_response(self, 200, row)
                return
            if data.get("manual_unread") is False:
                observed = data.get("manual_unread_revision")
                if observed != row["manual_unread_revision"] or not isinstance(observed, int) or observed < 0:
                    ack(row, data.get("read_message_ids") or [])
                    json_response(self, 409, {"error": "manual_unread_revision 已變更", "id": row["id"], "manual_unread": row["manual_unread"], "manual_unread_revision": row["manual_unread_revision"]})
                    return
                row["manual_unread"] = False
                row["manual_unread_revision"] += 1
                ack(row, data.get("read_message_ids") or [])
                json_response(self, 200, row)
                return
            if data.get("mark_read") is True:
                ack(row, data.get("read_message_ids") or [])
                json_response(self, 200, row)
                return
            json_response(self, 200, row)
            return
        if path.endswith("/messages") and method == "GET":
            sid = path.split("/")[2]
            json_response(self, 200, store["messages"].get(sid, []))
            return
        if method == "GET":
            json_response(self, 200, [])
            return
        json_response(self, 200, {})


def clears():
    return [item for item in store["raw"] if isinstance(item["body"], dict) and item["body"].get("manual_unread") is False]


def wait_until(pred, timeout=6):
    deadline = time.time() + timeout
    while time.time() < deadline:
        if pred():
            return True
        pump = getattr(wait_until, "page", None)
        if pump is not None:
            pump.wait_for_timeout(50)
        else:
            time.sleep(0.05)
    return False


def boot(page, base):
    page.goto(base + "/", wait_until="domcontentloaded")
    expect(page.get_by_text("Zero", exact=True)).to_be_visible(timeout=15000)


def open_named(page, name):
    page.locator(".ra-session-card", has_text=name).click()


def menu_mark(page, name, label):
    page.get_by_role("button", name=f"更多操作：{name}").click()
    page.get_by_role("menuitem", name=label).click()


def main():
    os.environ["GOPROXY"] = "off"
    compile_bundle()
    store["sessions"] = [
        session("s-zero", "Zero", 0, True, 1, []),
        session("s-three", "Three", 3, True, 1, [10, 11, 12]),
        session("s-hundred", "Hundred", 100, True, 1, []),
        session("s-tg", "Tele", 0, True, 1, [], "telegram"),
    ]
    httpd = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
    host, port = httpd.server_address
    server_thread = threading.Thread(target=httpd.serve_forever, daemon=True)
    server_thread.start()
    base = f"http://{host}:{port}"
    errors = []
    try:
        with sync_playwright() as playwright:
            browser = playwright.chromium.launch(channel="msedge", headless=True)
            context = browser.new_context(viewport={"width": 1194, "height": 834})
            context.route("**/*", lambda route: route.abort() if not route.request.url.startswith(base) else route.continue_())
            context.add_init_script("""
              const desc = Object.getOwnPropertyDescriptor(Element.prototype, 'scrollTop');
              Object.defineProperty(Element.prototype, 'scrollTop', {
                configurable: true,
                get() { return desc.get.call(this); },
                set(value) { if (window.__freezeScroll) return; desc.set.call(this, value); }
              });
              const hasFocus = Document.prototype.hasFocus;
              Document.prototype.hasFocus = function() { return window.__blurred ? false : hasFocus.call(this); };
              Object.defineProperty(document, 'hidden', { configurable: true, get() { return !!window.__hidden; } });
            """)

            events = []

            def route_ws(ws):
                if "/events" in ws.url and "/sessions/" not in ws.url:
                    events.append(ws)
                    return
                if "/sessions/" in ws.url and "/ws" in ws.url:
                    sid = ws.url.split("/sessions/")[1].split("/ws")[0]
                    payload = {"type": "sync", "value": "IDLE", "messages": copy.deepcopy(store["messages"].get(sid, []))}
                    if store.get("hold_sync") and sid == "s-open":
                        store["held"].append((ws, payload))
                        return
                    ws.send(json.dumps(payload))

            context.route_web_socket(re.compile(r".*"), route_ws)
            page = context.new_page()
            wait_until.page = page
            page.on("pageerror", lambda err: errors.append(str(err)))
            boot(page, base)
            expect(page).to_have_title("(103) MiniApp")
            expect(page.locator(".ra-session-card", has_text="Zero").get_by_label("待回看")).to_be_visible()
            expect(page.locator(".ra-session-card", has_text="Three").get_by_label("未讀訊息 3 則，待回看")).to_be_visible()
            expect(page.locator(".ra-session-card", has_text="Hundred").get_by_label("未讀訊息 99+ 則，待回看")).to_be_visible()
            expect(page.locator(".ra-session-card", has_text="Tele").get_by_label("待回看")).to_be_visible()
            page.get_by_role("button", name="更多操作：Zero").focus()
            page.keyboard.press("Enter")
            page.keyboard.press("End")
            expect(page.get_by_role("menuitem", name="標示已讀")).to_be_focused()
            page.keyboard.press("ArrowUp")
            expect(page.get_by_role("menuitem", name="標示未讀")).to_be_focused()
            page.keyboard.press("Escape")
            listed = len([item for item in store["raw"] if item["method"] == "GET" and item["path"] == "/sessions"])
            store["omit_gate"] = {"omit": True, "id": "s-zero"}
            assert events, "events socket missing"
            page.evaluate("() => window.dispatchEvent(new Event('miniapp:sessions-refresh'))")
            assert wait_until(lambda: len([item for item in store["raw"] if item["method"] == "GET" and item["path"] == "/sessions"]) > listed), errors
            expect(page.locator(".ra-session-card", has_text="Zero").get_by_label("待回看")).to_be_visible()
            store["omit_gate"] = None

            store["sessions"] = [
                session("s-open", "Open", 1, True, 4, [2]),
                session("s-other", "Other", 0, False, 0, []),
            ]
            store["messages"] = {"s-open": [{"id": 2, "role": "claude", "content": "done reply", "status": "done"}]}
            store["raw"].clear()
            page.reload(wait_until="domcontentloaded")
            expect(page.get_by_text("Open", exact=True)).to_be_visible(timeout=15000)
            before = len(store["raw"])
            open_named(page, "Open")
            expect(page.get_by_text("done reply", exact=True)).to_be_visible(timeout=10000)
            page.wait_for_timeout(400)
            assert clears() == [], store["raw"][before:]
            unread_before = row_by("s-open")["unread_count"]
            ids_before = list(row_by("s-open")["read_message_ids"])
            menu_mark(page, "Open", "標示未讀")
            assert wait_until(lambda: row_by("s-open")["manual_unread_revision"] == 5)
            assert row_by("s-open")["unread_count"] == unread_before and row_by("s-open")["read_message_ids"] == ids_before
            assert row_by("s-open")["last_active"] == "2020-01-02 03:04:05"
            mark = next(item for item in store["raw"] if item["body"].get("manual_unread") is True)
            assert "manual_unread_revision" not in mark["body"]
            assert all("manual_unread" not in item["body"] for item in store["raw"] if item["body"].get("mark_read") is True)
            page.reload(wait_until="domcontentloaded")
            expect(page.locator(".ra-session-card", has_text="Open").get_by_label("待回看")).to_be_visible(timeout=15000)
            assert clears() == []
            rev = row_by("s-open")["manual_unread_revision"]
            ids = list(row_by("s-open")["read_message_ids"])
            open_named(page, "Other")
            open_named(page, "Open")
            expect(page.get_by_text("done reply", exact=True)).to_be_visible(timeout=10000)
            assert wait_until(lambda: any(item["body"].get("manual_unread") is False and item["body"].get("manual_unread_revision") == rev and item["body"].get("read_message_ids") == ids for item in clears()))
            assert row_by("s-open")["manual_unread"] is False
            assert not any(item["body"].get("manual_unread_revision") == 6 for item in clears())

            store["sessions"] = [session("s-blur", "Blur", 1, True, 4, [9]), session("s-other", "Other", 0, False, 0, [])]
            store["messages"] = {"s-blur": [{"id": 9, "role": "claude", "content": "blur reply", "status": "done"}]}
            store["raw"].clear()
            page.evaluate("() => localStorage.removeItem('miniapp_tab_groups_v1')")
            page.reload(wait_until="domcontentloaded")
            expect(page.get_by_text("Blur", exact=True)).to_be_visible(timeout=15000)
            page.evaluate("() => { window.__blurred = true; }")
            open_named(page, "Blur")
            expect(page.get_by_text("blur reply", exact=True)).to_be_visible(timeout=10000)
            open_named(page, "Other")
            open_named(page, "Blur")
            expect(page.get_by_text("blur reply", exact=True)).to_be_visible(timeout=10000)
            page.wait_for_timeout(400)
            assert clears() == [], store["raw"]
            page.evaluate("() => { window.__blurred = false; window.dispatchEvent(new Event('focus')); }")
            assert wait_until(lambda: any(item["body"].get("manual_unread_revision") == 4 for item in clears()))

            store["sessions"] = [session("s-hide", "Hide", 1, True, 4, [7]), session("s-other", "Other", 0, False, 0, [])]
            store["messages"] = {"s-hide": [{"id": 7, "role": "claude", "content": "hide reply", "status": "done"}]}
            store["raw"].clear()
            page.evaluate("() => localStorage.removeItem('miniapp_tab_groups_v1')")
            page.reload(wait_until="domcontentloaded")
            expect(page.get_by_text("Hide", exact=True)).to_be_visible(timeout=15000)
            page.evaluate("() => { window.__hidden = true; }")
            open_named(page, "Hide")
            expect(page.get_by_text("hide reply", exact=True)).to_be_visible(timeout=10000)
            open_named(page, "Other")
            open_named(page, "Hide")
            expect(page.get_by_text("hide reply", exact=True)).to_be_visible(timeout=10000)
            page.wait_for_timeout(400)
            assert clears() == [], store["raw"]
            page.evaluate("() => { window.__hidden = false; document.dispatchEvent(new Event('visibilitychange')); }")
            assert wait_until(lambda: any(item["path"] == "/sessions/s-hide" and item["body"].get("manual_unread") is False for item in store["raw"]))

            store["sessions"] = [
                session("s-fore", "Fore", 1, True, 4, [3]),
                session("s-back", "Back", 1, True, 4, [4]),
            ]
            store["messages"] = {
                "s-fore": [{"id": 3, "role": "claude", "content": "fore reply", "status": "done"}],
                "s-back": [{"id": 4, "role": "claude", "content": "back reply", "status": "done"}],
            }
            store["raw"].clear()
            page.evaluate("() => { window.__hidden = false; window.__blurred = false; localStorage.removeItem('miniapp_tab_groups_v1'); }")
            page.reload(wait_until="domcontentloaded")
            expect(page.get_by_text("Fore", exact=True)).to_be_visible(timeout=15000)
            open_named(page, "Fore")
            expect(page.get_by_text("fore reply", exact=True)).to_be_visible(timeout=10000)
            page.get_by_role("button", name="更多操作：Back").click()
            page.get_by_role("menuitem", name="在另一個窗格開啟").click()
            expect(page.locator('[data-chat-pane="secondary"]').get_by_text("back reply", exact=True)).to_be_visible(timeout=10000)
            page.wait_for_timeout(500)
            assert clears() == [], store["raw"]
            page.locator('[data-chat-pane="primary"]').click()
            assert wait_until(lambda: any(item["path"] == "/sessions/s-fore" and item["body"].get("manual_unread") is False for item in store["raw"]))
            assert not any(item["path"] == "/sessions/s-back" and item["body"].get("manual_unread") is False for item in store["raw"])
            page.locator('[data-chat-pane="secondary"]').click()
            assert wait_until(lambda: any(item["path"] == "/sessions/s-back" and item["body"].get("manual_unread") is False for item in store["raw"]))

            store["sessions"] = [session("s-empty", "Empty", 0, True, 4, []), session("s-other", "Other", 0, False, 0, [])]
            store["messages"] = {}
            store["raw"].clear()
            page.evaluate("() => localStorage.removeItem('miniapp_tab_groups_v1')")
            page.reload(wait_until="domcontentloaded")
            expect(page.get_by_text("Empty", exact=True)).to_be_visible(timeout=15000)
            open_named(page, "Empty")
            open_named(page, "Other")
            open_named(page, "Empty")
            page.wait_for_timeout(500)
            assert clears() == [], store["raw"]
            menu_mark(page, "Empty", "標示已讀")
            assert wait_until(lambda: any(item["body"].get("read_message_ids") == [] and item["body"].get("manual_unread_revision") == 4 for item in clears()))
            assert row_by("s-empty")["manual_unread"] is False

            store["sessions"] = [session("s-pend", "Pend", 0, True, 4, []), session("s-other", "Other", 0, False, 0, [])]
            store["messages"] = {"s-pend": [{"id": 1, "role": "claude", "content": "still pending", "status": "pending"}]}
            store["raw"].clear()
            page.reload(wait_until="domcontentloaded")
            expect(page.get_by_text("Pend", exact=True)).to_be_visible(timeout=15000)
            open_named(page, "Pend")
            expect(page.get_by_text("still pending", exact=True)).to_be_visible(timeout=10000)
            open_named(page, "Other")
            open_named(page, "Pend")
            expect(page.get_by_text("still pending", exact=True)).to_be_visible(timeout=10000)
            page.wait_for_timeout(400)
            assert clears() == [], store["raw"]

            tall = "\n".join(f"line {i}" for i in range(120))
            store["sessions"] = [session("s-hist", "Hist", 1, True, 4, [8]), session("s-other", "Other", 0, False, 0, [])]
            store["messages"] = {"s-hist": [{"id": 8, "role": "claude", "content": tall, "status": "done"}]}
            store["raw"].clear()
            page.reload(wait_until="domcontentloaded")
            expect(page.get_by_text("Hist", exact=True)).to_be_visible(timeout=15000)
            page.evaluate("() => { window.__freezeScroll = true; }")
            open_named(page, "Hist")
            open_named(page, "Other")
            open_named(page, "Hist")
            expect(page.locator("[data-chat-pane]")).to_contain_text("line 119", timeout=10000)
            page.wait_for_timeout(500)
            assert clears() == [], store["raw"]
            page.evaluate("() => { window.__freezeScroll = false; const el = document.querySelector('[data-chat-pane] .app-scroll'); if (el) el.scrollTop = el.scrollHeight; }")
            assert wait_until(lambda: any(item["body"].get("manual_unread_revision") == 4 for item in clears()))

            store["sessions"] = [session("s-open", "Open", 1, True, 4, [2]), session("s-other", "Other", 0, False, 0, [])]
            store["messages"] = {"s-open": [{"id": 2, "role": "claude", "content": "done reply", "status": "done"}]}
            store["raw"].clear()
            page.evaluate("() => { window.__freezeScroll = false; }")
            page.reload(wait_until="domcontentloaded")
            other = context.new_page()
            other.goto(base + "/", wait_until="domcontentloaded")
            expect(other.get_by_text("Open", exact=True)).to_be_visible(timeout=15000)
            open_named(page, "Open")
            expect(page.get_by_text("done reply", exact=True)).to_be_visible(timeout=10000)
            menu_mark(other, "Open", "標示未讀")
            assert wait_until(lambda: row_by("s-open")["manual_unread_revision"] == 5)
            open_named(page, "Other")
            open_named(page, "Open")
            assert wait_until(lambda: any(item["body"].get("manual_unread_revision") == 4 for item in clears()))
            expect(page.get_by_role("alert")).to_contain_text("已變更")
            assert row_by("s-open")["manual_unread"] is True and row_by("s-open")["manual_unread_revision"] == 5
            assert not any(item["body"].get("manual_unread_revision") == 5 and item["body"].get("manual_unread") is False for item in clears())
            other.close()

            store["sessions"] = [
                session("s-late", "Late", 1, True, 4, [5]),
                session("s-mark", "Mark", 0, False, 1, []),
            ]
            store["messages"] = {"s-late": [{"id": 5, "role": "claude", "content": "old done", "status": "done"}, {"id": 1, "role": "claude", "content": "later", "status": "pending"}]}
            store["raw"].clear()
            store["gate"] = threading.Event()
            page.reload(wait_until="domcontentloaded")
            expect(page.get_by_text("Late", exact=True)).to_be_visible(timeout=15000)
            page.get_by_role("button", name="全部標記已讀").click()
            assert wait_until(lambda: any(item["path"] == "/sessions/read-all" for item in store["raw"]))
            sent = next(item for item in store["raw"] if item["path"] == "/sessions/read-all")
            assert sent["text"].strip().startswith("{")
            late = next(item for item in sent["body"]["sessions"] if item["id"] == "s-late")
            assert late["manual_unread_revision"] == 4 and late["read_message_ids"] == [5]
            row_by("s-mark")["manual_unread"] = True
            row_by("s-mark")["manual_unread_revision"] = 2
            store["messages"]["s-late"][1]["status"] = "done"
            sync_ids(row_by("s-late"))
            store["gate"].set()
            store["gate"] = None
            expect(page.get_by_role("alert")).to_contain_text("已變更", timeout=5000)
            assert wait_until(lambda: row_by("s-mark")["manual_unread"] is True)
            assert any(m["id"] == 1 and not m.get("is_read") for m in store["messages"]["s-late"])
            assert not any(item["path"] == "/sessions/read-all" and item is not sent for item in store["raw"])

            ids = list(range(1, 1002))
            store["sessions"] = [session("s-bulk", "Bulk", 1001, True, 3, ids)]
            store["messages"] = {}
            store["raw"].clear()
            page.reload(wait_until="domcontentloaded")
            expect(page.get_by_text("Bulk", exact=True)).to_be_visible(timeout=15000)
            menu_mark(page, "Bulk", "標示已讀")
            assert wait_until(lambda: len([item for item in store["raw"] if item["method"] == "PATCH"]) >= 2)
            patches = [item for item in store["raw"] if item["method"] == "PATCH"]
            assert patches[0]["body"]["manual_unread"] is False and patches[0]["body"]["manual_unread_revision"] == 3
            assert patches[0]["body"]["read_message_ids"] == ids[:1000]
            assert patches[1]["body"] == {"mark_read": True, "read_message_ids": [1001]}
            assert "manual_unread" not in patches[1]["body"]

            store["sessions"] = [session("s-err", "Err", 0, False, 1, [])]
            store["messages"] = {}
            store["raw"].clear()
            store["fail_mark"] = True
            page.reload(wait_until="domcontentloaded")
            expect(page.get_by_text("Err", exact=True)).to_be_visible(timeout=15000)
            menu_mark(page, "Err", "標示未讀")
            expect(page.get_by_role("alert")).to_contain_text("失敗", timeout=5000)
            assert row_by("s-err")["manual_unread"] is False
            menu_mark(page, "Err", "標示未讀")
            assert wait_until(lambda: row_by("s-err")["manual_unread"] is True and row_by("s-err")["manual_unread_revision"] == 2)

            touch = context.new_page()
            touch.set_viewport_size({"width": 390, "height": 844})
            touch.goto(base + "/", wait_until="domcontentloaded")
            expect(touch.get_by_text("Err", exact=True)).to_be_visible(timeout=15000)
            touch.get_by_role("button", name="更多操作：Err").click()
            item = touch.get_by_role("menuitem", name="標示未讀")
            expect(item).to_be_visible()
            height = item.bounding_box()["height"]
            assert height >= 44, height
            touch.close()

            store["sessions"] = [
                session("s-open", "Open", 1, True, 4, [2]),
                session("s-other", "Other", 0, False, 0, []),
            ]
            store["messages"] = {"s-open": [{"id": 2, "role": "claude", "content": "completed result for deferred revisit", "status": "done"}]}
            store["raw"].clear()
            page.evaluate("() => { window.__blurred = false; window.__hidden = false; localStorage.removeItem('miniapp_tab_groups_v1'); }")
            page.reload(wait_until="domcontentloaded")
            expect(page.get_by_text("Open", exact=True)).to_be_visible(timeout=15000)
            open_named(page, "Open")
            expect(page.get_by_text("completed result for deferred revisit", exact=True)).to_be_visible(timeout=10000)
            page.wait_for_timeout(150)
            assert clears() == []
            page.evaluate("() => { window.__blurred = true; }")
            open_named(page, "Other")
            open_named(page, "Open")
            expect(page.get_by_text("completed result for deferred revisit", exact=True)).to_be_visible(timeout=10000)
            page.wait_for_timeout(150)
            assert clears() == [], store["raw"]
            menu_mark(page, "Open", "標示未讀")
            assert wait_until(lambda: row_by("s-open")["manual_unread_revision"] == 5)
            expect(page.locator(".ra-session-card", has_text="Open").get_by_label("待回看", exact=True)).to_be_visible()
            assert row_by("s-open")["manual_unread"] is True and clears() == []
            tab_before = page.evaluate("() => localStorage.getItem('miniapp_tab_groups_v1')")
            page.screenshot(path=str(out / "new-marker-before-focus.png"))
            page.evaluate("() => { window.__blurred = false; window.dispatchEvent(new Event('focus')); }")
            page.wait_for_timeout(400)
            (out / "exact-focus-requests.json").write_text(json.dumps(store["raw"], ensure_ascii=False, indent=2), encoding="utf-8")
            page.screenshot(path=str(out / "after-focus.png"))
            assert clears() == [], store["raw"]
            assert row_by("s-open")["manual_unread"] is True and row_by("s-open")["manual_unread_revision"] == 5
            assert page.evaluate("() => localStorage.getItem('miniapp_tab_groups_v1')") == tab_before
            open_named(page, "Other")
            open_named(page, "Open")
            expect(page.get_by_text("completed result for deferred revisit", exact=True)).to_be_visible(timeout=10000)
            assert wait_until(lambda: any(item["body"].get("manual_unread") is False and item["body"].get("manual_unread_revision") == 5 for item in clears()))
            assert len(clears()) == 1
            assert row_by("s-open")["manual_unread"] is False and row_by("s-open")["manual_unread_revision"] == 6
            assert not any(item["body"].get("manual_unread_revision") == 6 for item in clears())

            store["sessions"] = [
                session("s-open", "Open", 1, True, 4, [2]),
                session("s-other", "Other", 0, False, 0, []),
            ]
            store["messages"] = {"s-open": [{"id": 2, "role": "claude", "content": "completed result for deferred revisit", "status": "done"}]}
            store["raw"].clear()
            page.evaluate("() => { window.__blurred = false; window.__hidden = false; localStorage.removeItem('miniapp_tab_groups_v1'); }")
            known_events = len(events)
            page.reload(wait_until="domcontentloaded")
            expect(page.get_by_text("Open", exact=True)).to_be_visible(timeout=15000)
            open_named(page, "Open")
            expect(page.get_by_text("completed result for deferred revisit", exact=True)).to_be_visible(timeout=10000)
            page.evaluate("() => { window.__blurred = true; }")
            open_named(page, "Other")
            open_named(page, "Open")
            expect(page.get_by_text("completed result for deferred revisit", exact=True)).to_be_visible(timeout=10000)
            page.wait_for_timeout(150)
            assert clears() == []
            other = context.new_page()
            other.goto(base + "/", wait_until="domcontentloaded")
            expect(other.get_by_role("button", name="更多操作：Open")).to_be_visible(timeout=15000)
            menu_mark(other, "Open", "標示未讀")
            assert wait_until(lambda: row_by("s-open")["manual_unread_revision"] == 5)
            assert len(events) > known_events
            with page.expect_response(lambda r: r.request.method == "GET" and r.url.rstrip("/").endswith("/sessions")) as refreshed:
                for ws in events[known_events:]:
                    ws.send('{"type":"sessions_changed"}')
            assert refreshed.value.ok
            page.wait_for_timeout(150)
            expect(page.locator(".ra-session-card", has_text="Open").get_by_label("待回看", exact=True)).to_be_visible()
            page.evaluate("() => { window.__blurred = false; window.dispatchEvent(new Event('focus')); }")
            page.wait_for_timeout(400)
            assert not any(item["body"].get("manual_unread") is False for item in store["raw"]), store["raw"]
            assert row_by("s-open")["manual_unread"] is True and row_by("s-open")["manual_unread_revision"] == 5
            open_named(page, "Other")
            open_named(page, "Open")
            expect(page.get_by_text("completed result for deferred revisit", exact=True)).to_be_visible(timeout=10000)
            assert wait_until(lambda: any(item["body"].get("manual_unread") is False and item["body"].get("manual_unread_revision") == 5 for item in clears()))
            assert len(clears()) == 1
            assert row_by("s-open")["manual_unread"] is False and row_by("s-open")["manual_unread_revision"] == 6
            other.close()

            store["sessions"] = [
                session("s-open", "Open", 1, True, 4, [2]),
                session("s-other", "Other", 0, False, 0, []),
            ]
            store["messages"] = {"s-open": [{"id": 2, "role": "claude", "content": "old completed result", "status": "done"}]}
            store["raw"].clear()
            store["hold_sync"] = False
            store["held"].clear()
            page.evaluate("() => { window.__blurred = false; window.__hidden = false; window.__freezeScroll = false; localStorage.removeItem('miniapp_tab_groups_v1'); }")
            page.reload(wait_until="domcontentloaded")
            expect(page.get_by_role("button", name="更多操作：Open")).to_be_visible(timeout=15000)
            open_named(page, "Open")
            expect(page.get_by_text("old completed result", exact=True)).to_be_visible(timeout=10000)
            assert wait_until(lambda: any(m["id"] == 2 and m.get("is_read") == 1 for m in store["messages"]["s-open"]))
            page.evaluate("() => { window.__blurred = true; }")
            open_named(page, "Other")
            store["hold_sync"] = True
            open_named(page, "Open")
            assert wait_until(lambda: bool(store["held"]))
            page.wait_for_timeout(200)
            assert clears() == []
            store["messages"]["s-open"].append({"id": 3, "role": "claude", "content": "new completed result not yet delivered", "status": "done"})
            sync_ids(row_by("s-open"))
            assert row_by("s-open")["read_message_ids"] == [3] and row_by("s-open")["unread_count"] == 1
            listed = len([item for item in store["raw"] if item["method"] == "GET" and item["path"] == "/sessions"])
            events[-1].send('{"type":"sessions_changed"}')
            assert wait_until(lambda: len([item for item in store["raw"] if item["method"] == "GET" and item["path"] == "/sessions"]) > listed)
            expect(page.locator(".ra-session-card", has_text="Open").get_by_label("未讀訊息 1 則，待回看", exact=True)).to_be_visible()
            held_ws = store["held"][0][0]
            for ws, payload in store["held"]:
                ws.send(json.dumps(payload))
            store["held"].clear()
            store["hold_sync"] = False
            expect(page.get_by_text("old completed result", exact=True)).to_be_visible(timeout=10000)
            expect(page.get_by_text("new completed result not yet delivered", exact=True)).to_have_count(0)
            page.screenshot(path=str(out / "B-old-loaded-new-absent.png"))
            page.evaluate("() => { window.__blurred = false; window.dispatchEvent(new Event('focus')); }")
            page.wait_for_timeout(400)
            (out / "B-focus-requests.json").write_text(json.dumps(store["raw"], ensure_ascii=False, indent=2), encoding="utf-8")
            page.screenshot(path=str(out / "B-focus-known-absent.png"))
            assert clears() == [], store["raw"]
            assert row_by("s-open")["manual_unread"] is True and row_by("s-open")["manual_unread_revision"] == 4
            assert row_by("s-open")["unread_count"] == 1
            assert next(m for m in store["messages"]["s-open"] if m["id"] == 3).get("is_read") != 1
            held_ws.send(json.dumps({"type": "sync", "value": "IDLE", "messages": copy.deepcopy(store["messages"]["s-open"])}))
            expect(page.get_by_text("new completed result not yet delivered", exact=True)).to_be_visible(timeout=10000)
            assert wait_until(lambda: any(item["body"].get("manual_unread") is False and item["body"].get("manual_unread_revision") == 4 for item in clears()))
            assert len(clears()) == 1
            loaded_ids = {2, 3}
            sent_ids = clears()[0]["body"]["read_message_ids"]
            assert sent_ids and set(sent_ids) <= loaded_ids and 3 in sent_ids
            assert row_by("s-open")["manual_unread"] is False and row_by("s-open")["manual_unread_revision"] == 5
            assert next(m for m in store["messages"]["s-open"] if m["id"] == 3).get("is_read") == 1
            page.screenshot(path=str(out / "B-loaded-clear.png"))
            (out / "B-loaded-clear.json").write_text(json.dumps(clears(), ensure_ascii=False, indent=2), encoding="utf-8")

            store["sessions"] = [
                session("s-open", "Open", 0, True, 4, []),
                session("s-other", "Other", 0, False, 0, []),
            ]
            store["messages"] = {"s-open": [{"id": 20, "role": "claude", "content": "older high result", "status": "done", "is_read": 1}]}
            store["raw"].clear()
            store["hold_sync"] = False
            store["held"].clear()
            page.evaluate("() => { window.__blurred = false; localStorage.removeItem('miniapp_tab_groups_v1'); }")
            page.reload(wait_until="domcontentloaded")
            expect(page.get_by_role("button", name="更多操作：Open")).to_be_visible(timeout=15000)
            expect(page.locator(".ra-session-card", has_text="Open").get_by_label("待回看", exact=True)).to_be_visible()
            open_named(page, "Open")
            expect(page.get_by_text("older high result", exact=True)).to_be_visible(timeout=10000)
            page.evaluate("() => { window.__blurred = true; }")
            open_named(page, "Other")
            store["hold_sync"] = True
            open_named(page, "Open")
            assert wait_until(lambda: bool(store["held"]))
            store["messages"]["s-open"].append({"id": 3, "role": "claude", "content": "late small result", "status": "pending"})
            store["messages"]["s-open"][-1]["status"] = "done"
            sync_ids(row_by("s-open"))
            assert 3 in row_by("s-open")["read_message_ids"] and 3 < 20
            with page.expect_response(lambda r: r.request.method == "GET" and r.url.rstrip("/").endswith("/sessions")) as refreshed:
                events[-1].send('{"type":"sessions_changed"}')
            assert refreshed.value.ok
            expect(page.locator(".ra-session-card", has_text="Open").get_by_label("未讀訊息 1 則，待回看", exact=True)).to_be_visible()
            for ws, payload in store["held"]:
                assert [m["id"] for m in payload["messages"]] == [20]
                ws.send(json.dumps(payload))
            store["held"].clear()
            store["hold_sync"] = False
            expect(page.get_by_text("older high result", exact=True)).to_be_visible(timeout=10000)
            expect(page.get_by_text("late small result", exact=True)).to_have_count(0)
            page.evaluate("() => { window.__blurred = false; window.dispatchEvent(new Event('focus')); }")
            page.wait_for_timeout(400)
            assert clears() == [], store["raw"]
            assert row_by("s-open")["manual_unread"] is True and row_by("s-open")["manual_unread_revision"] == 4
            assert next(m for m in store["messages"]["s-open"] if m["id"] == 3).get("is_read") != 1
            (out / "C-small-id-requests.json").write_text(json.dumps(store["raw"], ensure_ascii=False, indent=2), encoding="utf-8")

            raw_before = len(store["raw"])
            guest = context.new_page()
            guest.goto(base + "/share/tok", wait_until="domcontentloaded")
            guest.evaluate("""() => sessionStorage.setItem('cc_guest_v1:' + decodeURIComponent(location.pathname.split('/').pop()), JSON.stringify({guest_token:'g', nickname:'訪客', role:'editor', mode:'live'}))""")
            guest.reload(wait_until="domcontentloaded")
            expect(guest.get_by_role("heading", name="Open")).to_be_visible(timeout=15000)
            assert guest.get_by_text("標示未讀").count() == 0
            assert guest.get_by_text("待回看").count() == 0
            assert not any(item["body"] and item["body"].get("manual_unread") is not None for item in store["raw"][raw_before:])
            guest.close()

            manifest = {
                "base": base, "port": port, "owner": "fake-http-no-go-start",
                "viewport": [1194, 834], "touch": [390, 844],
                "scripts": script_names, "scriptCount": len(script_names),
                "fullHeight": False, "layout": "Sidebar+Split",
                "errors": errors, "not": "fake HTTP is not SQLite and not the Go server",
            }
            (out / "identity.json").write_text(json.dumps(manifest, ensure_ascii=False, indent=2), encoding="utf-8")
            (out / "requests.json").write_text(json.dumps(store["raw"], ensure_ascii=False), encoding="utf-8")
            browser.close()
    finally:
        httpd.shutdown()
        httpd.server_close()
        server_thread.join(timeout=3)
        (out / "server-stop.json").write_text(json.dumps({"alive": server_thread.is_alive(), "port": port}), encoding="utf-8")
    if errors:
        raise SystemExit("pageerror " + " | ".join(errors[:8]))


if __name__ == "__main__":
    main()
