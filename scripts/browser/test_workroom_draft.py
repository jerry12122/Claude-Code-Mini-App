"""工作室草稿與 @。跟既有修整測試分開，避免前段失敗就看不到這段。"""
import base64
import uuid

from playwright.sync_api import expect

PNG_B64 = base64.b64encode(base64.b64decode(
    "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg=="
)).decode()


def test_room_draft_survives_switch_and_mention_reaches_pm(page, login):
    mark = uuid.uuid4().hex[:6]
    login()
    created = page.evaluate("""async mark => {
      const post = async (url, body) => {
        const res = await apiFetch(url, {method:'POST', headers:{'Content-Type':'application/json'}, body:JSON.stringify(body)});
        if (!res.ok) throw new Error(await res.text());
        return res.json();
      };
      const pm = await post('/sessions', {name: '草稿PM-' + mark, agent_type:'claude', permission_mode:'default'});
      const alpha = await post('/sessions', {name: '同名', work_dir: 'D:/mention-root/alpha-' + mark, agent_type:'claude', permission_mode:'default'});
      const beta = await post('/sessions', {name: '同名', work_dir: 'D:/mention-root/beta-' + mark, agent_type:'codex', permission_mode:'default'});
      const other = await post('/sessions', {name: '另一間PM-' + mark, agent_type:'claude', permission_mode:'default'});
      const room = await post('/rooms', {name: '草稿間-' + mark, members:[{session_id:pm.id, role:'pm'}]});
      const otherRoom = await post('/rooms', {name: '空草稿-' + mark, members:[{session_id:other.id, role:'pm'}]});
      return {pm, alpha, beta, room, otherRoom};
    }""", mark)
    page.reload()
    room_name = "草稿間-" + mark
    page.locator(".room-list-card").filter(has_text=room_name).locator("button").first.click()
    screen = page.get_by_role("region", name="工作室 " + room_name, exact=True)
    box = screen.locator('textarea[name="text"]')
    box.fill("房間A草稿")
    box.press_sequentially(" @同名")
    menu = screen.get_by_role("listbox", name="提及 session")
    expect(menu).to_be_visible()
    expect(menu.get_by_role("option")).to_have_count(2)
    page.evaluate("""() => {
      const input = document.querySelector('.room-screen textarea[name="text"]');
      const event = new KeyboardEvent('keydown', { key: 'Enter', bubbles: true, cancelable: true });
      Object.defineProperty(event, 'isComposing', { get: () => true });
      Object.defineProperty(event, 'keyCode', { get: () => 229 });
      input.dispatchEvent(event);
    }""")
    expect(screen.get_by_label("已標記要詢問的 session")).to_have_count(0)
    menu.get_by_role("option").filter(has_text="alpha-" + mark).click()
    chip = screen.get_by_label("已標記要詢問的 session")
    expect(chip).to_be_visible()
    assert created["alpha"]["id"] in (chip.locator("span[title]").first.get_attribute("title") or "")
    chip.get_by_role("button", name="移除 同名").click()
    expect(chip).to_have_count(0)
    box.press_sequentially("@同名")
    expect(menu).to_be_visible()
    box.press("ArrowDown")
    box.press("Enter")
    expect(chip).to_be_visible()
    chip.get_by_role("button", name="移除 同名").click()
    box.fill("房間A草稿")
    box.press_sequentially(" @同名")
    menu.get_by_role("option").filter(has_text="alpha-" + mark).click()
    page.evaluate("""(png) => {
      const binary = atob(png);
      const bytes = new Uint8Array(binary.length);
      for (let i = 0; i < binary.length; i += 1) bytes[i] = binary.charCodeAt(i);
      const data = new DataTransfer();
      data.items.add(new File([bytes], 'draft.png', { type: 'image/png' }));
      const input = document.querySelector('.room-screen textarea[name="text"]');
      const event = new Event('paste', { bubbles: true, cancelable: true });
      Object.defineProperty(event, 'clipboardData', { value: data });
      input.dispatchEvent(event);
    }""", PNG_B64)
    expect(screen.locator('[aria-label="待送出的附件"] img')).to_have_count(1)
    stored = page.evaluate("""({roomId, pmId}) => ({
      room: localStorage.getItem('cc_room_draft:' + roomId) || '',
      session: localStorage.getItem('cc_draft_input:' + pmId) || '',
    })""", {"roomId": created["room"]["id"], "pmId": created["pm"]["id"]})
    assert "房間A草稿" in stored["room"]
    assert created["alpha"]["id"] in stored["room"]
    assert "房間A草稿" not in stored["session"]
    page.get_by_role("button", name="關閉工作室", exact=True).click()
    page.locator(".room-list-card").filter(has_text="空草稿-" + mark).locator("button").first.click()
    other = page.get_by_role("region", name="工作室 空草稿-" + mark, exact=True)
    expect(other.locator('textarea[name="text"]')).to_have_value("")
    expect(other.get_by_label("已標記要詢問的 session")).to_have_count(0)
    page.get_by_role("button", name="關閉工作室", exact=True).click()
    page.locator(".room-list-card").filter(has_text=room_name).locator("button").first.click()
    expect(box).to_have_value("房間A草稿 ")
    expect(chip).to_be_visible()
    page.wait_for_function("""() => { const img = document.querySelector('.room-screen [aria-label="待送出的附件"] img'); return !!img && img.naturalWidth > 0; }""")
    page.reload()
    page.locator(".room-list-card").filter(has_text=room_name).locator("button").first.click()
    expect(box).to_have_value("房間A草稿 ")
    expect(screen.get_by_label("已標記要詢問的 session")).to_be_visible()
    page.wait_for_function("""() => { const img = document.querySelector('.room-screen [aria-label="待送出的附件"] img'); return !!img && img.naturalWidth > 0; }""")
    captured = {}

    def grab(route):
        captured["body"] = route.request.post_data_json
        route.continue_()

    page.route("**/rooms/*/messages", grab)
    screen.get_by_role("button", name="傳給 PM", exact=True).click()
    expect(box).to_have_value("", timeout=30000)
    page.unroute("**/rooms/*/messages", grab)
    body = captured["body"]
    assert body["pm_id"] == created["pm"]["id"]
    assert created["alpha"]["id"] in body["text"]
    assert "session_id=" + created["beta"]["id"] not in body["text"]
    assert "[miniapp]" in body["text"]
    box.fill("失敗還在")

    def reject(route):
        route.fulfill(status=500, content_type="application/json", body='{"error":"暫時無法送出"}')

    page.route("**/rooms/*/messages", reject)
    screen.get_by_role("button", name="傳給 PM", exact=True).click()
    expect(screen.get_by_role("alert")).to_contain_text("暫時無法送出")
    expect(box).to_have_value("失敗還在")
    page.unroute("**/rooms/*/messages", reject)
    page.reload()
    page.locator(".room-list-card").filter(has_text=room_name).locator("button").first.click()
    expect(screen.locator('textarea[name="text"]')).to_have_value("失敗還在")
