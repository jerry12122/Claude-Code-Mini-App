"""保存成功畫面與版面證據，供逐張視覺檢查；不把尺寸模擬當成真機。"""
from contextlib import nullcontext
from datetime import datetime, timezone
import json
import os
from pathlib import Path
import re
import time
from urllib.parse import quote, urlsplit
import uuid

import pytest
from playwright.sync_api import sync_playwright

PROFILE = os.environ.get("UI_PROFILE", "local-edge")
PLATFORMS = {
    "chrome": {"os": "Windows", "os_version": "11", "browser": "chrome", "browser_version": "latest"},
    "edge": {"os": "Windows", "os_version": "11", "browser": "edge", "browser_version": "latest"},
    "firefox": {"os": "Windows", "os_version": "11", "browser": "playwright-firefox"},
    "webkit": {"os": "OS X", "os_version": "Sequoia", "browser": "playwright-webkit"},
    "android": {"device": "Samsung Galaxy S25 Ultra", "osVersion": "15.0", "browser": "chrome"},
    "ios": {"device": "iPhone 17 Pro", "os_version": "26.0", "browser": "safari"},
}
NATIVE = PROFILE in ("android", "ios")

# Test-only diagnostic observation settings (enabled only via explicit env gate)
DIAGNOSTICS_ENABLED = os.environ.get("CMA_UI_DIAGNOSTICS") == "1"
MAX_EVENTS = 500
MAX_STRING_LEN = 500
MAX_STACK_LEN = 1000

DIAGNOSTIC_EVENTS = []
CURRENT_CASE = {"name": "init"}
EVENTS_TRUNCATED = False
FILE_WRITE_ERROR = None


def _sanitize_embedded_url(match):
    raw = match.group(0)
    trailing = ""
    while raw and raw[-1] in ".,;:!?)":
        trailing = raw[-1] + trailing
        raw = raw[:-1]
    try:
        parts = urlsplit(raw)
        netloc = parts.netloc
        if "@" in netloc:
            netloc = netloc.split("@")[-1]
        return f"{parts.scheme}://{netloc}{parts.path}{trailing}"
    except Exception:
        clean = raw.split("?")[0].split("#")[0]
        if "@" in clean:
            clean = clean.split("@")[-1]
        return f"{clean}{trailing}"


def _sanitize_embedded_relative(match):
    lead = match.group(1) or ""
    path = match.group(2)
    if path.startswith("//"):
        path = "//" + path[2:].rsplit("@", 1)[-1]
    trailing = match.group(5) or ""
    return f"{lead}{path}{trailing}"


def mask_sensitive(text, max_len=MAX_STRING_LEN):
    """Masks credentials, raw and URL-encoded access keys, query tokens, and strips userinfo/query/fragment from embedded URLs."""
    if not text:
        return ""
    s = str(text)

    # 1. Mask raw and URL-encoded BrowserStack access key
    bs_key = os.environ.get("BROWSERSTACK_ACCESS_KEY", "")
    if bs_key:
        s = s.replace(bs_key, "[REDACTED_BS_KEY]")
        for q in (quote(bs_key), quote(bs_key, safe="")):
            if q and q != bs_key:
                s = s.replace(q, "[REDACTED_BS_KEY]")

    # 2. Mask raw and URL-encoded BrowserStack username
    bs_user = os.environ.get("BROWSERSTACK_USERNAME", "")
    if bs_user:
        s = s.replace(bs_user, "[REDACTED_BS_USER]")
        for q in (quote(bs_user), quote(bs_user, safe="")):
            if q and q != bs_user:
                s = s.replace(q, "[REDACTED_BS_USER]")

    # 3. Sanitize absolute URLs in freeform text (stripping userinfo, query, fragment)
    s = re.sub(r'[a-zA-Z][a-zA-Z0-9+.-]*://[^\s"\'<>\]\)]+', _sanitize_embedded_url, s)

    # 4. Sanitize relative URLs with query or fragment (stripping query and fragment)
    s = re.sub(r'([\s"\'\(]|^)(/[^\s"\'<>\]\)\?#]*)(\?[^\s"\'<>\]\)]*)?(#[^\s"\'<>\]\)]*)?([\.,;:!\)]*)(?=[\s"\'\)]|$)', _sanitize_embedded_relative, s)

    # 5. Mask remaining credential query/form tokens
    s = re.sub(r'(?i)(token|password|access_key|accesskey|secret|auth|key)=[^&\s"\'`]+', r'\1=[REDACTED]', s)

    # 6. Apply bounded length limit
    if max_len and len(s) > max_len:
        s = s[:max_len] + "...[truncated]"
    return s


def sanitize_url(raw_url):
    """Returns safe scheme://host:port/path without userinfo, query params, or fragments."""
    if not raw_url:
        return ""
    try:
        parts = urlsplit(str(raw_url))
        netloc = parts.netloc.split("@")[-1]
        return f"{parts.scheme}://{netloc}{parts.path}" if parts.scheme else (f"//{netloc}{parts.path}" if netloc else parts.path)
    except Exception:
        clean = str(raw_url).split("?")[0].split("#")[0]
        if "@" in clean:
            clean = clean.split("@")[-1]
        return mask_sensitive(clean)


def is_session_path(raw_url):
    """Validates that parsed URL path strictly equals /sessions or has /sessions/ prefix."""
    if not raw_url:
        return False
    try:
        p = urlsplit(str(raw_url)).path
        return p == "/sessions" or p.startswith("/sessions/")
    except Exception:
        return False


def log_diag_event(event_type, **kwargs):
    if not DIAGNOSTICS_ENABLED:
        return
    global EVENTS_TRUNCATED, FILE_WRITE_ERROR
    if len(DIAGNOSTIC_EVENTS) >= MAX_EVENTS:
        if not EVENTS_TRUNCATED:
            EVENTS_TRUNCATED = True
            DIAGNOSTIC_EVENTS.append({
                "event": "diagnostic_events_limit_reached",
                "limit": MAX_EVENTS,
                "utc": datetime.now(timezone.utc).isoformat(),
                "monotonic_s": round(time.monotonic(), 6),
                "correlation": "non-atomic (path+method+time); note: proxy observes HTTP request dispatch, not TCP/TLS transport accept",
            })
        return

    entry = {
        "event": event_type,
        "utc": datetime.now(timezone.utc).isoformat(),
        "monotonic_s": round(time.monotonic(), 6),
        "correlation": "non-atomic (path+method+time); note: proxy observes HTTP request dispatch, not TCP/TLS transport accept",
        **kwargs,
    }
    DIAGNOSTIC_EVENTS.append(entry)
    try:
        diag_dir = Path("test-results/ui") / PROFILE
        diag_dir.mkdir(parents=True, exist_ok=True)
        with (diag_dir / "diagnostic-events.jsonl").open("a", encoding="utf-8") as f:
            f.write(json.dumps(entry, ensure_ascii=False) + "\n")
    except Exception as ex:
        FILE_WRITE_ERROR = str(ex)


@pytest.fixture(autouse=True)
def observe_case_lifecycle(request):
    """Observes true pytest fixture setup and teardown boundaries without altering error listeners."""
    if not DIAGNOSTICS_ENABLED:
        yield
        return
    viewport = request.node.callspec.params.get("viewport")
    size = "native" if viewport is None else str(viewport[0])
    log_diag_event("case_fixture_setup", case=size)
    yield
    log_diag_event("case_fixture_teardown", case=size)


@pytest.fixture(scope="module")
def page(app, request):
    tunnel = browser = device = context = current = None
    try:
        shared = request.getfixturevalue("playwright")
    except pytest.FixtureLookupError:
        shared = None
    with nullcontext(shared) if shared is not None else sync_playwright() as p:
        try:
            if PROFILE == "local-edge":
                browser = p.chromium.launch(channel="msedge", headless=True)
            else:
                from browserstack.local import Local
                identifier = "ui-" + uuid.uuid4().hex
                key = os.environ["BROWSERSTACK_ACCESS_KEY"]
                tunnel = Local()
                tunnel.start(key=key, localIdentifier=identifier)
                # Safari Local 依官方建議使用 bs-local.com；仍須另驗證 WebSocket。
                app["url"] = app["url"].replace("127.0.0.1", "bs-local.com")
                caps = {
                    **PLATFORMS[PROFILE],
                    "browserstack.username": os.environ["BROWSERSTACK_USERNAME"],
                    "browserstack.accessKey": key,
                    "browserstack.local": True,
                    "browserstack.localIdentifier": identifier,
                    "client.playwrightVersion": "1.62.0",
                    "project": "Claude-Code-Mini-App",
                    "build": "ui-review-" + os.environ.get("GITHUB_RUN_ID", "local"),
                    "name": "UI-" + PROFILE,
                    "browserstack.video": True,
                }
                endpoint = "wss://cdp.browserstack.com/playwright?caps=" + quote(json.dumps(caps))
                if PROFILE == "android":
                    device = p.android.connect(endpoint, timeout=120000)
                    context = device.launch_browser()
                else:
                    browser = p.chromium.connect(endpoint, timeout=120000)
            if context is None:
                options = {"no_viewport": True} if NATIVE else {}
                if app["url"].startswith("https:"):
                    options["ignore_https_errors"] = True  # Isolated fixture's ephemeral self-signed certificate.
                context = browser.new_context(**options)
            current = context.new_page()
            current.set_default_timeout(30000)

            # Test-only safe diagnostic listeners: attached only under explicit env flag
            if DIAGNOSTICS_ENABLED:
                def on_diag_pageerror(err):
                    name = getattr(err, "name", type(err).__name__)
                    msg = str(err)
                    stack = getattr(err, "stack", "")
                    log_diag_event(
                        "pageerror",
                        case=CURRENT_CASE.get("name", "unknown"),
                        error_name=name,
                        message=mask_sensitive(msg, max_len=MAX_STRING_LEN),
                        stack=mask_sensitive(stack, max_len=MAX_STACK_LEN),
                    )

                def on_diag_request(req):
                    raw_url = req.url
                    if not is_session_path(raw_url):
                        return
                    log_diag_event(
                        "session_request",
                        case=CURRENT_CASE.get("name", "unknown"),
                        method=req.method,
                        safe_url=sanitize_url(raw_url),
                        safe_path=urlsplit(raw_url).path if raw_url else "",
                    )

                def on_diag_response(resp):
                    raw_url = resp.url
                    if not is_session_path(raw_url):
                        return
                    safe_loc = ""
                    if "location" in resp.headers:
                        safe_loc = sanitize_url(resp.headers["location"])
                    log_diag_event(
                        "session_response",
                        case=CURRENT_CASE.get("name", "unknown"),
                        method=resp.request.method,
                        status=resp.status,
                        safe_url=sanitize_url(raw_url),
                        safe_path=urlsplit(raw_url).path if raw_url else "",
                        safe_location=safe_loc,
                    )

                def on_diag_requestfailed(req):
                    raw_url = req.url
                    if not is_session_path(raw_url):
                        return
                    failure_text = mask_sensitive(str(req.failure), max_len=MAX_STRING_LEN) if req.failure else ""
                    log_diag_event(
                        "session_requestfailed",
                        case=CURRENT_CASE.get("name", "unknown"),
                        method=req.method,
                        safe_url=sanitize_url(raw_url),
                        safe_path=urlsplit(raw_url).path if raw_url else "",
                        failure_text=failure_text,
                    )

                current.on("pageerror", on_diag_pageerror)
                current.on("request", on_diag_request)
                current.on("response", on_diag_response)
                current.on("requestfailed", on_diag_requestfailed)

            yield current
        finally:
            if DIAGNOSTICS_ENABLED:
                try:
                    diag_dir = Path("test-results/ui") / PROFILE
                    diag_dir.mkdir(parents=True, exist_ok=True)
                    summary = {
                        "profile": PROFILE,
                        "correlation_notice": "Non-atomic correlation across browser, proxy and app events using method, sanitized path, and timestamps; note: proxy observes HTTP request dispatch, not TCP/TLS transport accept.",
                        "redaction_policy": "pattern-based masking of credentials, raw and URL-encoded access keys, query parameters, userinfo, and fragments in all freeform fields",
                        "total_events": len(DIAGNOSTIC_EVENTS),
                        "events_limit": MAX_EVENTS,
                        "events_truncated": EVENTS_TRUNCATED,
                        "file_write_error": FILE_WRITE_ERROR,
                        "diagnostic_status": "BLOCKED_WRITE_ERROR" if FILE_WRITE_ERROR else ("TRUNCATED" if EVENTS_TRUNCATED else "RECORDED"),
                        "pageerror_count": sum(1 for e in DIAGNOSTIC_EVENTS if e.get("event") == "pageerror"),
                        "session_request_count": sum(1 for e in DIAGNOSTIC_EVENTS if e.get("event") == "session_request"),
                        "session_response_count": sum(1 for e in DIAGNOSTIC_EVENTS if e.get("event") == "session_response"),
                        "session_failure_count": sum(1 for e in DIAGNOSTIC_EVENTS if e.get("event") == "session_requestfailed"),
                        "recent_pageerrors": [e for e in DIAGNOSTIC_EVENTS if e.get("event") == "pageerror"][:10],
                        "recent_session_requests": [e for e in DIAGNOSTIC_EVENTS if e.get("event") == "session_request"][:10],
                    }
                    (diag_dir / "diagnostic-summary.json").write_text(json.dumps(summary, ensure_ascii=False, indent=2), encoding="utf-8")
                except Exception as ex:
                    print("DIAGNOSTIC_ERROR: summary write failed", type(ex).__name__, flush=True)
            try:
                if current and PROFILE != "local-edge":
                    status = "failed" if request.session.testsfailed else "passed"
                    current.evaluate("_ => {}", "browserstack_executor: " + json.dumps({
                        "action": "setSessionStatus", "arguments": {"status": status, "reason": "UI screenshots and layout checks"},
                    }))
            finally:
                try:
                    if context:
                        context.close()
                    if device:
                        device.close()
                    if browser:
                        browser.close()
                finally:
                    if tunnel:
                        tunnel.stop()


@pytest.fixture(autouse=True)
def final_snapshot(page, request):
    yield
    viewport = request.node.callspec.params["viewport"]
    folder = Path("test-results/ui") / PROFILE / (str(viewport[0]) if viewport else "native")
    folder.mkdir(parents=True, exist_ok=True)
    page.screenshot(path=str(folder / "99-final.png"), animations="disabled")


def capture(page, folder, stage, issues, control=None):
    if DIAGNOSTICS_ENABLED:
        log_diag_event("capture", case=folder.name, stage=stage)
    page.screenshot(path=str(folder / (stage + ".png")), animations="disabled")
    metrics = page.evaluate("""() => ({
      width: innerWidth, height: innerHeight, scrollWidth: document.documentElement.scrollWidth,
      theme: document.documentElement.dataset.theme, userAgent: navigator.userAgent,
      coarsePointer: matchMedia('(pointer: coarse)').matches,
      visualViewport: visualViewport && {width: visualViewport.width, height: visualViewport.height,
        offsetTop: visualViewport.offsetTop, offsetLeft: visualViewport.offsetLeft}
    })""")
    if metrics["scrollWidth"] > metrics["width"] + 1:
        issues.append(stage + ": 頁面橫向溢出")
    if control is not None:
        bounds = control.bounding_box()
        metrics["control"] = bounds
        if not bounds or bounds["x"] < -1 or bounds["x"] + bounds["width"] > metrics["width"] + 1:
            issues.append(stage + ": 主要控制項超出水平範圍")
        if bounds and (bounds["y"] < -1 or bounds["y"] + bounds["height"] > metrics["height"] + 1):
            issues.append(stage + ": 主要控制項超出垂直範圍")
    (folder / (stage + ".json")).write_text(json.dumps(metrics, ensure_ascii=False, indent=2), encoding="utf-8")


@pytest.mark.parametrize("viewport", [None] if NATIVE else [(1280, 900), (768, 1024), (390, 844), (320, 640)])
def test_ui_review(page, app, viewport):
    if viewport:
        page.set_viewport_size({"width": viewport[0], "height": viewport[1]})
    size = "native" if viewport is None else str(viewport[0])
    CURRENT_CASE["name"] = size
    if DIAGNOSTICS_ENABLED:
        log_diag_event("case_start", case=size, viewport=list(viewport) if viewport else None)
    folder = Path("test-results/ui") / PROFILE / size
    folder.mkdir(parents=True, exist_ok=True)
    issues = []
    if PROFILE != "ios":
        if DIAGNOSTICS_ENABLED:
            log_diag_event("cookieclear_before", case=size)
        page.context.clear_cookies()
        if DIAGNOSTICS_ENABLED:
            log_diag_event("cookieclear_after", case=size)
        # Reset before app startup so the previous viewport cannot restore a chat.
        page.add_init_script("localStorage.clear(); localStorage.setItem('cc_theme_v1', 'dark');")
    # iOS bridge may return before the document has left about:blank.
    if DIAGNOSTICS_ENABLED:
        log_diag_event("goto_before", case=size, safe_url=sanitize_url(app["url"]))
    page.goto(app["url"], wait_until="domcontentloaded", timeout=60000)
    if DIAGNOSTICS_ENABLED:
        log_diag_event("goto_after", case=size, safe_url=sanitize_url(app["url"]))
    page.wait_for_function("document.getElementById('root')?.childElementCount > 0")
    password = page.locator('input[type="password"]')
    password.wait_for(state="visible")
    capture(page, folder, "01-login-dark", issues, page.get_by_role("button", name="進入", exact=True))
    password.fill(app["password"])
    page.get_by_role("button", name="進入", exact=True).click()
    page.get_by_role("button", name="建立新 Session", exact=True).wait_for(state="visible")
    page.get_by_text("載入會話中…", exact=True).wait_for(state="hidden", timeout=30000)
    capture(page, folder, "02-sessions-dark", issues)

    page.get_by_role("button", name="設定", exact=True).click()
    dialog = page.get_by_role("dialog")
    dialog.wait_for(state="visible")
    capture(page, folder, "03-settings-dark", issues)
    dialog.get_by_role("button", name="外觀", exact=True).click()
    capture(page, folder, "04-appearance-dark", issues)
    dialog.get_by_role("radio", name="淺色", exact=True).click()
    page.wait_for_function("document.documentElement.dataset.theme === 'light'")
    capture(page, folder, "05-appearance-light", issues)
    dialog.get_by_role("button", name="關閉", exact=True).click()
    dialog.wait_for(state="hidden")
    capture(page, folder, "06-sessions-light", issues)
    page.get_by_role("button", name="設定", exact=True).click()
    dialog.get_by_role("button", name="外觀", exact=True).click()
    dialog.get_by_role("radio", name="深色", exact=True).click()
    dialog.get_by_role("button", name="關閉", exact=True).click()

    page.get_by_role("button", name="建立新 Session", exact=True).click()
    name = "UI 檢查｜很長的會話名稱與中文排版檢查 " + PROFILE + " " + size
    page.get_by_placeholder("Session 名稱（選填，留空自動產生）").fill(name)
    capture(page, folder, "07-new-session-dark", issues)
    page.get_by_role("button", name="建立", exact=True).click()
    draft = page.locator("textarea[data-chat-input]")
    draft.wait_for(state="visible")
    page.wait_for_function("document.querySelector('textarea[data-chat-input]')?.disabled === false")
    page.get_by_text("載入對話中…", exact=True).wait_for(state="hidden", timeout=30000)
    page.evaluate("() => document.activeElement?.blur()")
    capture(page, folder, "08-chat-empty-dark", issues, draft)
    if page.evaluate("innerWidth < 1024") and not page.get_by_role("button", name="回到會話列表", exact=True).is_visible():
        issues.append("08-chat-empty-dark: 單欄聊天缺少返回會話列表按鈕")
    draft.click()
    draft.fill("這是一份應保留的多行草稿\n第二行測試中文輸入")
    capture(page, folder, "09-chat-draft-keyboard", issues, draft)
    page.evaluate("() => document.activeElement?.blur()")
    tools = page.get_by_role("button", name="聊天工具", exact=True)
    if tools.is_visible():
        tools.click()
        page.get_by_role("dialog").wait_for(state="visible")
        capture(page, folder, "10-tools-dark", issues, page.get_by_role("button", name="關閉聊天工具"))
        page.get_by_role("button", name="模型", exact=True).click()
        page.get_by_role("heading", name="選擇模型", exact=True).wait_for(state="visible")
        capture(page, folder, "11-models-dark", issues)
        page.get_by_role("button", name="關閉聊天工具", exact=True).click()
        assert draft.input_value() == "這是一份應保留的多行草稿\n第二行測試中文輸入"
    draft.fill("PERM")
    page.get_by_role("button", name="送出", exact=True).click()
    page.get_by_text("需要授權", exact=True).wait_for(state="visible", timeout=45000)
    capture(page, folder, "12-permission-dark", issues, page.get_by_role("button", name="拒絕", exact=True))
    page.get_by_role("button", name="拒絕", exact=True).click()
    page.get_by_text("需要授權", exact=True).wait_for(state="hidden")
    page.wait_for_function("[...document.querySelectorAll('button')].some(b => b.getAttribute('aria-label') === '送出' && b.disabled)", timeout=15000)
    capture(page, folder, "13-chat-history-dark", issues, draft)
    details = page.locator("details.ra-mobile-session-details")
    if details.is_visible():
        details.locator("summary").click()
    page.get_by_role("button", name="分享這個聊天室", exact=True).click()
    share = page.get_by_role("dialog", name="分享聊天室", exact=True)
    share.wait_for(state="visible")
    capture(page, folder, "14-share-dark", issues, share.get_by_role("button", name="關閉", exact=True))
    # A visible close button alone does not prove the scrollable form is usable.
    share_layout = page.evaluate("""() => {
      const dialog = document.querySelector('dialog.ra-dialog-share[open]');
      const body = dialog.children[1];
      const create = [...body.querySelectorAll('button')].find(b => b.textContent.trim() === '建立分享連結');
      return {dialogHeight: dialog.clientHeight, bodyHeight: body.clientHeight,
        bodyScrollHeight: body.scrollHeight, createHeight: create.getBoundingClientRect().height};
    }""")
    (folder / "14-share-layout.json").write_text(json.dumps(share_layout, indent=2), encoding="utf-8")
    if share_layout["bodyHeight"] + 1 < share_layout["createHeight"]:
        issues.append("14-share-dark: 分享內容區高度不足以完整呈現建立分享按鈕")
    share.get_by_role("button", name="關閉", exact=True).click()
    back = page.get_by_role("button", name="回到會話列表", exact=True)
    if page.evaluate("innerWidth < 1024") and back.is_visible():
        back.click()
        page.get_by_role("button", name="建立新 Session", exact=True).wait_for(state="visible")
    (folder / "issues.json").write_text(json.dumps(issues, ensure_ascii=False, indent=2), encoding="utf-8")
    if DIAGNOSTICS_ENABLED:
        log_diag_event("test_body_end", case=size)
    assert not issues, issues
