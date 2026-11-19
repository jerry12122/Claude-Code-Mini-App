"""設定畫面重排、對比、分組留白、真實分享與 200% 重排。沿用 conftest 的隔離正式版 server 與假 CLI。

200% 用 CDP 半 CSS viewport（640×400）與 deviceScaleFactor 2，不用 style.zoom。
未涵蓋 Wails 與 Telegram 容器。截圖在 test-results/settings-design/（相對 pytest 工作目錄）。

    python -m pytest scripts/browser/test_settings_design.py -q --browser-channel msedge
"""
import json
import re
from pathlib import Path

import pytest
from playwright.sync_api import expect

OUT = Path("test-results/settings-design")

SECTIONS = [
    ("一般", "開啟 VSCode", "general"),
    ("外觀", "對話文字大小", "appearance"),
    ("分享", "沒有進行中的分享。在聊天室頂欄按「分享」建立。", "shares"),
    ("系統資源", "伺服器資源", "system"),
    ("日誌", "暫停", "logs"),
    ("更新", "版本資訊", "update"),
]


def shot(page, name):
    path = OUT / f"{name}.png"
    path.parent.mkdir(parents=True, exist_ok=True)
    page.screenshot(path=str(path), animations="disabled")


def layout_ok(page):
    return page.evaluate(
        """() => {
          const dialog = document.querySelector('dialog[open]');
          const root = document.documentElement;
          const close = dialog && [...dialog.querySelectorAll('button')]
            .find((b) => (b.textContent || '').trim() === '關閉');
          const b = close && close.getBoundingClientRect();
          const d = dialog && dialog.getBoundingClientRect();
          return {
            page: root.scrollWidth <= root.clientWidth + 1,
            dialog: !dialog || dialog.scrollWidth <= dialog.clientWidth + 1,
            hidden: !dialog || getComputedStyle(dialog).overflow !== 'scroll',
            close: !!(b && d && b.top >= d.top - 1 && b.bottom <= d.bottom + 1
              && b.left >= d.left - 1 && b.right <= d.right + 1 && b.height >= 36),
          };
        }"""
    )


def open_settings(page):
    opener = page.get_by_role("button", name="設定", exact=True)
    opener.click()
    dialog = page.locator("dialog.ra-dialog-settings")
    expect(dialog).to_be_visible()
    return opener, dialog


def dialog_name(dialog):
    return dialog.evaluate(
        """el => {
          const id = el.getAttribute('aria-labelledby');
          const node = id && el.ownerDocument.getElementById(id);
          return (node && node.textContent || '').replace(/\\s+/g, ' ').trim();
        }"""
    )


def paint_contrast(page, button_name):
    return page.evaluate(
        """(name) => {
          const dialog = document.querySelector('dialog.ra-dialog-settings');
          const save = [...dialog.querySelectorAll('button')].find((b) => (b.textContent || '').trim() === name);
          const rgba = (css) => {
            const ctx = document.createElement('canvas').getContext('2d');
            ctx.fillStyle = '#000';
            ctx.fillStyle = css;
            ctx.fillRect(0, 0, 1, 1);
            return [...ctx.getImageData(0, 0, 1, 1).data];
          };
          const cs = getComputedStyle(save);
          const fg = rgba(cs.color);
          const bg = rgba(cs.backgroundColor);
          const lin = (v) => {
            const c = v / 255;
            return c <= 0.04045 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4;
          };
          const lum = (c) => 0.2126 * lin(c[0]) + 0.7152 * lin(c[1]) + 0.0722 * lin(c[2]);
          const l1 = lum(fg);
          const l2 = lum(bg);
          return {fg, bg, ratio: (Math.max(l1, l2) + 0.05) / (Math.min(l1, l2) + 0.05)};
        }""",
        button_name,
    )


def group_gaps(page):
    return page.evaluate(
        """() => {
          const dialog = document.querySelector('dialog.ra-dialog-settings');
          const legend = [...dialog.querySelectorAll('legend')].find((n) => n.textContent.includes('Markdown'));
          const field = legend && legend.closest('fieldset').parentElement;
          const prev = field && field.previousElementSibling;
          const title = [...dialog.querySelectorAll('div')].find((n) => (n.textContent || '').trim() === '開啟 VSCode');
          const block = title && title.parentElement;
          const before = block && block.previousElementSibling;
          const box = (el) => el.getBoundingClientRect();
          const edged = field || block;
          return {
            legendGap: legend ? legend.getBoundingClientRect().top - box(field).top : null,
            lineGap: field && prev ? box(field).top - box(prev).bottom : null,
            titleGap: title ? title.getBoundingClientRect().top - box(block).top : null,
            blockGap: block && before ? box(block).top - box(before).bottom : null,
            border: edged ? parseFloat(getComputedStyle(edged).borderTopWidth) || 0 : null,
          };
        }"""
    )


def enter(dialog, label):
    nav = dialog.get_by_role("navigation", name="設定分類")
    if nav.count() == 0:
        dialog.get_by_role("button", name="返回設定分類").click()
        expect(nav).to_be_visible()
    nav.get_by_role("button", name=re.compile("^" + re.escape(label))).click()


@pytest.mark.parametrize(
    "width,height,theme",
    [
        (1280, 800, "dark"),
        (1280, 800, "light"),
        (960, 800, "dark"),
        (960, 800, "light"),
        (768, 1024, "dark"),
        (768, 1024, "light"),
        (390, 844, "dark"),
        (390, 844, "light"),
        (320, 640, "dark"),
        (320, 640, "light"),
        (1280, 480, "dark"),
    ],
)
def test_settings_sections_fit(page, login, width, height, theme):
    page.set_viewport_size({"width": width, "height": height})
    login()
    _, dialog = open_settings(page)
    enter(dialog, "外觀")
    dialog.get_by_role("radio", name="淺色" if theme == "light" else "深色", exact=True).click()
    expect(page.locator("html")).to_have_attribute("data-theme", theme)
    for label, marker, slug in SECTIONS:
        enter(dialog, label)
        if label == "分享":
            expect(
                dialog.get_by_text("沒有進行中的分享。在聊天室頂欄按「分享」建立。")
                .or_(dialog.locator("[data-share-id]").first)
            ).to_be_visible()
        else:
            expect(dialog.get_by_text(marker, exact=True).first).to_be_visible()
        back = dialog.get_by_role("button", name="返回設定分類")
        assert dialog_name(dialog) == (label if back.count() else "設定")
        if label == "一般" and width <= 639:
            size = page.locator("#quota-guard-percent").evaluate(
                "el => parseFloat(getComputedStyle(el).fontSize)"
            )
            assert size >= 16
            box = dialog.get_by_role("button", name="關閉", exact=True).bounding_box()
            assert box["height"] >= 47
        assert layout_ok(page) == {
            "page": True,
            "dialog": True,
            "hidden": True,
            "close": True,
        }
        shot(page, f"{width}x{height}/{theme}/{slug}")
    if width > 639:
        enter(dialog, "一般")
        expect(dialog.get_by_role("button", name="儲存", exact=True)).to_have_count(0)
        expect(dialog.get_by_role("button", name="還原預設", exact=True)).to_have_count(0)


@pytest.mark.parametrize("width,height", [(1280, 800), (390, 844)])
def test_settings_focus_and_back(page, login, width, height):
    page.set_viewport_size({"width": width, "height": height})
    login()
    opener, dialog = open_settings(page)
    page.keyboard.press("Tab")
    assert page.evaluate("() => !!document.activeElement?.closest('dialog')")
    page.keyboard.press("Shift+Tab")
    assert page.evaluate("() => !!document.activeElement?.closest('dialog')")
    if width <= 639:
        enter(dialog, "一般")
        expect(dialog.get_by_role("button", name="返回設定分類")).to_be_focused()
        page.keyboard.press("Escape")
        expect(dialog.get_by_role("navigation", name="設定分類")).to_be_visible()
        assert dialog_name(dialog) == "設定"
        expect(dialog.get_by_role("button", name="一般", exact=True)).to_be_focused()
        page.keyboard.press("Escape")
    else:
        page.keyboard.press("Escape")
    expect(dialog).to_be_hidden()
    expect(opener).to_be_focused()


def test_appearance_save_reset_and_failure(page, login):
    page.set_viewport_size({"width": 1280, "height": 800})
    login()
    opener, dialog = open_settings(page)
    enter(dialog, "外觀")
    css = page.locator("#settings-custom-css")
    css.fill(".prose { color: pink; }")
    shot(page, "appearance/before-save")
    dialog.get_by_role("button", name="儲存", exact=True).click()
    expect(dialog.get_by_text("已儲存", exact=True)).to_be_visible()
    shot(page, "appearance/after-save")
    dialog.get_by_role("button", name="關閉", exact=True).click()
    expect(opener).to_be_focused()
    page.reload()
    expect(page.get_by_role("button", name="建立新 Session", exact=True)).to_be_visible()
    _, dialog = open_settings(page)
    enter(dialog, "外觀")
    expect(css).to_have_value(".prose { color: pink; }")
    css.fill(".prose { color: blue; }")
    dialog.get_by_role("button", name="還原預設", exact=True).click()
    expect(css).to_have_value("")
    shot(page, "appearance/reset-unsaved")
    dialog.get_by_role("button", name="關閉", exact=True).click()
    _, dialog = open_settings(page)
    enter(dialog, "外觀")
    expect(css).to_have_value(".prose { color: pink; }")
    css.fill(".prose { color: orange; }")

    def fail_put(route):
        if route.request.method == "PUT":
            route.fulfill(status=500, content_type="application/json", body='{"error":"伺服器未存到"}')
        else:
            route.continue_()

    page.route("**/settings/appearance", fail_put)
    dialog.get_by_role("button", name="儲存", exact=True).click()
    expect(dialog.get_by_role("alert")).to_contain_text("伺服器未存到")
    expect(css).to_have_value(".prose { color: orange; }")
    shot(page, "appearance/save-failed")
    page.unroute("**/settings/appearance", fail_put)
    dialog.get_by_role("button", name="儲存", exact=True).click()
    expect(dialog.get_by_text("已儲存", exact=True)).to_be_visible()
    page.reload()
    expect(page.get_by_role("button", name="建立新 Session", exact=True)).to_be_visible()
    _, dialog = open_settings(page)
    enter(dialog, "外觀")
    expect(css).to_have_value(".prose { color: orange; }")


def test_general_keeps_its_own_save(page, login):
    page.set_viewport_size({"width": 1280, "height": 800})
    login()
    _, dialog = open_settings(page)
    enter(dialog, "一般")
    expect(dialog.get_by_role("button", name="儲存", exact=True)).to_have_count(0)
    expect(dialog.get_by_role("button", name="還原預設", exact=True)).to_have_count(0)
    expect(dialog.get_by_role("button", name="儲存全域指令")).to_be_visible()
    box = page.locator("#vscode-no-admin")
    expect(box).to_be_enabled()
    with page.expect_response(lambda r: r.url.endswith("/settings/general") and r.request.method == "PUT") as saved:
        box.check()
    assert saved.value.ok
    page.reload()
    expect(page.get_by_role("button", name="建立新 Session", exact=True)).to_be_visible()
    _, dialog = open_settings(page)
    enter(dialog, "一般")
    expect(page.locator("#vscode-no-admin")).to_be_checked()
    shot(page, "general/immediate-save")


def test_secondary_sections(page, login):
    page.set_viewport_size({"width": 1280, "height": 800})
    login()
    _, dialog = open_settings(page)
    enter(dialog, "分享")
    expect(dialog.get_by_role("button", name=re.compile("^結束全部"))).to_be_visible()
    empty = dialog.get_by_text("沒有進行中的分享。在聊天室頂欄按「分享」建立。")
    expect(empty.or_(dialog.locator("[data-share-id]").first)).to_be_visible()
    shot(page, "states/shares-empty" if empty.count() else "states/shares")
    enter(dialog, "系統資源")
    expect(dialog.get_by_text("CPU", exact=True)).to_be_visible()
    expect(dialog.get_by_text("記憶體", exact=True)).to_be_visible()
    shot(page, "states/system")
    enter(dialog, "日誌")
    expect(dialog.get_by_role("button", name="暫停", exact=True)).to_be_visible()
    expect(dialog.get_by_text(re.compile("連線中…|已連線|沒有符合的日誌")).first).to_be_visible()
    shot(page, "states/logs")
    enter(dialog, "更新")
    expect(dialog.get_by_text(re.compile("不會安裝或重新啟動|請由主機管理者更新|請自行重新整理"))).to_be_visible()
    expect(dialog.get_by_role("button", name=re.compile("下載|重新啟動|確定重新啟動"))).to_have_count(0)
    shot(page, "states/update-web")


def _assert_ratio(sample, theme, state):
    assert sample["ratio"] >= 4.5, {"theme": theme, "state": state, **sample}


def test_settings_contrast_spacing_and_focus(page, login):
    page.set_viewport_size({"width": 1280, "height": 800})
    login()
    _, dialog = open_settings(page)
    enter(dialog, "一般")
    general = group_gaps(page)
    assert general["titleGap"] >= 16 and general["blockGap"] >= 16 and general["border"] >= 1, general
    shot(page, "desktop/general")
    enter(dialog, "外觀")
    appearance = group_gaps(page)
    assert appearance["legendGap"] >= 16 and appearance["lineGap"] >= 16, appearance
    assert appearance["border"] >= 1, appearance
    shot(page, "desktop/appearance")
    save = dialog.get_by_role("button", name="儲存", exact=True)
    _assert_ratio(paint_contrast(page, "儲存"), "dark", "rest")
    save.hover()
    _assert_ratio(paint_contrast(page, "儲存"), "dark", "hover")
    dialog.get_by_role("button", name="關閉", exact=True).focus()
    page.keyboard.press("Tab")
    outline = page.evaluate(
        """() => {
          const el = document.activeElement;
          const s = getComputedStyle(el);
          return {text: (el.textContent || '').trim(), style: s.outlineStyle, width: parseFloat(s.outlineWidth) || 0};
        }"""
    )
    assert outline["text"] == "儲存", outline
    assert outline["style"] != "none" and outline["width"] >= 1, outline
    dialog.get_by_role("radio", name="淺色", exact=True).click()
    expect(page.locator("html")).to_have_attribute("data-theme", "light")
    _assert_ratio(paint_contrast(page, "儲存"), "light", "rest")
    save.hover()
    _assert_ratio(paint_contrast(page, "儲存"), "light", "hover")
    page.set_viewport_size({"width": 390, "height": 844})
    expect(dialog.get_by_role("heading", name="外觀", exact=True)).to_be_visible()
    assert dialog_name(dialog) == "外觀"
    assert save.bounding_box()["height"] >= 47
    shot(page, "mobile/appearance")
    enter(dialog, "一般")
    assert dialog_name(dialog) == "一般"
    instruction = dialog.get_by_role("button", name="儲存全域指令")
    assert instruction.bounding_box()["height"] >= 47
    shot(page, "mobile/general")


def test_settings_reflow_at_200(page, login):
    page.set_viewport_size({"width": 1280, "height": 800})
    login()
    _, dialog = open_settings(page)
    enter(dialog, "外觀")
    zoom = page.context.new_cdp_session(page)
    try:
        zoom.send("Emulation.setDeviceMetricsOverride", {
            "width": 640,
            "height": 400,
            "deviceScaleFactor": 2,
            "mobile": False,
        })
        viewport = page.evaluate("() => ({w: innerWidth, h: innerHeight, dpr: devicePixelRatio})")
        assert viewport == {"w": 640, "h": 400, "dpr": 2}, viewport
        reach = page.evaluate(
            """() => {
              const dialog = document.querySelector('dialog.ra-dialog-settings');
              const pick = (name) => [...dialog.querySelectorAll('button')].find((b) => (b.textContent || '').trim() === name);
              const close = pick('關閉');
              const save = pick('儲存');
              const d = dialog.getBoundingClientRect();
              const boxes = [close, save].map((el) => el.getBoundingClientRect());
              const h = innerHeight;
              const w = innerWidth;
              const seen = (b) => b.top >= -1 && b.bottom <= h + 1 && b.left >= -1 && b.right <= w + 1
                && b.top >= d.top - 1 && b.bottom <= d.bottom + 1;
              return {
                dialog: d.top >= -1 && d.bottom <= h + 1 && d.left >= -1 && d.right <= w + 1,
                close: seen(boxes[0]),
                save: seen(boxes[1]),
                dialogBottom: d.bottom,
                closeBottom: boxes[0].bottom,
                saveBottom: boxes[1].bottom,
              };
            }"""
        )
        assert reach["dialog"] and reach["close"] and reach["save"], reach
        shot(page, "zoom-200/reflow")
    finally:
        zoom.send("Emulation.clearDeviceMetricsOverride")
        zoom.detach()


LONG_SHARE = "設定分享長名稱驗收" + ("欄位" * 12)
SHORT_SHARE = "已撤銷對照室"


def _api(response):
    if response.ok:
        if response.status == 204:
            return None
        return response.json()
    raise AssertionError(f"{response.status} {response.text()}")


def test_settings_real_shares(page, login):
    page.set_viewport_size({"width": 1280, "height": 800})
    login()
    headers = {"Content-Type": "application/json", "Authorization": "Bearer " + page.evaluate("localStorage.getItem(WEB_SESSION_STORAGE_KEY)")}
    active_session = _api(page.request.post(
        page.url.rstrip("/") + "/sessions",
        data=json.dumps({"name": LONG_SHARE, "agent_type": "claude"}),
        headers=headers,
    ))
    ended_session = _api(page.request.post(
        page.url.rstrip("/") + "/sessions",
        data=json.dumps({"name": SHORT_SHARE, "agent_type": "claude"}),
        headers=headers,
    ))
    _api(page.request.post(
        page.url.rstrip("/") + f"/sessions/{active_session['id']}/shares",
        data=json.dumps({"ttl": 3600, "role": "viewer", "mode": "live"}),
        headers=headers,
    ))
    ended = _api(page.request.post(
        page.url.rstrip("/") + f"/sessions/{ended_session['id']}/shares",
        data=json.dumps({"ttl": 3600, "role": "viewer", "mode": "live"}),
        headers=headers,
    ))
    revoked = page.request.delete(page.url.rstrip("/") + f"/shares/{ended['id']}", headers=headers)
    assert revoked.status == 204

    def show(width, height):
        page.evaluate("() => document.querySelector('dialog.ra-dialog-settings')?.close()")
        page.set_viewport_size({"width": width, "height": height})
        _, dialog = open_settings(page)
        enter(dialog, "分享")
        expect(dialog.locator("[data-share-id]", has_text=LONG_SHARE)).to_be_visible()
        return dialog

    dialog = show(390, 844)
    assert dialog_name(dialog) == "分享"
    expect(dialog.locator("[data-share-id]", has_text=SHORT_SHARE)).to_have_count(0)
    dialog.get_by_role("button", name="已結束", exact=True).click()
    ended_row = dialog.locator("[data-share-id]", has_text=SHORT_SHARE)
    expect(ended_row).to_be_visible()
    expect(ended_row).to_contain_text("已結束")
    expect(dialog.locator("[data-share-id]", has_text=LONG_SHARE)).to_have_count(0)
    dialog.get_by_role("button", name="全部", exact=True).click()
    expect(dialog.locator("[data-share-id]", has_text=LONG_SHARE)).to_be_visible()
    expect(ended_row).to_be_visible()

    def rows_fit(dialog):
        fit = page.evaluate(
            """() => {
              const dialog = document.querySelector('dialog.ra-dialog-settings');
              const d = dialog.getBoundingClientRect();
              return [...dialog.querySelectorAll('[data-share-id]')].map((row) => {
                const copy = [...row.querySelectorAll('button')].find((b) => (b.textContent || '').trim() === '複製連結');
                copy.scrollIntoView({block: 'nearest', inline: 'nearest'});
                const b = copy.getBoundingClientRect();
                const r = row.getBoundingClientRect();
                return {
                  scroll: row.scrollWidth <= row.clientWidth + 1,
                  right: r.right <= d.right + 1,
                  copyHeight: b.height,
                  enabled: !copy.disabled,
                  copyInside: b.left >= d.left - 1 && b.right <= d.right + 1 && b.bottom > 0 && b.top < innerHeight,
                };
              });
            }"""
        )
        assert fit and all(item["scroll"] and item["right"] and item["enabled"] and item["copyInside"] and item["copyHeight"] >= 47 for item in fit), fit
        return fit

    rows_fit(dialog)
    dialog.locator("[data-share-id]", has_text=LONG_SHARE).get_by_role("button", name="複製連結", exact=True).click()
    shot(page, "mobile/shares")
    dialog = show(320, 640)
    assert dialog_name(dialog) == "分享"
    dialog.get_by_role("button", name="全部", exact=True).click()
    rows_fit(dialog)
    shot(page, "320/shares")
