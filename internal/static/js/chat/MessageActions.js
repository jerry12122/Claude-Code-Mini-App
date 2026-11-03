/** 訊息長按：浮動小選單（複製；非自己的訊息再加轉發）
 *  樣式參考 Claude iOS：背景變暗，被按的訊息保持亮著，選單浮在訊息附近，頂端顯示時間。
 *  觸控裝置上 .msg-lp 會關掉原生選字與 callout（見 index.html），由這裡接手。
 */

const LONG_PRESS_MS = 380;
const LONG_PRESS_MOVE_TOL = 8;
// 按下超過這個時間才顯示「按住」回饋，避免一般點擊或捲動起手時閃一下。
const PRESS_FEEDBACK_MS = 120;
const MENU_GAP = 10;
const MENU_EDGE = 12;

/**
 * 長按偵測。因為訊息是 map 出來的，hook 不能放進迴圈，
 * 這裡共用一組 ref（同一時間只會有一根手指在長按），回傳 bind(payload) 產生各訊息的事件屬性。
 * 觸發時會把觸控座標、訊息位置與 DOM 一併帶進 payload，供選單定位與「抬起」訊息使用。
 */
function useLongPress(onTrigger) {
  const timerRef = useRef(null);
  const startRef = useRef(null);
  const firedRef = useRef(false);
  const lastTouchAtRef = useRef(0);
  const feedbackTimerRef = useRef(null);
  const pressedElRef = useRef(null);
  const onTriggerRef = useRef(onTrigger);
  useEffect(() => { onTriggerRef.current = onTrigger; }, [onTrigger]);

  const clearFeedback = useCallback(() => {
    if (feedbackTimerRef.current) { clearTimeout(feedbackTimerRef.current); feedbackTimerRef.current = null; }
    if (pressedElRef.current) { pressedElRef.current.classList.remove('msg-pressing'); pressedElRef.current = null; }
  }, []);
  const clear = useCallback(() => {
    if (timerRef.current) { clearTimeout(timerRef.current); timerRef.current = null; }
    clearFeedback();
  }, [clearFeedback]);
  useEffect(() => clear, [clear]);

  return useCallback((payload) => ({
    onTouchStart: (e) => {
      lastTouchAtRef.current = Date.now();
      firedRef.current = false;
      clear();
      // 按在按鈕／連結／輸入元件上時不接管，維持原本點擊行為。
      if (e.target.closest && e.target.closest('button, a, input, textarea, select, .code-copy-btn')) return;
      const t = e.touches && e.touches[0];
      if (!t) return;
      startRef.current = { x: t.clientX, y: t.clientY };
      const el = e.currentTarget;
      feedbackTimerRef.current = setTimeout(() => {
        feedbackTimerRef.current = null;
        if (el && el.classList) { el.classList.add('msg-pressing'); pressedElRef.current = el; }
      }, PRESS_FEEDBACK_MS);
      timerRef.current = setTimeout(() => {
        timerRef.current = null;
        firedRef.current = true;
        clearFeedback();
        hapticTap();
        const s = startRef.current || { x: 0, y: 0 };
        const r = el.getBoundingClientRect();
        onTriggerRef.current({
          ...payload,
          x: s.x,
          y: s.y,
          rect: { left: r.left, top: r.top, right: r.right, bottom: r.bottom, width: r.width, height: r.height },
          el,
        });
      }, LONG_PRESS_MS);
    },
    onTouchMove: (e) => {
      if (!timerRef.current) return;
      const t = e.touches && e.touches[0];
      const s = startRef.current;
      if (!t || !s) return;
      // 手指移動超過門檻視為捲動，取消長按。
      if (Math.abs(t.clientX - s.x) > LONG_PRESS_MOVE_TOL || Math.abs(t.clientY - s.y) > LONG_PRESS_MOVE_TOL) clear();
    },
    onTouchEnd: (e) => {
      clear();
      // 長按已觸發時吃掉後續合成的 click，避免誤點到圖片或連結。
      if (firedRef.current && e.cancelable) e.preventDefault();
    },
    onTouchCancel: clear,
    // Android 長按會補發 contextmenu；只在剛觸控過時攔截，桌面滑鼠右鍵維持原生選單。
    onContextMenu: (e) => {
      if (Date.now() - lastTouchAtRef.current < 1500) e.preventDefault();
    },
  }), [clear, clearFeedback]);
}

/** 訊息夠短、且完整在畫面內時，才把它「抬」到遮罩上方；否則只用觸控點定位選單。 */
function canLiftMessage(rect) {
  if (!rect) return false;
  const vh = window.innerHeight;
  return rect.height <= vh * 0.5 && rect.top >= MENU_EDGE && rect.bottom <= vh - MENU_EDGE;
}

/** 完整時間（選單頂端用）：2026/9/29 00:50 */
function formatMenuTime(raw) {
  const d = typeof parseMessageCreatedAt === 'function' ? parseMessageCreatedAt(raw) : null;
  if (!d) return '';
  const pad = (n) => String(n).padStart(2, '0');
  return `${d.getFullYear()}/${d.getMonth() + 1}/${d.getDate()} ${pad(d.getHours())}:${pad(d.getMinutes())}`;
}

const MENU_ICON_PROPS = {
  xmlns: 'http://www.w3.org/2000/svg',
  viewBox: '0 0 24 24',
  fill: 'none',
  stroke: 'currentColor',
  strokeWidth: 1.8,
  strokeLinecap: 'round',
  strokeLinejoin: 'round',
  className: 'w-[18px] h-[18px] shrink-0 opacity-80',
  'aria-hidden': true,
};
function IconCopy() {
  return (<svg {...MENU_ICON_PROPS}><rect x="9" y="9" width="11" height="11" rx="2.5" /><path d="M5 15V6.5A2.5 2.5 0 0 1 7.5 4H15" /></svg>);
}
function IconForward() {
  return (<svg {...MENU_ICON_PROPS}><path d="M14 5l6 6-6 6" /><path d="M20 11H9a5 5 0 0 0-5 5v2" /></svg>);
}

/**
 * 浮動動作選單（複製；非自己的訊息再加轉發）。
 * payload: { text, role, canForward, messageKey, createdAt, x, y, rect, el }
 * 背景變暗＋輕微模糊；訊息夠短時複製一份 DOM 疊在遮罩上方，看起來像被抬起。
 */
function MessageContextMenu({ payload, onClose, onCopy, onForward }) {
  const menuRef = useRef(null);
  const liftRef = useRef(null);
  const [pos, setPos] = useState(null);
  const lifted = !!payload && canLiftMessage(payload.rect);

  // 量測選單尺寸後再定位（第一次 render 先隱藏，避免閃到左上角）。
  useLayoutEffect(() => {
    if (!payload) { setPos(null); return; }
    const menu = menuRef.current;
    if (!menu) return;
    const vw = window.innerWidth;
    const vh = window.innerHeight;
    const mw = menu.offsetWidth;
    const mh = menu.offsetHeight;
    const r = payload.rect;
    const isUser = payload.role === 'user';

    let top;
    let above = true;
    if (lifted) {
      if (r.top - MENU_GAP - mh >= MENU_EDGE) top = r.top - MENU_GAP - mh;
      else { above = false; top = r.bottom + MENU_GAP; }
    } else if (payload.y - 16 - mh >= MENU_EDGE) {
      top = payload.y - 16 - mh;
    } else {
      above = false;
      top = payload.y + 28;
    }
    top = Math.max(MENU_EDGE, Math.min(top, vh - mh - MENU_EDGE));

    let left;
    if (lifted) left = isUser ? r.right - mw : r.left;
    else left = payload.x - mw / 2;
    left = Math.max(MENU_EDGE, Math.min(left, vw - mw - MENU_EDGE));

    setPos({ left, top, origin: `${isUser ? 'right' : 'left'} ${above ? 'bottom' : 'top'}` });
  }, [payload, lifted]);

  // 抬起的訊息：複製一份目前的 DOM 疊在遮罩上，保持原寬度與位置。
  useLayoutEffect(() => {
    const box = liftRef.current;
    if (!box) return undefined;
    box.textContent = '';
    if (!payload || !lifted || !payload.el) return undefined;
    const clone = payload.el.cloneNode(true);
    clone.classList.remove('msg-lp', 'msg-pressing');
    clone.style.maxWidth = 'none';
    clone.style.width = `${payload.rect.width}px`;
    clone.style.margin = '0';
    box.appendChild(clone);
    return () => { box.textContent = ''; };
  }, [payload, lifted]);

  useEffect(() => {
    if (!payload) return undefined;
    const onKey = (e) => { if (e.key === 'Escape') onClose(); };
    window.addEventListener('keydown', onKey);
    window.addEventListener('resize', onClose);
    return () => {
      window.removeEventListener('keydown', onKey);
      window.removeEventListener('resize', onClose);
    };
  }, [payload, onClose]);

  if (!payload) return null;

  const itemCls = 'w-full flex items-center justify-between gap-8 px-4 py-3 text-[16px] text-left text-ra-t1 active:bg-ra-active disabled:opacity-35';
  const timeText = formatMenuTime(payload.createdAt);

  return (
    <div
      className="fixed inset-0 z-[150] bg-black/40 backdrop-blur-[3px] overscroll-none touch-none"
      onClick={onClose}
      onContextMenu={(e) => e.preventDefault()}
      role="dialog"
      aria-modal="true"
      aria-label="訊息操作"
    >
      {lifted ? (
        <div
          ref={liftRef}
          className="fixed pointer-events-none"
          style={{ left: payload.rect.left, top: payload.rect.top, width: payload.rect.width }}
        />
      ) : null}
      <div
        ref={menuRef}
        className="msg-pop fixed min-w-[210px] rounded-[22px] border border-ra-line-strong bg-ra-raised shadow-[0_12px_40px_rgba(0,0,0,0.5)] overflow-hidden"
        style={{
          left: pos ? pos.left : 0,
          top: pos ? pos.top : 0,
          visibility: pos ? 'visible' : 'hidden',
          transformOrigin: pos ? pos.origin : 'left top',
        }}
        onClick={(e) => e.stopPropagation()}
      >
        {timeText ? (
          <div className="px-4 pt-3 pb-1.5 text-[12px] text-ra-t4 tabular-nums">{timeText}</div>
        ) : null}
        <div className="pb-1.5">
          <button type="button" className={itemCls} onClick={() => onCopy(payload)}>
            <span>複製</span><IconCopy />
          </button>
          {payload.role !== 'user' ? (
            <button type="button" className={itemCls} disabled={!payload.canForward} onClick={() => onForward(payload)}>
              <span>轉發</span><IconForward />
            </button>
          ) : null}
        </div>
      </div>
    </div>
  );
}

/**
 * 長按選單的容器：選單的狀態放在這裡，而不是 ChatView。
 * 長按時只重繪這個小元件，不會連帶重繪整串訊息（每則都有 Markdown HTML），動畫才不會卡。
 * openRef.current 由本元件掛上，供 ChatView 的長按回呼呼叫：openRef.current(payload)。
 */
function MessageActionsHost({ openRef, onForward }) {
  const [menu, setMenu] = useState(null);

  useEffect(() => {
    openRef.current = setMenu;
    return () => { openRef.current = null; };
  }, [openRef]);

  const close = useCallback(() => setMenu(null), []);

  const handleCopy = async (p) => {
    setMenu(null);
    if (await copyText(p.text)) showToast('已複製');
    else showToast('複製失敗', { error: true });
  };
  const handleForward = (p) => {
    setMenu(null);
    onForward(p);
  };

  return (
    <MessageContextMenu
      payload={menu}
      onClose={close}
      onCopy={handleCopy}
      onForward={handleForward}
    />
  );
}
