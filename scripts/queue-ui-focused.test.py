"""重新同步與排隊編輯的隔離畫面。假 HTTP／假 WebSocket，不啟動 Go MiniApp。

執行：python scripts/queue-ui-focused.test.py
"""
import json
import os
import re
import subprocess
import threading
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from urllib.parse import parse_qs, urlparse

from playwright.sync_api import expect, sync_playwright

repo = Path(__file__).resolve().parents[1]
static = repo / "internal" / "static"
out = Path(os.environ.get("QUEUE_UI_OUT", r"D:/Temp/miniapp-session-queue-ui-20261121/revision-158/ui"))
out.mkdir(parents=True, exist_ok=True)
index_html = (static / "index.html").read_text(encoding="utf-8")
styles = "\n".join(re.findall(r"<style[^>]*>(.*?)</style>", index_html, re.S))
script_names = re.findall(r'<script[^>]*src="\./(js/[^"]+)"[^>]*></script>', index_html.split("<body", 1)[1])
SESSIONS = [{
    "id": "s-pm", "name": "PM會話", "status": "idle", "unread_count": 0, "agent_type": "claude",
    "work_dir": "D:/work", "pinned": False, "category_id": None, "last_active": "2020-01-02 03:04:05",
}]
ROOMS = [
    {"id": "room-1", "name": "甲工作室", "member_count": 1, "pm_id": "s-pm"},
    {"id": "room-2", "name": "乙工作室", "member_count": 1, "pm_id": "s-pm"},
]
CHAT_QUEUE = [
    {"id": 4, "content": "收斂worker", "author": "room-user:room-1", "source_session_id": ""},
    {"id": 9, "content": "私人第二筆", "author": "", "source_session_id": ""},
    {"id": 3, "content": "工作項", "author": "room-job:room-1", "source_session_id": ""},
    {"id": 5, "content": "別的來源", "author": "", "source_session_id": "other"},
    {"id": 6, "content": "已成批", "author": "", "source_session_id": "", "batch_id": "batch-1"},
]


class Gate:
    def __init__(self):
        self.cv = threading.Condition()
        self.mode = "ok"
        self.seq = 0
        self.tickets = []
        self.http = []
        self.frames = []
        self.batch_ready = True
        self.rows = [
            {"id": 1, "content": "第一則", "author": "", "source_session_id": ""},
            {"id": 2, "content": "第二則", "author": "", "source_session_id": ""},
        ]

    def begin(self, path):
        with self.cv:
            self.seq += 1
            mode = self.mode if "/rooms/room-1/queue" == path else "ok"
            ticket = {"n": self.seq, "path": path, "go": threading.Event(), "mode": mode, "rows": None}
            self.tickets.append(ticket)
            self.cv.notify_all()
        if mode == "hold":
            ticket["go"].wait(8)
        return ticket

    def wait_held(self, after, count):
        with self.cv:
            ok = self.cv.wait_for(lambda: len([t for t in self.tickets if t["n"] > after and "/queue" in t["path"]]) >= count, 8)
            fresh = [t for t in self.tickets if t["n"] > after and "/queue" in t["path"]]
        return ok, fresh


gate = Gate()
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
    if not raw:
        return {}
    return json.loads(raw.decode("utf-8"))


def room_detail(room_id):
    return {
        "id": room_id, "name": "甲工作室" if room_id == "room-1" else "乙工作室", "pm_id": "s-pm",
        "members": [{
            "session_id": "s-pm", "session_name": "PM會話", "role": "pm",
            "agent_type": "claude", "status": "idle",
        }],
    }


def queue_payload(pm):
    with gate.cv:
        rows = [dict(row) for row in gate.rows]
        ready = gate.batch_ready
    return {"pm_id": pm, "batch_ready": ready, "queue": rows, "has_more": False, "next_after_id": 0}


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
        parsed = urlparse(self.path)
        path, method = parsed.path, self.command
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
            self.send_header("Content-Type", "text/css" if path.endswith(".css") else "application/javascript")
            self.send_header("Content-Length", str(len(data)))
            self.end_headers()
            self.wfile.write(data)
            return
        body = read_body(self) if method in ("POST", "PATCH", "DELETE") else None
        if not path.startswith("/js/"):
            gate.http.append({"method": method, "path": path, "query": parsed.query, "body": body})
        if path == "/setup/status":
            json_response(self, 200, {"needs_setup": False})
            return
        if path == "/config":
            json_response(self, 200, {"version": "0.0.0", "sha": "test"})
            return
        if path == "/settings/appearance":
            json_response(self, 200, {"stored": False})
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
        if path == "/sessions/s-pm/messages" or path.startswith("/sessions/s-pm/messages"):
            json_response(self, 200, [])
            return
        if path == "/rooms" and method == "GET":
            json_response(self, 200, ROOMS)
            return
        if path in ("/rooms/room-1", "/rooms/room-2") and method == "GET":
            json_response(self, 200, room_detail(path.rsplit("/", 1)[1]))
            return
        if path.endswith("/timeline") and method == "GET":
            json_response(self, 200, {"messages": [], "has_more": False, "next_cursor": ""})
            return
        if path.endswith("/queue") and method == "GET":
            ticket = gate.begin(path)
            pm = parse_qs(parsed.query).get("expected_pm_id", ["s-pm"])[0]
            if ticket["mode"] == "fail":
                json_response(self, 500, {"error": "同步失敗"})
                return
            if ticket["mode"] == "stale":
                json_response(self, 200, queue_payload("other-pm"))
                return
            payload = queue_payload(pm)
            if ticket.get("rows") is not None:
                payload["queue"] = ticket["rows"]
            json_response(self, 200, payload)
            return
        if path == "/rooms/room-1/messages/batch" and method == "POST":
            json_response(self, 500, {"error": "舊批次錯誤"})
            return
        if method == "PATCH" and path.startswith("/rooms/") and "/messages/" in path:
            json_response(self, 409, {"error": "conflict"})
            return
        if path == "/guest/me":
            json_response(self, 200, {
                "nickname": "訪客", "role": "editor", "mode": "live",
                "session": {"id": "s-guest", "name": "共享", "agent_type": "claude", "permission_mode": "default"},
            })
            return
        json_response(self, 404, {"error": "missing"})


def sync_for(session_id):
    queue = CHAT_QUEUE if session_id == "s-pm" else [
        {"id": 4, "content": "收斂worker", "author": "room-user:room-1", "source_session_id": ""},
        {"id": 9, "content": "私人第二筆", "author": "", "source_session_id": ""},
    ]
    return {"type": "sync", "value": "IDLE", "messages": [], "queue": queue, "model": {"display_text": "gpt-6.1-sol"}}


def pump(page, pred, turns=40):
    for _ in range(turns):
        if pred():
            return True
        page.evaluate("() => 0")
    return bool(pred())


def main():
    compile_bundle()
    server = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
    thread = threading.Thread(target=server.serve_forever, daemon=True)
    thread.start()
    origin = f"http://127.0.0.1:{server.server_port}"
    errors = []
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

                    def on_sent(frame):
                        payload = frame.decode("utf-8") if isinstance(frame, bytes) else frame
                        gate.frames.append(payload)
                        try:
                            msg = json.loads(payload)
                        except (TypeError, json.JSONDecodeError):
                            return
                        if msg.get("type") == "queue_edit":
                            ws.send(json.dumps({"type": "queue_edit_success", "request_id": msg.get("request_id"), "id": msg.get("id")}))

                    ws.on_message(on_sent)
                    if "/sessions/" in ws.url and "/ws" in ws.url:
                        sid = ws.url.split("/sessions/")[1].split("/ws")[0]
                        ws.send(json.dumps(sync_for(sid)))

                ctx.route("**/*", allow)
                ctx.route_web_socket(re.compile(r".*"), route_ws)
                page = ctx.new_page()
                page.on("pageerror", lambda err: errors.append(str(err)))
                page.goto(origin, wait_until="domcontentloaded")
                room1 = page.locator("button.room-list-open", has_text="甲工作室")
                expect(room1).to_be_visible(timeout=15000)
                room1.click()
                panel = page.locator(".room-queue-panel")
                expect(panel.get_by_role("button", name="一起送")).to_be_visible()
                panel.get_by_role("button", name="一起送").click()
                expect(panel.get_by_role("alert")).to_contain_text("舊批次錯誤")
                with gate.cv:
                    mark = gate.seq
                    gate.mode = "hold"
                panel.get_by_role("button", name="重新同步").click()
                ok, held = gate.wait_held(mark, 1)
                assert ok, gate.tickets
                expect(panel.get_by_text("正在重新同步佇列…")).to_be_visible()
                expect(panel.get_by_role("button", name="重新同步中")).to_be_disabled()
                with gate.cv:
                    gate.rows = [
                        {"id": 1, "content": "已更新", "author": "", "source_session_id": ""},
                        {"id": 2, "content": "第二則", "author": "", "source_session_id": ""},
                    ]
                    gate.mode = "ok"
                held[0]["go"].set()
                expect(panel.get_by_text("已重新同步")).to_be_visible()
                expect(panel.get_by_text("已更新")).to_be_visible()
                expect(panel.get_by_text("舊批次錯誤")).to_have_count(0)
                page.screenshot(path=str(out / "resync-success.png"))
                with gate.cv:
                    mark = gate.seq
                    gate.mode = "fail"
                panel.get_by_role("button", name="重新同步").click()
                ok, failed = gate.wait_held(mark, 1)
                assert ok, "失敗請求沒有進伺服器"
                expect(panel.get_by_role("alert")).to_contain_text("同步失敗")
                expect(panel.get_by_text("已重新同步")).to_have_count(0)
                with gate.cv:
                    gate.mode = "hold"
                    mark = gate.seq
                panel.get_by_role("button", name="重新同步").click()
                ok, first = gate.wait_held(mark, 1)
                assert ok
                held_count = len([t for t in gate.tickets if t["n"] > mark and t["path"] == "/rooms/room-1/queue"])
                panel.get_by_role("button", name="重新同步中").click(force=True, timeout=2000)
                assert len([t for t in gate.tickets if t["n"] > mark and t["path"] == "/rooms/room-1/queue"]) == held_count
                page.evaluate("() => window.dispatchEvent(new Event('miniapp:rooms-changed'))")
                fresh = lambda: [t for t in gate.tickets if t["n"] > mark and t["path"] == "/rooms/room-1/queue"]
                assert pump(page, lambda: len(fresh()) >= 2), [t["path"] for t in gate.tickets]
                both = fresh()
                both[0]["rows"] = [{"id": 1, "content": "舊回應", "author": "", "source_session_id": ""}]
                both[1]["rows"] = [{"id": 1, "content": "新回應", "author": "", "source_session_id": ""}]
                both[1]["mode"] = "ok"
                both[1]["go"].set()
                expect(panel.get_by_text("新回應")).to_be_visible()
                both[0]["go"].set()
                expect(panel.get_by_text("舊回應")).to_have_count(0)
                with gate.cv:
                    gate.mode = "hold"
                    gate.rows = [
                        {"id": 1, "content": "房間一", "author": "", "source_session_id": ""},
                        {"id": 2, "content": "仍在", "author": "", "source_session_id": ""},
                    ]
                    mark = gate.seq
                page.locator(".room-queue-panel").get_by_role("button", name="重新同步").click()
                ok, late = gate.wait_held(mark, 1)
                assert ok and late[0]["path"] == "/rooms/room-1/queue"
                page.locator("button.room-list-open", has_text="乙工作室").click()
                expect(page.get_by_role("button", name="重新同步")).to_be_visible()
                expect(page.get_by_text("已重新同步")).to_have_count(0)
                late[0]["rows"] = [{"id": 8, "content": "房間一舊回應", "author": "", "source_session_id": ""}]
                late[0]["go"].set()
                expect(page.get_by_text("房間一舊回應")).to_have_count(0)
                expect(page.get_by_text("已重新同步")).to_have_count(0)
                page.screenshot(path=str(out / "resync-handoff.png"))
                with gate.cv:
                    gate.mode = "ok"
                page.locator(".ra-session-card", has_text="PM會話").click()
                expect(page.get_by_text("收斂worker")).to_be_visible()
                expect(page.get_by_text("立刻執行會中斷目前執行中的動作。")).to_have_count(0)
                edits = page.get_by_role("button", name="編輯", exact=True)
                expect(edits).to_have_count(2)
                assert page.locator('[aria-label="立刻執行"]').evaluate_all("els => els.every(el => !el.hasAttribute('aria-describedby'))")
                edits.nth(0).click()
                form4 = page.locator("[data-queue-edit='4']")
                box = form4.locator("textarea")
                expect(box).to_have_value("收斂worker")
                box.fill("收斂worker-改")
                form4.get_by_role("button", name="取消").click()
                expect(page.locator("[data-queue-edit='4']")).to_have_count(0)
                assert not any(item["method"] == "PATCH" for item in gate.http)
                edits.nth(0).click()
                form4 = page.locator("[data-queue-edit='4']")
                form4.locator("textarea").fill("收斂worker-存")
                form4.get_by_role("button", name="儲存修改").click()
                expect(form4.get_by_text("草稿已保留")).to_be_visible()
                expect(form4.locator("textarea")).to_have_value("收斂worker-存")
                patch = [item for item in gate.http if item["method"] == "PATCH"][-1]
                assert patch["path"] == "/rooms/room-1/messages/4", patch
                assert patch["body"] == {"expected_pm_id": "s-pm", "previous": "收斂worker", "content": "收斂worker-存"}, patch["body"]
                form4.get_by_role("button", name="取消").click()
                edits.nth(1).click()
                form9 = page.locator("[data-queue-edit='9']")
                form9.locator("textarea").fill("私人第二筆-存")
                form9.get_by_role("button", name="儲存修改").click()

                def edit_frame():
                    for frame in gate.frames:
                        if not isinstance(frame, str) or not frame.startswith("{"):
                            continue
                        try:
                            msg = json.loads(frame)
                        except json.JSONDecodeError:
                            continue
                        if msg.get("type") == "queue_edit" and msg.get("id") == 9:
                            return msg
                    return None

                assert pump(page, lambda: edit_frame() is not None), {"tail": gate.frames[-4:], "form": form9.inner_text()}
                msg = edit_frame()
                for sock in sockets:
                    if "/sessions/s-pm/" in sock.url:
                        sock.send(json.dumps({"type": "queue_edit_success", "request_id": msg["request_id"], "id": msg["id"]}))
                expect(page.locator("[data-queue-edit='9']")).to_have_count(0)
                sent = [json.loads(frame) for frame in gate.frames if isinstance(frame, str) and frame.startswith("{")]
                edit = next(item for item in sent if item.get("type") == "queue_edit" and item.get("id") == 9)
                assert edit["id"] == 9 and edit["previous"] == "私人第二筆" and edit["content"] == "私人第二筆-存"
                page.screenshot(path=str(out / "queue-edit.png"))
                ctx.close()
                guest = browser.new_context(viewport={"width": 390, "height": 844})
                guest.add_init_script("Object.defineProperty(Document.prototype, 'hidden', { configurable: true, get() { return true; } }); sessionStorage.setItem('cc_guest_v1:demo', JSON.stringify({guest_token:'guest-test', nickname:'訪客', role:'editor', mode:'live'}));")
                guest.route("**/*", allow)
                guest.route_web_socket(re.compile(r".*"), route_ws)
                gpage = guest.new_page()
                gpage.on("pageerror", lambda err: errors.append("guest:" + str(err)))
                gpage.goto(origin + "/share/demo", wait_until="domcontentloaded")
                expect(gpage.get_by_text("收斂worker")).to_be_visible(timeout=15000)
                expect(gpage.get_by_role("button", name="編輯", exact=True)).to_have_count(0)
                expect(gpage.get_by_text("立刻執行會中斷目前執行中的動作。")).to_have_count(0)
                gpage.screenshot(path=str(out / "guest-no-edit.png"))
                guest.close()
                assert not errors, errors
            finally:
                browser.close()
    finally:
        server.shutdown()
        server.server_close()
        thread.join(timeout=5)
        (out / "requests.json").write_text(json.dumps({"stopped": not thread.is_alive(), "http": gate.http, "pageerrors": errors}, ensure_ascii=False, indent=2), encoding="utf-8")
        print(json.dumps({"stopped": not thread.is_alive(), "http": len(gate.http), "pageerrors": errors}, ensure_ascii=False))


if __name__ == "__main__":
    main()
