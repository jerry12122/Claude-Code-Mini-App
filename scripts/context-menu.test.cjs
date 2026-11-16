const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const root = path.join(__dirname, '../internal/static/js');
const read = name => fs.readFileSync(path.join(root, name), 'utf8');
const timers = new Map();
let nextTimer = 0, selection = null, opened = [], haptics = 0;
const context = vm.createContext({
  window: { getSelection: () => selection },
  useRef: value => ({ current: value }), useCallback: callback => callback,
  useEffect: callback => { callback(); },
  setTimeout: (callback, delay) => { const id = ++nextTimer; timers.set(id, { callback, delay }); return id; },
  clearTimeout: id => timers.delete(id), hapticTap: () => haptics++,
});
const menuSource = read('ui/ContextMenu.js');
vm.runInContext(menuSource.slice(0, menuSource.indexOf('function ContextMenu(')), context);
const actionsSource = read('chat/MessageActions.js');
vm.runInContext(actionsSource.slice(0, actionsSource.indexOf('function MessageActionsHost(')), context);
vm.runInContext(read('hooks/tabGroups.js'), context);
const tabsSource = read('ui/SessionTabs.js');
vm.runInContext(tabsSource.slice(0, tabsSource.indexOf('function SessionTabBar(')), context);
const plain = value => JSON.parse(JSON.stringify(value));

assert.deepEqual(plain(context.contextMenuPosition(100, 200, 240, 180, 1000, 800)), { left: 100, top: 200 });
assert.deepEqual(plain(context.contextMenuPosition(990, 790, 240, 180, 1000, 800)), { left: 752, top: 612 });
assert.deepEqual(plain(context.contextMenuPosition(-20, -30, 240, 180, 1000, 800)), { left: 8, top: 8 });
const items = [{ label: 'copy' }, { separator: true }, { label: 'forward', disabled: true }, { label: 'quote' }];
assert.equal(context.contextMenuNext(items, 0, 'ArrowDown'), 3);
assert.equal(context.contextMenuNext(items, 3, 'ArrowDown'), 0);
assert.equal(context.contextMenuNext(items, 0, 'ArrowUp'), 3);
assert.equal(context.contextMenuNext(items, -1, 'ArrowUp'), 3);
assert.equal(context.contextMenuNext(items, 0, 'End'), 3);
assert.equal(context.contextMenuNext(items, 3, 'Home'), 0);
assert.equal(context.contextMenuNext(items, 0, 'Enter'), -1);
assert.equal(context.contextMenuNext([{ disabled: true }], 0, 'ArrowDown'), -1);
assert.deepEqual(plain(context.contextTabCloseIds(['a', 'b', 'c'], 'b', 'right')), ['c']);
assert.deepEqual(plain(context.contextTabCloseIds(['a', 'b', 'c'], 'b', 'others')), ['a', 'c']);
assert.deepEqual(plain(context.contextTabCloseIds(['a'], 'missing', 'others')), []);
let groups = { groups: [{ tabs: ['main'], active: 'main' }, { tabs: ['a', 'b', 'c'], active: 'a' }], focused: 1, ratio: 0.5, orientation: 'horizontal' };
for (const id of context.contextTabCloseIds(groups.groups[1].tabs, 'b', 'others')) {
  groups = context.tabGroupsReducer(groups, { type: 'close', sessionId: id, groupIndex: 1 });
}
assert.deepEqual(plain(groups.groups), [{ tabs: ['main'], active: 'main' }, { tabs: ['b'], active: 'b' }]);

const sessionMap = new Map([
  ['idle', { status: 'idle' }], ['done', { status: 'idle', last_error: 'previous failure' }],
  ['running', { status: 'running' }], ['confirm', { status: 'awaiting_confirm' }],
  ['input', { status: 'awaiting_input' }], ['shell', { status: 'awaiting_shell_confirm' }],
  ['lingering', { status: 'idle', lingering: true }], ['unknown', { status: 'new-state' }],
  ['unset', {}], ['other-pane', { status: 'idle' }],
]);
const paneTabs = [...sessionMap.keys()].filter(id => id !== 'other-pane').concat('missing');
const idleIds = context.idleSessionTabIds(paneTabs, sessionMap);
assert.deepEqual(plain(idleIds), ['idle', 'done'], 'only explicit idle tabs in this pane may close');
assert.deepEqual(plain(context.idleSessionTabIds(['running', 'missing'], sessionMap)), []);
groups = { groups: [{ tabs: ['other-pane'], active: 'other-pane' }, { tabs: ['idle', 'done'], active: 'idle' }], focused: 1 };
for (const id of idleIds) groups = context.tabGroupsReducer(groups, { type: 'close', sessionId: id, groupIndex: 1 });
assert.deepEqual(plain(groups.groups), [{ tabs: ['other-pane'], active: 'other-pane' }], 'closing every idle tab preserves the other pane');

const textNode = value => ({ nodeType: 3, nodeValue: value });
const node = (name, children, skip = false) => ({ nodeType: 1, nodeName: name, childNodes: children, matches: () => skip });
const prose = node('DIV', [node('P', [textNode('Hello')]), node('PRE', [node('BUTTON', [textNode('copy')], true), textNode('go test')])]);
assert.equal(context.readableMessageText(prose).trim(), 'Hello\ngo test');
const element = {
  getBoundingClientRect: () => ({ left: 10, top: 20, right: 300, bottom: 80, width: 290, height: 60 }),
  contains: value => value === 'selected', querySelector: () => prose,
  classList: { add() {}, remove() {} },
};
const target = { closest: () => null };
const event = overrides => ({
  target, currentTarget: element, clientX: 44, clientY: 55, key: '',
  preventDefault() { this.prevented = true; }, stopPropagation() { this.stopped = true; }, ...overrides,
});
const binding = context.useLongPress(payload => opened.push(payload))({ text: '**Hello**', sessionId: 'background' });
let right = event({});
binding.onContextMenu(right);
assert.equal(opened.length, 1);
assert.equal(opened[0].sessionId, 'background');
assert.equal(opened[0].plainText, 'Hello\ngo test');
assert.equal(opened[0].x, 44);
assert.equal(right.prevented, true);
assert.equal(right.stopped, true);
for (const native of [event({ shiftKey: true }), event({ target: { closest: () => ({}) } })]) {
  binding.onContextMenu(native);
  assert.equal(native.prevented, undefined, 'Shift / native input, link, code, image must retain browser menu');
}
const surfaceButton = { getAttribute: () => null, hasAttribute: name => name === 'data-context-surface' };
const plainButton = { getAttribute: () => null, hasAttribute: () => false };
const onButton = button => event({ target: { closest: selector => selector === 'button' ? button : null } });
const beforeButton = opened.length;
const onSurface = onButton(surfaceButton);
binding.onContextMenu(onSurface);
assert.equal(onSurface.prevented, true, 'button marked data-context-surface (tab close X) opens the custom menu');
assert.equal(opened.length, beforeButton + 1);
const onPlain = onButton(plainButton);
binding.onContextMenu(onPlain);
assert.equal(onPlain.prevented, undefined, 'other nested buttons keep the native menu');
assert.match(tabsSource.slice(tabsSource.indexOf('{/* 關閉按鈕 */}'), tabsSource.indexOf('title="關閉分頁"')), /data-context-surface/);
// 滾動：只有「含錨點的捲動容器」或整頁捲動才關選單；選單自己或無關區域（例如串流中的聊天）不關
const anchorEl = {}, menuEl = { contains: node => node === menuInner };
const menuInner = { nodeType: 1, contains: () => false };
assert.equal(context.contextMenuScrollCloses({ nodeType: 9 }, menuEl, anchorEl), true, 'page scroll closes');
assert.equal(context.contextMenuScrollCloses({ nodeType: 1, contains: node => node === anchorEl }, menuEl, anchorEl), true, 'anchor scroller closes');
assert.equal(context.contextMenuScrollCloses({ nodeType: 1, contains: () => false }, menuEl, anchorEl), false, 'unrelated scroller keeps menu');
assert.equal(context.contextMenuScrollCloses(menuInner, menuEl, anchorEl), false, 'scrolling the menu itself keeps it');
// Esc：選單用 capture 階段攔截，才不會讓 app.js 的連按兩次 Esc 關分頁把這次 Esc 算進去
const menuSrc = menuSource.slice(menuSource.indexOf('function ContextMenu('));
assert.match(menuSrc, /window\.addEventListener\('keydown', key, true\)/);
assert.match(menuSrc, /window\.removeEventListener\('keydown', key, true\)/);
// 會話列的「⋯」按鈕右鍵也要開自訂選單
const viewSrc = read('session/SessionView.js');
const dotsAt = viewSrc.indexOf('aria-haspopup="menu"');
assert.ok(dotsAt > 0);
assert.match(viewSrc.slice(dotsAt - 120, dotsAt + 120), /data-context-surface/);
selection = { anchorNode: 'selected', focusNode: 'selected', toString: () => 'Hello' };
binding.onContextMenu(event({}));
assert.equal(opened.at(-1).selectedText, 'Hello');
selection = { anchorNode: 'outside', focusNode: 'selected', toString: () => 'other message' };
right = event({});
binding.onContextMenu(right);
assert.equal(right.prevented, undefined, 'cross-message selection keeps native menu');
selection = null;
const key = event({ key: 'F10', shiftKey: true });
binding.onKeyDown(key);
assert.equal(key.prevented, true);
assert.equal(opened.at(-1).sessionId, 'background');
const beforeTouch = opened.length;
binding.onTouchStart(event({ touches: [{ clientX: 40, clientY: 60 }] }));
binding.onTouchMove(event({ touches: [{ clientX: 80, clientY: 60 }] }));
assert.equal(timers.size, 0, 'scrolling cancels long press and feedback');
assert.equal(opened.length, beforeTouch);
binding.onTouchStart(event({ touches: [{ clientX: 40, clientY: 60 }] }));
const longPress = [...timers.values()].find(timer => timer.delay === 380);
longPress.callback();
assert.equal(opened.at(-1).touch, true);
assert.equal(opened.at(-1).x, 40);
assert.equal(haptics, 1);
const count = opened.length;
binding.onContextMenu(event({}));
assert.equal(opened.length, count, 'touch-generated contextmenu must not open twice');
const end = event({ cancelable: true });
binding.onTouchEnd(end);
assert.equal(end.prevented, true, 'long press must suppress synthetic click');
console.log('context menu checks passed');
