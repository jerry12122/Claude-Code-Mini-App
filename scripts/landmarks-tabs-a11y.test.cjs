const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const test = require('node:test');

const read = (file) => fs.readFileSync(path.join(__dirname, '../internal/static/js', file), 'utf8');

test('分頁列：tablist 子層只有 tab，關閉鈕僅供指標，鍵盤改用 Delete 關閉', () => {
  const tabs = read('ui/SessionTabs.js');
  assert.match(tabs, /aria-keyshortcuts="Delete"/);
  assert.match(tabs, /e\.key === 'Delete'[\s\S]{0,160}onCloseTab\?\.\(tabId\)/);
  const close = tabs.slice(tabs.indexOf('// tablist 的子層只能是 tab'));
  assert.match(close.slice(0, 300), /tabIndex=\{-1\}\s+aria-hidden="true"/);
});

test('手機版聊天畫面外層有 main 地標，寬螢幕沿用自己的 main', () => {
  const app = read('app.js');
  assert.match(app, /isWideScreen \? renderAuthedLayout\(\) : <main className="h-app">\{renderAuthedLayout\(\)\}<\/main>/);
  assert.equal((app.match(/<main\b/g) || []).length, 3, '登入頁、桌面版、手機版各一個 main');
});
