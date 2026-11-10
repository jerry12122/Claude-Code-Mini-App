"""Run with: python scripts/composer-attachment.test.py (Playwright + Edge)."""
import re
from pathlib import Path

from playwright.sync_api import sync_playwright


root = Path(__file__).resolve().parents[1] / "internal/static"
styles = "\n".join(re.findall(r"<style[^>]*>(.*?)</style>", (root / "index.html").read_text(encoding="utf-8"), re.S))

with sync_playwright() as p:
    browser = p.chromium.launch(channel="msedge", headless=True)
    try:
        for width, height in [(390, 844), (844, 390), (1280, 900)]:
            page = browser.new_page(viewport={"width": width, "height": height}, has_touch=True)
            page.set_content(f"""<style>{styles}</style>
                <div class="ra-chat-composer"><div class="ra-cmd-bar">
                  <div class="ra-composer-input"><textarea data-chat-input></textarea></div>
                  <div class="ra-composer-tools">
                    <input id="attachment" type="file" multiple style="display:none">
                    <button type="button" onclick="document.getElementById('attachment').click()">Attach</button>
                  </div>
                </div></div>""")
            field = page.locator("textarea")
            button = page.get_by_role("button", name="Attach")
            field.focus()
            assert button.is_visible(), "attachment button must be visible while typing"
            # Opening a native picker can remove focus from the whole WebView.
            field.evaluate("el => el.blur()")
            assert button.is_visible(), f"{width}x{height}: losing keyboard focus hides the attachment controls"
            with page.expect_file_chooser() as picked:
                button.click()
            assert picked.value.is_multiple()
            button.evaluate("el => el.blur()")
            page.set_viewport_size({"width": width, "height": height + 200})
            assert button.is_visible(), "keyboard dismissal during the picker must keep its controls visible"
            picked.value.set_files({"name": "attachment.txt", "mimeType": "text/plain", "buffer": b"hello"})
            assert page.locator("#attachment").evaluate("el => el.files[0].name") == "attachment.txt"
            assert button.is_visible(), "attachment controls must survive returning from the picker"
            page.close()
    finally:
        browser.close()

print("composer attachment checks passed (portrait, landscape, desktop)")
