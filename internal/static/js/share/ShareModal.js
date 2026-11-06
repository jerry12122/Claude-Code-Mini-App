// ── 分享聊天室（擁有者端）────────────────────────────────────────────────────────
// ShareModal：在聊天室頂欄按「分享」開啟，建立臨時連結＋PIN，並管理這個聊天室目前有效的分享。
// ShareRow／格式化工具同時給設定頁的 SharesSection 使用。

const SHARE_TTL_OPTIONS = [
  { label: '15 分鐘', sec: 900 },
  { label: '1 小時', sec: 3600 },
  { label: '6 小時', sec: 21600 },
  { label: '24 小時', sec: 86400 },
  { label: '7 天', sec: 604800 },
];

/** 完整分享連結：用目前瀏覽器所在的 origin，並套用反代前綴（appPath）。 */
function shareLinkOf(s) {
  return `${window.location.origin}${appPath(s.path || `/share/${s.token}`)}`;
}

/** 含 PIN 的分享連結：開啟後加入表單會自動填入 PIN（見 JoinView 的 ?pin=），訪客只需輸入暱稱。 */
function shareLinkWithPin(s) {
  const link = shareLinkOf(s);
  return s.pin ? `${link}?pin=${encodeURIComponent(s.pin)}` : link;
}

/** 剩餘毫秒 → 「1 小時 5 分」這類短字串。 */
function formatRemaining(ms) {
  if (ms <= 0) return '已結束';
  const s = Math.floor(ms / 1000);
  const d = Math.floor(s / 86400);
  const h = Math.floor((s % 86400) / 3600);
  const m = Math.floor((s % 3600) / 60);
  if (d > 0) return `${d} 天 ${h} 小時`;
  if (h > 0) return `${h} 小時 ${m} 分`;
  if (m > 0) return `${m} 分`;
  return `${s} 秒`;
}

function shareRoleLabel(s) {
  if (s.mode === 'snapshot') return '唯讀快照';
  return s.role === 'editor' ? '可編輯' : '唯讀即時';
}

/** active / expired / revoked，另外 PIN 鎖定視為「已鎖定」。 */
function shareStatusInfo(s) {
  if (s.status === 'revoked') return { key: 'ended', label: '已結束' };
  if (s.status === 'expired') return { key: 'ended', label: '已到期' };
  if (s.locked) return { key: 'locked', label: '已鎖定' };
  return { key: 'active', label: '進行中' };
}

function useNowTick(ms = 1000) {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const t = setInterval(() => setNow(Date.now()), ms);
    return () => clearInterval(t);
  }, [ms]);
  return now;
}

async function readApiError(res, fallback) {
  try {
    const j = await res.json();
    if (j && j.error) return j.error;
  } catch (_) {}
  return fallback;
}

const shareBtnCls = 'px-2 py-1 rounded-md text-xs bg-ra-raised hover:bg-ra-raised text-ra-t2 border border-ra-line disabled:opacity-40';

/**
 * 單一分享的資訊卡：連結／PIN（可複製）、剩餘時間、線上名單、結束分享。
 * showSession：列出所屬聊天室名稱（設定頁用）；onJump：點名稱跳到該聊天室。
 */
function ShareRow({ share, onRevoke, onClear = null, clearing = false, showSession = false, onJump = null, pinRevealed = true }) {
  const now = useNowTick(1000);
  const [showPin, setShowPin] = useState(pinRevealed);
  const info = shareStatusInfo(share);
  const ended = info.key !== 'active';
  const remaining = new Date(share.expires_at).getTime() - now;
  const link = shareLinkOf(share);
  const online = Array.isArray(share.online) ? share.online : [];

  const copy = async (text, what) => {
    const ok = await copyText(text);
    showToast(ok ? `已複製${what}` : '複製失敗，請手動選取', { error: !ok });
  };

  const dot = info.key === 'active' ? 'bg-emerald-400' : info.key === 'locked' ? 'bg-amber-400' : 'bg-gray-600';

  return (
    <div className={`rounded-xl border border-ra-line bg-ra-card p-3 text-sm space-y-2 ${ended ? 'opacity-55' : ''}`} data-share-id={share.id}>
      <div className="flex items-center gap-2 min-w-0">
        <span className={`w-2 h-2 rounded-full shrink-0 ${dot}`} aria-hidden />
        <span className="text-xs font-semibold text-ra-t1">{shareRoleLabel(share)}</span>
        <span className="text-[11px] text-ra-t4">{info.label}</span>
        {showSession && (
          onJump ? (
            <button type="button" onClick={() => onJump(share.session_id)} className="min-w-0 truncate text-xs text-violet-300 hover:underline" title="跳到這個聊天室">
              {share.session_name || share.session_id}
            </button>
          ) : (
            <span className="min-w-0 truncate text-xs text-ra-t3">{share.session_name || share.session_id}</span>
          )
        )}
        <span className="ml-auto shrink-0 text-[11px] text-ra-t4 tabular-nums">
          {ended ? '' : `剩 ${formatRemaining(remaining)}`}
        </span>
      </div>

      <div className="flex items-center gap-2 min-w-0">
        <input
          readOnly
          value={link}
          onFocus={(e) => e.target.select()}
          className="flex-1 min-w-0 rounded-md bg-ra-sunken border border-ra-line px-2 py-1 text-xs ra-mono text-ra-t2"
        />
        <button type="button" className={shareBtnCls} onClick={() => copy(link, '連結')}>複製連結</button>
        <button type="button" className={shareBtnCls} onClick={() => copy(shareLinkWithPin(share), '連結（含 PIN）')}
          title="連結已帶入 PIN，對方開啟後只需輸入暱稱">複製連結＋PIN</button>
      </div>

      <div className="flex items-center gap-2 flex-wrap">
        <span className="text-xs text-ra-t4">PIN</span>
        <span className="ra-mono text-base tracking-[0.25em] text-ra-t1 select-all">{showPin ? share.pin : '••••••'}</span>
        <button type="button" className={shareBtnCls} onClick={() => setShowPin(!showPin)}>{showPin ? '隱藏' : '顯示'}</button>
        <button type="button" className={shareBtnCls} onClick={() => copy(share.pin, ' PIN')}>複製 PIN</button>
        <button type="button" className={shareBtnCls} onClick={() => copy(`${link}\nPIN：${share.pin}`, '連結與 PIN')}>複製全部</button>
      </div>

      <div className="flex items-center gap-2 text-xs text-ra-t4">
        <span className="min-w-0 truncate">
          {ended ? '' : online.length > 0 ? `線上：${online.join('、')}` : '目前沒有訪客在線'}
          {share.locked && info.key !== 'ended' ? ' · PIN 錯誤次數過多，已鎖定' : ''}
        </span>
        {info.key === 'ended' && onClear && (
          <button type="button" onClick={() => onClear(share)} disabled={clearing}
            className={`${shareBtnCls} ml-auto shrink-0`} title="清除這筆分享紀錄，保留聊天室內容">
            清除紀錄
          </button>
        )}
        {!ended || info.key === 'locked' ? (
          <button type="button" onClick={() => onRevoke(share)}
            className="ml-auto shrink-0 px-2 py-1 rounded-md text-xs bg-red-900/60 hover:bg-red-800 text-red-200">
            結束分享
          </button>
        ) : null}
      </div>
    </div>
  );
}

function ShareModal({ session, onClose }) {
  const [role, setRole] = useState('viewer');
  const [mode, setMode] = useState('live');
  const [ttl, setTtl] = useState(3600);
  const [busy, setBusy] = useState(false);
  const [shares, setShares] = useState([]);
  const [loaded, setLoaded] = useState(false);

  const load = useCallback(async () => {
    try {
      const res = await apiFetch(`/sessions/${session.id}/shares`);
      if (res.ok) {
        const data = await res.json();
        setShares(Array.isArray(data) ? data : []);
      }
    } catch (_) {
    } finally {
      setLoaded(true);
    }
  }, [session.id]);

  useEffect(() => {
    load();
    const t = setInterval(load, 5000); // 順便更新線上名單
    return () => clearInterval(t);
  }, [load]);

  useEffect(() => {
    const h = (e) => { if (e.key === 'Escape') onClose(); };
    window.addEventListener('keydown', h);
    return () => window.removeEventListener('keydown', h);
  }, [onClose]);

  const pickRole = (r) => {
    setRole(r);
    if (r === 'editor') setMode('live'); // 可編輯一定是即時；snapshot 只能唯讀
  };

  const create = async () => {
    if (busy) return;
    setBusy(true);
    try {
      const res = await apiFetch(`/sessions/${session.id}/shares`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ ttl, role, mode }),
      });
      if (!res.ok) {
        showToast(await readApiError(res, '建立分享失敗'), { error: true, duration: 4000 });
        return;
      }
      showToast('已建立分享');
      await load();
    } catch (_) {
      showToast('建立分享失敗', { error: true });
    } finally {
      setBusy(false);
    }
  };

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

  const active = shares.filter((s) => s.status === 'active');
  const optBtn = (on) => `flex-1 rounded-lg border px-3 py-2 text-left text-xs transition-colors ${on
    ? 'border-violet-500/70 bg-violet-500/15 text-violet-100'
    : 'border-ra-line bg-ra-card text-ra-t3 hover:border-ra-line-strong'}`;

  return (
    <div className="fixed inset-0 z-[110] flex items-center justify-center p-4 bg-black/60 backdrop-blur-[2px]" onClick={onClose} role="presentation">
      <div
        className="w-full max-w-lg max-h-[88vh] flex flex-col overflow-hidden rounded-2xl border border-ra-line bg-ra-bg shadow-2xl shadow-black/50"
        onClick={(e) => e.stopPropagation()}
        role="dialog"
        aria-modal="true"
        aria-label="分享聊天室"
      >
        <div className="shrink-0 flex items-center justify-between px-5 pt-4 pb-3">
          <div className="min-w-0">
            <div className="text-sm font-semibold text-ra-t1">分享聊天室</div>
            <div className="text-xs text-ra-t4 truncate">{session.name || '未命名'}</div>
          </div>
          <button type="button" onClick={onClose} className="text-ra-t4 hover:text-ra-t1 text-lg leading-none px-1" aria-label="關閉">×</button>
        </div>

        <div className="flex-1 min-h-0 overflow-y-auto app-scroll px-5 pb-5 space-y-4">
          <div className="space-y-3 rounded-xl border border-ra-line-soft p-3">
            <div>
              <div className="mb-1.5 text-xs text-ra-t4">對方的權限</div>
              <div className="flex gap-2">
                <button type="button" className={optBtn(role === 'viewer')} onClick={() => pickRole('viewer')}>
                  <div className="font-semibold">僅檢視</div>
                  <div className="opacity-70">只能看，不能送出任何指令</div>
                </button>
                <button type="button" className={optBtn(role === 'editor')} onClick={() => pickRole('editor')}>
                  <div className="font-semibold">可編輯</div>
                  <div className="opacity-70">可送訊息、回應授權，等同代你操作</div>
                </button>
              </div>
            </div>
            <div>
              <div className="mb-1.5 text-xs text-ra-t4">內容</div>
              <div className="flex gap-2">
                <button type="button" className={optBtn(mode === 'live')} onClick={() => setMode('live')}>
                  <div className="font-semibold">即時</div>
                  <div className="opacity-70">看到之後的新訊息與進度</div>
                </button>
                <button type="button" className={`${optBtn(mode === 'snapshot')} ${role === 'editor' ? 'opacity-40 cursor-not-allowed' : ''}`}
                  disabled={role === 'editor'} onClick={() => setMode('snapshot')}
                  title={role === 'editor' ? '可編輯必須是即時' : ''}>
                  <div className="font-semibold">快照</div>
                  <div className="opacity-70">只看現在為止的內容，不會更新</div>
                </button>
              </div>
            </div>
            <div>
              <div className="mb-1.5 text-xs text-ra-t4">有效時間</div>
              <div className="flex flex-wrap gap-1.5">
                {SHARE_TTL_OPTIONS.map((o) => (
                  <button key={o.sec} type="button"
                    className={`rounded-md border px-2.5 py-1 text-xs ${ttl === o.sec ? 'border-violet-500/70 bg-violet-500/15 text-violet-100' : 'border-ra-line text-ra-t3 hover:border-ra-line-strong'}`}
                    onClick={() => setTtl(o.sec)}>
                    {o.label}
                  </button>
                ))}
              </div>
            </div>
            {role === 'editor' && (
              <div className="rounded-lg bg-amber-500/10 border border-amber-700/40 px-3 py-2 text-xs text-amber-200/90">
                可編輯的訪客能讓 agent 在你的工作目錄執行動作，請只分享給信任的人。
              </div>
            )}
            <button type="button" onClick={create} disabled={busy}
              className="w-full py-2 rounded-lg bg-violet-600 hover:bg-violet-500 disabled:opacity-50 text-white text-sm font-medium">
              {busy ? '建立中…' : '建立分享連結'}
            </button>
          </div>

          <div>
            <div className="mb-2 text-xs text-ra-t4">目前有效的分享{active.length ? `（${active.length}）` : ''}</div>
            {!loaded ? (
              <div className="text-xs text-ra-t5">載入中…</div>
            ) : active.length === 0 ? (
              <div className="text-xs text-ra-t5">還沒有分享。建立後把連結與 PIN 傳給對方。</div>
            ) : (
              <div className="space-y-2.5">
                {active.map((s) => <ShareRow key={s.id} share={s} onRevoke={revoke} />)}
              </div>
            )}
          </div>
        </div>
      </div>
    </div>
  );
}
