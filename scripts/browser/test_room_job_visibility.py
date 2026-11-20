"""真實 DOM：正式派工已結束時，仍如實呈現未綁定的 worker 活動。"""
import os
from pathlib import Path
import re
import uuid

import pytest
from playwright.sync_api import expect
from test_workroom_polish import _room, _open


@pytest.fixture
def browser_context_args(browser_context_args):
    # 網路 mock 必須直接攔截頁面請求，不能由 Service Worker 代發。
    return {**browser_context_args, 'service_workers': 'block'}


def test_unbound_activity_refresh_and_board_failure(page, login):
    name = '活動可見-' + uuid.uuid4().hex[:6]
    page.set_viewport_size({'width': 1280, 'height': 800})
    card = _room(page, login, name)
    worker = {'session_id': 'visibility-worker', 'session_name': name + ' worker',
              'role': 'worker', 'status': 'running', 'attention': '', 'queue_length': 0}
    board = {'memo': '', 'jobs': [{'id': number, 'worker_id': worker['session_id'],
                       'worker_name': '派工時名稱-' + str(number), 'status': status,
                       'title': '當次工作-' + str(number), 'task_text': '<b>原任務-' + str(number) + '</b>\n保留換行',
                       'result_text': '已保存結果-' + str(number), 'result_saved': True, 'worker_exists': True}
                      for number, status in ((31, 'done'), (30, 'failed'), (29, 'interrupted'))]}
    failed = False

    def respond(route):
        if failed:
            route.fulfill(status=503, json={'error': '測試看板暫時無法更新'})
        else:
            route.fulfill(json=board)

    def detail(route):
        response = route.fetch()
        data = response.json()
        for member in data['members']:
            if member['role'] == 'worker':
                member['status'] = worker['status']
                worker['session_id'] = member['session_id']
                worker['session_name'] = member['session_name']
                for job in board['jobs']:
                    job['worker_id'] = member['session_id']
            else:
                member['status'] = 'running'
        route.fulfill(response=response, json=data)

    page.route(re.compile(r'/rooms/[^/?]+$'), detail)
    page.route('**/rooms/*/board*', respond)
    screen = _open(page, card, name)
    screen.locator('.room-board-toggle').click()
    panel = screen.locator('#room-work-board')
    expect(panel).to_be_visible()
    out = Path(os.environ.get('ROOM_VISIBILITY_ARTIFACTS', 'test-results/room-job-visibility'))
    out.mkdir(parents=True, exist_ok=True)
    panel.get_by_role('button', name='派工', exact=True).click()
    expect(panel.get_by_role('heading', name='進行中與需要處理', exact=True)).to_be_visible()
    page.screenshot(path=str(out / 'running.png'))
    expect(panel.get_by_text('完成紀錄 3', exact=True)).to_be_visible()
    activity = panel.get_by_role('button').filter(has_text='未綁定派工')
    expect(activity).to_have_count(1)
    expect(activity).to_contain_text(worker['session_name'])
    expect(activity).to_contain_text('會話活動')
    expect(activity).to_contain_text('執行中，活動資料未提供')
    expect(panel.get_by_text('沒有進行中的派工', exact=True)).to_have_count(0)

    worker['status'] = 'idle'
    expect(activity).to_have_count(0, timeout=10000)
    expect(panel.get_by_text('沒有進行中的派工', exact=True)).to_be_visible()
    panel.locator('summary').filter(has_text='完成紀錄').click()
    for number, status in ((31, '已完成'), (30, '失敗'), (29, '已中斷')):
        record = panel.locator(f'.room-job-record[data-job-id="{number}"]')
        expect(record.locator('summary')).to_contain_text(f'#{number} · 當次工作-{number}')
        expect(record.locator('summary')).to_contain_text(status)
    record = panel.locator('.room-job-record[data-job-id="31"]')
    record.locator('summary').focus()
    page.keyboard.press('Enter')
    expect(record.get_by_text('<b>原任務-31</b>\n保留換行', exact=True)).to_be_visible()
    expect(record.get_by_text('已保存結果-31', exact=True)).to_be_visible()
    expect(record.locator('b')).to_have_count(0)
    expect(record.get_by_role('button', name='開啟執行者會話')).to_be_visible()
    # 改名與刪除的是此測試建立的 session；mock 只供 DOM 狀態，DB 持久化另有 Go 整合測試。
    page.evaluate("""async id => {
      const res = await apiFetch('/sessions/' + id, {method:'PATCH', headers:{'Content-Type':'application/json'}, body:JSON.stringify({name:'改名後的會話'})});
      if (!res.ok) throw new Error(await res.text());
    }""", worker['session_id'])
    expect(record.locator('summary')).to_contain_text('派工時名稱-31')
    page.evaluate("""async id => {
      const res = await apiFetch('/sessions/' + id, {method:'DELETE'});
      if (!res.ok) throw new Error(await res.text());
    }""", worker['session_id'])
    for job in board['jobs']:
        job['worker_exists'] = False
    expect(record.get_by_role('button', name='開啟執行者會話')).to_have_count(0, timeout=10000)
    expect(record.get_by_text('已保存結果-31', exact=True)).to_be_visible()
    expect(record.get_by_text('執行者會話已不存在，以上為已保存工作紀錄。', exact=True)).to_be_visible()
    legacy = board['jobs'][2]
    legacy.update(title='', task_text='', result_text='', result_saved=False)
    legacy_record = panel.locator('.room-job-record[data-job-id="29"]')
    expect(legacy_record.locator('summary')).to_contain_text('舊任務（標題未保存）', timeout=10000)
    legacy_record.locator('summary').click()
    expect(legacy_record.get_by_text('舊任務內容未保存', exact=True)).to_be_visible()
    expect(legacy_record.get_by_text('結果未保存', exact=True)).to_be_visible()
    page.screenshot(path=str(out / 'idle-closed-history.png'))

    failed = True
    expect(panel.get_by_role('alert')).to_contain_text('測試看板暫時無法更新', timeout=10000)
    expect(panel.get_by_text('沒有進行中的派工', exact=True)).to_have_count(0)
    page.screenshot(path=str(out / 'board-error.png'))
