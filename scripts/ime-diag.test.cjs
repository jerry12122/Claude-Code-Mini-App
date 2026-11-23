const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');

const sourcePath = path.join(__dirname, '../internal/imediag/recorder.js');
const source = fs.readFileSync(sourcePath, 'utf8');
const sentinels = ['SECRET-DATA', 'SECRET-VALUE', 'SECRET-KEY', 'SECRET-URL', 'SECRET-DOM-ID'];

assert.equal(source.includes('.preventDefault('), false);
assert.equal(source.includes('setInterval'), false);
assert.equal(source.includes('.value'), false);
assert.equal(source.includes('selectionStart'), false);
assert.equal(source.includes('prototype'), false);

const context = vm.createContext({ Date, JSON, Object, Math, Number, console });
vm.runInContext(source, context);
const { installImeDiag, imeDiagAuto } = context;
assert.equal(typeof installImeDiag, 'function');
assert.equal(typeof imeDiagAuto, 'function');

function harness() {
  const listeners = {};
  const input = {
    tagName: 'TEXTAREA',
    isConnected: true,
    id: 'SECRET-DOM-ID',
    value: 'SECRET-VALUE',
    selectionStart: 2,
    selectionEnd: 2,
    isContentEditable: false,
    getBoundingClientRect() {
      return { x: 10, y: 20, width: 30, height: 40 };
    },
  };
  const doc = {
    activeElement: input,
    addEventListener(type, fn) {
      (listeners[type] ||= []).push(fn);
    },
    dispatch(type, event) {
      for (const fn of listeners[type] || []) fn(event);
    },
  };
  const win = {
    innerWidth: 800,
    innerHeight: 600,
    devicePixelRatio: 1.25,
    location: { origin: 'https://owner.test', pathname: '/', href: 'https://owner.test/SECRET-URL' },
    document: doc,
    addEventListener(type, fn) {
      (listeners['win:' + type] ||= []).push(fn);
    },
  };
  win.top = win;
  let posted = [];
  const api = installImeDiag(win, doc, (payload) => posted.push(payload));
  return { listeners, input, doc, win, posted, api };
}

function fire(doc, type, extra) {
  const event = {
    type,
    target: extra.target,
    data: extra.data,
    key: extra.key,
    code: extra.code,
    defaultPrevented: extra.defaultPrevented === true,
    preventDefault() {
      this.defaultPrevented = true;
    },
  };
  doc.dispatch(type, event);
  return event;
}

const originalNow = Date.now;
let clock = 1_000;
Date.now = () => clock;
try {
  const h = harness();
  const down = fire(h.doc, 'keydown', { target: h.input, key: 'SECRET-KEY', code: 'KeyA' });
  assert.equal(down.defaultPrevented, false);
  assert.equal(h.input.value, 'SECRET-VALUE');
  assert.equal(h.input.selectionStart, 2);

  fire(h.doc, 'focusin', { target: h.input });
  fire(h.doc, 'compositionstart', { target: h.input, data: 'SECRET-DATA' });
  fire(h.doc, 'compositionupdate', { target: h.input, data: 'SECRET-DATA' });
  fire(h.doc, 'compositionupdate', { target: h.input, data: 'SECRET-DATA' });
  fire(h.doc, 'compositionend', { target: h.input, data: 'SECRET-DATA' });
  const shift = fire(h.doc, 'keydown', { target: h.input, key: 'Shift', code: 'ShiftLeft' });
  assert.equal(shift.defaultPrevented, false);
  fire(h.doc, 'keyup', { target: h.input, key: 'Shift', code: 'SECRET-KEY' });

  const first = h.api.freeze();
  const kinds = Array.from(first.events, (e) => e.kind).join(',');
  assert.equal(kinds, 'focus,compositionstart,compositionupdate,compositionend,shift,shift');
  assert.equal(first.frozenBeforeExportUI, true);
  const dumped = JSON.stringify(first) + h.posted.join('\n');
  for (const s of sentinels) assert.equal(dumped.includes(s), false, s);
  assert.equal(dumped.includes('ShiftLeft'), false);
  const downShift = first.events[4];
  assert.equal(downShift.phase, 'down');
  assert.equal(downShift.side, 'left');
  assert.equal(downShift.defaultPrevented, false);
  assert.equal(first.events[5].phase, 'up');
  assert.equal(first.events[5].side, 'unspecified');
  assert.equal(Object.hasOwn(downShift, 'key'), false);
  assert.equal(Object.hasOwn(downShift, 'data'), false);

  fire(h.doc, 'focusin', { target: h.input });
  assert.equal(first.events.map((e) => e.kind).includes('focus') , true);
  assert.equal(first.events.length, 6);

  const again = installImeDiag(h.win, h.doc, () => {});
  assert.equal(again.duplicate, true);
  fire(h.doc, 'focusin', { target: h.input });
  const second = h.api.freeze();
  assert.equal(second.events.filter((e) => e.kind === 'focus').length, 3);

  const ring = harness();
  for (let i = 0; i < 300; i++) fire(ring.doc, 'focusin', { target: ring.input });
  const capped = ring.api.freeze();
  assert.equal(capped.events.length, 256);
  assert.equal(capped.events[0].seq, 45);
  assert.equal(capped.events[255].seq, 300);

  clock += 300;
  const paced = harness();
  fire(paced.doc, 'compositionupdate', { target: paced.input, data: 'SECRET-DATA' });
  fire(paced.doc, 'compositionupdate', { target: paced.input, data: 'SECRET-DATA' });
  clock += 300;
  fire(paced.doc, 'compositionupdate', { target: paced.input, data: 'SECRET-DATA' });
  assert.equal(paced.api.freeze().events.length, 2);

  const winEv = harness();
  winEv.doc.hasFocus = () => false;
  winEv.doc.activeElement = winEv.input;
  for (const fn of winEv.listeners['win:blur'] || []) fn({ type: 'blur', target: winEv.win, key: 'SECRET-KEY' });
  for (const fn of winEv.listeners['win:focus'] || []) fn({ type: 'focus', target: winEv.win });
  const dumpedWin = JSON.stringify(winEv.api.freeze());
  assert.equal(dumpedWin.includes('"kind":"window-blur"'), true);
  assert.equal(dumpedWin.includes('"kind":"window-focus"'), true);
  assert.equal(dumpedWin.includes('"documentHasFocus":false'), true);
  assert.equal(dumpedWin.includes('SECRET-VALUE'), false);
  assert.equal(dumpedWin.includes('SECRET-DOM-ID'), false);
  assert.equal(dumpedWin.includes('SECRET-KEY'), false);
  assert.equal(winEv.input.value, 'SECRET-VALUE');
  assert.equal(winEv.input.selectionStart, 2);
} finally {
  Date.now = originalNow;
}

function page(partial) {
  const listeners = [];
  const win = {
    location: { origin: 'https://owner.test', pathname: '/', href: 'https://owner.test/SECRET-URL' },
    document: { addEventListener() { listeners.push('doc'); } },
    addEventListener() { listeners.push('win'); },
    __imeDiagOwner: 'https://owner.test',
    chrome: { webview: { postMessage() {} } },
    ...partial,
  };
  if (!Object.hasOwn(partial, 'top')) win.top = win;
  return { win, listeners };
}

{
  const plain = page({ chrome: undefined });
  assert.equal(imeDiagAuto(plain.win), false);
  assert.equal(plain.listeners.length, 0);
  const evil = page({});
  evil.win.location = { origin: 'https://evil.test', pathname: '/', href: 'https://evil.test/SECRET-URL' };
  assert.equal(imeDiagAuto(evil.win), false);
  const share = page({});
  share.win.location = { origin: 'https://owner.test', pathname: '/share', href: 'https://owner.test/share/SECRET-URL' };
  assert.equal(imeDiagAuto(share.win), false);
  const frame = page({ top: {} });
  assert.equal(imeDiagAuto(frame.win), false);
  const home = page({});
  assert.equal(imeDiagAuto(home.win), true);
  const before = home.listeners.length;
  assert.ok(before > 0);
  assert.equal(imeDiagAuto(home.win), true);
  assert.equal(home.listeners.length, before);
}

console.log('ime-diag harness ok');
