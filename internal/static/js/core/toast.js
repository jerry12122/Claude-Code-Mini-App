// 純 DOM 的使用者回饋工具（toast／複製／觸覺）。不含 React，放在 core 讓 hooks 與呈現層都能向下依賴。
// 原本在 ui/ui-atoms.js，依 docs/spec/frontend-architecture.md 第 2 節移出。

/**
 * 以 document.execCommand('copy') 複製純文字（相容非安全上下文，不使用 navigator.clipboard）。
 */
function copyTextExecCommand(text) {
  const t = text == null ? '' : String(text);
  if (!t) return false;
  let ta;
  try {
    ta = document.createElement('textarea');
    ta.value = t;
    ta.setAttribute('readonly', '');
    ta.style.position = 'fixed';
    ta.style.left = '-9999px';
    ta.style.top = '0';
    document.body.appendChild(ta);
    // 16px 避免 iOS 聚焦時自動放大；iOS 的 select() 對 textarea 不一定生效，需再補 setSelectionRange。
    ta.style.fontSize = '16px';
    ta.focus();
    ta.select();
    ta.setSelectionRange(0, t.length);
    return document.execCommand('copy');
  } catch (e) {
    return false;
  } finally {
    if (ta && ta.parentNode) ta.parentNode.removeChild(ta);
  }
}

/**
 * 複製純文字：安全環境（HTTPS／Telegram）優先用 Clipboard API，失敗再退回 execCommand。
 * 回傳 Promise<boolean>。
 */
async function copyText(text) {
  const t = text == null ? '' : String(text);
  if (!t) return false;
  if (navigator.clipboard && window.isSecureContext) {
    try {
      await navigator.clipboard.writeText(t);
      return true;
    } catch (_) { /* 權限被拒或失去手勢，改走 fallback */ }
  }
  return copyTextExecCommand(t);
}

/** 輕量 Toast（不需 Provider）：畫面下方浮出一行字，預設約 1.6 秒後淡出。
 *  action={label,onClick} 會多一顆可點的按鈕（如「復原」），此時 duration 通常要拉長。 */
function showToast(message, { error = false, action = null, duration = 1600 } = {}) {
  const el = document.createElement('div');
  el.setAttribute('role', 'status');
  const text = document.createElement('span');
  text.textContent = message;
  el.appendChild(text);
  const dismiss = () => {
    el.style.opacity = '0';
    setTimeout(() => { if (el.parentNode) el.parentNode.removeChild(el); }, 220);
  };
  if (action) {
    const btn = document.createElement('button');
    btn.type = 'button';
    btn.textContent = action.label;
    Object.assign(btn.style, { marginLeft: '12px', fontWeight: '600', color: 'oklch(0.8 0.12 275)', cursor: 'pointer' });
    btn.onclick = () => { dismiss(); action.onClick(); };
    el.appendChild(btn);
  }
  Object.assign(el.style, {
    position: 'fixed',
    // popover 的 UA 樣式是 inset:0 + margin:auto（置中），要先清掉才能用 bottom 定位
    inset: 'auto auto calc(88px + env(safe-area-inset-bottom)) 50%',
    margin: '0',
    transform: 'translateX(-50%)',
    maxWidth: '80vw',
    padding: '8px 14px',
    borderRadius: '999px',
    fontSize: '13px',
    lineHeight: '1.3',
    color: error ? 'oklch(0.9 0.08 25)' : 'oklch(0.95 0.01 264)',
    background: error ? 'oklch(0.3 0.1 25 / 0.95)' : 'oklch(0.28 0.02 264 / 0.95)',
    border: '1px solid oklch(0.38 0.02 264)',
    boxShadow: '0 4px 16px rgba(0,0,0,0.4)',
    pointerEvents: action ? 'auto' : 'none',
    opacity: '0',
    transition: 'opacity 0.18s',
  });
  // popover="manual" 讓 toast 進入 top layer，蓋在原生 modal 之上（modern-web-guidance「persistent-toast-notifications」）。
  // 兩個實測過的限制：
  // 1. 必須掛在最上層 modal 底下。掛在 body 時雖然也在 top layer，但 body 已被 modal 設成 inert，動作鈕按不到。
  // 2. 不能只用一般的 fixed 子元素：<dialog> 會成為它的包含區塊並裁切它，toast 落在對話框外就被切掉。
  // modal 關閉時把 toast 移回 body；移動節點會關掉 popover，所以要重新 showPopover()。
  // ponytail: 指南建議 modal 開著時不要顯示 toast；這裡的 toast 多是 modal 內動作的回饋（「已複製連結」），所以仍顯示。
  el.setAttribute('popover', 'manual');
  const show = () => { try { el.showPopover(); } catch (_) { /* 已卸載或不支援 popover：退回一般 fixed 元素 */ } };
  const modals = document.querySelectorAll('dialog:modal');
  const host = modals[modals.length - 1];
  if (host) {
    host.appendChild(el);
    host.addEventListener('close', () => { if (el.parentNode) { document.body.appendChild(el); show(); } }, { once: true });
  } else {
    document.body.appendChild(el);
  }
  show();
  requestAnimationFrame(() => { el.style.opacity = '1'; });
  setTimeout(dismiss, duration);
}

/** 觸覺回饋：Telegram 內用原生 Haptic，其餘退回 vibrate（iOS Safari 不支援則無動作）。 */
function hapticTap() {
  try {
    const h = window.Telegram?.WebApp?.HapticFeedback;
    if (h && typeof h.impactOccurred === 'function') { h.impactOccurred('medium'); return; }
    if (navigator.vibrate) navigator.vibrate(15);
  } catch (_) { /* 無觸覺裝置，忽略 */ }
}
