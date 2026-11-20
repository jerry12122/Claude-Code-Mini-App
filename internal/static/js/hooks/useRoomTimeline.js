// ── 工作室畫面的資料 hook（無 JSX）──────────────────────────────────────────
// useRoomDetail：成員清單（狀態點、額度）；useRoomTimeline：合併時間軸。兩者都只在畫面存在、
// 且頁籤可見時每 ROOM_POLL_MS 輪詢一次（排程見 hooks/roomPoller.js），卸載即停。
// 工作室被編輯時，RoomsSection 會發出 miniapp:rooms-changed。

const ROOMS_CHANGED_EVENT = 'miniapp:rooms-changed';

function roomQueuePageHint(page) {
  if (!page || typeof page.batch_ready !== 'boolean') return null;
  return page.batch_ready;
}

function roomQueueKeepHint(kept, page, firstPage) {
  return firstPage ? roomQueuePageHint(page) : kept;
}

function roomQueueHintForScope(snapshot, scope) {
  if (!snapshot || snapshot.scope !== scope || snapshot.ready !== true) return null;
  return typeof snapshot.batchReady === 'boolean' ? snapshot.batchReady : null;
}

function useRoomQueue(roomId, pmId) {
  const scope = roomId + ':' + pmId;
  const current = useRef(scope); current.current = scope;
  const generation = useRef(0);
  const requestRef = useRef(null);
  const handoffNotified = useRef('');
  const [snapshot, setSnapshot] = useState({ scope, rows: [], error: '', ready: false });
  const refresh = useCallback(async () => {
    if (!pmId) return;
    const mine = ++generation.current;
    requestRef.current?.abort();
    const controller = new AbortController(); requestRef.current = controller;
    try {
      let after = 0, rows = [], more, hint = null, firstPage = true;
      do {
        const res = await apiFetch(roomUrl(roomId) + '/queue?expected_pm_id=' + encodeURIComponent(pmId) + '&limit=100&after_id=' + after, { signal: controller.signal });
        if (!res.ok) {
          if (res.status === 409 && handoffNotified.current !== scope) { handoffNotified.current = scope; window.dispatchEvent(new Event(ROOMS_CHANGED_EVENT)); }
          throw new Error(await roomApiError(res, '無法載入佇列，請重新同步'));
        }
        const data = await res.json();
        if (current.current !== scope || mine !== generation.current) return;
        if (data.pm_id !== pmId) throw new Error('PM 已變更，請重新同步');
        hint = roomQueueKeepHint(hint, data, firstPage);
        firstPage = false;
        rows.push(...(data.queue || []).map(q => ({ ...q, pm_id: pmId })));
        more = data.has_more;
        if (more && !(data.next_after_id > after)) throw new Error('佇列分頁無法繼續，請重新同步');
        after = data.next_after_id;
      } while (more);
      setSnapshot({ scope, rows, error: '', ready: true, batchReady: hint });
      return rows;
    } catch (err) {
      if (!controller.signal.aborted && current.current === scope && mine === generation.current) setSnapshot(s => ({ scope, rows: s.scope === scope ? s.rows : [], error: err.message, ready: false, batchReady: null }));
    }
  }, [roomId, pmId, scope]);
  useEffect(() => {
    refresh();
    const stop = startRoomPoller({ tick: refresh });
    window.addEventListener(ROOMS_CHANGED_EVENT, refresh);
    return () => { ++generation.current; requestRef.current?.abort(); stop(); window.removeEventListener(ROOMS_CHANGED_EVENT, refresh); };
  }, [refresh]);
  return { rows: snapshot.scope === scope ? snapshot.rows : [], error: snapshot.scope === scope ? snapshot.error : '', ready: snapshot.scope === scope && snapshot.ready, batchReady: roomQueueHintForScope(snapshot, scope), refresh };
}

async function roomApiError(res, fallback) {
  try {
    const body = await res.json();
    return body && body.error ? body.error : fallback;
  } catch (_) {
    return fallback;
  }
}

function useRoomDetail(roomId) {
  const [room, setRoom] = useState(null);
  const [error, setError] = useState('');
  const [notFound, setNotFound] = useState(false);
  const [stale, setStale] = useState(false);
  const generation = useRef(0);
  const roomRef = useRef(null);

  const load = useCallback(async () => {
    const mine = ++generation.current;
    try {
      const res = await apiFetch(roomUrl(roomId));
      if (mine !== generation.current) return;
      if (res.status === 404) { setNotFound(true); return; }
      if (!res.ok) {
        if (roomRef.current) setStale(true);
        setError(await roomApiError(res, '載入工作室失敗'));
        return;
      }
      const data = await res.json();
      if (mine !== generation.current) return;
      roomRef.current = data;
      setRoom(data);
      setStale(false);
      setError('');
    } catch (_) {
      if (mine === generation.current) {
        if (roomRef.current) setStale(true);
        setError('載入工作室失敗');
      }
    }
  }, [roomId]);

  useEffect(() => {
    roomRef.current = null;
    setRoom(null); setError(''); setNotFound(false); setStale(false);
    load();
    return () => { ++generation.current; };
  }, [load]);

  useEffect(() => {
    if (notFound) return undefined; // 工作室已被刪除：不會自己好起來，停止輪詢
    window.addEventListener(ROOMS_CHANGED_EVENT, load);
    const stop = startRoomPoller({ tick: load });
    return () => { window.removeEventListener(ROOMS_CHANGED_EVENT, load); stop(); };
  }, [load, notFound]);

  return { room, error, notFound, stale };
}

// target：成員篩選對象 { id, role }，null＝總覽。篩選在後端先於分頁，所以換對象就是整條時間軸重載；
// 每份狀態都帶著所屬的 scope，晚到的舊回應與切換前的游標一律丟棄。
function useRoomTimeline(roomId, target) {
  const memberId = target ? target.id : '';
  const scope = roomTimelineScope(roomId, target);
  const [raw, setState] = useState(() => roomTimelineReset(roomTimelineEmpty(), scope));
  const state = raw.scope === scope ? raw : roomTimelineReset(raw, scope);
  const [error, setError] = useState('');
  const [notFound, setNotFound] = useState(false);
  const [loadingOlder, setLoadingOlder] = useState(false);
  const generation = useRef(0);
  const stateRef = useRef(state);
  stateRef.current = state;
  const olderBusy = useRef(false);

  const fetchPage = useCallback(async (before) => {
    const mine = generation.current;
    const res = await apiFetch(roomTimelineUrl(roomId, before, ROOM_PAGE_SIZE, memberId));
    if (mine !== generation.current) return null;
    if (res.status === 404 && memberId) throw new Error('這位成員已不在工作室'); // 成員被移出：工作室還在，等成員清單更新後回到總覽
    if (res.status === 404) { setNotFound(true); return null; }
    if (!res.ok) throw new Error(await roomApiError(res, '載入時間軸失敗'));
    const page = await res.json();
    return mine === generation.current ? page : null;
  }, [roomId, memberId, scope]);

  const refresh = useCallback(async () => {
    try {
      const page = await fetchPage('');
      if (!page) return;
      setState((prev) => roomTimelineAccept(prev, scope, page, 'latest'));
      setError('');
    } catch (err) {
      setError(err.message || '載入時間軸失敗');
    }
  }, [fetchPage, scope]);

  // 取更早的一頁；游標在請求期間被輪詢「重來」改掉時，這一頁已經接不上，直接丟棄。
  const loadOlder = useCallback(async () => {
    const { hasMore, nextCursor, scope: stateScope } = stateRef.current;
    if (stateScope !== scope || !hasMore || !nextCursor || olderBusy.current) return;
    olderBusy.current = true;
    setLoadingOlder(true);
    try {
      const page = await fetchPage(nextCursor);
      if (page && stateRef.current.nextCursor === nextCursor) {
        setState((prev) => roomTimelineAccept(prev, scope, page, 'older'));
      }
    } catch (err) {
      setError(err.message || '載入更早訊息失敗');
    } finally {
      olderBusy.current = false;
      setLoadingOlder(false);
    }
  }, [fetchPage, scope]);

  // 換工作室、換篩選對象或工作室被編輯（成員可能變了）：整條時間軸重載，不拿舊成員的訊息混進來。
  useEffect(() => {
    const reset = () => {
      ++generation.current;
      olderBusy.current = false;
      setLoadingOlder(false);
      setState((prev) => roomTimelineReset(prev, scope));
      setError('');
      refresh();
    };
    setNotFound(false);
    reset();
    window.addEventListener(ROOMS_CHANGED_EVENT, reset);
    return () => { window.removeEventListener(ROOMS_CHANGED_EVENT, reset); ++generation.current; };
  }, [refresh, scope]);

  useEffect(() => {
    if (notFound) return undefined;
    return startRoomPoller({ tick: refresh });
  }, [refresh, notFound]);

  const admitLocal = useCallback((message) => {
    setState((prev) => roomAdmitLocal(prev, message));
  }, []);
  const patchLocal = useCallback((requestId, patch) => {
    setState((prev) => roomPatchLocal(prev, requestId, patch));
  }, []);
  const dropLocal = useCallback((requestId) => {
    setState((prev) => roomDropLocal(prev, requestId));
  }, []);

  return { state, error, notFound, loadingOlder, loadOlder, refresh, admitLocal, patchLocal, dropLocal };
}
