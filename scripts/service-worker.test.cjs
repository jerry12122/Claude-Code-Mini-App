const assert = require('node:assert/strict');
const path = require('node:path');
const sw = require(path.join(__dirname, '../internal/static/sw.js'));

// 1. 驗證快取資產包含核心靜態檔
assert.ok(Array.isArray(sw.PRECACHE_ASSETS), 'PRECACHE_ASSETS 必須是陣列');
for (const required of ['/', '/index.html', '/manifest.webmanifest', '/favicon.ico']) {
  assert.ok(sw.PRECACHE_ASSETS.includes(required), `PRECACHE_ASSETS 缺少必要檔案: ${required}`);
}

// 2. 驗證路由旁路規則（嚴格放行 API 與即時通訊）
const bypassPaths = [
  '/api/sessions',
  '/api/events',
  '/api/auth/web-login',
  '/auth/login',
  '/uploads/screenshot.png',
  '/setup/status',
  '/sessions/abc/ws',
  '/logs/ws',
  '/events',
];

for (const p of bypassPaths) {
  assert.equal(sw.shouldBypass(p, null), true, `路徑 ${p} 必須被旁路（不攔截快取）`);
}

// 驗證 SSE 標頭旁路
assert.equal(sw.shouldBypass('/any-stream', { accept: 'text/event-stream' }), true, 'text/event-stream 必須被旁路');
assert.equal(sw.shouldBypass('/any-stream', { get: (k) => (k === 'accept' ? 'text/event-stream' : '') }), true, 'Headers 物件 text/event-stream 必須被旁路');

// 3. 靜態資源不得被旁路（必須進入快取邏輯）
const staticPaths = [
  '/',
  '/index.html',
  '/manifest.webmanifest',
  '/favicon.ico',
  '/js/app.js',
  '/js/vendor/tailwind.js',
];

for (const p of staticPaths) {
  assert.equal(sw.shouldBypass(p, null), false, `靜態路徑 ${p} 不得被旁路`);
}

console.log('Service Worker bypass & precache checks passed');
