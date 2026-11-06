// ── Session 列表畫面 ──────────────────────────────────────────────────────────
function SessionView({ onEnter, onSessionsLoaded, onSortedSessionsChange, activeSessionId = null, onCreateNew, onOpenSettings, onToggleSidebar, collapsed = false, isSplit = false, onToggleSplit = null, isWideScreen = true, onOpenOtherPane, canOpenOtherPane }) {
  const [sessionMenu, setSessionMenu] = useState(null);
  const bindSessionMenu = useLongPress(setSessionMenu);
  const closeSessionMenu = useCallback(() => setSessionMenu(null), []);
  useEffect(() => setSessionMenu(null), [activeSessionId, collapsed]);
  const [sessions, setSessions]   = useState([]);
  const [sessionSearch, setSessionSearch] = useState('');
  const [sessionSort, setSessionSort]     = useState('last_active_desc');
  const [groupByDir] = useState(true); // RemoteAgent：固定依工作目錄分組
  const [collapsedDirs, setCollapsedDirs] = useState(() => new Set());
  const [renamingId, setRenamingId] = useState(null);
  const [renameVal, setRenameVal]   = useState('');
  const renameInputRef = useRef(null);
  const renameSavingRef = useRef(false);
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

  const [listLoading, setListLoading] = useState(true);
  const [listLoaded, setListLoaded] = useState(false);
  const [listError, setListError] = useState('');
  const loadControllerRef = useRef(null);
  const loadGenerationRef = useRef(0);
  const mountedRef = useRef(true);
  const onLoadedRef = useRef(onSessionsLoaded);
  onLoadedRef.current = onSessionsLoaded;
  const load = async () => {
    if (!mountedRef.current) return;
    const generation = ++loadGenerationRef.current;
    loadControllerRef.current?.abort();
    const controller = new AbortController();
    loadControllerRef.current = controller;
    setListLoading(true);
    try {
      const res = await apiFetch('/sessions', { signal: controller.signal });
      if (!res.ok) throw new Error('會話列表載入失敗');
      const data = await res.json();
      if (!Array.isArray(data)) throw new Error('無法讀取會話列表');
      if (controller.signal.aborted || generation !== loadGenerationRef.current || !mountedRef.current) return;
      const sorted = sortSessionsByWorkDirThenName(data).filter(s => !pendingDelRef.current.has(s.id));
      notifyStatusChanges(sorted);
      setSessions(sorted);
      setListLoaded(true);
      setListError('');
      onLoadedRef.current?.(sorted);
    } catch (err) {
      if (!controller.signal.aborted && generation === loadGenerationRef.current && mountedRef.current) {
        setListError(err.message || '會話列表載入失敗');
      }
    } finally {
      if (generation === loadGenerationRef.current && mountedRef.current) setListLoading(false);
    }
  };

  useEffect(() => {
    mountedRef.current = true;
    load();
    return () => {
      mountedRef.current = false;
      ++loadGenerationRef.current;
      loadControllerRef.current?.abort();
    };
  }, []);

  // 即時更新走 /events WS；輪詢只當斷線時的保底，回到分頁時也立即補抓一次。
  useEffect(() => {
    let ws = null;
    let retry = null;
    let debounce = null;
    let closed = false;
    const connect = () => {
      ws = new WebSocket(eventsWsURL());
      // 斷線期間可能漏掉 sessions_changed，連上就補抓一次。
      ws.onopen = () => load();
      ws.onmessage = (e) => {
        try {
          if (JSON.parse(e.data).type !== 'sessions_changed') return;
        } catch (_) { return; }
        clearTimeout(debounce);
        debounce = setTimeout(load, 150);
      };
      ws.onclose = () => { if (!closed) retry = setTimeout(connect, 1000); };
    };
    connect();
    const onVisible = () => { if (!document.hidden) load(); };
    document.addEventListener('visibilitychange', onVisible);
    window.addEventListener('miniapp:sessions-refresh', load);
    const poll = setInterval(load, 30000);
    return () => {
      closed = true;
      clearTimeout(retry);
      clearTimeout(debounce);
      clearInterval(poll);
      document.removeEventListener('visibilitychange', onVisible);
      window.removeEventListener('miniapp:sessions-refresh', load);
      if (ws) ws.close();
    };
  }, []);

  useEffect(() => {
    if (renamingId) renameInputRef.current?.focus();
  }, [renamingId]);

  const del = (id, e) => {
    e?.stopPropagation();
    const target = sessions.find((x) => x.id === id);
    pendingDelRef.current.add(id);
    setSessions((prev) => prev.filter((x) => x.id !== id));
    const timer = setTimeout(async () => {
      try {
        const res = await apiFetch(`/sessions/${id}`, { method: 'DELETE' });
        if (!res.ok) throw new Error('刪除失敗');
        try { await writeComposerFiles(draftInputStorageKey(id), []); }
        catch (_) { showToast('會話已刪除，但本機附件草稿未能清除', { error: true, duration: 5000 }); }
      } catch (_) {
        setSessions(prev => prev.some(s => s.id === id) || !target ? prev : [...prev, target]);
        showToast('無法確認刪除結果，請確認列表後再試', { error: true, duration: 5000 });
      } finally {
        pendingDelRef.current.delete(id);
        load();
      }
    }, 5000);
    showToast(`將刪除「${(target && target.name) || '未命名'}」，5 秒內可復原`, {
      duration: 5000,
      action: {
        label: '復原',
        onClick: () => { clearTimeout(timer); pendingDelRef.current.delete(id); load(); },
      },
    });
  };

  const startRename = (s, e) => {
    e?.stopPropagation();
    setRenamingId(s.id);
    setRenameVal(s.name || '');
  };

  const commitRename = async (id) => {
    if (renameSavingRef.current) return;
    const trimmed = renameVal.trim();
    if (!trimmed) return;
    renameSavingRef.current = true;
    try {
      const res = await apiFetch(`/sessions/${id}`, {
        method: 'PATCH',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ name: trimmed }),
      });
      if (!res.ok) {
        const body = await res.json().catch(() => ({}));
        throw new Error(body.error || '重新命名失敗');
      }
      setRenamingId(null);
      setRenameVal('');
      load();
    } catch (err) {
      showToast(err.message || '重新命名失敗，請稍後再試', { error: true, duration: 5000 });
      renameInputRef.current?.focus();
    } finally {
      renameSavingRef.current = false;
    }
  };

  // Optimistic：先改畫面，失敗再還原並提示。
  const togglePin = async (s, e) => {
    e?.stopPropagation();
    const next = !s.pinned;
    setSessions((prev) => prev.map((row) => (row.id === s.id ? { ...row, pinned: next } : row)));
    try {
      const res = await apiFetch(`/sessions/${s.id}`, {
        method: 'PATCH',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ pinned: next }),
      });
      if (!res.ok) {
        const body = await res.json().catch(() => ({}));
        throw new Error(body.error || (next ? '釘選失敗' : '取消釘選失敗'));
      }
    } catch (err) {
      setSessions((prev) => prev.map((row) => (row.id === s.id ? { ...row, pinned: !next } : row)));
      showToast(err.message || '操作失敗，請稍後再試', { error: true, duration: 5000 });
    }
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

  // 釘選的 session 抽出成虛擬群組，固定排最上面
  const PIN_GROUP = '釘選';

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
      if (s.pinned) key = PIN_GROUP;
      if (!map.has(key)) map.set(key, []);
      map.get(key).push(s);
    }
    return [...map.entries()].sort((a, b) => {
      const ka = String(a[0]);
      const kb = String(b[0]);
      const tgGroup = 'Telegram 聊天室';
      if (ka === PIN_GROUP && kb !== PIN_GROUP) return -1;
      if (ka !== PIN_GROUP && kb === PIN_GROUP) return 1;
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
    if (isWideScreen) revealSessionTab(s.id);
  };

  const listStatus = (
    <div aria-live="polite" aria-busy={listLoading} className="px-2 py-2 text-xs text-ra-t4">
      {listLoading && <div role="status">{listLoaded ? '更新中…' : '載入會話中…'}</div>}
      {listError && <div role="alert">
        <span>{listError}{listLoaded ? '，目前顯示上次載入的內容。' : ''}</span>
        <button type="button" onClick={load} disabled={listLoading} className="mt-1 block rounded border border-ra-line px-2 py-1 disabled:opacity-40">重試</button>
      </div>}
    </div>
  );

  const renderSessionRow = (s) => {
    const menuBindings = bindSessionMenu({ sessionId: s.id });
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
        {...menuBindings}
        tabIndex={renamingId === s.id ? -1 : 0}
        aria-label={s.name || '未命名會話'}
        onKeyDown={event => {
          menuBindings.onKeyDown(event);
          if (!event.defaultPrevented && event.target === event.currentTarget && (event.key === 'Enter' || event.key === ' ')) {
            event.preventDefault(); handleEnter(s);
          }
        }}
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
              className="w-full bg-ra-card border border-violet-500 rounded-lg px-2 py-1 text-sm text-ra-t1 focus:outline-none"
            />
          ) : (
            <div className="flex items-center gap-2 min-w-0" title={rowTitle}>
              <span className={`inline-flex shrink-0 items-center justify-center w-[22px] h-[22px] rounded-[6px] ${getAgentBadgeClass(displayAgent, s)}`} title={agentLabel}>
                <AgentBadgeIcon agentType={displayAgent} session={s} />
              </span>
              <span
                className={
                  'flex-1 min-w-0 text-[13.5px] truncate leading-snug ' +
                  (unread ? 'font-semibold text-ra-t1' : 'font-medium text-ra-t3')
                }
              >
                {s.name || '未命名'}
              </span>
              {s.pinned && <span className="shrink-0 text-[10px] text-violet-400" title="已釘選">釘選</span>}
              <button type="button" aria-label={`更多操作：${s.name || '未命名'}`} aria-haspopup="menu"
                className="shrink-0 min-w-[32px] min-h-[32px] rounded-lg text-ra-t4 hover:bg-ra-raised hover:text-ra-t1 focus-visible:ring-2 focus-visible:ring-violet-400"
                onClick={event => { event.stopPropagation(); setSessionMenu(openContextPayload(event, { sessionId: s.id }, !isWideScreen)); }}>⋯</button>
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

  const menuSession = sessions.find(s => s.id === sessionMenu?.sessionId);
  const menuBusy = menuSession && (['running', 'awaiting_confirm', 'awaiting_input'].includes(menuSession.status) || menuSession.lingering);
  const menuItems = menuSession ? [
    { label: '開啟會話', onSelect: () => handleEnter(menuSession) },
    ...(isWideScreen && onOpenOtherPane ? [{ label: '在另一個窗格開啟', disabled: !canOpenOtherPane?.(menuSession),
      reason: '需要另一個已開啟的會話才能分割', onSelect: () => onOpenOtherPane(menuSession) }] : []),
    { separator: true },
    { label: menuSession.pinned ? '取消釘選' : '釘選', onSelect: () => togglePin(menuSession) },
    { label: '重新命名…', onSelect: () => { if (collapsed) onToggleSidebar?.(); startRename(menuSession); } },
    ...(menuSession.work_dir ? [{ label: '複製工作目錄', onSelect: async () => {
      if (await copyText(menuSession.work_dir)) showToast('已複製工作目錄');
      else showToast('複製失敗', { error: true });
    } }] : []),
    { separator: true },
    { label: '刪除會話…', danger: true, disabled: !!menuBusy, reason: '請先停止任務，再刪除會話',
      onSelect: () => del(menuSession.id) },
  ] : [];
  const menuElement = <ContextMenu menu={menuSession ? sessionMenu : null} items={menuItems} onClose={closeSessionMenu} label="會話操作" />;

  const renderFullPanel = () => (
    <div className="flex flex-col h-app min-w-0 overflow-hidden">
      {menuElement}
      {/* Header — 設計 1a / 2a */}
      <div className="flex items-center justify-between px-5 pt-[18px] pb-3 shrink-0"
           style={{ paddingTop: 'calc(18px + env(safe-area-inset-top) + var(--tg-content-safe-top))' }}>
        <div className="flex items-center gap-2 min-w-0">
          <div className="w-6 h-6 rounded-[6px] bg-gradient-to-br from-[oklch(0.62_0.19_275)] to-[oklch(0.6_0.17_300)] flex items-center justify-center shrink-0" aria-hidden>
            <div className="w-[9px] h-[9px] rounded-[2px] bg-white" />
          </div>
          <span className="ra-display text-base text-ra-t1 tracking-tight">Agent Console</span>
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
              className="flex h-[30px] w-[30px] shrink-0 items-center justify-center rounded-[9px] border border-ra-line text-gray-400 hover:text-gray-200 hover:bg-ra-card transition-colors focus:outline-none"
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
          <div className="h-9 flex-1 min-w-0 flex items-center gap-1.5 bg-ra-card border border-ra-line rounded-[9px] px-2.5">
            <span className="text-ra-t5 text-sm shrink-0" aria-hidden>⌕</span>
            <input
              type="search"
              data-session-navigation
              aria-label="搜尋會話"
              value={sessionSearch}
              onChange={(e) => setSessionSearch(e.target.value)}
              placeholder="搜尋 session…"
              autoComplete="off"
              className="flex-1 min-w-0 bg-transparent border-0 p-0 text-[13px] text-ra-t1 placeholder-ra-t5 focus:outline-none"
            />
          </div>
        )}
        <button
          type="button"
          data-session-navigation
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
              className="shrink-0 bg-ra-card border border-ra-line rounded-[9px] px-2.5 py-2 text-xs text-ra-t4 focus:outline-none cursor-pointer max-w-[6.5rem]"
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
              className="flex h-9 w-9 shrink-0 items-center justify-center rounded-[9px] border border-ra-line bg-ra-card text-ra-t4 hover:text-violet-300 hover:border-violet-500/50 transition-colors focus:outline-none"
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
        {(listLoading || listError) && listStatus}
        {!listLoaded ? null : sessions.length === 0 ? (
          <div className="text-center text-ra-t5 mt-24 text-sm px-4">尚無 Session，點 + 建立</div>
        ) : displaySessions.length === 0 ? (
          <div className="text-center text-gray-600 mt-24 text-sm px-4">沒有符合搜尋條件的 Session</div>
        ) : groupByDir && groupedSessions ? (
          groupedSessions.map(([dirKey, dirSessions]) => {
            const expanded = sessionSearchActive || !collapsedDirs.has(dirKey);
            const shortLabel = workDirGroupShortLabel(dirKey);
            // 同一 work_dir 即同一 git working tree，分支必然相同，取第一筆即可
            const isPinGroup = dirKey === PIN_GROUP; // 虛擬群組：混合多個目錄，無單一分支、不能在此建立 session
            const dirBranch = isPinGroup ? '' : (dirSessions.find((s) => s.git_branch) || {}).git_branch;
            return (
              <div key={dirKey} className="space-y-0">
                <div className="group/dir flex w-full min-w-0 items-center gap-1 px-2 py-1.5 mt-2 transition-colors hover:bg-violet-500/[0.06]">
                  <button
                    type="button"
                    onClick={() => toggleDirCollapsed(dirKey)}
                    className="flex min-w-0 flex-1 items-center gap-1.5 rounded-md px-1 py-1 text-left focus:outline-none focus-visible:ring-2 focus-visible:ring-violet-500/50 focus-visible:ring-offset-1 focus-visible:ring-offset-ra-sunken"
                  >
                    <span className="shrink-0 text-ra-t5 w-3 text-center text-[10px]" aria-hidden>
                      {expanded ? '▾' : '▸'}
                    </span>
                    <span className="shrink-0 text-[13px] leading-none" aria-hidden>{isPinGroup ? '📌' : '📁'}</span>
                    <span
                      className="min-w-0 flex-1 truncate text-[12.5px] text-ra-t3 font-bold"
                      title={dirKey + (expanded ? '' : `（${dirSessions.length}）`)}
                    >
                      {shortLabel}
                    </span>
                  </button>
                  {dirBranch ? (
                    <span
                      className="ra-mono shrink-0 max-w-[8rem] truncate text-[11px] text-ra-t5"
                      title={dirBranch}
                    >
                      {dirBranch}
                    </span>
                  ) : null}
                  {!isPinGroup && (
                    <button
                      type="button"
                      onClick={() => openNewSessionForDir(dirKey)}
                      aria-label="在此目錄建立新 Session"
                      title="在此目錄建立新 Session"
                      className="shrink-0 rounded-md px-1.5 py-1 text-[13px] leading-none font-semibold text-violet-400 hover:bg-violet-500/10 hover:text-violet-300 focus:outline-none focus-visible:ring-2 focus-visible:ring-violet-500/50 sm:opacity-0 sm:group-hover/dir:opacity-100 transition-opacity"
                    >
                      ＋
                    </button>
                  )}
                </div>
                {expanded && (
                  <div className="ml-[18px] border-l border-ra-line-soft pl-2.5">
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
      <div className="shrink-0 px-3.5 py-2.5 border-t border-ra-line-soft">
        <button
          type="button"
          onClick={onOpenSettings}
          className="w-full flex items-center gap-2 rounded-lg px-2.5 py-2 text-sm text-ra-t4 hover:bg-ra-card hover:text-ra-t2 transition-colors"
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
        {menuElement}
        <div
          className="pt-[18px] pb-2.5 shrink-0 flex flex-col items-center gap-2"
          style={{ paddingTop: 'calc(18px + env(safe-area-inset-top) + var(--tg-content-safe-top))' }}
        >
          <button
            type="button"
            onClick={onToggleSidebar}
            aria-label="展開會話列表"
            data-session-navigation
            title="展開會話列表"
            className="flex h-9 w-9 items-center justify-center rounded-[9px] border border-ra-line text-gray-400 hover:text-gray-200 hover:bg-ra-card transition-colors focus:outline-none"
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
          {(listLoading || listError) && listStatus}
          {(groupedSessions || []).map(([dirKey, dirSessions]) => (
            <React.Fragment key={dirKey}>
              <span
                className="mt-1.5 flex h-7 w-7 shrink-0 items-center justify-center rounded-md text-[13px] text-ra-t5"
                title={workDirGroupShortLabel(dirKey) + '（' + dirKey + '）'}
                aria-hidden
              >
                {dirKey === PIN_GROUP ? '📌' : '📁'}
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
                    {...bindSessionMenu({ sessionId: s.id })}
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
                        className="absolute -top-1 -right-1 min-w-[16px] h-4 px-1 rounded-full bg-violet-600 text-white text-[10px] font-bold flex items-center justify-center leading-none pointer-events-none ring-2 ring-ra-bg"
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
