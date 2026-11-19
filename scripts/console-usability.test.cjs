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

// Revealing either zone must not scroll its sibling, the pane, or the document.
let zoneScroll;
const zone = {
  scrollLeft: 100,
  getBoundingClientRect: () => ({ left: 200, right: 400 }),
  scrollTo: options => { zoneScroll = options; },
};
const zoneTab = {
  closest: () => zone,
  getBoundingClientRect: () => ({ left: 380, right: 524 }),
  scrollIntoView: () => assert.fail('must scroll only the local zone'),
};
context.scrollSessionTabIntoView(zoneTab);
assert.equal(zoneScroll.left, 224);
context.scrollSessionTabIntoView(zoneTab, { inline: 'center', behavior: 'smooth' });
assert.equal(zoneScroll.left, 252);
assert.equal(zoneScroll.behavior, 'smooth');

const sessionSource = fs.readFileSync(path.join(root, 'js/session/SessionView.js'), 'utf8');
const sidebarActions = [];
const sidebarContext = vm.createContext({
  onEnter: session => sidebarActions.push(['open', session.id]),
  isWideScreen: true, revealSessionTab: id => sidebarActions.push(['reveal', id]),
});
vm.runInContext(fs.readFileSync(path.join(root, 'js/hooks/messageRows.js'), 'utf8'), sidebarContext);
const enterStart = sessionSource.indexOf('  const handleEnter =');
vm.runInContext(sessionSource.slice(enterStart, sessionSource.indexOf('  const listStatus =', enterStart)) + '\nthis.enter = handleEnter;', sidebarContext);
sidebarContext.enter({ id: 'secondary-hidden' });
assert.deepEqual(sidebarActions, [['open', 'secondary-hidden'], ['reveal', 'secondary-hidden']], 'reveal only after selecting the actual session/pane');
sidebarContext.setSessions = () => { throw new Error('Navigation alone must not acknowledge unread results'); };
assert.doesNotThrow(() => sidebarContext.enter({ id: 'unread', unread_count: 1 }), 'wait for the visible chat to acknowledge loaded results');
sidebarContext.sessions = [{ id: 'current', unread_count: 1 }];
sidebarContext.activeSessionId = 'current';
sidebarContext.useMemo = fn => fn();
const countStart = sessionSource.indexOf('  const unreadCount =');
vm.runInContext(sessionSource.slice(countStart, sessionSource.indexOf('  const hasUnread', countStart)) + '\nthis.count = unreadCount;', sidebarContext);
assert.equal(sidebarContext.count, 1, 'an active chat can still have unread results while the app is in the background');

// quota 的顏色函式在 ui 層，數值計算在 hooks/quotaMetrics.js（見 docs/spec/frontend-architecture.md）
const shared = fs.readFileSync(path.join(root, 'js/ui/uiAtoms.js'), 'utf8')
  + '\n' + fs.readFileSync(path.join(root, 'js/hooks/quotaMetrics.js'), 'utf8');
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
// actionLock 是元件裡的 ref。成功送出後面板會收起，下一個請求由 effect 解鎖；
// 這段切片沒有跑 effect，所以每個 handler 開始時把鎖復原，代表新的請求。
let canSend = false, cleared = false, sends = 0;
const authorization = vm.createContext({
  permTools: [{ tool_name: 'Bash' }],
  actionLock: { current: false },
  sendOrToast: () => {
    if (!canSend) return false;
    sends += 1;
    return true;
  },
  setPermTools: tools => { cleared = tools.length === 0; },
});
const actions = fs.readFileSync(path.join(root, 'js/chat/SessionActionCards.js'), 'utf8');
const allowStart = actions.indexOf('  const handleAllowOnce =');
const allowEnd = actions.indexOf('  /**', allowStart);
vm.runInContext(actions.slice(allowStart, allowEnd) + '\nglobalThis.handlers = [handleAllowOnce, handleDenyOnce];', authorization);
for (const handler of authorization.handlers) {
  authorization.actionLock.current = false;
  cleared = false;
  canSend = false;
  sends = 0;
  handler();
  assert.equal(cleared, false, 'failed authorization retains the pending tools');
  assert.equal(sends, 0, 'a disconnected click does not count as sent');
  assert.equal(authorization.actionLock.current, false, 'failed send releases the lock');
  canSend = true;
  handler();
  handler();
  assert.equal(sends, 1, 'one request is sent once when clicked twice synchronously');
  assert.equal(cleared, true, 'successful authorization retains the existing clear behavior');
  assert.equal(authorization.actionLock.current, true, 'the same request stays locked after the first send');
}

const shell = vm.createContext({
  canAct: true,
  connected: true,
  shellConfirmPending: false,
  actionLock: { current: false },
  setShellConfirmPending: (value) => { shell.shellConfirmPending = value; },
  sendOrToast: () => false,
});
const shellStart = actions.indexOf('  const sendShellConfirm =');
const shellEnd = actions.indexOf('  const handleShellApprove =', shellStart);
vm.runInContext(actions.slice(shellStart, shellEnd) + '\nthis.sendShell = sendShellConfirm;', shell);
shell.connected = false;
shell.sendShell('shell_allow_once');
assert.equal(shell.actionLock.current, false, 'disconnect does not lock or clear the shell request');
assert.equal(shell.shellConfirmPending, false);
shell.connected = true;
shell.sendOrToast = () => false;
shell.sendShell('shell_deny');
assert.equal(shell.actionLock.current, false, 'a failed shell send releases the lock and keeps the request');
assert.equal(shell.shellConfirmPending, false);
let shellSends = 0;
shell.sendOrToast = (message) => { shellSends += 1; shell.last = message; return true; };
shell.sendShell('shell_allow_once');
shell.sendShell('shell_allow_once');
assert.equal(shellSends, 1, 'the same shell request is sent once');
assert.equal(shell.last.type, 'shell_allow_once');
assert.equal(shell.actionLock.current, true);
assert.equal(shell.shellConfirmPending, true);
const timers = [];
shell.setTimeout = (fn, ms) => { timers.push({ fn, ms }); return 1; };
shell.clearTimeout = () => {};
shell.useEffect = (fn) => { fn(); };
const timeoutStart = actions.indexOf('  // shell-confirm-timeout');
const timeoutEnd = actions.indexOf('  return (<>', timeoutStart);
vm.runInContext(actions.slice(timeoutStart, timeoutEnd), shell);
assert.equal(timers.length, 1);
assert.equal(timers[0].ms, 10000);
timers[0].fn();
assert.equal(shell.shellConfirmPending, false, 'timeout clears the pending flag');
assert.equal(shell.actionLock.current, false, 'timeout releases the action lock when state and request stay unchanged');
shell.sendShell('shell_allow_once');
assert.equal(shellSends, 2, 'after the timeout the same card can be sent again');
console.log('console usability checks passed');
