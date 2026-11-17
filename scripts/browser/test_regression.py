"""真實 HTTP／WebSocket／SQLite；agent CLI 使用既有 fixture。"""
import re
import uuid

import pytest
from playwright.sync_api import expect


def create_chat(page, login):
    login()
    page.get_by_role("button", name="建立新 Session", exact=True).click()
    page.get_by_placeholder("Session 名稱（選填，留空自動產生）").fill("browser-" + uuid.uuid4().hex[:8])
    page.get_by_role("button", name="建立", exact=True).click()
    expect(page.locator("textarea[data-chat-input]")).to_be_enabled()


def test_login_rejection_and_reload(page, app, login):
    page.goto(app["url"])
    page.get_by_role("textbox", name="密碼").fill("incorrect-browser-test-password")
    with page.expect_response(lambda r: r.url.endswith("/auth/login")) as response:
        page.get_by_role("button", name="進入", exact=True).click()
    assert response.value.status == 401
    expect(page.get_by_role("textbox", name="密碼")).to_be_visible()
    login()
    expect(page.get_by_role("button", name="建立新 Session", exact=True)).to_be_visible()
    page.reload()
    expect(page.get_by_role("button", name="建立新 Session", exact=True)).to_be_visible()
    expect(page.get_by_role("textbox", name="密碼")).to_have_count(0)


def test_chat_stream_and_history(page, login):
    create_chat(page, login)
    page.locator("textarea[data-chat-input]").fill("BrowserStack 串流測試")
    page.get_by_role("button", name="送出", exact=True).click()
    expect(page.get_by_text(re.compile("第 6 段："))).to_be_visible(timeout=30000)
    expect(page.get_by_role("button", name="送出", exact=True)).to_be_disabled()
    page.reload()
    expect(page.locator("textarea[data-chat-input]")).to_be_enabled()
    expect(page.get_by_text(re.compile("第 6 段："))).to_be_visible()


def test_permission_denial(page, login):
    create_chat(page, login)
    page.locator("textarea[data-chat-input]").fill("PERM")
    page.get_by_role("button", name="送出", exact=True).click()
    expect(page.get_by_text("需要授權", exact=True)).to_be_visible(timeout=30000)
    page.get_by_role("button", name="拒絕", exact=True).click()
    expect(page.get_by_text("需要授權", exact=True)).to_have_count(0)
    expect(page.locator("textarea[data-chat-input]")).to_be_enabled()


def test_attachment_upload(page, login):
    create_chat(page, login)
    with page.expect_file_chooser() as picker:
        page.get_by_role("button", name="附加圖片或檔案", exact=True).click()
    picker.value.set_files({"name": "browser-test.txt", "mimeType": "text/plain", "buffer": b"isolated browser test"})
    expect(page.get_by_text("browser-test.txt", exact=True)).to_be_visible()
    expect(page.get_by_role("button", name="送出", exact=True)).to_be_enabled(timeout=30000)
    page.get_by_role("button", name="送出", exact=True).click()
    expect(page.get_by_text(re.compile("第 6 段："))).to_be_visible(timeout=30000)
    page.reload()
    expect(page.get_by_text("browser-test.txt", exact=True)).to_be_visible()


@pytest.mark.parametrize("width,height", [(1280, 900), (390, 844), (320, 640)])
def test_settings_theme_and_layout(page, login, width, height):
    page.set_viewport_size({"width": width, "height": height})
    login()
    page.get_by_role("button", name="設定", exact=True).click()
    dialog = page.get_by_role("dialog")
    expect(dialog).to_be_visible()
    dialog.get_by_role("button", name="外觀", exact=True).click()
    dialog.get_by_role("radio", name="淺色", exact=True).click()
    expect(page.locator("html")).to_have_attribute("data-theme", "light")
    assert page.evaluate("document.documentElement.scrollWidth <= innerWidth")
    dialog.get_by_role("button", name="關閉", exact=True).click()
    expect(dialog).to_have_count(0)
    page.reload()
    expect(page.locator("html")).to_have_attribute("data-theme", "light")
