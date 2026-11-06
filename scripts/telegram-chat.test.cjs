const assert = require('node:assert');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const { test, afterEach } = require('node:test');
const mounted = new Set();
afterEach(() => { for (const rt of mounted) rt.unmount(); mounted.clear(); });

// Telegram 時間線：純函式合併 + 以假 React／WebSocket／fetch 驅動真正的 useChatSocket。
// 不使用任何測試框架或新依賴；不連任何真實 Telegram。

const root = path.join(__dirname, '../internal/static/js');
const read = (p) => fs.readFileSync(path.join(root, p), 'utf8').replace(/\r\n/g, '\n');

function sliceFunction(source, name) {
  const start = source.indexOf(`function ${name}(`);
  assert.ok(start >= 0, `找不到 ${name}`);
  const end = source.indexOf('\n}\n', start);
  return source.slice(start, end + 3);
}

const clone = (x) => JSON.parse(JSON.stringify(x));
// vm 內建立的陣列與本 realm 的 Array 原型不同；比較前轉成本 realm 的陣列。
const arr = (x) => Array.from(x);
const tick = () => new Promise((r) => setImmediate(r));
const pad = (n) => String(n).padStart(2, '0');
const at = (n) => `2026-10-19 08:00:${pad(n)}`;
const ref = (remote, account = '1', peer = '101') => ({ account_id: account, peer_id: peer, message_id: remote });
// 伺服器 DTO（HTTP／WS 共用）
const srv = (id, remote, content, sec, extra = {}) => ({
  id, role: 'claude', content, status: 'done', created_at: at(sec), author: 'Alice',
  ...(remote ? { telegram: ref(remote) } : {}), ...extra,
});

function loadContext() {
  const ctx = vm.createContext({
    console,
    parseMarkdown: (s) => s,
    parseShellExitCode: () => undefined,
    normalizePermMode: (_a, m) => m,
    readInputModeForSession: () => 'agent',
    showToast: () => {},
    wsURL: (id) => `ws://test/${id}`,
    WEB_SESSION_STORAGE_KEY: 'k',
    isTelegram: false,
    localStorage: { getItem: () => null, setItem: () => {} },
    requestAnimationFrame: (fn) => fn(),
    document: Object.assign(new EventTarget(), { hidden: false }),
    window: new EventTarget(),
    AbortController,
    setTimeout, clearTimeout,
  });
  vm.runInContext(read('hooks/telegramTimeline.js'), ctx);
  vm.runInContext(sliceFunction(read('ui/ui-atoms.js'), 'mapMessageRow'), ctx);
  return ctx;
}

// ── 純函式 ───────────────────────────────────────────────────────────────

test('mapMessageRow 保留 telegram／delivery／client_message_id／attachments', () => {
  const ctx = loadContext();
  const row = ctx.mapMessageRow(srv(7, 900, 'hi', 1, {
    role: 'user', author: '', delivery: 'sent', client_message_id: '7', replaces_client_message_id: '6',
    attachments: [{ id: 'a1', name: 'f.txt' }],
  }));
  assert.deepStrictEqual(clone({
    telegram: row.telegram, delivery: row.delivery, clientMessageId: row.clientMessageId,
    replacesClientMessageId: row.replacesClientMessageId, attachments: row.attachments,
  }), {
    telegram: ref(900), delivery: 'sent', clientMessageId: '7', replacesClientMessageId: '6', attachments: [{ id: 'a1', name: 'f.txt' }],
  });
});

test('合併：依 created_at、Telegram message_id、本機 ID 排序；同內容不同遠端 ID 兩者都保留；重播只有一個', () => {
  const ctx = loadContext();
  const m = (o) => ctx.mapMessageRow(o);
  let list = [];
  list = ctx.tgMergeList(list, [m(srv(3, 902, '同文字', 5)), m(srv(1, 901, '同文字', 5)), m(srv(2, 899, '早', 4))]);
  assert.deepStrictEqual(arr(list).map((x) => x.telegram.message_id), [899, 901, 902]);
  // 時間較早但遠端 ID 較大、與同一秒的排序：以時間為先。
  list = ctx.tgMergeList(list, [m(srv(9, 950, '晚匯入但早發生', 1))]);
  assert.deepStrictEqual(arr(list).map((x) => x.telegram.message_id), [950, 899, 901, 902]);
  // HTTP／sync／live 重播同一遠端識別：仍只有一個，且不改變其他項目。
  const again = ctx.tgMergeList(list, [m(srv(3, 902, '同文字', 5)), m(srv(1, 901, '同文字', 5))]);
  assert.strictEqual(again.length, 4);
  assert.strictEqual(again.filter((x) => x.content === '同文字').length, 2);
});

test('待傳以 client_message_id 取代：同本機 ID 合併；回音先到的 alias 由 replaces 移除；不降級 delivery', () => {
  const ctx = loadContext();
  const m = (o) => ctx.mapMessageRow(o);
  let list = ctx.tgUpsert([], m(srv(50, null, '你好', 9, { role: 'user', author: '', delivery: 'pending', client_message_id: '50' })));
  assert.strictEqual(list.length, 1);
  assert.strictEqual(list[0].delivery, 'pending');

  // 回音先到：不同本機 ID 的 alias 先被廣播，暫時出現兩個。
  list = ctx.tgUpsert(list, m(srv(51, 902, '你好', 10, { role: 'user', author: '', delivery: 'sent' })));
  assert.strictEqual(list.length, 2);
  // 取代事件：canonical 保留本機 ID 50，並移除 alias 51。
  list = ctx.tgUpsert(list, m(srv(50, 902, '你好', 10, { role: 'user', author: '', delivery: 'sent', replaces_client_message_id: '51' })));
  assert.deepStrictEqual(clone(arr(list).map((x) => [x.id, x.telegram && x.telegram.message_id, x.delivery])), [[50, 902, 'sent']]);

  // 過期事件不得把 sent 降級回 pending／failed。
  list = ctx.tgUpsert(list, m(srv(50, null, '你好', 9, { role: 'user', author: '', delivery: 'pending', client_message_id: '50' })));
  list = ctx.tgApplyDelivery(list, { clientMessageId: '50', delivery: 'failed', error: 'late' });
  assert.strictEqual(list.length, 1);
  assert.strictEqual(list[0].delivery, 'sent');
  assert.strictEqual(list[0].telegram.message_id, 902);
});

test('replaces_client_message_id 獨立於遠端識別與本機 ID 也能取代待傳項目；有遠端識別的 delivery 不降級', () => {
  const ctx = loadContext();
  const m = (o) => ctx.mapMessageRow(o);
  // canonical 的本機 ID 與待傳項目不同、也沒有先前的遠端識別可比對：只能靠 replaces 取代。
  let list = ctx.tgUpsert([], m(srv(60, null, '待傳', 9, { role: 'user', author: '', delivery: 'pending', client_message_id: '60' })));
  list = ctx.tgUpsert(list, m(srv(99, 905, '待傳', 10, { role: 'user', author: '', delivery: 'sent', replaces_client_message_id: '60' })));
  assert.deepStrictEqual(clone(arr(list).map((x) => [x.id, x.telegram.message_id])), [[99, 905]]);
  // 同一遠端識別的過期列（delivery 較舊）不得降級已 sent 的項目。
  list = ctx.tgUpsert(list, m(srv(99, 905, '待傳', 10, { role: 'user', author: '', delivery: 'pending' })));
  assert.strictEqual(list[0].delivery, 'sent');
  assert.strictEqual(list.length, 1);
});
test('late alias 競態：reconcile 後 canonical 本機 ID 持續存在；遲到的 alias 不復活、遲到的 pending 不重複', () => {
  const ctx = loadContext();
  const m = (o) => ctx.mapMessageRow(o);
  const canonical = () => m(srv(50, 902, '你好', 10, { role: 'user', author: '', delivery: 'sent', replaces_client_message_id: '51' }));
  const view = (l) => clone(arr(l).map((x) => [x.id, x.telegram ? x.telegram.message_id : null, x.delivery]));

  // 取代事件先到（alias 51 從未載入過）。
  let list = ctx.tgUpsert([], canonical());
  assert.deepStrictEqual(view(list), [[50, 902, 'sent']]);
  // 先前已發出的 HTTP／page 含 alias 51，晚於 reconcile 事件返回：不得覆寫 canonical 的本機 ID。
  list = ctx.tgUpsert(list, m(srv(51, 902, '你好', 10, { role: 'user', author: '', delivery: 'sent' })));
  assert.deepStrictEqual(view(list), [[50, 902, 'sent']]);
  // 過期的 pending user 事件／replay：不重複、不降級。
  list = ctx.tgUpsert(list, m({ id: 50, role: 'user', content: '你好', status: 'done', created_at: at(10), delivery: 'pending', client_message_id: '50' }));
  assert.deepStrictEqual(view(list), [[50, 902, 'sent']]);
  // 以批次（sync／HTTP 頁）形式重播 alias 與 pending 也一樣。
  list = ctx.tgMergeList(list, [m(srv(51, 902, '你好', 10, { role: 'user', author: '', delivery: 'sent' })), m({ id: 50, role: 'user', content: '你好', status: 'done', created_at: at(10), delivery: 'pending', client_message_id: '50' })]);
  assert.deepStrictEqual(view(list), [[50, 902, 'sent']]);
  // 取代事件本身重播仍冪等。
  list = ctx.tgUpsert(list, canonical());
  assert.deepStrictEqual(view(list), [[50, 902, 'sent']]);
});

test('late alias：只靠穩定識別，不靠文字或時間；同文字不同遠端 ID 不被誤刪', () => {
  const ctx = loadContext();
  const m = (o) => ctx.mapMessageRow(o);
  let list = ctx.tgMergeList([], [
    m(srv(1, 901, '同文字', 5, { role: 'user', author: '' })),
    m(srv(2, 902, '同文字', 5, { role: 'user', author: '' })),
    m(srv(60, null, '同文字', 5, { role: 'user', author: '', delivery: 'pending', client_message_id: '60' })),
  ]);
  // alias 61 先出現（回音），再由 canonical 60（遠端 903）取代；同文字的 901／902 必須保留。
  list = ctx.tgUpsert(list, m(srv(61, 903, '同文字', 5, { role: 'user', author: '', delivery: 'sent' })));
  list = ctx.tgUpsert(list, m(srv(60, 903, '同文字', 5, { role: 'user', author: '', delivery: 'sent', replaces_client_message_id: '61' })));
  const ids = () => clone(arr(list).map((x) => [x.id, x.telegram ? x.telegram.message_id : null]));
  assert.deepStrictEqual(ids(), [[1, 901], [2, 902], [60, 903]]);
  // 遲到的 alias 61 不復活，其他同文字項目不受影響。
  list = ctx.tgUpsert(list, m(srv(61, 903, '同文字', 5, { role: 'user', author: '', delivery: 'sent' })));
  assert.deepStrictEqual(ids(), [[1, 901], [2, 902], [60, 903]]);
  // 新的遠端訊息照常加入。
  list = ctx.tgUpsert(list, m(srv(70, 904, '同文字', 6, { role: 'user', author: '' })));
  assert.deepStrictEqual(ids(), [[1, 901], [2, 902], [60, 903], [70, 904]]);
});
test('送達事件：錯誤呈現在原待傳項目；找不到項目時不新增', () => {
  const ctx = loadContext();
  const m = (o) => ctx.mapMessageRow(o);
  let list = ctx.tgUpsert([], m(srv(60, null, 'x', 1, { role: 'user', author: '', delivery: 'pending', client_message_id: '60' })));
  list = ctx.tgApplyDelivery(list, { clientMessageId: '60', delivery: 'unknown', error: '結果不明' });
  assert.deepStrictEqual(clone([list[0].delivery, list[0].deliveryError, list.length]), ['unknown', '結果不明', 1]);
  list = ctx.tgApplyDelivery(list, { clientMessageId: '999', delivery: 'failed', error: 'x' });
  assert.strictEqual(list.length, 1);
  assert.strictEqual(list[0].delivery, 'unknown');
});

test('sync：不覆蓋已載入舊頁；離線（無 scope）保留；改綁後丟棄舊 account/peer 與舊待傳', () => {
  const ctx = loadContext();
  const m = (o) => ctx.mapMessageRow(o);
  const scopeA = { account_id: '1', peer_id: '101' };
  const keyA = ctx.tgScopeKey(scopeA);
  // 已載入 1..6（含舊頁），sync 只帶最新 5..8。
  let list = ctx.tgMergeList([], [1, 2, 3, 4, 5, 6].map((i) => m(srv(i, 900 + i, `m${i}`, i))));
  list = ctx.tgApplySync(list, [5, 6, 7, 8].map((i) => m(srv(i, 900 + i, `m${i}`, i))), scopeA, '');
  assert.deepStrictEqual(arr(list).map((x) => x.telegram.message_id), [901, 902, 903, 904, 905, 906, 907, 908]);

  // 離線：沒有 scope 也沒有資料，保留已有資料。
  const offline = ctx.tgApplySync(list, [], null, keyA);
  assert.strictEqual(offline.length, 8);

  // 同 scope 的待傳不被 sync 丟棄；改綁到另一個 peer：舊 peer 的列與舊待傳都不帶進新聊天。
  const withPending = ctx.tgUpsert(list, m(srv(70, null, 'pending', 20, { role: 'user', author: '', delivery: 'pending', client_message_id: '70' })));
  const same = ctx.tgApplySync(withPending, [m(srv(8, 908, 'm8', 8))], scopeA, keyA);
  assert.strictEqual(same.length, 9);
  const scopeB = { account_id: '1', peer_id: '202' };
  const b = ctx.tgApplySync(withPending, [m(srv(30, 3001, 'b1', 30, { telegram: ref(3001, '1', '202') }))], scopeB, keyA);
  assert.deepStrictEqual(clone(arr(b).map((x) => x.content)), ['b1']);
});

// ── 以真正的 useChatSocket 驅動 ────────────────────────────────────────────

function createHarness() {
  const ctx = loadContext();
  const sockets = [];
  const fetches = [];
  class FakeWS {
    static CONNECTING = 0;
    static CLOSED = 3;
    static CLOSING = 2;
    constructor(url) { this.url = url; this.readyState = 0; sockets.push(this); }
    send() {}
    close() {
      if (this.readyState === 3) return;
      this.closed = true; this.readyState = 3;
      this.onclose?.({ code: 1000, reason: 'test cleanup' });
    }
    open() { this.readyState = 1; if (this.onopen) this.onopen(); }
    emit(o) { this.onmessage({ data: JSON.stringify(o) }); }
  }
  ctx.WebSocket = FakeWS;
  // 每個 apiFetch 都是可手動回應的 deferred，才能模擬 HTTP 與 WS 的競態與過期回應。
  ctx.apiFetch = (p, opts = {}) => new Promise((resolve, reject) => {
    fetches.push({
      path: p,
      signal: opts.signal,
      respond: (data, status = 200, headers = {}) => resolve({ ok: status < 400, status, headers: { get: (k) => (k in headers ? headers[k] : null) }, json: async () => data }),
      fail: (err) => reject(err),
    });
    if (opts.signal) opts.signal.addEventListener('abort', () => reject(Object.assign(new Error('aborted'), { name: 'AbortError' })));
  });
  // 可控時鐘：429 的重試延遲用它推進。
  let nowMs = 1000000;
  ctx.Date = class FakeDate extends Date { static now() { return nowMs; } };
  vm.runInContext(read('hooks/useChatSocket.js'), ctx);

  function mount(props) {
    const hooks = [];
    let idx = 0;
    let queue = [];
    let busy = false;
    let dirty = false;
    let out;
    const rt = { props };
    // 每個實例（窗格）有自己的 hook 狀態；render 前把全域 hook 綁到本實例，setState／setProps 同樣經過這裡。
    const api = {
      useState: (init) => {
        const h = hooks[idx] || (hooks[idx] = { v: typeof init === 'function' ? init() : init });
        idx++;
        return [h.v, (u) => {
          const nv = typeof u === 'function' ? u(h.v) : u;
          if (!Object.is(nv, h.v)) { h.v = nv; render(); }
        }];
      },
      useRef: (init) => { const h = hooks[idx] || (hooks[idx] = { current: init }); idx++; return h; },
      useCallback: (fn) => fn,
      useEffect: (fn, deps) => {
        const h = hooks[idx] || (hooks[idx] = { deps: undefined, cleanup: null });
        idx++;
        const changed = !deps || !h.deps || deps.length !== h.deps.length || deps.some((d, i) => !Object.is(d, h.deps[i]));
        if (changed) queue.push(() => { if (h.cleanup) h.cleanup(); h.cleanup = fn() || null; h.deps = deps; });
      },
    };
    function render() {
      if (busy) { dirty = true; return; }
      busy = true;
      try {
        do {
          dirty = false; idx = 0; queue = [];
          Object.assign(ctx, api);
          out = ctx.useChatSocket(rt.props);
          const run = queue; queue = [];
          for (const fn of run) fn();
        } while (dirty);
      } finally { busy = false; }
    }
    rt.render = render;
    rt.get = () => out;
    rt.setProps = (p) => { rt.props = p; render(); };
    rt.unmount = () => hooks.forEach((h) => h && h.cleanup && h.cleanup());
    mounted.add(rt);
    return rt;
  }  return { ctx, sockets, fetches, mount, advance: (ms) => { nowMs += ms; } };
}

const tgProps = (id) => ({
  session: { id, permission_mode: 'default', input_mode: 'agent' },
  agentType: 'telegram', showPermModeSelect: false, showEffortSelect: false, guest: null, onHandoffChange: null,
});

const view = (rt) => clone(rt.get().messages.map((m) => [m.id, m.telegram ? m.telegram.message_id : null, m.content, m.delivery || '']));

test('HTTP 初載使用 pagination=telegram limit=50，保留游標；之後的 sync 與 live 合併而不覆蓋已載入頁', async () => {
  const h = createHarness();
  const rt = h.mount(tgProps('s1'));
  rt.render();
  await tick();
  assert.strictEqual(h.fetches.length, 1);
  assert.strictEqual(h.fetches[0].path, '/sessions/s1/messages?pagination=telegram&limit=50');

  h.fetches[0].respond({
    messages: [srv(1, 901, 'm1', 1), srv(2, 902, 'm2', 2), srv(3, 903, 'm3', 3)],
    next_cursor: 'cur-1', has_more: true, sync_state: 'ready',
  });
  await tick();
  assert.deepStrictEqual(view(rt).map((x) => x[1]), [901, 902, 903]);
  assert.deepStrictEqual(clone(rt.get().tgPaging), { nextCursor: 'cur-1', hasMore: true, syncState: 'ready', loading: false, error: null, needsInit: false, retryAt: 0 });

  // WS 連上後的 sync 只帶最新有界頁（2..4）：舊的 901 不得消失。
  assert.strictEqual(h.sockets.length, 1);
  h.sockets[0].open();
  h.sockets[0].emit({
    type: 'sync', value: 'IDLE', messages: [srv(2, 902, 'm2', 2), srv(3, 903, 'm3', 3), srv(4, 904, 'm4', 4)],
    sync_state: 'ready', telegram_scope: { account_id: '1', peer_id: '101' },
  });
  assert.deepStrictEqual(view(rt).map((x) => x[1]), [901, 902, 903, 904]);

  // live：新訊息 905；重播同一則與同內容不同遠端 ID 都正確處理。
  const live = (id, remote, text, sec) => h.sockets[0].emit({ type: 'message', ...srv(id, remote, text, sec) });
  live(5, 905, '同文字', 5);
  live(5, 905, '同文字', 5);
  live(6, 906, '同文字', 5);
  assert.deepStrictEqual(view(rt).map((x) => x[1]), [901, 902, 903, 904, 905, 906]);
  assert.strictEqual(arr(rt.get().messages).filter((m) => m.content === '同文字').length, 2);
});

test('HTTP 與 WS 競態：sync 先到、HTTP 後到（舊頁）仍合併，不覆蓋 live', async () => {
  const h = createHarness();
  const rt = h.mount(tgProps('s1'));
  rt.render();
  await tick();
  // 模擬 sync／live 已先寫入（例如重連），HTTP 才回來。
  h.fetches[0].respond({ messages: [srv(1, 901, 'old', 1)], next_cursor: 'c', has_more: true, sync_state: 'ready' });
  await tick();
  h.sockets[0].open();
  h.sockets[0].emit({ type: 'sync', value: 'IDLE', messages: [srv(9, 909, 'new', 9)], sync_state: 'ready', telegram_scope: { account_id: '1', peer_id: '101' } });
  h.sockets[0].emit({ type: 'message', ...srv(10, 910, 'live', 10) });
  assert.deepStrictEqual(view(rt).map((x) => x[1]), [901, 909, 910]);
  // 重連：新的 sync 不覆蓋較舊已載入頁。
  h.sockets[0].emit({ type: 'sync', value: 'IDLE', messages: [srv(10, 910, 'live', 10)], sync_state: 'offline', telegram_scope: { account_id: '1', peer_id: '101' } });
  assert.deepStrictEqual(view(rt).map((x) => x[1]), [901, 909, 910]);
  assert.strictEqual(rt.get().tgPaging.syncState, 'offline');
});

test('待傳項目：user_message 建立；delivery 事件呈現錯誤；完整 upsert 取代；alias 被移除', async () => {
  const h = createHarness();
  const rt = h.mount(tgProps('s1'));
  rt.render();
  await tick();
  h.fetches[0].respond({ messages: [], next_cursor: null, has_more: false, sync_state: 'ready' });
  await tick();
  const ws = h.sockets[0];
  ws.open();
  ws.emit({ type: 'user_message', id: 50, content: '你好', created_at: at(20), client_message_id: '50', delivery: 'pending' });
  assert.deepStrictEqual(view(rt), [[50, null, '你好', 'pending']]);

  ws.emit({ type: 'delivery', client_message_id: '50', delivery: 'unknown', error: '結果不明' });
  const item = rt.get().messages[0];
  assert.deepStrictEqual(clone([item.delivery, item.deliveryError, rt.get().messages.length]), ['unknown', '結果不明', 1]);

  // 回音先到：alias 51 暫時出現；取代事件後只剩 canonical 50。
  ws.emit({ type: 'message', id: 51, role: 'user', content: '你好', created_at: at(21), telegram: ref(902), delivery: 'sent' });
  assert.strictEqual(rt.get().messages.length, 2);
  ws.emit({ type: 'message', id: 50, role: 'user', content: '你好', created_at: at(21), telegram: ref(902), delivery: 'sent', replaces_client_message_id: '51' });
  assert.deepStrictEqual(view(rt), [[50, 902, '你好', 'sent']]);
  assert.strictEqual(rt.get().messages[0].deliveryError || '', '');

  // 重複事件與過期的失敗事件都不改變結果。
  ws.emit({ type: 'message', id: 50, role: 'user', content: '你好', created_at: at(21), telegram: ref(902), delivery: 'sent', replaces_client_message_id: '51' });
  ws.emit({ type: 'delivery', client_message_id: '50', delivery: 'failed', error: 'late' });
  assert.deepStrictEqual(view(rt), [[50, 902, '你好', 'sent']]);
});

test('session generation：過期的 HTTP 回應不進新聊天；分割窗格各自獨立', async () => {
  const h = createHarness();
  const a = h.mount(tgProps('A'));
  a.render();
  await tick();
  const b = h.mount(tgProps('B')); // 另一個窗格（獨立 hook 實例）
  b.render();
  await tick();
  assert.deepStrictEqual(h.fetches.map((f) => f.path), [
    '/sessions/A/messages?pagination=telegram&limit=50',
    '/sessions/B/messages?pagination=telegram&limit=50',
  ]);

  // 窗格 A 切到 session C 後，A 的舊 HTTP 才回來。
  a.setProps(tgProps('C'));
  await tick();
  h.fetches[0].respond({ messages: [srv(1, 901, 'from-A', 1)], next_cursor: 'curA', has_more: true, sync_state: 'ready' });
  await tick();
  assert.deepStrictEqual(view(a), []);
  assert.strictEqual(a.get().tgPaging, null);

  // B 的回應只進 B；C 的回應只進 C（游標各自獨立）。
  h.fetches[1].respond({ messages: [srv(2, 902, 'from-B', 2)], next_cursor: 'curB', has_more: false, sync_state: 'ready' });
  await tick();
  const cFetch = h.fetches.find((f) => f.path.includes('/sessions/C/'));
  cFetch.respond({ messages: [srv(3, 903, 'from-C', 3)], next_cursor: 'curC', has_more: true, sync_state: 'ready' });
  await tick();
  assert.deepStrictEqual(view(b).map((x) => x[2]), ['from-B']);
  assert.deepStrictEqual(view(a).map((x) => x[2]), ['from-C']);
  assert.strictEqual(b.get().tgPaging.nextCursor, 'curB');
  assert.strictEqual(a.get().tgPaging.nextCursor, 'curC');
});

test('帳號切換：無 scope 的 sync 不清除也不回填；新 scope 資料保留，舊帳號的遲到事件被丟棄', async () => {
  const h = createHarness();
  const rt = h.mount(tgProps('s1'));
  rt.render();
  await tick();
  // 本機 HTTP 已由伺服器以目前帳號 2 / peer 101 過濾；WS 之後才連上（hook 在 HTTP 後才建立 WS）。
  h.fetches[0].respond({
    messages: [srv(1, 801, 'B-1', 1, { telegram: ref(801, '2', '101') })],
    next_cursor: 'c', has_more: true, sync_state: 'ready',
  });
  await tick();
  const ws = h.sockets[0];
  ws.open();
  // 伺服器暫時無法驗證對象（帳號剛切換）：空 scope、沒有資料。不得清掉已載入的 B-1。
  ws.emit({ type: 'sync', value: 'IDLE', messages: [], sync_state: 'offline' });
  assert.deepStrictEqual(view(rt).map((x) => x[2]), ['B-1']);
  // 之後 sync 已驗證 scope(2,101)：併入。
  ws.emit({ type: 'sync', value: 'IDLE', messages: [srv(2, 802, 'B-2', 2, { telegram: ref(802, '2', '101') })], sync_state: 'ready', telegram_scope: { account_id: '2', peer_id: '101' } });
  assert.deepStrictEqual(view(rt).map((x) => x[2]), ['B-1', 'B-2']);
  // 再來一次無 scope 的 sync（暫時無法驗證／離線）：仍不得清掉已載入的正確資料。
  ws.emit({ type: 'sync', value: 'IDLE', messages: [], sync_state: 'offline' });
  assert.deepStrictEqual(view(rt).map((x) => x[2]), ['B-1', 'B-2']);
  // 舊帳號 A 的遲到 live 事件不得回填。
  ws.emit({ type: 'message', ...srv(9, 701, 'A-late', 3, { telegram: ref(701, '1', '101') }) });
  assert.deepStrictEqual(view(rt).map((x) => x[2]), ['B-1', 'B-2']);
});

test('HTTP 載入了舊帳號資料後 sync 到新 scope：舊帳號項目與舊待傳一併丟棄；再改綁亦同', async () => {
  const h = createHarness();
  const rt = h.mount(tgProps('s1'));
  rt.render();
  await tick();
  // HTTP 在帳號切換前產生：回來的是舊帳號 A 的資料。
  h.fetches[0].respond({
    messages: [srv(1, 701, 'A-1', 1, { telegram: ref(701, '1', '101') })],
    next_cursor: 'c', has_more: false, sync_state: 'ready',
  });
  await tick();
  assert.deepStrictEqual(view(rt).map((x) => x[2]), ['A-1']);
  const ws = h.sockets[0];
  ws.open();
  ws.emit({ type: 'user_message', id: 70, content: 'old-pending', created_at: at(20), client_message_id: '70', delivery: 'pending' });
  // 第一次 sync 就是新帳號 B：A 的列與（來自 A 的）待傳都不得帶進新聊天。
  ws.emit({ type: 'sync', value: 'IDLE', messages: [srv(5, 805, 'B-5', 5, { telegram: ref(805, '2', '101') })], sync_state: 'ready', telegram_scope: { account_id: '2', peer_id: '101' } });
  assert.deepStrictEqual(view(rt).map((x) => x[2]), ['B-5']);
  // 再改綁到 peer 303：B 的列與待傳都被丟棄，只留新 scope。
  ws.emit({ type: 'user_message', id: 71, content: 'pending-B', created_at: at(21), client_message_id: '71', delivery: 'pending' });
  ws.emit({ type: 'sync', value: 'IDLE', messages: [srv(31, 3101, 'C-1', 31, { telegram: ref(3101, '2', '303') })], sync_state: 'ready', telegram_scope: { account_id: '2', peer_id: '303' } });
  assert.deepStrictEqual(view(rt).map((x) => x[2]), ['C-1']);
});
test('late alias 的 live／sync 競態（真正的 useChatSocket）：reconcile 後遲到的 alias 與 pending 重播不改變結果', async () => {
  const h = createHarness();
  const rt = h.mount(tgProps('s1'));
  rt.render();
  await tick();
  h.fetches[0].respond({ messages: [srv(1, 901, '同文字', 5, { role: 'user', author: '' })], next_cursor: null, has_more: false, sync_state: 'ready' });
  await tick();
  const ws = h.sockets[0];
  ws.open();
  const scope = { account_id: '1', peer_id: '101' };
  ws.emit({ type: 'user_message', id: 50, content: '同文字', created_at: at(9), client_message_id: '50', delivery: 'pending' });
  ws.emit({ type: 'message', id: 51, role: 'user', content: '同文字', created_at: at(10), telegram: ref(902), delivery: 'sent' }); // 回音先到的 alias
  ws.emit({ type: 'message', id: 50, role: 'user', content: '同文字', created_at: at(10), telegram: ref(902), delivery: 'sent', replaces_client_message_id: '51' });
  const settled = view(rt);
  assert.deepStrictEqual(settled.map((x) => [x[0], x[1]]), [[1, 901], [50, 902]]);
  // 遲到：alias 的重播、pending 的重播，以及 sync（重連）夾帶的過期 alias／pending。
  ws.emit({ type: 'message', id: 51, role: 'user', content: '同文字', created_at: at(10), telegram: ref(902), delivery: 'sent' });
  ws.emit({ type: 'user_message', id: 50, content: '同文字', created_at: at(9), client_message_id: '50', delivery: 'pending' });
  ws.emit({ type: 'sync', value: 'IDLE', sync_state: 'ready', telegram_scope: scope, messages: [
    srv(51, 902, '同文字', 10, { role: 'user', author: '', delivery: 'sent' }),
    { id: 50, role: 'user', content: '同文字', status: 'done', created_at: at(9), delivery: 'pending', client_message_id: '50' },
  ] });
  assert.deepStrictEqual(view(rt), settled);
});
// ── 較早訊息按需載入（task 2.4） ─────────────────────────────────────────
const OLDER = '/sessions/s1/messages?pagination=telegram&limit=50';

async function startTG(h, id = 's1', page = { messages: [srv(5, 905, 'm5', 5), srv(6, 906, 'm6', 6)], next_cursor: 'cur-1', has_more: true, sync_state: 'ready' }) {
  const rt = h.mount(tgProps(id));
  rt.render();
  await tick();
  const f = h.fetches.find((x) => x.path.includes(`/sessions/${id}/`) && !x.used);
  f.used = true;
  f.respond(page);
  await tick();
  return rt;
}

test('loadOlder：每次至多一頁、單頁 inflight 去重；與 live 交錯後已載入頁與 live 都保留；到頂即停', async () => {
  const h = createHarness();
  const rt = await startTG(h);
  const ws = h.sockets[0];
  ws.open();
  const pg = () => h.fetches.filter((x) => x.path.includes('pagination=telegram')); // 排除 markRead 的 PATCH
  rt.get().loadOlder();
  rt.get().loadOlder();
  rt.get().loadOlder(); // sentinel／按鈕連續觸發
  assert.strictEqual(pg().length, 2);
  assert.strictEqual(pg()[1].path, `${OLDER}&before=cur-1`);
  assert.strictEqual(rt.get().tgPaging.loading, true);
  ws.emit({ type: 'message', ...srv(7, 907, 'live7', 7) }); // 回應前先到的 live
  pg()[1].respond({ messages: [srv(3, 903, 'm3', 3), srv(4, 904, 'm4', 4)], next_cursor: 'cur-2', has_more: true, sync_state: 'ready' });
  await tick();
  assert.deepStrictEqual(view(rt).map((x) => x[1]), [903, 904, 905, 906, 907]);
  assert.deepStrictEqual(clone([rt.get().tgPaging.nextCursor, rt.get().tgPaging.hasMore, rt.get().tgPaging.loading, rt.get().tgPaging.error]), ['cur-2', true, false, null]);
  // 有 has_more 也不自動連抓：只有使用者再次要求才取下一頁。
  await tick();
  assert.strictEqual(pg().length, 2);
  rt.get().loadOlder();
  pg()[2].respond({ messages: [srv(1, 901, 'm1', 1)], next_cursor: null, has_more: false, sync_state: 'ready' });
  await tick();
  assert.deepStrictEqual(view(rt).map((x) => x[1]), [901, 903, 904, 905, 906, 907]);
  assert.strictEqual(rt.get().tgPaging.hasMore, false);
  rt.get().loadOlder();
  assert.strictEqual(pg().length, 3, '到頂後不得再請求');
});

test('空可見頁推進游標但不自動連抓；錯誤可重試且不變成到頂；429 尊重延遲；400 重新初始化', async () => {
  const h = createHarness();
  const rt = await startTG(h);
  const paging = () => clone(rt.get().tgPaging);
  rt.get().loadOlder();
  h.fetches[1].respond({ messages: [], next_cursor: 'cur-2', has_more: true, sync_state: 'ready' });
  await tick();
  assert.strictEqual(h.fetches.length, 2, '空可見頁後不得自動再抓');
  assert.deepStrictEqual([paging().nextCursor, paging().hasMore], ['cur-2', true]);

  // 503：顯示可重試的錯誤，游標與 has_more 不變。
  rt.get().loadOlder();
  h.fetches[2].respond({ error: 'x', code: 'telegram_unavailable' }, 503);
  await tick();
  assert.deepStrictEqual([paging().nextCursor, paging().hasMore, paging().loading], ['cur-2', true, false]);
  assert.ok(paging().error && paging().error.message, '需有可讀錯誤訊息');
  assert.strictEqual(paging().needsInit, false);
  // 重試用同一個游標。
  rt.get().loadOlder();
  assert.strictEqual(h.fetches[3].path, `${OLDER}&before=cur-2`);

  // 429：記下延遲，延遲內不發請求；時間到才可重試。
  h.fetches[3].respond({ error: 'x', code: 'telegram_rate_limited', retry_after_seconds: 7 }, 429, { 'Retry-After': '7' });
  await tick();
  assert.strictEqual(paging().hasMore, true);
  assert.strictEqual(paging().retryAt, 1000000 + 7000);
  rt.get().loadOlder();
  assert.strictEqual(h.fetches.length, 4, '延遲內不得請求');
  h.advance(7001);
  rt.get().loadOlder();
  assert.strictEqual(h.fetches.length, 5);

  // 400：游標失效 → 改為重新取最新頁（不帶 before），不是到頂。
  h.fetches[4].respond({ error: 'x', code: 'invalid_cursor' }, 400);
  await tick();
  assert.deepStrictEqual([paging().needsInit, paging().hasMore === false], [true, false]);
  rt.get().loadOlder();
  assert.strictEqual(h.fetches[5].path, OLDER);
  h.fetches[5].respond({ messages: [srv(6, 906, 'm6', 6)], next_cursor: 'cur-9', has_more: true, sync_state: 'ready' });
  await tick();
  assert.deepStrictEqual([paging().needsInit, paging().nextCursor, paging().error], [false, 'cur-9', null]);
});

test('初次 HTTP 失敗有明確重試入口（含網路錯誤）；重試成功後才有游標', async () => {
  const h = createHarness();
  const rt = h.mount(tgProps('s1'));
  rt.render();
  await tick();
  h.fetches[0].fail(new Error('network down'));
  await tick();
  const p = clone(rt.get().tgPaging);
  assert.deepStrictEqual([p.needsInit, p.hasMore, p.loading, !!p.error], [true, null, false, true]);
  assert.strictEqual(rt.get().histLoaded, true);
  rt.get().loadOlder();
  assert.strictEqual(h.fetches[1].path, OLDER);
  h.fetches[1].respond({ error: 'x' }, 500);
  await tick();
  assert.strictEqual(rt.get().tgPaging.needsInit, true);
  rt.get().loadOlder();
  h.fetches[2].respond({ messages: [srv(2, 902, 'm2', 2)], next_cursor: 'cur-1', has_more: true, sync_state: 'ready' });
  await tick();
  assert.deepStrictEqual(clone([rt.get().tgPaging.needsInit, rt.get().tgPaging.nextCursor, view(rt).length]), [false, 'cur-1', 1]);
});

test('切換 session 取消請求且遲到回應不進新聊天；分割窗格的游標與 loading 互不污染', async () => {
  const h = createHarness();
  const a = await startTG(h, 'A', { messages: [srv(1, 901, 'A1', 1)], next_cursor: 'curA', has_more: true, sync_state: 'ready' });
  const b = await startTG(h, 'B', { messages: [srv(2, 902, 'B1', 2)], next_cursor: 'curB', has_more: true, sync_state: 'ready' });
  a.get().loadOlder();
  const pendingA = h.fetches[h.fetches.length - 1];
  assert.strictEqual(pendingA.path, '/sessions/A/messages?pagination=telegram&limit=50&before=curA');
  assert.strictEqual(a.get().tgPaging.loading, true);
  assert.strictEqual(b.get().tgPaging.loading, false);
  assert.strictEqual(b.get().tgPaging.nextCursor, 'curB');

  a.setProps(tgProps('C')); // 窗格 A 換成 session C
  await tick();
  assert.strictEqual(pendingA.signal.aborted, true);
  pendingA.respond({ messages: [srv(0, 900, 'late-A', 0)], next_cursor: 'zzz', has_more: true, sync_state: 'ready' });
  await tick();
  assert.deepStrictEqual(view(a), []);
  assert.strictEqual(a.get().tgPaging, null);
  assert.strictEqual(b.get().tgPaging.loading, false);

  const cInit = h.fetches.find((x) => x.path.includes('/sessions/C/'));
  cInit.respond({ messages: [srv(3, 903, 'C1', 3)], next_cursor: 'curC', has_more: true, sync_state: 'ready' });
  await tick();
  a.get().loadOlder();
  assert.ok(h.fetches[h.fetches.length - 1].path.endsWith('&before=curC'));
  assert.strictEqual(b.get().tgPaging.nextCursor, 'curB');
  assert.strictEqual(b.get().tgPaging.loading, false);
});

test('scope 變更（帳號／對象）中止請求並重置游標：不向新 scope 送舊 before，舊回應被丟棄', async () => {
  const h = createHarness();
  const rt = await startTG(h, 's1', { messages: [srv(5, 905, 'A5', 5, { telegram: ref(905, '1', '101') })], next_cursor: 'curA', has_more: true, sync_state: 'ready' });
  const ws = h.sockets[0];
  ws.open();
  ws.emit({ type: 'sync', value: 'IDLE', messages: [], sync_state: 'ready', telegram_scope: { account_id: '1', peer_id: '101' } });
  rt.get().loadOlder();
  const inflight = h.fetches[h.fetches.length - 1];
  assert.ok(inflight.path.endsWith('&before=curA'));
  // 登入切到帳號 2：sync 帶新 scope。
  ws.emit({ type: 'sync', value: 'IDLE', messages: [srv(8, 808, 'B8', 8, { telegram: ref(808, '2', '101') })], sync_state: 'ready', telegram_scope: { account_id: '2', peer_id: '101' } });
  assert.strictEqual(inflight.signal.aborted, true);
  const p = clone(rt.get().tgPaging);
  assert.deepStrictEqual([p.needsInit, p.nextCursor, p.loading], [true, null, false]);
  inflight.respond({ messages: [srv(4, 704, 'A4', 4, { telegram: ref(704, '1', '101') })], next_cursor: 'old', has_more: true, sync_state: 'ready' });
  await tick();
  assert.deepStrictEqual(view(rt).map((x) => x[2]), ['B8']);
  // 下一次載入從新 scope 的最新頁重新開始，不帶舊 before。
  rt.get().loadOlder();
  assert.strictEqual(h.fetches[h.fetches.length - 1].path, OLDER);
});
test('telegram_scope 事件：不必重連就套用新 scope（中止請求、重置舊游標、丟棄舊帳號資料）並更新 sync_state', async () => {
  const h = createHarness();
  const rt = await startTG(h, 's1', { messages: [srv(5, 905, 'A5', 5, { telegram: ref(905, '1', '101') })], next_cursor: 'curA', has_more: true, sync_state: 'ready' });
  const ws = h.sockets[0];
  ws.open();
  ws.emit({ type: 'sync', value: 'IDLE', messages: [], sync_state: 'ready', telegram_scope: { account_id: '1', peer_id: '101' } });
  rt.get().loadOlder();
  const inflight = h.fetches[h.fetches.length - 1];
  assert.ok(inflight.path.endsWith('&before=curA'));

  // 登入切到帳號 2：伺服器驗證出新 scope 後主動推送（補漏中）。
  ws.emit({ type: 'telegram_scope', sync_state: 'catching_up', telegram_scope: { account_id: '2', peer_id: '101' }, messages: [srv(8, 808, 'B8', 8, { telegram: ref(808, '2', '101') })] });
  assert.strictEqual(inflight.signal.aborted, true);
  const p = clone(rt.get().tgPaging);
  assert.deepStrictEqual([p.needsInit, p.nextCursor, p.loading, p.syncState], [true, null, false, 'catching_up']);
  assert.deepStrictEqual(view(rt).map((x) => x[2]), ['B8']);
  // 之後帳號 2 的 live 被接受、舊帳號 1 的遲到事件被丟棄（不必等重連重送 sync）。
  ws.emit({ type: 'message', ...srv(9, 809, 'B9', 9, { telegram: ref(809, '2', '101') }) });
  ws.emit({ type: 'message', ...srv(7, 707, 'A-late', 7, { telegram: ref(707, '1', '101') }) });
  assert.deepStrictEqual(view(rt).map((x) => x[2]), ['B8', 'B9']);

  // 斷線／暫時無法驗證：只帶狀態，不清除已載入的資料。
  ws.emit({ type: 'telegram_scope', sync_state: 'offline', messages: [] });
  assert.deepStrictEqual(view(rt).map((x) => x[2]), ['B8', 'B9']);
  assert.strictEqual(rt.get().tgPaging.syncState, 'offline');
  ws.emit({ type: 'telegram_scope', sync_state: 'ready', telegram_scope: { account_id: '2', peer_id: '101' }, messages: [srv(9, 809, 'B9', 9, { telegram: ref(809, '2', '101') })] });
  assert.strictEqual(rt.get().tgPaging.syncState, 'ready');
  assert.deepStrictEqual(view(rt).map((x) => x[2]), ['B8', 'B9']);
});
test('非 Telegram：只用 WS 初載，較早頁與 live／重連合併且不重複', async () => {
  const h = createHarness();
  const rt = h.mount({ ...tgProps('s9'), agentType: 'claude' });
  rt.render();
  assert.strictEqual(h.fetches.length, 0);
  h.sockets[0].open();
  const row = id => ({ id, role: 'user', content: `m${id}`, status: 'done', created_at: at(1) });
  h.sockets[0].emit({ type: 'sync', value: 'IDLE', history_has_more: true, messages: [row(3), row(4)] });
  assert.strictEqual(rt.get().histLoaded, true);
  const pending = rt.get().loadOlder();
  rt.get().loadOlder();
  assert.strictEqual(h.fetches.length, 2); // mark-read PATCH + 一個補頁請求
  assert.strictEqual(h.fetches[1].path, '/sessions/s9/messages?limit=100&before=3');
  h.sockets[0].emit({ type: 'user_message', ...row(5) });
  h.fetches[1].respond([row(1), row(2)]);
  await pending;
  h.sockets[0].emit({ type: 'sync', value: 'IDLE', history_has_more: true, messages: [row(4), row(5), row(6)] });
  assert.deepStrictEqual(arr(rt.get().messages).map(m => m.id), [1, 2, 3, 4, 5, 6]);
  h.sockets[0].emit({ type: 'sync', value: 'IDLE', history_has_more: true, messages: [row(100), row(101)] });
  assert.deepStrictEqual(arr(rt.get().messages).map(m => m.id), [100, 101]);
  assert.strictEqual(rt.get().historyPaging.nextCursor, 100);
  rt.unmount();
});

test('非 Telegram：設定刷新不重置訊息與連線', async () => {
  const h = createHarness();
  const props = { ...tgProps('fast'), agentType: 'claude' };
  const rt = h.mount(props);
  rt.render();
  assert.strictEqual(h.sockets.length, 1);
  h.sockets[0].open();
  h.sockets[0].emit({ type: 'sync', value: 'IDLE', messages: [{ id: 2, role: 'user', content: 'new', status: 'done', created_at: at(2) }] });
  await tick();
  rt.setProps({ ...props, session: { ...props.session, model: 'updated' } });
  assert.strictEqual(rt.get().connected, true);
  assert.strictEqual(rt.get().modelSel, 'updated');
  assert.deepStrictEqual(arr(rt.get().messages).map((m) => m.id), [2]);
  assert.strictEqual(h.sockets.length, 1);
  rt.unmount();
});

test('初連失敗且 HTTP 探測也斷線，仍安排重連', async () => {
  const h = createHarness();
  const timers = new Map();
  let next = 0;
  h.ctx.setTimeout = (fn, ms) => { const id = ++next; timers.set(id, { fn, ms }); return id; };
  h.ctx.clearTimeout = id => timers.delete(id);
  h.ctx.localStorage.getItem = () => 'token';
  const rt = h.mount({ ...tgProps('offline'), agentType: 'claude' });
  rt.render();
  h.sockets[0].onclose({ code: 1006 });
  h.fetches[0].fail(new Error('offline'));
  await tick();
  const retry = [...timers.values()].find(t => t.ms === 400);
  assert.ok(retry);
  retry.fn();
  assert.strictEqual(h.sockets.length, 2);
  rt.unmount();
});

test('串流合併更新，結束事件先排出尚未繪製的最後文字', () => {
  const h = createHarness();
  const rt = h.mount({ ...tgProps('stream'), agentType: 'claude' });
  rt.render();
  const ws = h.sockets[0];
  ws.open();
  ws.emit({ type: 'sync', value: 'IDLE', messages: [] });
  ws.emit({ type: 'status', value: 'THINKING' });
  ws.emit({ type: 'delta', content: 'hello ' });
  ws.emit({ type: 'delta', content: 'world' });
  ws.emit({ type: 'status', value: 'IDLE' });
  assert.strictEqual(rt.get().messages[0].content, 'hello world');
  assert.strictEqual(rt.get().messages[0].streaming, false);
  rt.unmount();
});

test('即時活動獨立於回覆，交錯思考／工具不覆寫內容；結束、重連、切換會話會清除', () => {
  const h = createHarness();
  const props = { ...tgProps('activity'), agentType: 'claude' };
  const rt = h.mount(props);
  rt.render();
  const ws = h.sockets[0];
  ws.open();
  ws.emit({ type: 'sync', value: 'IDLE', messages: [] });
  ws.emit({ type: 'status', value: 'THINKING' });
  ws.emit({ type: 'thinking', content: '先檢查 runner' });
  assert.deepStrictEqual(clone(rt.get().liveActivity), { kind: 'thinking', content: '先檢查 runner' });
  assert.strictEqual(rt.get().messages[0].content, '');
  ws.emit({ type: 'delta', content: '檢查中。' });
  ws.emit({ type: 'activity', content: '讀取 runner.go' });
  assert.strictEqual(rt.get().messages[0].content, '檢查中。');
  assert.strictEqual(rt.get().liveActivity.content, '讀取 runner.go');
  ws.emit({ type: 'status', value: 'STREAMING' });
  assert.strictEqual(rt.get().liveActivity.content, '讀取 runner.go');
  ws.emit({ type: 'thinking', content: '接著確認測試' });
  ws.emit({ type: 'delta', content: '測試通過。' });
  assert.strictEqual(rt.get().liveActivity, null);
  ws.emit({ type: 'status', value: 'IDLE' });
  assert.strictEqual(rt.get().messages.length, 1);
  assert.strictEqual(rt.get().messages[0].content, '檢查中。測試通過。');
  assert.strictEqual(rt.get().messages[0].streaming, false);
  for (const value of ['IDLE', 'AWAITING_CONFIRM', 'AWAITING_INPUT', 'THINKING']) {
    ws.emit({ type: 'activity', content: '舊活動' });
    ws.emit({ type: 'status', value });
    assert.strictEqual(rt.get().liveActivity, null, value);
  }
  ws.emit({ type: 'activity', content: '舊活動' });
  ws.emit({ type: 'sync', value: 'STREAMING', messages: [] });
  assert.strictEqual(rt.get().liveActivity, null);
  ws.emit({ type: 'activity', content: '舊活動' });
  ws.emit({ type: 'reset' });
  assert.strictEqual(rt.get().liveActivity, null);
  ws.emit({ type: 'activity', content: '舊活動' });
  rt.setProps({ ...props, session: { ...props.session, id: 'other' } });
  assert.strictEqual(rt.get().liveActivity, null);
  assert.strictEqual(rt.get().messages.length, 0);
});

test('計時從後端任務開始恢復；不同裝置時鐘、斷線、重連與新任務不混用起點', () => {
  const h = createHarness();
  const props = { ...tgProps('timing'), agentType: 'claude' };
  const rt = h.mount(props);
  rt.render();
  const ws = h.sockets[0];
  ws.open();
  const start = 5_000_000_000;
  ws.emit({ type: 'sync', value: 'THINKING', messages: [], run_started_at: start, server_time: start + 90_000 });
  assert.strictEqual(h.ctx.Date.now() - rt.get().runStartedAt, 90_000, '晚進入也要顯示已經過的 90 秒');
  const anchor = rt.get().runStartedAt;
  h.advance(20_000);
  assert.strictEqual(h.ctx.Date.now() - rt.get().runStartedAt, 110_000, '沒有新事件也繼續計時');
  ws.emit({ type: 'sync', value: 'STREAMING', messages: [], run_started_at: start, server_time: start + 110_000 });
  assert.strictEqual(rt.get().runStartedAt, anchor, '重連不重設任務起點');
  const fresh = h.mount(props);
  fresh.render();
  const otherSocket = h.sockets.at(-1);
  otherSocket.open();
  otherSocket.emit({ type: 'sync', value: 'STREAMING', messages: [], run_started_at: start, server_time: start + 110_000 });
  assert.strictEqual(h.ctx.Date.now() - fresh.get().runStartedAt, 110_000, '全新畫面也恢復相同耗時');
  ws.emit({ type: 'status', value: 'THINKING', run_started_at: start + 120_000, server_time: start + 120_000 });
  assert.strictEqual(rt.get().runStartedAt, h.ctx.Date.now(), '新任務從自己的起點計算');
  for (const timing of [{}, { run_started_at: 'invalid', server_time: start }, { run_started_at: start, server_time: start - 1 }]) {
    ws.emit({ type: 'status', value: 'THINKING', ...timing });
    assert.strictEqual(rt.get().runStartedAt, null, '缺少／無效時間不冒充已執行零秒');
  }
  ws.emit({ type: 'status', value: 'IDLE' });
  assert.strictEqual(rt.get().runStartedAt, null);
  ws.emit({ type: 'status', value: 'THINKING', run_started_at: start, server_time: start + 90_000 });
  ws.emit({ type: 'reset' });
  assert.strictEqual(rt.get().runStartedAt, null);
  ws.emit({ type: 'status', value: 'THINKING', run_started_at: start, server_time: start + 90_000 });
  rt.setProps({ ...props, session: { ...props.session, id: 'other-timing' } });
  assert.strictEqual(rt.get().runStartedAt, null);
});
