/**
 * 計算選取文字浮動工具列（Selection Quote Popover）的絕對像素座標
 * 並進行水平與垂直螢幕邊界防溢出（Clamping 與 Flip）。
 */
function computePopoverPosition(rect, popoverW = 80, popoverH = 36, viewportW = window.innerWidth, viewportH = window.innerHeight) {
  if (!rect) return { left: 0, top: 0, placement: 'top' };
  const GAP = 8;
  const MARGIN = 12;

  let placement = 'top';
  let top = rect.top - popoverH - GAP;
  if (top < MARGIN) {
    placement = 'bottom';
    top = rect.bottom + GAP;
  }
  top = Math.max(MARGIN, Math.min(top, viewportH - popoverH - MARGIN));

  const centerX = rect.left + rect.width / 2;
  let left = centerX - popoverW / 2;
  left = Math.max(MARGIN, Math.min(left, viewportW - popoverW - MARGIN));

  return { left, top, placement };
}

/**
 * 偵測選取範圍是否位於訊息捲動容器內部，且排除輸入框等表單元件。
 * 若有效則回傳 { text, targetRect }，無效或空白則回傳 null。
 */
function detectMessageSelection(sel, containerEl) {
  if (!sel || sel.isCollapsed || sel.rangeCount === 0) return null;
  const text = (sel.toString ? sel.toString() : '').trim();
  if (!text) return null;
  const range = sel.getRangeAt(0);
  if (!range) return null;
  const common = range.commonAncestorContainer;
  if (!containerEl || !containerEl.contains(common)) return null;
  const ancestorEl = common.nodeType === 1 ? common : common.parentElement;
  if (ancestorEl && ancestorEl.closest && ancestorEl.closest('input, textarea, button, select, [data-chat-input]')) return null;
  const rect = range.getBoundingClientRect ? range.getBoundingClientRect() : null;
  if (!rect || (rect.width === 0 && rect.height === 0)) return null;
  return { text, targetRect: rect };
}


/**
 * 浮動引言工具列元件
 * props:
 *   - targetRect: { top, bottom, left, right, width, height }
 *   - onQuote: () => void
 *   - onClose: () => void
 */
function SelectionQuotePopover({ targetRect, onQuote, onClose }) {
  const popoverRef = React.useRef(null);
  const [pos, setPos] = React.useState(() => (targetRect ? computePopoverPosition(targetRect) : null));

  React.useLayoutEffect(() => {
    if (!targetRect) {
      setPos(null);
      return;
    }
    const el = popoverRef.current;
    const w = el ? el.offsetWidth : 80;
    const h = el ? el.offsetHeight : 36;
    setPos(computePopoverPosition(targetRect, w, h));
  }, [targetRect]);

  React.useEffect(() => {
    const handleKey = (e) => {
      if (e.key === 'Escape' && typeof onClose === 'function') {
        onClose();
      }
    };
    window.addEventListener('keydown', handleKey);
    return () => window.removeEventListener('keydown', handleKey);
  }, [onClose]);

  if (!targetRect || !pos) return null;

  return (
    <div
      ref={popoverRef}
      className="fixed z-[120] pointer-events-auto select-none transition-transform duration-75 ra-fade-in"
      style={{
        left: `${pos.left}px`,
        top: `${pos.top}px`,
      }}
      onMouseDown={(e) => {
        // 防止按下按鈕時造成選取範圍被取消（blur）
        e.preventDefault();
        e.stopPropagation();
      }}
    >
      <button
        type="button"
        onClick={(e) => {
          e.stopPropagation();
          if (typeof onQuote === 'function') onQuote();
        }}
        title="引用所選文字並於輸入框回覆"
        aria-label="引用所選文字回覆"
        className="inline-flex items-center gap-1.5 px-3 py-1.5 rounded-full border border-ra-line-strong bg-ra-raised hover:bg-ra-active text-ra-t1 text-xs font-medium shadow-[0_6px_20px_rgba(0,0,0,0.45)] backdrop-blur-md active:scale-95 transition-all"
      >
        <svg
          xmlns="http://www.w3.org/2000/svg"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          strokeWidth="2"
          strokeLinecap="round"
          strokeLinejoin="round"
          className="w-3.5 h-3.5 text-violet-400 shrink-0"
          aria-hidden="true"
        >
          <path d="M3 21c3 0 7-1 7-8V5c0-1.25-.75-2-2-2H4c-1.25 0-2 .75-2 2v6c0 1.25.75 2 2 2 0 4 1.5 6 3 8z" />
          <path d="M15 21c3 0 7-1 7-8V5c0-1.25-.75-2-2-2h-4c-1.25 0-2 .75-2 2v6c0 1.25.75 2 2 2 0 4 1.5 6 3 8z" />
        </svg>
        <span>引言</span>
      </button>
    </div>
  );
}
