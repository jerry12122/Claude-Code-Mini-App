// 工作室（唯讀虛擬群聊）前端：純邏輯（hooks/rooms.js、hooks/roomPoller.js）與唯讀／安全的靜態斷言。
// 執行：node scripts/rooms.test.cjs
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');

const JS = path.join(__dirname, '../internal/static/js');
const read = (rel) => fs.readFileSync(path.join(JS, rel), 'utf8');

// messageRows.js 提供真正的 formatMessageTime（UTC → 本地）；AGENT_LABEL 是 core 的常數，這裡放同形狀的替身。
const ctx = vm.createContext({});
vm.runInContext('const AGENT_LABEL = { claude: "Claude", codex: "Codex", cursor: "Cursor" };', ctx);
for (const f of ['hooks/messageRows.js', 'hooks/rooms.js', 'hooks/roomPoller.js']) vm.runInContext(read(f), ctx, { filename: f });
const g = vm.runInContext(`({
  ROOM_POLL_MS, ROOM_PAGE_SIZE, ROOM_READONLY_NOTE,
  roomsUrl, roomUrl, roomTimelineUrl, roomTimelineEmpty, roomMergeMessages, roomTimelineApply,
  roomChip, roomQuotaView, roomMessageMeta, roomIsNearBottom,
  roomFormInit, roomFormToggle, roomFormSetRole, roomFormError, roomFormBody, startRoomPoller,
})`, ctx);
const plain = (v) => JSON.parse(JSON.stringify(v));

// 訊息：API 回傳新到舊；id 是全域遞增整數
const msg = (id, at, extra = {}) => ({
  id, session_id: 's' + (id % 3), session_name: 'S' + (id % 3), member_role: 'worker', agent_type: 'claude',
  role: 'claude', author: '', text: 'm' + id, status: 'done', created_at: at, ...extra,
});
const ids = (state) => plain(state.messages.map((m) => m.id)); // vm 內外的 Array 不同 realm，deepEqual 前先轉

// ── URL ──────────────────────────────────────────────────────────────
assert.equal(g.roomsUrl(), '/rooms');
assert.equal(g.roomUrl('a/b'), '/rooms/a%2Fb');
assert.equal(g.roomTimelineUrl('r1', '', 50), '/rooms/r1/timeline?limit=50');
// 游標是後端給的不透明字串：原樣編碼帶回，不自己組
assert.equal(g.roomTimelineUrl('r1', 'a+b/c=', 20), '/rooms/r1/timeline?limit=20&before=a%2Bb%2Fc%3D');
assert.equal(g.ROOM_POLL_MS, 3000);

// ── 以 id 合併：舊在上新在下、同秒以 id 決勝、不重複 ──────────────────
{
  const page = { messages: [msg(5, '2026-11-18 10:00:02'), msg(4, '2026-11-18 10:00:01'), msg(3, '2026-11-18 10:00:01'), msg(2, '2026-11-18 10:00:00')], has_more: true, next_cursor: 'C2' };
  const s = g.roomTimelineApply(g.roomTimelineEmpty(), page, 'latest');
  assert.deepEqual(ids(s), [2, 3, 4, 5], '新到舊的 API 回應要轉成舊在上、新在下，同秒以 id 排');
  assert.equal(s.hasMore, true);
  assert.equal(s.nextCursor, 'C2');
  assert.equal(s.loaded, true);

  // 輪詢再拿到同一頁 + 一則新的：不重複
  const again = g.roomTimelineApply(s, { messages: [msg(6, '2026-11-18 10:00:03'), ...page.messages], has_more: true, next_cursor: 'C2b' }, 'latest');
  assert.deepEqual(ids(again), [2, 3, 4, 5, 6]);
  assert.equal(again.nextCursor, 'C2', '輪詢最新頁不得覆蓋「往更早翻頁」用的游標');
  assert.equal(again.messages.filter((m) => m.id === 4).length, 1);
  // 沒變的訊息保留同一個物件（讓氣泡不必重新渲染）
  assert.equal(again.messages[0], s.messages[0]);
}

// ── pending 同 id 就地更新，不重複插入 ────────────────────────────────
{
  const base = g.roomTimelineApply(g.roomTimelineEmpty(), { messages: [msg(8, '2026-11-18 10:00:05', { status: 'pending', text: '' }), msg(7, '2026-11-18 10:00:04')], has_more: false, next_cursor: '' }, 'latest');
  assert.equal(base.messages[1].status, 'pending');
  const streaming = g.roomTimelineApply(base, { messages: [msg(8, '2026-11-18 10:00:05', { status: 'pending', text: '串流中' }), msg(7, '2026-11-18 10:00:04')], has_more: false, next_cursor: '' }, 'latest');
  assert.deepEqual(ids(streaming), [7, 8]);
  assert.equal(streaming.messages[1].text, '串流中');
  assert.notEqual(streaming.messages[1], base.messages[1]);
  assert.equal(streaming.messages[0], base.messages[0]);
  const done = g.roomTimelineApply(streaming, { messages: [msg(8, '2026-11-18 10:00:05', { status: 'done', text: '串流中完成' }), msg(7, '2026-11-18 10:00:04')], has_more: false, next_cursor: '' }, 'latest');
  assert.deepEqual(ids(done), [7, 8], '完成後仍是同一個 id，不得多出一筆');
  assert.equal(done.messages[1].status, 'done');
  assert.equal(done.messages[1].text, '串流中完成');
}

// ── 更早頁面往前接，並更新游標 ────────────────────────────────────────
{
  let s = g.roomTimelineApply(g.roomTimelineEmpty(), { messages: [msg(10, '2026-11-18 10:01:00'), msg(9, '2026-11-18 10:00:59')], has_more: true, next_cursor: 'C9' }, 'latest');
  s = g.roomTimelineApply(s, { messages: [msg(8, '2026-11-18 10:00:58'), msg(7, '2026-11-18 10:00:58')], has_more: true, next_cursor: 'C7' }, 'older');
  assert.deepEqual(ids(s), [7, 8, 9, 10]);
  assert.equal(s.nextCursor, 'C7');
  assert.equal(s.hasMore, true);
  s = g.roomTimelineApply(s, { messages: [msg(6, '2026-11-18 10:00:57')], has_more: false, next_cursor: '' }, 'older');
  assert.deepEqual(ids(s), [6, 7, 8, 9, 10]);
  assert.equal(s.hasMore, false);
  assert.equal(s.nextCursor, '');
  // 翻頁和輪詢撞在一起造成的重疊也不重複
  s = g.roomTimelineApply(s, { messages: [msg(7, '2026-11-18 10:00:58'), msg(6, '2026-11-18 10:00:57')], has_more: false, next_cursor: '' }, 'older');
  assert.deepEqual(ids(s), [6, 7, 8, 9, 10]);
}

// 兩次輪詢之間新訊息超過一頁（最新頁與已載入的完全不相接）：丟掉舊的重來，避免時間軸中間缺一塊
{
  let s = g.roomTimelineApply(g.roomTimelineEmpty(), { messages: [msg(2, '2026-11-18 10:00:01'), msg(1, '2026-11-18 10:00:00')], has_more: false, next_cursor: '' }, 'latest');
  s = g.roomTimelineApply(s, { messages: [msg(9, '2026-11-18 10:00:09'), msg(8, '2026-11-18 10:00:08')], has_more: true, next_cursor: 'C8' }, 'latest');
  assert.deepEqual(ids(s), [8, 9]);
  assert.equal(s.hasMore, true);
  assert.equal(s.nextCursor, 'C8');
}
// 空工作室：先載入成功（空），之後第一則訊息進來
{
  let s = g.roomTimelineApply(g.roomTimelineEmpty(), { messages: [], has_more: false, next_cursor: '' }, 'latest');
  assert.equal(s.loaded, true);
  s = g.roomTimelineApply(s, { messages: [msg(1, '2026-11-18 10:00:00')], has_more: false, next_cursor: '' }, 'latest');
  assert.deepEqual(ids(s), [1]);
}
// 壞回應不得讓時間軸壞掉
assert.deepEqual(ids(g.roomTimelineApply(g.roomTimelineEmpty(), null, 'latest')), []);
assert.deepEqual(ids(g.roomTimelineApply(g.roomTimelineEmpty(), {}, 'older')), []);
assert.deepEqual(plain(g.roomMergeMessages([], [msg(1, 'x'), msg(1, 'x')])).length, 1);

// ── 成員晶片資料整理 ──────────────────────────────────────────────────
{
  const chip = g.roomChip({
    session_id: 'abc', role: 'pm', position: 0, session_name: 'PM 小隊長', agent_type: 'codex', status: 'running',
    quota: { provider: 'codex', display_text: '週 80%', stale: false, updated_at: '2026-11-18T05:00:00Z' },
  });
  assert.equal(chip.sessionId, 'abc');
  assert.equal(chip.name, 'PM 小隊長');
  assert.equal(chip.agentType, 'codex');
  assert.equal(chip.agentLabel, 'Codex');
  assert.equal(chip.role, 'pm');
  assert.equal(chip.roleLabel, 'PM');
  assert.equal(chip.status, 'running');
  assert.equal(chip.quotaText, '週 80%');
  assert.equal(chip.quotaStale, false);

  const worker = g.roomChip({ session_id: 'w', role: 'worker', session_name: '', agent_type: 'weird', status: 'idle' });
  assert.equal(worker.roleLabel, '執行者');
  assert.equal(worker.name, '未命名');
  assert.equal(worker.agentLabel, 'weird', '未知的 agent 類型直接顯示原字串');
  assert.equal(worker.quotaText, '—', '沒有 quota 物件也不能壞');
  assert.equal(worker.quotaStale, false);
}

// ── 額度過期標示 ──────────────────────────────────────────────────────
{
  const fresh = g.roomQuotaView({ display_text: '5h 40%', stale: false });
  assert.equal(fresh.text, '5h 40%');
  assert.equal(fresh.stale, false);

  const stale = g.roomQuotaView({ display_text: '5h 40%', stale: true, updated_at: '2026-11-18T05:00:00Z' });
  assert.equal(stale.text, '5h 40%');
  assert.equal(stale.stale, true);
  assert.match(stale.title, /過期/);

  // 後端把「從未取得額度」也標 stale=true；那時只有佔位 —，不該顯示成「過期的數字」
  const unknown = g.roomQuotaView({ display_text: '—', stale: true });
  assert.equal(unknown.text, '—');
  assert.equal(unknown.stale, false);
  assert.match(unknown.title, /尚無/);
  assert.equal(g.roomQuotaView(null).text, '—');
  assert.equal(g.roomQuotaView({ display_text: '', stale: true }).stale, false);
}

// ── 訊息氣泡資料：user／claude／system 要分得出來；時間是本地時間 ──────
{
  const pad = (n) => String(n).padStart(2, '0');
  const now = new Date();
  const utc = `${now.getUTCFullYear()}-${pad(now.getUTCMonth() + 1)}-${pad(now.getUTCDate())} ${pad(now.getUTCHours())}:${pad(now.getUTCMinutes())}:00`;
  const localHM = (() => { const d = new Date(Date.UTC(now.getUTCFullYear(), now.getUTCMonth(), now.getUTCDate(), now.getUTCHours(), now.getUTCMinutes())); return `${pad(d.getHours())}:${pad(d.getMinutes())}`; })();

  const user = g.roomMessageMeta(msg(1, utc, { role: 'user', author: '' }));
  assert.equal(user.side, 'user');
  assert.equal(user.roleLabel, '使用者');
  assert.equal(g.roomMessageMeta(msg(1, utc, { role: 'user', author: '小明' })).roleLabel, '小明');

  const ai = g.roomMessageMeta(msg(2, utc, { role: 'claude', agent_type: 'cursor', session_name: 'W1', member_role: 'worker' }));
  assert.equal(ai.side, 'assistant');
  assert.equal(ai.roleLabel, 'Cursor', '助理訊息在資料庫一律是 claude，標籤要看 agent 類型');
  assert.equal(ai.sourceName, 'W1');
  assert.equal(ai.agentLabel, 'Cursor');
  assert.equal(ai.memberRoleLabel, '執行者');

  const sys = g.roomMessageMeta(msg(3, utc, { role: 'system' }));
  assert.equal(sys.side, 'system');
  assert.equal(sys.roleLabel, '系統');

  assert.notEqual(user.side, ai.side);
  assert.notEqual(ai.side, sys.side);
  assert.equal(g.roomMessageMeta(msg(4, utc, { status: 'pending' })).pending, true);
  assert.equal(g.roomMessageMeta(msg(4, utc, { status: 'done' })).pending, false);
  assert.equal(user.time, localHM, '時間要轉成本地時間（今天只顯示 HH:mm）');
  assert.equal(g.roomMessageMeta(msg(5, 'garbage')).time, '');
}

// ── 捲動：停在底部才自動跟隨 ──────────────────────────────────────────
assert.equal(g.roomIsNearBottom({ scrollHeight: 1000, scrollTop: 600, clientHeight: 400 }), true);
assert.equal(g.roomIsNearBottom({ scrollHeight: 1000, scrollTop: 560, clientHeight: 400 }), true, '差 40px 仍算在底部');
assert.equal(g.roomIsNearBottom({ scrollHeight: 1000, scrollTop: 300, clientHeight: 400 }), false, '往上看歷史就不跟隨');
assert.equal(g.roomIsNearBottom({ scrollHeight: 300, scrollTop: 0, clientHeight: 400 }), true, '內容不足一屏視同在底部');

// ── 表單 ──────────────────────────────────────────────────────────────
{
  assert.deepEqual(plain(g.roomFormInit(null)), { name: '', members: [] });
  const edit = g.roomFormInit({ name: 'R', members: [{ session_id: 'a', role: 'pm', session_name: 'A' }, { session_id: 'b', role: 'worker' }] });
  assert.deepEqual(plain(edit), { name: 'R', members: [{ session_id: 'a', role: 'pm' }, { session_id: 'b', role: 'worker' }] });

  let m = g.roomFormToggle([], 'a');
  assert.deepEqual(plain(m), [{ session_id: 'a', role: 'worker' }], '新勾選的成員預設是 worker');
  m = g.roomFormSetRole(m, 'a', 'pm');
  assert.equal(m[0].role, 'pm');
  assert.equal(g.roomFormSetRole(m, 'a', 'boss')[0].role, 'pm', '非法角色不接受');
  m = g.roomFormToggle(m, 'b');
  assert.deepEqual(plain(m.map((x) => x.session_id)), ['a', 'b'], '勾選順序就是成員順序');
  m = g.roomFormToggle(m, 'a');
  assert.deepEqual(plain(m.map((x) => x.session_id)), ['b'], '再點一次取消');

  assert.match(g.roomFormError({ name: '   ', members: [] }), /名稱/);
  assert.match(g.roomFormError({ name: 'x'.repeat(101), members: [] }), /名稱/);
  assert.match(g.roomFormError({ name: 'ok', members: Array.from({ length: 51 }, (_, i) => ({ session_id: 's' + i, role: 'worker' })) }), /50/);
  assert.equal(g.roomFormError({ name: ' ok ', members: [] }), '');

  assert.deepEqual(plain(g.roomFormBody({ name: '  新  ', members: [{ session_id: 'a', role: 'pm', extra: 1 }] })), { name: '新', members: [{ session_id: 'a', role: 'pm' }] });
}

// ── 輪詢：可見時每 3 秒一次；不可見就停；回到可見立刻補一次 ──────────────
async function pollerScenarios() {
  const flush = () => new Promise((r) => setImmediate(r));
  const makeEnv = () => {
    const timers = new Map();
    let nextId = 1;
    const doc = {
      hidden: false, listeners: {},
      addEventListener(t, f) { (this.listeners[t] ||= new Set()).add(f); },
      removeEventListener(t, f) { this.listeners[t]?.delete(f); },
      setHidden(h) { this.hidden = h; for (const f of [...(this.listeners.visibilitychange || [])]) f(); },
    };
    const env = {
      timers, doc, ticks: 0,
      setTimer(fn, ms) { const id = nextId++; timers.set(id, { fn, ms }); return id; },
      clearTimer(id) { timers.delete(id); },
      async fire() { const [id, t] = [...timers.entries()][0]; timers.delete(id); t.fn(); await flush(); },
    };
    env.tick = async () => { env.ticks++; };
    return env;
  };

  // 可見：排程一個 3 秒的計時器；觸發後執行一次並接著排下一個
  let e = makeEnv();
  let stop = g.startRoomPoller({ tick: e.tick, intervalMs: 3000, doc: e.doc, setTimer: e.setTimer, clearTimer: e.clearTimer });
  assert.equal(e.timers.size, 1);
  assert.equal([...e.timers.values()][0].ms, 3000);
  assert.equal(e.ticks, 0, '啟動時不立刻 tick（首次載入由呼叫端負責）');
  await e.fire();
  assert.equal(e.ticks, 1);
  assert.equal(e.timers.size, 1, 'tick 完成後排下一次');
  await e.fire();
  assert.equal(e.ticks, 2);

  // 頁籤變成不可見：待處理的計時器被取消，之後完全不再 tick
  e.doc.setHidden(true);
  assert.equal(e.timers.size, 0, '不可見時不得留著計時器');
  assert.equal(e.ticks, 2);

  // 回到可見：立刻補抓一次，並恢復排程
  e.doc.setHidden(false);
  await flush();
  assert.equal(e.ticks, 3, '回到可見立刻補一次');
  assert.equal(e.timers.size, 1);

  // 離開畫面（stop）：計時器清掉、監聽移除、之後再怎麼切換可見性也不 tick
  stop();
  assert.equal(e.timers.size, 0);
  assert.equal((e.doc.listeners.visibilitychange || new Set()).size, 0);
  e.doc.setHidden(true);
  e.doc.setHidden(false);
  await flush();
  assert.equal(e.ticks, 3);

  // 計時器碰巧在不可見時觸發：不 tick、不重新排程
  e = makeEnv();
  stop = g.startRoomPoller({ tick: e.tick, intervalMs: 3000, doc: e.doc, setTimer: e.setTimer, clearTimer: e.clearTimer });
  e.doc.hidden = true; // 沒有發出 visibilitychange 的競態
  await e.fire();
  assert.equal(e.ticks, 0);
  assert.equal(e.timers.size, 0);
  stop();

  // 一次 tick 還沒做完時不重疊；tick 出錯也不能讓輪詢停掉
  e = makeEnv();
  let release;
  let calls = 0;
  const slow = () => { calls++; return calls === 1 ? new Promise((r) => { release = r; }) : Promise.reject(new Error('boom')); };
  stop = g.startRoomPoller({ tick: slow, intervalMs: 3000, doc: e.doc, setTimer: e.setTimer, clearTimer: e.clearTimer });
  await e.fire();
  assert.equal(calls, 1);
  assert.equal(e.timers.size, 0, 'tick 進行中不排下一次');
  e.doc.setHidden(true);
  e.doc.setHidden(false); // 進行中又回到可見：不另開一個重疊的 tick
  await flush();
  assert.equal(calls, 1);
  release();
  await flush();
  assert.equal(e.timers.size, 1, '做完才排下一次');
  await e.fire();
  assert.equal(calls, 2);
  assert.equal(e.timers.size, 1, '出錯後仍會繼續排程');
  stop();

  // stop 之後才完成的 tick 不得再排程
  e = makeEnv();
  calls = 0;
  stop = g.startRoomPoller({ tick: slow, intervalMs: 3000, doc: e.doc, setTimer: e.setTimer, clearTimer: e.clearTimer });
  await e.fire();
  stop();
  release();
  await flush();
  assert.equal(e.timers.size, 0);
}

// ── 靜態斷言：唯讀、安全渲染、不新增通道 ──────────────────────────────
function staticChecks() {
  const roomFiles = ['hooks/rooms.js', 'hooks/roomPoller.js', 'hooks/useRoomTimeline.js', 'session/RoomsSection.js', 'session/RoomFormModal.js', 'session/RoomView.js', 'session/RoomMessageBubble.js'];
  const strip = (s) => s.replace(/\/\*[\s\S]*?\*\//g, '').replace(/^\s*\/\/.*$/gm, '');
  for (const f of roomFiles) {
    const src = strip(read(f));
    assert.ok(!/new WebSocket|eventsWsURL|wsURL\(/.test(src), `${f} 不得新增 WebSocket`);
    assert.ok(!/\bfetch\(/.test(src), `${f} 要走 apiFetch`);
    assert.ok(!/\.innerHTML\s*=|insertAdjacentHTML|document\.write/.test(src), `${f} 不得直接寫 innerHTML`);
    assert.ok(!/role="dialog"|aria-modal/.test(src), `${f} 不得自製 modal`);
  }
  // 工作室畫面：沒有輸入框，也沒有任何會觸發成員的呼叫
  const view = strip(read('session/RoomView.js'));
  assert.ok(!/<textarea|<input|contentEditable|contenteditable/.test(view), '工作室畫面不得有輸入框');
  assert.ok(!/method:\s*'(POST|PUT|PATCH|DELETE)'/.test(view), '工作室畫面只讀');
  assert.ok(view.includes('ROOM_READONLY_NOTE'), '底部要有固定的唯讀說明');
  assert.equal(g.ROOM_READONLY_NOTE, '唯讀：要指示成員請進入該成員的 session');
  // 只有助理訊息走 markdown（parseMarkdown 內建 DOMPurify）；使用者與系統訊息是純文字節點
  const bubble = strip(read('session/RoomMessageBubble.js'));
  assert.equal((bubble.match(/dangerouslySetInnerHTML/g) || []).length, 1, '氣泡只能有一處 dangerouslySetInnerHTML');
  assert.ok(/parseMarkdown\(/.test(bubble), 'HTML 必須來自 parseMarkdown');
  assert.ok(/side === 'assistant'/.test(bubble), '只有助理訊息才渲染 markdown');
  // 訪客畫面不顯示工作室
  assert.ok(/isGuestMode/.test(strip(read('session/RoomsSection.js'))), '工作室區塊要在訪客模式下不渲染');
  // 刪除要二次確認並說明不會刪 session
  const section = read('session/RoomsSection.js');
  assert.ok(/不會刪除/.test(section) && /session/.test(section), '刪除確認要說明不會刪除 session');
  assert.ok(/showModal\(\)/.test(section), '刪除確認走原生 dialog');
  // 路由：索引註冊與桌面分頁系統無關
  assert.ok(!/tabGroups|dispatchTabGroups/.test(view), '工作室不整合分頁／分割視窗系統');
}

(async () => {
  await pollerScenarios();
  staticChecks();
  console.log('rooms checks passed');
})().catch((e) => { console.error(e); process.exit(1); });
