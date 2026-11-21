// 工作室前端：時間軸、輪詢、PM 輸入與安全渲染。
// 執行：node scripts/rooms.test.cjs
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');

const JS = path.join(__dirname, '../internal/static/js');
const read = (rel) => fs.readFileSync(path.join(JS, rel), 'utf8');

// messageRows.js 提供真正的 formatMessageTime（UTC → 本地）；AGENT_LABEL 是 core 的常數，這裡放同形狀的替身。
const ctx = vm.createContext({ File, TextEncoder });
vm.runInContext('const AGENT_LABEL = { claude: "Claude", codex: "Codex", cursor: "Cursor" };', ctx);
for (const f of ['hooks/messageRows.js', 'hooks/rooms.js', 'hooks/roomPoller.js']) vm.runInContext(read(f), ctx, { filename: f });
const g = vm.runInContext(`({
  ROOM_POLL_MS, ROOM_PAGE_SIZE,
  roomsUrl, roomUrl, roomTimelineUrl, roomTimelineEmpty, roomMergeMessages, roomTimelineApply,
  roomChip, roomQuotaView, roomMessageMeta, roomMemberState, roomJobsSplit, roomUnboundWorkers, roomIsNearBottom,
  roomFormInit, roomFormToggle, roomFormSetRole, roomFormError, roomFormBody, startRoomPoller,
  clipboardPasteFiles, roomLatestBodyId, roomTimelineDeliver, roomAdmitLocal, roomDropLocal, roomPatchLocal,
  roomDraftStorageKey, roomDraftParse, roomDraftSerialize, roomDraftUniqueFiles, roomDraftSameSnapshot, roomDraftHoldTarget,
  roomMentionChoices, roomMentionBody, roomComposerEnterAction,
  roomFilterTarget, roomTimelineScope, roomTimelineReset, roomTimelineAccept, roomFilterVisible, roomFilterEmptyText,
  roomQueueEditUrl, roomUtf8Bytes, roomQueueEditTarget, roomQueueEditInvalid, roomQueueEditFailure, roomQueueEditNetworkError, ROOM_QUEUE_EDIT_MAX_BYTES,
  chatQueueEditRoute, chatQueueEditRequest,
})`, ctx);
vm.runInContext('function sessionDisplayAgent(s) { return (s && (s.disguise_agent || s.agent_type)) || ""; }', ctx);
const mentionSrc = read('chat/MentionMenu.js');
vm.runInContext(mentionSrc.slice(mentionSrc.indexOf('function mentionQueryAtCursor'), mentionSrc.indexOf('function MentionChips')), ctx, { filename: 'chat/MentionMenu.js' });
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
  assert.equal(chip.statePrimary, '執行中，活動資料未提供');

  const pausedEmpty = g.roomMemberState({ status: 'idle', queue_paused: true, queue_length: 0, last_error: '舊錯誤', quota: { display_text: '5h 100%' } });
  assert.equal(pausedEmpty.primary, '閒置', '空佇列沒有東西可繼續，不顯示暫停');
  assert.equal(pausedEmpty.paused, false);
  assert.equal(pausedEmpty.attention, '');
  const pausedQueued = g.roomMemberState({ status: 'idle', queue_paused: true, queue_length: 2 });
  assert.equal(pausedQueued.primary, '佇列已暫停，還有 2 則');
  const quotaOnly = g.roomMemberState({ status: 'idle', queue_paused: false, queue_length: 0, last_error: '', quota: { display_text: '5h 100%' } });
  assert.equal(quotaOnly.primary, '閒置');
  const recovered = g.roomMemberState({ status: 'idle', queue_paused: false, last_error: '' });
  assert.equal(recovered.error, '');
  const jobs = g.roomJobsSplit([{ id: 2, status: 'done' }, { id: 1, status: 'running' }, { id: 3, status: 'failed' }]);
  assert.deepEqual(jobs.open.map((job) => job.id), [1]);
  assert.deepEqual(jobs.closed.map((job) => job.id), [2, 3]);
}

// ── 正式派工之外的會話活動：不偽造 job，也不讓空的暫停佇列看似忙碌 ──────
{
  const running = { session_id: 'running', session_name: '執行者', role: 'worker', status: 'running' };
  const jobs = ['done', 'failed', 'interrupted'].map((status, id) => ({ id, worker_id: 'running', status }));
  const members = [running, { ...running, session_id: 'pm', role: 'pm' },
    { ...running, session_id: 'idle', status: 'idle', last_error: '舊錯誤' },
    { ...running, session_id: 'paused-empty', status: 'idle', queue_paused: true, queue_length: 0 }];
  const snapshot = JSON.stringify({ members, jobs });
  assert.deepEqual(plain(g.roomUnboundWorkers(members, jobs).map((m) => m.session_id)), ['running'],
    '所有正式 job 已結束，running worker 仍須作為未綁定會話活動顯示；PM 與閒置者排除');
  assert.equal(g.roomUnboundWorkers(members, jobs)[0], running, '保留真實成員資料，不建立假派工');
  assert.equal(JSON.stringify({ members, jobs }), snapshot, '不得改寫已完成／失敗／中斷 job 或成員狀態');
  for (const status of ['queued', 'running', 'slow', 'blocked']) {
    const open = { id: 31, worker_id: 'running', status };
    assert.deepEqual(plain(g.roomUnboundWorkers(members, [...jobs, open])), [], `${status} job 已綁定，不重複活動列`);
    assert.deepEqual(plain(g.roomJobsSplit([...jobs, open]).open), [open], '保留原始 open job 生命週期');
  }
  const pending = [
    { session_id: 'confirm', role: 'worker', status: 'idle', attention: 'confirm' },
    { session_id: 'input', role: 'worker', status: 'idle', attention: 'input' },
    { session_id: 'shell', role: 'worker', status: 'idle', attention: 'shell' },
    { session_id: 'queued', role: 'worker', status: 'idle', queue_length: 2 },
    { session_id: 'paused', role: 'worker', status: 'idle', queue_length: 1, queue_paused: true },
  ];
  assert.deepEqual(plain(g.roomUnboundWorkers(pending, []).map((m) => m.session_id)), pending.map((m) => m.session_id));
  assert.deepEqual(plain(g.roomUnboundWorkers([{ ...running, status: 'idle' }], jobs)), [], '刷新為 idle 後活動消失');
  assert.deepEqual(plain(g.roomUnboundWorkers(null, null)), []);
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
  const unknown = g.roomMessageMeta(msg(6, utc, { role: 'user', author: '', source_kind: 'unknown', sender: '來源未辨識', body: '真正的問題', text: '信封原文', mine: false }));
  assert.equal(unknown.mine, false);
  assert.equal(unknown.sender, '來源未辨識');
  assert.equal(unknown.body, '真正的問題');
  const agent = g.roomMessageMeta(msg(7, utc, { role: 'user', source_kind: 'agent', sender: '寄件者', source_room: '別間', mine: false }));
  assert.equal(agent.mine, false);
  assert.equal(agent.sender, '寄件者');
  assert.equal(agent.sourceRoom, '別間');
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

// ── 送出合併與最新全文 ────────────────────────────────────────
{
  const local = (id, text, extra = {}) => ({
    id: 'local:' + id, local: true, input_request_id: id, role: 'user', text, body: text,
    delivery: 'sending', created_at: '2026-11-19 12:00:00', ...extra,
  });
  const saved = (id, requestId) => msg(id, '2026-11-19 12:00:0' + id, { role: 'user', text: '同樣文字', body: '同樣文字', input_request_id: requestId });
  let state = g.roomAdmitLocal(g.roomTimelineEmpty(), local('req-a', '同樣文字'));
  state = g.roomAdmitLocal(state, local('req-b', '同樣文字'));
  assert.deepEqual(ids(state), ['local:req-a', 'local:req-b']);
  state = g.roomPatchLocal(state, 'req-a', { delivery: 'queued' });
  assert.equal(state.messages.find((m) => m.input_request_id === 'req-a').delivery, 'queued');
  const promoted = g.roomTimelineDeliver(state, { messages: [saved(3, 'req-a')], has_more: false, next_cursor: '' }, 'latest');
  assert.deepEqual(ids(promoted), [3, 'local:req-b']);
  const both = g.roomTimelineDeliver(promoted, { messages: [saved(4, 'req-b'), saved(3, 'req-a')], has_more: false, next_cursor: '' }, 'latest');
  assert.deepEqual(ids(both), [3, 4]);
  const retried = g.roomAdmitLocal(g.roomAdmitLocal(g.roomTimelineEmpty(), local('req-a', '同樣文字', { delivery: 'failed' })), local('req-a', '同樣文字'));
  assert.equal(retried.messages.filter((m) => m.input_request_id === 'req-a').length, 1);
  assert.equal(g.roomDropLocal(retried, 'req-a').messages.length, 0);
  const assistant = msg(8, '2026-11-19 12:00:08', { role: 'claude', text: '長回覆', body: '長回覆' });
  const older = msg(7, '2026-11-19 12:00:07', { role: 'user', text: '上一則', body: '上一則' });
  const system = msg(9, '2026-11-19 12:00:09', { role: 'system', text: '交接', body: '交接' });
  assert.equal(g.roomLatestBodyId([older, assistant, system]), 8);
  assert.equal(g.roomLatestBodyId([older, assistant, local('req-c', '新的')]), 'local:req-c');
  const png = new File(['png'], 'shot', { type: 'image/png' });
  const note = new File(['hi'], 'note.txt', { type: 'text/plain' });
  const pasted = g.clipboardPasteFiles({ files: [png, note] });
  assert.equal(pasted[0].name, 'paste.png');
  assert.equal(pasted[0].type, 'image/png');
  assert.equal(pasted[1].name, 'note.txt');
  assert.equal(g.clipboardPasteFiles({ files: [] }).length, 0);
  assert.equal(g.clipboardPasteFiles(null).length, 0);
  const queued = msg(-3, '2026-11-19 12:00:01', { role: 'user', text: '排隊重整仍可見', body: '排隊重整仍可見', input_request_id: 'req-q', queued: true });
  const visible = g.roomTimelineApply(g.roomTimelineEmpty(), { messages: [queued], has_more: false, next_cursor: '' }, 'latest');
  assert.deepEqual(ids(visible), [-3]);
  const confirmed = g.roomTimelineApply(visible, { messages: [msg(9, '2026-11-19 12:00:01', { role: 'user', text: '排隊重整仍可見', body: '排隊重整仍可見', input_request_id: 'req-q' })], has_more: false, next_cursor: '' }, 'latest');
  assert.deepEqual(ids(confirmed), [9]);
}

// ── 工作室草稿與 @ ────────────────────────────────────────────
{
  assert.equal(g.roomDraftStorageKey('room-a').startsWith('cc_room_draft:'), true);
  assert.notEqual(g.roomDraftStorageKey('room-a'), 'cc_draft_input:room-a');
  assert.notEqual(g.roomDraftStorageKey('room-a'), g.roomDraftStorageKey('room-b'));
  const alpha = { id: 'alpha', name: '同名', work_dir: 'D:/mention-root/alpha', agent_type: 'claude' };
  const beta = { id: 'beta', name: '同名', work_dir: 'D:/mention-root/beta', agent_type: 'codex' };
  const raw = g.roomDraftSerialize('房間A草稿', [alpha], 'pm-old');
  const parsed = g.roomDraftParse(raw);
  assert.equal(parsed.ok, true);
  assert.equal(parsed.text, '房間A草稿');
  assert.equal(parsed.targetPmId, 'pm-old');
  assert.equal(parsed.mentions[0].id, 'alpha');
  assert.equal(g.roomDraftParse('{').ok, false);
  assert.equal(g.roomDraftUniqueFiles([{ id: 'f1' }, { id: 'f1' }, { id: 'f2' }]).length, 2);
  const sent = { text: '房間A草稿', files: [{ id: 'f1' }], mentions: [{ id: 'alpha' }] };
  assert.equal(g.roomDraftSameSnapshot(sent, sent), true);
  assert.equal(g.roomDraftSameSnapshot({ text: '新草稿', files: [{ id: 'f1' }], mentions: [{ id: 'alpha' }] }, sent), false);
  assert.equal(g.roomDraftHoldTarget(parsed, 'pm-new', true), true);
  assert.equal(g.roomDraftHoldTarget({ text: '', mentions: [], targetPmId: 'pm-old' }, 'pm-new', false), false);
  const choices = g.roomMentionChoices([alpha, beta], 'pm-1', '同名', []);
  assert.deepEqual(choices.map((item) => item.id), ['alpha', 'beta']);
  const body = g.roomMentionBody('請問', { session_id: 'pm-1', session_name: 'PM', agent_type: 'claude' }, [beta]);
  assert.ok(body.includes('session_id=beta'), body);
  assert.ok(body.includes('from_session_id=<self>'), body);
  assert.ok(!body.includes('session_id=alpha'), body);
  assert.equal(g.roomMentionBody('純@未選', { session_id: 'pm-1' }, []), '純@未選');
  assert.equal(g.roomComposerEnterAction({ key: 'Enter', keyCode: 229, isComposing: true }, { open: true, count: 2 }), 'ime');
  assert.equal(g.roomComposerEnterAction({ key: 'ArrowDown' }, { open: true, count: 2 }), 'next');
  assert.equal(g.roomComposerEnterAction({ key: 'Enter' }, { open: true, count: 2 }), 'select');
  assert.equal(g.roomComposerEnterAction({ key: 'Enter' }, { open: true, count: 0 }), 'hold');
  assert.equal(g.roomComposerEnterAction({ key: 'Escape' }, { open: true, count: 2 }), 'close');
  assert.equal(g.roomComposerEnterAction({ key: 'Enter' }, { open: false, count: 0 }), 'send');
}

// ── 成員篩選：只改閱讀範圍，競態世代靠 scope ──────────────────────────
{
  assert.equal(g.roomTimelineUrl('r1', '', 50, 's1'), '/rooms/r1/timeline?limit=50&member=s1');
  assert.equal(g.roomTimelineUrl('r1', 'C', 50, 'a/b'), '/rooms/r1/timeline?limit=50&before=C&member=a%2Fb');
  assert.equal(g.roomTimelineUrl('r1', '', 50, ''), '/rooms/r1/timeline?limit=50', '總覽不帶 member');

  const room = { members: [{ session_id: 'pm', role: 'pm', session_name: 'PM' }, { session_id: 'w1', role: 'worker', session_name: '同名' }, { session_id: 'w2', role: 'worker', session_name: '同名' }] };
  assert.deepEqual(plain(g.roomFilterTarget(room, 'w2')), { id: 'w2', role: 'worker', name: '同名' }, '以 id 認人，同名不混淆');
  assert.equal(g.roomFilterTarget(room, ''), null, '空字串＝總覽');
  assert.equal(g.roomFilterTarget(room, 'gone'), null, '被移出的成員回到總覽');
  assert.equal(g.roomFilterTarget(null, 'w1'), null);
  // 交接後同一個 session 角色變了：scope 變，時間軸要重載
  const asWorker = g.roomTimelineScope('r1', g.roomFilterTarget(room, 'w1'));
  const asPm = g.roomTimelineScope('r1', { id: 'w1', role: 'pm', name: '同名' });
  assert.notEqual(asWorker, asPm);
  assert.notEqual(g.roomTimelineScope('r1', null), asWorker);
  assert.notEqual(g.roomTimelineScope('r2', null), g.roomTimelineScope('r1', null));

  // A→B 切換：A 的延遲回應不得插進 B 的時間軸，也不能動 B 的游標
  const scopeA = g.roomTimelineScope('r1', { id: 'w1', role: 'worker' });
  const scopeB = g.roomTimelineScope('r1', { id: 'w2', role: 'worker' });
  const stateA = g.roomTimelineAccept(g.roomTimelineReset(g.roomTimelineEmpty(), scopeA), scopeA, { messages: [msg(9, '2026-11-18 10:00:09')], has_more: true, next_cursor: 'CA' }, 'latest');
  assert.deepEqual(ids(stateA), [9]);
  assert.equal(stateA.scope, scopeA, '結果要帶著它所屬的 scope');
  const stateB = g.roomTimelineReset(stateA, scopeB);
  assert.deepEqual(ids(stateB), [], '切換後清空');
  assert.equal(stateB.loaded, false);
  assert.equal(stateB.nextCursor, '');
  assert.equal(g.roomTimelineAccept(stateB, scopeA, { messages: [msg(8, '2026-11-18 10:00:08')], has_more: false, next_cursor: '' }, 'latest'), stateB, 'A 的晚到最新頁被丟棄');
  assert.equal(g.roomTimelineAccept(stateB, scopeA, { messages: [msg(7, '2026-11-18 10:00:07')], has_more: false, next_cursor: '' }, 'older'), stateB, 'A 的晚到舊頁被丟棄');
  const okB = g.roomTimelineAccept(stateB, scopeB, { messages: [msg(5, '2026-11-18 10:00:05')], has_more: false, next_cursor: '' }, 'latest');
  assert.deepEqual(ids(okB), [5]);

  // 樂觀訊息跨切換保留（不能因為換篩選就丟掉送不出去的訊息），但別的訊息一律清掉
  const local = { id: 'local:x', local: true, input_request_id: 'x', role: 'user', text: '送出中', created_at: '' };
  const withLocal = g.roomAdmitLocal(stateA, local);
  const reset = g.roomTimelineReset(withLocal, scopeB);
  assert.deepEqual(ids(reset), ['local:x']);
  // 樂觀訊息是給 PM 的：看 worker 時不顯示，看總覽／PM 時顯示
  assert.equal(g.roomFilterVisible(reset.messages, 'worker').length, 0);
  assert.equal(g.roomFilterVisible(reset.messages, 'pm').length, 1);
  assert.equal(g.roomFilterVisible(reset.messages, '').length, 1);
  assert.equal(g.roomFilterVisible(reset.messages, '').length, reset.messages.length);
  // 沒有要隱藏的就沿用同一個陣列（版面不必重算）
  assert.equal(g.roomFilterVisible(okB.messages, 'worker'), okB.messages);
  // 空狀態要說目前篩選，不能說整間沒有訊息
  assert.equal(g.roomFilterEmptyText(null), '成員還沒有任何訊息。');
  assert.match(g.roomFilterEmptyText({ id: 'w1', role: 'worker', name: '小明' }), /「小明」/);
  assert.match(g.roomFilterEmptyText({ id: 'pm', role: 'pm', name: 'PM' }), /你與.*PM/);
  assert.ok(!/成員還沒有任何訊息/.test(g.roomFilterEmptyText({ id: 'w1', role: 'worker', name: 'x' })));
}

// ── 靜態斷言：PM 輸入、安全渲染 ──────────────────────────────
function staticChecks() {
  const roomFiles = ['hooks/rooms.js', 'hooks/roomPoller.js', 'hooks/useRoomTimeline.js', 'session/RoomsSection.js', 'session/RoomFormModal.js', 'session/RoomView.js', 'session/RoomMessageBubble.js', 'session/RoomControls.js'];
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
  assert.ok(/<RoomComposer\b/.test(view), '工作室掛載 PM 輸入框');
  const controls = strip(read('session/RoomControls.js'));
  assert.ok(/apiFetch\(roomUrl\(roomId\) \+ '\/messages'/.test(controls), '輸入只走工作室 PM 入口');
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

// 排隊輸入編輯：資格、bytes 規則與失敗文字（純函式）。
function queueEditChecks() {
  const queued = { id: -7, queued: true, role: 'user', source_kind: 'user', session_id: 'pm-1', text: '原文', attachments: [{ id: 'a' }] };
  assert.deepEqual(plain(g.roomQueueEditTarget(queued, 'pm-1')), { msgId: -7, queueId: 7, expectedPm: 'pm-1', previous: '原文', hasAttachments: true });
  const no = (patch, pm = 'pm-1') => assert.equal(g.roomQueueEditTarget({ ...queued, ...patch }, pm), null);
  no({ queued: false });           // 已開始的正式訊息
  no({ id: 7 });                   // 正 id：持久訊息
  no({ id: 0 });
  no({ id: -1.5 });
  no({ id: 'local:abc' });         // 尚未上傳的本地氣泡
  no({ local: true });             // sending／failed
  no({ source_kind: 'job' });      // 正式 job／report／agent 來源
  no({ source_kind: 'agent' });
  no({ role: 'claude' });
  no({ session_id: '' });
  no({}, 'pm-2');                  // PM 已交接：項目不在現任 PM 佇列
  assert.equal(g.roomQueueEditTarget(null, 'pm-1'), null);
  assert.equal(g.roomQueueEditUrl('r/1', 7), '/rooms/r%2F1/messages/7');
  // 64000 bytes 依 UTF-8 計，不是字元數；剛好 64000 可以，超過不行；不截字
  assert.equal(g.roomUtf8Bytes('中'), 3);
  assert.equal(g.roomQueueEditInvalid('中'.repeat(21333) + 'a', false), '');
  assert.match(g.roomQueueEditInvalid('中'.repeat(21334), false), /64000.*64002/);
  assert.equal(g.roomQueueEditInvalid('a'.repeat(64000), true), '');
  assert.match(g.roomQueueEditInvalid('a'.repeat(64001), true), /64001/);
  // 空白：沒有附件不可；有原附件可
  assert.match(g.roomQueueEditInvalid(' \n ', false), /請輸入文字/);
  assert.equal(g.roomQueueEditInvalid('', true), '');
  // 失敗文字都保留草稿、409 涵蓋出列／修改／交接
  assert.match(g.roomQueueEditFailure(409, ''), /PM 已交接/);
  assert.match(g.roomQueueEditFailure(409, ''), /草稿已保留/);
  assert.match(g.roomQueueEditFailure(404, ''), /草稿已保留/);
  assert.match(g.roomQueueEditFailure(403, ''), /草稿已保留/);
  assert.equal(g.roomQueueEditFailure(400, '文字無效'), '文字無效');
  assert.match(g.roomQueueEditFailure(500, ''), /草稿已保留/);
  assert.match(g.roomQueueEditNetworkError(new Error('連線逾時，請稍後再試')), /^連線逾時.*不會自動重送/);
  assert.match(g.roomQueueEditNetworkError(new TypeError('Failed to fetch')), /^連線中斷/);
  // 接線：單一 per-room 草稿狀態放在 RoomView；儲存只走 PATCH，且只在使用者操作時送出（沒有自動重試）
  const view = read('session/RoomView.js');
  assert.ok(/method: 'PATCH'/.test(view) && /expected_pm_id: cur\.expectedPm/.test(view), '編輯走 PATCH 並帶捕捉到的 expected PM');
  assert.ok(!/setInterval|setTimeout\([^)]*onSave|retry/i.test(view.slice(view.indexOf('onSave'), view.indexOf('onReload'))), '儲存失敗不得自動重試');
  const bubble = read('session/RoomMessageBubble.js');
  assert.ok(/<textarea/.test(bubble) && /htmlFor=\{id\}/.test(bubble) && !/onKeyDown/.test(bubble.slice(bubble.indexOf('function RoomQueueEditor'), bubble.indexOf('const RoomMessageBubble'))), '原生 textarea＋label，不攔截 Enter／IME');
}

function batchHintChecks() {
  const src = read('hooks/useRoomTimeline.js');
  const start = src.indexOf('function roomQueuePageHint');
  const end = src.indexOf('function useRoomQueue');
  if (start < 0 || end < start) throw new Error('roomQueuePageHint 尚未提供第一頁能力提示');
  vm.runInContext(src.slice(start, end), ctx);
  const hint = vm.runInContext('({ roomQueuePageHint, roomQueueKeepHint, roomQueueHintForScope })', ctx);
  assert.equal(hint.roomQueuePageHint(null), null);
  assert.equal(hint.roomQueuePageHint({}), null);
  assert.equal(hint.roomQueuePageHint({ batch_ready: false }), false);
  assert.equal(hint.roomQueuePageHint({ batch_ready: true }), true);
  assert.equal(hint.roomQueueKeepHint(false, { batch_ready: true }, false), false);
  assert.equal(hint.roomQueueKeepHint(null, { batch_ready: true }, true), true);
  assert.equal(hint.roomQueueHintForScope({ scope: 'pm-1', ready: true, batchReady: false }, 'pm-1'), false);
  assert.equal(hint.roomQueueHintForScope({ scope: 'pm-1', ready: true, batchReady: true }, 'pm-2'), null);
  assert.equal(hint.roomQueueHintForScope({ scope: 'pm-1', ready: false, batchReady: true }, 'pm-1'), null);
  const view = read('session/RoomView.js');
  assert.ok(/queueState\.batchReady !== true/.test(view), '缺提示或不可批次時不得送出');
  const controls = read('session/RoomControls.js');
  assert.ok(controls.includes('這幾則中間有其他來源訊息，無法一起送；會依原順序執行'), '不可批次要有可讀原因');
  assert.ok(controls.includes('無法確認能否一起送，請重新同步'), '未知提示要要求重新同步');
  assert.ok(/aria-disabled="true"/.test(controls) && /aria-describedby=\{reasonId\}/.test(controls), '原因要能被聚焦讀到');
}

function queueUiChecks() {
  const route = g.chatQueueEditRoute;
  const req = g.chatQueueEditRequest;
  const roomUser = { id: 4, content: '收斂worker', author: 'room-user:room-1', source_session_id: '' };
  const priv = { id: 9, content: '私人', author: '', source_session_id: '' };
  assert.equal(route(roomUser, false).kind, 'room');
  assert.equal(route(roomUser, false).roomId, 'room-1');
  assert.equal(route(priv, false).kind, 'private');
  assert.equal(route(roomUser, true), null);
  assert.equal(route({ ...roomUser, source_session_id: 'worker-1' }, false), null);
  assert.equal(route({ ...roomUser, batch_id: 'b' }, false), null);
  assert.equal(route({ ...roomUser, author: 'room-job:1' }, false), null);
  assert.equal(route({ ...roomUser, author: 'room-report:room-1' }, false), null);
  assert.equal(route({ ...roomUser, author: 'room-user:' }, false), null);
  assert.equal(route({ ...priv, id: 0 }, false), null);
  const http = req(route(roomUser, false), 'pm-1', '收斂worker', '新文');
  assert.equal(http.channel, 'http');
  assert.equal(http.url, '/rooms/room-1/messages/4');
  assert.deepEqual(plain(http.body), { expected_pm_id: 'pm-1', previous: '收斂worker', content: '新文' });
  const ws = req(route(priv, false), 'pm-1', '私人', '改');
  assert.equal(ws.channel, 'ws');
  assert.equal(ws.body.type, 'queue_edit');
  assert.equal(ws.body.id, 9);
  const chat = read('chat/ChatView.js');
  assert.equal(chat.includes('立刻執行會中斷目前執行中的動作。'), false);
  assert.equal(chat.includes('aria-describedby={helpId}'), false);
  assert.ok(chat.includes('chatQueueEditRoute'), '聊天室編輯要走來源判斷');
  const src = read('hooks/useRoomTimeline.js');
  const start = src.indexOf('function roomQueueManualOutcome');
  const end = src.indexOf('function useRoomQueue');
  assert.ok(start >= 0 && end > start, '重新同步還沒有可測的結果判斷');
  vm.runInContext(src.slice(start, end), ctx);
  const out = vm.runInContext('roomQueueManualOutcome', ctx);
  const outcome = (...args) => plain(out(...args));
  assert.deepEqual(outcome(0, 2, true), { notice: '', clearError: false, pending: 0, refreshing: false });
  assert.deepEqual(outcome(3, 3, true), { notice: '已重新同步', clearError: true, pending: 0, refreshing: false });
  assert.deepEqual(outcome(3, 4, true), { notice: '已重新同步', clearError: true, pending: 0, refreshing: false });
  assert.deepEqual(outcome(3, 3, false), { notice: '', clearError: false, pending: 3, refreshing: false });
  assert.equal(out(5, 4, true).refreshing, true);
  assert.equal(out(5, 4, false).clearError, false);
  const view = read('session/RoomView.js');
  const controls = read('session/RoomControls.js');
  assert.ok(view.includes('queueState.notice'), '成功同步要清掉舊批次錯誤');
  assert.ok(controls.includes('重新同步中') && controls.includes('正在重新同步佇列'), '按鈕要有等待與狀態文字');
  assert.ok(!src.includes('batchReady: true'), '不得猜批次能力為 true');
}

(async () => {
  queueEditChecks();
  batchHintChecks();
  queueUiChecks();
  await pollerScenarios();
  staticChecks();
  console.log('rooms checks passed');
})().catch((e) => { console.error(e); process.exit(1); });
