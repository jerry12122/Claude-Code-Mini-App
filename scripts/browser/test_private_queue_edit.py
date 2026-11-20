"""普通私人會話佇列文字編輯UI：真 loopback／fake CLI happy path、衝突重載、雙擊防重送，與受控 WS 邊界。"""
import json
import os
import re
from pathlib import Path
import uuid

import pytest
from playwright.sync_api import expect

OUT = Path(os.environ.get("PRIVATE_QUEUE_EDIT_ARTIFACTS", r"D:\Temp\miniapp-queue-edit-20261119\private-ui-artifacts"))


def _controlled_env(page, login, clock=False):
    """受控 WS 資料；使用真正 ChatView 與 useChatSocket，不代表真後端狀態／權限 E2E。"""
    routes, frames = [], []
    current = {"value": "STREAMING", "lingering": False}
    queue = [
        {"id": 701, "content": "受控排隊 B"},
        {"id": 702, "content": "受控排隊 C"},
    ]

    def connect(ws):
        routes.append(ws)
        ws.on_message(lambda text: frames.append({"url": ws.url, "body": json.loads(text)}))

    page.context.route("**/guest/me*", lambda r: r.fulfill(status=200, json={"role": "editor", "mode": "live"}))
    page.context.route("**/messages*", lambda r: r.fulfill(status=200, json=[]))
    page.route_web_socket(re.compile(r"/sessions/edit-fixture-[^/]+/ws"), connect)
    login()
    if clock:
        page.clock.install()
    page.evaluate("""() => {
      document.getElementById('root').hidden=true;
      const host=document.createElement('div'); host.id='queue-ui-fixture'; host.style='position:fixed;inset:0;background:var(--ra-bg);z-index:100'; document.body.append(host);
      window.queueFixtureRoot=ReactDOM.createRoot(host);
      window.mountQueueFixture=p=>queueFixtureRoot.render(React.createElement(ChatView,{session:p.session,guest:p.guest||null,onBack:()=>{}}));
    }""")
    root = page.locator("#queue-ui-fixture")
    counter = 0

    def wait_connection(before):
        for _ in range(200):
            if len(routes) > before:
                return
            page.wait_for_timeout(50)
        raise AssertionError("hook 沒有建立 fixture WebSocket")

    def mount(agent="claude", value="STREAMING", lingering=False, guest=None, sid=None, custom_queue=None):
        nonlocal counter
        counter += 1
        current.update(value=value, lingering=lingering)
        sid = sid or f"edit-fixture-{counter}"
        before = len(routes)
        q = queue if custom_queue is None else custom_queue
        page.evaluate("p=>mountQueueFixture(p)", {
            "session": {"id": sid, "name": "受控佇列編輯", "agent_type": agent, "permission_mode": "default", "input_mode": "agent"},
            "guest": guest,
        })
        wait_connection(before)
        routes[-1].send(json.dumps({"type": "sync", "messages": [], "queue": q, **current}))
        readonly = agent == "kiro" or (guest and guest["role"] == "viewer")
        expect(root.locator(".ra-chat-composer")).to_have_count(0 if readonly else 1)
        if not readonly and q:
            expect(root.get_by_label("排隊中的訊息", exact=True).locator("li")).to_have_count(len(q))
        page.wait_for_function("document.body.textContent.includes('受控佇列編輯')")
        assert len(routes) > before
        return sid

    def sync(custom_queue=None):
        q = queue if custom_queue is None else custom_queue
        routes[-1].send(json.dumps({"type": "sync", "messages": [], "queue": q, **current}))

    return root, mount, sync, wait_connection, routes, frames, queue


def test_private_queue_edit_dom(page, login):
    """真 loopback／fake CLI happy path：佇列編輯按鈕、16px 原生表單、44px、取消、儲存與收斂。"""
    OUT.mkdir(parents=True, exist_ok=True)
    login()
    name = "private-queue-edit-" + uuid.uuid4().hex[:8]
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
    page.reload()
    expect(draft).to_be_enabled()

    try:
        # A 阻塞中
        draft.fill("QUEUE_RUN_NOW_UI_A")
        page.get_by_role("button", name="送出", exact=True).click()
        expect(page.get_by_text("queue-run-now partial A", exact=False)).to_be_visible(timeout=15000)

        # 排入 B 與 C
        for text in ["排隊 B 原文", "排隊 C 原文"]:
            draft.fill(text)
            page.get_by_role("button", name="排入佇列", exact=True).click()
            expect(draft).to_have_value("")

        queue = page.get_by_label("排隊中的訊息", exact=True)
        expect(queue.locator("li")).to_have_count(2)

        # RED 斷言：原始真 DOM 缺少「編輯」按鈕
        edit_buttons = queue.get_by_role("button", name="編輯", exact=True)
        expect(edit_buttons).to_have_count(2)

        # 響應式檢查 (1280x800, 390x844 與 320x640 不溢寬，按鈕觸控區 24~28px，整列高 24~28px)
        for width, height in [(1280, 800), (390, 844), (320, 640)]:
            page.set_viewport_size({"width": width, "height": height})
            assert page.evaluate("document.documentElement.scrollWidth <= innerWidth")
            for btn in edit_buttons.all():
                box = btn.bounding_box()
                assert 24 <= box["width"] <= 28 and 24 <= box["height"] <= 28, box
                assert btn.evaluate("el => el.querySelector('svg') !== null && el.innerText.trim() === ''")
            for row in queue.locator("li").all():
                rbox = row.bounding_box()
                assert 24 <= rbox["height"] <= 28, rbox
            page.screenshot(path=str(OUT / f"queue-edit-{width}.png"))

        page.set_viewport_size({"width": 1280, "height": 800})

        # 1. 測試取消：開啟編輯器，輸入草稿，按取消，原文不變
        edit_buttons.nth(0).click()
        editor = queue.locator("form")
        expect(editor).to_be_visible()
        textarea = editor.locator("textarea[name='content']")
        expect(textarea).to_have_value("排隊 B 原文")

        # 檢查 textarea font-size 至少 16px 防止 iOS/mobile 縮放
        font_size = page.evaluate("el => window.getComputedStyle(el).fontSize", textarea.element_handle())
        assert float(font_size.replace("px", "")) >= 16, f"Font size must be >= 16px, got {font_size}"

        textarea.fill("排隊 B 暫存修改草稿")
        cancel_btn = editor.get_by_role("button", name="取消", exact=True)
        cancel_btn.click()
        expect(editor).to_have_count(0)
        expect(queue.locator("li").nth(0)).to_contain_text("排隊 B 原文")

        # 2. 測試成功修改：開啟編輯器，修改並儲存
        edit_buttons = queue.get_by_role("button", name="編輯", exact=True)
        edit_buttons.nth(0).click()
        editor = queue.locator("form")
        textarea = editor.locator("textarea[name='content']")
        textarea.fill("排隊 B 修改後全新文字")
        save_btn = editor.get_by_role("button", name="儲存修改", exact=True)
        save_btn.click()

        # 等待儲存完成，編輯器收合，佇列顯示新字
        expect(editor).to_have_count(0, timeout=10000)
        expect(queue.locator("li").nth(0)).to_contain_text("排隊 B 修改後全新文字")

        # 驗證 WS 送出的 frame 格式
        edit_frames = [f for f in frames if f.get("type") == "queue_edit"]
        assert len(edit_frames) == 1
        assert edit_frames[0]["previous"] == "排隊 B 原文"
        assert edit_frames[0]["content"] == "排隊 B 修改後全新文字"
        assert edit_frames[0]["request_id"].startswith("qedit-")

        page.screenshot(path=str(OUT / "queue-edit-success.png"))

    finally:
        # 清理 session
        page.evaluate("""async id => {
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
          await apiFetch('/sessions/'+id,{method:'DELETE'});
        }""", sid)


def test_private_queue_edit_sync_lock_and_conflict(page, login):
    """同 task 兩次 submit 只送一筆 queue_edit；後端衝突保留草稿，重新載入更新 previous 後再存成功。"""
    OUT.mkdir(parents=True, exist_ok=True)
    login()
    name = "private-queue-lock-" + uuid.uuid4().hex[:8]
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
    page.reload()
    expect(draft).to_be_enabled()

    try:
        draft.fill("QUEUE_RUN_NOW_UI_A")
        page.get_by_role("button", name="送出", exact=True).click()
        expect(page.get_by_text("queue-run-now partial A", exact=False)).to_be_visible(timeout=15000)

        draft.fill("待測試防重複與衝突項目")
        page.get_by_role("button", name="排入佇列", exact=True).click()
        expect(draft).to_have_value("")

        queue = page.get_by_label("排隊中的訊息", exact=True)
        expect(queue.locator("li")).to_have_count(1)
        queue.get_by_role("button", name="編輯", exact=True).click()

        editor = queue.locator("form")
        textarea = editor.locator("textarea[name='content']")
        textarea.fill("我的草稿新文字")

        # 1. 驗證同步防重複鎖：同一個事件回圈呼叫兩次 requestSubmit
        page.evaluate("""() => {
          const form = document.querySelector("div[aria-label='排隊中的訊息'] form");
          form.requestSubmit();
          form.requestSubmit();
        }""")

        expect(editor).to_have_count(0, timeout=10000)
        expect(queue.locator("li").nth(0)).to_contain_text("我的草稿新文字")

        # 斷言只送出一則 queue_edit
        edit_frames = [f for f in frames if f.get("type") == "queue_edit"]
        assert len(edit_frames) == 1, f"Expected exactly 1 queue_edit, got {len(edit_frames)}"

        # 2. 測試衝突 (Conflict) 與重新載入 (Reload)：
        # 先開啟編輯器，在 client 端的 previous 記錄為「我的草稿新文字」
        queue.get_by_role("button", name="編輯", exact=True).click()
        textarea = queue.locator("form textarea[name='content']")
        textarea.fill("使用者想要更新的第二版文字")

        # 透過另一個獨立 WS 連線模擬別台裝置將該項目修改為「別台裝置搶先修改」
        qid = page.evaluate("() => Number(document.querySelector('form[data-queue-edit]').getAttribute('data-queue-edit'))")

        page.evaluate("""({id, qid}) => new Promise((resolve, reject) => {
          const ws = new WebSocket(wsURL(id));
          ws.onerror = () => reject(new Error('concurrent ws failed'));
          ws.onmessage = e => {
            const m = JSON.parse(e.data);
            if (m.type === 'sync') {
              ws.send(JSON.stringify({
                type: 'queue_edit',
                id: qid,
                request_id: 'qedit-concurrent-' + Date.now(),
                previous: '我的草稿新文字',
                content: '別台裝置搶先修改'
              }));
            }
            if (m.type === 'queue_edit_success') {
              ws.close();
              resolve();
            }
            if (m.type === 'queue_edit_error') {
              ws.close();
              reject(new Error(m.content || 'edit error'));
            }
          };
        })""", {"id": sid, "qid": qid})

        # 回到畫面按「儲存修改」，此時 client 端仍用舊 previous 送出，後端應回報衝突
        save_btn = queue.locator("form").get_by_role("button", name="儲存修改", exact=True)
        save_btn.click()

        # 衝突發生：編輯器依然開啟，草稿「使用者想要更新的第二版文字」保留，顯示衝突錯誤與「重新載入」按鈕
        expect(queue.locator("form")).to_be_visible()
        expect(textarea).to_have_value("使用者想要更新的第二版文字")
        expect(queue.locator("form [role='alert']")).to_contain_text("衝突")
        reload_btn = queue.locator("form").get_by_role("button", name="重新載入", exact=True)
        expect(reload_btn).to_be_visible()

        # 按下「重新載入」：讀取最新佇列內容作為新的 previous，錯誤清除，使用者草稿依然保留！
        reload_btn.click()
        expect(queue.locator("form [role='alert']")).to_have_count(0)
        expect(textarea).to_have_value("使用者想要更新的第二版文字")

        # 再次儲存：成功！
        save_btn.click()
        expect(queue.locator("form")).to_have_count(0, timeout=10000)
        expect(queue.locator("li").nth(0)).to_contain_text("使用者想要更新的第二版文字")

    finally:
        page.evaluate("""async id => {
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
          await apiFetch('/sessions/'+id,{method:'DELETE'});
        }""", sid)


def test_private_queue_edit_dequeued_retention(page, login):
    """編輯中項目被取走（例如被 RunNow 或移除）：編輯表單維持可見，保留草稿並提示已出列，可取消。"""
    OUT.mkdir(parents=True, exist_ok=True)
    login()
    name = "private-queue-dequeue-" + uuid.uuid4().hex[:8]
    page.get_by_role("button", name="建立新 Session", exact=True).click()
    page.get_by_placeholder("Session 名稱（選填，留空自動產生）").fill(name)
    page.get_by_role("button", name="建立", exact=True).click()
    draft = page.locator("textarea[data-chat-input]")
    expect(draft).to_be_enabled()
    sid = page.evaluate("""async name => (await (await apiFetch('/sessions')).json()).find(s => s.name === name).id""", name)

    page.reload()
    expect(draft).to_be_enabled()

    try:
        draft.fill("QUEUE_RUN_NOW_UI_A")
        page.get_by_role("button", name="送出", exact=True).click()
        expect(page.get_by_text("queue-run-now partial A", exact=False)).to_be_visible(timeout=15000)

        draft.fill("即將被移除的排隊項目")
        page.get_by_role("button", name="排入佇列", exact=True).click()
        expect(draft).to_have_value("")

        queue = page.get_by_label("排隊中的訊息", exact=True)
        expect(queue.locator("li")).to_have_count(1)

        # 進入編輯
        queue.get_by_role("button", name="編輯", exact=True).click()
        editor = queue.locator("form")
        textarea = editor.locator("textarea[name='content']")
        textarea.fill("正在編輯中的珍貴草稿")

        # 此時透過外部 API / WS 將該項目移除（模擬出列或在別台被刪除）
        qid = page.evaluate("() => Number(document.querySelector('form[data-queue-edit]').getAttribute('data-queue-edit'))")

        page.evaluate("""({id, qid}) => new Promise((resolve, reject) => {
          const ws = new WebSocket(wsURL(id));
          ws.onerror = () => reject(new Error('cleanup ws failed'));
          ws.onmessage = e => {
            const m = JSON.parse(e.data);
            if (m.type === 'sync') ws.send(JSON.stringify({type: 'queue_remove', id: qid}));
            if (m.type === 'queue_update' && !(m.queue || []).some(x => x.id === qid)) {
              ws.close();
              resolve();
            }
          };
        })""", {"id": sid, "qid": qid})

        # 驗證：雖然佇列清空了，但編輯器依然存在！草稿依然保留，且顯示出列提示與取消按鈕
        expect(editor).to_be_visible()
        expect(textarea).to_have_value("正在編輯中的珍貴草稿")
        expect(editor).to_contain_text("已出列或已移除")

        # 按取消按鈕收合
        editor.get_by_role("button", name="取消", exact=True).click()
        expect(queue).to_have_count(0)

    finally:
        page.evaluate("""async id => {
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
          await apiFetch('/sessions/'+id,{method:'DELETE'});
        }""", sid)


def test_private_queue_edit_controlled_dom(page, login):
    """受控 WS DOM：guest/內部非人類來源排除、無附件空白拒絕、斷線錯誤保護、切 session 不殘留。"""
    OUT.mkdir(parents=True, exist_ok=True)
    root, mount, sync, wait_connection, routes, frames, queue = _controlled_env(page, login)

    # 1. 訪客權限排除檢查：viewer 與 editor 訪客皆不得看到「編輯」按鈕
    mount(guest={"role": "viewer", "mode": "live", "nickname": "viewer"})
    expect(root.get_by_role("button", name="編輯", exact=True)).to_have_count(0)

    mount(guest={"role": "editor", "mode": "live", "nickname": "editor"})
    expect(root.get_by_role("button", name="編輯", exact=True)).to_have_count(0)

    # 2. 非人類來源排除檢查：author 或 source_session_id 有值時不得編輯
    special_queue = [
        {"id": 801, "content": "人類擁有者排隊", "author": ""},
        {"id": 802, "content": "來自 room 的排隊", "author": "room-user-1"},
        {"id": 803, "content": "來自子 session 的排隊", "source_session_id": "sess-sub-1"},
    ]
    mount(custom_queue=special_queue)
    edit_buttons = root.get_by_role("button", name="編輯", exact=True)
    expect(edit_buttons).to_have_count(1)  # 只有 801 可編輯

    # 3. 驗證無附件時不可送出空白內容（由前端原生阻擋，不發出 WS 訊息）
    edit_buttons.first.click()
    form = root.locator("form")
    textarea = form.locator("textarea[name='content']")
    textarea.fill("   ")  # 純空白
    form.get_by_role("button", name="儲存修改", exact=True).click()
    expect(form.locator("[role='alert']")).to_contain_text("不得為空")
    assert not [f for f in frames if f["body"].get("type") == "queue_edit"]

    # 4. 驗證有附件時允許空白內容
    attach_queue = [
        {"id": 901, "content": "有附件的排隊", "attachment_ids": ["att-1"]},
    ]
    mount(custom_queue=attach_queue)
    root.get_by_role("button", name="編輯", exact=True).click()
    form = root.locator("form")
    textarea = form.locator("textarea[name='content']")
    textarea.fill("")
    form.get_by_role("button", name="儲存修改", exact=True).click()
    # 應發送 queue_edit 且 content 為 ""
    assert any(f["body"].get("type") == "queue_edit" and f["body"].get("content") == "" for f in frames)

    # 5. 斷線保護：送出中若 socket 斷線，不永久鎖死，呈現提示並保留草稿
    routes[-1].close(code=1000, reason="fixture disconnect")
    expect(form.locator("[role='alert']")).to_contain_text("連線中斷")
    expect(form.get_by_role("button", name="儲存修改", exact=True)).to_be_enabled()
    expect(textarea).to_have_value("")

    # 6. 切換 session：舊 session 的編輯狀態被清除，不會殘留到新 session
    mount(sid="edit-fixture-switched")
    expect(root.locator("form")).to_have_count(0)
    page.evaluate("queueFixtureRoot.unmount()")


def test_private_queue_edit_pending_timeout_controlled(page, login):
    """受控 WS＋Playwright 假時鐘：25 秒逾時保護，解鎖並提示，不重送、不丟草稿。"""
    OUT.mkdir(parents=True, exist_ok=True)
    root, mount, sync, wait_connection, routes, frames, queue = _controlled_env(page, login, clock=True)
    mount()

    edit_btn = root.get_by_role("button", name="編輯", exact=True).first
    edit_btn.click()
    form = root.locator("form")
    textarea = form.locator("textarea[name='content']")
    textarea.fill("受控逾時測試文字")

    save_btn = form.locator("button[type='submit']")
    save_btn.click()

    # 儲存中...
    expect(save_btn).to_contain_text("儲存中…")
    expect(save_btn).to_be_disabled()

    # 24 秒後：仍在等待
    page.clock.fast_forward(24000)
    expect(save_btn).to_contain_text("儲存中…")
    expect(save_btn).to_be_disabled()

    # 25 秒後：逾時解鎖，顯示錯誤提示，草稿保留
    page.clock.fast_forward(1500)
    expect(save_btn).to_contain_text("儲存修改")
    expect(save_btn).to_be_enabled()
    expect(form.locator("[role='alert']")).to_contain_text("逾時")
    expect(textarea).to_have_value("受控逾時測試文字")

    page.evaluate("queueFixtureRoot.unmount()")
