// 檔案瀏覽對話框的純邏輯（hooks/fileBrowser.js：路徑組合、麵包屑、大小格式、請求網址）。
// 執行：node scripts/file-browser.test.cjs
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');

const source = fs.readFileSync(path.join(__dirname, '../internal/static/js/hooks/fileBrowser.js'), 'utf8');
const context = vm.createContext({});
vm.runInContext(source, context);
const { fileBrowserChild, fileBrowserParent, fileBrowserCrumbs, fileBrowserSize, fileBrowserUrl } = context;

assert.equal(fileBrowserChild('', 'a.txt'), 'a.txt');
assert.equal(fileBrowserChild('src', 'a.txt'), 'src/a.txt');
assert.equal(fileBrowserChild('src/ui', 'x'), 'src/ui/x');

assert.equal(fileBrowserParent(''), '');
assert.equal(fileBrowserParent('src'), '');
assert.equal(fileBrowserParent('src/ui/x'), 'src/ui');

assert.deepEqual(JSON.parse(JSON.stringify(fileBrowserCrumbs(''))), [{ label: '根目錄', path: '' }]);
assert.deepEqual(JSON.parse(JSON.stringify(fileBrowserCrumbs('src/ui'))), [
  { label: '根目錄', path: '' },
  { label: 'src', path: 'src' },
  { label: 'ui', path: 'src/ui' },
]);

assert.equal(fileBrowserSize(0), '0 B');
assert.equal(fileBrowserSize(1023), '1023 B');
assert.equal(fileBrowserSize(1536), '1.5 KB');
assert.equal(fileBrowserSize(2 * 1024 * 1024), '2.0 MB');
assert.equal(fileBrowserSize(undefined), '');

// 路徑與 session id 都要編碼；不得出現未編碼的 .. 或 /（交給後端驗證，但前端不能自己拼出歧義）
assert.equal(fileBrowserUrl('abc', 'list', ''), '/sessions/abc/files?path=');
assert.equal(fileBrowserUrl('abc', 'list', 'src/ui'), '/sessions/abc/files?path=src%2Fui');
assert.equal(fileBrowserUrl('abc', 'content', 'a b&c#.txt'), '/sessions/abc/files/content?path=a%20b%26c%23.txt');
assert.equal(fileBrowserUrl('a/b', 'list', ''), '/sessions/a%2Fb/files?path=');

console.log('file browser checks passed');
