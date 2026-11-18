/** 工作室畫面（唯讀）：標頭＋成員晶片＋合併時間軸＋底部唯讀說明。刻意沒有輸入框，也不送任何寫入請求。
 *  捲動規則：停在底部 → 新訊息自動捲到底；往上看歷史 → 不被拉走，改顯示「有新訊息」按鈕；
 *  捲到頂 → 用 next_cursor 載入更早一頁，並補償高度差讓視線停在原位。 */

function RoomMemberChip({ member, onOpen }) {
  const chip = roomChip(member);
  const dotClass = sessionStatusDotClass({ status: chip.status });
  const statusLabel = sessionStatusLabel({ status: chip.status }) || '閒置';
  const label = `${chip.name}，${chip.agentLabel}，${chip.roleLabel}，${statusLabel}，額度 ${chip.quotaText}${chip.quotaStale ? '（已過期）' : ''}。進入此 session`;
  return (
    <button
      type="button"
      onClick={() => onOpen(chip.sessionId)}
      aria-label={label}
      title={chip.quotaTitle}
      className="flex shrink-0 max-w-[220px] items-center gap-1.5 rounded-lg border border-ra-line bg-ra-card px-2 py-1 text-left hover:bg-ra-raised focus-visible:outline focus-visible:outline-2 focus-visible:outline-violet-400"
    >
      {dotClass ? <span className={dotClass} aria-hidden="true" /> : <span className="ra-status-dot bg-ra-t5" aria-hidden="true" />}
      <span className={`inline-flex h-[18px] w-[18px] shrink-0 items-center justify-center rounded ${getAgentBadgeClass(chip.agentType)}`} aria-hidden="true">
        <AgentBadgeIcon agentType={chip.agentType} />
      </span>
      <span className="flex min-w-0 flex-col leading-tight">
        <span className="flex min-w-0 items-center gap-1">
          <span className="min-w-0 truncate text-xs font-semibold text-ra-t2">{chip.name}</span>
          <span className="shrink-0 rounded bg-ra-raised px-1 text-[10px] text-ra-t3">{chip.roleLabel}</span>
        </span>
        <span className={`truncate text-[10px] ${chip.quotaStale ? 'text-amber-400' : 'text-ra-t4'}`}>
          {chip.quotaStale ? <s>{chip.quotaText}</s> : chip.quotaText}
          {chip.quotaStale ? <span className="ml-1 no-underline">已過期</span> : null}
        </span>
      </span>
    </button>
  );
}

function RoomView({ roomId, onClose, onOpenSession }) {
  const { room, error: roomError, notFound: roomGone } = useRoomDetail(roomId);
  const { state, error: tlError, notFound: tlGone, loadingOlder, loadOlder } = useRoomTimeline(roomId);
  const scrollRef = useRef(null);
  const contentRef = useRef(null);
  const stickRef = useRef(true);
  const anchorRef = useRef(null);
  const firstIdRef = useRef(null);
  const lastIdRef = useRef(null);
  const [showJump, setShowJump] = useState(false);
  const messages = state.messages;
  const gone = roomGone || tlGone;

  const requestOlder = useCallback(() => {
    const el = scrollRef.current;
    if (!el || anchorRef.current) return;
    anchorRef.current = { height: el.scrollHeight, top: el.scrollTop };
    loadOlder();
  }, [loadOlder]);

  const onScroll = useCallback(() => {
    const el = scrollRef.current;
    if (!el) return;
    const near = roomIsNearBottom(el);
    stickRef.current = near;
    if (near) setShowJump(false);
    if (el.scrollTop <= 40 && state.hasMore && !loadingOlder) requestOlder();
  }, [state.hasMore, loadingOlder, requestOlder]);

  const jumpToBottom = useCallback(() => {
    const el = scrollRef.current;
    if (!el) return;
    stickRef.current = true;
    el.scrollTop = el.scrollHeight;
    setShowJump(false);
  }, []);

  // 訊息變動後、繪製前調整捲動位置，避免閃一下。
  useLayoutEffect(() => {
    const el = scrollRef.current;
    if (!el) return;
    const firstId = messages.length ? messages[0].id : null;
    const lastId = messages.length ? messages[messages.length - 1].id : null;
    const anchor = anchorRef.current;
    if (anchor && firstId !== firstIdRef.current) {
      // 前置了更早的訊息：補回新增內容的高度，視線留在原位。
      el.scrollTop = anchor.top + (el.scrollHeight - anchor.height);
      anchorRef.current = null;
    } else if (stickRef.current) {
      el.scrollTop = el.scrollHeight;
    } else if (lastId !== lastIdRef.current && lastIdRef.current !== null) {
      setShowJump(true);
    }
    firstIdRef.current = firstId;
    lastIdRef.current = lastId;
  }, [messages]);

  // 載入結束卻沒有新訊息（失敗或游標失效）：放掉錨點，下次才能再觸發。
  useEffect(() => { if (!loadingOlder) anchorRef.current = null; }, [loadingOlder]);

  // 圖片、程式碼區塊等晚到的內容會撐高版面：停在底部時跟著補捲。
  useEffect(() => {
    const content = contentRef.current;
    if (!content || typeof ResizeObserver === 'undefined') return undefined;
    const ro = new ResizeObserver(() => {
      const el = scrollRef.current;
      if (el && stickRef.current && !anchorRef.current) el.scrollTop = el.scrollHeight;
    });
    ro.observe(content);
    return () => ro.disconnect();
  }, []);

  const members = room && Array.isArray(room.members) ? room.members : [];
  const err = roomError || tlError;

  return (
    <section className="flex h-app min-w-0 flex-col bg-ra-bg" aria-label={room ? `工作室 ${room.name}` : '工作室'}>
      <header
        className="shrink-0 border-b border-ra-line-soft px-3 pb-2"
        style={{ paddingTop: 'calc(0.5rem + env(safe-area-inset-top) + var(--tg-content-safe-top))' }}
      >
        <div className="flex items-center gap-2">
          <button type="button" onClick={onClose} aria-label="關閉工作室" title="關閉工作室"
            className="ra-icon-btn shrink-0 rounded-lg p-1.5 text-ra-t3 hover:bg-ra-raised">
            <svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true"><path d="M15 18l-6-6 6-6" /></svg>
          </button>
          <h1 className="min-w-0 flex-1 truncate text-sm font-bold text-ra-t1">{room ? room.name : '工作室'}</h1>
          <span className="shrink-0 text-[11px] text-ra-t4">{members.length ? `${members.length} 位成員` : ''}</span>
        </div>
        {members.length ? (
          <div className="mt-2 flex gap-2 overflow-x-auto pb-1 app-scroll" role="list" aria-label="成員">
            {members.map((mb) => (
              <div role="listitem" key={mb.session_id} className="shrink-0"><RoomMemberChip member={mb} onOpen={onOpenSession} /></div>
            ))}
          </div>
        ) : null}
      </header>

      <div className="relative min-h-0 flex-1">
        <div ref={scrollRef} onScroll={onScroll} role="log" aria-label="合併時間軸" tabIndex={0}
          className="app-scroll h-full overflow-y-auto px-4 py-3"
          style={{ overflowAnchor: 'none' }}>
          <div ref={contentRef} className="mx-auto flex max-w-3xl flex-col gap-3">
            {gone ? (
              <div className="py-10 text-center text-sm text-ra-t4">這個工作室已不存在（可能已被刪除）。</div>
            ) : (
              <>
                {state.hasMore ? (
                  <button type="button" onClick={requestOlder} disabled={loadingOlder}
                    className="mx-auto rounded-lg border border-ra-line px-3 py-1 text-xs text-ra-t3 hover:bg-ra-raised disabled:opacity-60">
                    {loadingOlder ? '載入中…' : '載入更早訊息'}
                  </button>
                ) : state.loaded && messages.length ? (
                  <div className="text-center text-[11px] text-ra-t5">已到最早的訊息</div>
                ) : null}
                {!state.loaded && !err ? <div className="py-10 text-center text-sm text-ra-t4">載入中…</div> : null}
                {state.loaded && !messages.length ? <div className="py-10 text-center text-sm text-ra-t4">成員還沒有任何訊息。</div> : null}
                {messages.map((m) => <RoomMessageBubble key={m.id} m={m} />)}
              </>
            )}
          </div>
        </div>
        {err && !gone ? (
          <div role="alert" className="absolute inset-x-3 top-2 rounded-lg border border-red-500/40 bg-red-950/80 px-3 py-1.5 text-xs text-red-200">{err}</div>
        ) : null}
        {showJump ? (
          <button type="button" onClick={jumpToBottom}
            className="absolute bottom-3 left-1/2 -translate-x-1/2 rounded-full bg-violet-600 px-3 py-1 text-xs font-semibold text-white shadow-lg">
            有新訊息 ↓
          </button>
        ) : null}
      </div>

      <footer className="shrink-0 border-t border-ra-line-soft px-4 py-2 text-center text-xs text-ra-t4"
        style={{ paddingBottom: 'calc(0.5rem + env(safe-area-inset-bottom))' }}>
        {ROOM_READONLY_NOTE}
      </footer>
    </section>
  );
}
