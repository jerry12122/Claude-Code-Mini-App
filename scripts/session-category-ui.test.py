"""真 App 的分類呈現。假 API 在 127.0.0.1 隨機埠，不是 Go Start，也不是 SQLite。

與真 DB 的差：歸屬存在這個行程的 dict；guest 403 由這個假路由回；沒有 runner。
執行：python scripts/session-category-ui.test.py
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
out = Path(os.environ.get("CATEGORY_UI_ARTIFACTS", r"D:/Temp/miniapp-next-four-20261121/session-category-ui-v3"))
out.mkdir(parents=True, exist_ok=True)

SESSIONS = [
    {"id": "s-alpha", "name": "Alpha", "status": "running", "unread_count": 3, "agent_type": "claude",
     "work_dir": "A", "pinned": False, "category_id": None, "last_active": "2020-01-02 03:04:05"},
    {"id": "s-beta", "name": "Beta", "status": "idle", "unread_count": 0, "agent_type": "claude",
     "work_dir": "A", "pinned": False, "category_id": "cat-a", "last_active": "2020-01-02 03:04:05"},
    {"id": "s-pin", "name": "PinMe", "status": "idle", "unread_count": 0, "agent_type": "claude",
     "work_dir": "A", "pinned": True, "category_id": "cat-a", "last_active": "2020-01-02 03:04:05"},
    {"id": "s-gamma", "name": "Gamma", "status": "idle", "unread_count": 0, "agent_type": "claude",
     "work_dir": "B", "pinned": False, "category_id": "cat-b", "last_active": "2020-01-02 03:04:05"},
    {"id": "s-tg", "name": "Tele", "status": "idle", "unread_count": 0, "agent_type": "telegram",
     "work_dir": "", "pinned": False, "category_id": None, "last_active": "2020-01-02 03:04:05"},
    {"id": "s-unset", "name": "NoDir", "status": "idle", "unread_count": 0, "agent_type": "claude",
     "work_dir": "", "pinned": False, "category_id": None, "last_active": "2020-01-02 03:04:05"},
]
CATEGORIES = [
    {"id": "cat-a", "work_dir": "A", "name": "Review"},
    {"id": "cat-empty", "work_dir": "A", "name": "Empty"},
    {"id": "cat-b", "work_dir": "B", "name": "Review"},
    {"id": "cat-c", "work_dir": "C", "name": "Shelf"},
]
WORK_DIRS = ["A", "B", "C"]
store = {"sessions": SESSIONS, "categories": CATEGORIES, "writes": []}
index_html = (static / "index.html").read_text(encoding="utf-8")
styles = "\n".join(re.findall(r"<style[^>]*>(.*?)</style>", index_html, re.S))
script_names = re.findall(r'<script[^>]*src="\./(js/[^"]+)"[^>]*></script>', index_html.split("<body", 1)[1])


def compile_bundle():
    chunks = []
    for name in script_names:
        chunks.append((static / name).read_text(encoding="utf-8"))
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
    handler.wfile.write(body)


def read_body(handler):
    n = int(handler.headers.get("Content-Length") or 0)
    raw = handler.rfile.read(n) if n else b""
    return json.loads(raw.decode("utf-8") or "{}")


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
        if path == "/guest/me":
            json_response(self, 200, {"nickname": "訪客", "role": "editor", "mode": "live",
                                      "session": {"id": "s-alpha", "name": "Alpha", "agent_type": "claude", "permission_mode": "default"}})
            return
        if self.headers.get("X-Share-Token") and (path.startswith("/session-categories") or path.startswith("/sessions")):
            json_response(self, 403, {"error": "share_forbidden"})
            return
        if path == "/setup/status":
            json_response(self, 200, {"needs_setup": False})
            return
        if path == "/config":
            json_response(self, 200, {"version": "0.0.0", "sha": "test"})
            return
        if path == "/settings/appearance":
            json_response(self, 200, {"stored": False})
            return
        if path == "/rooms":
            json_response(self, 200, [])
            return
        if path == "/work-dirs" and method == "GET":
            json_response(self, 200, WORK_DIRS)
            return
        if path == "/sessions" and method == "GET":
            json_response(self, 200, store["sessions"])
            return
        if path == "/session-categories" and method == "GET":
            work = parse_qs(parsed.query).get("work_dir", [""])[0]
            json_response(self, 200, [c for c in store["categories"] if c["work_dir"] == work])
            return
        if path == "/session-categories" and method == "POST":
            body = read_body(self)
            name = str(body.get("name") or "")
            work = str(body.get("work_dir") or "")
            if name == "BAD":
                json_response(self, 400, {"error": "分類名稱或工作目錄無效"})
                return
            if any(c["work_dir"] == work and c["name"] == name for c in store["categories"]):
                json_response(self, 409, {"error": "同目錄已有相同分類名稱"})
                return
            cat = {"id": "cat-new", "work_dir": work, "name": name}
            store["categories"].append(cat)
            store["writes"].append("POST category")
            json_response(self, 201, cat)
            return
        m = re.fullmatch(r"/session-categories/([^/]+)", path)
        if m and method == "PATCH":
            body = read_body(self)
            cat = next(c for c in store["categories"] if c["id"] == m.group(1))
            cat["name"] = str(body.get("name") or "")
            store["writes"].append("PATCH category")
            json_response(self, 200, cat)
            return
        if m and method == "DELETE":
            cid = m.group(1)
            store["categories"] = [c for c in store["categories"] if c["id"] != cid]
            for s in store["sessions"]:
                if s["category_id"] == cid:
                    s["category_id"] = None
            store["writes"].append("DELETE category")
            self.send_response(204)
            self.end_headers()
            return
        sm = re.fullmatch(r"/sessions/([^/]+)", path)
        if sm and method == "PATCH":
            body = read_body(self)
            row = next(s for s in store["sessions"] if s["id"] == sm.group(1))
            if "category_id" in body:
                row["category_id"] = body["category_id"]
            if "pinned" in body:
                row["pinned"] = bool(body["pinned"])
            if "name" in body:
                row["name"] = body["name"]
            store["writes"].append(("PATCH session", body))
            json_response(self, 200, row)
            return
        if sm and method == "GET":
            json_response(self, 200, next(s for s in store["sessions"] if s["id"] == sm.group(1)))
            return
        if path.endswith("/messages") and method == "GET":
            json_response(self, 200, [])
            return
        if method == "DELETE" and path.startswith("/sessions/"):
            store["writes"].append("DELETE session")
            self.send_response(204)
            self.end_headers()
            return
        json_response(self, 404, {"error": "missing"})


def labels(page):
    return page.locator("[data-category-label]").evaluate_all("els => els.map(el => el.dataset.categoryLabel)")


def session_stamp():
    return [(s["id"], s["name"], s["last_active"], s["unread_count"], s["status"]) for s in store["sessions"]]


def main():
    compile_bundle()
    httpd = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
    host, port = httpd.server_address
    server_thread = threading.Thread(target=httpd.serve_forever, daemon=True)
    server_thread.start()
    base = f"http://{host}:{port}"
    errors = []
    sockets = []
    blocked = []

    def allow_local(route):
        if route.request.url.startswith(base):
            route.continue_()
        else:
            blocked.append(route.request.url)
            route.abort()

    def local_page(browser, **kwargs):
        page = browser.new_page(**kwargs)
        page.context.route("**/*", allow_local)
        return page

    try:
        with sync_playwright() as p:
            browser = p.chromium.launch(channel="msedge", headless=True)
            context = browser.new_context(viewport={"width": 1194, "height": 834})
            context.route("**/*", allow_local)
            context.route_web_socket(re.compile(r".*/events(?:\?.*)?$"), lambda ws: sockets.append(ws))
            page = context.new_page()
            page.on("pageerror", lambda err: errors.append(str(err)))
            page.goto(base + "/", wait_until="domcontentloaded")
            page.wait_for_timeout(500)
            if page.get_by_text("Alpha", exact=True).count() == 0:
                (out / "boot.txt").write_text(page.locator("body").inner_text()[:2000], encoding="utf-8")
                page.screenshot(path=str(out / "boot.png"))
            expect(page.get_by_text("Alpha", exact=True)).to_be_visible(timeout=15000)
            page.screenshot(path=str(out / "red-missing-category.png"))
            expect(page.get_by_role("button", name="管理 A 的分類")).to_be_visible()
            assert labels(page)[:3] == ["Empty", "Review", "未分類"], labels(page)
            assert page.get_by_text("Beta", exact=True).count() == 1
            alpha = page.locator(".ra-session-card", has_text="Alpha")
            expect(alpha.locator(".ra-status-dot.running")).to_be_visible()
            expect(alpha.locator(".ra-unread-badge")).to_have_text("3")
            assert page.evaluate("() => categoryCodePoints('😀') === 1 && '😀'.length === 2")
            page.get_by_label("搜尋會話").fill("Beta")
            assert labels(page) == ["Review"], labels(page)
            page.get_by_label("搜尋會話").fill("")
            assert "Empty" in labels(page)
            more = page.get_by_role("button", name="更多操作：Beta")
            more.focus()
            page.keyboard.press("Enter")
            expect(page.get_by_role("menuitem", name="未分類")).to_be_visible()
            page.keyboard.press("Escape")
            expect(more).to_be_focused()
            page.get_by_role("button", name="管理 A 的分類").click()
            page.get_by_role("menuitem", name="建立分類").click()
            dialog = page.locator("dialog")
            dialog.locator("input").fill(" \t ")
            dialog.locator("button", has_text="建立").click()
            expect(page.get_by_role("alert")).to_contain_text("不可為空")
            assert not any(w == "POST category" for w in store["writes"])
            dialog.locator("input").fill("Review")
            dialog.locator("button", has_text="建立").click()
            expect(page.get_by_role("alert")).to_contain_text("相同分類名稱")
            assert sum(1 for c in store["categories"] if c["work_dir"] == "A" and c["name"] == "Review") == 1
            dialog.locator("input").fill("BAD")
            dialog.locator("button", has_text="建立").click()
            expect(page.get_by_role("alert")).to_contain_text("無效")
            dialog.locator("input").fill("Later")
            dialog.locator("button", has_text="建立").click()
            expect(page.locator("[data-category-label='Later']")).to_be_visible()
            page.get_by_role("button", name="分類操作：Later").click()
            page.get_by_role("menuitem", name="刪除分類").click()
            expect(page.get_by_text("不會刪除會話")).to_be_visible()
            page.get_by_role("button", name="刪除分類", exact=True).click()
            expect(page.locator("[data-category-label='Later']")).to_have_count(0)
            assert all(s["id"] != "DELETE" for s in store["sessions"])
            assert not any(w == "DELETE session" for w in store["writes"])
            beta = next(s for s in store["sessions"] if s["id"] == "s-beta")
            assert beta["last_active"] == "2020-01-02 03:04:05" and beta["unread_count"] == 0
            page.get_by_role("button", name="更多操作：Beta").click()
            page.get_by_role("menuitem", name="未分類").click()
            expect(page.locator("[data-category-label='未分類']").locator("xpath=../..").get_by_text("Beta", exact=True)).to_be_visible()
            assert beta["category_id"] is None
            page.get_by_role("button", name="更多操作：Beta").click()
            page.get_by_role("menuitem", name="歸類：Review").click()
            expect(page.locator("[data-category-label='Review']").first.locator("xpath=../..").get_by_text("Beta", exact=True)).to_be_visible()
            assert beta["category_id"] == "cat-a", beta
            more.click()
            page.get_by_role("menuitem", name="取消釘選" if False else "釘選").click()
            expect(page.get_by_text("Beta", exact=True)).to_have_count(1)
            page.get_by_role("button", name="更多操作：Beta").click()
            page.get_by_role("menuitem", name="取消釘選").click()
            assert next(s for s in store["sessions"] if s["id"] == "s-beta")["category_id"] == "cat-a"
            other = local_page(browser, viewport={"width": 1194, "height": 834})
            other.goto(base + "/", wait_until="domcontentloaded")
            expect(other.locator("[data-category-label='Review']").first).to_be_visible(timeout=15000)
            assert "Empty" in labels(other)
            touch = local_page(browser, viewport={"width": 390, "height": 844}, has_touch=True)
            touch.goto(base + "/", wait_until="domcontentloaded")
            btn = touch.get_by_role("button", name="管理 A 的分類")
            expect(btn).to_be_visible(timeout=15000)
            box = btn.bounding_box()
            assert box["width"] >= 44 and box["height"] >= 44, box
            guest = local_page(browser)
            guest.goto(base + "/share/guesttok", wait_until="domcontentloaded")
            guest.evaluate("""() => sessionStorage.setItem('cc_guest_v1:guesttok', JSON.stringify({guest_token:'g',nickname:'訪客',role:'editor',mode:'live',session:{id:'s-alpha',name:'Alpha',agent_type:'claude'}}))""")
            guest.reload(wait_until="domcontentloaded")
            expect(guest.get_by_role("button", name="管理 A 的分類")).to_have_count(0)
            denied = guest.evaluate("""async () => {
              const res = await fetch('/session-categories?work_dir=A', {headers:{'X-Share-Token':'g'}});
              return res.status;
            }""")
            assert denied == 403
            assert not any(s["id"] == "s-alpha" and s["category_id"] not in (None, "cat-a") for s in store["sessions"])
            chat = page.locator("main .h-full")
            page.get_by_text("Alpha", exact=True).click()
            expect(chat.first).to_be_visible(timeout=10000)
            page.screenshot(path=str(out / "owner-categories.png"))
            touch.screenshot(path=str(out / "touch-390.png"))
            for row in store["sessions"]:
                if row["work_dir"] == "A":
                    row["pinned"] = True
            stamp = session_stamp()
            pinned = context.new_page()
            pinned.on("pageerror", lambda err: errors.append(str(err)))
            pinned.goto(base + "/", wait_until="domcontentloaded")
            expect(pinned.locator(".ra-session-card", has_text="Alpha")).to_be_visible(timeout=15000)
            expect(pinned.get_by_role("button", name="管理 B 的分類")).to_be_visible()
            pinned.screenshot(path=str(out / "all-A-pinned.png"))
            assert pinned.locator("[data-category-label='Empty']").count() == 1
            assert pinned.get_by_role("button", name="管理 A 的分類").count() == 1
            assert pinned.locator(".ra-session-card", has_text="Alpha").count() == 1
            assert pinned.locator(".ra-session-card", has_text="Beta").count() == 1
            pinned.get_by_label("搜尋會話").fill("沒有這則")
            assert pinned.locator("[data-category-label='Empty']").count() == 0
            pinned.get_by_label("搜尋會話").fill("")
            expect(pinned.locator("[data-category-label='Empty']")).to_be_visible()
            shelf = context.new_page()
            shelf.goto(base + "/", wait_until="domcontentloaded")
            expect(shelf.get_by_role("button", name="管理 C 的分類")).to_be_visible(timeout=15000)
            expect(shelf.locator("[data-category-label='Shelf']")).to_be_visible()
            shelf.reload(wait_until="domcontentloaded")
            expect(shelf.get_by_role("button", name="管理 C 的分類")).to_be_visible(timeout=15000)
            known = len(sockets)
            watcher = context.new_page()
            watcher.on("pageerror", lambda err: errors.append(str(err)))
            watcher.goto(base + "/", wait_until="domcontentloaded")
            expect(watcher.locator("[data-category-label='Empty']")).to_be_visible(timeout=15000)
            watcher_url = watcher.url
            for _ in range(50):
                if len(sockets) > known:
                    break
                watcher.wait_for_timeout(100)
            watcher_ws = sockets[-1]
            pinned.get_by_role("button", name="分類操作：Empty").click()
            pinned.get_by_role("menuitem", name="重新命名…").click()
            rename = pinned.locator("dialog")
            rename.locator("input").fill("Kept")
            rename.locator("button", has_text="儲存").click()
            expect(pinned.locator("[data-category-label='Kept']")).to_be_visible()
            assert watcher.url == watcher_url
            assert watcher.locator("[data-category-label='Kept']").count() == 0
            watcher_ws.send('{"type":"sessions_changed"}')
            expect(watcher.locator("[data-category-label='Kept']")).to_be_visible(timeout=5000)
            assert watcher.url == watcher_url
            assert session_stamp() == stamp
            pinned.locator("div.space-y-0", has=pinned.get_by_role("button", name="管理 A 的分類")).get_by_role("button", name="分類操作：Review").click()
            pinned.get_by_role("menuitem", name="刪除分類").click()
            expect(pinned.get_by_text("不會刪除會話")).to_be_visible()
            pinned.get_by_role("button", name="刪除分類", exact=True).click()
            expect(pinned.locator("[data-category-label='Review']").first).to_be_visible()
            beta = next(s for s in store["sessions"] if s["id"] == "s-beta")
            assert beta["category_id"] is None
            assert beta["last_active"] == "2020-01-02 03:04:05" and beta["unread_count"] == 0
            assert pinned.get_by_text("Beta", exact=True).count() == 1
            assert not any(w == "DELETE session" for w in store["writes"])
            assert session_stamp() == stamp
            pinned.get_by_role("button", name="更多操作：Beta").click()
            pinned.get_by_role("menuitem", name="取消釘選").click()
            expect(pinned.locator("[data-category-label='未分類']").locator("xpath=../..").get_by_text("Beta", exact=True)).to_be_visible()
            assert pinned.get_by_text("Beta", exact=True).count() == 1
            saved_sessions = list(store["sessions"])
            saved_categories = list(store["categories"])
            store["sessions"] = []
            store["categories"] = [{"id": "cat-c", "work_dir": "C", "name": "Shelf"}]
            empty = local_page(browser, viewport={"width": 1194, "height": 834})
            empty.on("pageerror", lambda err: errors.append(str(err)))
            empty_api = []

            def remember(res):
                if res.request.method != "GET":
                    return
                path = urlparse(res.url).path
                if path not in ("/sessions", "/work-dirs", "/session-categories"):
                    return
                empty_api.append({"url": res.url, "status": res.status, "body": res.json()})

            empty.on("response", remember)
            empty.goto(base + "/", wait_until="domcontentloaded")
            expect(empty.locator("[data-category-label='Shelf']")).to_be_visible(timeout=15000)
            manage = empty.get_by_role("button", name="管理 C 的分類")
            expect(manage).to_be_visible()
            assert empty.get_by_text("尚無 Session，點 + 建立").count() == 0
            assert empty.get_by_label("搜尋會話").count() == 0
            assert empty.locator(".ra-session-card").count() == 0
            manage.click()
            expect(empty.get_by_role("menuitem", name="建立分類")).to_be_visible()
            empty.keyboard.press("Escape")
            empty.reload(wait_until="domcontentloaded")
            expect(empty.locator("[data-category-label='Shelf']")).to_be_visible(timeout=15000)
            expect(empty.get_by_role("button", name="管理 C 的分類")).to_be_visible()
            empty.screenshot(path=str(out / "empty-category.png"))
            (out / "empty-category.json").write_text(json.dumps({
                "sessions": [],
                "shelf": empty.locator("[data-category-label='Shelf']").count(),
                "manage_c": empty.get_by_role("button", name="管理 C 的分類").count(),
                "empty_copy": empty.get_by_text("尚無 Session，點 + 建立").count(),
                "search": empty.get_by_label("搜尋會話").count(),
                "session_rows": empty.locator(".ra-session-card").count(),
                "responses": empty_api,
            }, ensure_ascii=False, indent=2), encoding="utf-8")
            store["categories"] = []
            bare = local_page(browser, viewport={"width": 1194, "height": 834})
            bare.on("pageerror", lambda err: errors.append(str(err)))
            bare.goto(base + "/", wait_until="domcontentloaded")
            expect(bare.get_by_text("尚無 Session，點 + 建立")).to_be_visible(timeout=15000)
            assert bare.locator("[data-category-label='Shelf']").count() == 0
            assert bare.get_by_role("button", name="管理 C 的分類").count() == 0
            bare.screenshot(path=str(out / "no-category.png"))
            store["sessions"] = saved_sessions
            store["categories"] = saved_categories
            manifest = {
                "base": base, "port": port, "owner": "fake-http-no-go-start",
                "viewport": [1194, 834], "touch": [390, 844],
                "scripts": script_names, "scriptCount": len(script_names),
                "errors": errors, "writes": store["writes"], "blocked": blocked,
                "fullHeightClass": "h-full",
                "not": "fake HTTP is not SQLite and fake 403 is not Go guest middleware",
            }
            (out / "identity.json").write_text(json.dumps(manifest, ensure_ascii=False, indent=2), encoding="utf-8")
            browser.close()
    finally:
        httpd.shutdown()
        httpd.server_close()
        server_thread.join(timeout=3)
        (out / "server-stop.json").write_text(json.dumps({"alive": server_thread.is_alive(), "port": port}), encoding="utf-8")
    if errors:
        raise SystemExit("pageerror " + " | ".join(errors))


if __name__ == "__main__":
    main()
