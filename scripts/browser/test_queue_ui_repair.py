"""退修真行為：專用 Room queue、正 ID 編輯與批次。"""
import json
import os
import re
import time
from pathlib import Path
import pytest
from playwright.sync_api import expect

OUT = Path(os.environ.get('QUEUE_REPAIR_ARTIFACTS', 'test-results/queue-repair'))


@pytest.fixture
def browser_context_args(browser_context_args):
    return {**browser_context_args, 'service_workers': 'block'}


def test_private_batch_ack_timeout_disconnect_and_scope(page, login):
    from test_private_queue_edit import _controlled_env
    root, mount, sync, wait, routes, frames, queue = _controlled_env(page, login, clock=True)
    mount()
    batch = root.get_by_role('button', name=re.compile('^(一起送|安排中…)'))
    draft = root.locator('textarea[data-chat-input]')
    draft.fill('保留的草稿')
    batch.evaluate('b => { b.click(); b.click(); }')
    expect(batch).to_be_disabled()
    requests = lambda: [f['body'] for f in frames if f['body']['type'].startswith('queue_batch')]
    assert len(requests()) == 1
    rid = requests()[0]['request_id']
    routes[-1].send(json.dumps({'type':'queue_batch_success','request_id':'wrong','batch_id':'wrong'}))
    expect(batch).to_be_disabled()
    routes[-1].send(json.dumps({'type':'queue_batch_error','request_id':rid,'content':'精確拒絕內容'}))
    expect(page.get_by_text('精確拒絕內容', exact=True)).to_be_visible()
    expect(batch).to_be_enabled()
    batch.click()
    page.clock.run_for(25001)
    expect(page.get_by_text('25秒未確認批次結果', exact=False)).to_be_visible()
    expect(batch).to_be_enabled()
    expect(draft).to_have_value('保留的草稿')
    assert len(requests()) == 2
    batch.click()
    before = len(routes)
    routes[-1].close(code=1000, reason='controlled disconnect')
    page.clock.run_for(1000)
    wait(before)
    sync()
    expect(batch).to_be_enabled()
    assert len(requests()) == 3
    batch.click()
    stale = requests()[-1]['request_id']
    mount(sid='edit-fixture-other')
    expect(batch).to_be_enabled()
    routes[-1].send(json.dumps({'type':'queue_batch_success','request_id':stale,'batch_id':'stale'}))
    expect(batch).to_be_enabled()
    batch.click()
    request = requests()[-1]
    for q in queue:
        q['batch_id'] = 'batch-restored'
    sync()
    routes[-1].send(json.dumps({'type':'queue_batch_success','request_id':request['request_id'],'batch_id':'batch-restored'}))
    cancel = root.get_by_role('button', name='取消批次', exact=True)
    expect(cancel).to_be_enabled()
    expect(root.get_by_role('button', name='編輯', exact=True).first).to_be_disabled()
    cancel.evaluate('b=>{b.click();b.click()}')
    expect(cancel).to_be_disabled()
    cancel_request = requests()[-1]
    assert cancel_request['type'] == 'queue_batch_cancel'
    routes[-1].send(json.dumps({'type':'queue_batch_success','request_id':cancel_request['request_id'],'batch_id':'batch-restored'}))
    expect(page.get_by_text('已取消批次', exact=True)).to_be_visible()
    for q in queue:
        q.pop('batch_id')
    sync()
    remove = root.get_by_role('button', name='移除這則排隊訊息').first
    remove.evaluate('b=>{b.click();b.click()}')
    expect(batch).to_be_disabled()
    assert len([f for f in frames if f['body']['type'] == 'queue_remove']) == 1
    routes[-1].send(json.dumps({'type':'error','content':'移除拒絕'}))
    expect(page.get_by_text('移除拒絕', exact=True)).to_be_visible()
    expect(batch).to_be_enabled()
    page.evaluate('queueFixtureRoot.unmount()')


def test_room_queue_pagination_and_late_scope(page, login):
    login()
    held = []
    calls = []
    def respond(route):
        from urllib.parse import urlparse, parse_qs
        q = parse_qs(urlparse(route.request.url).query)
        pm, after = q['expected_pm_id'][0], int(q['after_id'][0])
        calls.append((pm, after))
        if pm == 'old':
            held.append(route)
            return
        rows = [{'id': i, 'content': str(i)} for i in (range(1, 101) if after == 0 else range(101, 106))]
        route.fulfill(status=200, json={'pm_id':pm, 'queue':rows, 'has_more':after == 0, 'next_after_id':rows[-1]['id']})
    page.route('**/rooms/queue-scope/queue?*', respond)
    page.evaluate("""() => {
      const host=document.createElement('div'); host.id='scope-probe';document.body.append(host);
      window.scopeProbeRoot=ReactDOM.createRoot(host);
      function Probe(){const [pm,setPM]=React.useState('old');window.changeProbePM=setPM;
        const state=useRoomQueue('queue-scope',pm);return React.createElement('pre',null,JSON.stringify(state));}
      scopeProbeRoot.render(React.createElement(Probe));
    }""")
    expect(page.locator('#scope-probe')).to_contain_text('"ready":false')
    page.wait_for_function('typeof changeProbePM === "function"')
    page.evaluate('changeProbePM("new")')
    expect(page.locator('#scope-probe')).to_contain_text('"id":105')
    assert calls[:3] == [('old', 0), ('new', 0), ('new', 100)]
    assert len(held) == 1
    held[0].fulfill(status=200, json={'pm_id':'old','queue':[{'id':999,'content':'舊回應'}],'has_more':False})
    expect(page.locator('#scope-probe')).not_to_contain_text('舊回應')
    data = json.loads(page.locator('#scope-probe').inner_text())
    assert len(data['rows']) == 105 and data['ready']
    page.evaluate('scopeProbeRoot.unmount()')


@pytest.mark.parametrize('kind', ['private', 'room'])
def test_real_batch_waits_for_turn_and_excludes_later(page, login, kind):
    """真後端＋既有45秒fake marker；不可用受控ACK冒充runner驗證。"""
    import uuid
    login()
    name = '批次-' + uuid.uuid4().hex[:6]
    ids = page.evaluate("""async ({name,kind}) => {
      const post=async(url,body)=>{const r=await apiFetch(url,{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(body)});if(!r.ok)throw Error(await r.text());return r.json()};
      const pm=await post('/sessions',{name,agent_type:'claude',permission_mode:'default'});
      const room=kind==='room'?await post('/rooms',{name,members:[{session_id:pm.id,role:'pm'}]}):null;
      return {pm:pm.id,room:room?.id};
    }""", {'name':name,'kind':kind})
    page.reload()
    if kind == 'room':
        from test_workroom_polish import _open
        screen = _open(page, page.locator('.room-list-card').filter(has_text=name), name)
        draft = screen.locator('textarea[name=text]')
        send = lambda: screen.get_by_role('button', name='傳給 PM', exact=True).click()
    else:
        page.locator('.ra-session-card').filter(has_text=name).click()
        screen = page
        draft = page.locator('textarea[data-chat-input]')
        send = lambda: page.get_by_role('button', name=re.compile('^(送出|排入佇列)$')).click()
    draft.fill('QUEUE_RUN_NOW_UI_A')
    send()
    expect(screen.get_by_text('queue-run-now partial A', exact=False)).to_be_visible(timeout=15000)
    for text in ('快照甲', '快照乙'):
        draft.fill(text)
        send()
        expect(draft).to_have_value('')
    queue = screen.get_by_label('排隊中的訊息', exact=True)
    expect(queue.locator('li')).to_have_count(2)
    queue.get_by_role('button', name='一起送', exact=True).click()
    expect(queue.get_by_role('button', name='取消批次', exact=True)).to_be_visible()
    # 重整/取消與正常批次共用同一 setup，兩種 scope 均實測。
    page.reload()
    if kind == 'room':
        screen = _open(page, page.locator('.room-list-card').filter(has_text=name), name)
        draft = screen.locator('textarea[name=text]')
    queue = screen.get_by_label('排隊中的訊息', exact=True)
    cancel = queue.get_by_role('button', name='取消批次', exact=True)
    expect(cancel).to_be_visible()
    edit_buttons = queue.get_by_role('button', name='編輯排隊訊息' if kind == 'room' else '編輯', exact=True)
    expect(edit_buttons.first).to_be_disabled()
    cancel.click()
    expect(cancel).to_have_count(0)
    expect(edit_buttons.first).to_be_enabled()
    expect(queue.locator('li')).to_have_count(2)
    queue.get_by_role('button', name='一起送', exact=True).click()
    expect(cancel).to_be_visible()
    draft.fill('後來的輸入')
    send()
    expect(draft).to_have_value('')
    expect(queue.locator('li')).to_have_count(3)
    # A 未被中斷，批次仍完整排隊；等其正常結束才一起promotion。
    history = lambda: page.evaluate("async id=>(await apiFetch('/sessions/'+id+'/messages')).json()", ids['pm'])
    before = history()
    assert [m['content'] for m in before if m['role']=='user'] == ['QUEUE_RUN_NOW_UI_A']
    deadline = time.monotonic() + 60
    while not any(m['role'] == 'user' and m['content'] == '快照乙' for m in history()):
        assert time.monotonic() < deadline, history()
        page.wait_for_timeout(250)
    after = history()
    assert [m['content'] for m in after if m['role']=='user'] == ['QUEUE_RUN_NOW_UI_A','快照甲','快照乙']
    assert len([m for m in after if m['role']=='claude']) == 2, after
    expect(queue).to_contain_text('後來的輸入')
    expect(queue).not_to_contain_text('快照甲', timeout=15000)
    expect(queue).not_to_contain_text('快照乙')
    if kind == 'room':
        history_dom = screen.locator('.room-timeline')
        expect(history_dom).to_have_count(1)
        expect(history_dom.locator('article[data-side="user"]').filter(has_text='快照甲')).to_have_count(1)
        expect(history_dom.locator('article[data-side="user"]').filter(has_text='快照乙')).to_have_count(1)
        expect(history_dom.locator('[data-queued="true"]')).to_have_count(0)
    OUT.mkdir(parents=True, exist_ok=True)
    (OUT / f'{kind}-real-batch.json').write_text(json.dumps({'kind':'real-loopback-fake-cli','before':before,'after':after}, ensure_ascii=False, indent=2), encoding='utf-8')
    page.screenshot(path=str(OUT / f'{kind}-real-batch.png'))
    # 等後來一筆也正常完成，證明仍是另一輪，再刪自己的暫存資料。
    deadline = time.monotonic() + 45
    while True:
        rows = history()
        idle = page.evaluate("async id=>(await(await apiFetch('/sessions')).json()).find(s=>s.id===id)?.status==='idle'", ids['pm'])
        if any(m['role'] == 'user' and m['content'] == '後來的輸入' for m in rows) and idle:
            break
        assert time.monotonic() < deadline, rows
        page.wait_for_timeout(250)
    final = history()
    assert len([m for m in final if m['role']=='claude']) == 3
    page.evaluate("""async ids => {
      if(ids.room)await apiFetch('/rooms/'+ids.room,{method:'DELETE'});
      await apiFetch('/sessions/'+ids.pm,{method:'DELETE'});
    }""", ids)
