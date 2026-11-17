"""保存成功畫面與版面證據，供逐張視覺檢查；不把尺寸模擬當成真機。"""
import json
import os
from pathlib import Path
from urllib.parse import quote
import uuid

import pytest
from playwright.sync_api import expect, sync_playwright

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


@pytest.fixture(scope="module")
def page(app, request):
    tunnel = browser = device = context = current = None
    with sync_playwright() as p:
        try:
            if PROFILE == "local-edge":
                browser = p.chromium.launch(channel="msedge", headless=True)
            else:
                from browserstack.local import Local
                identifier = "ui-" + uuid.uuid4().hex
                key = os.environ["BROWSERSTACK_ACCESS_KEY"]
                tunnel = Local()
                tunnel.start(key=key, localIdentifier=identifier)
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
                context = browser.new_context(**({"no_viewport": True} if NATIVE else {}))
            current = context.new_page()
            current.set_default_timeout(30000)
            yield current
        finally:
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


def capture(page, folder, stage, issues, control=None):
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
    folder = Path("test-results/ui") / PROFILE / size
    folder.mkdir(parents=True, exist_ok=True)
    issues = []
    page.context.clear_cookies()
    page.goto(app["url"])
    page.evaluate("() => { localStorage.clear(); localStorage.setItem('cc_theme_v1', 'dark'); }")
    page.reload()
    expect(page.get_by_role("textbox", name="密碼")).to_be_visible()
    capture(page, folder, "01-login-dark", issues, page.get_by_role("button", name="進入", exact=True))
    page.get_by_role("textbox", name="密碼").fill(app["password"])
    page.get_by_role("button", name="進入", exact=True).click()
    expect(page.get_by_role("button", name="建立新 Session", exact=True)).to_be_visible()
    capture(page, folder, "02-sessions-dark", issues)

    page.get_by_role("button", name="設定", exact=True).click()
    dialog = page.get_by_role("dialog")
    expect(dialog).to_be_visible()
    capture(page, folder, "03-settings-dark", issues)
    dialog.get_by_role("button", name="外觀", exact=True).click()
    capture(page, folder, "04-appearance-dark", issues)
    dialog.get_by_role("radio", name="淺色", exact=True).click()
    expect(page.locator("html")).to_have_attribute("data-theme", "light")
    capture(page, folder, "05-appearance-light", issues)
    dialog.get_by_role("button", name="關閉", exact=True).click()
    expect(dialog).to_have_count(0)
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
    expect(draft).to_be_enabled()
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
        expect(page.get_by_role("dialog")).to_be_visible()
        capture(page, folder, "10-tools-dark", issues, page.get_by_role("button", name="關閉聊天工具"))
        page.get_by_role("button", name="模型", exact=True).click()
        expect(page.get_by_role("heading", name="選擇模型", exact=True)).to_be_visible()
        capture(page, folder, "11-models-dark", issues)
        page.get_by_role("button", name="關閉聊天工具", exact=True).click()
        expect(draft).to_have_value("這是一份應保留的多行草稿\n第二行測試中文輸入")
    draft.fill("PERM")
    page.get_by_role("button", name="送出", exact=True).click()
    expect(page.get_by_text("需要授權", exact=True)).to_be_visible(timeout=45000)
    capture(page, folder, "12-permission-dark", issues, page.get_by_role("button", name="拒絕", exact=True))
    page.get_by_role("button", name="拒絕", exact=True).click()
    expect(page.get_by_text("需要授權", exact=True)).to_have_count(0)
    expect(page.get_by_role("button", name="送出", exact=True)).to_be_disabled(timeout=15000)
    capture(page, folder, "13-chat-history-dark", issues, draft)
    details = page.locator("details.ra-mobile-session-details")
    if details.is_visible():
        details.locator("summary").click()
    page.get_by_role("button", name="分享這個聊天室", exact=True).click()
    share = page.get_by_role("dialog", name="分享聊天室", exact=True)
    expect(share).to_be_visible()
    capture(page, folder, "14-share-dark", issues, share.get_by_role("button", name="關閉", exact=True))
    share.get_by_role("button", name="關閉", exact=True).click()
    (folder / "issues.json").write_text(json.dumps(issues, ensure_ascii=False, indent=2), encoding="utf-8")
    assert not issues, issues
