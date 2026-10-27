// ── Session 列表畫面 ──────────────────────────────────────────────────────────
function SessionView({ onEnter, onSessionsLoaded, onSortedSessionsChange, activeSessionId = null, onCreateNew, onOpenSettings, onToggleSidebar, collapsed = false, isSplit = false, onToggleSplit = null, isWideScreen = true }) {
  const [sessions, setSessions]   = useState([]);
  const [sessionSearch, setSessionSearch] = useState('');
  const [sessionSort, setSessionSort]     = useState('last_active_desc');
  const [groupByDir] = useState(true); // RemoteAgent：固定依工作目錄分組
  const [collapsedDirs, setCollapsedDirs] = useState(() => new Set());
  const [renamingId, setRenamingId] = useState(null);
  const [renameVal, setRenameVal]   = useState('');
  const renameInputRef = useRef(null);
  // 刪除有 5 秒復原窗口：這段期間先從列表隱藏，時間到才真的打 DELETE
  const pendingDelRef = useRef(new Set());
  const activeIdRef = useRef(activeSessionId);
  activeIdRef.current = activeSessionId;
  const prevStatusRef = useRef(null);

  // 非目前 session 從執行中變成完成／待授權時提示，避免人不在該 session 就錯過
  const notifyStatusChanges = (list) => {
    const prev = prevStatusRef.current;
    prevStatusRef.current = new Map(list.map((s) => [s.id, String(s.status || '').toLowerCase()]));
    if (!prev) return;
    for (const s of list) {
      if (prev.get(s.id) !== 'running' || s.id === activeIdRef.current || s.agent_type === 'telegram') continue;
      const st = String(s.status || '').toLowerCase();
      if (st === 'running') continue;
      const name = s.name || '未命名';
      showToast(st === 'awaiting_input' ? `「${name}」待回答` : st === 'awaiting_confirm' ? `「${name}」待授權` : `「${name}」已完成`, { duration: 3000 });
      return;
    }
  };

  const load = async () => {
    try {
      const res = await apiFetch('/sessions');
      if (!res.ok) {
        console.warn('[sessions] load failed', res.status);
        return;
      }
      const data = await res.json();
      if (Array.isArray(data)) {
        const sorted = sortSessionsByWorkDirThenName(data).filter((s) => !pendingDelRef.current.has(s.id));
        notifyStatusChanges(sorted);
        setSessions(sorted);
        onSessionsLoaded?.(sorted);
      } else {
        console.warn('[sessions] unexpected payload', data);
      }
    } catch (err) {
      console.warn('[sessions] load error', err);
    }
  };

  useEffect(() => { load(); }, []);

  // 即時更新走 /events WS；輪詢只當斷線時的保底，回到分頁時也立即補抓一次。
  useEffect(() => {
    let ws = null;
    let retry = null;
    let debounce = null;
    let closed = false;
    const connect = () => {
      ws = new WebSocket(eventsWsURL());
      ws.onmessage = (e) => {
        try {
          if (JSON.parse(e.data).type !== 'sessions_changed') return;
        } catch (_) { return; }
        clearTimeout(debounce);
        debounce = setTimeout(load, 150);
      };
      ws.onclose = () => { if (!closed) retry = setTimeout(connect, 3000); };
    };
    connect();
    const onVisible = () => { if (!document.hidden) load(); };
    document.addEventListener('visibilitychange', onVisible);
    const poll = setInterval(load, 30000);
    return () => {
      closed = true;
      clearTimeout(retry);
      clearTimeout(debounce);
      clearInterval(poll);
      document.removeEventListener('visibilitychange', onVisible);
      if (ws) ws.close();
    };
  }, []);

  useEffect(() => {
    if (renamingId) renameInputRef.current?.focus();
  }, [renamingId]);

  const del = (id, e) => {
    e.stopPropagation();
    const target = sessions.find((x) => x.id === id);
    pendingDelRef.current.add(id);
    setSessions((prev) => prev.filter((x) => x.id !== id));
    const timer = setTimeout(async () => {
      await apiFetch(`/sessions/${id}`, { method: 'DELETE' });
      pendingDelRef.current.delete(id);
      load();
    }, 5000);
    showToast(`已刪除「${(target && target.name) || '未命名'}」`, {
      duration: 5000,
      action: {
        label: '復原',
        onClick: () => { clearTimeout(timer); pendingDelRef.current.delete(id); load(); },
      },
    });
  };

  const startRename = (s, e) => {
    e.stopPropagation();
    setRenamingId(s.id);
    setRenameVal(s.name || '');
  };

  const commitRename = async (id) => {
    const trimmed = renameVal.trim();
    if (trimmed) {
      await apiFetch(`/sessions/${id}`, {
        method: 'PATCH',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ name: trimmed }),
      });
      await load();
    }
    setRenamingId(null);
    setRenameVal('');
  };

  const handleRenameKey = (e, id) => {
    if (e.key === 'Enter') { e.preventDefault(); commitRename(id); }
    if (e.key === 'Escape') { setRenamingId(null); setRenameVal(''); }
  };

  const sortOptions = [
    { value: 'last_active_desc', label: '活動 新→舊' },
    { value: 'last_active_asc', label: '活動 舊→新' },
    { value: 'name_asc', label: '名稱 A→Z' },
    { value: 'name_desc', label: '名稱 Z→A' },
    { value: 'agent_asc', label: 'Runner' },
  ];

  const displaySessions = useMemo(() => {
    const q = sessionSearch.trim().toLowerCase();
    let list = Array.isArray(sessions) ? sessions : [];
    if (q) {
      list = list.filter((s) => {
        const parts = [
          s.name,
          s.work_dir,
          s.git_branch,
          s.description,
          s.agent_type,
          s.disguise_agent,
          s.agent_session_id,
          s.id,
          ...(Array.isArray(s.cli_extra_args) ? s.cli_extra_args : []),
        ]
          .filter(Boolean)
          .join(' ')
          .toLowerCase();
        return parts.includes(q);
      });
    }
    const out = [...list];
    const cmpName = (a, b) => (a.name || '').localeCompare(b.name || '', 'zh-Hant');
    const cmpAgent = (a, b) => (a.agent_type || '').localeCompare(b.agent_type || '');
    if (sessionSort === 'last_active_asc' || sessionSort === 'last_active_desc') {
      const groupMaxMs = new Map();
      for (const s of out) {
        const k = String(s.work_dir != null ? String(s.work_dir).trim() : '').toLowerCase();
        const t = sessionLastActiveMs(s);
        if (!groupMaxMs.has(k) || t > groupMaxMs.get(k)) groupMaxMs.set(k, t);
      }
      const sign = sessionSort === 'last_active_desc' ? 1 : -1;
      out.sort((a, b) => {
        const ka = String(a.work_dir != null ? String(a.work_dir).trim() : '').toLowerCase();
        const kb = String(b.work_dir != null ? String(b.work_dir).trim() : '').toLowerCase();
        const gd = sign * ((groupMaxMs.get(kb) ?? 0) - (groupMaxMs.get(ka) ?? 0));
        if (gd !== 0) return gd;
        return sign * (sessionLastActiveMs(b) - sessionLastActiveMs(a));
      });
    } else {
      out.sort((a, b) => {
        const wd = cmpSessionWorkDir(a, b);
        if (wd !== 0) return wd;
        switch (sessionSort) {
          case 'name_asc':  return cmpName(a, b);
          case 'name_desc': return cmpName(b, a);
          case 'agent_asc': return cmpAgent(a, b) || cmpName(a, b);
          default: return 0;
        }
      });
    }
    return out;
  }, [sessions, sessionSearch, sessionSort]);

  const sortedSessionsForForward = useMemo(() => {
    const out = [...(Array.isArray(sessions) ? sessions : [])];
    const cmpName = (a, b) => (a.name || '').localeCompare(b.name || '', 'zh-Hant');
    const cmpAgent = (a, b) => (a.agent_type || '').localeCompare(b.agent_type || '');
    if (sessionSort === 'last_active_asc' || sessionSort === 'last_active_desc') {
      const groupMaxMs = new Map();
      for (const s of out) {
        const k = String(s.work_dir != null ? String(s.work_dir).trim() : '').toLowerCase();
        const t = sessionLastActiveMs(s);
        if (!groupMaxMs.has(k) || t > groupMaxMs.get(k)) groupMaxMs.set(k, t);
      }
      const sign = sessionSort === 'last_active_desc' ? 1 : -1;
      out.sort((a, b) => {
        const ka = String(a.work_dir != null ? String(a.work_dir).trim() : '').toLowerCase();
        const kb = String(b.work_dir != null ? String(b.work_dir).trim() : '').toLowerCase();
        const gd = sign * ((groupMaxMs.get(kb) ?? 0) - (groupMaxMs.get(ka) ?? 0));
        if (gd !== 0) return gd;
        return sign * (sessionLastActiveMs(b) - sessionLastActiveMs(a));
      });
    } else {
      out.sort((a, b) => {
        const wd = cmpSessionWorkDir(a, b);
        if (wd !== 0) return wd;
        switch (sessionSort) {
          case 'name_asc':  return cmpName(a, b);
          case 'name_desc': return cmpName(b, a);
          case 'agent_asc': return cmpAgent(a, b) || cmpName(a, b);
          default: return 0;
        }
      });
    }
    return out;
  }, [sessions, sessionSort]);

  useEffect(() => { onSortedSessionsChange?.(sortedSessionsForForward); }, [sortedSessionsForForward]);

  const groupedSessions = useMemo(() => {
    if (!groupByDir) return null;
    const map = new Map();
    const unset = '（未設定工作目錄）';
    for (const s of displaySessions) {
      const raw = s.work_dir != null ? String(s.work_dir).trim() : '';
      let key = raw || unset;
      if (s.agent_type === 'telegram' && !s.disguise_agent) {
        key = 'Telegram 聊天室';
      }
      if (!map.has(key)) map.set(key, []);
      map.get(key).push(s);
    }
    return [...map.entries()].sort((a, b) => {
      const ka = String(a[0]);
      const kb = String(b[0]);
      const tgGroup = 'Telegram 聊天室';
      if (ka === tgGroup && kb !== tgGroup) return -1;
      if (ka !== tgGroup && kb === tgGroup) return 1;
      if (ka === unset && kb !== unset) return 1;
      if (ka !== unset && kb === unset) return -1;
      if (sessionSort === 'last_active_asc' || sessionSort === 'last_active_desc') {
        const maxA = Math.max(0, ...a[1].map(sessionLastActiveMs));
        const maxB = Math.max(0, ...b[1].map(sessionLastActiveMs));
        if (maxA !== maxB) return sessionSort === 'last_active_desc' ? maxB - maxA : maxA - maxB;
      }
      return ka.localeCompare(kb, undefined, { numeric: true, sensitivity: 'base' });
    });
  }, [groupByDir, displaySessions, sessionSort]);

  const sessionSearchActive = sessionSearch.trim().length > 0;

  const toggleDirCollapsed = (dirKey) => {
    setCollapsedDirs((prev) => {
      const next = new Set(prev);
      if (next.has(dirKey)) next.delete(dirKey);
      else next.add(dirKey);
      return next;
    });
  };

  const openNewSessionForDir = (dirKey) => {
    const unset = '（未設定工作目錄）';
    const dir = dirKey === unset ? '' : dirKey;
    onCreateNew?.({ workDir: dir, name: workDirBasename(dirKey) });
  };

  // 未讀判斷：若後端有精確 unread_count 則以其為準；否則 last_active 比 last_read_at 新即為未讀。
  const isUnread = (s) => {
    if (typeof s.unread_count === 'number' && s.unread_count > 0) return true;
    if (typeof s.unread_count === 'number' && s.unread_count === 0) return false;
    const active = sessionLastActiveMs(s);
    if (!active) return false;
    const read = s.last_read_at ? Date.parse(String(s.last_read_at).trim().replace(' ', 'T')) : 0;
    return active > (Number.isNaN(read) ? 0 : read);
  };

  const unreadCount = useMemo(
    () => (Array.isArray(sessions) ? sessions : []).filter((s) => activeSessionId !== s.id && isUnread(s)).length,
    [sessions, activeSessionId]
  );
  const hasUnread = unreadCount > 0;

  // 分頁標題顯示未讀數，切到別的分頁／視窗也看得到
  useEffect(() => {
    document.title = (unreadCount > 0 ? `(${unreadCount}) ` : '') + 'Claude Code · Remote Agent';
  }, [unreadCount]);
  useEffect(() => () => { document.title = 'Claude Code · Remote Agent'; }, []);

  const handleReadAll = () => {
    // Optimistic：點了就消掉；背景打 API，不等回應、失敗不回滾，下次 5 秒 poll 覆蓋掉也沒差。
    const nowStr = formatSqliteNow();
    setSessions((prev) => prev.map((s) => ({ ...s, last_read_at: nowStr, unread_count: 0 })));
    apiFetch('/sessions/read-all', { method: 'POST' }).catch((err) => {
      console.warn('[sessions] read-all error', err);
    });
  };

  // 進入 session 前先 optimistic 標已讀（點了就消掉）；真正的 mark_read 由
  // useChatSocket.js 的 WS onopen 背景送出，失敗或被下次 poll 覆蓋都沒差。
  const handleEnter = (s, targetPane = null) => {
    if (isUnread(s)) {
      const nowStr = formatSqliteNow();
      setSessions((prev) => prev.map((row) => (row.id === s.id ? { ...row, last_read_at: nowStr, unread_count: 0 } : row)));
    }
    onEnter(s, targetPane);
  };

  const renderSessionRow = (s) => {
    const active = activeSessionId != null && s.id === activeSessionId;
    const extraArgN = Array.isArray(s.cli_extra_args) ? s.cli_extra_args.length : 0;
    const displayAgent = sessionDisplayAgent(s);
    const agentLabel = AGENT_LABEL[displayAgent] || displayAgent || 'claude';
    const unread = !active && isUnread(s);
    const unreadCountNum = typeof s.unread_count === 'number' ? s.unread_count : 1;
    const unreadBadgeText = unreadCountNum > 99 ? '99+' : String(unreadCountNum > 0 ? unreadCountNum : 1);
    const statusClass = sessionStatusDotClass(s);
    const rowTitle = [agentLabel, s.git_branch, extraArgN > 0 ? `+${extraArgN} CLI 引數` : '']
      .filter(Boolean)
      .join(' · ');
    return (
      <div
        key={s.id}
        draggable={isWideScreen && renamingId !== s.id ? true : undefined}
        onDragStart={(e) => {
          if (!isWideScreen) {
            e.preventDefault();
            return;
          }
          const payload = JSON.stringify({ sessionId: s.id, fromGroup: null });
          e.dataTransfer.setData(typeof MINIAPP_SESSION_MIME !== 'undefined' ? MINIAPP_SESSION_MIME : 'application/x-miniapp-session', payload);
          e.dataTransfer.effectAllowed = 'move';
          if (typeof window !== 'undefined') {
            window.__miniapp_drag_source = { sessionId: s.id, fromGroup: null };
          }
        }}
        onDragEnd={() => {
          if (typeof window !== 'undefined') {
            window.__miniapp_drag_source = null;
          }
        }}
        onClick={() => renamingId !== s.id && handleEnter(s)}
        className={'ra-session-card group/card' + (active ? ' active' : '') + (unread ? ' unread' : '')}
      >
          {renamingId === s.id ? (
            <input
              ref={renameInputRef}
              value={renameVal}
              onChange={(e) => setRenameVal(e.target.value)}
              onKeyDown={(e) => handleRenameKey(e, s.id)}
              onBlur={() => commitRename(s.id)}
              onClick={(e) => e.stopPropagation()}
              className="w-full bg-[oklch(0.19_0.02_264)] border border-violet-500 rounded-lg px-2 py-1 text-sm text-[oklch(0.94_0.01_264)] focus:outline-none"
            />
          ) : (
            <div className="flex items-center gap-2 min-w-0" title={rowTitle}>
              <span className={`inline-flex shrink-0 items-center justify-center w-[22px] h-[22px] rounded-[6px] ${getAgentBadgeClass(displayAgent, s)}`} title={agentLabel}>
                <AgentBadgeIcon agentType={displayAgent} session={s} />
              </span>
              <span
                className={
                  'flex-1 min-w-0 text-[13.5px] truncate leading-snug ' +
                  (unread ? 'font-semibold text-[oklch(0.94_0.01_264)]' : 'font-medium text-[oklch(0.75_0.01_264)]')
                }
              >
                {s.name || '未命名'}
              </span>
              <div className="flex items-center gap-0.5 shrink-0 sm:opacity-0 sm:group-hover/card:opacity-100 transition-opacity">
                <button
                  type="button"
                  onClick={(e) => startRename(s, e)}
                  aria-label="重新命名"
                  title="重新命名"
                  className="text-[oklch(0.5_0.01_264)] hover:text-violet-400 p-1 rounded transition-colors"
                >
                  <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" className="w-3.5 h-3.5" aria-hidden="true">
                    <path d="M11 4H4a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h14a2 2 0 0 0 2-2v-7" />
                    <path d="M18.5 2.5a2.121 2.121 0 0 1 3 3L12 15l-4 1 1-4 9.5-9.5z" />
                  </svg>
                </button>
                <button
                  type="button"
                  onClick={(e) => del(s.id, e)}
                  aria-label="刪除"
                  title="刪除"
                  className="text-[oklch(0.5_0.01_264)] hover:text-red-400 p-1 rounded transition-colors inline-flex items-center justify-center"
                >
                  <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" className="w-3.5 h-3.5" aria-hidden="true">
                    <path d="M3 6h18" />
                    <path d="M8 6V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2" />
                    <path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6" />
                    <path d="M10 11v6" />
                    <path d="M14 11v6" />
                  </svg>
                </button>
              </div>
              {statusClass ? (
                <span
                  className={statusClass + ' sm:group-hover/card:opacity-0 transition-opacity'}
                  title={sessionStatusLabel(s)}
                  aria-label={sessionStatusLabel(s)}
                />
              ) : null}
              {unread ? (
                <span
                  className="ra-unread-badge sm:group-hover/card:opacity-0 transition-opacity"
                  title={`未讀訊息 ${unreadBadgeText} 則`}
                  aria-label={`未讀訊息 ${unreadBadgeText} 則`}
                >
                  {unreadBadgeText}
                </span>
              ) : null}
            </div>
        )}
      </div>
    );
  };

  const renderFullPanel = () => (
    <div className="flex flex-col h-app min-w-0 overflow-hidden">
      {/* Header — 設計 1a / 2a */}
      <div className="flex items-center justify-between px-5 pt-[18px] pb-3 shrink-0"
           style={{ paddingTop: 'calc(18px + env(safe-area-inset-top) + var(--tg-content-safe-top))' }}>
        <div className="flex items-center gap-2 min-w-0">
          <div className="w-6 h-6 rounded-[6px] bg-gradient-to-br from-[oklch(0.62_0.19_275)] to-[oklch(0.6_0.17_300)] flex items-center justify-center shrink-0" aria-hidden>
            <div className="w-[9px] h-[9px] rounded-[2px] bg-white" />
          </div>
          <span className="ra-display text-base text-[oklch(0.94_0.01_264)] tracking-tight">Agent Console</span>
        </div>
        <div className="flex items-center gap-1.5 shrink-0">
          {onToggleSplit && (
            <SplitViewButton isSplit={isSplit} onClick={onToggleSplit} />
          )}
          {onToggleSidebar && (
            <button
              type="button"
              onClick={onToggleSidebar}
              aria-label="收合會話列表"
              title="收合會話列表"
              className="flex h-[30px] w-[30px] shrink-0 items-center justify-center rounded-[9px] border border-[oklch(0.28_0.02_264)] text-gray-400 hover:text-gray-200 hover:bg-[oklch(0.19_0.02_264)] transition-colors focus:outline-none"
            >
              <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 20 20" fill="currentColor" className="w-5 h-5 rotate-180" aria-hidden>
                <path fillRule="evenodd" d="M7.21 14.77a.75.75 0 01-.02-1.06L11.168 10 7.23 6.29a.75.75 0 111.04-1.08l4.5 4.25a.75.75 0 010 1.08l-4.5 4.25a.75.75 0 01-1.06-.02z" clipRule="evenodd" />
              </svg>
            </button>
          )}
        </div>
      </div>

      {/* 搜尋／排序 — 設計列 */}
      <div className="px-5 pb-3 shrink-0 flex gap-2 min-w-0 items-center">
        {sessions.length > 0 && (
          <div className="h-9 flex-1 min-w-0 flex items-center gap-1.5 bg-[oklch(0.19_0.02_264)] border border-[oklch(0.28_0.02_264)] rounded-[9px] px-2.5">
            <span className="text-[oklch(0.5_0.01_264)] text-sm shrink-0" aria-hidden>⌕</span>
            <input
              type="search"
              value={sessionSearch}
              onChange={(e) => setSessionSearch(e.target.value)}
              placeholder="搜尋 session…"
              autoComplete="off"
              className="flex-1 min-w-0 bg-transparent border-0 p-0 text-[13px] text-[oklch(0.9_0.01_264)] placeholder-[oklch(0.5_0.01_264)] focus:outline-none"
            />
          </div>
        )}
        <button
          type="button"
          onClick={() => onCreateNew?.()}
          aria-label="建立新 Session"
          title="新 Session"
          className="flex h-9 w-9 shrink-0 items-center justify-center rounded-[9px] bg-[oklch(0.62_0.19_275)] text-white text-lg leading-none transition hover:brightness-110 active:scale-95 focus:outline-none"
        >
          +
        </button>
        {/* 排序下拉選單暫時隱藏（無人使用），固定用預設的 last_active_desc；邏輯保留供日後復原。 */}
        {sessions.length > 0 && false && (
            <select
              id="session-sort"
              value={sessionSort}
              onChange={(e) => setSessionSort(e.target.value)}
              title="排序"
              aria-label="排序"
              className="shrink-0 bg-[oklch(0.19_0.02_264)] border border-[oklch(0.28_0.02_264)] rounded-[9px] px-2.5 py-2 text-xs text-[oklch(0.65_0.01_264)] focus:outline-none cursor-pointer max-w-[6.5rem]"
            >
              {sortOptions.map((opt) => (
                <option key={opt.value} value={opt.value}>{opt.label.replace('活動 ', '')}</option>
              ))}
            </select>
          )}
          {hasUnread && (
            <button
              type="button"
              onClick={handleReadAll}
              aria-label="全部標記已讀"
              title="全部標記已讀"
              className="flex h-9 w-9 shrink-0 items-center justify-center rounded-[9px] border border-[oklch(0.28_0.02_264)] bg-[oklch(0.19_0.02_264)] text-[oklch(0.65_0.01_264)] hover:text-violet-300 hover:border-violet-500/50 transition-colors focus:outline-none"
            >
              <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" className="w-4 h-4" aria-hidden="true">
                <path d="M2 12.5l4 4 9-10" />
                <path d="M9 12.5l4 4 9-10" />
              </svg>
            </button>
          )}
      </div>

      {/* Session 列表 */}
      <div className="flex-1 overflow-y-auto app-scroll px-3.5 pb-5">
        {sessions.length === 0 ? (
          <div className="text-center text-[oklch(0.5_0.01_264)] mt-24 text-sm px-4">尚無 Session，點 + 建立</div>
        ) : displaySessions.length === 0 ? (
          <div className="text-center text-gray-600 mt-24 text-sm px-4">沒有符合搜尋條件的 Session</div>
        ) : groupByDir && groupedSessions ? (
          groupedSessions.map(([dirKey, dirSessions]) => {
            const expanded = sessionSearchActive || !collapsedDirs.has(dirKey);
            const shortLabel = workDirGroupShortLabel(dirKey);
            // 同一 work_dir 即同一 git working tree，分支必然相同，取第一筆即可
            const dirBranch = (dirSessions.find((s) => s.git_branch) || {}).git_branch;
            return (
              <div key={dirKey} className="space-y-0">
                <div className="group/dir flex w-full min-w-0 items-center gap-1 px-2 py-1.5 mt-2 transition-colors hover:bg-violet-500/[0.06]">
                  <button
                    type="button"
                    onClick={() => toggleDirCollapsed(dirKey)}
                    className="flex min-w-0 flex-1 items-center gap-1.5 rounded-md px-1 py-1 text-left focus:outline-none focus-visible:ring-2 focus-visible:ring-violet-500/50 focus-visible:ring-offset-1 focus-visible:ring-offset-[oklch(0.13_0.02_264)]"
                  >
                    <span className="shrink-0 text-[oklch(0.5_0.01_264)] w-3 text-center text-[10px]" aria-hidden>
                      {expanded ? '▾' : '▸'}
                    </span>
                    <span className="shrink-0 text-[13px] leading-none" aria-hidden>📁</span>
                    <span
                      className="min-w-0 flex-1 truncate text-[12.5px] text-[oklch(0.72_0.01_264)] font-bold"
                      title={dirKey + (expanded ? '' : `（${dirSessions.length}）`)}
                    >
                      {shortLabel}
                    </span>
                  </button>
                  {dirBranch ? (
                    <span
                      className="ra-mono shrink-0 max-w-[8rem] truncate text-[11px] text-[oklch(0.5_0.01_264)]"
                      title={dirBranch}
                    >
                      {dirBranch}
                    </span>
                  ) : null}
                  <button
                    type="button"
                    onClick={() => openNewSessionForDir(dirKey)}
                    aria-label="在此目錄建立新 Session"
                    title="在此目錄建立新 Session"
                    className="shrink-0 rounded-md px-1.5 py-1 text-[13px] leading-none font-semibold text-violet-400 hover:bg-violet-500/10 hover:text-violet-300 focus:outline-none focus-visible:ring-2 focus-visible:ring-violet-500/50 sm:opacity-0 sm:group-hover/dir:opacity-100 transition-opacity"
                  >
                    ＋
                  </button>
                </div>
                {expanded && (
                  <div className="ml-[18px] border-l border-[oklch(0.26_0.02_264)] pl-2.5">
                    {dirSessions.map((s) => renderSessionRow(s))}
                  </div>
                )}
              </div>
            );
          })
        ) : (
          displaySessions.map((s) => renderSessionRow(s))
        )}
      </div>

      {/* 底部設定入口（仿 Claude Desktop 左下角） */}
      <div className="shrink-0 px-3.5 py-2.5 border-t border-[oklch(0.24_0.02_264)]">
        <button
          type="button"
          onClick={onOpenSettings}
          className="w-full flex items-center gap-2 rounded-lg px-2.5 py-2 text-sm text-[oklch(0.6_0.01_264)] hover:bg-[oklch(0.19_0.02_264)] hover:text-[oklch(0.85_0.01_264)] transition-colors"
        >
          <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" className="w-4 h-4 shrink-0" aria-hidden="true">
            <circle cx="12" cy="12" r="3" />
            <path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 1 1-2.83 2.83l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 0 1-4 0v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 1 1-2.83-2.83l.06-.06a1.65 1.65 0 0 0 .33-1.82 1.65 1.65 0 0 0-1.51-1H3a2 2 0 0 1 0-4h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 1 1 2.83-2.83l.06.06a1.65 1.65 0 0 0 1.82.33H9a1.65 1.65 0 0 0 1-1.51V3a2 2 0 0 1 4 0v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 1 1 2.83 2.83l-.06.06a1.65 1.65 0 0 0-.33 1.82V9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 0 1 0 4h-.09a1.65 1.65 0 0 0-1.51 1z" />
          </svg>
          設定
        </button>
      </div>
    </div>
  );

  // 收合狀態：專注模式用的迷你 icon rail — 只留展開鍵、新增鍵、和「folder icon + 該資料夾底下各 session 的 runner icon」，
  // 點 session icon 直接切換（維持收合），不用先展開才能換 session。
  if (collapsed) {
    return (
      <div className="flex flex-col h-app min-w-0 overflow-hidden items-center">
        <div
          className="pt-[18px] pb-2.5 shrink-0 flex flex-col items-center gap-2"
          style={{ paddingTop: 'calc(18px + env(safe-area-inset-top) + var(--tg-content-safe-top))' }}
        >
          <button
            type="button"
            onClick={onToggleSidebar}
            aria-label="展開會話列表"
            title="展開會話列表"
            className="flex h-9 w-9 items-center justify-center rounded-[9px] border border-[oklch(0.28_0.02_264)] text-gray-400 hover:text-gray-200 hover:bg-[oklch(0.19_0.02_264)] transition-colors focus:outline-none"
          >
            <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 20 20" fill="currentColor" className="w-5 h-5 rotate-180" aria-hidden>
              <path fillRule="evenodd" d="M12.79 5.23a.75.75 0 01.02 1.06L8.832 10l3.938 3.71a.75.75 0 11-1.04 1.08l-4.5-4.25a.75.75 0 010-1.08l4.5-4.25a.75.75 0 011.06.02z" clipRule="evenodd" />
            </svg>
          </button>
          {onToggleSplit && (
            <SplitViewButton isSplit={isSplit} onClick={onToggleSplit} />
          )}
        </div>
        <button
          type="button"
          onClick={() => onCreateNew?.()}
          aria-label="建立新 Session"
          title="新 Session"
          className="mb-2 flex h-9 w-9 shrink-0 items-center justify-center rounded-[9px] bg-[oklch(0.62_0.19_275)] text-white text-lg leading-none transition hover:brightness-110 active:scale-95 focus:outline-none"
        >
          +
        </button>
        <div className="flex-1 w-full overflow-y-auto app-scroll flex flex-col items-center gap-1.5 pb-4">
          {(groupedSessions || []).map(([dirKey, dirSessions]) => (
            <React.Fragment key={dirKey}>
              <span
                className="mt-1.5 flex h-7 w-7 shrink-0 items-center justify-center rounded-md text-[13px] text-[oklch(0.5_0.01_264)]"
                title={workDirGroupShortLabel(dirKey) + '（' + dirKey + '）'}
                aria-hidden
              >
                📁
              </span>
              {dirSessions.map((s) => {
                const active = activeSessionId != null && s.id === activeSessionId;
                const unread = !active && isUnread(s);
                const displayAgent = sessionDisplayAgent(s);
                const agentLabel = AGENT_LABEL[displayAgent] || displayAgent || 'claude';
                const unreadCountNum = typeof s.unread_count === 'number' ? s.unread_count : 1;
                const unreadBadgeText = unreadCountNum > 99 ? '99+' : String(unreadCountNum > 0 ? unreadCountNum : 1);
                return (
                  <button
                    key={s.id}
                    type="button"
                    draggable={isWideScreen ? true : undefined}
                    onDragStart={(e) => {
                      if (!isWideScreen) {
                        e.preventDefault();
                        return;
                      }
                      const payload = JSON.stringify({ sessionId: s.id, fromGroup: null });
                      e.dataTransfer.setData(typeof MINIAPP_SESSION_MIME !== 'undefined' ? MINIAPP_SESSION_MIME : 'application/x-miniapp-session', payload);
                      e.dataTransfer.effectAllowed = 'move';
                      if (typeof window !== 'undefined') {
                        window.__miniapp_drag_source = { sessionId: s.id, fromGroup: null };
                      }
                    }}
                    onDragEnd={() => {
                      if (typeof window !== 'undefined') {
                        window.__miniapp_drag_source = null;
                      }
                    }}
                    onClick={() => handleEnter(s)}
                    aria-label={s.name || agentLabel}
                    title={(s.name || '未命名') + ' · ' + agentLabel}
                    className={
                      'relative flex h-9 w-9 shrink-0 items-center justify-center rounded-[8px] transition-colors ' +
                      (active ? 'ring-2 ring-violet-500 ' : 'hover:brightness-110 ') +
                      getAgentBadgeClass(displayAgent, s)
                    }
                  >
                    <AgentBadgeIcon agentType={displayAgent} session={s} />
                    {unread && (
                      <span
                        className="absolute -top-1 -right-1 min-w-[16px] h-4 px-1 rounded-full bg-violet-600 text-white text-[10px] font-bold flex items-center justify-center leading-none pointer-events-none ring-2 ring-[oklch(0.15_0.02_264)]"
                        aria-label={`未讀訊息 ${unreadBadgeText} 則`}
                      >
                        {unreadBadgeText}
                      </span>
                    )}
                  </button>
                );
              })}
            </React.Fragment>
          ))}
        </div>
      </div>
    );
  }

  return renderFullPanel();
}
