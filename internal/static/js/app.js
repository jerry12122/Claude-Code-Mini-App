// ── 密碼輸入畫面（非 TMA 環境）────────────────────────────────────────────────
function PasswordView({ onSuccess }) {
  const [pwd, setPwd]     = useState('');
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(false);

  const submit = async () => {
    if (!pwd.trim()) return;
    setLoading(true);
    setError('');
    try {
      const res = await fetchWithTimeout(resolveApiUrl('/auth/login'), {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ password: pwd }),
      });
      if (res.ok) {
        const data = await res.json().catch(() => ({}));
        if (data.token) {
          try {
            localStorage.setItem(WEB_SESSION_STORAGE_KEY, data.token);
          } catch (_) {}
        }
        onSuccess();
      } else {
        const data = await res.json().catch(() => ({}));
        setError(data.error || '密碼錯誤，請重試');
      }
    } catch (_) {
      setError('連線失敗，請重試');
    } finally {
      setLoading(false);
    }
  };

  const handleKey = (e) => {
    if (e.key === 'Enter') submit();
  };

  return (
    <div className="flex items-center justify-center h-app">
      <div className="w-72 space-y-4">
        <div className="text-center">
          <div className="flex flex-col items-center gap-2 mb-1">
            <div className="w-10 h-10 rounded-[10px] bg-gradient-to-br from-violet-500 to-fuchsia-600 flex items-center justify-center" aria-hidden>
              <div className="w-3.5 h-3.5 rounded-[3px] bg-white" />
            </div>
            <div className="ra-display text-2xl" style={{color:'oklch(var(--ra-t1))'}}>Agent Console</div>
          </div>
          <div className="text-gray-500 text-sm">Claude Code · Remote Agent</div>
          <div className="text-gray-600 text-xs">請輸入存取密碼</div>
        </div>
        <input
          type="password"
          value={pwd}
          onChange={e => setPwd(e.target.value)}
          onKeyDown={handleKey}
          placeholder="密碼"
          autoFocus
          className="w-full bg-gray-800 border border-gray-700 rounded-lg px-4 py-2.5 text-sm text-gray-200 placeholder-gray-500 focus:outline-none focus:border-violet-600"
        />
        {error && <div className="text-red-400 text-xs text-center">{error}</div>}
        <button onClick={submit} disabled={loading || !pwd.trim()}
          className="w-full py-2.5 bg-violet-600 hover:bg-violet-500 disabled:opacity-40 text-white rounded-lg text-sm font-medium">
          {loading ? '驗證中…' : '進入'}
        </button>
      </div>
    </div>
  );
}

// ── Debug Banner（開發用，上線前移除）────────────────────────────────────────
const DEBUG_BANNER = false;
function DebugBanner() {
  if (!DEBUG_BANNER) return null;
  return (
    <div style={{position:'fixed',top:0,left:0,right:0,background:'#1a1a00',color:'#ffff00',fontSize:'10px',padding:'4px 8px',zIndex:9999,wordBreak:'break-all'}}>
      isTMA={String(isTMA)} | TG={String(isTelegram)} | initData={initData ? initData.slice(0,40)+'…' : '(empty)'}
    </div>
  );
}

// ── 分割視窗偏好持久化（新鍵 miniapp_tab_groups_v1，支援舊鍵遷移）──────────────
const SPLIT_STATE_STORAGE_KEY = 'miniapp_split_chat_state';

// ── Root ──────────────────────────────────────────────────────────────────────
function App() {
  const [isWideScreen, setIsWideScreen] = useState(() =>
    typeof window !== 'undefined' && window.matchMedia ? window.matchMedia('(min-width: 1024px)').matches : true
  );
  const [tabGroups, dispatchTabGroups] = React.useReducer(tabGroupsReducer, undefined, () => readStoredTabGroupsState());
  const [knownSessionsMap, setKnownSessionsMap] = useState({});
  const [sidebarSortedSessions, setSidebarSortedSessions] = useState([]);
  const [mobileActivePane, setMobileActivePane] = useState('primary');
  const [creating, setCreating] = useState(false);
  const [composerPrefill, setComposerPrefill] = useState(null);
  const [authed, setAuthed]   = useState(false);
  const [settingsOpen, setSettingsOpen] = useState(false);
  const lastEscTimeRef = useRef(0);

  // 桌面可更新；瀏覽器與 Telegram 只讀版本並通知。
  const desktopUpdate = useDesktopUpdate({ enabled: authed, onOpen: () => setSettingsOpen(true) });

  // 衍生值維持舊介面
  const isSplit = tabGroups.groups.length > 1;
  const splitRatio = tabGroups.ratio;
  const splitOrientation = tabGroups.orientation;
  const activePane = tabGroups.focused === 1 ? 'secondary' : 'primary';

  const primarySessionId = tabGroups.groups[0]?.active || null;
  const secondarySessionId = tabGroups.groups[1]?.active || null;

  const session = useMemo(() => {
    if (!primarySessionId) return null;
    if (knownSessionsMap[primarySessionId]) return knownSessionsMap[primarySessionId];
    if (Array.isArray(sidebarSortedSessions)) {
      const match = sidebarSortedSessions.find((s) => s.id === primarySessionId);
      if (match) return match;
    }
    return null;
  }, [primarySessionId, knownSessionsMap, sidebarSortedSessions]);

  const secondarySession = useMemo(() => {
    if (!secondarySessionId) return null;
    if (knownSessionsMap[secondarySessionId]) return knownSessionsMap[secondarySessionId];
    if (Array.isArray(sidebarSortedSessions)) {
      const match = sidebarSortedSessions.find((s) => s.id === secondarySessionId);
      if (match) return match;
    }
    return null;
  }, [secondarySessionId, knownSessionsMap, sidebarSortedSessions]);

  // 外觀設定：先套本機快取避免 FOUC；登入後再 GET 伺服器 hydrate（必要時種子遷移）。
  useEffect(() => {
    applyAppearance(readStoredAppearance());
  }, []);
  useEffect(() => {
    if (!authed) return;
    hydrateAppearanceFromServer();
  }, [authed]);

  // 持久化儲存分頁群組狀態（寫入新鍵 miniapp_tab_groups_v1，舊鍵不覆寫）
  useEffect(() => {
    try {
      localStorage.setItem(TAB_GROUPS_STORAGE_KEY, serializeTabGroupsState(tabGroups));
    } catch (_) {}
  }, [tabGroups]);

  const openComposer = useCallback((prefill) => {
    setComposerPrefill(prefill || {});
    setCreating(true);
  }, []);
  const closeComposer = useCallback(() => setCreating(false), []);

  const handleCreated = useCallback((s) => {
    setCreating(false);
    if (!s || !s.id) return;
    setKnownSessionsMap((prev) => ({ ...prev, [s.id]: s }));
    const targetGroup = isSplit && activePane === 'secondary' ? 1 : 0;
    dispatchTabGroups({ type: 'open', sessionId: s.id, groupIndex: targetGroup });
  }, [isSplit, activePane]);

  const selectSession = useCallback((s, targetPane = null) => {
    setCreating(false);
    if (!s) {
      if (!isWideScreen) {
        dispatchTabGroups({ type: 'clear' });
      } else {
        const currActive = tabGroups.groups[0]?.active;
        if (currActive) {
          dispatchTabGroups({ type: 'close', sessionId: currActive, groupIndex: 0 });
        }
      }
      return;
    }

    if (s.id) {
      setKnownSessionsMap((prev) => ({ ...prev, [s.id]: s }));
    }

    if (targetPane === 'secondary') {
      if (isSplit) {
        dispatchTabGroups({ type: 'open', sessionId: s.id, groupIndex: 1 });
      } else {
        if (tabGroups.groups[0]?.tabs.length > 0 && !tabGroups.groups[0].tabs.includes(s.id)) {
          dispatchTabGroups({ type: 'toggleSplit', secondSessionId: s.id });
        } else {
          dispatchTabGroups({ type: 'open', sessionId: s.id, groupIndex: 0 });
        }
      }
      setMobileActivePane('secondary');
    } else if (targetPane === 'primary') {
      dispatchTabGroups({ type: 'open', sessionId: s.id, groupIndex: 0 });
      setMobileActivePane('primary');
    } else {
      const targetGroup = tabGroups.focused === 1 && isSplit ? 1 : 0;
      dispatchTabGroups({ type: 'open', sessionId: s.id, groupIndex: targetGroup });
      setMobileActivePane(targetGroup === 1 ? 'secondary' : 'primary');
    }
  }, [isSplit, isWideScreen, tabGroups]);

  const canOpenOtherPane = s => tabGroups.groups.length > 1 || tabGroups.groups[0].tabs.length > 1 ||
    (tabGroups.groups[0].tabs.length > 0 && !tabGroups.groups[0].tabs.includes(s.id));
  const openInOtherPane = (s, sourceGroup = tabGroups.focused) => {
    if (!s?.id || !canOpenOtherPane(s)) return;
    setKnownSessionsMap(previous => ({ ...previous, [s.id]: { ...previous[s.id], ...s } }));
    if (tabGroups.groups.length === 1) {
      dispatchTabGroups({ type: 'splitDrop', sessionId: s.id, edge: 'right' });
      return;
    }
    const target = sourceGroup === 0 ? 1 : 0;
    const origin = tabGroups.groups.findIndex(group => group.tabs.includes(s.id));
    dispatchTabGroups(origin >= 0 && origin !== target
      ? { type: 'move', sessionId: s.id, toGroup: target }
      : { type: 'open', sessionId: s.id, groupIndex: target });
  };

  const toggleSplit = useCallback(() => {
    if (tabGroups.groups.length > 1) {
      dispatchTabGroups({ type: 'toggleSplit' });
    } else {
      const group0 = tabGroups.groups[0];
      if (group0 && group0.tabs.length >= 2) {
        dispatchTabGroups({ type: 'toggleSplit' });
      } else {
        const currentId = group0?.active || group0?.tabs[0];
        const other = Array.isArray(sidebarSortedSessions)
          ? sidebarSortedSessions.find((s) => s.id !== currentId)
          : null;
        if (other) {
          setKnownSessionsMap((prev) => ({ ...prev, [other.id]: other }));
          dispatchTabGroups({ type: 'toggleSplit', secondSessionId: other.id });
        }
      }
    }
  }, [tabGroups.groups, sidebarSortedSessions]);

  const toggleSplitOrientation = useCallback(() => {
    const next = tabGroups.orientation === 'vertical' ? 'horizontal' : 'vertical';
    dispatchTabGroups({ type: 'setOrientation', orientation: next });
  }, [tabGroups.orientation]);

  const closeSecondaryPane = useCallback(() => {
    if (tabGroups.groups.length > 1) {
      dispatchTabGroups({ type: 'toggleSplit' });
    }
    setMobileActivePane('primary');
  }, [tabGroups.groups.length]);

  const setSplitRatio = useCallback((r) => {
    dispatchTabGroups({ type: 'setRatio', ratio: r });
  }, []);

  const setActivePane = useCallback((pane) => {
    dispatchTabGroups({ type: 'focus', groupIndex: pane === 'secondary' ? 1 : 0 });
  }, []);

  // 放置邏輯在 tabGroups.js 的 resolveDropAction（純函式、有測試）；這裡只過濾未知會話
  const handleDropOnTabBar = useCallback((sessionId, fromGroup, targetGroupIdx, insertIndex) => {
    if (!sidebarSortedSessions.some((s) => s.id === sessionId)) return;
    const action = resolveDropAction(tabGroups, sessionId, { kind: 'tabbar', group: targetGroupIdx, index: insertIndex });
    if (action) dispatchTabGroups(action);
  }, [sidebarSortedSessions, tabGroups]);

  const handleDropOnContent = useCallback((sessionId, fromGroup, region, targetGroupIdx) => {
    if (!sidebarSortedSessions.some((s) => s.id === sessionId)) return;
    const action = resolveDropAction(tabGroups, sessionId, { kind: 'content', group: targetGroupIdx, region });
    if (action) dispatchTabGroups(action);
  }, [sidebarSortedSessions, tabGroups]);

  // 鍵盤快捷鍵：Ctrl+\ 切換分割；Ctrl+Alt+O 切換方向；Ctrl+Alt+Left/Right 切換焦點
  useEffect(() => {
    const onKey = (e) => {
      if (e.isComposing || e.keyCode === 229) return;
      const act = resolveTabShortcut(e);
      if (act && act.type === 'toggleSplit') {
        e.preventDefault();
        // 寬螢幕：單一分頁時不動作（spec）；窄螢幕沿用舊行為（可自動開另一個會話）
        if (isWideScreen) dispatchTabGroups(act);
        else toggleSplit();
      } else if (act && act.type === 'focus') {
        e.preventDefault();
        ReactDOM.flushSync(() => {
          dispatchTabGroups(act);
          setMobileActivePane(act.groupIndex === 1 ? 'secondary' : 'primary');
        });
        const pane = document.querySelector(`[data-chat-pane="${act.groupIndex === 1 ? 'secondary' : 'primary'}"][data-chat-focused="true"]`);
        (pane?.querySelector('textarea[data-chat-input]:not(:disabled)') || pane)?.focus({ preventScroll: true });
      } else if ((e.ctrlKey || e.metaKey) && e.altKey && (e.key === 'o' || e.key === 'O' || e.code === 'KeyO')) {
        e.preventDefault();
        toggleSplitOrientation();
      } else if (e.altKey && (e.key === 'b' || e.key === 'B' || e.code === 'KeyB')) {
        e.preventDefault();
        const activeGroupIdx = tabGroups.focused === 1 ? 1 : 0;
        const activeId = tabGroups.groups[activeGroupIdx]?.active || tabGroups.groups[0]?.active;
        if (activeId) {
          dispatchTabGroups({ type: 'close', sessionId: activeId, groupIndex: activeGroupIdx });
        }
      } else if (e.key === 'Escape') {
        const now = Date.now();
        if (now - lastEscTimeRef.current < 450) {
          e.preventDefault();
          const activeGroupIdx = tabGroups.focused === 1 ? 1 : 0;
          const activeId = tabGroups.groups[activeGroupIdx]?.active || tabGroups.groups[0]?.active;
          if (activeId) {
            dispatchTabGroups({ type: 'close', sessionId: activeId, groupIndex: activeGroupIdx });
          }
          lastEscTimeRef.current = 0;
        } else {
          lastEscTimeRef.current = now;
        }
      }
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [isWideScreen, toggleSplit, toggleSplitOrientation]);
  // ponytail: 接手提示只存本頁記憶體；取消表單不丟失，新一輪清除。需要離線恢復時再持久化。
  const [handoffBySession, setHandoffBySession] = useState({});
  const handleHandoffChange = useCallback((sessionId, value) => {
    setHandoffBySession((prev) => {
      if (value == null) {
        if (!(sessionId in prev)) return prev;
        const next = { ...prev };
        delete next[sessionId];
        return next;
      }
      return { ...prev, [sessionId]: value };
    });
  }, []);
  // 非 TMA 環境：mount 時試探 cookie 是否仍有效
  const [checking, setChecking] = useState(true);

  const [sidebarWidthPx, setSidebarWidthPx] = useState(() =>
    typeof window !== 'undefined' ? clampSidebarWidthPx(readStoredSidebarWidthPx()) : SIDEBAR_WIDTH_DEFAULT
  );
  const lastSidebarWidthRef = useRef(sidebarWidthPx);

  useEffect(() => {
    lastSidebarWidthRef.current = sidebarWidthPx;
  }, [sidebarWidthPx]);

  const [sidebarCollapsed, setSidebarCollapsed] = useState(readStoredSidebarCollapsed);

  const toggleSidebarCollapsed = useCallback(() => {
    setSidebarCollapsed((prev) => {
      const next = !prev;
      try { localStorage.setItem(SIDEBAR_COLLAPSED_KEY, next ? '1' : '0'); } catch (_) {}
      return next;
    });
  }, []);

  useEffect(() => {
    const onResize = () => {
      setSidebarWidthPx((w) => clampSidebarWidthPx(w));
    };
    window.addEventListener('resize', onResize);
    return () => window.removeEventListener('resize', onResize);
  }, []);

  const handleSidebarResizeStart = useCallback((e) => {
    if (e.button !== undefined && e.button !== 0) return;
    e.preventDefault();
    const getX = (ev) => {
      if (ev.touches && ev.touches.length) return ev.touches[0].clientX;
      return ev.clientX;
    };
    const startX = getX(e);
    const startW = lastSidebarWidthRef.current;

    const onMove = (ev) => {
      ev.preventDefault();
      const x = getX(ev);
      const next = clampSidebarWidthPx(startW + (x - startX));
      setSidebarWidthPx(next);
      lastSidebarWidthRef.current = next;
    };

    const onUp = () => {
      document.removeEventListener('mousemove', onMove);
      document.removeEventListener('mouseup', onUp);
      document.removeEventListener('touchmove', onMove);
      document.removeEventListener('touchend', onUp);
      document.removeEventListener('touchcancel', onUp);
      document.body.style.cursor = '';
      document.body.style.userSelect = '';
      try {
        localStorage.setItem(SIDEBAR_WIDTH_STORAGE_KEY, String(lastSidebarWidthRef.current));
      } catch (_) {}
    };

    if (e.type === 'touchstart') {
      document.addEventListener('touchmove', onMove, { passive: false });
      document.addEventListener('touchend', onUp);
      document.addEventListener('touchcancel', onUp);
    } else {
      document.addEventListener('mousemove', onMove);
      document.addEventListener('mouseup', onUp);
      document.body.style.cursor = 'col-resize';
      document.body.style.userSelect = 'none';
    }
  }, []);

  /** 側欄輪詢 /sessions 時同步目前聊天室 Session 的 git_branch、work_dir，並執行 prune */
  const mergeSessionMetaFromList = useCallback((list) => {
    if (!Array.isArray(list)) return;
    setKnownSessionsMap((prev) => {
      const next = { ...prev };
      for (const s of list) {
        next[s.id] = s;
      }
      return next;
    });

    // 若從舊狀態遷移出雙欄但 group 0 無分頁，先補上第一個非 secondary 的 session
    if (tabGroups.groups.length > 1 && tabGroups.groups[0]?.tabs.length === 0) {
      const secId = tabGroups.groups[1]?.active;
      const first = list.find((s) => s.id !== secId);
      if (first) {
        dispatchTabGroups({ type: 'open', sessionId: first.id, groupIndex: 0 });
      }
    }

    const validIds = list.map((s) => s.id);
    dispatchTabGroups({ type: 'prune', validSessionIds: validIds });
  }, [tabGroups.groups]);

  useEffect(() => {
    registerSessionExpiredHandler(() => { setAuthed(false); setChecking(false); });
    return () => registerSessionExpiredHandler(null);
  }, []);

  useEffect(() => {
    if (isTelegram) {
      ensureTelegramSession().then(() => setAuthed(true)).catch(() => {}).finally(() => setChecking(false));
      return;
    }
    apiFetch('/sessions').then((res) => {
      if (res.ok) setAuthed(true);
    }).catch(() => {}).finally(() => setChecking(false));
  }, []);

  useEffect(() => {
    const media = window.matchMedia('(min-width: 1024px)');
    const handleChange = (e) => setIsWideScreen(e.matches);
    setIsWideScreen(media.matches);
    if (media.addEventListener) {
      media.addEventListener('change', handleChange);
    } else {
      media.addListener(handleChange);
    }
    return () => {
      if (media.removeEventListener) {
        media.removeEventListener('change', handleChange);
      } else {
        media.removeListener(handleChange);
      }
    };
  }, []);

  if (checking) {
    return <div className="flex items-center justify-center h-app"><div className="text-gray-500 text-sm">載入中…</div></div>;
  }

  const renderAuthedLayout = () => {
    if (!isWideScreen) {
      if (creating) {
        return <NewSessionComposer prefill={composerPrefill} onCreated={handleCreated} onCancel={closeComposer} />;
      }
      if (session) {
        if (isSplit) {
          const effectiveSecondary = secondarySession || (Array.isArray(sidebarSortedSessions) ? sidebarSortedSessions.find((s) => s.id !== session.id) : null);
          if (effectiveSecondary) {
            const isNarrowLayout = splitOrientation === 'vertical'
              ? (typeof window !== 'undefined' ? window.innerHeight < 600 : false)
              : (typeof window !== 'undefined' ? window.innerWidth < 900 : true);
            return (
              <SplitChatContainer
                isSplit={true}
                isNarrow={isNarrowLayout}
                orientation={splitOrientation}
                onToggleOrientation={toggleSplitOrientation}
                splitRatio={splitRatio}
                onSplitRatioChange={setSplitRatio}
                activePane={activePane}
                onFocusPane={setActivePane}
                mobileActivePane={mobileActivePane}
                onMobileActivePaneChange={setMobileActivePane}
                primarySessionName={session?.name || '主聊天室'}
                secondarySessionName={effectiveSecondary?.name || '副聊天室'}
                onCloseSecondary={closeSecondaryPane}
                primaryContent={
                  <ChatView
                    session={session}
                    onBack={() => selectSession(null)}
                    showBack={false}
                    fullHeight={false}
                    usePermModeDropdown
                    onJumpToSession={selectSession}
                    allSessions={sidebarSortedSessions}
                    onOpenComposer={openComposer}
                    handoffInfo={handoffBySession[session.id] || null}
                    onHandoffChange={handleHandoffChange}
                    isSplit={isSplit}
                    onToggleSplit={toggleSplit}
                    splitOrientation={splitOrientation}
                    onToggleSplitOrientation={toggleSplitOrientation}
                    isSecondaryPane={false}
                    isFocused={activePane === 'primary'}
                  />
                }
                secondaryContent={
                  <ChatView
                    session={effectiveSecondary}
                    onBack={() => closeSecondaryPane()}
                    showBack={false}
                    fullHeight={false}
                    usePermModeDropdown
                    onJumpToSession={(s) => selectSession(s, 'secondary')}
                    allSessions={sidebarSortedSessions}
                    onOpenComposer={openComposer}
                    handoffInfo={handoffBySession[effectiveSecondary.id] || null}
                    onHandoffChange={handleHandoffChange}
                    isSplit={isSplit}
                    onToggleSplit={toggleSplit}
                    splitOrientation={splitOrientation}
                    onToggleSplitOrientation={toggleSplitOrientation}
                    isSecondaryPane={true}
                    onClosePane={closeSecondaryPane}
                    isFocused={activePane === 'secondary'}
                  />
                }
              />
            );
          }
        }
        return (
          <ChatView
            session={session}
            onBack={() => selectSession(null)}
            usePermModeDropdown
            onJumpToSession={selectSession}
            allSessions={sidebarSortedSessions}
            onOpenComposer={openComposer}
            handoffInfo={handoffBySession[session.id] || null}
            onHandoffChange={handleHandoffChange}
            isSplit={isSplit}
            onToggleSplit={toggleSplit}
            splitOrientation={splitOrientation}
            onToggleSplitOrientation={toggleSplitOrientation}
          />
        );
      }
      return (
        <SessionView
          onEnter={selectSession}
          onSessionsLoaded={mergeSessionMetaFromList}
          onSortedSessionsChange={setSidebarSortedSessions}
          activeSessionId={activePane === 'secondary' ? secondarySession?.id : session?.id}
          onCreateNew={openComposer}
          onOpenSettings={() => setSettingsOpen(true)}
          hasUpdate={desktopUpdate.hasUpdate}
          onOpenUpdate={desktopUpdate.openUpdate}
          isSplit={isSplit}
          onToggleSplit={toggleSplit}
          isWideScreen={false}
          onOpenOtherPane={openInOtherPane}
          canOpenOtherPane={canOpenOtherPane}
        />
      );
    }

    return (
      <div className="ra-desktop-shell h-app flex bg-ra-bg min-w-0 relative">
        <aside
          style={{ width: sidebarCollapsed ? SIDEBAR_RAIL_WIDTH : sidebarWidthPx, flexShrink: 0 }}
          className="min-w-0 bg-ra-sunken border-r border-ra-line-soft flex flex-col"
        >
          <SessionView
            onEnter={selectSession}
            onSessionsLoaded={mergeSessionMetaFromList}
            onSortedSessionsChange={setSidebarSortedSessions}
            activeSessionId={activePane === 'secondary' ? secondarySession?.id : session?.id}
            onCreateNew={openComposer}
            onOpenSettings={() => setSettingsOpen(true)}
            hasUpdate={desktopUpdate.hasUpdate}
            onOpenUpdate={desktopUpdate.openUpdate}
            onToggleSidebar={toggleSidebarCollapsed}
            collapsed={sidebarCollapsed}
            onOpenOtherPane={openInOtherPane}
            canOpenOtherPane={canOpenOtherPane}
            isSplit={isSplit}
            onToggleSplit={toggleSplit}
            isWideScreen={isWideScreen}
          />
        </aside>
        {!sidebarCollapsed && (
          <div
            role="separator"
            aria-orientation="vertical"
            aria-valuenow={sidebarWidthPx}
            aria-valuemin={SIDEBAR_WIDTH_MIN}
            aria-valuemax={sidebarWidthMaxPx()}
            title="拖曳調整側欄寬度"
            className="group relative w-1.5 shrink-0 cursor-col-resize select-none touch-none bg-transparent hover:bg-[oklch(0.62_0.19_275_/15%)]"
            onMouseDown={handleSidebarResizeStart}
            onTouchStart={handleSidebarResizeStart}
          >
            <span
              className="pointer-events-none absolute left-1/2 top-0 bottom-0 w-px -translate-x-1/2 bg-gray-800 group-hover:bg-violet-500/50"
              aria-hidden
            />
          </div>
        )}
        <main className="flex-1 min-w-0 min-h-0">
          {creating ? (
            <NewSessionComposer prefill={composerPrefill} onCreated={handleCreated} onCancel={closeComposer} />
          ) : session ? (
            <SplitChatContainer
              isSplit={isSplit}
              splitRatio={splitRatio}
              onSplitRatioChange={setSplitRatio}
              orientation={splitOrientation}
              onToggleOrientation={toggleSplitOrientation}
              activePane={activePane}
              onFocusPane={setActivePane}
              mobileActivePane={mobileActivePane}
              onMobileActivePaneChange={setMobileActivePane}
              primarySessionName={session?.name || '主聊天室'}
              secondarySessionName={secondarySession?.name || '副聊天室'}
              onCloseSecondary={closeSecondaryPane}
              primaryContent={
                <SessionGroupFrame
                  groupIndex={0}
                  group={tabGroups.groups[0]}
                  isFocused={activePane === 'primary'}
                  allSessions={sidebarSortedSessions}
                  groupCount={tabGroups.groups.length}
                  isWideScreen={isWideScreen}
                  onSelectTab={(tabId) => dispatchTabGroups({ type: 'open', sessionId: tabId, groupIndex: 0 })}
                  onCloseTab={(tabId) => dispatchTabGroups({ type: 'close', sessionId: tabId, groupIndex: 0 })}
                  onMoveTab={tabId => openInOtherPane({ id: tabId }, 0)}
                  onFocusGroup={() => setActivePane('primary')}
                  onDropOnTabBar={(sessionId, fromGroup, insertIndex) =>
                    handleDropOnTabBar(sessionId, fromGroup, 0, insertIndex)
                  }
                  onDropOnContent={(sessionId, fromGroup, region) =>
                    handleDropOnContent(sessionId, fromGroup, region, 0)
                  }
                >
                  <ChatView
                    key={session.id}
                    session={session}
                    onBack={() => selectSession(null)}
                    showBack={false}
                    fullHeight={false}
                    onJumpToSession={selectSession}
                    allSessions={sidebarSortedSessions}
                    onOpenComposer={openComposer}
                    handoffInfo={handoffBySession[session.id] || null}
                    onHandoffChange={handleHandoffChange}
                    isSplit={isSplit}
                    onToggleSplit={toggleSplit}
                    splitOrientation={splitOrientation}
                    onToggleSplitOrientation={toggleSplitOrientation}
                    isSecondaryPane={false}
                    isFocused={activePane === 'primary'}
                  />
                </SessionGroupFrame>
              }
              secondaryContent={
                <SessionGroupFrame
                  groupIndex={1}
                  group={tabGroups.groups[1]}
                  isFocused={activePane === 'secondary'}
                  allSessions={sidebarSortedSessions}
                  groupCount={tabGroups.groups.length}
                  isWideScreen={isWideScreen}
                  onSelectTab={(tabId) => dispatchTabGroups({ type: 'open', sessionId: tabId, groupIndex: 1 })}
                  onCloseTab={(tabId) => dispatchTabGroups({ type: 'close', sessionId: tabId, groupIndex: 1 })}
                  onMoveTab={tabId => openInOtherPane({ id: tabId }, 1)}
                  onFocusGroup={() => setActivePane('secondary')}
                  onDropOnTabBar={(sessionId, fromGroup, insertIndex) =>
                    handleDropOnTabBar(sessionId, fromGroup, 1, insertIndex)
                  }
                  onDropOnContent={(sessionId, fromGroup, region) =>
                    handleDropOnContent(sessionId, fromGroup, region, 1)
                  }
                >
                  {secondarySession ? (
                    <ChatView
                      key={secondarySession.id}
                      session={secondarySession}
                      onBack={closeSecondaryPane}
                      showBack={false}
                      fullHeight={false}
                      onJumpToSession={(s) => selectSession(s, 'secondary')}
                      allSessions={sidebarSortedSessions}
                      onOpenComposer={openComposer}
                      handoffInfo={handoffBySession[secondarySession.id] || null}
                      onHandoffChange={handleHandoffChange}
                      isSplit={isSplit}
                      onToggleSplit={toggleSplit}
                      splitOrientation={splitOrientation}
                      onToggleSplitOrientation={toggleSplitOrientation}
                      isSecondaryPane={true}
                      onClosePane={closeSecondaryPane}
                      isFocused={activePane === 'secondary'}
                    />
                  ) : (
                    <div className="h-full flex flex-col items-center justify-center gap-3 bg-ra-bg p-6 text-center">
                      <div className="text-sm text-gray-400">副聊天室尚未選取會話</div>
                      <div className="text-xs text-gray-500 mb-2">點擊下方任一會話直接在副欄開啟：</div>
                      <div className="flex flex-wrap gap-2 justify-center max-w-md">
                        {sidebarSortedSessions.filter((s) => s.id !== session?.id).slice(0, 6).map((s) => (
                          <button
                            key={s.id}
                            type="button"
                            onClick={() => selectSession(s, 'secondary')}
                            className="px-3 py-1.5 rounded-lg bg-ra-card hover:bg-violet-600/30 text-xs text-gray-200 border border-ra-line truncate max-w-[180px]"
                          >
                            {s.name || '未命名'}
                          </button>
                        ))}
                      </div>
                      <button
                        type="button"
                        onClick={closeSecondaryPane}
                        className="mt-4 px-3 py-1.5 text-xs text-gray-400 hover:text-red-400 border border-gray-700 rounded-md"
                      >
                        關閉副視窗
                      </button>
                    </div>
                  )}
                </SessionGroupFrame>
              }
            />
          ) : (
            <div
              className="session-group-frame h-full w-full flex flex-col items-center justify-center gap-3 bg-ra-bg relative"
              onDragOver={(e) => {
                e.preventDefault();
                e.dataTransfer.dropEffect = 'move';
              }}
              onDrop={(e) => {
                e.preventDefault();
                if (typeof window !== 'undefined') window.__miniapp_drag_source = null;
                const data = parseSessionDragData(e.dataTransfer);
                if (!data || !data.sessionId) return;
                if (!sidebarSortedSessions.some((s) => s.id === data.sessionId)) return;
                dispatchTabGroups({ type: 'open', sessionId: data.sessionId, groupIndex: 0 });
              }}
            >
              <div className="w-10 h-10 rounded-[10px] bg-gradient-to-br from-[oklch(0.62_0.19_275)] to-[oklch(0.6_0.17_300)] flex items-center justify-center opacity-80" aria-hidden>
                <div className="w-3.5 h-3.5 rounded-[3px] bg-white" />
              </div>
              <div className="text-ra-t5 text-sm">從左側選擇 Session，或按 + 建立新對話</div>
            </div>
          )}
        </main>
      </div>
    );
  };

  return (
    <>
      <DebugBanner />
      {!authed
        ? (isTelegram ? <div className="p-6 text-center text-ra-t2" role="alert">
            <p>{telegramAuthEnded ? '登入已失效，請關閉後重新開啟 Mini App；草稿會保留。' : '暫時無法連線，請稍後重試。'}</p>
            <button className="mt-4 rounded-lg px-4 py-2 bg-ra-raised" onClick={() => telegramAuthEnded ? tgApp.close() : location.reload()}>{telegramAuthEnded ? '關閉 Mini App' : '重試'}</button>
          </div> : <PasswordView onSuccess={() => setAuthed(true)} />)
        : renderAuthedLayout()
      }
      <SettingsModal
        open={settingsOpen}
        onClose={() => setSettingsOpen(false)}
        desktopUpdate={desktopUpdate}
        onJumpToSession={(sessionId) => {
          const hit = sidebarSortedSessions.find((x) => x.id === sessionId);
          if (!hit) {
            showToast('找不到這個聊天室（可能已刪除）', { error: true });
            return;
          }
          setSettingsOpen(false);
          selectSession(hit);
        }}
      />
      {authed && <QuickSwitcher sessions={sidebarSortedSessions} onSelect={selectSession} />}
    </>
  );
}

// /share/<token> 進入訪客模式（只看／參與單一聊天室，不載入側欄、設定、快速切換）；其餘為擁有者主介面。
// 先等 /setup/status。needs_setup 才進精靈；查詢中與失敗都不掛 App。
ReactDOM.createRoot(document.getElementById('root')).render(<SetupGate guest={isGuestMode} />);
