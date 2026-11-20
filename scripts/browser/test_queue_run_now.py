"""佇列立刻執行：真 loopback／fake CLI happy path，與明確標示的受控 WS DOM 邊界。"""
import json
import os
import re
from pathlib import Path
import uuid

from playwright.sync_api import expect

OUT = Path(os.environ.get("QUEUE_RUN_NOW_ARTIFACTS", "test-results/queue-run-now"))


def test_queue_run_now_dom(page, login):
    OUT.mkdir(parents=True, exist_ok=True)
    login()
    name = "queue-run-now-" + uuid.uuid4().hex[:8]
    page.get_by_role("button", name="建立新 Session", exact=True).click()
    page.get_by_placeholder("Session 名稱（選填，留空自動產生）").fill(name)
    page.get_by_role("button", name="建立", exact=True).click()
    draft = page.locator("textarea[data-chat-input]")
    expect(draft).to_be_enabled()
    sid = page.evaluate("""async name => (await (await apiFetch('/sessions')).json()).find(s => s.name === name).id""", name)
    frames = []
    def socket_open(ws):
        ws.on("framesent", lambda text: frames.append(json.loads(text)))
    page.on("websocket", socket_open)
    # 此 socket 已於建立聊天室時連上，重新載入以擷取整個輸入／中斷流程。
    page.reload()
    expect(draft).to_be_enabled()
    try:
        draft.fill("QUEUE_RUN_NOW_UI_A")
        page.get_by_role("button", name="送出", exact=True).click()
        expect(page.get_by_text("queue-run-now partial A", exact=False)).to_be_visible(timeout=15000)
        for text in ["排隊 B", "QUEUE_RUN_NOW_UI_C"]:
            draft.fill(text)
            page.get_by_role("button", name="排入佇列", exact=True).click()
            expect(draft).to_have_value("")
        queue = page.get_by_label("排隊中的訊息", exact=True)
        expect(queue.locator("li")).to_have_count(2)
        buttons = queue.get_by_role("button", name="立刻執行", exact=True)
        expect(buttons).to_have_count(2)  # RED：原始真 DOM 缺少此獨立按鈕。
        assert not [m for m in frames if m.get("type") == "queue_run_now"]
        draft.fill("手機草稿 中文輸入")
        draft.focus()
        page.evaluate("""() => {const el=document.querySelector('textarea[data-chat-input]'); el.setSelectionRange(2,5)}""")
        for width, height in [(1280, 800), (390, 844), (320, 640)]:
            page.set_viewport_size({"width": width, "height": height})
            assert page.evaluate("document.documentElement.scrollWidth <= innerWidth")
            for button in buttons.all():
                box = button.bounding_box()
                assert 24 <= box["width"] <= 28 and 24 <= box["height"] <= 28, box
                assert button.evaluate("el => el.querySelector('svg') !== null && el.innerText.trim() === ''")
            for row in queue.locator("li").all():
                rbox = row.bounding_box()
                assert 24 <= rbox["height"] <= 28, rbox
            page.screenshot(path=str(OUT / f"queue-{width}.png"))
        # 響應式 composer 在換寬度時可能重建 textarea；先建立本次動作的焦點／選取前提。
        draft.focus()
        page.evaluate("""() => document.querySelector('textarea[data-chat-input]').setSelectionRange(2,5)""")
        assert page.evaluate("document.activeElement === document.querySelector('textarea[data-chat-input]')")
        buttons.nth(1).click()
        expect(queue.locator("li")).to_have_count(1, timeout=20000)  # Windows runner 的 WaitDelay 為 5 秒，後端等待上限為 15 秒。
        expect(queue).to_contain_text("排隊 B")
        expect(page.get_by_text("queue-run-now partial C", exact=False)).to_be_visible(timeout=15000)
        expect(draft).to_have_value("手機草稿 中文輸入")
        assert page.evaluate("document.activeElement === document.querySelector('textarea[data-chat-input]')")
        assert page.evaluate("document.querySelector('textarea[data-chat-input]').selectionStart") == 2
        requests = [m for m in frames if m.get("type") == "queue_run_now"]
        assert len(requests) == 1 and set(requests[0]) == {"type", "id"}
        history = page.evaluate("""async id => (await apiFetch('/sessions/'+id+'/messages')).json()""", sid)
        users = [m["content"] for m in history if m["role"] == "user"]
        assert users == ["QUEUE_RUN_NOW_UI_A", "QUEUE_RUN_NOW_UI_C"], users
        assert history[1]["status"] == "done" and "partial A" in history[1]["content"]
        (OUT / "real-flow.json").write_text(json.dumps({"kind": "real-server-fake-cli", "frames": frames, "users": users, "old_finalized": True}, ensure_ascii=False, indent=2), encoding="utf-8")
    finally:
        # 只清自己建立的 session；先透過既有 WS API 移除剩餘佇列並停止 fake，再刪暫存資料。
        cleanup = page.evaluate("""async id => {
          await new Promise((resolve,reject) => {
            const ws=new WebSocket(wsURL(id)); let stopping=false;
            const timer=setTimeout(()=>{ws.close();reject(new Error('fake cleanup timeout'))},10000);
            ws.onmessage=e=>{const m=JSON.parse(e.data);
              if(m.type==='sync'&&!stopping){stopping=true; for(const q of m.queue||[]) ws.send(JSON.stringify({type:'queue_remove',id:q.id})); ws.send(JSON.stringify({type:'interrupt'})); ws.send(JSON.stringify({type:'ping'}))}
              if(stopping&&((m.type==='status'&&m.value==='IDLE')||m.type==='pong')){clearTimeout(timer);ws.close();resolve()}
            }; ws.onerror=()=>reject(new Error('cleanup websocket failed'));
          });
          let idle=false;
          for(let n=0;n<250;n++){const s=await (await apiFetch('/sessions')).json();const current=s.find(s=>s.id===id);if(!current||current.status==='idle'){idle=true;break}await new Promise(r=>setTimeout(r,50))}
          if(!idle)throw new Error('fake runner did not finish cleanup');
          const removed=await apiFetch('/sessions/'+id,{method:'DELETE'});return {deleted:removed.status===204};
        }""", sid)
        (OUT / "cleanup.json").write_text(json.dumps(cleanup), encoding="utf-8")
        assert cleanup["deleted"]


def _controlled_env(page, login, clock=False):
    """受控 WS 資料；使用真正 ChatView 與 useChatSocket，不代表真後端狀態／權限 E2E。

    route_web_socket 只攔截「註冊之後才載入的文件」裡建立的 WebSocket，所以必須在 login()（頁面載入）之前註冊；
    晚於載入才註冊時，hook 會連到真 server 而不是 fixture（舊版的失敗原因，見 worker-report-ui-repair.md）。
    """
    routes, frames = [], []
    current = {"value": "STREAMING", "lingering": False}
    queue = [{"id": 701, "content": "受控排隊 B"}, {"id": 702, "content": "受控排隊 C"}]
    def connect(ws):
        routes.append(ws)
        ws.on_message(lambda text: frames.append({"url": ws.url, "body": json.loads(text)}))
    page.context.route("**/guest/me*", lambda r: r.fulfill(status=200, json={"role": "editor", "mode": "live"}))
    page.context.route("**/messages*", lambda r: r.fulfill(status=200, json=[]))
    page.route_web_socket(re.compile(r"/sessions/run-now-fixture-[^/]+/ws"), connect)
    login()
    if clock:
        page.clock.install()  # 之後建立的計時器由測試快轉；產品 timeout 不縮短
    page.evaluate("""() => {
      document.getElementById('root').hidden=true;
      const host=document.createElement('div'); host.id='queue-ui-fixture'; host.style='position:fixed;inset:0;background:var(--ra-bg);z-index:100'; document.body.append(host);
      window.queueFixtureRoot=ReactDOM.createRoot(host);
      window.mountQueueFixture=p=>queueFixtureRoot.render(React.createElement(ChatView,{session:p.session,guest:p.guest||null,onBack:()=>{}}));
    }""")
    root = page.locator("#queue-ui-fixture")
    counter = 0
    def wait_connection(before):
        # 不能在 route handler 裡呼叫 page.evaluate（同步 API 會死結）；改由測試端輪詢已攔到的連線數。
        for _ in range(200):
            if len(routes) > before:
                return
            page.wait_for_timeout(50)
        raise AssertionError("hook 沒有建立 fixture WebSocket")
    def mount(agent="claude", value="STREAMING", lingering=False, guest=None, sid=None):
        nonlocal counter
        counter += 1
        current.update(value=value, lingering=lingering)
        sid = sid or f"run-now-fixture-{counter}"
        before = len(routes)
        page.evaluate("p=>mountQueueFixture(p)", {"session": {"id": sid, "name": "受控佇列", "agent_type": agent, "permission_mode": "default", "input_mode": "agent"}, "guest": guest})
        wait_connection(before)
        routes[-1].send(json.dumps({"type": "sync", "messages": [], "queue": queue, **current}))
        # 真 socket hook 已建立 fixture 連線並套用 queue 後再斷言可見性。
        readonly = agent == "kiro" or (guest and guest["role"] == "viewer")
        expect(root.locator(".ra-chat-composer")).to_have_count(0 if readonly else 1)
        if not readonly:
            expect(root.get_by_label("排隊中的訊息", exact=True).locator("li")).to_have_count(2)
        page.wait_for_function("document.body.textContent.includes('受控佇列')")
        assert len(routes) > before
        return sid
    def sync():
        """對目前（含重連後）的 socket 補送 sync，否則 hook 的 15 秒 sync 守門會把它關掉。"""
        routes[-1].send(json.dumps({"type": "sync", "messages": [], "queue": queue, **current}))
    return root, mount, sync, wait_connection, routes, frames, queue


def test_queue_run_now_controlled_dom(page, login):
    OUT.mkdir(parents=True, exist_ok=True)
    _controlled_socket_dom(page, login)


def _controlled_socket_dom(page, login):
    root, mount, _sync, _wait, routes, frames, queue = _controlled_env(page, login)
    buttons = root.get_by_role("button", name="立刻執行", exact=True)
    guards = []
    for agent in ["claude", "codex", "cursor", "kiro", "kiroacp", "antigravity", "gemini", "telegram"]:
        mount(agent)
        count = 2 if agent in ["claude", "codex", "cursor"] else 0
        expect(buttons).to_have_count(count)
        guards.append({"agent": agent, "buttons": count})
    mount(value="IDLE"); expect(buttons).to_have_count(0)
    mount(value="IDLE", lingering=True); expect(buttons).to_have_count(2)
    mount(value="AWAITING_CONFIRM"); expect(buttons).to_have_count(0)
    mount(guest={"role": "viewer", "mode": "live", "nickname": "viewer"}); expect(buttons).to_have_count(0)
    old_id = mount(guest={"role": "editor", "mode": "live", "nickname": "editor"})
    expect(buttons).to_have_count(2)
    draft = root.locator("textarea[data-chat-input]")
    draft.fill("受控草稿 中文組字")
    draft.focus()
    page.evaluate("""() => {
      const el=document.querySelector('#queue-ui-fixture textarea[data-chat-input]');
      el.dispatchEvent(new CompositionEvent('compositionstart',{bubbles:true}));
      el.dispatchEvent(new KeyboardEvent('keydown',{key:'Enter',isComposing:true,bubbles:true}));
      el.dispatchEvent(new CompositionEvent('compositionend',{bubbles:true}));
    }""")
    assert not [f for f in frames if f["body"].get("type") == "input"]
    # 同步送出丟例外：仍走 sendOrToast，不留下 pending。
    page.evaluate("""() => {
      window.queueOriginalSend=WebSocket.prototype.send;
      WebSocket.prototype.send=function(data){if(JSON.parse(data).type==='queue_run_now')throw new Error('fixture send failure');return queueOriginalSend.call(this,data)};
    }""")
    buttons.nth(1).click()
    expect(buttons.nth(1)).to_be_enabled()
    expect(page.get_by_text("連線中斷，操作未送出，請稍後再試", exact=True)).to_be_visible()
    page.evaluate("() => { WebSocket.prototype.send = queueOriginalSend; }")
    # 原生鍵盤操作，接著同步連點也不得重送。
    buttons.nth(1).focus(); buttons.nth(1).press("Space")
    expect(buttons.nth(1)).to_be_disabled()
    page.evaluate("""() => {for(let i=0;i<2;i++)Array.from(document.querySelectorAll('#queue-ui-fixture button')).find(b=>b.getAttribute('aria-label')==='立刻執行'||b.textContent.trim()==='立刻執行').click()}""")
    assert len([f for f in frames if f["body"].get("type") == "queue_run_now"]) == 1
    routes[-1].send(json.dumps({"type": "queue_update", "queue": queue}))
    expect(buttons.nth(1)).to_be_disabled()
    routes[-1].send(json.dumps({"type": "error", "content": "受控後端拒絕，請重試"}))
    expect(buttons.nth(1)).to_be_enabled()
    expect(page.get_by_text("受控後端拒絕，請重試", exact=True)).to_be_visible()
    expect(draft).to_have_value("受控草稿 中文組字")
    buttons.nth(1).click(); expect(buttons.nth(1)).to_be_disabled()
    before_close = len(routes)
    routes[-1].close(code=1000, reason="fixture disconnect")
    _wait(before_close)
    _sync()
    expect(buttons.nth(1)).to_be_enabled(timeout=10000)  # hook 重連後收到新的 sync，沒有自動重送。
    assert len([f for f in frames if f["body"].get("type") == "queue_run_now"]) == 2
    expect(draft).to_have_value("受控草稿 中文組字")
    buttons.nth(1).click(); expect(buttons.nth(1)).to_be_disabled()
    mount(sid="run-now-fixture-switched")
    expect(buttons.nth(1)).to_be_enabled()
    assert all(old_id in f["url"] for f in frames if f["body"].get("type") == "queue_run_now")
    mount(sid=old_id, guest={"role": "editor", "mode": "live", "nickname": "editor"})
    expect(draft).to_have_value("受控草稿 中文組字")
    buttons.nth(1).click(); expect(buttons.nth(1)).to_be_disabled()
    routes[-1].send(json.dumps({"type": "queue_update", "queue": queue[:1]}))
    expect(buttons).to_have_count(1); expect(buttons.first).to_be_enabled()
    (OUT / "controlled-fixture.json").write_text(json.dumps({"kind": "controlled-websocket-dom", "guards": guards, "frames": frames, "keyboard_ime_draft_error_disconnect_switch_success": True}, ensure_ascii=False, indent=2), encoding="utf-8")
    page.evaluate("queueFixtureRoot.unmount()")


TIMEOUT_TOAST = "執行結果尚未確認，請查看佇列後再試"


def test_queue_run_now_pending_timeout_controlled(page, login):
    """連線仍在、後端卻一直沒回覆：25 秒後只解 UI 鎖並提示，不重送、不動佇列與草稿；舊 timer 不能清新的操作。

    受控 WS＋Playwright 假時鐘（fast_forward，不真等、不縮短產品 timeout）；不是 provider E2E。
    """
    OUT.mkdir(parents=True, exist_ok=True)
    root, mount, sync, wait_connection, routes, frames, queue = _controlled_env(page, login, clock=True)
    buttons = root.get_by_role("button", name="立刻執行", exact=True)
    toast = page.get_by_text(TIMEOUT_TOAST, exact=True)
    runs = lambda: [f for f in frames if f["body"].get("type") == "queue_run_now"]
    mount()
    draft = root.locator("textarea[data-chat-input]")
    draft.fill("逾時草稿")

    # 1) 24 秒仍鎖住；25 秒後解鎖並提示；沒有自動重送、佇列與草稿都在
    buttons.nth(1).click()
    expect(buttons.nth(1)).to_be_disabled()
    page.clock.fast_forward(24000)
    expect(buttons.nth(1)).to_be_disabled()
    expect(toast).to_have_count(0)
    page.clock.fast_forward(1500)
    expect(buttons.nth(1)).to_be_enabled()
    expect(toast).to_be_visible()
    assert len(runs()) == 1
    expect(buttons).to_have_count(2)
    expect(draft).to_have_value("逾時草稿")

    # 2) 後端 error 先清掉第一筆；再次點擊後，第一筆的舊 timer 到期不得解開新的操作
    buttons.nth(1).click()
    page.clock.fast_forward(10000)
    routes[-1].send(json.dumps({"type": "error", "content": "受控後端拒絕"}))
    expect(buttons.nth(1)).to_be_enabled()
    buttons.nth(1).click()
    assert len(runs()) == 3
    page.clock.fast_forward(16000)  # 距第二筆 26 秒（舊 timer 會在此時到期），距第三筆 16 秒
    expect(buttons.nth(1)).to_be_disabled()
    page.clock.fast_forward(10000)
    expect(buttons.nth(1)).to_be_enabled()
    assert len(runs()) == 3

    # 3) 斷線後重連、再點：斷線前那筆的 timer 已清除
    buttons.nth(1).click()
    expect(buttons.nth(1)).to_be_disabled()
    before_close = len(routes)
    routes[-1].close(code=1000, reason="fixture disconnect")
    expect(buttons.nth(1)).to_be_enabled()
    wait_connection(before_close)
    page.clock.fast_forward(1000)
    sync()
    expect(buttons.nth(1)).to_be_enabled()
    page.clock.fast_forward(20000)
    buttons.nth(1).click()
    expect(buttons.nth(1)).to_be_disabled()
    page.clock.fast_forward(6000)  # 距斷線前那筆 27 秒；新的一筆才 6 秒
    expect(buttons.nth(1)).to_be_disabled()
    page.clock.fast_forward(20000)
    expect(buttons.nth(1)).to_be_enabled()

    # 4) 換 session：舊 session 的 timer 不得解開新 session 的操作
    mount(sid="run-now-fixture-other")
    buttons.nth(1).click()
    expect(buttons.nth(1)).to_be_disabled()
    page.clock.fast_forward(12000)
    mount(sid="run-now-fixture-third")
    buttons.nth(1).click()
    expect(buttons.nth(1)).to_be_disabled()
    page.clock.fast_forward(14000)  # 第一個 session 的 timer 此時才到期（若沒清）
    expect(buttons.nth(1)).to_be_disabled()
    page.clock.fast_forward(12000)
    expect(buttons.nth(1)).to_be_enabled()

    # 5) 成功（queue_update 不再含該 id）清 timer：之後不會出現逾時提示
    buttons.nth(1).click()
    routes[-1].send(json.dumps({"type": "queue_update", "queue": queue[:1]}))
    expect(buttons).to_have_count(1)
    page.clock.fast_forward(40000)
    expect(toast).to_have_count(0)
    page.evaluate("queueFixtureRoot.unmount()")
    (OUT / "pending-timeout.json").write_text(json.dumps({"kind": "controlled-websocket-dom-fake-clock", "frames": frames}, ensure_ascii=False, indent=2), encoding="utf-8")
