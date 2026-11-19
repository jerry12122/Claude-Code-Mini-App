// 任務 5.1：執行 useChatSocket 的真實 onmessage，確認 sync／queue_update 寫進橫幅狀態。
// 執行：node scripts/quota-resume-banner.test.cjs
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const assert = require('node:assert/strict');

const root = path.join(__dirname, '..', 'internal', 'static', 'js');
const helperSrc = fs.readFileSync(path.join(root, 'hooks', 'quotaResumeBanner.js'), 'utf8');
const hookSrc = fs.readFileSync(path.join(root, 'hooks', 'useChatSocket.js'), 'utf8');
const bannerSrc = fs.readFileSync(path.join(root, 'chat', 'QuotaResumeBanner.js'), 'utf8');
const chatSrc = fs.readFileSync(path.join(root, 'chat', 'ChatView.js'), 'utf8');

const at = '2026-11-18T10:01:00Z';
const localParts = new Intl.DateTimeFormat('en-GB', {
  hour: '2-digit', minute: '2-digit', hourCycle: 'h23',
}).formatToParts(new Date(at));
const localHM = `${localParts.find((p) => p.type === 'hour').value}:${localParts.find((p) => p.type === 'minute').value}`
  .replace(/^24:/, '00:');

function createReact() {
  const store = [];
  const effects = [];
  let cursor = 0;
  return {
    effects,
    useState(init) {
      const i = cursor++;
      if (store[i] === undefined) store[i] = typeof init === 'function' ? init() : init;
      return [store[i], (value) => {
        store[i] = typeof value === 'function' ? value(store[i]) : value;
      }];
    },
    useRef(init) {
      const i = cursor++;
      if (store[i] === undefined) store[i] = { current: init };
      return store[i];
    },
    useEffect(fn) { effects.push(fn); },
    useCallback(fn) { return fn; },
    useMemo(fn) { return fn(); },
    rewind() { cursor = 0; effects.length = 0; },
  };
}

function mount(context, react, session) {
  bind(context, react);
  react.rewind();
  const socket = context.useChatSocket({
    session,
    agentType: 'fakeq',
    showPermModeSelect: false,
    showEffortSelect: false,
  });
  const cleanup = react.effects[0]();
  const connect = react.effects[3]();
  return { socket, cleanup, connect, react };
}

function bind(context, react) {
  context.useState = react.useState.bind(react);
  context.useRef = react.useRef.bind(react);
  context.useEffect = react.useEffect.bind(react);
  context.useCallback = react.useCallback.bind(react);
  context.useMemo = react.useMemo.bind(react);
}

function read(context, react, session) {
  bind(context, react);
  react.rewind();
  return context.useChatSocket({
    session,
    agentType: 'fakeq',
    showPermModeSelect: false,
    showEffortSelect: false,
  });
}

function deliver(socket, msg) {
  socket.onmessage({ data: JSON.stringify(msg) });
}

function plain(value) {
  return JSON.parse(JSON.stringify(value));
}

const context = {
  Intl, Date, Object, Array, Map, Set, JSON, Math, Number, Promise, String,
  console,
  isTelegram: false,
  socketMessageLimit: 16 * 1024 * 1024,
  AbortController: class { abort() {} },
  setTimeout() { return 0; },
  clearTimeout() {},
  WebSocket: class {
    static CONNECTING = 0;
    static OPEN = 1;
    static CLOSED = 3;
    static CLOSING = 2;
    constructor(url) { this.url = url; this.readyState = 0; context.WebSocket.latest = this; }
    close() { this.readyState = 3; }
    send() {}
  },
  document: { hidden: false, addEventListener() {}, removeEventListener() {}, hasFocus() { return false; } },
  window: { addEventListener() {}, removeEventListener() {} },
  normalizePermMode() { return 'default'; },
  readInputModeForSession() { return 'agent'; },
  wsURL(id) { return `ws://fixture/sessions/${id}/ws`; },
  parseMarkdown(text) { return text; },
  mapMessageRow(row) { return row; },
  showToast() {},
  apiFetch() { return Promise.resolve({ ok: true, json: async () => [] }); },
};
vm.createContext(context);
vm.runInContext(helperSrc, context);
vm.runInContext(hookSrc, context);
assert.equal(typeof context.useChatSocket, 'function');
assert.equal(typeof context.applyQuotaResumeFrame, 'undefined');
assert.equal(typeof context.quotaResumeCancelEffect, 'undefined');
assert.equal(typeof context.resetQuotaResume, 'undefined');

const sessionA = { id: 'session-a', permission_mode: 'default', model: '', effort: '', input_mode: 'agent' };
const left = createReact();
const mounted = mount(context, left, sessionA);
const ws = context.WebSocket.latest;
assert.ok(ws, 'useChatSocket 沒有建立 WebSocket');
assert.equal(typeof ws.onmessage, 'function');

const queue = [{ id: 7, content: 'queued' }];
deliver(ws, {
  type: 'sync',
  value: 'IDLE',
  queue,
  queue_paused: true,
  quota_resume_state: 'waiting',
  quota_resume_at: at,
});
let view = read(context, left, sessionA);
assert.equal(view.quotaResume.state, 'waiting');
assert.equal(view.quotaResume.at, at);
assert.deepEqual(plain(view.queue), queue);
assert.equal(view.queuePaused, true);
assert.equal(view.connected, false);

const owner = context.quotaResumeBannerDescriptor({ resume: view.quotaResume, isOwner: true, connected: true });
assert.equal(owner.role, 'status');
assert.equal(owner.live, 'polite');
assert.equal(owner.text, `額度用盡，預計 ${localHM} 自動繼續`);
assert.deepEqual(plain(owner.button), { type: 'button', name: '取消自動繼續', disabled: false });
const offline = context.quotaResumeBannerDescriptor({ resume: view.quotaResume, isOwner: true, connected: false });
assert.equal(offline.button.disabled, true);
for (const role of ['editor', 'viewer']) {
  const guest = context.quotaResumeBannerDescriptor({ resume: view.quotaResume, isOwner: false, connected: true, role });
  assert.equal(guest.text, owner.text);
  assert.equal(guest.button, null, role);
}
assert.equal(context.quotaResumeLocalHM(at), localHM);
assert.equal(context.quotaResumeLocalHM(at, 'Asia/Taipei'), '18:01');
assert.equal(context.quotaResumeLocalHM(''), '');
assert.equal(context.quotaResumeLocalHM('not-a-time'), '');

deliver(ws, {
  type: 'queue_update',
  queue,
  queue_paused: true,
  quota_resume_state: 'exhausted',
  quota_resume_at: '',
});
view = read(context, left, sessionA);
const exhausted = context.quotaResumeBannerDescriptor({ resume: view.quotaResume, isOwner: true, connected: true });
assert.equal(exhausted.text, '已達自動繼續上限，請手動繼續');
assert.equal(exhausted.button, null);
assert.deepEqual(plain(view.queue), queue);
assert.equal(view.queuePaused, true);

deliver(ws, { type: 'queue_update', queue, queue_paused: true, quota_resume_state: 'none', quota_resume_at: '' });
view = read(context, left, sessionA);
assert.equal(context.quotaResumeBannerDescriptor({ resume: view.quotaResume, isOwner: true, connected: true }), null);
assert.deepEqual(plain(view.queue), queue);

deliver(ws, {
  type: 'sync', value: 'IDLE', queue, queue_paused: true,
  quota_resume_state: 'waiting', quota_resume_at: at,
});
deliver(ws, { type: 'queue_update', queue, queue_paused: true });
view = read(context, left, sessionA);
assert.deepEqual(plain(view.quotaResume), { state: 'none', at: '' });
assert.deepEqual(plain(view.queue), queue);

deliver(ws, {
  type: 'sync', value: 'IDLE', queue, queue_paused: true,
  quota_resume_state: 'resuming', quota_resume_at: at,
});
view = read(context, left, sessionA);
assert.equal(context.quotaResumeBannerDescriptor({ resume: view.quotaResume, isOwner: true, connected: true }), null);

const sessionB = { id: 'session-b', permission_mode: 'default', model: '', effort: '', input_mode: 'agent' };
const right = createReact();
mount(context, right, sessionB);
assert.equal(typeof context.WebSocket.latest.onmessage, 'function');
deliver(context.WebSocket.latest, {
  type: 'sync', value: 'IDLE', queue: [{ id: 1, content: 'other' }], queue_paused: false,
  quota_resume_state: 'waiting', quota_resume_at: at,
});
view = read(context, left, sessionA);
const other = read(context, right, sessionB);
assert.equal(view.quotaResume.state, 'resuming');
assert.equal(other.quotaResume.state, 'waiting');
assert.deepEqual(plain(other.queue), [{ id: 1, content: 'other' }]);

mount(context, left, sessionB);
const afterSwitch = read(context, left, sessionB);
assert.deepEqual(plain(afterSwitch.quotaResume), { state: 'none', at: '' });
assert.deepEqual(plain(afterSwitch.queue), []);
assert.equal(other.quotaResume.state, 'waiting');

assert.match(bannerSrc, /quotaResumeBannerDescriptor\(/);
assert.match(bannerSrc, /role=\{view\.role\}/);
assert.match(bannerSrc, /aria-live=\{view\.live\}/);
assert.match(chatSrc, /onCancel=\{\(\) => sendOrToast\(\{ type: 'quota_resume_cancel' \}\)\}/);
assert.doesNotMatch(chatSrc, /quotaResumeCancelEffect/);
assert.doesNotMatch(hookSrc, /applyQuotaResumeFrame|resetQuotaResume/);

console.log('quota resume banner ok');
