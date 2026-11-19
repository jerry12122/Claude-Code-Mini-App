"""工作室點擊區、PM 排序及真實授權／問答通道；沿用隔離 server 與假 CLI。"""
from pathlib import Path
import re
import uuid

import pytest
from playwright.sync_api import expect


@pytest.mark.parametrize("width,scenario", [(1280, "PERM"), (390, "ROOM_QUESTION")])
def test_room_controls(page, login, width, scenario):
    page.set_viewport_size({"width": width, "height": 900})
    login()
    name = "room-" + uuid.uuid4().hex[:8]
    created = page.evaluate("""async name => {
      const post = async (url, body) => {
        const res = await apiFetch(url, {method:'POST', headers:{'Content-Type':'application/json'}, body:JSON.stringify(body)});
        if (!res.ok) throw new Error(await res.text());
        return res.json();
      };
      const worker = await post('/sessions', {name:name+' worker', agent_type:'claude', permission_mode:'default'});
      const pm = await post('/sessions', {name:name+' PM', agent_type:'claude', permission_mode:'default'});
      const room = await post('/rooms', {name, members:[{session_id:worker.id,role:'worker'},{session_id:pm.id,role:'pm'}]});
      return {room,worker,pm};
    }""", name)
    page.reload()
    card = page.locator(".room-list-card").filter(has_text=name)
    expect(card).to_be_visible()
    # 點按鈕的留白，不點文字；卡片與按鈕一樣高，不留下不可操作的 padding。
    opener = card.locator("button").first
    metrics = card.evaluate("""el => {
      const button = el.querySelector('button');
      const buttonBox = button.getBoundingClientRect();
      const cardBox = el.getBoundingClientRect();
      const session = document.querySelector('.ra-session-card:not(.room-list-card)');
      const sessionBox = session ? session.getBoundingClientRect() : null;
      const name = el.querySelector('.room-list-name');
      const sessionName = session ? session.querySelector('.truncate') : null;
      return {
        button: buttonBox.height,
        card: cardBox.height,
        coarse: matchMedia('(pointer: coarse)').matches,
        session: sessionBox ? sessionBox.height : buttonBox.height,
        nameTop: name ? name.getBoundingClientRect().top - cardBox.top : 0,
        sessionTop: sessionName ? sessionName.getBoundingClientRect().top - sessionBox.top : 0,
        roomBorder: getComputedStyle(el).borderTopColor,
        sessionBorder: session ? getComputedStyle(session).borderTopColor : '',
      };
    }""")
    if metrics["coarse"]:
        assert metrics["button"] >= 44, metrics
    assert abs(metrics["card"] - metrics["button"]) <= 2, metrics
    assert abs(metrics["card"] - metrics["session"]) <= 4, metrics
    assert abs(metrics["nameTop"] - metrics["sessionTop"]) <= 4, metrics
    assert metrics["roomBorder"] == metrics["sessionBorder"], metrics
    opener.click(position={"x": 3, "y": 3})
    screen = page.get_by_role("region", name="工作室 " + name, exact=True)
    expect(screen).to_be_visible()
    expect(screen.locator(".room-header [role=listitem]").first).to_contain_text(name + " PM")
    if scenario == "PERM":
        screen.locator('textarea[name="text"]').fill(scenario)
        screen.get_by_role("button", name="傳給 PM", exact=True).click()
        actions = screen.get_by_role("region", name=name + " PM 的授權與回答", exact=True)
        expect(actions.get_by_text("需要授權", exact=True)).to_be_visible(timeout=30000)
        expect(actions.get_by_text("ls -la", exact=False)).to_be_visible()
    else:
        # 對執行者開工，確認工作室也能回答執行者的選項，不只 PM。
        page.evaluate("""({id,text}) => new Promise((resolve,reject) => {
          const ws = new WebSocket(wsURL(id));
          ws.onerror = () => reject(new Error('fixture connection failed'));
          ws.onmessage = event => {
            const msg = JSON.parse(event.data);
            if (msg.type === 'sync') ws.send(JSON.stringify({type:'input', data:text, request_id:'room-question-fixture'}));
            if (msg.type === 'input_accepted') {ws.close();resolve();}
            if (msg.type === 'input_rejected') {ws.close();reject(new Error(msg.content));}
          };
        })""", {"id": created["worker"]["id"], "text": scenario})
        actions = screen.get_by_role("region", name=name + " worker 的授權與回答", exact=True)
        expect(actions.get_by_role("radio", name=re.compile("繼續"))).to_be_visible(timeout=30000)
        page.reload()
        opener.click()
        expect(actions.get_by_role("radio", name=re.compile("繼續"))).to_be_visible(timeout=30000)
    assert screen.evaluate("el => el.scrollWidth <= el.clientWidth")
    artifacts = Path("test-results/rooms")
    artifacts.mkdir(parents=True, exist_ok=True)
    page.screenshot(path=str(artifacts / f"{width}-{scenario}.png"), full_page=True)
    if scenario == "PERM":
        actions.get_by_role("button", name="拒絕", exact=True).click()
        expect(actions.get_by_text("需要授權", exact=True)).to_have_count(0)
    else:
        actions.get_by_role("radio", name=re.compile("繼續")).check()
        actions.get_by_role("button", name="送出回答", exact=True).click()
        expect(screen.get_by_text(re.compile("選擇結果：繼續"))).to_be_visible(timeout=30000)
    expect(screen).to_be_visible()
