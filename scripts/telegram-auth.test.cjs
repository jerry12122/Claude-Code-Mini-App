const { test } = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const core = fs.readFileSync(path.join(__dirname, '../internal/static/js/core/core.js'), 'utf8');
const auth = core.slice(core.indexOf('// Session storage also works'), core.indexOf('// WebSocket：'));
const response = (status, body = {}) => ({ status, ok: status === 200, json: async () => body });

test('Telegram exchanges once, uses bearer sessions and silently replaces invalid sessions', async () => {
  const calls = [];
  let generation = 0, expired = 0, rejectOld = false;
  const storage = new Map();
  const context = vm.createContext({
    tgApp: { initDataUnsafe: { user: { id: 123 } } }, initData: 'fresh-signed-data',
    isTelegram: true, isGuestMode: false,
    sessionStorage: { getItem: k => storage.get(k), setItem: (k,v) => storage.set(k,v), removeItem: k => storage.delete(k) },
    resolveApiUrl: p => p, handleUnauthorized: () => {}, onSessionExpired: () => expired++,
    fetchWithTimeout: async (url, opts) => {
      calls.push([url, opts.headers]);
      if (url === '/auth/telegram') return response(200, { token: 'tg_' + ++generation });
      if (rejectOld && opts.headers.Authorization === 'Bearer tg_1') return response(401);
      return response(200);
    },
  });
  vm.runInContext(auth, context);
  await Promise.all([vm.runInContext("apiFetch('/sessions')", context), vm.runInContext("apiFetch('/config')", context)]);
  assert.equal(generation, 1);
  assert(calls.filter(([p]) => p !== '/auth/telegram').every(([,h]) => h.Authorization === 'Bearer tg_1' && !h['X-Telegram-Init-Data']));
  rejectOld = true;
  await Promise.all([vm.runInContext("apiFetch('/sessions')", context), vm.runInContext("apiFetch('/config')", context)]);
  assert.equal(generation, 2);
  assert.equal(expired, 0);
  assert.equal(storage.get('cc_tg_session:123'), 'tg_2');
});

test('Expired Telegram credentials stop retries and preserve draft storage', async () => {
  let calls = 0, expired = 0;
  const storage = new Map([['draft:session', 'keep this draft']]);
  const context = vm.createContext({
    tgApp: {}, initData: 'old-signed-data', isTelegram: true, isGuestMode: false,
    sessionStorage: { getItem: k => storage.get(k), setItem: (k,v) => storage.set(k,v), removeItem: k => storage.delete(k) },
    resolveApiUrl: p => p, handleUnauthorized: () => {}, onSessionExpired: () => expired++,
    fetchWithTimeout: async () => { calls++; return response(401); },
  });
  vm.runInContext(auth, context);
  await assert.rejects(vm.runInContext("apiFetch('/sessions')", context), /草稿會保留/);
  await assert.rejects(vm.runInContext("apiFetch('/sessions')", context), /草稿會保留/);
  assert.equal(calls, 1);
  assert.equal(expired, 1);
  assert.equal(storage.get('draft:session'), 'keep this draft');
});

test('Oversized UTF-8 messages are detected locally without dropping the input', () => {
  const source = core.slice(core.indexOf('let socketMessageLimit'), core.indexOf('const wsURL ='));
  const context = vm.createContext({ TextEncoder });
  vm.runInContext(source, context);
  vm.runInContext('socketMessageLimit = 1024', context);
  assert.equal(vm.runInContext("socketPayloadError({data:'hello'})", context), '');
  assert.match(vm.runInContext("socketPayloadError({data:'中'.repeat(400)})", context), /草稿已保留/);
});
