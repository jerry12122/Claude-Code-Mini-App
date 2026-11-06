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
