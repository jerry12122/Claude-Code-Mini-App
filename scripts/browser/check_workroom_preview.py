"""獨立畫面稿檢查，不啟動 server，也不連接正式工作室。"""
from pathlib import Path

from playwright.sync_api import expect, sync_playwright


repo = Path(__file__).resolve().parents[2]
preview = repo / "docs/plan/todo/workroom-optimization-preview.html"
artifacts = repo / "test-results/workroom-plan"
artifacts.mkdir(parents=True, exist_ok=True)

with sync_playwright() as playwright:
    browser = playwright.chromium.launch(channel="msedge", headless=True)
    page = browser.new_page(viewport={"width": 1440, "height": 1050})
    errors, external_requests = [], []
    page.on("pageerror", lambda error: errors.append(str(error)))
    page.on("request", lambda request: external_requests.append(request.url)
            if request.url.startswith(("http:", "https:", "ws:", "wss:")) else None)
    page.goto(preview.as_uri())

    for width in (1440, 1044, 816, 438, 390, 320):
        page.set_viewport_size({"width": width, "height": 1050})
        for theme in ("dark", "light"):
            page.locator("#theme").select_option(theme)
            for scenario in ("running", "decision", "quota"):
                page.locator("#scenario").select_option(scenario)
                assert page.evaluate("document.documentElement.scrollWidth <= innerWidth"), (width, theme, scenario)
                assert page.locator("#preview").evaluate("el => el.scrollWidth <= el.clientWidth + 1"), (width, theme, scenario)
                assert page.locator(".shell").evaluate("el => el.scrollHeight <= el.clientHeight + 1"), (width, theme, scenario)

    page.set_viewport_size({"width": 1440, "height": 1050})
    page.locator("#scenario").select_option("running")
    page.locator("#theme").select_option("dark")
    page.screenshot(path=str(artifacts / "desktop-running.png"))
    page.locator("#scenario").select_option("decision")
    page.locator("#theme").select_option("dark")
    page.locator("#jump-decision").click()
    expect(page.locator("#decision-card")).to_be_focused()
    page.screenshot(path=str(artifacts / "desktop-decision.png"))
    page.locator('[data-demo="已允許此操作"]').click()
    expect(page.locator("#prototype-status")).to_contain_text("未執行工具")

    page.locator("#device").select_option("phone")
    page.locator("#theme").select_option("light")
    page.locator("#open-members").click()
    expect(page.locator("#view-people")).to_be_visible()
    page.screenshot(path=str(artifacts / "phone-members.png"))
    page.locator("#close-board").click()
    expect(page.locator("#open-board")).to_be_focused()
    expect(page.locator(".thread")).to_be_visible()
    page.get_by_role("radio", name="輪詢恢復", exact=True).check()
    page.get_by_role("button", name="送出回答", exact=True).click()
    expect(page.locator("#prototype-status")).to_contain_text("未送出回答")
    page.locator("#draft").fill("畫面稿草稿")
    page.locator("#composer button[type=submit]").click()
    expect(page.locator("#draft")).to_have_value("畫面稿草稿")
    page.locator("#scenario").select_option("quota")
    page.locator("#view-paused").click()
    expect(page.locator("#view-people .pm-paused")).to_be_visible()
    page.locator("#close-board").click()
    page.screenshot(path=str(artifacts / "phone-quota.png"))

    assert not errors, errors
    assert not external_requests, external_requests
    browser.close()
print("workroom preview: 36 版面情境、操作與焦點檢查通過，無對外請求")
