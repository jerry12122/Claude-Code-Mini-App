"""驗證普通會話佇列三列緊湊尺寸與截圖：24x24 按鈕、24~28px 列高、14~16px 圖示。"""
import json
import os
from pathlib import Path
import uuid

import pytest
from playwright.sync_api import expect

OUT = Path(os.environ.get("QUEUE_COMPACT_ARTIFACTS", r"D:\Temp\miniapp-private-queue-compact-20261119\screens"))


def test_queue_compact_geometry_and_screens(page, login, request):
    OUT.mkdir(parents=True, exist_ok=True)
    login()
    name = "compact-geom-" + uuid.uuid4().hex[:8]
    def cleanup():
        page.evaluate("""async name => {
          const res = await apiFetch('/sessions');
          const session = (await res.json()).find(s => s.name === name);
          if (session) await apiFetch('/sessions/' + encodeURIComponent(session.id), {method:'DELETE'});
        }""", name)
    request.addfinalizer(cleanup)
    page.get_by_role("button", name="建立新 Session", exact=True).click()
    page.get_by_placeholder("Session 名稱（選填，留空自動產生）").fill(name)
    page.get_by_role("button", name="建立", exact=True).click()
    draft = page.locator("textarea[data-chat-input]")
    expect(draft).to_be_enabled()

    # 先送出一則讓助理運行中，使後續訊息可以排入佇列並出現「立刻執行」
    draft.fill("QUEUE_RUN_NOW_UI_A")
    page.get_by_role("button", name="送出", exact=True).click()
    expect(page.get_by_text("queue-run-now partial A", exact=False)).to_be_visible(timeout=15000)

    # 排入三則訊息以產生三列佇列
    for text in ["排隊第一則：資料庫查詢優化", "排隊第二則：快取失效處理", "排隊第三則：邊界條件驗證"]:
        draft.fill(text)
        page.get_by_role("button", name="排入佇列", exact=True).click()
        expect(draft).to_have_value("")

    queue = page.get_by_label("排隊中的訊息", exact=True)
    rows = queue.locator("li")
    expect(rows).to_have_count(3)

    run_buttons = queue.get_by_role("button", name="立刻執行", exact=True)
    edit_buttons = queue.get_by_role("button", name="編輯", exact=True)
    remove_buttons = queue.get_by_role("button", name="移除這則排隊訊息", exact=True)

    expect(run_buttons).to_have_count(3)
    expect(edit_buttons).to_have_count(3)
    expect(remove_buttons).to_have_count(3)

    geometry_data = {}

    for width, height in [(1280, 800), (390, 844), (320, 640)]:
        page.set_viewport_size({"width": width, "height": height})
        assert page.evaluate("document.documentElement.scrollWidth <= innerWidth")

        vp_key = f"{width}x{height}"
        geometry_data[vp_key] = {
            "queueContainer": queue.bounding_box(),
            "rows": [],
            "buttons": {
                "runNow": [],
                "edit": [],
                "remove": [],
            },
        }

        # 檢查每列高度 24~28px
        for i, row in enumerate(rows.all()):
            rbox = row.bounding_box()
            assert 24 <= rbox["height"] <= 28, f"Row {i} height mismatch: {rbox}"
            geometry_data[vp_key]["rows"].append(rbox)

        # 檢查立刻執行按鈕 24x24，圖示 14~16px
        for i, btn in enumerate(run_buttons.all()):
            bbox = btn.bounding_box()
            assert 24 <= bbox["width"] <= 28 and 24 <= bbox["height"] <= 28, f"RunNow {i} bbox: {bbox}"
            assert btn.evaluate("el => el.querySelector('svg') !== null && el.innerText.trim() === ''")
            svg_box = btn.locator("svg").bounding_box()
            assert 14 <= svg_box["width"] <= 16 and 14 <= svg_box["height"] <= 16, f"RunNow SVG {i}: {svg_box}"
            geometry_data[vp_key]["buttons"]["runNow"].append({"btn": bbox, "svg": svg_box})

        # 檢查編輯按鈕 24x24，圖示 14~16px
        for i, btn in enumerate(edit_buttons.all()):
            bbox = btn.bounding_box()
            assert 24 <= bbox["width"] <= 28 and 24 <= bbox["height"] <= 28, f"Edit {i} bbox: {bbox}"
            assert btn.evaluate("el => el.querySelector('svg') !== null && el.innerText.trim() === ''")
            svg_box = btn.locator("svg").bounding_box()
            assert 14 <= svg_box["width"] <= 16 and 14 <= svg_box["height"] <= 16, f"Edit SVG {i}: {svg_box}"
            geometry_data[vp_key]["buttons"]["edit"].append({"btn": bbox, "svg": svg_box})

        # 檢查移除按鈕 24x24，圖示 14~16px
        for i, btn in enumerate(remove_buttons.all()):
            bbox = btn.bounding_box()
            assert 24 <= bbox["width"] <= 28 and 24 <= bbox["height"] <= 28, f"Remove {i} bbox: {bbox}"
            assert btn.evaluate("el => el.querySelector('svg') !== null && el.innerText.trim() === ''")
            svg_box = btn.locator("svg").bounding_box()
            assert 14 <= svg_box["width"] <= 16 and 14 <= svg_box["height"] <= 16, f"Remove SVG {i}: {svg_box}"
            geometry_data[vp_key]["buttons"]["remove"].append({"btn": bbox, "svg": svg_box})

        page.screenshot(path=str(OUT / f"queue-3rows-{width}.png"))

    (OUT / "geometry.json").write_text(json.dumps(geometry_data, ensure_ascii=False, indent=2), encoding="utf-8")
