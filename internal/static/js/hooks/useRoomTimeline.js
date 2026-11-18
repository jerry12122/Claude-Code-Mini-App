// ── 工作室畫面的資料 hook（無 JSX）──────────────────────────────────────────
// useRoomDetail：成員清單（狀態點、額度）；useRoomTimeline：合併時間軸。兩者都只在畫面存在、
// 且頁籤可見時每 ROOM_POLL_MS 輪詢一次（排程見 hooks/roomPoller.js），卸載即停。
// 工作室被編輯時，RoomsSection 會發出 miniapp:rooms-changed。

const ROOMS_CHANGED_EVENT = 'miniapp:rooms-changed';

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
  const generation = useRef(0);

  const load = useCallback(async () => {
    const mine = ++generation.current;
    try {
      const res = await apiFetch(roomUrl(roomId));
      if (mine !== generation.current) return;
      if (res.status === 404) { setNotFound(true); return; }
      if (!res.ok) { setError(await roomApiError(res, '載入工作室失敗')); return; }
      const data = await res.json();
      if (mine !== generation.current) return;
      setRoom(data);
      setError('');
    } catch (_) {
      if (mine === generation.current) setError('載入工作室失敗');
    }
  }, [roomId]);

  useEffect(() => {
    setRoom(null); setError(''); setNotFound(false);
    load();
    return () => { ++generation.current; };
  }, [load]);

  useEffect(() => {
    if (notFound) return undefined; // 工作室已被刪除：不會自己好起來，停止輪詢
    window.addEventListener(ROOMS_CHANGED_EVENT, load);
    const stop = startRoomPoller({ tick: load });
    return () => { window.removeEventListener(ROOMS_CHANGED_EVENT, load); stop(); };
  }, [load, notFound]);

  return { room, error, notFound };
}

function useRoomTimeline(roomId) {
  const [state, setState] = useState(roomTimelineEmpty);
  const [error, setError] = useState('');
  const [notFound, setNotFound] = useState(false);
  const [loadingOlder, setLoadingOlder] = useState(false);
  const generation = useRef(0);
  const stateRef = useRef(state);
  stateRef.current = state;
  const olderBusy = useRef(false);

  const fetchPage = useCallback(async (before) => {
    const mine = generation.current;
    const res = await apiFetch(roomTimelineUrl(roomId, before, ROOM_PAGE_SIZE));
    if (mine !== generation.current) return null;
    if (res.status === 404) { setNotFound(true); return null; }
    if (!res.ok) throw new Error(await roomApiError(res, '載入時間軸失敗'));
    const page = await res.json();
    return mine === generation.current ? page : null;
  }, [roomId]);

  const refresh = useCallback(async () => {
    try {
      const page = await fetchPage('');
      if (!page) return;
      setState((prev) => roomTimelineApply(prev, page, 'latest'));
      setError('');
    } catch (err) {
      setError(err.message || '載入時間軸失敗');
    }
  }, [fetchPage]);

  // 取更早的一頁；游標在請求期間被輪詢「重來」改掉時，這一頁已經接不上，直接丟棄。
  const loadOlder = useCallback(async () => {
    const { hasMore, nextCursor } = stateRef.current;
    if (!hasMore || !nextCursor || olderBusy.current) return;
    olderBusy.current = true;
    setLoadingOlder(true);
    try {
      const page = await fetchPage(nextCursor);
      if (page && stateRef.current.nextCursor === nextCursor) {
        setState((prev) => roomTimelineApply(prev, page, 'older'));
      }
    } catch (err) {
      setError(err.message || '載入更早訊息失敗');
    } finally {
      olderBusy.current = false;
      setLoadingOlder(false);
    }
  }, [fetchPage]);

  // 換工作室或工作室被編輯（成員可能變了）：整條時間軸重載，不拿舊成員的訊息混進來。
  useEffect(() => {
    const reset = () => {
      ++generation.current;
      olderBusy.current = false;
      setLoadingOlder(false);
      setState(roomTimelineEmpty());
      setError('');
      refresh();
    };
    setNotFound(false);
    reset();
    window.addEventListener(ROOMS_CHANGED_EVENT, reset);
    return () => { window.removeEventListener(ROOMS_CHANGED_EVENT, reset); ++generation.current; };
  }, [refresh]);

  useEffect(() => {
    if (notFound) return undefined;
    return startRoomPoller({ tick: refresh });
  }, [refresh, notFound]);

  return { state, error, notFound, loadingOlder, loadOlder, refresh };
}
