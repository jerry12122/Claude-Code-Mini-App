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

  const load = useCallback(async () => {
    try {
      const res = await apiFetch('/shares');
      if (!res.ok) {
        setError(await readApiError(res, '載入分享清單失敗'));
        return;
      }
      const data = await res.json();
      setShares(Array.isArray(data) ? data : []);
      setError('');
    } catch (_) {
      setError('載入分享清單失敗');
    } finally {
      setLoaded(true);
    }
  }, []);

  useEffect(() => {
    load();
    const t = setInterval(load, 5000);
    return () => clearInterval(t);
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

  const isActive = (s) => s.status === 'active';
  const activeCount = shares.filter(isActive).length;
  const visible = shares.filter((s) => filter === 'all' || (filter === 'active' ? isActive(s) : !isActive(s)));

  return (
    <div className="space-y-4">
      <div className="flex items-center gap-2 flex-wrap">
        <div className="text-sm font-semibold text-ra-t1">分享</div>
        <div className="flex gap-1 ml-2">
          {SHARE_FILTERS.map((f) => (
            <button key={f.id} type="button" onClick={() => setFilter(f.id)}
              className={`px-2.5 py-1 rounded-md text-xs ${filter === f.id ? 'bg-violet-500/20 text-violet-200' : 'text-ra-t4 hover:text-ra-t2'}`}>
              {f.label}
            </button>
          ))}
        </div>
        <button type="button" onClick={revokeAll} disabled={activeCount === 0}
          className="ml-auto px-2.5 py-1 rounded-md text-xs bg-red-900/60 hover:bg-red-800 text-red-200 disabled:opacity-30">
          結束全部（{activeCount}）
        </button>
      </div>

      {error && <div className="text-xs text-red-400">{error}</div>}
      {!loaded ? (
        <div className="text-xs text-ra-t5">載入中…</div>
      ) : visible.length === 0 ? (
        <div className="text-xs text-ra-t5">
          {filter === 'active' ? '沒有進行中的分享。在聊天室頂欄按「分享」建立。' : '沒有符合的分享。'}
        </div>
      ) : (
        <div className="space-y-2.5">
          {visible.map((s) => (
            <ShareRow key={s.id} share={s} onRevoke={revoke} showSession onJump={onJumpToSession} pinRevealed={false} />
          ))}
        </div>
      )}
    </div>
  );
}
