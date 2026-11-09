const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const root = path.join(__dirname, '../internal/static/js');
const read = name => fs.readFileSync(path.join(root, name), 'utf8').replace(/\r\n/g, '\n');

function virtualPositionCheck(source) {
  const slots = [], effects = [], listeners = new Map();
  let cursor = 0, position = 1400, writes = 0;
  const equal = (a, b) => a && b && a.length === b.length && a.every((value, index) => Object.is(value, b[index]));
  const scroll = {
    clientHeight: 800, clientWidth: 800, scrollHeight: 54040,
    get scrollTop() { return position; }, set scrollTop(value) { writes++; position = value; },
    getBoundingClientRect: () => ({ top: 0 }),
    addEventListener: (name, fn) => listeners.set(name, fn), removeEventListener: name => listeners.delete(name),
  };
  const rootNode = { children: [], getBoundingClientRect: () => ({ top: 40 - position }) };
  const context = vm.createContext({
    useRef(initial) { return slots[cursor++] ||= { current: initial }; },
    useState(initial) {
      const slot = slots[cursor++] ||= { value: initial };
      return [slot.value, update => { slot.value = typeof update === 'function' ? update(slot.value) : update; }];
    },
    useMemo(fn, deps) {
      const index = cursor++, slot = slots[index];
      if (!slot || !equal(slot.deps, deps)) slots[index] = { value: fn(), deps };
      return slots[index].value;
    },
    useLayoutEffect(fn, deps) {
      const index = cursor++, previous = slots[index];
      if (!previous || !equal(previous.deps, deps)) effects.push(() => {
        previous?.cleanup?.();
        slots[index] = { deps, cleanup: fn() };
      });
    },
    requestAnimationFrame: () => 1, cancelAnimationFrame() {},
    ResizeObserver: class { observe() {} disconnect() {} },
  });
  // Drive the actual hooks/scroll effects without JSX, a DOM or an application.
  const component = source.slice(0, source.indexOf('\n  return (\n    <div ref={rootRef}')) + '\n  return rootRef;\n}\n';
  vm.runInContext(component, context);
  const props = {
    messages: Array.from({ length: 300 }, (_, id) => ({ id })),
    scrollRef: { current: scroll }, followRef: { current: false }, gestureRef: { current: true }, showAll: false,
  };
  const render = () => {
    cursor = 0;
    context.VirtualMessages(props).current = rootNode;
    for (const effect of effects.splice(0)) effect();
  };
  render();
  position = 1400;
  listeners.get('scroll')();
  render();
  position = 1370; // input/render arrives before the browser delivers scroll
  writes = 0;
  render();
  assert.equal(writes, 0, '打字等無關重繪不能把捲動位置拉回舊錨點');
  assert.equal(position, 1370);
  listeners.get('scroll')();
  render();
  props.messages = [{ id: 'older' }, ...props.messages];
  render();
  assert.equal(position, 1550, '補一列歷史仍保留正在閱讀的訊息位置');
}

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
  const statusMarkup = sessions.slice(sessions.indexOf('  const listStatus ='), sessions.indexOf('  const renderSessionRow'));
  const statusClass = statusMarkup.match(/className=\{([^}]+)\}/)?.[1] || JSON.stringify(statusMarkup.match(/className="([^"]+)"/)[1]);
  for (const [listLoaded, listError, hidden] of [[false, '', false], [true, '', true], [false, 'failed', false], [true, 'failed', false]]) {
    const className = vm.runInNewContext(statusClass, { listLoaded, listError });
    assert.equal(className.split(/\s+/).includes('sr-only'), hidden, '背景刷新不撐高列表，首次載入和錯誤仍顯示');
  }
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
  virtualPositionCheck(virtual);

  const chat = read('chat/ChatView.js');
  const body = {}, inputElement = { disabled: false, focus() { focusCount++; } };
  let focusCount = 0, selected = false, mobile = false;
  const controls = vm.createContext({
    useCallback: fn => fn, requestAnimationFrame: fn => fn(),
    chatScrollRef: { current: { scrollHeight: 1000, scrollTop: 170, clientHeight: 800 } },
    chatNearBottomRef: { current: true }, setQuoteSelection() {}, setShowJumpLatest() {},
    chatInputRef: { current: inputElement }, focusedPaneRef: { current: true },
    composerSessionRef: { current: 's' }, session: { id: 's' },
    document: { body, documentElement: {}, activeElement: body, hidden: false },
    window: { getSelection: () => ({ isCollapsed: !selected }), matchMedia: () => ({ matches: mobile }) },
  });
  const syncStart = chat.indexOf('  const syncChatNearBottom =');
  vm.runInContext(chat.slice(syncStart, chat.indexOf('  const jumpToLatest =', syncStart)) + '\nglobalThis.sync = syncChatNearBottom;', controls);
  controls.sync();
  assert.equal(controls.chatNearBottomRef.current, false, '小幅往上捲 30px 必須停止跟隨');
  const focusStart = chat.indexOf('  const focusChatInput =');
  vm.runInContext(chat.slice(focusStart, chat.indexOf('  /** 將游標放回輸入框', focusStart)) + '\nglobalThis.focusInput = focusChatInput;', controls);
  controls.focusInput(true);
  assert.equal(focusCount, 0, '閱讀歷史時不得自動搶焦點');
  controls.chatNearBottomRef.current = true;
  selected = true;
  controls.focusInput(true);
  assert.equal(focusCount, 0, '選取訊息時不得自動搶焦點');
  selected = false;
  controls.focusedPaneRef.current = false;
  controls.focusInput(true);
  assert.equal(focusCount, 0, '背景窗格不得搶焦點');
  controls.focusedPaneRef.current = true;
  controls.document.activeElement = inputElement;
  controls.focusInput(true);
  assert.equal(focusCount, 0, '輸入框已有游標時不重設焦點');
  controls.document.activeElement = body;
  controls.focusInput(true);
  assert.equal(focusCount, 1, '桌面仍會在對話載入後自動聚焦');
  focusCount = 0;
  mobile = true;
  controls.focusInput(true);
  assert.equal(focusCount, 0, '手機閱讀時不自動聚焦，避免展開輸入工具列');
  controls.document.activeElement = { closest: () => null }; // clicked suggestion button
  controls.focusInput();
  assert.equal(focusCount, 1, '使用者明確操作仍可聚焦輸入');

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
