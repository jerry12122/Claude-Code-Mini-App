"""真 DOM：成員卡縮高、狀態可讀與 timeline 內距。"""
import json
import os
from pathlib import Path

import pytest
from playwright.sync_api import expect
from test_room_member_filter import _make_room, _open

OUT = Path(os.environ.get('ROOM_HEADER_ARTIFACTS', 'test-results/room-header'))


@pytest.fixture
def browser_context_args(browser_context_args):
    return {**browser_context_args, 'service_workers': 'block'}


def test_compact_header(page, login):
    OUT.mkdir(parents=True, exist_ok=True)
    page.set_viewport_size({'width': 1280, 'height': 900})
    ids, card = _make_room(page, login, '縮高驗證')
    screen = _open(page, card, '縮高驗證')
    metrics = {}
    problems = []
    for width in (1280, 1920, 390, 320):
        page.set_viewport_size({'width': width, 'height': 900})
        page.evaluate("""width => {
          document.querySelector('#header-probe')?.remove();
          const host = document.createElement('div'); host.id = 'header-probe';
          host.style.cssText = 'position:fixed;top:180px;left:0;width:100%;z-index:100;display:flex;align-items:center;gap:8px;overflow:auto;background:var(--ra-bg)';
          document.body.append(host);
          const base = {session_id:'probe', session_name:'一般', role:'worker', agent_type:'claude', status:'idle'};
          const members = [
            {...base, session_name:'閒置成員'},
            {...base, session_name:'執行成員', status:'running', queue_length:2},
            {...base, session_name:'額度成員', role:'pm', status:'running', quota:{display_text:'5h 17% / 週 18%', stale:true}},
            ...['confirm','input','shell'].map(attention => ({...base, session_name:attention, attention})),
            {...base, session_name:'暫停', queue_paused:true, queue_length:3},
            {...base, session_name:'錯誤', last_error:'精確錯誤內容'},
          ];
          ReactDOM.createRoot(host).render(React.createElement(React.Fragment, null,
            ...members.map((member, i) => React.createElement(RoomMemberChip, {
              key:i, member, compact:width < 1024, filter:{pressed:i===2}, onOpen:()=>{}
            }))));
        }""", width)
        chips = page.locator('#header-probe .room-member-chip')
        expect(chips).to_have_count(8)
        data = chips.evaluate_all("""els => els.map(el => ({height:el.getBoundingClientRect().height,
          text:el.innerText, aria:el.getAttribute('aria-label'), description:el.getAttribute('aria-description'),
          title:el.title, strike:!!el.querySelector('s'),
          inline: [...el.querySelector('.ra-status-dot').parentElement.children].every(child =>
            child.getBoundingClientRect().right <= el.getBoundingClientRect().right + 1)}))""")
        padding = screen.locator('.room-timeline').evaluate("""el => {
          const s=getComputedStyle(el); return {left:parseFloat(s.paddingLeft), right:parseFloat(s.paddingRight),
            available:el.clientWidth-parseFloat(s.paddingLeft)-parseFloat(s.paddingRight),
            content:el.firstElementChild.getBoundingClientRect().width}; }""")
        metrics[str(width)] = {'cards':data, 'timeline':padding}
        page.screenshot(path=str(OUT / f'header-{width}.png'))
        for i in (3, 5, 6, 7):
            chips.nth(i).scroll_into_view_if_needed()
            chips.nth(i).screenshot(path=str(OUT / f'badge-{width}-{i}.png'))
        if not all(d['inline'] for d in data):
            problems.append((width, 'badge overflows card', data))
        for d in data[:3]:
            if not 44 <= d['height'] <= 60 or '執行中' in d['text'] or '閒置\n' in d['text']:
                problems.append((width, 'extra status row / height', d))
            if not d['title'] or not d['aria']:
                problems.append((width, 'missing accessible status', d))
        if '執行中，活動資料未提供' not in data[1]['aria'] or '佇列 2 則' not in (data[1]['description'] or ''):
            problems.append((width, 'missing full status / queue description', data[1]))
        if '精確錯誤內容' not in data[7]['title'] or '精確錯誤內容' not in (data[7]['description'] or ''):
            problems.append((width, 'missing error details', data[7]))
        for d, status in zip(data[3:], ['待你授權','待你回答','待你確認指令','佇列已暫停','錯誤']):
            if status not in d['text']:
                problems.append((width, 'missing visible badge', d))
        if width >= 1024 and not data[2]['strike']:
            problems.append((width, 'missing stale quota'))
        expected = 32 if width >= 640 else 16
        if padding['left'] != expected or padding['right'] != expected or abs(padding['available']-padding['content']) > 1:
            problems.append((width, 'padding/width', padding))
    (OUT / 'metrics.json').write_text(json.dumps(metrics, ensure_ascii=False, indent=2), encoding='utf-8')
    assert not problems, problems


def test_timeline_padding_against_ordinary(page, login):
    from test_room_visual_parity import _create_env, _open_room, _open_session
    ordinary, room = _create_env(page, login)
    metrics = {}
    for width in (1280, 1920, 390, 320):
        page.set_viewport_size({'width': width, 'height': 900})
        _open_session(page, ordinary)
        chat = page.locator('.app-scroll').evaluate_all("""els => {
          const el=els.find(el=>el.classList.contains('sm:px-8') && el.clientHeight > 0);
          const s=getComputedStyle(el); return {left:s.paddingLeft,right:s.paddingRight,width:el.clientWidth};
        }""")
        _open_room(page, room)
        timeline = page.locator('.room-timeline').evaluate("""el => {
          const s=getComputedStyle(el);return {left:s.paddingLeft,right:s.paddingRight,width:el.clientWidth};
        }""")
        metrics[str(width)] = {'ordinary':chat,'room':timeline}
        assert timeline['left'] == chat['left'] and timeline['right'] == chat['right'], metrics
        page.screenshot(path=str(OUT / f'actual-room-{width}.png'))
    (OUT / 'padding-comparison.json').write_text(json.dumps(metrics, indent=2), encoding='utf-8')
