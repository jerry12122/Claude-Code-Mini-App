const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const root = path.join(__dirname, '../internal/static/js');
const read = name => fs.readFileSync(path.join(root, name), 'utf8').replace(/\r\n/g, '\n');

async function main() {
  const requests = [];
  let rows = [], error = '', loading = false, loaded = false, callbacks = 0;
  const context = vm.createContext({
    AbortController, Set,
    mountedRef: { current: true }, loadGenerationRef: { current: 0 }, loadControllerRef: { current: null },
    pendingDelRef: { current: new Set() }, onLoadedRef: { current: () => callbacks++ },
    setListLoading: value => { loading = value; }, setListLoaded: value => { loaded = value; },
    setListError: value => { error = value; }, setSessions: value => { rows = value; },
    notifyStatusChanges() {}, sortSessionsByWorkDirThenName: value => value,
    apiFetch: (_url, options) => new Promise(resolve => { requests.push({ resolve, options }); }),
  });
  const sessions = read('session/SessionView.js');
  const start = sessions.indexOf('  const load = async');
  const end = sessions.indexOf('\n  useEffect(', start);
  vm.runInContext(sessions.slice(start, end) + '\nglobalThis.load = load;', context);
  const first = context.load();
  const second = context.load();
  assert.equal(requests[0].options.signal.aborted, true);
  requests[1].resolve({ ok: true, json: async () => [{ id: 'new' }] });
  await second;
  // Ignore cancellation to model a response/body already in flight.
  requests[0].resolve({ ok: true, json: async () => [{ id: 'old' }] });
  await first;
  assert.equal(rows[0].id, 'new');
  assert.equal(callbacks, 1);
  assert.equal(loading, false);
  assert.equal(loaded, true);
  const failure = context.load();
  requests[2].resolve({ ok: false });
  await failure;
  assert.match(error, /載入失敗/);
  assert.equal(rows[0].id, 'new', '失敗保留最後成功列表');
  const unmounted = context.load();
  context.mountedRef.current = false;
  requests[3].resolve({ ok: true, json: async () => [{ id: 'after-unmount' }] });
  await unmounted;
  assert.equal(rows[0].id, 'new');

  const virtual = read('chat/VirtualMessages.js');
  const math = vm.createContext({});
  vm.runInContext(virtual.slice(0, virtual.indexOf('function VirtualMessages(')), math);
  const offsets = Array.from({ length: 10001 }, (_, index) => index * 180);
  assert.equal(math.virtualMessageIndex([0], 0), 0);
  assert.equal(math.virtualMessageIndex([0, 70, 150], 70), 1);
  assert.equal(math.virtualMessageIndex([0, 70, 150], 10000), 1);
  const window = math.virtualMessageWindow(offsets, 900000, 800, true);
  assert.ok(window.start <= 5000 && window.end > 5000);
  assert.ok(window.end - window.start < 25, '一萬則訊息只掛載視窗附近的列');
  assert.equal(math.virtualMessageWindow(offsets, 1799200, 800, true).end, 10000);
  assert.equal(math.virtualMessageWindow(offsets, 0, 800, false).end, 10000);
  assert.equal(math.virtualMessageWindow([0], 0, 800, true).end, 0);
  assert.equal(math.virtualMessageWindow(offsets, -80, 800, true).start, 0);
  const pending = { role: 'claude', createdAt: '2026-11-05T00:00:00Z' };
  assert.equal(math.virtualMessageKey(pending, 3), math.virtualMessageKey(pending, 103), '補頁不改變 pending key');

  // Minimal IndexedDB event model: writes become visible at transaction commit,
  // with a separate abort path. The application checks never call a real API.
  const disk = new Map();
  let abortWrite = false;
  const database = {
    createObjectStore() {}, close() {},
    transaction(_store, mode) {
      let commit = () => {};
      const transaction = {
        objectStore: () => ({
          get: key => ({ result: structuredClone(disk.get(key)) }),
          put: (value, key) => { const copy = structuredClone(value); commit = () => disk.set(key, copy); },
          delete: key => { commit = () => disk.delete(key); },
        }),
      };
      const abort = abortWrite && mode === 'readwrite';
      setImmediate(() => {
        if (abort) {
          transaction.error = new Error('quota exceeded');
          transaction.onerror?.();
          transaction.onabort?.();
        } else { commit(); transaction.oncomplete?.(); }
      });
      return transaction;
    },
  };
  const files = vm.createContext({
    indexedDB: { open() {
      const request = { result: database };
      setImmediate(() => { request.onupgradeneeded?.(); request.onsuccess(); });
      return request;
    } },
    URL: { createObjectURL: () => 'blob:restored-preview' },
  });
  vm.runInContext(read('hooks/composerFiles.js'), files);
  const original = { id: 'a', name: 'draft.png', file: new Blob(['original bytes'], { type: 'image/png' }), previewUrl: 'blob:old-document', status: 'uploading' };
  await files.writeComposerFiles('owner:s1', [original]);
  const recovered = files.restoreComposerFiles(await files.readComposerFiles('owner:s1'));
  assert.equal(await recovered[0].file.text(), 'original bytes');
  assert.equal(recovered[0].previewUrl, 'blob:restored-preview');
  assert.equal(recovered[0].status, 'error', '中斷的上傳必須可重試');
  assert.match(recovered[0].error, /重試/);
  assert.equal(disk.get('owner:s1')[0].previewUrl, undefined);
  assert.equal((await files.readComposerFiles('share:s1')).length, 0);
  abortWrite = true;
  await assert.rejects(files.writeComposerFiles('owner:s1', []), /quota/);
  assert.equal((await files.readComposerFiles('owner:s1')).length, 1, '保存失敗不覆蓋原檔');
  abortWrite = false;
  await files.writeComposerFiles('owner:s1', []);
  assert.equal((await files.readComposerFiles('owner:s1')).length, 0);
  console.log('Remaining UX checks passed');
}

main().catch(error => { console.error(error); process.exitCode = 1; });
