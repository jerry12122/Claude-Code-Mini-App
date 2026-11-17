const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
const path = require('node:path');

function harness(apiFetch, sessionId = 'one') {
  const slots = [], effects = [];
  let cursor = 0, value;
  const context = {
    apiFetch,
    useState(initial) {
      const i = cursor++;
      if (!(i in slots)) slots[i] = initial;
      return [slots[i], next => { slots[i] = typeof next === 'function' ? next(slots[i]) : next; }];
    },
    useRef(initial) {
      const i = cursor++;
      return slots[i] ||= { current: initial };
    },
    useCallback(fn, deps) {
      const i = cursor++;
      if (!slots[i] || deps.some((d, n) => d !== slots[i].deps[n])) slots[i] = { fn, deps };
      return slots[i].fn;
    },
    useEffect(fn, deps) {
      const i = cursor++;
      if (!slots[i] || deps.some((d, n) => d !== slots[i].deps[n])) {
        const prior = slots[i];
        slots[i] = { deps };
        effects.push(() => { prior?.cleanup?.(); slots[i].cleanup = fn(); });
      }
    },
  };
  vm.createContext(context);
  vm.runInContext(fs.readFileSync(path.join(__dirname, '../internal/static/js/hooks/useGitStatus.js'), 'utf8'), context);
  return {
    render(id = sessionId, state) {
      sessionId = id;
      cursor = 0;
      value = context.useGitStatus(id, state);
      while (effects.length) effects.shift()();
      return value;
    },
    unmount() { slots.forEach(slot => slot?.cleanup?.()); },
  };
}
const response = (data, status = 200) => ({ ok: status === 200, status, json: async () => data });
const settle = () => new Promise(resolve => setImmediate(resolve));

(async () => {
  const calls = [];
  const readOptions = [];
  let failure = false, nonRepo = false;
  const h = harness(async (url, options) => {
    calls.push(url);
    readOptions.push(options);
    if (failure) return response({ error: 'git 查詢失敗' }, 504);
    return response(url.endsWith('/status') ? { is_repo: !nonRepo, branch: 'main', files: [], total: 0 } : { current: 'main', branches: ['dev', 'main'] });
  });
  h.render();
  assert.equal(h.render().loading, true);
  await settle();
  let state = h.render();
  assert.equal(state.status.branch, 'main');
  assert.deepEqual(Array.from(state.branches), ['dev', 'main']);
  assert.deepEqual(calls, ['/sessions/one/git/status', '/sessions/one/git/branches']);
  assert.equal(readOptions[0]?.timeoutMs, 45000, '狀態＋增刪統計的前端期限涵蓋伺服器查詢');
  failure = true;
  await state.reload();
  state = h.render();
  assert.equal(state.error, 'git 查詢失敗');
  assert.equal(state.status.branch, 'main', '重新整理失敗仍保留先前資料');
  assert.equal(state.loading, false);
  failure = false;
  nonRepo = true;
  const before = calls.length;
  await state.reload();
  state = h.render();
  assert.equal(state.status.is_repo, false);
  assert.equal(state.branches.length, 0);
  assert.equal(calls.length, before + 1, '非 repo 不查詢分支');
  h.unmount();

  let finishOld;
  const race = harness(url => url.includes('/one/')
    ? new Promise(resolve => { finishOld = resolve; })
    : Promise.resolve(response(url.endsWith('/status') ? { is_repo: true, branch: 'new' } : { branches: ['new'] })));
  race.render();
  race.render('two');
  await settle();
  finishOld(response({ is_repo: true, branch: 'stale' }));
  await settle();
  assert.equal(race.render().status.branch, 'new', '先前 session 的回應不可覆寫新 session');
  race.unmount();

  const guest = harness(() => { throw new Error('訪客不得送出 Git 查詢'); }, null);
  guest.render(null);
  await settle();
  assert.equal(guest.render(null).loading, false);
  assert.equal(guest.render(null).status, null);
  guest.unmount();

  const idleCalls = [];
  const idle = harness(async url => {
    idleCalls.push(url);
    return response(url.endsWith('/status') ? { is_repo: true, branch: 'main' } : { branches: ['main'] });
  });
  idle.render('one', 'THINKING');
  await settle();
  assert.equal(idleCalls.length, 2);
  idle.render('two', 'IDLE');
  await settle();
  assert.equal(idleCalls.length, 4, '換 session 同時回到閒置只查一輪');
  idle.render('two', 'THINKING');
  idle.render('two', 'IDLE');
  await settle();
  assert.equal(idleCalls.length, 6, 'agent 回到閒置更新行內徽章');
  idle.unmount();

  const writes = [];
  let branch = 'main', reject = true, finishWrite;
  const operations = harness(async (url, opts = {}) => {
    writes.push({ url, ...opts });
    if (opts.method === 'POST') {
      if (reject) return response({ error: '切換會覆寫本機修改', output: '完整 git 訊息' }, 422);
      if (url.endsWith('/checkout')) branch = JSON.parse(opts.body).branch;
      if (url.endsWith('/push')) return new Promise(resolve => { finishWrite = resolve; });
      return response({ ok: true, output: '已完成' });
    }
    return response(url.endsWith('/status') ? { is_repo: true, branch } : { branches: ['dev', 'main'] });
  });
  operations.render();
  await settle();
  state = operations.render();
  await state.run('checkout', 'dev');
  state = operations.render();
  assert.equal(state.status.branch, 'main', '操作失敗保留先前分支');
  assert.equal(state.error, '切換會覆寫本機修改');
  let post = writes.find(call => call.method === 'POST');
  assert.equal(post.url, '/sessions/one/git/checkout');
  assert.deepEqual(JSON.parse(post.body), { branch: 'dev' });
  assert.equal(post.headers['Content-Type'], 'application/json');
  assert.equal(post.timeoutMs, 50000);
  reject = false;
  const beforeSuccess = writes.length;
  await state.run('checkout', 'dev');
  state = operations.render();
  assert.equal(state.status.branch, 'dev');
  assert.equal(state.error, '');
  assert.equal(state.result, '已完成');
  assert.deepEqual(writes.slice(beforeSuccess).map(call => call.url.split('/').pop()), ['checkout', 'status', 'branches']);
  const pending = state.run('push');
  assert.equal(operations.render().action, 'push');
  await state.run('pull');
  assert.equal(writes.filter(call => call.url.endsWith('/pull')).length, 0, '進行中不得重疊寫入');
  post = writes.find(call => call.url.endsWith('/push'));
  assert.equal(post.timeoutMs, 125000);
  finishWrite(response({ ok: true }));
  await pending;
  assert.equal(operations.render().action, '');
  operations.unmount();
  let finishOldWrite;
  const staleWrite = harness(async (url, opts = {}) => {
    if (opts.method === 'POST') return new Promise(resolve => { finishOldWrite = resolve; });
    return response(url.endsWith('/status') ? { is_repo: true, branch: url.includes('/two/') ? 'two' : 'one' } : { branches: [] });
  });
  staleWrite.render();
  await settle();
  const oldWrite = staleWrite.render().run('push');
  staleWrite.render('two');
  await settle();
  finishOldWrite(response({ ok: true, output: '舊 session 已完成' }));
  await oldWrite;
  assert.equal(staleWrite.render().status.branch, 'two', '舊操作不得刷新或覆寫新 session');
  assert.equal(staleWrite.render().result, '');
  assert.equal(staleWrite.render().action, '');
  staleWrite.unmount();
  const panel = fs.readFileSync(path.join(__dirname, '../internal/static/js/chat/GitControls.js'), 'utf8');
  assert.match(panel, /session\.status !== 'idle' \|\| state !== 'IDLE'/, 'DB 與即時狀態都必須閒置');
  console.log('git-panel: 唯讀、寫入、逾時、錯誤保留、刷新與競態檢查通過');
})().catch(error => { console.error(error); process.exitCode = 1; });
