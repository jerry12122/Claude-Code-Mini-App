"""工作室修整：列表、輸入高度、貼圖、側欄、送出回饋、最新全文。各段獨立，避免前段失敗就看不到後段。"""
from pathlib import Path
import base64
import time
import uuid

from playwright.sync_api import expect

OUT = Path("test-results/workroom-polish")
PNG = base64.b64decode("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==")
PNG_B64 = base64.b64encode(PNG).decode()


def _room(page, login, name):
    login()
    page.evaluate("""async name => {
      const post = async (url, body) => {
        const res = await apiFetch(url, {method:'POST', headers:{'Content-Type':'application/json'}, body:JSON.stringify(body)});
        if (!res.ok) throw new Error(await res.text());
        return res.json();
      };
      const worker = await post('/sessions', {name: name + ' worker', agent_type:'claude', permission_mode:'default'});
      const pm = await post('/sessions', {name: name + ' PM', agent_type:'claude', permission_mode:'default'});
      await post('/rooms', {name, members:[{session_id:worker.id, role:'worker'},{session_id:pm.id, role:'pm'}]});
    }""", name)
    page.reload()
    card = page.locator(".room-list-card").filter(has_text=name)
    expect(card).to_be_visible()
    return card


def _open(page, card, name):
    card.locator("button").first.click()
    screen = page.get_by_role("region", name="工作室 " + name, exact=True)
    expect(screen).to_be_visible()
    return screen


def _list_delta(page):
    return page.evaluate("""() => {
      const room = document.querySelector('.room-list-card');
      const session = document.querySelector('.ra-session-card:not(.room-list-card)');
      if (!room || !session) return null;
      const roomName = room.querySelector('.room-list-name').getBoundingClientRect();
      const sessionName = session.querySelector('.truncate').getBoundingClientRect();
      const roomBox = room.getBoundingClientRect();
      const sessionBox = session.getBoundingClientRect();
      return {
        height: Math.abs(roomBox.height - sessionBox.height),
        text: Math.abs((roomName.top - roomBox.top) - (sessionName.top - sessionBox.top)),
        overflow: room.scrollWidth > room.clientWidth + 1 || session.scrollWidth > session.clientWidth + 1,
      };
    }""")


def _paste(page, files):
    page.evaluate("""(files) => {
      const data = new DataTransfer();
      for (const file of files) {
        const binary = atob(file.b64);
        const bytes = new Uint8Array(binary.length);
        for (let i = 0; i < binary.length; i += 1) bytes[i] = binary.charCodeAt(i);
        data.items.add(new File([bytes], file.name, { type: file.type }));
      }
      const input = document.querySelector('.room-screen textarea[name="text"]');
      const event = new Event('paste', { bubbles: true, cancelable: true });
      Object.defineProperty(event, 'clipboardData', { value: data });
      input.dispatchEvent(event);
    }""", files)


def test_list_alignment_and_composer_height(page, login):
    name = "列高-" + uuid.uuid4().hex[:6]
    other = "鄰列-" + uuid.uuid4().hex[:6]
    card = _room(page, login, name)
    page.evaluate("""async name => {
      const post = async (url, body) => {
        const res = await apiFetch(url, {method:'POST', headers:{'Content-Type':'application/json'}, body:JSON.stringify(body)});
        if (!res.ok) throw new Error(await res.text());
        return res.json();
      };
      const pm = await post('/sessions', {name: name + ' PM', agent_type:'claude', permission_mode:'default'});
      await post('/rooms', {name, members:[{session_id:pm.id, role:'pm'}]});
    }""", other)
    page.reload()
    expect(page.locator(".room-list-card").filter(has_text=name)).to_be_visible()
    expect(page.locator(".room-list-card").filter(has_text=other)).to_be_visible()
    boundary = page.evaluate("""() => {
      const cards = [...document.querySelectorAll('.room-list-card')];
      if (cards.length < 2) return { ok: false, reason: 'need two rooms' };
      const ordered = cards.sort((a, b) => a.getBoundingClientRect().top - b.getBoundingClientRect().top);
      const upper = ordered[0].getBoundingClientRect();
      const lower = ordered[1].getBoundingClientRect();
      const hit = (x, y) => {
        const node = document.elementFromPoint(x, y);
        return node ? ordered.indexOf(node.closest('.room-list-card')) : -1;
      };
      return {
        ok: hit(upper.left + 16, upper.bottom - 1) === 0 && hit(lower.left + 16, lower.top + 1) === 1,
        upper: hit(upper.left + 16, upper.bottom - 1),
        lower: hit(lower.left + 16, lower.top + 1),
      };
    }""")
    assert boundary["ok"], boundary
    for label, edge in ((name, -1), (other, 1)):
        row = page.locator(".room-list-card").filter(has_text=label)
        box = row.bounding_box()
        page.mouse.click(box["x"] + 16, box["y"] + (box["height"] - 4 if edge < 0 else 4))
        expect(page.get_by_role("region", name="工作室 " + label, exact=True)).to_be_visible()
        page.get_by_role("button", name="關閉工作室", exact=True).click()
        expect(row).to_be_visible()
    OUT.mkdir(parents=True, exist_ok=True)
    for width in (1280, 960, 768, 390, 320):
        page.set_viewport_size({"width": width, "height": 800})
        # 1024 以下會卸掉桌面殼、另掛一份列表。先等這次寬度的殼已換上，再等列載入，避免量到舊殼或空列表。
        page.wait_for_function("""() => {
          const wide = matchMedia('(min-width: 1024px)').matches;
          const shell = !!document.querySelector('.ra-desktop-shell');
          if (wide !== shell) return false;
          return document.querySelector('.room-list-card .room-list-name')
            && document.querySelector('.ra-session-card:not(.room-list-card) .truncate');
        }""")
        delta = _list_delta(page)
        assert delta and delta["height"] <= 4, {"width": width, "delta": delta}
        assert delta["text"] <= 4, delta
        assert not delta["overflow"], delta
        page.screenshot(path=str(OUT / f"list-{width}.png"))
    screen = _open(page, card, name)
    for width, theme in ((1280, "dark"), (390, "light"), (320, "dark")):
        page.set_viewport_size({"width": width, "height": 800})
        page.evaluate("""theme => { document.documentElement.dataset.theme = theme; }""", theme)
        box = screen.locator('textarea[name="text"]')
        page.wait_for_function("""() => { const el = document.querySelector('.room-screen textarea[name="text"]'); const h = el ? el.getBoundingClientRect().height : 0; return h >= 32 && h <= 48; }""")
        empty = box.evaluate("el => el.getBoundingClientRect().height")
        assert 32 <= empty <= 48, empty
        form = screen.locator(".room-composer").evaluate("el => el.getBoundingClientRect().height")
        assert form <= 160, form
        box.fill("第一行\n第二行\n第三行\n第四行")
        grown = box.evaluate("el => el.getBoundingClientRect().height")
        assert grown > empty + 10, grown
        box.fill("")
        page.wait_for_function("([el, empty]) => Math.abs(el.getBoundingClientRect().height - empty) <= 2", arg=[box.element_handle(), empty])
        page.screenshot(path=str(OUT / f"composer-{width}-{theme}.png"))


def test_paste_image_and_plain_text(page, login):
    name = "貼圖-" + uuid.uuid4().hex[:6]
    login()
    created = page.evaluate("""async name => {
      const post = async (url, body) => {
        const res = await apiFetch(url, {method:'POST', headers:{'Content-Type':'application/json'}, body:JSON.stringify(body)});
        if (!res.ok) throw new Error(await res.text());
        return res.json();
      };
      const worker = await post('/sessions', {name: name + ' worker', agent_type:'claude', permission_mode:'default'});
      const pm = await post('/sessions', {name: name + ' PM', agent_type:'claude', permission_mode:'default'});
      const next = await post('/sessions', {name: name + ' 新PM', agent_type:'claude', permission_mode:'default'});
      const room = await post('/rooms', {name, members:[{session_id:worker.id, role:'worker'},{session_id:pm.id, role:'pm'}]});
      return {room, worker, pm, next};
    }""", name)
    page.reload()
    screen = _open(page, page.locator(".room-list-card").filter(has_text=name), name)
    box = screen.locator('textarea[name="text"]')
    text_paste = box.evaluate("""el => {
      const event = new Event('paste', { bubbles: true, cancelable: true });
      Object.defineProperty(event, 'clipboardData', { value: { files: [], getData: () => '純文字' } });
      return el.dispatchEvent(event);
    }""")
    assert text_paste, "純文字貼上不該被攔下"
    page.evaluate("""() => {
      const input = document.querySelector('.room-screen textarea[name="text"]');
      const data = new DataTransfer();
      for (let i = 0; i < 21; i += 1) data.items.add(new File(['x'], 'f' + i + '.txt', { type: 'text/plain' }));
      const event = new Event('paste', { bubbles: true, cancelable: true });
      Object.defineProperty(event, 'clipboardData', { value: data });
      input.dispatchEvent(event);
    }""")
    expect(screen.get_by_text("每則訊息最多 20 個附件")).to_be_visible()
    expect(screen.locator('[aria-label="待送出的附件"]')).to_have_count(0)
    _paste(page, [
        {"b64": PNG_B64, "name": "shot.png", "type": "image/png"},
        {"b64": PNG_B64, "name": "second.png", "type": "image/png"},
    ])
    previews = screen.locator('[aria-label="待送出的附件"] img')
    expect(previews).to_have_count(2)
    page.wait_for_function("""() => {
      const imgs = [...document.querySelectorAll('.room-screen [aria-label="待送出的附件"] img')];
      return imgs.length === 2 && imgs.every(img => img.naturalWidth > 0);
    }""")
    screen.get_by_role("button", name="移除附件 second.png", exact=True).click()
    expect(previews).to_have_count(1)

    held = {}

    def hold_upload(route):
        held["route"] = route

    page.route("**/sessions/*/uploads", hold_upload)
    screen.get_by_role("button", name="傳給 PM", exact=True).click()
    page.wait_for_function("""() => {
      const img = document.querySelector('article[data-align="right"] img');
      if (!img) return false;
      const article = img.closest('article');
      const download = [...article.querySelectorAll('button')].some((button) => button.textContent === '下載');
      return img.naturalWidth > 0 && !download;
    }""")
    held["route"].continue_()
    page.unroute("**/sessions/*/uploads", hold_upload)
    expect(screen.locator('.room-timeline article[data-align="right"]')).to_have_count(1, timeout=30000)
    expect(screen.locator('.room-timeline article[data-align="right"] img')).to_have_count(1)
    page.wait_for_function("""() => {
      const img = document.querySelector('.room-timeline article[data-align="right"] img');
      return !!img && img.naturalWidth > 0;
    }""")
    expect(box).to_have_value("")

    def reject_upload(route):
        route.fulfill(status=500, content_type="application/json", body='{"error":"上傳失敗"}')

    page.route("**/sessions/*/uploads", reject_upload)
    _paste(page, [{"b64": PNG_B64, "name": "again.png", "type": "image/png"}])
    expect(screen.locator('[aria-label="待送出的附件"] img')).to_have_count(1)
    screen.get_by_role("button", name="傳給 PM", exact=True).click()
    expect(screen.get_by_role("alert")).to_contain_text("上傳失敗")
    expect(screen.locator('[aria-label="待送出的附件"]')).to_be_visible()
    screen.get_by_role("button", name="重試").click()
    expect(screen.get_by_text("已就緒")).to_be_visible()
    screen.get_by_role("button", name="移除附件 again.png", exact=True).click()
    expect(screen.locator('[aria-label="待送出的附件"]')).to_have_count(0)
    page.unroute("**/sessions/*/uploads", reject_upload)

    _paste(page, [{"b64": PNG_B64, "name": "handoff.png", "type": "image/png"}])
    expect(screen.locator('[aria-label="待送出的附件"] img')).to_have_count(1)
    expect(screen.locator(".room-pm-state")).to_contain_text("閒置", timeout=30000)
    page.evaluate("""async ({id, worker, pm, next}) => {
      const put = () => apiFetch('/rooms/' + encodeURIComponent(id), {method:'PUT', headers:{'Content-Type':'application/json'}, body:JSON.stringify({
        members:[{session_id:worker, role:'worker'},{session_id:pm, role:'worker'},{session_id:next, role:'pm'}],
        previous_pm_id: pm,
      })});
      for (let i = 0; i < 40; i += 1) {
        const res = await put();
        if (res.ok) return;
        if (i === 39) throw new Error(await res.text());
        await new Promise((resolve) => setTimeout(resolve, 500));
      }
    }""", {"id": created["room"]["id"], "worker": created["worker"]["id"], "pm": created["pm"]["id"], "next": created["next"]["id"]})
    expect(screen.get_by_text("不會自動轉過去")).to_be_visible(timeout=10000)
    seen = {}

    def capture_old_pm(route):
        seen["url"] = route.request.url
        route.fulfill(status=500, content_type="application/json", body='{"error":"上傳失敗"}')

    page.route("**/sessions/*/uploads", capture_old_pm)
    screen.get_by_role("button", name="傳給 PM", exact=True).click()
    expect(screen.get_by_role("alert")).to_contain_text("上傳失敗")
    assert created["pm"]["id"] in seen.get("url", ""), seen
    assert created["next"]["id"] not in seen.get("url", ""), seen
    expect(screen.locator('[aria-label="待送出的附件"]')).to_be_visible()
    page.unroute("**/sessions/*/uploads", capture_old_pm)


def _room_layout(screen):
    return screen.evaluate("""el => {
      const toggle = el.querySelector('.room-board-toggle');
      const thread = el.querySelector('.room-thread');
      const timeline = el.querySelector('.room-timeline');
      return {
        width: el.clientWidth,
        expanded: toggle ? toggle.getAttribute('aria-expanded') : '',
        threadHidden: thread ? getComputedStyle(thread).visibility === 'hidden' : true,
        scrollTop: timeline ? timeline.scrollTop : 0,
        scrollable: timeline ? timeline.scrollHeight > timeline.clientHeight + 20 : false,
      };
    }""")


def test_board_toggle_keeps_draft_and_scroll(page, login):
    name = "側欄-" + uuid.uuid4().hex[:6]
    screen = _open(page, _room(page, login, name), name)
    page.set_viewport_size({"width": 1280, "height": 800})
    box = screen.locator('textarea[name="text"]')
    # 長文送出後會摺疊，時間軸不再溢位；改送多則短訊息，每則等助理完成（跟隨捲底停止）再送下一則。
    for i in range(8):
        box.fill(f"捲動行{i}")
        screen.get_by_role("button", name="傳給 PM", exact=True).click()
        page.wait_for_function("""n => document.querySelectorAll('.room-screen article[data-side="assistant"][data-status="done"]').length >= n""", arg=i + 1, timeout=30000)
    placed = screen.locator(".room-timeline").evaluate("""el => {
      el.scrollTop = 0;
      el.dispatchEvent(new Event('scroll', { bubbles: true }));
      return { top: el.scrollTop, scrollable: el.scrollHeight > el.clientHeight + 20 };
    }""")
    assert placed["scrollable"], placed
    assert placed["top"] == 0, placed
    box.fill("草稿還在")
    toggle = screen.locator(".room-board-toggle")
    expect(toggle).to_be_visible()
    page.wait_for_function("""() => {
      const room = document.querySelector('.room-screen');
      const toggle = document.querySelector('.room-board-toggle');
      return room && toggle && room.clientWidth > 0 && toggle.getAttribute('aria-expanded') === 'false';
    }""")
    toggle.click()
    expect(toggle).to_have_attribute("aria-expanded", "true")
    expect(screen.locator("#room-work-board")).to_be_visible()
    wide = _room_layout(screen)
    if wide["width"] >= 960:
        assert not wide["threadHidden"], wide
    else:
        assert wide["threadHidden"], wide
    screen.locator("#room-work-board").get_by_role("button", name="備忘", exact=True).click()
    screen.locator("#room-work-board").get_by_role("button", name="編輯備忘", exact=True).click()
    memo = screen.locator("#room-work-board textarea")
    memo.fill("備忘草稿")
    toggle.click()
    expect(toggle).to_have_attribute("aria-expanded", "false")
    expect(screen.locator("#room-work-board")).to_be_hidden()
    expect(screen.locator(".room-thread")).to_be_visible()
    expect(box).to_have_value("草稿還在")
    assert screen.locator(".room-timeline").evaluate("el => el.scrollTop") == 0
    toggle.click()
    expect(memo).to_have_value("備忘草稿")
    page.set_viewport_size({"width": 390, "height": 800})
    page.wait_for_function("""() => {
      const room = document.querySelector('.room-screen');
      const toggle = room && room.querySelector('.room-board-toggle');
      return room && room.clientWidth < 960 && toggle && toggle.getAttribute('aria-expanded');
    }""")
    narrow = _room_layout(screen)
    if narrow["expanded"] == "true" or narrow["threadHidden"]:
        toggle.click()
    expect(screen.locator(".room-thread")).to_be_visible()
    expect(box).to_have_value("草稿還在")
    assert screen.locator(".room-timeline").evaluate("el => el.scrollTop") == 0


def test_send_feedback_dedup_failure_and_conflict(page, login):
    name = "送出-" + uuid.uuid4().hex[:6]
    screen = _open(page, _room(page, login, name), name)
    box = screen.locator('textarea[name="text"]')

    def stall(route):
        time.sleep(0.8)
        route.continue_()

    page.route("**/rooms/*/messages", stall)
    box.fill("延遲訊息")
    measured = screen.get_by_role("button", name="傳給 PM", exact=True).evaluate("""async button => {
      const started = performance.now();
      button.click();
      await new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve)));
      const bubble = document.querySelector('article[data-delivery="sending"], article[data-delivery="queued"]');
      return { elapsed: performance.now() - started, text: bubble ? bubble.innerText : '' };
    }""")
    assert measured["elapsed"] < 250, measured
    assert "傳送中" in measured["text"] or "已排隊" in measured["text"], measured
    expect(screen.locator("article", has_text="延遲訊息")).to_have_count(1, timeout=30000)
    page.unroute("**/rooms/*/messages", stall)

    box.fill("同樣文字")
    screen.get_by_role("button", name="傳給 PM", exact=True).click()
    expect(screen.locator("article", has_text="同樣文字")).to_have_count(1, timeout=30000)
    box.fill("同樣文字")
    screen.get_by_role("button", name="傳給 PM", exact=True).click()
    expect(screen.locator("article", has_text="同樣文字")).to_have_count(2, timeout=30000)

    def fail(route):
        route.fulfill(status=500, content_type="application/json", body='{"error":"暫時無法送出"}')

    page.route("**/rooms/*/messages", fail)
    box.fill("失敗保留")
    screen.get_by_role("button", name="傳給 PM", exact=True).click()
    expect(screen.get_by_role("alert")).to_contain_text("暫時無法送出")
    expect(box).to_have_value("失敗保留")
    page.unroute("**/rooms/*/messages", fail)

    def conflict(route):
        route.fulfill(status=409, content_type="application/json", body='{"error":"工作室的 PM 已變更或未設定，請確認成員後再送出"}')

    page.route("**/rooms/*/messages", conflict)
    box.fill("衝突保留")
    screen.get_by_role("button", name="傳給 PM", exact=True).click()
    expect(screen.get_by_text("不會自動轉過去")).to_be_visible()
    expect(box).to_have_value("衝突保留")
    page.unroute("**/rooms/*/messages", conflict)


def test_latest_message_stays_expanded(page, login):
    name = "展開-" + uuid.uuid4().hex[:6]
    screen = _open(page, _room(page, login, name), name)
    box = screen.locator('textarea[name="text"]')
    older = "上一則長文\n第二行\n第三行\n第四行還在"
    box.fill(older)
    screen.get_by_role("button", name="傳給 PM", exact=True).click()
    box.fill("請回六段")
    screen.get_by_role("button", name="傳給 PM", exact=True).click()
    page.wait_for_function("""() => {
      const articles = [...document.querySelectorAll('.room-screen article[data-side="assistant"]')];
      const last = articles.at(-1);
      if (!last || last.dataset.status !== 'done') return false;
      return (last.innerText || '').includes('第 6 段') && !last.querySelector('summary');
    }""", timeout=30000)
    latest = screen.locator('article[data-side="assistant"]').last
    expect(latest.get_by_text("第 6 段", exact=False)).to_be_visible()
    expect(latest.locator("summary", has_text="展開全文")).to_have_count(0)
    previous = screen.locator("article", has_text="上一則長文").locator("summary", has_text="展開全文")
    expect(previous).to_be_visible()
    timeline = screen.locator(".room-timeline")
    previous.scroll_into_view_if_needed()
    before = timeline.evaluate("el => el.scrollTop")
    preview = previous.locator('span.whitespace-pre-wrap')
    expect(preview).to_be_visible()
    previous.click()
    expect(screen.get_by_text("第四行還在")).to_be_visible()
    expect(preview).not_to_be_visible()
    expect(previous.get_by_text("收合全文", exact=True)).to_be_visible()
    assert timeline.evaluate("el => el.scrollTop") == before
    OUT.mkdir(parents=True, exist_ok=True)
    page.screenshot(path=str(OUT / "expanded-without-preview.png"))
    previous.press("Enter")
    expect(preview).to_be_visible()
    expect(screen.get_by_text("第四行還在")).not_to_be_visible()
