"""桌面／平板橫向標頭兩列的隔離 DOM 檢查。

假 API 在 127.0.0.1 隨機埠，假 WebSocket 只回一則 sync。
不啟動 Go MiniApp、Warmup、runner、provider 或 SQLite。
"""
import json
import os
import re
import subprocess
import sys
import threading
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from urllib.parse import urlparse

from playwright.sync_api import expect, sync_playwright

repo = Path(__file__).resolve().parents[1]
static = repo / "internal" / "static"
out = Path(os.environ.get("HEADER_UI_OUT", r"D:/Temp/miniapp-chat-header-restore-20261121/implementation"))
out.mkdir(parents=True, exist_ok=True)
shots = out / "shots"
shots.mkdir(exist_ok=True)

LONG = "這是很長的工作目錄名稱不能撞到額度" * 3
SESSIONS = [
    {
        "id": "s-1", "name": "lucky直版-cx-2", "status": "idle", "unread_count": 0,
        "agent_type": "claude", "work_dir": "D:/專案/" + LONG, "git_branch": "refactor",
        "pinned": False, "category_id": None, "last_active": "2020-01-02 03:04:05",
    },
    {
        "id": "s-2", "name": "第二個會話", "status": "idle", "unread_count": 0,
        "agent_type": "claude", "work_dir": "D:/其他/專案", "git_branch": "",
        "pinned": False, "category_id": None, "last_active": "2020-01-02 03:04:05",
    },
]
SYNC = {
    "type": "sync", "value": "IDLE", "messages": [],
    "model": {"display_text": "gpt-6.1-sol"},
    "quota": {"display_text": "10% · 10%"},
}
index_html = (static / "index.html").read_text(encoding="utf-8")
styles = "\n".join(re.findall(r"<style[^>]*>(.*?)</style>", index_html, re.S))
script_names = re.findall(r'<script[^>]*src="\./(js/[^"]+)"[^>]*></script>', index_html.split("<body", 1)[1])
sockets = []
errors = []


def compile_bundle():
    chunks = [(static / name).read_text(encoding="utf-8") for name in script_names]
    proc = subprocess.run(
        ["go", "run", "github.com/evanw/esbuild/cmd/esbuild", "--loader=jsx", "--target=es2020"],
        input="\n".join(chunks), encoding="utf-8", capture_output=True, cwd=repo, check=True,
    )
    (out / "app.bundle.js").write_text(proc.stdout, encoding="utf-8")


def page_html():
    return f"""<!doctype html><html lang="zh-TW"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1, viewport-fit=cover">
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
            kind = "text/css" if path.endswith(".css") else "application/javascript"
            self.send_response(200)
            self.send_header("Content-Type", kind)
            self.send_header("Content-Length", str(len(data)))
            self.end_headers()
            self.wfile.write(data)
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
        if path == "/work-dirs":
            json_response(self, 200, ["D:/專案", "D:/其他/專案"])
            return
        if path == "/sessions" and method == "GET":
            json_response(self, 200, SESSIONS)
            return
        if path == "/session-categories":
            json_response(self, 200, [])
            return
        if re.fullmatch(r"/sessions/[^/]+", path) and method == "GET":
            sid = path.rsplit("/", 1)[1]
            json_response(self, 200, next(s for s in SESSIONS if s["id"] == sid))
            return
        if path.endswith("/messages") and method == "GET":
            json_response(self, 200, [])
            return
        if re.fullmatch(r"/sessions/[^/]+/git/status", path):
            json_response(self, 200, {"is_repo": True, "branch": "refactor", "ahead": 0, "behind": 0, "additions": 0, "deletions": 0, "total": 0})
            return
        if re.fullmatch(r"/sessions/[^/]+/git/branches", path):
            json_response(self, 200, {"branches": ["refactor"]})
            return
        json_response(self, 404, {"error": "missing"})


MEASURE = """() => {
  const box = (el) => {
    if (!el) return null;
    const r = el.getBoundingClientRect();
    return {top: r.top, right: r.right, bottom: r.bottom, left: r.left, width: r.width, height: r.height};
  };
  const header = document.querySelector('.ra-chat-header');
  const tabs = document.querySelector('.session-tab-bar');
  const title = header && header.querySelector('h2');
  const sub = header && header.querySelector('.ra-subline');
  const primary = header && header.querySelector('[data-header-row="primary"]');
  const secondary = header && header.querySelector('[data-header-row="secondary"]');
  const conv = document.querySelector('[data-chat-pane="primary"] .app-scroll');
  const probe = document.createElement('div');
  probe.style.paddingTop = 'env(safe-area-inset-top, 0px)';
  document.body.appendChild(probe);
  const envTop = getComputedStyle(probe).paddingTop;
  probe.remove();
  const cs = header ? getComputedStyle(header) : null;
  const desk = header && header.querySelector('.ra-chat-header-desktop');
  const mob = header && header.querySelector('.ra-chat-header-mobile');
  return {
    viewport: {width: innerWidth, height: innerHeight, landscape: matchMedia('(orientation: landscape)').matches},
    coarse: matchMedia('(pointer: coarse)').matches,
    fine: matchMedia('(pointer: fine)').matches,
    envSafeTop: envTop,
    tgSafeTop: getComputedStyle(document.documentElement).getPropertyValue('--tg-content-safe-top').trim(),
    headerPaddingTop: cs ? cs.paddingTop : '',
    desktopDisplay: desk ? getComputedStyle(desk).display : '',
    mobileDisplay: mob ? getComputedStyle(mob).display : '',
    overflow: document.documentElement.scrollWidth - document.documentElement.clientWidth,
    tabs: box(tabs), header: box(header), title: box(title), sub: box(sub),
    primary: box(primary), secondary: box(secondary), conversation: box(conv),
    buttons: header ? [...header.querySelectorAll('button')].map((b) => {
      const r = b.getBoundingClientRect();
      return {name: b.getAttribute('aria-label') || '', width: r.width, height: r.height, top: r.top};
    }) : [],
  };
}"""


def gap(a, b):
    if not a or not b:
        return None
    return round(b["top"] - a["bottom"], 2)


def open_chat(page):
    card = page.locator(".ra-session-card", has_text="lucky直版-cx-2")
    expect(card).to_be_visible(timeout=15000)
    card.click()
    expect(page.locator("textarea[data-chat-input]")).to_be_visible(timeout=15000)
    expect(page.locator(".ra-chat-header")).to_be_visible()


def annotate(m):
    m["tabsToHeader"] = gap(m["tabs"], m["header"])
    m["headerToTitle"] = None if not m["header"] or not m["title"] else round(m["title"]["top"] - m["header"]["top"], 2)
    m["titleToSub"] = gap(m["title"], m["sub"])
    m["primaryToSecondary"] = gap(m["primary"], m["secondary"])
    m["headerToConversation"] = gap(m["header"], m["conversation"])
    m["rowsSeparated"] = bool(m["primary"] and m["secondary"] and m["secondary"]["top"] >= m["primary"]["bottom"] - 1)
    return m


def main():
    compile_bundle()
    server = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
    thread = threading.Thread(target=server.serve_forever, daemon=True)
    thread.start()
    origin = f"http://127.0.0.1:{server.server_port}"
    results = []
    failures = []
    try:
        with sync_playwright() as p:
            browser = p.chromium.launch(channel="msedge", headless=True)
            try:
                cases = [
                    {"name": "desktop-1280", "width": 1280, "height": 800, "rows": True, "shot": True},
                    {"name": "ipad-1194", "width": 1194, "height": 834, "rows": True, "shot": True, "bounds": True},
                    {"name": "landscape-1024", "width": 1024, "height": 768, "rows": True},
                    {"name": "portrait-768", "width": 768, "height": 1024, "rows": False},
                    {"name": "phone-390", "width": 390, "height": 844, "rows": False},
                    {"name": "rotate-portrait", "width": 1194, "height": 834, "then": (834, 1194), "rows": False},
                    {"name": "keyboard-short", "width": 1194, "height": 834, "keyboard": True, "rows": True},
                    {"name": "coarse-1194", "width": 1194, "height": 834, "coarse": True, "rows": True},
                    {"name": "safe-47", "width": 1194, "height": 834, "safe": "47px", "rows": True},
                    {"name": "split-1280", "width": 1280, "height": 800, "split": True, "rows": True, "shot": True},
                ]
                for case in cases:
                    options = {"viewport": {"width": case["width"], "height": case["height"]}}
                    if case.get("coarse"):
                        options["has_touch"] = True
                        options["is_mobile"] = True
                    ctx = browser.new_context(**options)
                    expected = ("http", "127.0.0.1", server.server_port)

                    def allow(route):
                        u = urlparse(route.request.url)
                        if (u.scheme, u.hostname, u.port) == expected:
                            route.continue_()
                        else:
                            route.abort()

                    def route_ws(ws):
                        sockets.append(ws)
                        if "/sessions/" in ws.url and "/ws" in ws.url:
                            ws.send(json.dumps(SYNC))

                    ctx.route("**/*", allow)
                    ctx.route_web_socket(re.compile(r".*"), route_ws)
                    page = ctx.new_page()
                    page.on("pageerror", lambda err, name=case["name"]: errors.append(f"{name}: {err}"))
                    page.goto(origin, wait_until="domcontentloaded")
                    open_chat(page)
                    if case.get("safe"):
                        page.evaluate("document.documentElement.style.setProperty('--tg-content-safe-top','47px')")
                    if case.get("coarse"):
                        page.context.new_cdp_session(page).send("Emulation.setEmulatedMedia", {
                            "media": "screen",
                            "features": [
                                {"name": "pointer", "value": "coarse"},
                                {"name": "hover", "value": "none"},
                                {"name": "any-pointer", "value": "coarse"},
                            ],
                        })
                    if case.get("then"):
                        page.set_viewport_size({"width": case["then"][0], "height": case["then"][1]})
                    if case.get("split"):
                        page.get_by_role("button", name="開啟分割視窗").click()
                        expect(page.locator(".ra-chat-header")).to_have_count(2, timeout=10000)
                    if case.get("keyboard"):
                        page.locator("textarea[data-chat-input]").first.focus()
                        page.set_viewport_size({"width": case["width"], "height": 420})
                        expect(page.locator("textarea[data-chat-input]").first).to_be_visible()
                    page.wait_for_timeout(150)
                    one = page.evaluate(MEASURE)
                    headers = page.locator(".ra-chat-header")
                    count = headers.count()
                    measured = []
                    for i in range(count):
                        item = headers.nth(i).evaluate(
                            """(header) => {
                              const box = (el) => {
                                if (!el) return null;
                                const r = el.getBoundingClientRect();
                                return {top: r.top, right: r.right, bottom: r.bottom, left: r.left, width: r.width, height: r.height};
                              };
                              const cs = getComputedStyle(header);
                              const desk = header.querySelector('.ra-chat-header-desktop');
                              const mob = header.querySelector('.ra-chat-header-mobile');
                              const deskOn = !!(desk && getComputedStyle(desk).display !== 'none');
                              const scope = deskOn ? desk : mob;
                              const title = scope && scope.querySelector('h2');
                              const sub = scope && scope.querySelector('.ra-subline');
                              const primary = scope && scope.querySelector('[data-header-row="primary"]');
                              const secondary = scope && scope.querySelector('[data-header-row="secondary"]');
                              const share = scope && scope.querySelector('.ra-share-btn');
                              const refresh = scope && scope.querySelector('.ra-quota-refresh');
                              const titleBox = box(title), shareBox = box(share);
                              const overlap = !!(titleBox && shareBox && titleBox.left < shareBox.right - 1 && titleBox.right > shareBox.left + 1 && titleBox.top < shareBox.bottom - 1 && titleBox.bottom > shareBox.top + 1);
                              return {
                                title: titleBox, sub: box(sub), primary: box(primary), secondary: box(secondary),
                                shareInPrimary: !!(share && primary && primary.contains(share)),
                                refreshInSecondary: !!(refresh && secondary && secondary.contains(refresh)),
                                titleOverlapsShare: overlap,
                                rowsSeparated: !!(primary && secondary && box(secondary).top >= box(primary).bottom - 1),
                                desktopDisplay: desk ? getComputedStyle(desk).display : '',
                                mobileDisplay: mob ? getComputedStyle(mob).display : '',
                                headerPaddingTop: cs.paddingTop,
                                buttons: [...(scope ? scope.querySelectorAll('button') : [])].filter((b) => {
                                  const r = b.getBoundingClientRect();
                                  return r.width >= 1 && r.height >= 1;
                                }).map((b) => {
                                  const r = b.getBoundingClientRect();
                                  return {name: b.getAttribute('aria-label') || '', width: r.width, height: r.height};
                                }),
                              };
                            }"""
                        )
                        measured.append(item)
                    doc = annotate(one)
                    doc["name"] = case["name"]
                    doc["headers"] = measured
                    doc["headerCount"] = count
                    doc["simulated"] = True
                    doc["realDevice"] = "NOT_RUN"
                    results.append(doc)
                    if case.get("shot"):
                        page.screenshot(path=str(shots / f"{case['name']}.png"))
                    if case.get("rows"):
                        if not doc["rowsSeparated"]:
                            failures.append(f"{case['name']}: 兩列未分離 {doc['primaryToSecondary']}")
                        for item in measured:
                            if item["desktopDisplay"] == "none":
                                failures.append(f"{case['name']}: 寬畫面仍隱藏桌面標頭")
                            if not item["shareInPrimary"] or not item["refreshInSecondary"]:
                                failures.append(f"{case['name']}: 分享或重新整理不在指定列")
                            if item["titleOverlapsShare"]:
                                failures.append(f"{case['name']}: 標題與分享重疊")
                    else:
                        if measured and measured[0]["desktopDisplay"] != "none":
                            failures.append(f"{case['name']}: 窄畫面不該顯示桌面雙列")
                        if measured and measured[0]["mobileDisplay"] == "none":
                            failures.append(f"{case['name']}: 窄畫面手機標頭不可消失")
                    if doc["overflow"] > 1:
                        failures.append(f"{case['name']}: 水平溢出 {doc['overflow']}")
                    if case.get("coarse"):
                        doc["coarseEmulation"] = "applied" if doc["coarse"] else "NOT_APPLIED"
                        if not doc["coarse"]:
                            failures.append(f"{case['name']}: pointer coarse 未生效")
                        for item in measured:
                            for button in item["buttons"]:
                                if button["width"] < 44 or button["height"] < 44:
                                    failures.append(f"{case['name']}: {button['name']} {button['width']:.1f}x{button['height']:.1f}")
                    if case.get("safe"):
                        if doc["headerPaddingTop"] != "47px":
                            failures.append(f"{case['name']}: padding {doc['headerPaddingTop']}")
                        if doc["headerToTitle"] is None or doc["headerToTitle"] < 47:
                            failures.append(f"{case['name']}: 標題進入 safe 區 {doc['headerToTitle']}")
                    ctx.close()
            finally:
                browser.close()
    finally:
        server.shutdown()
    payload = {"origin": origin, "realDevice": "NOT_RUN", "cases": results, "pageerrors": errors, "failures": failures}
    (out / "dom.json").write_text(json.dumps(payload, ensure_ascii=False, indent=2), encoding="utf-8")
    print(json.dumps({"failures": failures, "pageerrors": errors, "cases": [
        {"name": c["name"], "rows": c["rowsSeparated"], "gap": c["primaryToSecondary"], "tabsToHeader": c["tabsToHeader"], "padding": c["headerPaddingTop"], "env": c["envSafeTop"], "overflow": c["overflow"]}
        for c in results
    ]}, ensure_ascii=False))
    if errors:
        failures.extend(errors)
    if failures:
        sys.exit(1)


if __name__ == "__main__":
    main()
