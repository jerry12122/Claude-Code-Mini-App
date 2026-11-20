"""真實 DOM：工作室上方成員列的對話篩選（總覽／worker／PM）。

第一個案例用真實 server 與假 CLI 驗證資料語意與可及性；第二個案例攔截 timeline 回應，
驗證分頁走的是「篩選後」的游標，以及切換後晚到的舊回應不會混進新的閱讀範圍。
"""
import os
from pathlib import Path
import re
import uuid
from urllib.parse import parse_qs, urlparse

import pytest
from playwright.sync_api import expect

from test_workroom_polish import _open

OUT = Path(os.environ.get("ROOM_FILTER_ARTIFACTS", "test-results/room-member-filter"))


@pytest.fixture
def browser_context_args(browser_context_args):
    # 攔截要直接作用在頁面請求上，不能由 Service Worker 代發。
    return {**browser_context_args, "service_workers": "block"}


@pytest.fixture
def made_rooms(page):
    """測試結束刪掉建立的工作室與 session：server 是整個 session 共用的，別讓後面的案例看到多出來的 session。"""
    made = []
    yield made
    for ids in made:
        try:
            page.evaluate("""async ids => {
              await apiFetch('/rooms/' + ids.room, {method: 'DELETE'});
              for (const id of [ids.pm, ids.a, ids.b]) await apiFetch('/sessions/' + id, {method: 'DELETE'});
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
      const a = await mk(name + ' 同名');
      const b = await mk(name + ' 同名');
      const pm = await mk(name + ' PM');
      const room = await post('/rooms', {name, members:[{session_id:pm.id, role:'pm'},{session_id:a.id, role:'worker'},{session_id:b.id, role:'worker'}]});
      return {pm: pm.id, a: a.id, b: b.id, room: room.id};
    }""", name)
    page.reload()
    card = page.locator(".room-list-card").filter(has_text=name)
    expect(card).to_be_visible()
    return ids, card


def _send_input(page, session_id, text):
    """直接對某個 session 送一則輸入（每次用新的 request id）。"""
    page.evaluate("""({id, text, rid}) => new Promise((resolve, reject) => {
      const ws = new WebSocket(wsURL(id));
      ws.onerror = () => reject(new Error('fixture connection failed'));
      ws.onmessage = event => {
        const msg = JSON.parse(event.data);
        if (msg.type === 'sync') ws.send(JSON.stringify({type:'input', data:text, request_id:rid}));
        if (msg.type === 'input_accepted') { ws.close(); resolve(); }
        if (msg.type === 'input_rejected') { ws.close(); reject(new Error(msg.content)); }
      };
    })""", {"id": session_id, "text": text, "rid": "filter-" + uuid.uuid4().hex})


def _chips(screen, compact):
    """目前可見那一排的晶片：[總覽, PM, A, B]。"""
    row = screen.locator(".room-members-compact" if compact else ".room-members-full")
    return row.locator("button.room-overview-chip, button.room-member-chip")


def _done_assistants(screen):
    return screen.locator('article[data-side="assistant"][data-status="done"]')


def _no_page_overflow(page):
    over = page.evaluate("() => ({doc: document.documentElement.scrollWidth - innerWidth, body: document.body.scrollWidth - innerWidth})")
    assert over["doc"] <= 1 and over["body"] <= 1, over


def test_filter_overview_worker_pm_on_real_server(page, login, made_rooms):
    OUT.mkdir(parents=True, exist_ok=True)
    page.set_default_timeout(60000)
    page.set_viewport_size({"width": 1680, "height": 900})
    name = "篩選-" + uuid.uuid4().hex[:6]
    ids, card = _make_room(page, login, name)
    made_rooms.append(ids)
    screen = _open(page, card, name)
    log = screen.locator(".room-timeline")
    chips = _chips(screen, compact=False)
    expect(chips).to_have_count(4)  # 總覽、PM、A、B（A、B 同名，只能靠位置與 id 分辨）
    overview, pm, a, b = (chips.nth(i) for i in range(4))
    expect(overview).to_have_attribute("aria-pressed", "true")
    for chip in (pm, a, b):
        expect(chip).to_have_attribute("aria-pressed", "false")
    expect(pm).to_contain_text("現任 PM")

    _send_input(page, ids["a"], "A專屬-ALPHA")
    expect(log.get_by_text("A專屬-ALPHA")).to_be_visible()
    _send_input(page, ids["b"], "B專屬-BRAVO")
    expect(log.get_by_text("B專屬-BRAVO")).to_be_visible()
    box = screen.locator('textarea[name="text"]')
    box.fill("PM題-CHARLIE")
    screen.get_by_role("button", name="傳給 PM", exact=True).click()
    expect(log.get_by_text("PM題-CHARLIE")).to_be_visible()
    expect(_done_assistants(screen)).to_have_count(3, timeout=60000)
    page.screenshot(path=str(OUT / "overview-1680.png"))

    # 待授權的 worker：篩選只改閱讀範圍，授權卡不能因為篩選消失
    _send_input(page, ids["b"], "PERM")
    expect(screen.get_by_role("button", name="待你處理 1", exact=True)).to_be_visible(timeout=60000)
    cards = screen.locator('[id^="room-action-"]')
    expect(cards).to_have_count(1)

    box.fill("草稿不能丟")

    # 點 worker A：只剩與 A 有關的輸入與回覆
    a.click()
    expect(a).to_have_attribute("aria-pressed", "true")
    expect(overview).to_have_attribute("aria-pressed", "false")
    expect(pm).to_have_attribute("aria-pressed", "false")
    expect(log.get_by_text("A專屬-ALPHA")).to_be_visible()
    expect(log.get_by_text("B專屬-BRAVO")).to_have_count(0)
    expect(log.get_by_text("PM題-CHARLIE")).to_have_count(0)
    expect(screen.locator("article")).to_have_count(2)  # A 的輸入與回覆
    expect(log).to_have_attribute("aria-label", re.compile("篩選"))
    expect(cards).to_have_count(1)
    expect(box).to_have_value("草稿不能丟")
    expect(screen.locator(".room-composer-meta")).to_contain_text("傳訊息給")
    expect(screen.locator(".room-composer-meta")).to_contain_text("PM")
    page.screenshot(path=str(OUT / "worker-a-1680.png"))

    # 點 B：與 A 同名也不混淆；B 的授權卡仍在
    b.click()
    expect(b).to_have_attribute("aria-pressed", "true")
    expect(a).to_have_attribute("aria-pressed", "false")
    expect(log.get_by_text("B專屬-BRAVO")).to_be_visible()
    expect(log.get_by_text("A專屬-ALPHA")).to_have_count(0)
    expect(cards).to_have_count(1)

    # 點 PM：只有「你 ↔ PM」的對話
    pm.click()
    expect(pm).to_have_attribute("aria-pressed", "true")
    expect(log.get_by_text("PM題-CHARLIE")).to_be_visible()
    expect(log.get_by_text("A專屬-ALPHA")).to_have_count(0)
    expect(log.get_by_text("B專屬-BRAVO")).to_have_count(0)
    expect(screen.locator("article")).to_have_count(2)
    expect(box).to_have_value("草稿不能丟")
    page.screenshot(path=str(OUT / "pm-1680.png"))

    # 再點一次已選的晶片＝回總覽；點總覽也一樣
    pm.click()
    expect(overview).to_have_attribute("aria-pressed", "true")
    expect(log.get_by_text("A專屬-ALPHA")).to_be_visible()
    expect(log.get_by_text("B專屬-BRAVO")).to_be_visible()
    expect(log.get_by_text("PM題-CHARLIE")).to_be_visible()
    a.click()
    overview.click()
    expect(overview).to_have_attribute("aria-pressed", "true")
    expect(screen.locator("article")).to_have_count(8)  # 另含 B 的 PERM 輸入與其回覆

    # 點成員不再開 session（仍在工作室畫面）；進入 session 走成員面板
    expect(screen).to_be_visible()
    toggle = screen.locator(".room-board-toggle")
    if toggle.get_attribute("aria-expanded") != "true":
        toggle.click()
    board = screen.locator("#room-work-board")
    board.get_by_role("button", name="成員", exact=True).click()
    expect(board.get_by_role("button", name=re.compile("進入此 session")).first).to_be_visible()

    # 鍵盤操作：聚焦晶片、Enter／Space 切換，焦點留在原晶片
    a.focus()
    page.keyboard.press("Enter")
    expect(a).to_have_attribute("aria-pressed", "true")
    expect(a).to_be_focused()
    page.keyboard.press("Space")
    expect(overview).to_have_attribute("aria-pressed", "true")
    expect(a).to_be_focused()

    # 手機寬度：窄版成員列同樣可切換、不溢寬、焦點留在晶片
    for width in (390, 320):
        page.set_viewport_size({"width": width, "height": 800})
        narrow = _chips(screen, compact=True)
        expect(narrow).to_have_count(4)
        n_overview, n_pm, n_a, n_b = (narrow.nth(i) for i in range(4))
        expect(n_overview).to_have_attribute("aria-pressed", "true")
        _no_page_overflow(page)
        n_b.scroll_into_view_if_needed()
        n_b.click()
        expect(n_b).to_have_attribute("aria-pressed", "true")
        expect(n_b).to_be_focused()
        expect(log.get_by_text("B專屬-BRAVO")).to_be_visible()
        expect(log.get_by_text("A專屬-ALPHA")).to_have_count(0)
        for chip in (n_overview, n_pm, n_a, n_b):
            box_ = chip.bounding_box()
            assert box_ and box_["height"] >= 44, box_
        _no_page_overflow(page)
        page.screenshot(path=str(OUT / f"worker-b-{width}.png"))
        n_pm.scroll_into_view_if_needed()
        n_pm.click()
        expect(log.get_by_text("PM題-CHARLIE")).to_be_visible()
        expect(log.get_by_text("B專屬-BRAVO")).to_have_count(0)
        n_overview.scroll_into_view_if_needed()
        n_overview.click()
        expect(n_overview).to_have_attribute("aria-pressed", "true")
        expect(screen.get_by_role("button", name=re.compile(r"^成員 3$"))).to_be_visible()
        _no_page_overflow(page)


def _msg(mid, text, session, role="claude", **extra):
    return {"id": mid, "session_id": session, "session_name": "同名", "member_role": "worker", "agent_type": "claude",
            "role": role, "author": "", "text": text, "status": "done", "created_at": f"2026-11-18 10:{mid % 60:02d}:00",
            "sender": "同名", "recipient": "", "mine": False, "source_kind": "assistant" if role == "claude" else "user",
            **extra}


def test_filter_paginates_by_filtered_cursor_and_drops_stale_responses(page, login, made_rooms):
    OUT.mkdir(parents=True, exist_ok=True)
    page.set_default_timeout(60000)
    page.set_viewport_size({"width": 1680, "height": 900})
    name = "分頁-" + uuid.uuid4().hex[:6]
    ids, card = _make_room(page, login, name)
    made_rooms.append(ids)
    a_newest = [_msg(200 + i, f"A新-{i}", ids["a"]) for i in range(30)][::-1]
    a_oldest = [_msg(100 + i, f"A舊-{i}", ids["a"]) for i in range(5)][::-1]
    b_items = [_msg(300, "B內容-ONLY", ids["b"])]
    requests, held = [], []
    hold_older_a = {"on": False}

    def page_body(items, more, cursor):
        return {"messages": items, "has_more": more, "next_cursor": cursor}

    def timeline(route):
        query = parse_qs(urlparse(route.request.url).query)
        member = query.get("member", [""])[0]
        before = query.get("before", [""])[0]
        requests.append((member, before))
        if not member:
            return route.continue_()
        if member == ids["a"]:
            if before == "CURSOR-A":
                if hold_older_a["on"]:
                    held.append(route)
                    return None
                return route.fulfill(json=page_body(a_oldest, False, ""))
            return route.fulfill(json=page_body(a_newest, True, "CURSOR-A"))
        if member == ids["b"]:
            return route.fulfill(json=page_body(b_items, False, ""))
        return route.fulfill(json=page_body([], False, ""))

    page.route(re.compile(r"/rooms/[^/?]+/timeline"), timeline)
    screen = _open(page, card, name)
    log = screen.locator(".room-timeline")
    chips = _chips(screen, compact=False)
    overview, pm, a, b = (chips.nth(i) for i in range(4))

    a.click()
    expect(log.get_by_text("A新-0", exact=True)).to_be_visible()
    # 篩選後的第一頁就帶著 member；游標來自篩選後的結果
    assert (ids["a"], "") in requests, requests
    older = screen.get_by_role("button", name="載入更早訊息", exact=True)
    expect(older).to_be_visible()
    older.click()
    expect(log.get_by_text("A舊-0", exact=True)).to_be_visible()
    assert (ids["a"], "CURSOR-A") in requests, "載入更早訊息必須帶著同一個 member 與篩選後的游標"
    expect(log.get_by_text("已到最早的訊息")).to_be_visible()

    # 切到 B 再回 A：重新從篩選後的第一頁開始，不沿用上一次的游標或訊息
    b.click()
    expect(log.get_by_text("B內容-ONLY")).to_be_visible()
    expect(log.get_by_text("A新-0", exact=True)).to_have_count(0)
    expect(log.get_by_text("A舊-0", exact=True)).to_have_count(0)
    a.click()
    expect(log.get_by_text("A新-0", exact=True)).to_be_visible()
    expect(log.get_by_text("A舊-0", exact=True)).to_have_count(0)
    expect(older).to_be_visible()

    # A 的「載入更早」還在路上時切到 B：晚到的那頁不得插進 B
    hold_older_a["on"] = True
    older.click()
    for _ in range(50):
        if held:
            break
        page.wait_for_timeout(100)
    assert held, "舊頁請求沒有送出"
    b.click()
    expect(log.get_by_text("B內容-ONLY")).to_be_visible()
    for route in held:
        route.fulfill(json=page_body(a_oldest, False, ""))
    page.wait_for_timeout(500)
    expect(log.get_by_text("A舊-0", exact=True)).to_have_count(0)
    expect(log.get_by_text("B內容-ONLY")).to_be_visible()
    expect(log.get_by_text("已到最早的訊息")).to_be_visible()  # B 自己的狀態
    page.screenshot(path=str(OUT / "stale-dropped.png"))

    # 目前篩選沒有結果：說的是這個篩選，不是整間沒有訊息
    b_items.clear()
    page.evaluate("() => window.dispatchEvent(new Event('miniapp:rooms-changed'))")
    expect(log.get_by_text(re.compile("沒有與「.*」相關的訊息"))).to_be_visible()
    expect(log.get_by_text("成員還沒有任何訊息。")).to_have_count(0)
    page.screenshot(path=str(OUT / "empty-filter.png"))
    pm.click()
    expect(log.get_by_text("你與 PM 還沒有對話。")).to_be_visible()
