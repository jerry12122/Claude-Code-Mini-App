// ── 分享聊天室（訪客端）──────────────────────────────────────────────────────────
// 網址 /share/<token> 進入訪客模式（isGuestMode，見 core.js）：
//   輸入 PIN＋暱稱 → POST /share/:token/join → 取得 guest token（存 sessionStorage）→ 進入聊天室。

const GUEST_NICK_KEY = 'cc_guest_nick';

/** 距離分享到期的毫秒數；expiresAt 為空回 null。每 20 秒更新一次（夠用於「5 分鐘前警告」）。 */
function useShareRemaining(expiresAt) {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    if (!expiresAt) return undefined;
    const t = setInterval(() => setNow(Date.now()), 20000);
    return () => clearInterval(t);
  }, [expiresAt]);
  if (!expiresAt) return null;
  return new Date(expiresAt).getTime() - now;
}

/** 訪客專用提示列：快照說明、到期預警、已結束、在線名單。擁有者端只在有訪客在線時顯示在線名單。 */
function ShareStatusBar({ guest, online = [], remainingMs = null, ended = false, endReason = '' }) {
  const isGuest = !!guest;
  const bars = [];
  if (isGuest && guest.mode === 'snapshot') {
    const hm = formatMessageTime(guest.created_at) || '';
    bars.push(
      <div key="snap" className="bg-sky-500/10 text-sky-200 border-b border-sky-700/30 px-4 py-1.5 text-xs">
        唯讀快照（截至 {hm}）· 之後的新訊息不會出現
      </div>
    );
  }
  if (isGuest && ended) {
    bars.push(
      <div key="end" className="bg-red-500/15 text-red-200 border-b border-red-700/40 px-4 py-1.5 text-xs font-semibold">
        分享已結束{endReason === 'expired' ? '（已到期）' : endReason === 'revoked' ? '（擁有者已結束分享）' : ''}
      </div>
    );
  } else if (isGuest && guest.mode !== 'snapshot' && remainingMs != null && remainingMs > 0 && remainingMs <= 5 * 60 * 1000) {
    bars.push(
      <div key="warn" className="bg-amber-500/10 text-amber-200 border-b border-amber-700/30 px-4 py-1.5 text-xs">
        分享將在 {Math.max(1, Math.ceil(remainingMs / 60000))} 分鐘內結束
      </div>
    );
  }
  const showOnline = online.length > 0 && (isGuest || online.length > 1) && !(isGuest && ended);
  if (showOnline) {
    bars.push(
      <div key="online" className="border-b border-ra-line-soft bg-ra-sunken px-4 py-1.5 text-xs text-ra-t4 flex items-center gap-1.5 flex-wrap" data-share-online>
        <span className="w-1.5 h-1.5 rounded-full bg-emerald-400" aria-hidden />
        線上：
        {online.map((n) => (
          <span key={n} className={`px-1.5 py-0.5 rounded bg-ra-card ${isGuest && n === guest.nickname ? 'text-violet-200' : 'text-ra-t2'}`}>{n}</span>
        ))}
      </div>
    );
  }
  if (bars.length === 0) return null;
  return <div className="shrink-0">{bars}</div>;
}

/** 取代輸入區的提示列（viewer／快照／分享已結束）。 */
function GuestReadOnlyBar({ snapshot, ended }) {
  const text = ended ? '分享已結束，無法再傳送訊息' : snapshot ? '這是唯讀快照，不會更新' : '你是唯讀訪客，無法傳送訊息';
  return (
    <div className="shrink-0 px-4 py-3 border-t border-ra-line-soft bg-ra-bg text-center text-xs text-ra-t4"
      style={{ paddingBottom: 'calc(0.75rem + env(safe-area-inset-bottom))' }}>
      {text}
    </div>
  );
}

function ShareEndedView({ message }) {
  return (
    <div className="flex items-center justify-center h-app px-4">
      <div className="w-80 text-center space-y-3">
        <div className="mx-auto w-10 h-10 rounded-[10px] bg-gradient-to-br from-gray-600 to-gray-700 flex items-center justify-center" aria-hidden>
          <div className="w-3.5 h-3.5 rounded-[3px] bg-white/80" />
        </div>
        <div className="ra-display text-xl text-ra-t1">分享已結束</div>
        <div className="text-sm text-ra-t4">{message || '這個分享連結已被結束或已到期。如需繼續，請向擁有者索取新的連結。'}</div>
      </div>
    </div>
  );
}

/** 讀網址的選填參數 ?pin=123456&name=Alex，用來預填加入表單（不會自動送出）。 */
function readJoinPrefill() {
  try {
    const q = new URLSearchParams(window.location.search);
    return {
      pin: (q.get('pin') || '').replace(/\D/g, '').slice(0, 6),
      name: (q.get('name') || '').trim().slice(0, 20),
    };
  } catch (_) {
    return { pin: '', name: '' };
  }
}

function JoinView({ token, onJoined }) {
  const [prefill] = useState(readJoinPrefill);
  const [pin, setPin] = useState(prefill.pin);
  // 網址帶 name 時優先於上次記住的暱稱
  const [nick, setNick] = useState(() => {
    if (prefill.name) return prefill.name;
    try { return localStorage.getItem(GUEST_NICK_KEY) || ''; } catch (_) { return ''; }
  });
  const [error, setError] = useState('');
  const [locked, setLocked] = useState(false);
  const [ended, setEnded] = useState('');
  const [loading, setLoading] = useState(false);

  const canSubmit = !loading && !locked && /^\d{6}$/.test(pin) && nick.trim() !== '';

  const submit = async () => {
    if (!canSubmit) return;
    setLoading(true);
    setError('');
    try {
      // join 是公開端點，不帶任何憑證。
      const res = await fetch(resolveApiUrl(`/share/${encodeURIComponent(token)}/join`), {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ pin, nickname: nick.trim() }),
      });
      const data = await res.json().catch(() => ({}));
      if (res.ok && data.guest_token) {
        try { localStorage.setItem(GUEST_NICK_KEY, nick.trim()); } catch (_) {}
        saveGuestAuth(data);
        onJoined(data);
        return;
      }
      if (res.status === 401) {
        setError(typeof data.remaining === 'number' ? `PIN 錯誤，還剩 ${data.remaining} 次機會` : 'PIN 錯誤');
        setPin('');
      } else if (res.status === 423) {
        setLocked(true);
        setError('PIN 錯誤次數過多，這個分享已鎖定。請向擁有者索取新的連結。');
      } else if (res.status === 410) {
        setEnded(data.error || '');
      } else if (res.status === 404) {
        setError('找不到這個分享連結，請確認網址是否完整。');
      } else if (res.status === 429) {
        setError('嘗試太頻繁，請稍後再試。');
      } else {
        setError(data.error || '加入失敗，請重試');
      }
    } catch (_) {
      setError('連線失敗，請重試');
    } finally {
      setLoading(false);
    }
  };

  if (ended) return <ShareEndedView message="這個分享已結束或已到期。如需繼續，請向擁有者索取新的連結。" />;

  const inputCls = 'w-full bg-gray-800 border border-gray-700 rounded-lg px-4 py-2.5 text-sm text-gray-200 placeholder-gray-500 focus:outline-none focus:border-violet-600 disabled:opacity-50';
  return (
    <div className="flex items-center justify-center h-app px-4">
      <form className="w-72 space-y-4" onSubmit={(e) => { e.preventDefault(); submit(); }}>
        <div className="text-center">
          <div className="flex flex-col items-center gap-2 mb-1">
            <div className="w-10 h-10 rounded-[10px] bg-gradient-to-br from-violet-500 to-fuchsia-600 flex items-center justify-center" aria-hidden>
              <div className="w-3.5 h-3.5 rounded-[3px] bg-white" />
            </div>
            <div className="ra-display text-2xl" style={{ color: 'oklch(var(--ra-t1))' }}>加入共享聊天室</div>
          </div>
          <div className="text-gray-600 text-xs">請輸入擁有者給你的 6 位數 PIN，並取一個暱稱</div>
        </div>
        <input
          type="text"
          inputMode="numeric"
          autoComplete="one-time-code"
          maxLength={6}
          value={pin}
          onChange={(e) => setPin(e.target.value.replace(/\D/g, '').slice(0, 6))}
          placeholder="PIN（6 位數字）"
          autoFocus
          disabled={locked}
          className={`${inputCls} text-center tracking-[0.4em] ra-mono`}
          aria-label="PIN"
        />
        <input
          type="text"
          maxLength={20}
          value={nick}
          onChange={(e) => setNick(e.target.value)}
          placeholder="你的暱稱"
          disabled={locked}
          className={inputCls}
          aria-label="暱稱"
        />
        {error && <div className="text-red-400 text-xs text-center" role="alert">{error}</div>}
        <button type="submit" disabled={!canSubmit}
          className="w-full py-2.5 bg-violet-600 hover:bg-violet-500 disabled:opacity-40 text-white rounded-lg text-sm font-medium">
          {loading ? '加入中…' : '加入'}
        </button>
      </form>
    </div>
  );
}

/** 訪客模式的根元件：沿用已存的 guest token（重新整理不必重輸 PIN），失效就回到加入畫面或結束畫面。 */
function GuestApp() {
  const [info, setInfo] = useState(() => readGuestAuth());
  const [phase, setPhase] = useState(() => (readGuestAuth() ? 'loading' : 'join'));

  useEffect(() => {
    document.title = '共享聊天室';
    applyAppearance(readStoredAppearance());
  }, []);

  // apiFetch 遇到 401（撤銷／到期）會呼叫這裡，整頁切到「分享已結束」。
  useEffect(() => {
    registerGuestEndedHandler(() => {
      clearGuestAuth();
      setPhase('ended');
    });
    return () => registerGuestEndedHandler(null);
  }, []);

  // 重新整理：用 guest token 向 /guest/me 取回最新的分享資訊。
  useEffect(() => {
    if (phase !== 'loading') return;
    let cancelled = false;
    apiFetch('/guest/me').then(async (res) => {
      if (cancelled) return;
      if (res.ok) {
        const data = await res.json();
        const merged = { ...(readGuestAuth() || {}), ...data };
        saveGuestAuth(merged);
        setInfo(merged);
        setPhase('chat');
      } else if (res.status !== 401) { // 401 已由 handler 切到 ended
        clearGuestAuth();
        setPhase('join');
      }
    }).catch(() => {
      if (!cancelled) setPhase('join');
    });
    return () => { cancelled = true; };
  }, [phase]);

  if (phase === 'loading') {
    return <div className="flex items-center justify-center h-app"><div className="text-gray-500 text-sm">載入中…</div></div>;
  }
  if (phase === 'ended') return <ShareEndedView />;
  if (phase === 'join' || !info || !info.session) {
    return <JoinView token={SHARE_TOKEN_FROM_URL} onJoined={(data) => { setInfo(data); setPhase('chat'); }} />;
  }
  return (
    <ChatView
      session={info.session}
      guest={{
        nickname: info.nickname,
        role: info.role,
        mode: info.mode,
        expires_at: info.expires_at,
        created_at: info.created_at,
        snapshot_msg_id: info.snapshot_msg_id,
      }}
      showBack={false}
      onBack={() => {}}
      allSessions={[]}
    />
  );
}
