const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');

const root = path.join(__dirname, '../internal/static/js');
function readFunction(file, name) {
  const source = fs.readFileSync(path.join(root, file), 'utf8').replace(/\r\n/g, '\n');
  let start = source.indexOf(`function ${name}(`);
  if (source.slice(start - 6, start) === 'async ') start -= 6;
  assert.ok(start >= 0);
  return source.slice(start, source.indexOf('\n}\n', start) + 3);
}

async function main() {
  const sockets = [];
  const timers = new Map();
  let next = 0;
  class Socket {
    constructor() { sockets.push(this); }
    send(data) { this.sent = JSON.parse(data); }
    close() { this.closed = true; this.onclose?.(); }
    reply(data) { this.onmessage({ data: JSON.stringify(data) }); }
  }
  const storage = new Map();
  const drafts = new Map();
  const ctx = vm.createContext({
    WebSocket: Socket, wsURL: id => id, AbortController,
    setTimeout: fn => { const id = ++next; timers.set(id, fn); return id; },
    clearTimeout: id => timers.delete(id),
    composerDrafts: drafts,
    writeComposerFiles: async () => {},
    draftInputStorageKey: id => id,
    pendingInputStorageKey: id => id + ':pending',
    localStorage: { getItem: id => storage.get(id), setItem: (id, value) => storage.set(id, value), removeItem: id => storage.delete(id) },
    window: { dispatchEvent() {} }, CustomEvent: class {}, URL: { revokeObjectURL() {} },
  });
  for (const name of ['newInputRequestID', 'notifyPendingInput', 'beginPendingInput', 'sendPendingInput', 'sendPromptViaEphemeralWS']) {
    vm.runInContext(readFunction('ui/ui-atoms.js', name), ctx);
  }
  vm.runInContext(readFunction('ui/ui-atoms.js', 'restoreComposerDraft'), ctx);
  vm.runInContext(readFunction('ui/ui-atoms.js', 'recoverPendingInput'), ctx);
  vm.runInContext(readFunction('core/core.js', 'fetchWithTimeout'), ctx);

  let accepted = false;
  const sent = ctx.sendPromptViaEphemeralWS('s', 'hello').then(() => { accepted = true; });
  const ws = sockets.at(-1);
  ws.onopen();
  await Promise.resolve();
  assert.equal(accepted, false, '握手成功還不代表訊息被接受');
  ws.reply({ type: 'input_accepted', request_id: 'another-request' });
  assert.equal(accepted, false);
  ws.reply({ type: 'input_accepted', request_id: ws.sent.request_id });
  await sent;
  assert.equal(accepted, true);
  assert.equal(ws.closed, true);
  assert.equal(timers.size, 0);

  const rejected = ctx.sendPromptViaEphemeralWS('s', 'hello');
  const rejection = assert.rejects(rejected, /維護中/);
  sockets.at(-1).onopen();
  sockets.at(-1).reply({ type: 'input_rejected', request_id: sockets.at(-1).sent.request_id, content: '維護中' });
  await rejection;

  const disconnected = ctx.sendPromptViaEphemeralWS('s', 'hello');
  const closeFailure = assert.rejects(disconnected, /尚未確認/);
  sockets.at(-1).onopen();
  sockets.at(-1).onclose();
  await closeFailure;

  const retryId = sockets.at(-1).sent.request_id;
  drafts.delete('s'); // simulate a reload: recover the exact request from storage
  assert.equal(ctx.recoverPendingInput('s'), true);
  assert.equal(drafts.get('s').pending.requestId, retryId);
  const retry = ctx.sendPromptViaEphemeralWS('s', 'hello');
  sockets.at(-1).onopen();
  assert.equal(sockets.at(-1).sent.request_id, retryId, '重試必須沿用原 ID');
  sockets.at(-1).reply({ type: 'input_accepted', request_id: retryId });
  await retry;
  assert.equal(storage.has('s:pending'), false);

  const file = { id: 'file', attachmentId: 'uploaded-file', file: { name: 'screenshot.png' }, status: 'ready' };
  storage.set('s', 'new draft');
  drafts.set('s', { attachments: [file], mentions: [{ id: 'other' }] });
  ctx.restoreComposerDraft('s', 'original', [file], [{ id: 'target' }]);
  assert.equal(storage.get('s'), 'original\n\nnew draft');
  assert.equal(drafts.get('s').attachments.length, 1);
  assert.equal(drafts.get('s').attachments[0].file, file.file);
  assert.deepEqual(Array.from(drafts.get('s').mentions, s => s.id), ['target', 'other']);
  const normalStorage = ctx.localStorage;
  ctx.localStorage = { getItem() { throw new Error('blocked'); }, setItem() { throw new Error('blocked'); } };
  ctx.restoreComposerDraft('blocked', 'keep this', [file]);
  assert.equal(drafts.get('blocked').text, 'keep this', 'localStorage 被封鎖仍保留記憶體草稿');
  assert.equal(drafts.get('blocked').attachments[0].file, file.file);
  ctx.localStorage = normalStorage;
  storage.set('reload:pending', 'unconfirmed message');
  storage.set('reload', 'new draft');
  assert.equal(ctx.recoverPendingInput('reload'), true);
  assert.equal(storage.get('reload'), 'unconfirmed message\n\nnew draft');
  assert.equal(storage.has('reload:pending'), false);
  assert.equal(ctx.recoverPendingInput('reload'), false);

  // 收到 headers 之後本文仍可能卡住，timer 必須繼續有效。
  let signal;
  ctx.fetch = async (_url, opts) => { signal = opts.signal; return { json: () => new Promise((_, reject) => signal.addEventListener('abort', () => reject(signal.reason))) }; };
  const response = await ctx.fetchWithTimeout('/slow');
  const body = assert.rejects(response.json(), /連線逾時/);
  [...timers.values()].at(-1)();
  await body;
  assert.equal(signal.aborted, true);

  // 直接驅動元件的操作處理函式；不掛瀏覽器、不新增測試框架。
  const chat = fs.readFileSync(path.join(root, 'chat/ChatView.js'), 'utf8').replace(/\r\n/g, '\n');
  const handler = (source, name, nextName) => source.slice(source.indexOf(`  const ${name} =`), source.indexOf(`  const ${nextName} =`));
  let ack;
  Object.assign(ctx, {
    pendingInput: null, session: { id: 's' }, composerSessionRef: { current: 's' }, pendingSendRef: { current: null },
    isDisabled: false, inputMode: 'agent', state: 'IDLE', canQueue: false,
    attachmentItemsRef: { current: [file] }, mentionChips: [{ id: 'target' }], mentionChipsRef: { current: [{ id: 'target' }] },
    input: 'original', inputRef: { current: 'original' }, isTGChat: false, sendFollowUntilRef: { current: 0 },
    flushOrToast: () => true, expandMentionPrompt: text => text,
    clearDraftInputForSession: id => storage.delete(id),
    setInput: text => { ctx.input = text; ctx.inputRef.current = text; drafts.set('s', { ...drafts.get('s'), text }); },
    setMentionChips: mentions => { ctx.mentionChips = mentions; ctx.mentionChipsRef.current = mentions; drafts.set('s', { ...drafts.get('s'), mentions }); },
    updateAttachments: update => { ctx.attachmentItemsRef.current = update(ctx.attachmentItemsRef.current); drafts.set('s', { ...drafts.get('s'), attachments: ctx.attachmentItemsRef.current }); },
    closeComposerMenus() {}, setSending() {}, showToast() {},
    submitInput: () => new Promise(resolve => { ack = resolve; }),
    URL: { revokeObjectURL() {} },
  });
  ctx.sendComposerInput = record => ctx.sendPendingInput('s', record, ctx.submitInput).catch(() => {});
  vm.runInContext(handler(chat, 'handleSend', 'applySuggestion') + '\nglobalThis.handleSend = handleSend;', ctx);
  const send = ctx.handleSend();
  assert.equal(ctx.input, '', '等待確認時已可輸入下一則');
  ctx.setInput('next message');
  storage.set('s', ctx.input);
  ack();
  await send;
  assert.equal(ctx.input, 'next message', '較晚的接受確認不能清掉新文字');
  assert.equal(storage.get('s'), 'next message');

  let fail;
  ctx.input = 'restore original';
  ctx.inputRef.current = ctx.input;
  ctx.attachmentItemsRef.current = [file];
  ctx.submitInput = () => new Promise((_, reject) => { fail = reject; });
  const failedSend = ctx.handleSend();
  ctx.setInput('newer draft');
  storage.set('s', 'newer draft');
  fail(new Error('network lost'));
  await failedSend;
  assert.equal(storage.get('s'), 'newer draft', '未知結果與下一則草稿分開');
  const pending = drafts.get('s').pending;
  assert.equal(pending.text, 'restore original');
  assert.equal(pending.attachments[0].file, file.file);
  assert.equal(JSON.parse(storage.get('s:pending')).requestId, pending.requestId);
  assert.equal(ctx.beginPendingInput('s', { data: 'restore original', text: 'restore original', attachments: [file] }), pending);
  assert.throws(() => ctx.beginPendingInput('s', { data: 'different' }), /尚有訊息未確認/);
  await assert.rejects(ctx.sendPendingInput('s', pending, async (_data, _ids, id) => {
    assert.equal(id, pending.requestId);
    throw Object.assign(new Error('explicit rejection'), { rejected: true });
  }), /explicit rejection/);
  assert.equal(storage.get('s'), 'restore original\n\nnewer draft', '明確拒絕才合併還原');
  assert.equal(storage.has('s:pending'), false);
  assert.equal(drafts.get('s').attachments[0].file, file.file);

  const sessions = fs.readFileSync(path.join(root, 'session/SessionView.js'), 'utf8').replace(/\r\n/g, '\n');
  const target = { id: 's', name: 'keep me' };
  let list = [target];
  let editing = target.id;
  let notified = '';
  timers.clear();
  Object.assign(ctx, {
    sessions: list, pendingDelRef: { current: new Set() },
    setSessions: update => { list = update(list); }, load() {},
    showToast: text => { notified = text; },
    apiFetch: async () => ({ ok: false, json: async () => ({ error: 'denied' }) }),
    renameSavingRef: { current: false }, renameVal: 'new name', renameInputRef: { current: { focus() {} } },
    setRenamingId: id => { editing = id; }, setRenameVal: value => { ctx.renameVal = value; },
  });
  vm.runInContext(handler(sessions, 'del', 'startRename') + '\nglobalThis.del = del;', ctx);
  ctx.del('s', { stopPropagation() {} });
  assert.equal(list.length, 0);
  await [...timers.values()].at(-1)();
  assert.equal(list[0], target, '刪除未確認時恢復本地項目');
  assert.match(notified, /無法確認/);
  vm.runInContext(handler(sessions, 'commitRename', 'handleRenameKey') + '\nglobalThis.commitRename = commitRename;', ctx);
  await ctx.commitRename('s');
  assert.equal(editing, 's', '失敗時保留改名輸入框');
  assert.equal(ctx.renameVal, 'new name');
  assert.match(notified, /denied/);
  console.log('UX reliability checks passed');
}

main().catch(err => { console.error(err); process.exitCode = 1; });
