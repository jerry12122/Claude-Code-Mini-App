// Claude Code MiniApp Service Worker
const CACHE_NAME = 'cma-shell-v1';
const PRECACHE_ASSETS = [
  '/',
  '/index.html',
  '/manifest.webmanifest',
  '/favicon.ico',
  '/js/vendor/fonts.css',
  '/js/vendor/telegram-web-app.js',
  '/js/vendor/tailwind.js'
];

function shouldBypass(pathname, headers) {
  if (
    pathname.startsWith('/api/') ||
    pathname.startsWith('/auth/') ||
    pathname.startsWith('/uploads/') ||
    pathname.startsWith('/setup/') ||
    pathname.startsWith('/events') ||
    pathname.includes('/ws')
  ) {
    return true;
  }
  const accept = headers && typeof headers.get === 'function' ? headers.get('accept') : (headers && headers.accept ? headers.accept : '');
  if (accept && accept.includes('text/event-stream')) {
    return true;
  }
  return false;
}

if (typeof self !== 'undefined' && self.addEventListener) {
  self.addEventListener('install', (event) => {
    event.waitUntil(
      caches.open(CACHE_NAME).then((cache) => cache.addAll(PRECACHE_ASSETS)).then(() => self.skipWaiting())
    );
  });

  self.addEventListener('activate', (event) => {
    event.waitUntil(
      caches.keys().then((keys) =>
        Promise.all(keys.filter((k) => k !== CACHE_NAME).map((k) => caches.delete(k)))
      ).then(() => self.clients.claim())
    );
  });

  self.addEventListener('fetch', (event) => {
    if (event.request.method !== 'GET') {
      return;
    }
    const url = new URL(event.request.url);

    // 嚴格旁路：API、WebSocket、SSE 串流一律直接走網路，不進快取
    if (shouldBypass(url.pathname, event.request.headers)) {
      return;
    }

    // 靜態資源：網路優先，失敗則回退快取（Network-first with Cache fallback）
    event.respondWith(
      fetch(event.request)
        .then((response) => {
          if (response.ok && event.request.method === 'GET') {
            const clone = response.clone();
            caches.open(CACHE_NAME).then((cache) => cache.put(event.request, clone));
          }
          return response;
        })
        .catch(() => caches.match(event.request))
    );
  });
}

if (typeof module !== 'undefined' && module.exports) {
  module.exports = { CACHE_NAME, PRECACHE_ASSETS, shouldBypass };
}
