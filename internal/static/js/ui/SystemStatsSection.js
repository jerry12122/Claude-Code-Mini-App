/** 設定頁「系統資源」：整機 CPU／記憶體唯讀快照。元件存在期間每 3 秒輪詢；離開分類或關閉設定即卸載並停止。 */
function StatBar({ label, text, percent }) {
  return (
    <div>
      <div className="flex items-baseline justify-between mb-1">
        <div className="text-xs text-ra-t2">{label}</div>
        <div className="text-xs text-ra-t1 ra-mono">{text}</div>
      </div>
      <div className="h-1.5 w-full rounded-full bg-ra-raised overflow-hidden">
        <div className="h-full bg-violet-500 transition-[width]" style={{ width: (percent || 0) + '%' }} />
      </div>
    </div>
  );
}

function SystemStatsSection() {
  const [stats, setStats] = useState(null);
  const [error, setError] = useState('');
  const [stopped, setStopped] = useState(false);
  const generation = useRef(0);

  const load = useCallback(async () => {
    const mine = ++generation.current;
    try {
      const res = await apiFetch('/system/stats');
      if (mine !== generation.current) return;
      if (!res.ok) {
        const msg = await readApiError(res, '載入系統資源失敗');
        // 501（平台不支援）與 403 不會自己好起來，停止輪詢避免每 3 秒打一次無用請求
        if (res.status === 501 || res.status === 403) setStopped(true);
        setError(msg);
        return;
      }
      setStats(await res.json());
      setError('');
    } catch (_) {
      if (mine === generation.current) setError('載入系統資源失敗');
    }
  }, []);

  useEffect(() => {
    if (stopped) return undefined;
    load();
    // 分頁在背景時略過，回到前景的下一個 tick 就會更新
    const t = setInterval(() => { if (!document.hidden) load(); }, SYSTEM_STATS_POLL_MS);
    return () => { clearInterval(t); ++generation.current; };
  }, [load, stopped]);

  const v = formatSystemStats(stats);
  return (
    <div className="divide-y divide-ra-line-soft [&>*]:py-6 [&>:first-child]:pt-0 [&>:last-child]:pb-0">
      <div>
        <div className="text-sm font-semibold text-ra-t1 mb-1">伺服器資源</div>
        <div className="text-[11px] text-ra-t5 mb-4">
          執行 agent 的這台主機整機的 CPU 與記憶體，每 3 秒更新；只在此頁開著時才會查詢。
        </div>
        <div className="space-y-4">
          <StatBar label="CPU" text={v.cpuText} percent={v.cpuPercent} />
          <StatBar label="記憶體" text={v.memText} percent={v.memPercent} />
        </div>
        {stats && stats.cpu_percent === null ? <div className="text-[11px] text-ra-t5 mt-3">CPU 需要兩次取樣才能計算，下一次更新就會顯示。</div> : null}
        {error ? <div className="text-xs text-red-400 mt-3">{error}</div> : null}
      </div>
    </div>
  );
}
