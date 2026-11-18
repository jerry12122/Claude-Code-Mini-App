// 系統資源區塊的顯示換算（hooks/systemStats.js 為純函式）。
// 執行：node scripts/system-stats.test.cjs
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');

const source = fs.readFileSync(path.join(__dirname, '../internal/static/js/hooks/systemStats.js'), 'utf8');
const context = vm.createContext({});
vm.runInContext(source, context);
const fmt = context.formatSystemStats;
const GiB = 1024 ** 3;

// 第一次沒有基準：cpu 顯示「量測中」佔位，不能顯示成 0%
let v = fmt({ cpu_percent: null, mem_used_bytes: 6 * GiB, mem_total_bytes: 16 * GiB });
assert.equal(v.cpuPercent, null);
assert.equal(v.cpuText, '—');
assert.equal(v.memText, '6.0 / 16.0 GB');
assert.equal(v.memPercent, 37.5);

v = fmt({ cpu_percent: 12.34, mem_used_bytes: 0.5 * GiB, mem_total_bytes: 2 * GiB });
assert.equal(v.cpuText, '12.3%');
assert.equal(v.cpuPercent, 12.34);
assert.equal(v.memText, '0.5 / 2.0 GB');

// 異常值夾在 0–100，不讓進度條爆版
assert.equal(fmt({ cpu_percent: 130, mem_used_bytes: 9, mem_total_bytes: 3 }).cpuPercent, 100);
assert.equal(fmt({ cpu_percent: -5, mem_used_bytes: 9, mem_total_bytes: 3 }).cpuPercent, 0);
assert.equal(fmt({ cpu_percent: 1, mem_used_bytes: 9, mem_total_bytes: 3 }).memPercent, 100);

// total 為 0 或缺資料：不除以零、不顯示 NaN
v = fmt({ cpu_percent: 1, mem_used_bytes: 0, mem_total_bytes: 0 });
assert.equal(v.memText, '—');
assert.equal(v.memPercent, 0);
v = fmt(null);
assert.equal(v.cpuText, '—');
assert.equal(v.memText, '—');
assert.equal(v.memPercent, 0);

console.log('system stats checks passed');
