const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const test = require('node:test');
const vm = require('node:vm');

const source = fs.readFileSync(path.join(__dirname, '../internal/static/js/ui/ui-atoms.js'), 'utf8');
const start = source.indexOf('// 對話框焦點管理');
assert.ok(start >= 0, 'useDialogFocus must exist in ui-atoms.js');

// 假 DOM：只實作 hook 用到的 contains／focus／querySelectorAll／offsetParent
function setup({ openerConnected = true, openerIsBody = false } = {}) {
  const doc = { activeElement: null, listeners: {}, body: { id: 'body' } };
  doc.addEventListener = (type, fn) => { doc.listeners[type] = fn; };
  doc.removeEventListener = (type) => { delete doc.listeners[type]; };
  const el = (name) => ({ name, offsetParent: {}, focus() { doc.activeElement = this; } });
  const items = [el('first'), el('middle'), el('last')];
  const dialog = {
    name: 'dialog',
    focus() { doc.activeElement = this; },
    contains(node) { return node === this || items.includes(node); },
    querySelectorAll: () => items,
  };
  const opener = { name: 'opener', isConnected: openerConnected, focus() { doc.activeElement = this; } };
  doc.activeElement = openerIsBody ? doc.body : opener;
  let cleanup;
  const context = vm.createContext({
    document: doc,
    useEffect: (fn) => { cleanup = fn(); },
  });
  vm.runInContext(source.slice(start) + '\nthis.useDialogFocus = useDialogFocus;', context);
  const run = (active = true) => context.useDialogFocus({ current: dialog }, active);
  const press = (shiftKey) => {
    const e = { key: 'Tab', shiftKey, prevented: false, preventDefault() { this.prevented = true; } };
    doc.listeners.keydown(e);
    return e;
  };
  return { doc, dialog, items, opener, run, press, close: () => cleanup && cleanup() };
}

test('開啟時焦點移到對話框容器，不直接落在第一個控制項', () => {
  const { doc, dialog, run } = setup();
  run();
  assert.equal(doc.activeElement, dialog);
});

test('Tab 在最後一個控制項時繞回第一個；Shift+Tab 在第一個時繞到最後一個', () => {
  const { doc, items, run, press } = setup();
  run();
  doc.activeElement = items[2];
  assert.ok(press(false).prevented);
  assert.equal(doc.activeElement, items[0]);
  assert.ok(press(true).prevented);
  assert.equal(doc.activeElement, items[2]);
});

test('框內中間的 Tab 交給瀏覽器處理；容器上 Shift+Tab 不會跳出對話框', () => {
  const { doc, dialog, items, run, press } = setup();
  run();
  doc.activeElement = items[1];
  assert.ok(!press(false).prevented);
  assert.ok(!press(true).prevented);
  doc.activeElement = dialog;
  assert.ok(press(true).prevented);
  assert.equal(doc.activeElement, items[2]);
});

test('焦點跑到對話框外時，下一次 Tab 把它拉回框內', () => {
  const { doc, opener, items, run, press } = setup();
  run();
  doc.activeElement = opener;
  press(false);
  assert.equal(doc.activeElement, items[0]);
});

test('關閉後把焦點還原到開啟者；開啟者已卸載或是 body 時不搶焦點', () => {
  const normal = setup();
  normal.run();
  normal.close();
  assert.equal(normal.doc.activeElement, normal.opener);
  assert.equal(normal.doc.listeners.keydown, undefined, '關閉後要移除 keydown 監聽');

  const gone = setup({ openerConnected: false });
  gone.run();
  gone.close();
  assert.notEqual(gone.doc.activeElement, gone.opener);

  const body = setup({ openerIsBody: true });
  body.run();
  body.close();
  assert.equal(body.doc.activeElement, body.dialog);
});

test('未開啟時不做任何事', () => {
  const { doc, opener, run } = setup();
  run(false);
  assert.equal(doc.activeElement, opener);
  assert.equal(doc.listeners.keydown, undefined);
});

test('三個自製對話框都接上 useDialogFocus 並設定 ref 與 tabIndex', () => {
  for (const file of ['ui/SettingsModal.js', 'share/ShareModal.js', 'chat/ForwardModal.js']) {
    const text = fs.readFileSync(path.join(__dirname, '../internal/static/js', file), 'utf8');
    assert.match(text, /useDialogFocus\(dialogRef/, `${file} 缺少 useDialogFocus`);
    assert.match(text, /ref=\{dialogRef\}\s+tabIndex=\{-1\}/, `${file} 缺少 ref／tabIndex`);
  }
});
