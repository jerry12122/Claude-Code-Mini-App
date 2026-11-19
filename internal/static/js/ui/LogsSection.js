/** 設定頁「日誌」：即時顯示伺服器日誌（記憶體環形緩衝＋/logs/ws）。桌面版沒有 console 時用。 */
const LOG_LEVEL_RANK = { DEBUG: 0, INFO: 1, WARN: 2, ERROR: 3 };
const LOG_LEVEL_CLS = {
  DEBUG: 'text-ra-t5',
  INFO: 'text-ra-t2',
  WARN: 'text-amber-300',
  ERROR: 'text-red-400',
};
const LOG_MAX_ROWS = 1500;
const LOG_COLLAPSE_AT = 300;

const LogRow = React.memo(function LogRow({ e }) {
  const [open, setOpen] = useState(false);
  const long = e.msg.length > LOG_COLLAPSE_AT;
  const cls = LOG_LEVEL_CLS[e.level] || LOG_LEVEL_CLS.INFO;
  return (
    <div className="flex gap-2 px-2 py-0.5 hover:bg-ra-card">
      <span className="shrink-0 text-ra-t5">{e.ts.slice(11, 23)}</span>
      <span className={`shrink-0 w-11 ${cls}`}>{e.level}</span>
      <span
        className={`min-w-0 flex-1 whitespace-pre-wrap break-all ${cls} ${long && !open ? 'line-clamp-3 cursor-pointer' : ''}`}
        onClick={long ? () => setOpen(!open) : undefined}
        title={long ? '點擊展開／收合' : undefined}
      >
        {e.msg}
      </span>
    </div>
  );
});

const logBtnCls = 'inline-flex min-h-12 items-center justify-center self-end rounded-lg border border-ra-line bg-ra-card px-3 text-sm font-medium text-ra-t1 hover:bg-ra-raised focus-visible:outline focus-visible:outline-2 focus-visible:outline-ra-accent-text disabled:opacity-40 sm:min-h-10';
const logControlCls = 'min-h-12 rounded-lg border border-ra-line bg-ra-bg px-3 text-base text-ra-t1 placeholder:text-ra-t5 focus-visible:outline focus-visible:outline-2 focus-visible:outline-ra-accent-text sm:min-h-10';

function LogsSection() {
  const [entries, setEntries] = useState([]);
  const [minLevel, setMinLevel] = useState('INFO');
  const [query, setQuery] = useState('');
  const [frozen, setFrozen] = useState(null); // 暫停時的畫面快照；日誌照常累積在 entries
  const [connected, setConnected] = useState(false);
  const [debugOn, setDebugOn] = useState(false);
  const scrollRef = useRef(null);
  const stickRef = useRef(true); // 捲動位置在底部才自動跟著捲，往上看舊日誌時不拉回
  const idRef = useRef(0);

  const push = useCallback((list) => {
    const rows = list.map((e) => ({ ...e, id: ++idRef.current }));
    setEntries((prev) => prev.concat(rows).slice(-LOG_MAX_ROWS));
  }, []);

  // 只在日誌頁開著時連線；重連後 server 會重送 backlog，所以 onopen 先清空避免重複。
  useEffect(() => {
    let ws;
    let retry;
    let closed = false;
    const connect = () => {
      ws = new WebSocket(wsBaseURL('/logs/ws'));
      ws.onopen = () => {
        setConnected(true);
        setEntries([]);
        setFrozen(null);
      };
      ws.onmessage = (ev) => {
        try {
          const m = JSON.parse(ev.data);
          if (Array.isArray(m.entries) && m.entries.length) push(m.entries);
        } catch (_) {}
      };
      ws.onclose = () => {
        setConnected(false);
        if (!closed) retry = setTimeout(connect, 2000);
      };
    };
    connect();
    return () => {
      closed = true;
      clearTimeout(retry);
      if (ws) ws.close();
    };
  }, [push]);

  useEffect(() => {
    apiFetch('/logs/level')
      .then((res) => (res.ok ? res.json() : null))
      .then((j) => { if (j) setDebugOn(!!j.debug); })
      .catch(() => {});
  }, []);

  const view = frozen ?? entries;
  const shown = useMemo(() => {
    const min = LOG_LEVEL_RANK[minLevel];
    const q = query.trim().toLowerCase();
    return view.filter(
      (e) => (LOG_LEVEL_RANK[e.level] ?? 1) >= min && (!q || e.msg.toLowerCase().includes(q)),
    );
  }, [view, minLevel, query]);

  useEffect(() => {
    const el = scrollRef.current;
    if (el && stickRef.current) el.scrollTop = el.scrollHeight;
  }, [shown]);

  const onScroll = () => {
    const el = scrollRef.current;
    stickRef.current = el.scrollHeight - el.scrollTop - el.clientHeight < 40;
  };

  const copyShown = async () => {
    const ok = await copyText(shown.map((e) => `${e.ts}\t${e.level}\t${e.msg}`).join('\n'));
    showToast(ok ? `已複製 ${shown.length} 筆` : '複製失敗', { error: !ok });
  };

  const toggleDebug = async (on) => {
    try {
      const res = await apiFetch('/logs/level', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ debug: on }),
      });
      if (!res.ok) throw new Error(String(res.status));
      const j = await res.json();
      setDebugOn(!!j.debug);
      if (j.debug) setMinLevel('DEBUG'); // 不然開了也看不到
    } catch (_) {
      showToast('切換 Debug 失敗', { error: true });
    }
  };

  return (
    <div className="flex h-full min-h-0 flex-col gap-3">
      <div className="flex flex-wrap items-end gap-2">
        <div className="flex min-w-0 flex-col gap-1">
          <label htmlFor="settings-log-level" className="text-[13px] leading-5 text-ra-t3">等級</label>
          <select
            id="settings-log-level"
            value={minLevel}
            onChange={(e) => setMinLevel(e.target.value)}
            className={logControlCls}
          >
            <option value="DEBUG">DEBUG 以上</option>
            <option value="INFO">INFO 以上</option>
            <option value="WARN">WARN 以上</option>
            <option value="ERROR">ERROR</option>
          </select>
        </div>
        <div className="flex min-w-[8rem] flex-1 flex-col gap-1">
          <label htmlFor="settings-log-query" className="text-[13px] leading-5 text-ra-t3">搜尋</label>
          <input
            id="settings-log-query"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder="搜尋日誌…"
            autoCorrect="off"
            autoCapitalize="none"
            spellCheck={false}
            className={`${logControlCls} w-full`}
          />
        </div>
        <button type="button" onClick={() => setFrozen(frozen ? null : entries)} className={logBtnCls}>
          {frozen ? `繼續${entries.length > frozen.length ? `（+${entries.length - frozen.length}）` : ''}` : '暫停'}
        </button>
        <button type="button" onClick={() => { setEntries([]); setFrozen(null); }} className={logBtnCls}>清除</button>
        <button type="button" onClick={copyShown} className={logBtnCls}>複製</button>
      </div>

      <div
        ref={scrollRef}
        onScroll={onScroll}
        className="app-scroll min-h-0 flex-1 overflow-y-auto overscroll-contain rounded-lg border border-ra-line bg-ra-sunken py-1 font-mono text-[13px] leading-relaxed"
      >
        {shown.length === 0 ? (
          <div className="px-3 py-8 text-center text-[13px] leading-5 text-ra-t3">
            {connected ? '沒有符合的日誌' : '連線中…'}
          </div>
        ) : (
          shown.map((e) => <LogRow key={e.id} e={e} />)
        )}
      </div>

      <div className="flex items-center justify-between gap-3 text-[13px] leading-5 text-ra-t3">
        <span className="inline-flex min-w-0 items-center gap-2">
          <span className={`h-1.5 w-1.5 shrink-0 rounded-full ${connected ? 'bg-emerald-400' : 'bg-amber-400'}`} />
          <span className="truncate">
            {connected ? '已連線' : '連線中…'} · {shown.length}/{view.length} 筆（僅本次啟動後；完整紀錄見 logs/server.log）
          </span>
        </span>
        <label className="inline-flex min-h-12 shrink-0 cursor-pointer items-center gap-2 sm:min-h-10">
          <input
            type="checkbox"
            checked={debugOn}
            onChange={(e) => toggleDebug(e.target.checked)}
            className="h-5 w-5"
          />
          Debug 日誌
        </label>
      </div>
    </div>
  );
}
