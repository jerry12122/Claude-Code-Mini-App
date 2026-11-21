const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');

const file = path.join(__dirname, '../internal/static/js/session/SessionCategories.js');
const src = fs.readFileSync(file, 'utf8').replace(/\r\n/g, '\n');

function slice(from, to) {
  const start = src.indexOf(`function ${from}`);
  const end = src.indexOf(`function ${to}`);
  assert.ok(start >= 0 && end > start, `RED: 合法同目錄拖曳還沒有 ${from}`);
  return src.slice(start, end);
}

const constStart = src.indexOf('const CATEGORY_PIN_GROUP');
const constEnd = src.indexOf('function sessionCategoryDirs');
assert.ok(constStart >= 0 && constEnd > constStart, '分類目錄常數不在預期位置');
const sandbox = { String, Set, Map, Promise };
vm.createContext(sandbox);
vm.runInContext(
  `${src.slice(constStart, constEnd)}\n${slice('sessionById', 'SessionDirectoryGroups')}`,
  sandbox,
);

const categories = [
  { id: 'cat-a', work_dir: 'A', name: 'Review' },
  { id: 'cat-b', work_dir: 'B', name: 'Review' },
];
const loose = { id: 's-loose', work_dir: 'A', category_id: null, pinned: false };
const placed = { id: 's-placed', work_dir: 'A', category_id: 'cat-a', pinned: true };
const other = { id: 's-other', work_dir: 'B', category_id: null, pinned: false };

const plan = (session, targetDir, targetCategoryId, guest = false) => sandbox.resolveCategoryDrop({
  guest, session, categories, targetDir, targetCategoryId,
});

assert.deepEqual(plan(loose, 'A', 'cat-a').action, 'assign');
assert.equal(plan(loose, 'A', 'cat-a').categoryId, 'cat-a');
assert.equal(plan(placed, 'A', null).action, 'assign');
assert.equal(plan(placed, 'A', null).categoryId, null);
assert.equal(plan(placed, 'A', 'cat-a').action, 'noop');
assert.equal(plan(loose, 'A', null).action, 'noop');
assert.equal(plan(loose, 'B', 'cat-b').action, 'reject');
assert.equal(plan(loose, 'B', 'cat-b').reason, 'cross-directory');
assert.equal(plan(other, 'A', 'cat-a').reason, 'cross-directory');
assert.equal(plan(loose, '釘選', null).action, 'ignore');
assert.equal(plan(loose, 'Telegram 聊天室', null).action, 'ignore');
assert.equal(plan(loose, '（未設定工作目錄）', null).action, 'ignore');
assert.equal(plan({ id: 's-tg', work_dir: '', agent_type: 'telegram' }, 'A', 'cat-a').action, 'ignore');
assert.equal(plan({ id: 's-empty', work_dir: '  ' }, 'A', 'cat-a').action, 'ignore');
assert.equal(plan(null, 'A', 'cat-a').action, 'ignore');
assert.equal(plan(loose, 'A', 'missing').reason, 'foreign');
assert.equal(plan(loose, 'A', 'cat-b').reason, 'foreign');
assert.equal(plan(loose, 'A', 'cat-a', true).reason, 'guest');

const groups = [['釘選', [placed]], ['A', [loose]]];
assert.equal(sandbox.sessionById(groups, 's-placed').pinned, true);
assert.equal(sandbox.resolveCategoryDrop({
  guest: false,
  session: sandbox.sessionById(groups, 's-placed'),
  categories,
  targetDir: 'A',
  targetCategoryId: 'cat-a',
}).action, 'noop');

(async () => {
  let calls = 0;
  await sandbox.settleCategoryAssignment(() => { calls += 1; }, () => { calls += 10; });
  assert.equal(calls, 11);
  let reloaded = 0;
  await assert.rejects(
    sandbox.settleCategoryAssignment(() => Promise.reject(new Error('歸類失敗')), () => { reloaded += 1; }),
    /歸類失敗/,
  );
  assert.equal(reloaded, 1);
})().catch((err) => {
  console.error(err);
  process.exit(1);
});

assert.match(src, /JSON\.stringify\(\{ category_id: categoryId \}\)/);
assert.equal(src.includes('manual_unread'), false);
assert.match(src, /function sessionCategoryMenuItems/);
assert.match(src, /放到未分類/);
assert.match(src, /settleCategoryAssignment/);
