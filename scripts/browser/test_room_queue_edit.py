"""真 DOM：工作室已接受的排隊輸入可原地編輯（PATCH /rooms/:id/messages/:queue_id）。

PM 以 fake CLI 的 PERM 關鍵字卡在待授權，之後經 composer 同一條 API 接受的輸入才會留在佇列。
只用自有 127.0.0.1 server／tempDB／fake CLI；不連真實 provider。
"""
import json
import os
import re
import uuid
from pathlib import Path

import pytest
from playwright.sync_api import expect

from test_workroom_polish import _open

OUT = Path(os.environ.get("ROOM_QUEUE_EDIT_ARTIFACTS", "test-results/room-queue-edit"))
PNG_B64 = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg=="


@pytest.fixture
def made_rooms(page):
    made = []
    yield made
    for ids in made:
        try:
            page.evaluate("""async ids => {
              await apiFetch('/rooms/' + ids.room, {method: 'DELETE'});
              for (const id of [ids.pm, ids.worker]) await apiFetch('/sessions/' + id, {method: 'DELETE'});
            }""", ids)
        except Exception:
            pass


def _make_room(page, login, name):
    login()
    ids = page.evaluate("""async name => {
      const post = async (url, body) => {
        const res = await apiFetch(url, {method:'POST', headers:{'Content-Type':'application/json'}, body:JSON.stringify(body)});
        if (!res.ok) throw new Error(await res.text());
        return res.json();
      };
      const mk = (n) => post('/sessions', {name: n, agent_type:'claude', permission_mode:'default'});
      const worker = await mk(name + ' worker');
      const pm = await mk(name + ' PM');
      const room = await post('/rooms', {name, members:[{session_id:pm.id, role:'pm'},{session_id:worker.id, role:'worker'}]});
      return {pm: pm.id, worker: worker.id, room: room.id};
    }""", name)
    page.reload()
    card = page.locator(".room-list-card").filter(has_text=name)
    expect(card).to_be_visible()
    return ids, card


def _block_pm(page, ids):
    """PM 收到 PERM 就停在待授權，之後的室內輸入只會排隊。"""
    page.evaluate("""({id}) => new Promise((resolve, reject) => {
      const ws = new WebSocket(wsURL(id));
      ws.onerror = () => reject(new Error('fixture connection failed'));
      ws.onmessage = event => {
        const msg = JSON.parse(event.data);
        if (msg.type === 'sync') ws.send(JSON.stringify({type:'input', data:'PERM', request_id:'block-' + Date.now()}));
        if (msg.type === 'input_accepted') { ws.close(); resolve(); }
      };
    })""", {"id": ids["pm"]})
    page.wait_for_function("""async ({id}) => {
      const res = await apiFetch('/rooms/' + id);
      const room = await res.json();
      return (room.members || []).some(m => m.attention);
    }""", arg={"id": ids["room"]}, timeout=60000)


def _accept(page, ids, text, rid=None, attachments=()):
    rid = rid or "q-" + uuid.uuid4().hex[:8]
    status = page.evaluate("""async ({room, pm, rid, text, attachments}) => {
      const res = await apiFetch('/rooms/' + room + '/messages', {method:'POST', headers:{'Content-Type':'application/json'},
        body: JSON.stringify({pm_id: pm, request_id: rid, text, attachment_ids: attachments})});
      return res.status;
    }""", {"room": ids["room"], "pm": ids["pm"], "rid": rid, "text": text, "attachments": list(attachments)})
    assert status == 202, status
    return rid


def _upload(page, ids):
    return page.evaluate("""async ({pm, b64}) => {
      const bytes = Uint8Array.from(atob(b64), c => c.charCodeAt(0));
      const form = new FormData();
      form.append('file', new File([bytes], 'keep.png', {type: 'image/png'}));
      const res = await apiFetch('/sessions/' + pm + '/uploads', {method:'POST', body: form});
      if (!res.ok) throw new Error(await res.text());
      const data = await res.json();
      return data.id || (data.attachment && data.attachment.id) || (data.attachments && data.attachments[0].id);
    }""", {"pm": ids["pm"], "b64": PNG_B64})


def _timeline(page, ids):
    return page.evaluate("""async room => {
      const res = await apiFetch('/rooms/' + room + '/timeline?limit=100');
      return (await res.json()).messages;
    }""", ids["room"])


def _queued(page, ids):
    """依 FIFO（queue id 小到大）排序；時間軸同一秒的順序由別的規則決定，這裡不依賴它。"""
    return sorted((m for m in _timeline(page, ids) if m.get("queued")), key=lambda m: -m["id"])


def _room_state(page, ids):
    return page.evaluate("""async room => {
      const res = await apiFetch('/rooms/' + room);
      const data = await res.json();
      const pm = (data.members || []).find(m => m.role === 'pm');
      return {pm_id: data.pm_id, queue_length: pm.queue_length, queue_paused: pm.queue_paused};
    }""", ids["room"])


def _patch(page, ids, queue_id, previous, content):
    return page.evaluate("""async ({room, pm, qid, previous, content}) => {
      const res = await apiFetch('/rooms/' + room + '/messages/' + qid, {method:'PATCH', headers:{'Content-Type':'application/json'},
        body: JSON.stringify({expected_pm_id: pm, previous, content})});
      return res.status;
    }""", {"room": ids["room"], "pm": ids["pm"], "qid": queue_id, "previous": previous, "content": content})


def _watch_patches(page):
    sent = []
    page.on("request", lambda r: sent.append(json.loads(r.post_data or "{}")) if r.method == "PATCH" and "/messages/" in r.url else None)
    return sent


def _setup(page, login, made_rooms, name_prefix, texts=("排隊甲", "排隊乙"), width=1680):
    page.set_default_timeout(15000)
    page.set_viewport_size({"width": width, "height": 900})
    name = name_prefix + uuid.uuid4().hex[:6]
    ids, card = _make_room(page, login, name)
    ids["name"] = name
    made_rooms.append(ids)
    _block_pm(page, ids)
    rids = [_accept(page, ids, t) for t in texts]
    screen = _open(page, card, name)
    for t in texts:
        expect(screen.locator("article[data-queued='true']").filter(has_text=t)).to_be_visible(timeout=30000)
    return ids, screen, rids


def _bubble(screen, text):
    return screen.locator("article[data-queued='true']").filter(has_text=text)


def _edit_button(screen, text):
    return _bubble(screen, text).get_by_role("button", name="編輯排隊訊息")


def _editor(screen):
    return screen.locator("form[data-room-queue-edit]")


def test_edit_save_cancel_reload_and_receipt(page, login, made_rooms):
    OUT.mkdir(parents=True, exist_ok=True)
    patches = _watch_patches(page)
    ids, screen, rids = _setup(page, login, made_rooms, "編輯-")
    history = screen.locator('.room-timeline')
    expect(history).to_have_count(1)
    expect(history.locator('[data-queued="true"]')).to_have_count(0)
    queue = screen.get_by_role('region', name='排隊中的訊息', exact=True)
    expect(queue).to_be_visible()
    before = _queued(page, ids)
    assert [m["text"] for m in before] == ["排隊甲", "排隊乙"] and all(m["id"] < 0 for m in before), before
    state_before = _room_state(page, ids)
    # 只有 queued 的室內輸入有編輯入口；已開始的 PERM 輸入沒有
    expect(screen.get_by_role("button", name="編輯排隊訊息")).to_have_count(2)
    expect(screen.locator("article").filter(has_text="PERM").get_by_role("button", name="編輯排隊訊息")).to_have_count(0)

    # 取消：不送任何請求，原文不變
    _edit_button(screen, "排隊乙").click()
    editor = _editor(screen)
    box = editor.get_by_label("編輯排隊訊息")
    expect(box).to_be_focused()
    expect(box).to_have_value("排隊乙")
    box.fill("不會儲存")
    editor.get_by_role("button", name="取消").click()
    expect(editor).to_have_count(0)
    expect(_bubble(screen, "排隊乙")).to_be_visible()
    assert patches == [], patches

    # 儲存：PATCH 帶 expected_pm_id／previous／content；氣泡立即與重整後都是新字，順序不變
    _edit_button(screen, "排隊乙").click()
    box = _editor(screen).get_by_label("編輯排隊訊息")
    box.fill("  新的乙\n第二行  ")
    assert _editor(screen).locator('xpath=ancestor::form').count() == 0
    _editor(screen).evaluate('form=>{form.requestSubmit();form.requestSubmit()}')
    expect(_editor(screen)).to_have_count(0)
    expect(_bubble(screen, "新的乙")).to_be_visible()
    assert patches == [{"expected_pm_id": ids["pm"], "previous": "排隊乙", "content": "  新的乙\n第二行  "}], patches
    page.reload()
    screen = _open(page, page.locator(".room-list-card").filter(has_text=ids["name"]), ids["name"])
    expect(_bubble(screen, "新的乙")).to_be_visible()
    after = _queued(page, ids)
    assert [m["text"] for m in after] == ["排隊甲", "  新的乙\n第二行  "], after
    assert [m["id"] for m in after] == [m["id"] for m in before]
    assert _room_state(page, ids) == state_before  # paused／佇列長度／PM 不變

    # 原 receipt 舊重送（原 payload）仍被接受為同一筆，不覆蓋編輯、不新增
    assert _accept(page, ids, "排隊乙", rid=rids[1]) == rids[1]
    assert [m["text"] for m in _queued(page, ids)] == ["排隊甲", "  新的乙\n第二行  "]
    # 批次與正 ID editor 共用原有 PERM setup，保留同 task 防重與取消後解鎖。
    batches = []
    page.on('request', lambda r: batches.append(r.post_data_json) if r.method == 'POST' and r.url.endswith('/messages/batch') else None)
    queue = screen.get_by_role('region', name='排隊中的訊息', exact=True)
    with page.expect_response(lambda r: r.request.method == 'POST' and r.url.endswith('/messages/batch')) as batch:
        queue.get_by_role('button', name='一起送', exact=True).evaluate('b=>{b.click();b.click()}')
    assert batch.value.status == 202 and len(batches) == 1
    expect(queue.get_by_role('button', name='取消批次')).to_be_visible()
    expect(queue.get_by_role('button', name='編輯排隊訊息').first).to_be_disabled()
    with page.expect_response(lambda r: r.request.method == 'DELETE' and '/messages/batch/' in r.url) as cancel:
        queue.get_by_role('button', name='取消批次').click()
    assert cancel.value.status == 204
    expect(queue.get_by_role('button', name='編輯排隊訊息').first).to_be_enabled()
    page.screenshot(path=str(OUT / "saved-1680.png"))


def test_polling_and_filters_never_overwrite_the_draft(page, login, made_rooms):
    ids, screen, _ = _setup(page, login, made_rooms, "草稿-")
    _edit_button(screen, "排隊甲").click()
    box = _editor(screen).get_by_label("編輯排隊訊息")
    box.fill("輸入到一半的草稿")
    # 等到至少一次輪詢回來，草稿與焦點都不被覆蓋
    with page.expect_response(re.compile(r"/rooms/[^/?]+/timeline")):
        page.evaluate("() => window.dispatchEvent(new Event('miniapp:rooms-changed'))")
    expect(box).to_have_value("輸入到一半的草稿")
    # 別台把原文改掉：輪詢回來的新文字也不能蓋掉正在編輯的草稿
    queued = _queued(page, ids)
    assert _patch(page, ids, -queued[0]["id"], "排隊甲", "別台改的甲") == 204
    with page.expect_response(re.compile(r"/rooms/[^/?]+/timeline")):
        page.evaluate("() => window.dispatchEvent(new Event('miniapp:rooms-changed'))")
    expect(box).to_have_value("輸入到一半的草稿")
    # 切換對話篩選：草稿仍在（目前篩選看不到那則時改以獨立草稿區保留）
    chips = screen.locator(".room-members-full").locator("button.room-overview-chip, button.room-member-chip")
    chips.nth(2).click()  # worker：看不到 PM 佇列中的輸入
    expect(chips.nth(2)).to_have_attribute("aria-pressed", "true")
    expect(_editor(screen).get_by_label("編輯排隊訊息")).to_have_value("輸入到一半的草稿")
    chips.nth(1).click()  # PM：看得到
    expect(_editor(screen).get_by_label("編輯排隊訊息")).to_have_value("輸入到一半的草稿")
    expect(_editor(screen)).to_have_count(1)
    # 在 PM 篩選下儲存：原文已被別台改過 → 409，草稿保留，不自動重送
    patches = _watch_patches(page)
    _editor(screen).get_by_role("button", name="儲存修改").click()
    alert = _editor(screen).get_by_role("alert")
    expect(alert).to_be_visible()
    expect(_editor(screen).get_by_label("編輯排隊訊息")).to_have_value("輸入到一半的草稿")
    page.wait_for_timeout(3500)  # 超過一個輪詢週期
    assert len(patches) == 1, patches
    assert [m["text"] for m in _queued(page, ids)][0] == "別台改的甲"
    # 重新載入：以目前佇列文字為基準，草稿仍保留，使用者再按儲存才會蓋掉
    _editor(screen).get_by_role("button", name="重新載入").click()
    expect(_editor(screen).get_by_role("alert")).to_have_count(0)
    expect(_editor(screen).get_by_label("編輯排隊訊息")).to_have_value("輸入到一半的草稿")
    _editor(screen).get_by_role("button", name="儲存修改").click()
    expect(_editor(screen)).to_have_count(0)
    assert [m["text"] for m in _queued(page, ids)][0] == "輸入到一半的草稿"


def test_dequeued_item_keeps_visible_draft_and_conflict(page, login, made_rooms):
    ids, screen, _ = _setup(page, login, made_rooms, "出列-")
    _edit_button(screen, "排隊乙").click()
    box = _editor(screen).get_by_label("編輯排隊訊息")
    box.fill("出列前的草稿")
    # 真 API：從 PM 佇列移除這一筆（走既有 WS queue_remove）
    qid = -_queued(page, ids)[1]["id"]
    page.evaluate("""({id, qid}) => new Promise((resolve, reject) => {
      const ws = new WebSocket(wsURL(id));
      ws.onerror = () => reject(new Error('fixture connection failed'));
      ws.onmessage = event => {
        const msg = JSON.parse(event.data);
        if (msg.type === 'sync') ws.send(JSON.stringify({type:'queue_remove', id: qid}));
        if (msg.type === 'queue_update' && !(msg.queue || []).some(q => q.id === qid)) { ws.close(); resolve(); }
      };
    })""", {"id": ids["pm"], "qid": qid})
    expect(_bubble(screen, "排隊乙")).to_have_count(0, timeout=20000)  # 原氣泡已消失
    # 草稿與入口仍在，且說明這則已不在佇列
    expect(_editor(screen)).to_have_count(1)
    expect(_editor(screen).get_by_label("編輯排隊訊息")).to_have_value("出列前的草稿")
    expect(_editor(screen)).to_contain_text("已不在佇列")
    patches = _watch_patches(page)
    _editor(screen).get_by_role("button", name="儲存修改").click()
    expect(_editor(screen).get_by_role("alert")).to_be_visible()
    expect(_editor(screen).get_by_label("編輯排隊訊息")).to_have_value("出列前的草稿")
    assert len(patches) == 1
    _editor(screen).get_by_role("button", name="取消").click()
    expect(_editor(screen)).to_have_count(0)


def test_controlled_handoff_timeout_and_network_errors_do_not_retry(page, login, made_rooms):
    ids, screen, _ = _setup(page, login, made_rooms, "錯誤-")
    patches = _watch_patches(page)
    mode = {"v": "409"}

    def respond(route):
        if mode["v"] == "409":
            return route.fulfill(status=409, content_type="application/json", body='{"error":"工作室的 PM 已變更"}')
        if mode["v"] == "abort":
            return route.abort("connectionreset")
        return None  # hold：不回應，等前端 timeout

    page.route(re.compile(r"/rooms/[^/?]+/messages/\d+$"), respond)
    _edit_button(screen, "排隊甲").click()
    box = _editor(screen).get_by_label("編輯排隊訊息")
    box.fill("草稿甲")
    save = _editor(screen).locator("button[type=submit]")  # 忙碌時按鈕名稱會變，用型別定位
    save.click()
    expect(_editor(screen).get_by_role("alert")).to_contain_text("PM")
    expect(box).to_have_value("草稿甲")
    mode["v"] = "abort"
    save.click()
    expect(_editor(screen).get_by_role("alert")).to_contain_text("連線")
    expect(box).to_have_value("草稿甲")
    mode["v"] = "hold"
    save.click()
    expect(save).to_have_text(re.compile("儲存中"))
    expect(save).to_be_disabled()
    expect(box).to_have_value("草稿甲")  # 忙碌時仍保留、可讀
    expect(_editor(screen).get_by_role("alert")).to_contain_text("逾時", timeout=25000)
    expect(save).to_be_enabled()
    page.wait_for_timeout(3500)
    assert len(patches) == 3, patches  # 每次只由使用者按一次，沒有自動重試
    assert [m["text"] for m in _queued(page, ids)][0] == "排隊甲"  # 伺服器端資料沒被這些失敗改動


def test_blank_bytes_and_attachment_only(page, login, made_rooms):
    ids, screen, _ = _setup(page, login, made_rooms, "規則-", texts=("無附件",))
    att = _upload(page, ids)
    _accept(page, ids, "有附件的字", attachments=[att])
    expect(_bubble(screen, "有附件的字")).to_be_visible(timeout=30000)
    patches = _watch_patches(page)

    # 無附件：空白不送出，顯示可讀錯誤
    _edit_button(screen, "無附件").click()
    box = _editor(screen).get_by_label("編輯排隊訊息")
    box.fill("  \n ")
    _editor(screen).get_by_role("button", name="儲存修改").click()
    expect(_editor(screen).get_by_role("alert")).to_contain_text("請輸入文字")
    expect(box).to_have_attribute("aria-invalid", "true")
    assert patches == []
    # 依 UTF-8 bytes 計：21334 個中文字＝64002 bytes 超限不送出；64000 bytes 剛好可送且不截字
    over = "中" * 21334
    box.fill(over)
    expect(_editor(screen)).to_contain_text("64002")
    _editor(screen).get_by_role("button", name="儲存修改").click()
    expect(_editor(screen).get_by_role("alert")).to_contain_text("64000")
    assert patches == []
    exact = "中" * 21333 + "a"
    box.fill(exact)
    _editor(screen).get_by_role("button", name="儲存修改").click()
    expect(_editor(screen)).to_have_count(0)
    assert len(patches) == 1 and patches[0]["content"] == exact
    assert _queued(page, ids)[0]["text"] == exact

    # 有附件：可編成空白，附件與順序不變
    before = _queued(page, ids)
    _edit_button(screen, "有附件的字").click()
    _editor(screen).get_by_label("編輯排隊訊息").fill("")
    _editor(screen).get_by_role("button", name="儲存修改").click()
    expect(_editor(screen)).to_have_count(0)
    after = _queued(page, ids)
    assert [m["id"] for m in after] == [m["id"] for m in before]
    assert after[1]["text"] == "" and [a["id"] for a in after[1]["attachments"]] == [att], after
    expect(screen.locator("article[data-queued='true']").filter(has_text="keep.png")).to_have_count(1)


@pytest.mark.parametrize("width", [390, 320])
def test_mobile_keyboard_and_touch_targets(page, login, made_rooms, width):
    OUT.mkdir(parents=True, exist_ok=True)
    ids, screen, _ = _setup(page, login, made_rooms, f"手機{width}-", texts=("排隊甲",), width=width)
    patches = _watch_patches(page)
    button = _edit_button(screen, "排隊甲")
    box_ = button.bounding_box()
    assert box_ and 24 <= box_["height"] <= 28 and 24 <= box_["width"] <= 28, box_
    assert button.inner_text().strip() == ""
    expect(button.locator('svg[aria-hidden="true"]')).to_have_count(1)
    bubble = _bubble(screen, "排隊甲")
    assert bubble.bounding_box()["height"] <= 128
    button.focus()
    expect(button).to_be_focused()
    page.screenshot(path=str(OUT / f"compact-bubble-{width}.png"))
    button.click()
    editor = _editor(screen)
    box = editor.get_by_label("編輯排隊訊息")
    expect(box).to_be_focused()
    size = box.evaluate("el => parseFloat(getComputedStyle(el).fontSize)")
    assert size >= 16, size  # iOS 不縮放
    # Enter 保留原生換行，不送出；組字中的 Enter 同樣不送出
    box.fill("第一行")
    page.keyboard.press("End")
    page.keyboard.press("Enter")
    page.keyboard.type("第二行")
    expect(box).to_have_value("第一行\n第二行")
    box.dispatch_event("compositionstart")
    page.keyboard.press("Enter")
    box.dispatch_event("compositionend")
    assert patches == []  # Enter 只換行，不會送出
    box.fill("第一行\n第二行")
    for name in ("儲存修改", "取消"):
        b = editor.get_by_role("button", name=name).bounding_box()
        assert b and b["height"] >= 44 and b["width"] >= 44, (name, b)
    over = page.evaluate("() => ({doc: document.documentElement.scrollWidth - innerWidth, body: document.body.scrollWidth - innerWidth})")
    assert over["doc"] <= 1 and over["body"] <= 1, over
    page.screenshot(path=str(OUT / f"editor-{width}.png"))
    # 鍵盤：Tab 到儲存、Enter 送出
    page.keyboard.press("Tab")
    expect(editor.get_by_role("button", name="儲存修改")).to_be_focused()
    page.keyboard.press("Enter")
    expect(editor).to_have_count(0)
    assert len(patches) == 1 and patches[0]["content"] == "第一行\n第二行"
