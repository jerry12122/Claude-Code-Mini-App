"""只 mock session／工作室資料與寫入；驗證原生 dialog 中的本地搜尋。"""
import os
from pathlib import Path
import re

import pytest
from playwright.sync_api import expect


@pytest.fixture
def browser_context_args(browser_context_args):
    return {**browser_context_args, 'service_workers': 'block'}


@pytest.fixture
def data(page):
    sessions = [
        {'id': 'PM-Alpha-123', 'name': '中英 Alpha', 'agent_type': 'claude'},
        {'id': 'worker-BETA-456', 'name': '部署 Beta', 'agent_type': 'codex'},
        {'id': 'telegram-789', 'name': 'Telegram Bot', 'agent_type': 'telegram'},
        {'id': 'shell-abc', 'name': 'Shell console', 'agent_type': 'claude', 'input_mode': 'shell'},
    ] + [{'id': f'candidate-{i}', 'name': f'候選 session {i}', 'agent_type': 'claude'} for i in range(117)]
    for session in sessions:
        session.update(status='idle', work_dir='D:/fixture', permission_mode='default', last_active='2026-11-19 10:00:00')
    sessions[-1]['name'] = '極長' + '名稱' * 80
    room = {'id': 'search-fixture', 'name': '既有工作室', 'member_count': 2, 'members': [
        {'session_id': sessions[0]['id'], 'session_name': sessions[0]['name'], 'role': 'pm'},
        {'session_id': sessions[1]['id'], 'session_name': sessions[1]['name'], 'role': 'worker'},
    ]}
    state = {'sessions': sessions, 'writes': [], 'session_writes': [], 'reads': []}

    def session_route(route):
        if route.request.method != 'GET':
            state['session_writes'].append(route.request.method)
            route.fulfill(status=400, json={'error': '此測試禁止修改 session'})
        else:
            state['reads'].append(route.request.url)
            route.fulfill(json=sessions)

    def room_route(route):
        request = route.request
        if request.method == 'GET':
            state['reads'].append(request.url)
            route.fulfill(json=[room] if request.url.split('?')[0].endswith('/rooms') else room)
        else:
            body = request.post_data_json
            state['writes'].append({'method': request.method, 'body': body})
            route.fulfill(status=201 if request.method == 'POST' else 200, json={**room, **body})

    page.route(re.compile(r'/sessions(?:/[^?]*)?(?:\?.*)?$'), session_route)
    page.route(re.compile(r'/rooms(?:/[^/?]+)?(?:\?.*)?$'), room_route)
    return state


def new_dialog(page, login):
    login()
    page.get_by_role('button', name='新增工作室', exact=True).click()
    dialog = page.get_by_role('dialog', name='新增工作室', exact=True)
    expect(dialog).to_be_visible()
    return dialog


def shot(page, name):
    out = Path(os.environ.get('ROOM_SEARCH_ARTIFACTS', 'test-results/room-session-search'))
    out.mkdir(parents=True, exist_ok=True)
    page.screenshot(path=str(out / (name + '.png')))


def test_create_session_search(page, login, data):
    dialog = new_dialog(page, login)
    expect(dialog.get_by_role('checkbox')).to_have_count(121)
    shot(page, 'before-search')
    search = dialog.get_by_role('searchbox', name='搜尋 session（名稱或 ID）', exact=True)
    expect(search).to_be_visible()  # RED：原始表單沒有搜尋輸入。
    dialog.get_by_label('名稱', exact=True).fill('搜尋建立')
    reads = len(data['reads'])
    for query, name in [(' 部署 ', '部署 Beta'), ('  ALPHA  ', '中英 Alpha'), ('bEtA-456', '部署 Beta')]:
        search.fill(query)
        expect(dialog.get_by_role('checkbox')).to_have_count(1)
        expect(dialog.locator('li')).to_contain_text(name)
    search.fill('   ')
    expect(dialog.get_by_role('checkbox')).to_have_count(121)
    search.fill('沒有這個候選')
    expect(dialog.get_by_text('沒有符合搜尋的 session', exact=True)).to_be_visible()
    expect(dialog.get_by_role('checkbox')).to_have_count(0)
    shot(page, 'no-results')
    search.fill('ALPHA')
    dialog.get_by_role('checkbox').check()
    dialog.get_by_role('combobox', name='中英 Alpha 的角色').select_option('pm')
    search.fill('Beta')
    dialog.get_by_role('checkbox').check()
    expect(dialog.get_by_role('combobox', name='部署 Beta 的角色')).to_have_value('worker')
    expect(dialog.locator('legend')).to_have_text('成員（已選 2）')
    search.press('Enter')
    expect(dialog.get_by_role('button', name='建立', exact=True)).to_be_enabled()
    assert not data['writes']
    for composing, code, blocked in ((True, 13, False), (False, 229, False), (False, 13, True)):
        prevented = search.evaluate('''(el, values) => {
          const event = new KeyboardEvent('keydown', {key:'Enter', bubbles:true, cancelable:true,
            isComposing:values[0], keyCode:values[1]});
          el.dispatchEvent(event);
          return event.defaultPrevented;
        }''', [composing, code])
        assert prevented == blocked
    search.fill('Alpha')
    expect(dialog.get_by_role('checkbox')).to_be_checked()
    expect(dialog.get_by_role('combobox')).to_have_value('pm')
    search.fill('')
    expect(dialog.get_by_role('checkbox')).to_have_count(121)
    expect(dialog.locator('input[type=checkbox]:checked')).to_have_count(2)
    search.fill('Beta')  # PM 被隱藏，提交仍必須包含它。
    assert len(data['reads']) == reads, '搜尋不得觸發 API 讀取'
    shot(page, 'selected-hidden-pm')
    dialog.get_by_role('button', name='建立', exact=True).click()
    expect(dialog).not_to_be_visible()
    assert data['writes'] == [{'method': 'POST', 'body': {'name': '搜尋建立', 'members': [
        {'session_id': 'PM-Alpha-123', 'role': 'pm'}, {'session_id': 'worker-BETA-456', 'role': 'worker'}]}}]
    assert not data['session_writes']


def test_edit_preserves_members_and_pm_restrictions(page, login, data):
    login()
    page.get_by_role('button', name='更多操作：既有工作室', exact=True).click()
    page.get_by_role('menuitem', name='編輯／交接 PM', exact=True).click()
    dialog = page.get_by_role('dialog', name='編輯工作室', exact=True)
    search = dialog.get_by_role('searchbox', name='搜尋 session（名稱或 ID）', exact=True)
    expect(search).to_be_visible()
    search.fill('alpha')
    expect(dialog.get_by_role('checkbox')).to_be_checked()
    expect(dialog.get_by_role('checkbox')).to_be_disabled()
    expect(dialog.get_by_role('combobox')).to_have_value('pm')
    for query, name in [('telegram', 'Telegram Bot'), ('shell', 'Shell console')]:
        search.fill(query)
        dialog.get_by_role('checkbox').check()
        expect(dialog.get_by_role('combobox', name=name + ' 的角色').locator('option[value=pm]')).to_be_disabled()
        dialog.get_by_role('checkbox').uncheck()
    search.fill('Beta')
    expect(dialog.get_by_role('checkbox')).to_be_checked()
    expect(dialog.get_by_role('combobox')).to_have_value('worker')
    search.press('Enter')
    expect(dialog.get_by_role('button', name='儲存', exact=True)).to_be_enabled()
    assert not data['writes']
    dialog.get_by_role('button', name='儲存', exact=True).click()
    expect(dialog).not_to_be_visible()
    assert data['writes'] == [{'method': 'PUT', 'body': {'name': '既有工作室', 'previous_pm_id': 'PM-Alpha-123',
        'members': [{'session_id': 'PM-Alpha-123', 'role': 'pm'}, {'session_id': 'worker-BETA-456', 'role': 'worker'}]}}]
    assert not data['session_writes']


@pytest.mark.parametrize('width', [390, 320])
def test_narrow_search_focus_and_native_close(page, login, data, width):
    page.set_viewport_size({'width': width, 'height': 800})
    dialog = new_dialog(page, login)
    search = dialog.get_by_role('searchbox', name='搜尋 session（名稱或 ID）', exact=True)
    expect(search).to_be_visible()
    border = search.evaluate('el => getComputedStyle(el).borderColor')
    search.focus()
    assert search.evaluate('el => parseFloat(getComputedStyle(el).fontSize)') >= 16
    assert search.evaluate('el => getComputedStyle(el).borderColor') != border
    search.fill('極長')
    expect(dialog.get_by_role('checkbox')).to_have_count(1)
    assert dialog.evaluate('el => el.scrollWidth <= el.clientWidth')
    assert page.evaluate('document.documentElement.scrollWidth <= innerWidth')
    shot(page, 'narrow-' + str(width))
    search.press('Escape')
    if dialog.is_visible():  # 搜尋輸入可能先消耗 Escape 來清除文字。
        search.press('Escape')
    expect(dialog).not_to_be_visible()
    expect(page.get_by_role('button', name='新增工作室', exact=True)).to_be_focused()
    assert not data['writes'] and not data['session_writes']


def test_empty_session_list_is_distinct(page, login, data):
    data['sessions'].clear()
    dialog = new_dialog(page, login)
    search = dialog.get_by_role('searchbox', name='搜尋 session（名稱或 ID）', exact=True)
    search.fill('任意查詢')
    expect(dialog.get_by_text('目前沒有 session 可選', exact=True)).to_be_visible()
    expect(dialog.get_by_text('沒有符合搜尋的 session', exact=True)).to_have_count(0)
    dialog.get_by_role('button', name='取消', exact=True).click()
    expect(dialog).not_to_be_visible()
    assert not data['writes'] and not data['session_writes']
