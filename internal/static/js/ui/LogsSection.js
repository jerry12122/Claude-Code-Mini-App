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

const logBtnCls = 'px-2.5 py-1.5 rounded-lg bg-ra-raised hover:bg-ra-raised text-ra-t2 border border-ra-line';

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
    <div className="h-full min-h-0 flex flex-col gap-2">
      <div className="flex flex-wrap items-center gap-2 text-xs">
        <select
          value={minLevel}
          onChange={(e) => setMinLevel(e.target.value)}
          className="bg-ra-card border border-ra-line rounded-lg px-2 py-1.5 text-ra-t2"
        >
          <option value="DEBUG">DEBUG 以上</option>
          <option value="INFO">INFO 以上</option>
          <option value="WARN">WARN 以上</option>
          <option value="ERROR">ERROR</option>
        </select>
        <input
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          placeholder="搜尋日誌…"
          className="flex-1 min-w-[8rem] bg-ra-card border border-ra-line rounded-lg px-2.5 py-1.5 text-ra-t1 placeholder-ra-t5 focus:outline-none focus:border-violet-500"
        />
        <button type="button" onClick={() => setFrozen(frozen ? null : entries)} className={logBtnCls}>
          {frozen ? `繼續${entries.length > frozen.length ? `（+${entries.length - frozen.length}）` : ''}` : '暫停'}
        </button>
        <button type="button" onClick={() => { setEntries([]); setFrozen(null); }} className={logBtnCls}>清除</button>
        <button type="button" onClick={copyShown} className={logBtnCls}>複製</button>
      </div>

      <div
        ref={scrollRef}
        onScroll={onScroll}
        className="flex-1 min-h-0 overflow-y-auto app-scroll rounded-lg border border-ra-line-soft bg-ra-sunken font-mono text-[11.5px] leading-relaxed py-1"
      >
        {shown.length === 0 ? (
          <div className="px-3 py-6 text-center text-ra-t5">
            {connected ? '沒有符合的日誌' : '連線中…'}
          </div>
        ) : (
          shown.map((e) => <LogRow key={e.id} e={e} />)
        )}
      </div>

      <div className="flex items-center justify-between gap-3 text-[11px] text-ra-t5">
        <span className="inline-flex items-center gap-1.5 min-w-0">
          <span className={`w-1.5 h-1.5 shrink-0 rounded-full ${connected ? 'bg-emerald-400' : 'bg-amber-400'}`} />
          <span className="truncate">
            {connected ? '已連線' : '連線中…'} · {shown.length}/{view.length} 筆（僅本次啟動後；完整紀錄見 logs/server.log）
          </span>
        </span>
        <label className="inline-flex shrink-0 items-center gap-1.5 cursor-pointer">
          <input
            type="checkbox"
            checked={debugOn}
            onChange={(e) => toggleDebug(e.target.checked)}
            className="accent-violet-600"
          />
          Debug 日誌
        </label>
      </div>
    </div>
  );
}
