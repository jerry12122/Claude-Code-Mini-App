"""Run: python scripts/mobile-composer-mock.test.py (Playwright + Edge)."""
from pathlib import Path

from playwright.sync_api import sync_playwright

root = Path(__file__).resolve().parents[1]
url = (root / "docs/prototypes/mobile-composer-sheet.dc.html").as_uri()
screens = root / ".spectra/design-cache/mobile-composer-sheet"
screens.mkdir(parents=True, exist_ok=True)

with sync_playwright() as p:
    browser = p.chromium.launch(channel="msedge", headless=True)
    try:
        page = browser.new_page(viewport={"width": 390, "height": 844}, has_touch=True)
        errors = []
        page.on("pageerror", lambda error: errors.append(str(error)))
        page.goto(url)
        draft = page.get_by_role("textbox", name="訊息或指令")
        draft.fill("保留這份草稿\n還有第二行")
        height = draft.bounding_box()["height"]
        page.get_by_role("button", name="開啟聊天工具").click()
        assert page.locator("dialog").is_visible()
        assert draft.bounding_box()["height"] == height, "blur cannot collapse the draft"
        assert page.locator("select").count() == 0
        page.get_by_role("button", name="模型", exact=True).click()
        assert page.get_by_role("heading", name="選擇模型").is_visible()
        assert page.get_by_role("radio", name="預設模型", exact=True).is_checked()
        page.screenshot(path=str(screens / "mock-dark-models.png"), animations="disabled")
        page.get_by_role("radio", name="Opus", exact=True).check()
        assert page.locator("#tools-home").is_visible()
        assert page.locator("#model-value").inner_text() == "Opus"
        assert page.evaluate("document.activeElement.id") == "model"
        page.get_by_role("button", name="模型", exact=True).click()
        assert page.get_by_role("radio", name="Opus", exact=True).is_checked()
        page.get_by_role("button", name="返回聊天工具").click()
        page.get_by_role("button", name="權限", exact=True).click()
        page.locator('input[type="radio"][value="bypass"]').check()
        assert "danger" in page.locator("#permission").get_attribute("class")
        page.screenshot(path=str(screens / "mock-dark-tools.png"), animations="disabled")
        page.locator("#files").dispatch_event("cancel")
        assert page.locator("dialog").is_visible(), "picker cancel keeps tools open"
        with page.expect_file_chooser() as chosen:
            page.get_by_role("button", name="加入圖片或檔案").click()
        chosen.value.set_files({"name": "draft.txt", "mimeType": "text/plain", "buffer": b"demo"})
        assert not page.locator("dialog").is_visible()
        assert page.locator(".chip").inner_text() == "draft.txt"
        assert draft.input_value().startswith("保留這份草稿")
        assert page.evaluate("document.activeElement.id") == "plus"
        page.screenshot(path=str(screens / "mock-dark-draft.png"))
        page.get_by_role("button", name="切換淺色").click()
        page.get_by_role("button", name="開啟聊天工具").click()
        assert page.locator("#model-value").inner_text() == "Opus"
        page.screenshot(path=str(screens / "mock-light-tools.png"), animations="disabled")
        page.get_by_role("button", name="模型", exact=True).click()
        page.screenshot(path=str(screens / "mock-light-models.png"), animations="disabled")
        page.keyboard.press("Escape")
        assert page.locator("#tools-home").is_visible(), "Escape from a subpage goes back"
        assert page.locator("dialog").is_visible()
        page.get_by_role("button", name="模型", exact=True).click()
        page.keyboard.press("ArrowUp")
        assert page.locator("#model-value").inner_text() == "Sonnet", "native radio arrow keys select"
        page.get_by_role("button", name="模型", exact=True).click()
        page.get_by_role("radio", name="Sonnet", exact=True).click()
        assert page.locator("#tools-home").is_visible(), "choosing the current value also returns"
        page.keyboard.press("Escape")
        assert not page.locator("dialog").is_visible()
        page.get_by_role("button", name="開啟聊天工具").click()
        page.get_by_role("button", name="輸入模式", exact=True).click()
        page.get_by_role("radio", name="Shell", exact=True).check()
        assert page.locator("#tools-home").is_visible()
        assert not page.locator("#attach").is_visible()
        page.get_by_role("button", name="關閉聊天工具").click()
        assert not page.locator(".chip").is_visible()
        page.get_by_role("button", name="開啟聊天工具").click()
        assert not page.locator("#attach").is_visible()
        page.get_by_role("button", name="輸入模式", exact=True).click()
        page.get_by_role("radio", name="Claude", exact=True).check()
        page.get_by_role("button", name="關閉聊天工具").click()
        assert page.locator(".chip").is_visible(), "mode switch preserves attachments"
        for width, height in [(320, 640), (844, 390), (1280, 960)]:
            page.set_viewport_size({"width": width, "height": height})
            page.get_by_role("button", name="開啟聊天工具").click()
            assert page.evaluate("document.documentElement.scrollWidth <= innerWidth")
            if height == 390:
                before = page.evaluate("scrollY")
                page.mouse.move(width - 10, 100)
                page.mouse.wheel(0, 500)
                page.wait_for_timeout(150)
                assert page.evaluate("scrollY") == before, "an open sheet locks background scrolling"
            close = page.get_by_role("button", name="關閉聊天工具")
            assert 0 <= close.bounding_box()["y"] < height
            page.get_by_role("button", name="權限", exact=True).click()
            page.locator('input[type="radio"][value="default"]').check()
            assert page.locator("#permission-value").inner_text() == "標準"
            close.click()
        page.get_by_role("button", name="送出預覽訊息").click()
        assert page.locator(".user").last.inner_text().endswith("📎 draft.txt")
        assert not page.locator(".chip").count()
        page.get_by_role("button", name="開啟聊天工具").click()
        with page.expect_file_chooser() as chosen:
            page.get_by_role("button", name="加入圖片或檔案").click()
        chosen.value.set_files([
            {"name": name, "mimeType": "text/plain", "buffer": b"demo"}
            for name in ["first.txt", "second.txt"]
        ])
        assert not page.locator("dialog").is_visible()
        assert page.evaluate("document.activeElement.id") == "plus"
        page.get_by_role("button", name="移除 first.txt").focus()
        page.keyboard.press("Enter")
        assert page.evaluate("document.activeElement.getAttribute('aria-label')") == "移除 second.txt", page.evaluate("document.activeElement.outerHTML")
        page.keyboard.press("Enter")
        assert page.evaluate("document.activeElement.id") == "plus"
        assert page.locator("#send").is_disabled()
        page.get_by_role("button", name="開啟聊天工具").click()
        page.get_by_role("button", name="模型", exact=True).click()
        page.locator("dialog").dispatch_event("cancel", {"cancelable": True})
        assert page.locator("#tools-home").is_visible(), "platform cancel goes back within the sheet"
        page.mouse.click(5, 5)
        assert not page.locator("dialog").is_visible(), "backdrop dismisses the sheet"
        assert page.evaluate("getComputedStyle(document.documentElement).overflow") != "hidden"
        page.get_by_role("button", name="開啟聊天工具").click()
        page.evaluate("document.getElementById('close').click(); document.getElementById('plus').click()")
        page.evaluate("() => new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve)))")
        assert page.locator("dialog").is_visible(), "rapid reopening survives the previous close event"
        assert page.locator("#phone").get_attribute("data-tools-open") == "true"
        assert page.locator("#plus").get_attribute("aria-expanded") == "true"
        page.get_by_role("button", name="關閉聊天工具").click()
        assert not errors, errors
    finally:
        browser.close()

print("mobile composer mock checks passed: picker, draft, settings, themes, layouts, local send")
