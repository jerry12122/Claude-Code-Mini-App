/**
 * DevTools 密技：
 * 在頁面空白區域連續鍵入 "devtool"（不分大小寫），即可呼叫桌面版 DevTools。
 */
(() => {
  const KEY_DEVTOOL = 'devtool';
  const TIMEOUT_MS = 2000;
  let buffer = '';
  let timer = null;

  const isEditable = (el) =>
    Boolean(el?.closest?.('input, textarea, select, [contenteditable="true"]') || el?.isContentEditable);

  const resetBuffer = () => {
    buffer = '';
    if (timer) {
      clearTimeout(timer);
      timer = null;
    }
  };

  const openDevTools = () => {
    const wv = window.chrome?.webview;
    if (typeof wv?.postMessage === 'function') {
      wv.postMessage('open-devtools');
      if (typeof showToast === 'function') showToast('已開啟桌面端 DevTools 🛠️');
    }
  };

  window.addEventListener('keydown', (e) => {
    if (!e || e.defaultPrevented) return;
    if (isEditable(e.target)) return resetBuffer();
    if (e.key === 'F12') return openDevTools();
    if (e.ctrlKey || e.altKey || e.metaKey) return;
    if (e.key === 'Escape') return resetBuffer();

    if (typeof e.key === 'string' && e.key.length === 1) {
      buffer = (buffer + e.key.toLowerCase()).slice(-KEY_DEVTOOL.length);
      clearTimeout(timer);
      timer = setTimeout(resetBuffer, TIMEOUT_MS);

      if (buffer === KEY_DEVTOOL) {
        resetBuffer();
        openDevTools();
      }
    }
  });
})();
