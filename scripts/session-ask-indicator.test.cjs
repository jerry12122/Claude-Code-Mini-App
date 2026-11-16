const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const test = require('node:test');
const vm = require('node:vm');

const root = path.join(__dirname, '../internal/static');
const read = (file) => fs.readFileSync(path.join(root, file), 'utf8');

const atoms = read('js/ui/ui-atoms.js');
const src = atoms.slice(atoms.indexOf('function sessionStatusDotClass'), atoms.indexOf('/** 回傳 session 顯示用的 agent_type'));
const ctx = vm.createContext({});
vm.runInContext(src, ctx);
const dot = (status) => ctx.sessionStatusDotClass({ status });
const label = (status) => ctx.sessionStatusLabel({ status });

test('需要使用者處理的三種狀態共用「？」符號，且不同於執行中', () => {
  for (const st of ['awaiting_input', 'awaiting_confirm', 'awaiting_shell_confirm']) {
    assert.equal(dot(st), 'ra-status-ask', st);
    assert.ok(label(st), `${st} 要有文字標籤`);
  }
  assert.equal(dot('running'), 'ra-status-dot running');
  assert.notEqual(dot('awaiting_input'), dot('running'));
  assert.equal(dot('idle'), '');
});

test('CSS：待處理符號是帶「？」的圓形徽章，與未讀數字徽章不同', () => {
  const html = read('index.html');
  assert.match(html, /\.ra-status-ask::after\s*\{\s*content:\s*'\?'/);
  assert.doesNotMatch(html, /\.ra-status-dot\.awaiting/);
});

test('窄版圖示欄也顯示待處理符號', () => {
  const view = read('js/session/SessionView.js');
  const rail = view.slice(view.indexOf('dirSessions.map((s) => {'));
  assert.match(rail.slice(0, 3500), /ra-status-ask/);
});
