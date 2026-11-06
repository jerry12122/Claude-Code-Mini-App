const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');

const root = path.join(__dirname, '../internal/static');
const tabsSource = fs.readFileSync(path.join(root, 'js/ui/SessionTabs.js'), 'utf8');
const beforeComponent = tabsSource.slice(0, tabsSource.indexOf('function SessionTabBar('));
const frames = [];
let localTarget, survivingTarget, navigationTarget, focused;
const target = name => ({ focus: () => { focused = name; } });
const tabBarRef = { current: { querySelector: () => localTarget } };
const context = vm.createContext({
  requestAnimationFrame: callback => frames.push(callback),
  document: { querySelector: selector => selector.includes('role="tab"') ? survivingTarget : navigationTarget },
});
vm.runInContext(beforeComponent, context);
assert.equal(typeof context.focusSessionTabAfterClose, 'function');
localTarget = target('same group');
survivingTarget = target('other group');
navigationTarget = target('session navigation');
context.focusSessionTabAfterClose(tabBarRef);
assert.equal(focused, undefined, 'focus waits until React has removed the closed tab');
frames.shift()();
assert.equal(focused, 'same group');
context.focusSessionTabAfterClose(tabBarRef);
tabBarRef.current = null; // closing the final tab can unmount its whole group
frames.shift()();
assert.equal(focused, 'other group');
survivingTarget = null;
context.focusSessionTabAfterClose(tabBarRef);
frames.shift()();
assert.equal(focused, 'session navigation');
navigationTarget = null;
context.focusSessionTabAfterClose(tabBarRef);
assert.doesNotThrow(() => frames.shift()(), 'an unmounting app has no focus target');

let revealed = null, scrolled = null, flashed = 0, reduceMotion = false;
const tabNode = id => ({
  dataset: { sessionTabId: id },
  querySelector: () => ({ focus: options => { revealed = { id, options }; } }),
  scrollIntoView: options => { scrolled = { id, options }; },
  animate: () => { flashed++; },
});
context.document.querySelectorAll = () => [tabNode('primary'), tabNode('secondary-hidden')];
context.window = { matchMedia: () => ({ matches: reduceMotion }) };
context.revealSessionTab('secondary-hidden');
assert.equal(revealed, null, 'sidebar focus waits for the selected pane/tab to render');
frames.shift()();
assert.equal(revealed.id, 'secondary-hidden', 'locate the exact tab across both panes');
assert.equal(revealed.options.preventScroll, true);
assert.equal(scrolled.id, 'secondary-hidden');
assert.equal(scrolled.options.inline, 'center');
assert.equal(scrolled.options.behavior, 'smooth');
assert.equal(flashed, 1);
reduceMotion = true;
context.revealSessionTab('secondary-hidden'); // repeated click on an already selected tab still reveals it
frames.shift()();
assert.equal(scrolled.options.behavior, 'auto');
assert.equal(flashed, 1, 'respect reduced motion');
context.revealSessionTab('closed');
assert.doesNotThrow(() => frames.shift()(), 'closed tabs and mobile layouts have no target');

const sessionSource = fs.readFileSync(path.join(root, 'js/session/SessionView.js'), 'utf8');
const sidebarActions = [];
const sidebarContext = vm.createContext({
  isUnread: () => false, onEnter: session => sidebarActions.push(['open', session.id]),
  isWideScreen: true, revealSessionTab: id => sidebarActions.push(['reveal', id]),
});
const enterStart = sessionSource.indexOf('  const handleEnter =');
vm.runInContext(sessionSource.slice(enterStart, sessionSource.indexOf('  const listStatus =', enterStart)) + '\nthis.enter = handleEnter;', sidebarContext);
sidebarContext.enter({ id: 'secondary-hidden' });
assert.deepEqual(sidebarActions, [['open', 'secondary-hidden'], ['reveal', 'secondary-hidden']], 'reveal only after selecting the actual session/pane');

const shared = fs.readFileSync(path.join(root, 'js/ui/ui-atoms.js'), 'utf8');
const quotaContext = vm.createContext({});
const core = fs.readFileSync(path.join(root, 'js/core/core.js'), 'utf8');
const stateStart = core.indexOf('function sessionStateChipClass(');
vm.runInContext(core.slice(stateStart, core.indexOf('\n}', stateStart) + 2), quotaContext);
assert.match(quotaContext.sessionStateChipClass('STREAMING'), /text-sky-200/);
for (const state of ['SHELL_RUNNING', 'SHELL_EXEC', 'UNKNOWN']) {
  assert.match(quotaContext.sessionStateChipClass(state), /text-ra-warn/);
}
for (const name of ['extractMaxQuotaPercent', 'quotaTextColorClass', 'quotaBarTone']) {
  const start = shared.indexOf(`function ${name}(`);
  vm.runInContext(shared.slice(start, shared.indexOf('\n}', start) + 2), quotaContext);
}
for (const [quota, pct, expected] of [
  [{ error: 'unavailable' }, 20, 'text-ra-warn'],
  [{}, 100, 'text-red-200'],
  [{}, 80, 'text-orange-200'],
  [{ stale: true }, 20, 'text-ra-t3'],
  [{}, 20, 'text-gray-400'],
]) {
  assert.equal(quotaContext.quotaBarTone(quota, pct).text, expected);
  assert.equal(quotaContext.quotaTextColorClass(quota, pct + '%'), expected);
}

const chat = fs.readFileSync(path.join(root, 'js/chat/ChatView.js'), 'utf8');
const inputStart = chat.lastIndexOf('<textarea', chat.indexOf('data-chat-input'));
assert.match(chat.slice(inputStart, chat.indexOf('/>', inputStart)), /aria-label="訊息或指令"/);

// Failed authorization delivery must keep the pending tools available for retry.
let canSend = false, cleared = false;
const authorization = vm.createContext({
  permTools: [{ tool_name: 'Bash' }],
  sendOrToast: () => canSend,
  setPermTools: tools => { cleared = tools.length === 0; },
});
const allowStart = chat.indexOf('  const handleAllowOnce =');
const allowEnd = chat.indexOf('  /**', allowStart);
vm.runInContext(chat.slice(allowStart, allowEnd) + '\nglobalThis.handlers = [handleAllowOnce, handleDenyOnce];', authorization);
for (const handler of authorization.handlers) {
  cleared = false;
  canSend = false;
  handler();
  assert.equal(cleared, false, 'failed authorization retains the pending tools');
  canSend = true;
  handler();
  assert.equal(cleared, true, 'successful authorization retains the existing clear behavior');
}
console.log('console usability checks passed');
