/** 設定頁「分享」：全部聊天室的分享清單。複製連結／PIN、結束單一分享、結束全部、依狀態篩選；已結束的灰顯。 */
const SHARE_FILTERS = [
  { id: 'active', label: '有效' },
  { id: 'ended', label: '已結束' },
  { id: 'all', label: '全部' },
];

function SharesSection({ onJumpToSession = null }) {
  const [shares, setShares] = useState([]);
  const [loaded, setLoaded] = useState(false);
  const [error, setError] = useState('');
  const [filter, setFilter] = useState('active');
  const [clearing, setClearing] = useState(false);
  const loadGeneration = useRef(0);

  const load = useCallback(async () => {
    const generation = ++loadGeneration.current;
    try {
      const res = await apiFetch('/shares');
      if (generation !== loadGeneration.current) return;
      if (!res.ok) {
        setError(await readApiError(res, '載入分享清單失敗'));
        return;
      }
      const data = await res.json();
      if (generation !== loadGeneration.current) return;
      setShares(Array.isArray(data) ? data : []);
      setError('');
    } catch (_) {
      if (generation === loadGeneration.current) setError('載入分享清單失敗');
    } finally {
      if (generation === loadGeneration.current) setLoaded(true);
    }
  }, []);

  useEffect(() => {
    load();
    const t = setInterval(load, 5000);
    return () => { clearInterval(t); ++loadGeneration.current; };
  }, [load]);

  const revoke = async (s) => {
    if (!window.confirm('結束這個分享？目前在線的訪客會立刻被踢出。')) return;
    try {
      const res = await apiFetch(`/shares/${s.id}`, { method: 'DELETE' });
      if (!res.ok && res.status !== 204) {
        showToast(await readApiError(res, '結束分享失敗'), { error: true });
        return;
      }
      showToast('已結束分享');
      load();
    } catch (_) {
      showToast('結束分享失敗', { error: true });
    }
  };

  const revokeAll = async () => {
    if (!window.confirm('結束全部有效的分享？所有在線訪客會立刻被踢出。')) return;
    try {
      const res = await apiFetch('/shares', { method: 'DELETE' });
      if (!res.ok) {
        showToast(await readApiError(res, '結束全部失敗'), { error: true });
        return;
      }
      const data = await res.json().catch(() => ({}));
      showToast(`已結束 ${data.revoked ?? 0} 個分享`);
      load();
    } catch (_) {
      showToast('結束全部失敗', { error: true });
    }
  };

  const clearEnded = async (share = null) => {
    if (clearing) return;
    if (!share && !window.confirm('清除全部已結束或已到期的分享紀錄？聊天室內容會保留。')) return;
    setClearing(true);
    try {
      const res = await apiFetch(share ? `/shares/ended/${share.id}` : '/shares/ended', { method: 'DELETE' });
      if (!res.ok) {
        showToast(await readApiError(res, '清除分享紀錄失敗'), { error: true });
        return;
      }
      const result = await res.json();
      ++loadGeneration.current;
      setShares(previous => previous.filter(s => share ? s.id !== share.id : s.status !== 'revoked' && s.status !== 'expired'));
      showToast(`已清除 ${result.cleared} 筆分享紀錄`);
      await load();
    } catch (_) {
      showToast('清除分享紀錄失敗', { error: true });
    } finally {
      setClearing(false);
    }
  };

  const isActive = (s) => s.status === 'active';
  const activeCount = shares.filter(isActive).length;
  const endedCount = shares.filter(s => s.status === 'revoked' || s.status === 'expired').length;
  const visible = shares.filter((s) => filter === 'all' || (filter === 'active' ? isActive(s) : !isActive(s)));

  return (
    <div className="flex min-w-0 flex-col gap-6">
      <div className="flex flex-col gap-2">
        <h3 className="text-[15px] font-medium leading-6 text-ra-t1">分享</h3>
        <p className="text-[13px] leading-5 text-ra-t3">管理連結與 PIN。結束分享會立刻請訪客離開；清除紀錄不會刪聊天室。</p>
      </div>
      <div className="flex flex-wrap items-center gap-2">
        <div className="inline-flex max-w-full flex-wrap gap-1 rounded-lg border border-ra-line bg-ra-sunken p-1" role="group" aria-label="分享狀態">
          {SHARE_FILTERS.map((f) => (
            <button key={f.id} type="button" onClick={() => setFilter(f.id)}
              aria-pressed={filter === f.id}
              className={`min-h-12 rounded-md px-3 text-sm focus-visible:outline focus-visible:outline-2 focus-visible:outline-ra-accent-text sm:min-h-10 ${filter === f.id ? 'bg-ra-card font-medium text-ra-t1' : 'text-ra-t3 hover:text-ra-t1'}`}>
              {f.label}
            </button>
          ))}
        </div>
        <button type="button" onClick={revokeAll} disabled={activeCount === 0}
          className="min-h-12 rounded-lg bg-red-900/60 px-3 text-sm text-red-200 hover:bg-red-800 disabled:opacity-30 sm:min-h-10">
          結束全部（{activeCount}）
        </button>
        <button type="button" onClick={() => clearEnded()} disabled={!endedCount || clearing}
          className="min-h-12 rounded-lg border border-ra-line bg-ra-card px-3 text-sm font-medium text-ra-t1 hover:bg-ra-raised disabled:cursor-default disabled:opacity-40 sm:min-h-10">
          {clearing ? '清除中…' : `清除已結束（${endedCount}）`}
        </button>
      </div>

      {error && <p role="alert" className="text-[13px] leading-5 text-red-400">{error}</p>}
      {!loaded ? (
        <p className="text-[13px] leading-5 text-ra-t3">載入中…</p>
      ) : visible.length === 0 ? (
        <p className="py-6 text-[13px] leading-5 text-ra-t3">
          {filter === 'active' ? '沒有進行中的分享。在聊天室頂欄按「分享」建立。' : '沒有符合的分享。'}
        </p>
      ) : (
        <div className="flex min-w-0 flex-col gap-3">
          {visible.map((s) => (
            <ShareRow key={s.id} share={s} onRevoke={revoke} onClear={clearEnded} clearing={clearing} showSession onJump={onJumpToSession} pinRevealed={false} variant="settings" />
          ))}
        </div>
      )}
    </div>
  );
}
