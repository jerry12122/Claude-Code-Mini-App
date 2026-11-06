/** 訊息、會話與分頁共用右鍵／長按觸發；動作選單由 ContextMenu 呈現。 */

const LONG_PRESS_MS = 380;
const LONG_PRESS_MOVE_TOL = 8;
// 按下超過這個時間才顯示「按住」回饋，避免一般點擊或捲動起手時閃一下。
const PRESS_FEEDBACK_MS = 120;

/**
 * 長按偵測。因為訊息是 map 出來的，hook 不能放進迴圈，
 * 這裡共用一組 ref（同一時間只會有一根手指在長按），回傳 bind(payload) 產生各訊息的事件屬性。
 * 觸發時保留操作對象與座標，開啟選單不會觸發原物件的 click。
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
      if (contextMenuNativeTarget(e)) return;
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
          ...openContextPayload({ currentTarget: el }, payload, true),
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
    onContextMenu: (e) => {
      if (contextMenuNativeTarget(e)) return;
      e.preventDefault();
      e.stopPropagation();
      if (Date.now() - lastTouchAtRef.current < 1500) return;
      onTriggerRef.current(openContextPayload(e, payload));
    },
    onKeyDown: (e) => {
      if (e.key !== 'ContextMenu' && !(e.shiftKey && e.key === 'F10')) return;
      if (contextMenuNativeTarget({ target: e.target, currentTarget: e.currentTarget, shiftKey: false })) return;
      e.preventDefault(); e.stopPropagation();
      onTriggerRef.current(openContextPayload(e, payload));
    },
  }), [clear, clearFeedback]);
}

function MessageActionsHost({ openRef, onForward, onQuote, sessionId }) {
  const [menu, setMenu] = useState(null);
  useEffect(() => {
    openRef.current = setMenu;
    return () => { openRef.current = null; };
  }, [openRef]);
  useEffect(() => setMenu(null), [sessionId]);
  const close = useCallback(() => setMenu(null), []);
  const copy = async text => {
    if (await copyText(text)) showToast('已複製');
    else showToast('複製失敗', { error: true });
  };
  const selected = menu?.selectedText || '';
  const items = [
    { label: selected ? '複製選取文字' : '複製全文', onSelect: () => copy(selected || menu.plainText || menu.text) },
    ...(menu?.canQuote ? [{ label: selected ? '引用選取文字' : '引用回覆', onSelect: () => onQuote(selected || menu.plainText || menu.text) }] : []),
    ...(menu?.canShowForward ? [{ label: '轉發到其他會話…', disabled: !menu.canForward,
      reason: menu.streaming ? '回覆完成後可轉發' : '目前無法轉發', onSelect: () => onForward(menu) }] : []),
    ...(menu?.role === 'claude' ? [{ separator: true }, { label: '複製 Markdown', onSelect: () => copy(menu.text) }] : []),
  ];
  return <ContextMenu menu={menu} items={items} onClose={close} label="訊息操作" />;
}
