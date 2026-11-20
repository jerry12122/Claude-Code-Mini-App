"""工作室與普通會話視覺一致性驗證：對照相同內容、相同 viewport／主題下的 computed style 與截圖。"""
import json
import os
from pathlib import Path
import uuid

import pytest
from playwright.sync_api import expect

OUT = Path(os.environ.get("WORKROOM_VISUAL_PARITY_ARTIFACTS", r"D:\Temp\miniapp-workroom-visual-parity-20261119\screens"))


def _create_env(page, login):
    OUT.mkdir(parents=True, exist_ok=True)
    login()
    suffix = uuid.uuid4().hex[:6]
    session_name = f"parity-ordinary-{suffix}"
    room_name = f"parity-room-{suffix}"

    # 1. 建立普通 Session
    page.get_by_role("button", name="建立新 Session", exact=True).click()
    page.get_by_placeholder("Session 名稱（選填，留空自動產生）").fill(session_name)
    page.get_by_role("button", name="建立", exact=True).click()
    draft = page.locator("textarea[data-chat-input]")
    expect(draft).to_be_enabled()
    draft.fill("視覺一致性測試：這是使用者的測試訊息。")
    page.get_by_role("button", name="送出", exact=True).click()
    # 等待 Claude 回覆
    expect(page.locator(".bubble-user")).to_be_visible()
    expect(page.locator(".bubble-claude")).to_be_visible()

    # 2. 建立 Room (1 worker, 1 PM)
    page.evaluate("""async name => {
      const post = async (url, body) => {
        const res = await apiFetch(url, {method:'POST', headers:{'Content-Type':'application/json'}, body:JSON.stringify(body)});
        if (!res.ok) throw new Error(await res.text());
        return res.json();
      };
      const worker = await post('/sessions', {name: name + ' worker', agent_type:'claude', permission_mode:'default'});
      const pm = await post('/sessions', {name: name + ' PM', agent_type:'claude', permission_mode:'default'});
      await post('/rooms', {name, members:[{session_id:worker.id, role:'worker'},{session_id:pm.id, role:'pm'}]});
    }""", room_name)

    page.reload()
    expect(page.locator(".ra-session-card").filter(has_text=session_name)).to_be_visible()
    room_card = page.locator(".room-list-card").filter(has_text=room_name)
    expect(room_card).to_be_visible()

    # 打開 Room 並送出相同內容
    room_card.locator("button.room-list-open").click()
    room_screen = page.get_by_role("region", name="工作室 " + room_name, exact=True)
    expect(room_screen).to_be_visible()
    room_draft = room_screen.locator("textarea[name='text']")
    expect(room_draft).to_be_enabled()
    room_draft.fill("視覺一致性測試：這是使用者的測試訊息。")
    room_screen.get_by_role("button", name="傳給 PM", exact=True).click()
    # 等待訊息出現
    # 成功接受的輸入先在獨立 queue；此測試必須等真正歷史氣泡，不能拿 local pending 冒充。
    expect(room_screen.locator(".room-timeline .bubble-user")).to_be_visible(timeout=30000)

    return session_name, room_name


def _get_styles(page, is_room=False):
    """取得關鍵視覺元素的 computed style。"""
    prefix = ".room-screen " if is_room else ""
    return page.evaluate("""prefix => {
      const g = (sel) => {
        const el = document.querySelector(prefix + sel);
        if (!el) return null;
        const cs = window.getComputedStyle(el);
        const rect = el.getBoundingClientRect();
        return {
          bg: cs.backgroundColor,
          color: cs.color,
          fontSize: cs.fontSize,
          lineHeight: cs.lineHeight,
          borderRadius: cs.borderRadius,
          padding: cs.padding,
          boxShadow: cs.boxShadow,
          height: rect.height,
          minHeight: cs.minHeight,
        };
      };
      return {
        bubbleUser: g('.bubble-user'),
        bubbleAssistant: g(prefix ? '.bubble-claude, .room-read' : '.bubble-claude'),
        composer: g(prefix ? '.room-composer' : '.ra-chat-composer'),
        cmdBar: g('.ra-cmd-bar'),
        textarea: g(prefix ? 'textarea[name="text"]' : 'textarea[data-chat-input]'),
        sendBtn: g(prefix ? '.room-send' : 'button[aria-label="送出"], button[aria-label="排入佇列"]'),
      };
    }""", prefix)


def _back_to_list(page):
    close_room = page.get_by_role("button", name="關閉工作室")
    if close_room.is_visible():
        close_room.click()
    back_session = page.get_by_role("button", name="回到會話列表")
    if back_session.is_visible():
        back_session.click()


def _open_session(page, name):
    _back_to_list(page)
    card = page.locator(".ra-session-card").filter(has_text=name)
    expect(card).to_be_visible()
    card.click()
    expect(page.locator(".bubble-user")).to_be_visible()


def _open_room(page, name):
    _back_to_list(page)
    card = page.locator(".room-list-card").filter(has_text=name)
    expect(card).to_be_visible()
    card.locator("button.room-list-open").click()
    expect(page.get_by_role("region", name="工作室 " + name)).to_be_visible()
    bounds = page.locator('.room-timeline').evaluate("""el => {
      const style = getComputedStyle(el), content = el.firstElementChild.getBoundingClientRect();
      return {available: el.clientWidth - parseFloat(style.paddingLeft) - parseFloat(style.paddingRight),
        width: content.width, leftGap: content.left - el.getBoundingClientRect().left - el.clientLeft - parseFloat(style.paddingLeft)};
    }""")
    assert abs(bounds['width'] - bounds['available']) <= 1 and abs(bounds['leftGap']) <= 1, bounds



def test_workroom_visual_parity(page, login):
    """驗證工作室與普通會話在 1280 深/淺、390/320 窄版下的視覺一致性與 computed style。"""
    session_name, room_name = _create_env(page, login)

    diff_log = {}

    # 1. 1280x800 Dark
    page.set_viewport_size({"width": 1280, "height": 800})
    page.evaluate("() => { document.documentElement.dataset.theme = 'dark'; }")

    _open_session(page, session_name)
    page.screenshot(path=str(OUT / "ordinary-1280-dark.png"))
    ordinary_styles_1280_dark = _get_styles(page, is_room=False)

    _open_room(page, room_name)
    page.screenshot(path=str(OUT / "room-1280-dark.png"))
    room_styles_1280_dark = _get_styles(page, is_room=True)

    diff_log["1280-dark"] = {
        "ordinary": ordinary_styles_1280_dark,
        "room": room_styles_1280_dark,
    }

    # 2. 1280x800 Light
    page.evaluate("() => { document.documentElement.dataset.theme = 'light'; }")

    _open_session(page, session_name)
    page.screenshot(path=str(OUT / "ordinary-1280-light.png"))
    ordinary_styles_1280_light = _get_styles(page, is_room=False)

    _open_room(page, room_name)
    page.screenshot(path=str(OUT / "room-1280-light.png"))
    room_styles_1280_light = _get_styles(page, is_room=True)

    diff_log["1280-light"] = {
        "ordinary": ordinary_styles_1280_light,
        "room": room_styles_1280_light,
    }

    # 3. 390x844 Dark
    page.evaluate("() => { document.documentElement.dataset.theme = 'dark'; }")
    page.set_viewport_size({"width": 390, "height": 844})

    _open_session(page, session_name)
    assert page.evaluate("document.documentElement.scrollWidth <= innerWidth")
    page.screenshot(path=str(OUT / "ordinary-390-dark.png"))

    _open_room(page, room_name)
    assert page.evaluate("document.documentElement.scrollWidth <= innerWidth")
    page.screenshot(path=str(OUT / "room-390-dark.png"))

    # 4. 320x640 Dark
    page.set_viewport_size({"width": 320, "height": 640})

    _open_session(page, session_name)
    assert page.evaluate("document.documentElement.scrollWidth <= innerWidth")
    page.screenshot(path=str(OUT / "ordinary-320-dark.png"))

    _open_room(page, room_name)
    assert page.evaluate("document.documentElement.scrollWidth <= innerWidth")
    page.screenshot(path=str(OUT / "room-320-dark.png"))

    (OUT / "visual-diff.json").write_text(json.dumps(diff_log, ensure_ascii=False, indent=2), encoding="utf-8")

    # 斷言關鍵項目必須一致
    # A. 氣泡顏色與陰影必須一致（深色與淺色）
    assert room_styles_1280_dark["bubbleUser"]["bg"] == ordinary_styles_1280_dark["bubbleUser"]["bg"], (
        f"Bubble user background mismatch (dark): room={room_styles_1280_dark['bubbleUser']['bg']} vs "
        f"ordinary={ordinary_styles_1280_dark['bubbleUser']['bg']}"
    )
    assert room_styles_1280_light["bubbleUser"]["bg"] == ordinary_styles_1280_light["bubbleUser"]["bg"], (
        f"Bubble user background mismatch (light): room={room_styles_1280_light['bubbleUser']['bg']} vs "
        f"ordinary={ordinary_styles_1280_light['bubbleUser']['bg']}"
    )
    assert room_styles_1280_dark["bubbleUser"]["boxShadow"] == ordinary_styles_1280_dark["bubbleUser"]["boxShadow"], (
        f"Bubble user boxShadow mismatch: room={room_styles_1280_dark['bubbleUser']['boxShadow']} vs "
        f"ordinary={ordinary_styles_1280_dark['bubbleUser']['boxShadow']}"
    )

    # B. Composer 背景必須一致（深色與淺色）
    assert room_styles_1280_dark["composer"]["bg"] == ordinary_styles_1280_dark["composer"]["bg"], (
        f"Composer background mismatch (dark): room={room_styles_1280_dark['composer']['bg']} vs "
        f"ordinary={ordinary_styles_1280_dark['composer']['bg']}"
    )
    assert room_styles_1280_light["composer"]["bg"] == ordinary_styles_1280_light["composer"]["bg"], (
        f"Composer background mismatch (light): room={room_styles_1280_light['composer']['bg']} vs "
        f"ordinary={ordinary_styles_1280_light['composer']['bg']}"
    )

    # C. Textarea font-size 與 line-height 必須一致
    assert room_styles_1280_dark["textarea"]["fontSize"] == ordinary_styles_1280_dark["textarea"]["fontSize"], (
        f"Textarea font-size mismatch: room={room_styles_1280_dark['textarea']['fontSize']} vs "
        f"ordinary={ordinary_styles_1280_dark['textarea']['fontSize']}"
    )
    assert room_styles_1280_dark["textarea"]["lineHeight"] == ordinary_styles_1280_dark["textarea"]["lineHeight"], (
        f"Textarea line-height mismatch: room={room_styles_1280_dark['textarea']['lineHeight']} vs "
        f"ordinary={ordinary_styles_1280_dark['textarea']['lineHeight']}"
    )

    # D. Send Button 背景與圓角必須一致
    assert room_styles_1280_dark["sendBtn"]["bg"] == ordinary_styles_1280_dark["sendBtn"]["bg"], (
        f"Send button background mismatch: room={room_styles_1280_dark['sendBtn']['bg']} vs "
        f"ordinary={ordinary_styles_1280_dark['sendBtn']['bg']}"
    )
    assert room_styles_1280_dark["sendBtn"]["borderRadius"] == ordinary_styles_1280_dark["sendBtn"]["borderRadius"], (
        f"Send button border-radius mismatch: room={room_styles_1280_dark['sendBtn']['borderRadius']} vs "
        f"ordinary={ordinary_styles_1280_dark['sendBtn']['borderRadius']}"
    )
