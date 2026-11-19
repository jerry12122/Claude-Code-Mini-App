"""工作室版面：真實訊息的捲動錨點、容器寬度，以及真正的手機 viewport。"""
from pathlib import Path
import uuid

from playwright.sync_api import expect

OUT = Path("test-results/workroom-layout")
CONTAINER_WIDTHS = [1280, 996, 960, 768]
PHONE_WIDTHS = [390, 320]


def _metrics(screen):
    result = screen.evaluate("""el => {
      const board = el.querySelector('.room-board');
      const timeline = el.querySelector('.room-timeline');
      const parent = el.parentElement;
      const boardCs = board ? getComputedStyle(board) : null;
      const timelineCs = timeline ? getComputedStyle(timeline) : null;
      return {
        width: el.clientWidth,
        scroll: el.scrollHeight <= el.clientHeight + 1,
        parentScroll: !parent || parent.scrollHeight <= parent.clientHeight + 1,
        boardDisplay: boardCs ? boardCs.display : '',
        boardWidth: board ? Math.round(board.getBoundingClientRect().width) : 0,
        timelineBg: timelineCs ? timelineCs.backgroundColor : '',
        wide: el.scrollWidth <= el.clientWidth + 1,
        parentWide: !parent || parent.scrollWidth <= parent.clientWidth + 1,
        timelineScrolls: !!(timeline && timeline.scrollHeight > timeline.clientHeight + 40),
        heights: [el, parent, el.querySelector('.room-header'), el.querySelector('.room-workspace'), el.querySelector('.room-thread'), el.querySelector('.room-thread > .relative'), el.querySelector('.room-composer'), timeline, timeline?.firstElementChild].filter(Boolean).map(n => ({cls:n.className, client:n.clientHeight, scroll:n.scrollHeight, rect:n.getBoundingClientRect().height, flex:getComputedStyle(n).flex, top:n.getBoundingClientRect().top,bottom:n.getBoundingClientRect().bottom,overflow:getComputedStyle(n).overflow,position:getComputedStyle(n).position})),
      };
    }""")
    if not result['scroll'] or not result['parentScroll']:
        print('LAYOUT_DIAGNOSTIC', result, flush=True)
    return result


def _channel(color):
    value = color / 255
    return value / 12.92 if value <= 0.03928 else ((value + 0.055) / 1.055) ** 2.4


def _contrast(foreground, background):
    def lum(rgb):
        return 0.2126 * _channel(rgb[0]) + 0.7152 * _channel(rgb[1]) + 0.0722 * _channel(rgb[2])
    lighter, darker = max(lum(foreground), lum(background)), min(lum(foreground), lum(background))
    return (lighter + 0.05) / (darker + 0.05)


def _surface(screen):
    """用實際 computed style 量溢寬、44px 與正文對比，不寫死預期色。"""
    raw = screen.evaluate("""el => {
      const parse = (value) => {
        if (!value) return null;
        const rgb = value.match(/rgba?\\(([^)]+)\\)/);
        if (rgb) {
          const parts = rgb[1].split(',').map(part => parseFloat(part));
          return {r: parts[0], g: parts[1], b: parts[2], a: parts.length > 3 ? parts[3] : 1};
        }
        const modern = value.match(/color\\(srgb ([\\d.]+) ([\\d.]+) ([\\d.]+)(?: \\/ ([\\d.]+))?\\)/);
        if (!modern) {
          const ctx = document.createElement('canvas').getContext('2d');
          ctx.fillStyle = value; ctx.fillRect(0, 0, 1, 1);
          const p = ctx.getImageData(0, 0, 1, 1).data;
          return {r: p[0], g: p[1], b: p[2], a: p[3] / 255};
        }
        return {r: parseFloat(modern[1]) * 255, g: parseFloat(modern[2]) * 255, b: parseFloat(modern[3]) * 255, a: modern[4] ? parseFloat(modern[4]) : 1};
      };
      const paint = (node) => {
        let current = node;
        let bg = null;
        while (current) {
          const color = parse(getComputedStyle(current).backgroundColor);
          if (color && color.a > 0.05) { bg = color; break; }
          current = current.parentElement;
        }
        let fg = parse(getComputedStyle(node).color);
        if (fg && bg && fg.a < 1) {
          fg = {
            r: fg.r * fg.a + bg.r * (1 - fg.a),
            g: fg.g * fg.a + bg.g * (1 - fg.a),
            b: fg.b * fg.a + bg.b * (1 - fg.a),
          };
        }
        return {fg, bg};
      };
      const body = el.querySelector('article[data-align="right"] .whitespace-pre-wrap');
      const colors = body ? paint(body) : {fg: null, bg: null};
      const shown = (node) => node && node.getClientRects().length > 0 && getComputedStyle(node).visibility !== 'hidden';
      const pick = (selector) => el.querySelector(selector);
      const controls = [
        pick('button[aria-label="關閉工作室"]'),
        pick('button[aria-label="傳給 PM"]'),
        pick('.room-pending'),
        pick('.room-board-toggle'),
        [...el.querySelectorAll('.room-member-chip')].find(shown),
        pick('summary'),
      ].filter(shown).map(node => {
        const box = node.getBoundingClientRect();
        return {name: (node.getAttribute('aria-label') || node.textContent || '').trim().slice(0, 24), h: box.height, w: box.width};
      });
      return {fg: colors.fg, bg: colors.bg, controls};
    }""")
    assert raw["fg"] and raw["bg"], raw
    ratio = _contrast((raw["fg"]["r"], raw["fg"]["g"], raw["fg"]["b"]), (raw["bg"]["r"], raw["bg"]["g"], raw["bg"]["b"]))
    assert ratio >= 4.5, {"ratio": ratio, "fg": raw["fg"], "bg": raw["bg"]}
    for control in raw["controls"]:
        assert control["h"] >= 44, control
        if control["w"] <= 48:
            assert control["w"] >= 44, control
    return ratio


def _send_input(page, session_id, text):
    page.evaluate("""({id, text}) => new Promise((resolve, reject) => {
      const ws = new WebSocket(wsURL(id));
      ws.onerror = () => reject(new Error('fixture connection failed'));
      ws.onmessage = event => {
        const msg = JSON.parse(event.data);
        if (msg.type === 'sync') ws.send(JSON.stringify({type:'input', data:text, request_id:'layout-' + id}));
        if (msg.type === 'input_accepted') { ws.close(); resolve(); }
        if (msg.type === 'input_rejected') { ws.close(); reject(new Error(msg.content)); }
      };
    })""", {"id": session_id, "text": text})


def test_workroom_layout(page, login):
    page.set_default_timeout(180000)
    page.set_viewport_size({"width": 1680, "height": 900})
    login()
    token = uuid.uuid4().hex[:8]
    name = "版面" + token
    long_name = "很長的PM名稱" + ("甲" * 40)
    memo = "MEMO-MARKER-10000" + ("備" * 9980)
    created = page.evaluate("""async ({name, longName, memo}) => {
      const post = async (url, body) => {
        const res = await apiFetch(url, {method:'POST', headers:{'Content-Type':'application/json'}, body:JSON.stringify(body)});
        if (!res.ok) throw new Error(await res.text());
        return res.json();
      };
      const pm = await post('/sessions', {name: longName, agent_type:'claude', permission_mode:'default'});
      const perm = await post('/sessions', {name: name + ' 授權者', agent_type:'claude', permission_mode:'default'});
      const ask = await post('/sessions', {name: name + ' 回答者', agent_type:'claude', permission_mode:'default'});
      const members = [
        {session_id: pm.id, role:'pm'},
        {session_id: perm.id, role:'worker'},
        {session_id: ask.id, role:'worker'},
      ];
      for (let i = 0; i < 47; i++) {
        const worker = await post('/sessions', {name: name + '-w' + i, agent_type:'claude', permission_mode:'default'});
        members.push({session_id: worker.id, role:'worker'});
      }
      const room = await post('/rooms', {name, members});
      const memoRes = await apiFetch('/rooms/' + room.id + '/board', {
        method:'PUT', headers:{'Content-Type':'application/json'},
        body: JSON.stringify({text: memo, previous: ''}),
      });
      if (!memoRes.ok) throw new Error(await memoRes.text());
      return {perm: perm.id, ask: ask.id};
    }""", {"name": name, "longName": long_name, "memo": memo})
    page.reload()
    card = page.locator(".room-list-card").filter(has_text=name)
    expect(card).to_be_visible()
    card.locator("button").first.click()
    screen = page.get_by_role("region", name="工作室 " + name, exact=True)
    expect(screen).to_be_visible()
    OUT.mkdir(parents=True, exist_ok=True)

    def fit(px):
        screen.evaluate(
            """(el, px) => { el.style.width = px + 'px'; el.style.maxWidth = px + 'px'; el.style.flex = 'none'; }""",
            px,
        )

    def clear_fit():
        screen.evaluate("""el => { el.style.width = ''; el.style.maxWidth = ''; el.style.flex = ''; }""")

    fit(768)
    expect(screen.locator(".room-pm-state")).to_contain_text("閒置")
    box = screen.locator('textarea[name="text"]')
    lines = "\n".join(f"第{i}行可讀正文，用來確認時間軸真的能捲動並在切換後留在原處。" for i in range(1, 48))
    box.fill("錨點-ALPHA\n" + lines)
    screen.get_by_role("button", name="傳給 PM", exact=True).click()
    timeline = screen.locator(".room-timeline")
    message = screen.locator('article[data-align="right"]').filter(has_text="錨點-ALPHA").first
    expect(message).to_be_visible()
    message.locator("summary").filter(has_text="展開全文").click()
    page.wait_for_function("""() => {
      const el = document.querySelector('.room-timeline');
      return !!el && el.scrollHeight > el.clientHeight + 80;
    }""")
    box.fill("草稿保留")
    saved = timeline.evaluate("""el => {
      const max = el.scrollHeight - el.clientHeight;
      el.scrollTop = Math.floor(max / 2);
      const rect = el.getBoundingClientRect();
      const node = document.elementFromPoint(rect.left + 24, rect.top + 16);
      return {max, top: el.scrollTop, anchor: node ? node.textContent.slice(0, 40) : ''};
    }""")
    assert saved["max"] > 80, saved
    assert saved["top"] > 0, saved
    assert saved["anchor"], saved
    screen.get_by_role("button", name="工作概況").click()
    expect(screen.locator(".room-thread")).to_be_hidden()
    expect(screen.locator("#room-work-board")).to_be_visible()
    screen.get_by_role("button", name="回到對話").click()
    expect(box).to_have_value("草稿保留")
    kept = timeline.evaluate("""el => {
      const rect = el.getBoundingClientRect();
      const node = document.elementFromPoint(rect.left + 24, rect.top + 16);
      return {top: el.scrollTop, anchor: node ? node.textContent.slice(0, 40) : ''};
    }""")
    assert kept["top"] == saved["top"], kept
    assert kept["anchor"] == saved["anchor"], kept

    for px in CONTAINER_WIDTHS:
        fit(px)
        toggle = screen.locator(".room-board-toggle")
        if px >= 960 and toggle.get_attribute("aria-expanded") != "true":
            toggle.click()
        elif px < 960 and toggle.get_attribute("aria-expanded") == "true":
            toggle.click()
        m = _metrics(screen)
        assert abs(m["width"] - px) <= 1, m
        assert m["scroll"], m
        assert m["parentScroll"], m
        assert m["wide"], m
        assert m["parentWide"], m
        assert m["timelineScrolls"], m
        assert m["timelineBg"] not in ("rgba(0, 0, 0, 0)", "transparent"), m
        if px >= 960:
            assert m["boardDisplay"] != "none", m
            assert 278 <= m["boardWidth"] <= 342, m
            expect(screen.locator(".room-members-full")).to_be_visible()
            expect(toggle).to_be_visible()
            assert toggle.get_attribute("aria-expanded") == "true"
        else:
            assert m["boardDisplay"] == "none", m
            expect(screen.get_by_role("button", name="工作概況")).to_be_visible()
            expect(screen.get_by_role("button", name="成員 50", exact=True)).to_be_visible()
        timeline.evaluate("el => { el.scrollTop = 0; }")
        expect(screen.get_by_text("錨點-ALPHA").first).to_be_visible()
        _surface(screen)
        page.screenshot(path=str(OUT / f"container-{px}.png"))

    fit(1280)
    if screen.locator(".room-board-toggle").get_attribute("aria-expanded") != "true":
        screen.locator(".room-board-toggle").click()
    expect(screen.locator(".room-header [role=listitem]").first).to_contain_text("很長的PM名稱")
    board = screen.locator("#room-work-board")
    expect(board.get_by_role("heading", name="進行中與需要處理")).to_be_visible()
    expect(board.get_by_text("MEMO-MARKER-10000")).to_have_count(0)
    board.get_by_role("button", name="備忘", exact=True).click()
    expect(board.get_by_text("MEMO-MARKER-10000")).to_be_visible()
    page.screenshot(path=str(OUT / "memo.png"))
    board.get_by_role("button", name="派工", exact=True).click()

    _send_input(page, created["perm"], "PERM")
    _send_input(page, created["ask"], "ROOM_QUESTION")
    expect(screen.get_by_role("button", name="待你處理 2", exact=True)).to_be_visible(timeout=30000)
    expect(screen.get_by_role("region", name=name + " 授權者 的授權與回答", exact=True)).to_be_visible()
    expect(screen.get_by_role("region", name=name + " 回答者 的授權與回答", exact=True)).to_be_visible()
    page.screenshot(path=str(OUT / "pending-two.png"))
    if screen.locator(".room-board-toggle").get_attribute("aria-expanded") == "true":
        screen.locator(".room-board-toggle").click()
    fit(768)
    screen.get_by_role("button", name="工作概況").click()
    expect(screen.locator(".room-thread")).to_be_hidden()
    screen.get_by_role("button", name="待你處理 2", exact=True).click()
    expect(screen.locator(".room-thread")).to_be_visible()
    opened = screen.locator('[id^="room-action-"]').first
    expect(opened).to_be_visible()
    page.wait_for_function("""() => {
      const el = document.activeElement;
      return !!(el && el.id && el.id.startsWith('room-action-'));
    }""")
    assert page.evaluate("() => document.activeElement.id") == opened.get_attribute("id")

    clear_fit()
    for px in PHONE_WIDTHS:
        page.set_viewport_size({"width": px, "height": 800})
        phone = _metrics(screen)
        assert phone["width"] <= px, phone
        assert phone["width"] >= px - 24, phone
        assert phone["boardDisplay"] == "none", phone
        assert phone["scroll"], phone
        assert phone["parentScroll"], phone
        assert phone["wide"], phone
        assert phone["parentWide"], phone
        assert phone["timelineScrolls"], phone
        expect(screen.get_by_role("button", name="工作概況")).to_be_visible()
        expect(screen.get_by_role("button", name="成員 50", exact=True)).to_be_visible()
        timeline.evaluate("el => { el.scrollTop = 0; }")
        expect(screen.get_by_text("錨點-ALPHA").first).to_be_visible()
        _surface(screen)
        page.screenshot(path=str(OUT / f"viewport-{px}.png"))

    page.set_viewport_size({"width": 1680, "height": 900})
    fit(996)
    page.evaluate("() => { document.body.style.background = 'rgb(0, 255, 0)'; }")
    contrast = _metrics(screen)
    assert "0, 255, 0" not in contrast["timelineBg"], contrast
    assert contrast["timelineBg"] not in ("rgba(0, 0, 0, 0)", "transparent")
    timeline.evaluate("el => { el.scrollTop = 0; }")
    expect(screen.get_by_text("錨點-ALPHA").first).to_be_visible()
    _surface(screen)
    page.screenshot(path=str(OUT / "contrast.png"))

    page.evaluate("() => { document.documentElement.dataset.theme = 'light'; }")
    _surface(screen)
    page.screenshot(path=str(OUT / "light.png"))
    page.evaluate("() => { document.documentElement.dataset.theme = 'dark'; }")
    _surface(screen)
    page.screenshot(path=str(OUT / "dark.png"))
    clear_fit()
    zoom = page.context.new_cdp_session(page)
    # 200% reflow: halve CSS viewport and double device pixels, preserving the physical screen size.
    zoom.send('Emulation.setDeviceMetricsOverride', {'width': 840, 'height': 450, 'deviceScaleFactor': 2, 'mobile': False})
    zoomed = _metrics(screen)
    assert zoomed["scroll"], zoomed
    assert zoomed["parentScroll"], zoomed
    assert zoomed["wide"], zoomed
    assert zoomed["parentWide"], zoomed
    assert zoomed["timelineScrolls"], zoomed
    _surface(screen)
    page.screenshot(path=str(OUT / "zoom-dark.png"))
    zoom.send('Emulation.clearDeviceMetricsOverride')
    zoom.detach()
    page.set_viewport_size({"width": 1680, "height": 420})
    fit(996)
    short = _metrics(screen)
    assert short["scroll"], short
    assert short["parentScroll"], short
    assert short["wide"], short
    assert short["parentWide"], short
    assert short["timelineScrolls"], short
    _surface(screen)
    page.screenshot(path=str(OUT / "short.png"))
