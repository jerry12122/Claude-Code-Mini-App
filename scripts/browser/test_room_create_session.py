"""工作室內只建立 session；原搜尋測試與 fixtures 原樣沿用。"""
import os
from pathlib import Path
import re

import pytest
from playwright.sync_api import expect
from test_room_session_search import browser_context_args, data, new_dialog


@pytest.fixture
def creation(page, data):
    state = {'posts': [], 'pending': [], 'fail': False, 'hold': False, 'created': None}

    def sessions(route):
        if route.request.method != 'POST':
            route.fallback()
            return
        body = route.request.post_data_json
        state['posts'].append(body)
        if state['fail']:
            state['fail'] = False
            route.fulfill(status=503, json={'error': '測試建立失敗'})
            return
        state['created'] = {**body, 'id': 'created-in-room', 'status': 'idle', 'input_mode': 'agent'}
        if state['hold']:
            state['pending'].append(route)
        else:
            route.fulfill(status=201, json=state['created'])

    page.route('**/sessions', sessions)
    page.route('**/work-dirs', lambda route: route.fulfill(json=[]))
    page.route('**/model-options/*', lambda route: route.fulfill(json=[{'model_id': 'fixture-model', 'label': 'Fixture model'}]))
    return state


def shot(page, name):
    out = Path(os.environ.get('ROOM_CREATE_ARTIFACTS', 'test-results/room-create-session'))
    out.mkdir(parents=True, exist_ok=True)
    page.screenshot(path=str(out / (name + '.png')))


def prepare(page, login):
    dialog = new_dialog(page, login)
    dialog.get_by_label('名稱', exact=True).fill('工作草稿')
    search = dialog.get_by_role('searchbox')
    search.fill('Alpha')
    dialog.get_by_role('checkbox').check()
    dialog.get_by_role('combobox', name='中英 Alpha 的角色').select_option('pm')
    search.fill('Beta')
    dialog.get_by_role('checkbox').check()
    return dialog


def fill_creation(dialog):
    area = dialog.get_by_role('region', name='建立 session', exact=True)
    area.get_by_label('新 session 名稱', exact=True).fill(' 新執行者 ')
    area.get_by_placeholder('工作目錄（選填，如 /home/user/project）').fill(' D:/new-work ')
    return area


def test_create_select_merge_and_submit(page, login, data, creation):
    dialog = prepare(page, login)
    shot(page, 'before-create-entry')
    opener = dialog.get_by_role('button', name='建立 session', exact=True)
    expect(opener).to_be_visible()  # RED：原搜尋表單尚無建立入口。
    opener.click()
    area = fill_creation(dialog)
    expect(dialog.locator('form')).to_have_count(1)
    area.get_by_role('combobox', name='權限模式', exact=True).select_option('acceptEdits')
    area.get_by_role('combobox', name='Model', exact=True).select_option('fixture-model')
    area.get_by_label('載具', exact=True).select_option('codex')
    expect(area.get_by_role('combobox', name='權限模式', exact=True)).to_be_disabled()
    expect(area.get_by_role('combobox', name='權限模式', exact=True)).to_have_value('default')
    expect(area.get_by_role('combobox', name='Model', exact=True)).to_have_value('')
    area.get_by_label('載具', exact=True).select_option('claude')
    area.get_by_role('combobox', name='權限模式', exact=True).select_option('acceptEdits')
    area.get_by_role('combobox', name='Model', exact=True).select_option('fixture-model')
    name = area.get_by_label('新 session 名稱', exact=True)
    name.press('Enter')
    assert not creation['posts'] and not data['writes']
    assert name.evaluate('''el => {
      const e = new KeyboardEvent('keydown', {key:'Enter', isComposing:true, bubbles:true, cancelable:true});
      el.dispatchEvent(e); return e.defaultPrevented;
    }''') is False
    creation['hold'] = True
    confirm = area.get_by_role('button', name=re.compile(r'^(確認建立 session|建立中…)'))
    confirm.click()
    expect(confirm).to_be_disabled()
    confirm.evaluate('el => el.click()')
    assert len(creation['posts']) == 1
    assert creation['posts'][0] == {'name': '新執行者', 'work_dir': 'D:/new-work', 'permission_mode': 'acceptEdits',
                                    'agent_type': 'claude', 'cli_extra_args': ['--model', 'fixture-model'], 'effort': ''}
    with page.expect_response(lambda res: res.request.method == 'GET' and res.url.endswith('/sessions')):
        creation['pending'].pop().fulfill(status=201, json=creation['created'])
    expect(area).not_to_be_visible()
    expect(opener).to_be_focused()
    expect(dialog.get_by_role('searchbox')).to_have_value('')
    row = dialog.locator('li').filter(has_text='新執行者')
    expect(row).to_have_count(1)
    expect(row.get_by_role('checkbox')).to_be_checked()
    expect(row.get_by_role('combobox')).to_have_value('worker')
    expect(dialog.locator('legend').first).to_have_text('成員（已選 3）')
    # 先讓全域列表回傳舊資料，再發布新資料；局部合併不得消失或重複。
    data['sessions'].append(creation['created'])
    with page.expect_response(lambda res: res.request.method == 'GET' and res.url.endswith('/sessions')):
        page.evaluate("window.dispatchEvent(new Event('miniapp:sessions-refresh'))")
    expect(row).to_have_count(1)
    expect(row.get_by_role('checkbox')).to_be_checked()
    shot(page, 'created-worker')
    dialog.get_by_role('searchbox').fill('Beta')
    dialog.get_by_role('button', name='建立', exact=True).click()
    expect(dialog).not_to_be_visible()
    assert data['writes'][0]['body'] == {'name': '工作草稿', 'members': [
        {'session_id': 'PM-Alpha-123', 'role': 'pm'}, {'session_id': 'worker-BETA-456', 'role': 'worker'},
        {'session_id': 'created-in-room', 'role': 'worker'}]}
    assert not data['session_writes']


def test_edit_failure_retry_and_cancel_room(page, login, data, creation):
    login()
    page.get_by_role('button', name='更多操作：既有工作室', exact=True).click()
    page.get_by_role('menuitem', name='編輯／交接 PM', exact=True).click()
    dialog = page.get_by_role('dialog', name='編輯工作室', exact=True)
    opener = dialog.get_by_role('button', name='建立 session', exact=True)
    opener.click()
    area = dialog.get_by_role('region', name='建立 session', exact=True)
    area.get_by_role('button', name='確認建立 session', exact=True).click()
    expect(area.get_by_role('alert')).to_have_text('請填寫新會話名稱')
    assert not creation['posts']
    area = fill_creation(dialog)
    area.get_by_role('button', name='取消建立 session', exact=True).click()
    expect(area).not_to_be_visible()
    expect(opener).to_be_focused()
    expect(dialog.get_by_label('名稱', exact=True)).to_have_value('既有工作室')
    expect(dialog.locator('legend').first).to_have_text('成員（已選 2）')
    assert not creation['posts']
    opener.click()
    area = fill_creation(dialog)
    creation['fail'] = True
    area.get_by_role('button', name='確認建立 session', exact=True).click()
    expect(area.get_by_role('alert')).to_have_text('測試建立失敗')
    expect(area.get_by_label('新 session 名稱', exact=True)).to_have_value(' 新執行者 ')
    expect(area.get_by_placeholder('工作目錄（選填，如 /home/user/project）')).to_have_value(' D:/new-work ')
    shot(page, 'create-error')
    area.get_by_role('button', name='確認建立 session', exact=True).click()
    expect(area).not_to_be_visible()
    expect(dialog.locator('li').filter(has_text='新執行者').get_by_role('checkbox')).to_be_checked()
    data['sessions'].append(creation['created'])
    with page.expect_response(lambda res: res.request.method == 'GET' and res.url.endswith('/sessions')):
        page.evaluate("window.dispatchEvent(new Event('miniapp:sessions-refresh'))")
    dialog.get_by_role('button', name='取消', exact=True).click()
    expect(dialog).not_to_be_visible()
    expect(page.locator('.ra-session-card:not(.room-list-card)').filter(has_text='新執行者')).to_be_visible()
    assert len(creation['posts']) == 2 and not data['writes'] and not data['session_writes']
    assert creation['created']['id'] == 'created-in-room'  # 取消工作室沒有 DELETE。


@pytest.mark.parametrize('width', [390, 320])
def test_narrow_create_and_escape(page, login, data, creation, width):
    page.set_viewport_size({'width': width, 'height': 800})
    dialog = new_dialog(page, login)
    dialog.get_by_role('button', name='建立 session', exact=True).click()
    area = fill_creation(dialog)
    expect(area.get_by_text('取消工作室不會刪除已建立的 session。', exact=True)).to_be_visible()
    assert dialog.evaluate('el => el.scrollWidth <= el.clientWidth')
    assert page.evaluate('document.documentElement.scrollWidth <= innerWidth')
    shot(page, 'create-' + str(width))
    area.get_by_role('button', name='確認建立 session', exact=True).scroll_into_view_if_needed()
    expect(area.get_by_role('button', name='確認建立 session', exact=True)).to_be_in_viewport()
    assert dialog.evaluate('el => el.scrollWidth <= el.clientWidth')
    shot(page, 'create-actions-' + str(width))
    area.get_by_label('新 session 名稱', exact=True).press('Escape')
    expect(dialog).not_to_be_visible()
    expect(page.get_by_role('button', name='新增工作室', exact=True)).to_be_focused()
    assert not creation['posts'] and not data['writes']
