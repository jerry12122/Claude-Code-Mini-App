/** 工作室畫面：成員、合併時間軸、PM 輸入框與備忘。
 *  捲動規則：停在底部 → 新訊息自動捲到底；往上看歷史 → 不被拉走，改顯示「有新訊息」按鈕；
 *  捲到頂 → 用 next_cursor 載入更早一頁，並補償高度差讓視線停在原位。 */

// filter 有值＝標頭的對話篩選晶片（切換閱讀範圍，不開 session）；沒給＝成員面板用的「進入此 session」。
function RoomMemberChip({ member, onOpen, compact = false, filter }) {
  const chip = roomChip(member);
  const details = [chip.stateExtra, chip.lastError, chip.quotaTitle].filter(Boolean).join('；');
  const dotClass = chip.attention || chip.queuePaused ? 'ra-status-dot bg-amber-400' : (sessionStatusDotClass({ status: chip.status }) || 'ra-status-dot bg-ra-t5');
  const label = filter
    ? `${chip.name}，${chip.agentLabel}，${chip.roleLabel}，${chip.statePrimary}。${chip.role === 'pm' ? '只看你與 PM 的對話' : '看與他有關的對話'}`
    : `${chip.name}，${chip.agentLabel}，${chip.roleLabel}，${chip.statePrimary}。進入此 session`;
  return (
    <button
      type="button"
      onClick={() => onOpen(chip.sessionId)}
      aria-label={label}
      aria-description={details}
      aria-pressed={filter ? filter.pressed : undefined}
      title={`${label}；${details}`}
      style={compact && filter ? { flex: '0 0 auto' } : undefined}
      className={'room-member-chip flex min-h-11 shrink-0 max-w-[16rem] items-center gap-2 rounded-lg border border-ra-line-soft px-2.5 py-1.5 text-left hover:bg-ra-raised ' +
        (filter && filter.pressed ? 'bg-ra-raised ring-2 font-bold' : 'bg-ra-card')}
    >
      {filter && filter.pressed ? <span className="shrink-0 text-xs" aria-hidden="true">✓</span> : null}
      <span className={`inline-flex h-6 w-6 shrink-0 items-center justify-center rounded-md ${getAgentBadgeClass(chip.agentType)}`} aria-hidden="true">
        <AgentBadgeIcon agentType={chip.agentType} />
      </span>
      <span className="flex min-w-0 flex-col leading-tight">
        <span className="flex min-w-0 items-center gap-1">
          <span className={dotClass} aria-hidden="true" />
          <span className="min-w-0 truncate text-sm font-semibold text-ra-t1">{chip.name}</span>
          {chip.role === 'pm' ? <span className="shrink-0 rounded bg-ra-raised px-1.5 text-[10px] font-semibold text-ra-t2">現任 PM</span> : null}
          {chip.attention || chip.queuePaused ? <span className="shrink-0 rounded bg-ra-raised px-1.5 text-[10px] font-semibold text-amber-400">{chip.attention ? chip.statePrimary : '佇列已暫停'}</span> : null}
          {chip.stateExtra || (chip.queuePaused && !chip.attention) ? <span className="shrink-0 text-[10px] text-ra-t2">{chip.stateExtra || chip.statePrimary.replace('佇列已暫停，', '')}</span> : null}
          {chip.lastError ? <span className="shrink-0 rounded bg-ra-raised px-1.5 text-[10px] font-semibold text-rose-400">錯誤紀錄</span> : null}
        </span>
        {!compact && chip.quotaText !== '—' ? <span className={`truncate text-[10px] ${chip.quotaStale ? 'text-amber-400' : 'text-ra-t4'}`}>
          {chip.quotaStale ? <s>{chip.quotaText}</s> : chip.quotaText}
          {chip.quotaStale ? <span className="ml-1 no-underline">額度資料已過期</span> : null}
        </span> : null}
      </span>
    </button>
  );
}

// 「總覽」：預設選中，顯示全部工作室訊息。
function RoomOverviewChip({ pressed, onSelect }) {
  return (
    <button type="button" onClick={onSelect} aria-pressed={pressed} aria-label="總覽，顯示全部工作室訊息"
      style={{ flex: '0 0 auto' }}
      className={'room-overview-chip flex min-h-11 shrink-0 items-center gap-1.5 rounded-lg border border-ra-line-soft px-3 text-sm hover:bg-ra-raised ' +
        (pressed ? 'bg-ra-raised ring-2 font-bold text-ra-t1' : 'bg-ra-card font-semibold text-ra-t2')}>
      {pressed ? <span className="text-xs" aria-hidden="true">✓</span> : null}總覽
    </button>
  );
}

// 跨過 1024 會整棵換掉工作室畫面。閱讀位置留在這裡，重建後才還原得回來。
const roomScrollMemory = new Map();

function roomTimelineMeasurable(el) {
  if (!el || el.clientHeight < 1) return false;
  const thread = el.closest('.room-thread');
  if (!thread) return false;
  const style = getComputedStyle(thread);
  return style.visibility !== 'hidden' && style.display !== 'none';
}

function RoomView({ roomId, sessions, onClose, onOpenSession }) {
  const { room, error: roomError, notFound: roomGone, stale: roomStale } = useRoomDetail(roomId);
  const [filterId, setFilterId] = useState('');
  const target = roomFilterTarget(room, filterId);
  const { state, error: tlError, notFound: tlGone, loadingOlder, loadOlder, refresh, admitLocal, patchLocal } = useRoomTimeline(roomId, target);
  const scrollRef = useRef(null);
  const contentRef = useRef(null);
  const remembered = roomScrollMemory.get(roomId);
  const stickRef = useRef(remembered ? remembered.stick : true);
  const savedTopRef = useRef(remembered ? remembered.top : null);
  const ignoreScrollRef = useRef(false);
  const anchorRef = useRef(null);
  const firstIdRef = useRef(null);
  const lastIdRef = useRef(null);
  const [showJump, setShowJump] = useState(false);
  const [boardOpen, setBoardOpen] = useState(false);
  const [boardTab, setBoardTab] = useState('jobs');
  const [lightboxSrc, setLightboxSrc] = useState('');
  const role = target ? target.role : '';
  const gone = roomGone || tlGone;

  // ── 排隊輸入的編輯草稿：整間工作室一份，放在這裡而不是氣泡裡，輪詢、篩選、氣泡消失都不會弄丟它。──
  const [edit, setEdit] = useState(null);
  const editRef = useRef(null);
  editRef.current = edit;
  const saveSeq = useRef(0);
  const pmIdRef = useRef('');
  pmIdRef.current = room ? room.pm_id : '';
  const operationScope = roomId + ':' + pmIdRef.current;
  const operationScopeRef = useRef(operationScope); operationScopeRef.current = operationScope;
  useEffect(() => () => { saveSeq.current += 1; }, [operationScope]);
  const queueState = useRoomQueue(roomId, room?.pm_id || '');
  const queueBusy = useRef(false);
  const [batchBusy, setBatchBusy] = useState(false);
  const [batchError, setBatchError] = useState('');
  useEffect(() => {
    queueBusy.current = false; setBatchBusy(false); setBatchError('');
    setEdit(e => e && e.expectedPm !== pmIdRef.current ? { ...e, status: 'editing', error: 'PM 已交接，原草稿已保留，不會改投新 PM。' } : e);
  }, [operationScope]);
  const promotedRequests = new Set(state.messages.filter(m => !m.queued && !m.local && m.input_request_id).map(m => m.input_request_id));
  const queueRows = queueState.rows.filter(q => !q.input_request_id || !promotedRequests.has(q.input_request_id));
  const refreshQueue = queueState.refresh;
  const shown = state.messages;
  const messages = useMemo(() => roomFilterVisible(shown.filter(m => !m.queued && !m.local), role), [shown, role]);
  const editApi = useMemo(() => ({
    onBegin: (m) => {
      if (editRef.current || queueBusy.current || !queueState.ready) return;
      const t = roomQueueRowTarget(m, pmIdRef.current);
      if (t) { const next = { ...t, draft: t.previous, status: 'editing', error: '', invalid: false, focusTick: 0, reloading: false, rebase: false }; editRef.current = next; setEdit(next); }
    },
    onDraft: (draft) => setEdit((e) => (e && e.status !== 'saving' ? { ...e, draft, error: e.invalid ? '' : e.error, invalid: false } : e)),
    onCancel: () => { if (editRef.current && editRef.current.status !== 'saving') setEdit(null); },
    onSave: async () => {
      const cur = editRef.current;
      if (!cur || cur.status === 'saving' || queueBusy.current) return;
      if (cur.draft === cur.previous) { setEdit(null); return; }
      const bad = roomQueueEditInvalid(cur.draft, cur.hasAttachments);
      if (bad) { setEdit({ ...cur, error: bad, invalid: true, focusTick: cur.focusTick + 1 }); return; }
      const token = ++saveSeq.current;
      const scope = operationScopeRef.current;
      editRef.current = { ...cur, status: 'saving' };
      setEdit({ ...cur, status: 'saving', error: '', invalid: false });
      let res;
      try {
        res = await apiFetch(roomQueueEditUrl(roomId, cur.queueId), {
          method: 'PATCH', headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ expected_pm_id: cur.expectedPm, previous: cur.previous, content: cur.draft }),
        });
      } catch (err) {
        if (token === saveSeq.current && scope === operationScopeRef.current) setEdit((e) => (e && e.msgId === cur.msgId ? { ...e, status: 'editing', error: roomQueueEditNetworkError(err) } : e));
        return;
      }
      if (token !== saveSeq.current || scope !== operationScopeRef.current) return;
      if (res.ok) {
        setEdit(null);
        await refreshQueue();
        return;
      }
      const detail = res.status === 400 ? await roomApiError(res, '') : '';
      if (token === saveSeq.current) setEdit((e) => (e && e.msgId === cur.msgId ? { ...e, status: 'editing', error: roomQueueEditFailure(res.status, detail) } : e));
    },
    onReload: async () => {
      const cur = editRef.current;
      if (!cur || cur.status === 'saving' || cur.reloading) return;
      setEdit({ ...cur, reloading: true });
      await refreshQueue();
      setEdit((e) => (e && e.msgId === cur.msgId ? { ...e, reloading: false, rebase: true } : e));
    },
  }), [roomId, refreshQueue, queueState.ready]);
  // 重新載入後以目前佇列文字為新基準（草稿不動）；交接或出列則維持錯誤，絕不換 PM 重試。
  useEffect(() => {
    if (!edit || !edit.rebase) return;
    const t = roomQueueRowTarget(queueRows.find((m) => m.id === edit.msgId), room ? room.pm_id : '');
    if (t && t.expectedPm === edit.expectedPm) setEdit({ ...edit, rebase: false, previous: t.previous, hasAttachments: t.hasAttachments, error: '', invalid: false });
    else setEdit({ ...edit, rebase: false, error: '找不到可編輯的這則排隊訊息（已出列、已交接，或目前的對話篩選看不到）。草稿已保留；可取消，或切到總覽後再重新載入。' });
  }, [edit, queueRows, room]);
  const editPresent = !!edit && queueRows.some((m) => m.id === edit.msgId && m.pm_id === edit.expectedPm);

  async function changeBatch(batchId) {
    if (queueBusy.current || editRef.current || !queueState.ready) return;
    const expectedPm = pmIdRef.current, scope = operationScopeRef.current;
    const items = queueRows.slice(0, 100);
    if (!batchId && (queueState.batchReady !== true || items.length < 2 || queueRows.some(q => q.batch_id))) return;
    queueBusy.current = true; setBatchBusy(true); setBatchError('');
    try {
      const res = await apiFetch(roomUrl(roomId) + '/messages/batch' + (batchId ? '/' + encodeURIComponent(batchId) : ''), {
        method: batchId ? 'DELETE' : 'POST', headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(batchId ? { expected_pm_id: expectedPm } : { expected_pm_id: expectedPm, request_id: newInputRequestID(), items: items.map(q => ({ id: q.id, previous: q.content })) }),
      });
      if (scope !== operationScopeRef.current) return;
      if (!res.ok) throw new Error(await roomApiError(res, '批次操作失敗，請重新同步確認'));
      await refreshQueue();
    } catch (err) { if (scope === operationScopeRef.current) setBatchError(err.message || '結果未確認，請重新同步'); }
    finally { if (scope === operationScopeRef.current) { queueBusy.current = false; setBatchBusy(false); } }
  }

  // 換篩選＝換一份新的閱讀範圍：回到底部，不帶著上一份的捲動位置。
  const selectFilter = useCallback((sessionId) => {
    const next = sessionId === filterId ? '' : sessionId;
    if (next === filterId) return;
    stickRef.current = true;
    savedTopRef.current = null;
    anchorRef.current = null;
    setShowJump(false);
    setFilterId(next);
  }, [filterId]);

  const requestOlder = useCallback(() => {
    const el = scrollRef.current;
    if (!el || anchorRef.current) return;
    anchorRef.current = { height: el.scrollHeight, top: el.scrollTop };
    loadOlder();
  }, [loadOlder]);

  const rememberScroll = useCallback((el) => {
    if (!el || !roomTimelineMeasurable(el)) return;
    savedTopRef.current = el.scrollTop;
    roomScrollMemory.set(roomId, { top: el.scrollTop, stick: stickRef.current });
  }, [roomId]);

  const placeScroll = useCallback((el, top) => {
    ignoreScrollRef.current = true;
    el.scrollTop = top;
    ignoreScrollRef.current = false;
  }, []);

  const onScroll = useCallback(() => {
    const el = scrollRef.current;
    // 隱藏、還沒載入、或自己剛設的捲動，都不能拿來改跟隨旗標。空內容的高度會被算成貼底。
    if (!el || ignoreScrollRef.current || !state.loaded || !roomTimelineMeasurable(el)) return;
    const near = roomIsNearBottom(el);
    stickRef.current = near;
    if (near) setShowJump(false);
    rememberScroll(el);
    if (el.scrollTop <= 40 && state.hasMore && !loadingOlder) requestOlder();
  }, [state.loaded, state.hasMore, loadingOlder, requestOlder, rememberScroll]);

  const jumpToBottom = useCallback(() => {
    const el = scrollRef.current;
    if (!el) return;
    stickRef.current = true;
    placeScroll(el, el.scrollHeight);
    savedTopRef.current = el.scrollTop;
    roomScrollMemory.set(roomId, { top: el.scrollTop, stick: true });
    setShowJump(false);
  }, [roomId, placeScroll]);

  // 訊息變動後、繪製前調整捲動位置，避免閃一下。
  useLayoutEffect(() => {
    const el = scrollRef.current;
    if (!el) return;
    const visible = roomTimelineMeasurable(el);
    const firstId = messages.length ? messages[0].id : null;
    const lastId = messages.length ? messages[messages.length - 1].id : null;
    const anchor = anchorRef.current;
    if (anchor && firstId !== firstIdRef.current) {
      // 前置了更早的訊息：補回新增內容的高度，視線留在原位。
      placeScroll(el, anchor.top + (el.scrollHeight - anchor.height));
      anchorRef.current = null;
      savedTopRef.current = el.scrollTop;
      roomScrollMemory.set(roomId, { top: el.scrollTop, stick: stickRef.current });
    } else if (stickRef.current) {
      if (visible) placeScroll(el, el.scrollHeight);
    } else if (visible && savedTopRef.current != null) {
      placeScroll(el, savedTopRef.current);
      if (lastId !== lastIdRef.current && lastIdRef.current !== null) setShowJump(true);
    } else if (lastId !== lastIdRef.current && lastIdRef.current !== null) {
      setShowJump(true);
    }
    firstIdRef.current = firstId;
    lastIdRef.current = lastId;
  }, [messages, roomId, placeScroll]);

  // 載入結束卻沒有新訊息（失敗或游標失效）：放掉錨點，下次才能再觸發。
  useEffect(() => { if (!loadingOlder) anchorRef.current = null; }, [loadingOlder]);

  // 圖片、程式碼區塊等晚到的內容會撐高版面：停在底部時跟著補捲。
  useEffect(() => {
    const content = contentRef.current;
    if (!content || typeof ResizeObserver === 'undefined') return undefined;
    const ro = new ResizeObserver(() => {
      const el = scrollRef.current;
      if (!el || !stickRef.current || anchorRef.current || !roomTimelineMeasurable(el)) return;
      ignoreScrollRef.current = true;
      el.scrollTop = el.scrollHeight;
      ignoreScrollRef.current = false;
    });
    ro.observe(content);
    return () => ro.disconnect();
  }, []);

  // 寬度或側欄把對話藏起再顯示時，視線回到藏起前的位置；本來就在底部才繼續跟著捲。
  useEffect(() => {
    const thread = scrollRef.current && scrollRef.current.closest('.room-thread');
    if (!thread || typeof ResizeObserver === 'undefined') return undefined;
    const apply = () => {
      const el = scrollRef.current;
      if (!el || anchorRef.current || !roomTimelineMeasurable(el)) return;
      ignoreScrollRef.current = true;
      if (stickRef.current) el.scrollTop = el.scrollHeight;
      else if (savedTopRef.current != null) el.scrollTop = savedTopRef.current;
      ignoreScrollRef.current = false;
    };
    const ro = new ResizeObserver(apply);
    ro.observe(thread);
    return () => ro.disconnect();
  }, [roomId]);

  useEffect(() => () => {
    const el = scrollRef.current;
    if (el && roomTimelineMeasurable(el)) {
      roomScrollMemory.set(roomId, { top: el.scrollTop, stick: stickRef.current });
    }
  }, [roomId]);

  const members = (room && Array.isArray(room.members) ? [...room.members] : [])
    .sort((a, b) => Number(b.session_id === room.pm_id) - Number(a.session_id === room.pm_id) || a.position - b.position);
  const pm = members.find((member) => member.session_id === room?.pm_id);
  const pending = members.filter((member) => member.attention);
  const err = roomError || tlError;
  const pendingFocus = useRef(null);
  const focusPending = (sessionId) => {
    const el = document.getElementById('room-action-' + sessionId);
    if (!el) return;
    stickRef.current = false;
    el.scrollIntoView({ block: 'center' });
    el.focus();
  };
  const openPending = () => {
    const first = pending[0];
    if (!first) return;
    const thread = document.querySelector('.room-screen .room-thread');
    const covered = thread && getComputedStyle(thread).visibility === 'hidden';
    if (covered) {
      pendingFocus.current = first.session_id;
      setBoardOpen(false);
      return;
    }
    focusPending(first.session_id);
  };
  useEffect(() => {
    const sessionId = pendingFocus.current;
    if (!sessionId || boardOpen) return;
    pendingFocus.current = null;
    focusPending(sessionId);
  }, [boardOpen, pending]);

  return (
    <section className="room-screen flex h-full min-h-0 min-w-0 flex-col bg-ra-bg" aria-label={room ? `工作室 ${room.name}` : '工作室'}>
      <header className="room-header shrink-0 border-b border-ra-line-soft pb-2">
        <div className="flex flex-wrap items-center gap-2">
          <button type="button" onClick={onClose} aria-label="關閉工作室" title="關閉工作室"
            className="-ml-2 flex h-11 w-11 shrink-0 items-center justify-center rounded-lg text-ra-t3 hover:bg-ra-raised">
            <svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true"><path d="M15 18l-6-6 6-6" /></svg>
          </button>
          <h1 className="min-w-0 flex-1 truncate text-lg font-bold text-ra-t1" title={room?.name}>{room ? room.name : '工作室'}</h1>
          {pending.length ? <button type="button" onClick={openPending}
            className="room-pending flex min-h-11 shrink-0 items-center rounded-lg bg-ra-raised px-3 text-sm font-semibold text-ra-t1">
            待你處理 {pending.length}
          </button> : null}
          {room && !gone ? <button type="button" className={'room-board-toggle flex min-h-11 shrink-0 items-center gap-1.5 rounded-lg px-2 text-xs font-semibold text-ra-t2 ' + (boardOpen ? 'bg-ra-raised' : 'hover:bg-ra-raised')}
            aria-controls="room-work-board" aria-expanded={boardOpen} onClick={() => {
              rememberScroll(scrollRef.current);
              setBoardOpen((value) => !value);
            }}>
            <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true"><path d="M6 4h12v16H6zM9 8h6m-6 4h6m-6 4h4" /></svg>
            <span className="room-board-label-narrow">{boardOpen ? '回到對話' : '工作概況'}</span>
            <span className="room-board-label-wide">{boardOpen ? '隱藏工作概況' : '工作概況'}</span>
          </button> : null}
        </div>
        {roomStale ? <p role="status" className="px-1 pt-1 text-xs text-amber-300">狀態未更新{room?.observed_at ? `，最後成功於 ${room.observed_at}` : ''}</p> : null}
        {members.length ? (
          <>
            <div className="room-members-compact app-scroll mt-1 items-center gap-1.5 overflow-x-auto px-0.5 pb-1 pt-0.5" role="group" aria-label="對話篩選">
              <RoomOverviewChip pressed={!target} onSelect={() => selectFilter('')} />
              {members.map((mb) => (
                <RoomMemberChip key={mb.session_id} member={mb} onOpen={selectFilter} compact filter={{ pressed: !!target && target.id === mb.session_id }} />
              ))}
              <button type="button" className="min-h-11 shrink-0 rounded-lg border border-ra-line-soft bg-ra-card px-3 text-sm font-semibold text-ra-t1"
                style={{ flex: '0 0 auto' }}
                onClick={() => { setBoardTab('members'); setBoardOpen(true); }}>成員 {members.length}</button>
            </div>
            <div className="room-members-full app-scroll mt-1 gap-1.5 overflow-x-auto px-0.5 pb-1 pt-0.5" role="group" aria-label="對話篩選">
              <div className="flex shrink-0"><RoomOverviewChip pressed={!target} onSelect={() => selectFilter('')} /></div>
              <div role="list" aria-label="成員" className="flex shrink-0 gap-1.5">
                {members.map((mb) => (
                  <div role="listitem" key={mb.session_id} className="flex shrink-0"><RoomMemberChip member={mb} onOpen={selectFilter} filter={{ pressed: !!target && target.id === mb.session_id }} /></div>
                ))}
              </div>
            </div>
          </>
        ) : null}
      </header>

      <div className="room-workspace flex min-h-0 flex-1" data-board-open={boardOpen}>
        <div className="room-thread min-w-0 flex-1 flex-col">
          <div className="relative min-h-0 flex-1">
            <div ref={scrollRef} onScroll={onScroll} role="log" aria-label={target ? `合併時間軸（篩選：${target.name}）` : '合併時間軸'} tabIndex={0}
              className="room-timeline app-scroll h-full overflow-y-auto py-4"
              style={{ overflowAnchor: 'none' }}>
              <div ref={contentRef} className="flex min-w-0 flex-col gap-5">
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
                    {state.loaded && !messages.length ? <div className="py-10 text-center text-sm text-ra-t4">{roomFilterEmptyText(target)}</div> : null}
                    {messages.map((m) => <RoomMessageBubble key={m.id} m={m} expanded={m.id === roomLatestBodyId(messages)} onPreview={setLightboxSrc} />)}
                    {pending.map((member) => <RoomMemberActions key={member.session_id} member={member} />)}
                  </>
                )}
              </div>
            </div>
            {err && !gone ? (
              <div role="alert" className="absolute inset-x-3 top-2 rounded-lg border border-red-500/40 bg-red-950/80 px-3 py-1.5 text-xs text-red-200">{err}</div>
            ) : null}
            {showJump ? (
              <button type="button" onClick={jumpToBottom}
                className="absolute bottom-3 left-1/2 min-h-11 -translate-x-1/2 rounded-full border border-ra-line bg-ra-card px-3 text-xs font-semibold text-ra-t1 shadow-lg hover:bg-ra-raised">
                有新訊息 ↓
              </button>
            ) : null}
          </div>
          {pm && !gone ? <RoomQueuePanel queue={queueRows} error={queueState.error || batchError} busy={batchBusy} ready={queueState.ready} batchReady={queueState.batchReady}
            edit={edit} editPresent={editPresent} editApi={editApi} onBatch={changeBatch} onRefresh={refreshQueue}
            locals={state.messages.filter(m => m.local && m.delivery !== 'queued' && !queueRows.some(q => q.input_request_id === m.input_request_id))} /> : null}
          {pm && !gone ? <RoomComposer roomId={roomId} pm={pm} members={members} sessions={sessions} onDelivery={admitLocal} onDeliveryPatch={patchLocal}
            onAccepted={() => { if (stickRef.current) jumpToBottom(); refresh(); refreshQueue(); }} /> : (
            <footer className="shrink-0 border-t border-ra-line-soft px-4 py-3 text-center text-xs text-ra-t4">
              {room && !gone ? '指定一位 PM 後即可傳訊息。' : ''}
            </footer>
          )}
        </div>
        {room && !gone ? <RoomWorkBoard roomId={roomId} members={members} tab={boardTab} onTab={setBoardTab} onOpenSession={onOpenSession} /> : null}
      </div>
      {lightboxSrc ? <ChatImageLightbox src={lightboxSrc} onClose={() => setLightboxSrc('')} /> : null}
    </section>
  );
}
