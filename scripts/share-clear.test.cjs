const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const source = fs.readFileSync(path.join(__dirname, '../internal/static/js/share/SharesSection.js'), 'utf8');
const handler = source.slice(source.indexOf('  const clearEnded ='), source.indexOf('  const isActive =')) + '\nthis.clearEnded = clearEnded;';

(async () => {
  let rows = [{ id: 1, status: 'active' }, { id: 2, status: 'revoked' }, { id: 3, status: 'expired' }];
  const requests = [], notifications = [];
  let confirmation = true, loads = 0;
  const context = vm.createContext({
    clearing: false, loadGeneration: { current: 0 },
    window: { confirm: () => confirmation },
    setClearing: value => { context.clearing = value; },
    setShares: update => { rows = update(rows); },
    showToast: (message, options) => notifications.push({ message, options }),
    readApiError: async () => 'clear failed',
    load: async () => { loads++; },
    apiFetch: async (url, options) => { requests.push({ url, options }); return { ok: true, json: async () => ({ cleared: 1 }) }; },
  });
  vm.runInContext(handler, context);
  await context.clearEnded({ id: 2 });
  assert.deepEqual(rows.map(row => row.id), [1, 3]);
  assert.equal(requests[0].url, '/shares/ended/2');
  assert.equal(requests[0].options.method, 'DELETE');
  assert.equal(context.clearing, false);
  assert.equal(loads, 1);
  confirmation = false;
  await context.clearEnded();
  assert.equal(requests.length, 1, 'cancel must not delete anything');
  confirmation = true;
  await context.clearEnded();
  assert.equal(requests[1].url, '/shares/ended');
  assert.deepEqual(rows.map(row => row.id), [1], 'bulk cleanup must keep active shares');
  rows.push({ id: 4, status: 'revoked' });
  context.apiFetch = async () => ({ ok: false });
  await context.clearEnded({ id: 4 });
  assert.deepEqual(rows.map(row => row.id), [1, 4], 'failed deletion must keep the row');
  assert.equal(notifications.at(-1).options.error, true);
  assert.equal(context.clearing, false);
  context.apiFetch = async () => { throw Error('offline'); };
  await context.clearEnded({ id: 4 });
  assert.deepEqual(rows.map(row => row.id), [1, 4]);
  assert.equal(context.clearing, false);
  console.log('share cleanup checks passed');
})().catch(error => { console.error(error); process.exitCode = 1; });
