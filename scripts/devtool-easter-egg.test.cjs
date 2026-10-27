const assert = require('node:assert');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');

const targetFile = path.join(__dirname, '../internal/static/js/devtool-easter-egg.js');
const indexFile = path.join(__dirname, '../internal/static/index.html');
const source = fs.readFileSync(targetFile, 'utf8');

function createFakeWindow({ postMessage } = {}) {
  const postMessages = [];
  const toasts = [];
  const listeners = {};

  const win = {
    addEventListener(evt, fn) {
      if (!listeners[evt]) listeners[evt] = [];
      listeners[evt].push(fn);
    },
    removeEventListener(evt, fn) {
      if (listeners[evt]) {
        listeners[evt] = listeners[evt].filter((h) => h !== fn);
      }
    },
    chrome: {
      webview: {
        postMessage: postMessage || ((m) => postMessages.push(m))
      }
    },
    showToast(msg) {
      toasts.push(msg);
    }
  };

  return { win, postMessages, toasts, listeners };
}

function loadScript(win) {
  const context = vm.createContext({
    window: win,
    showToast: win.showToast,
    clearTimeout: clearTimeout,
    setTimeout: setTimeout,
    console: console
  });
  vm.runInContext(source, context);
  return win;
}

// ── 1. index.html 驗證 ──────────────────────────────────────────
{
  const html = fs.readFileSync(indexFile, 'utf8');
  const readyIdx = html.indexOf('src="./js/wails-runtime-ready.js"');
  const easterEggIdx = html.indexOf('src="./js/devtool-easter-egg.js"');

  assert.ok(readyIdx > 0, 'index.html 必須載入 wails-runtime-ready.js');
  assert.ok(easterEggIdx > 0, 'index.html 必須載入 devtool-easter-egg.js');
  assert.ok(readyIdx < easterEggIdx, 'wails-runtime-ready.js 必須在 devtool-easter-egg.js 之前載入');
  assert.ok(easterEggIdx < html.indexOf('</head>'), 'devtool-easter-egg.js 必須在 <head> 內宣告');
}

// ── 2. 空白區域鍵入 "devtool" 呼叫 DevTools ──────────────────────
{
  const { win, postMessages, toasts, listeners } = createFakeWindow();
  loadScript(win);

  const handler = listeners['keydown'][0];
  assert.strictEqual(typeof handler, 'function', '應註冊 keydown 監聽器');

  const fire = (key, target = { tagName: 'DIV' }) => {
    handler({ key, target, defaultPrevented: false });
  };

  'devtool'.split('').forEach((k) => fire(k));

  assert.deepStrictEqual(postMessages, ['open-devtools'], '應發送 open-devtools 給 WebView2');
  assert.strictEqual(toasts.length, 1);
  assert.ok(toasts[0].includes('DevTools'));
}

// ── 3. 大小寫不敏感（DevTool）─────────────────────────────────
{
  const { win, postMessages, listeners } = createFakeWindow();
  loadScript(win);

  const handler = listeners['keydown'][0];
  'DevTool'.split('').forEach((k) => {
    handler({ key: k, target: { tagName: 'DIV' }, defaultPrevented: false });
  });

  assert.deepStrictEqual(postMessages, ['open-devtools'], '大小寫混合應可正常觸發');
}

// ── 4. 可編輯元素內不應觸發 ────────────────────────────────────
{
  const { win, postMessages, listeners } = createFakeWindow();
  loadScript(win);

  const handler = listeners['keydown'][0];
  const inputEl = {
    tagName: 'INPUT',
    closest(sel) { return sel.includes('input') ? this : null; }
  };

  'devtool'.split('').forEach((k) => {
    handler({ key: k, target: inputEl, defaultPrevented: false });
  });

  assert.deepStrictEqual(postMessages, [], '在 input 內輸入不應觸發密技');

  const contentEditableEl = {
    tagName: 'DIV',
    isContentEditable: true
  };
  'devtool'.split('').forEach((k) => {
    handler({ key: k, target: contentEditableEl, defaultPrevented: false });
  });

  assert.deepStrictEqual(postMessages, [], '在 contenteditable 內輸入不應觸發密技');
}

// ── 5. F12 直通 ────────────────────────────────────────────────
{
  const { win, postMessages, listeners } = createFakeWindow();
  loadScript(win);

  listeners['keydown'][0]({ key: 'F12', target: { tagName: 'BODY' }, defaultPrevented: false });
  assert.deepStrictEqual(postMessages, ['open-devtools']);
}

// ── 6. Escape 清空緩衝區 ───────────────────────────────────────
{
  const { win, postMessages, listeners } = createFakeWindow();
  loadScript(win);

  const handler = listeners['keydown'][0];
  'dev'.split('').forEach((k) => handler({ key: k, target: { tagName: 'BODY' }, defaultPrevented: false }));
  handler({ key: 'Escape', target: { tagName: 'BODY' }, defaultPrevented: false });
  'tool'.split('').forEach((k) => handler({ key: k, target: { tagName: 'BODY' }, defaultPrevented: false }));

  assert.deepStrictEqual(postMessages, []);
}

// ── 7. 組合鍵（Ctrl/Alt/Meta）忽略 ─────────────────────────────
{
  const { win, postMessages, listeners } = createFakeWindow();
  loadScript(win);

  const handler = listeners['keydown'][0];
  'dev'.split('').forEach((k) => handler({ key: k, target: { tagName: 'BODY' }, defaultPrevented: false }));
  handler({ key: 'c', ctrlKey: true, target: { tagName: 'BODY' }, defaultPrevented: false });
  'tool'.split('').forEach((k) => handler({ key: k, target: { tagName: 'BODY' }, defaultPrevented: false }));

  assert.deepStrictEqual(postMessages, ['open-devtools']);
}

console.log('devtool-easter-egg lean tests passed');
