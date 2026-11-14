// ── 會話分頁列與群組外框元件 (VSCode-style Session Tabs) ───────────────────────
// 純瀏覽器 React 元件，無 import/export，依賴全域作用域。

function revealSessionTab(sessionId) {
  requestAnimationFrame(() => {
    const tab = Array.from(document.querySelectorAll('[data-session-tab-id]'))
      .find(element => element.dataset.sessionTabId === sessionId);
    if (!tab) return;
    const reduced = window.matchMedia('(prefers-reduced-motion: reduce)').matches;
    tab.querySelector('[role="tab"]')?.focus({ preventScroll: true });
    tab.scrollIntoView({ block: 'nearest', inline: 'center', behavior: reduced ? 'auto' : 'smooth' });
    if (!reduced) tab.animate?.([
      { boxShadow: 'inset 0 0 0 2px oklch(var(--ra-accent-text) / 0.85)' },
      { boxShadow: 'inset 0 0 0 2px transparent' },
    ], { duration: 1200 });
  });
}

function focusSessionTabAfterClose(tabBarRef) {
  requestAnimationFrame(() => {
    const target = tabBarRef.current?.querySelector('[role="tab"][aria-selected="true"]')
      || document.querySelector('.session-tab-bar [role="tab"][aria-selected="true"]')
      || document.querySelector('[data-session-navigation]');
    target?.focus();
  });
}

function idleSessionTabIds(tabs, sessionMap) {
  // ponytail: 只關閉明確閒置的分頁；缺少或未知狀態一律保留。
  return tabs.filter(id => {
    const session = sessionMap.get(id);
    return session?.status === 'idle' && !session.lingering;
  });
}

/**
 * 個別群組的分頁列元件
 */
function SessionTabBar({
  groupIndex = 0,
  tabs = [],
  activeTab = null,
  isFocused = false,
  allSessions = [],
  isWideScreen = true,
  onSelectTab,
  onCloseTab,
  onDropOnTabBar,
  onMoveTab,
  groupCount = 1,
}) {
  const tabBarRef = useRef(null);
  const [tabMenu, setTabMenu] = useState(null);
  const bindTabMenu = useLongPress(setTabMenu);
  const closeTabMenu = useCallback(() => setTabMenu(null), []);
  useEffect(() => setTabMenu(null), [activeTab]);
  const [dropInsertIndex, setDropInsertIndex] = useState(null);

  const sessionMap = useMemo(() => {
    const map = new Map();
    if (Array.isArray(allSessions)) {
      for (const s of allSessions) {
        if (s && s.id) map.set(s.id, s);
      }
    }
    return map;
  }, [allSessions]);

  useEffect(() => {
    const handleKeyDown = (e) => {
      if (e.key === 'Escape') {
        setDropInsertIndex(null);
      }
    };
    const handleDragEnd = () => {
      setDropInsertIndex(null);
    };
    window.addEventListener('keydown', handleKeyDown);
    window.addEventListener('dragend', handleDragEnd);
    window.addEventListener('drop', handleDragEnd);
    return () => {
      window.removeEventListener('keydown', handleKeyDown);
      window.removeEventListener('dragend', handleDragEnd);
      window.removeEventListener('drop', handleDragEnd);
    };
  }, []);

  // 滾輪（垂直）轉橫向捲動分頁列；React onWheel 為 passive，需原生 listener 才能 preventDefault
  const hasTabs = Array.isArray(tabs) && tabs.length > 0;
  const closeTabs = ids => {
    ids.forEach(id => onCloseTab?.(id));
    focusSessionTabAfterClose(tabBarRef);
  };
  useEffect(() => {
    const el = tabBarRef.current;
    if (!el) return undefined;
    const onWheel = (e) => {
      if (e.ctrlKey || el.scrollWidth <= el.clientWidth) return;
      // 觸控板已有橫向位移時維持原生行為
      if (Math.abs(e.deltaX) >= Math.abs(e.deltaY)) return;
      // deltaMode: 0=pixel, 1=line, 2=page
      const delta = e.deltaMode === 1 ? e.deltaY * 32 : e.deltaMode === 2 ? e.deltaY * el.clientWidth : e.deltaY;
      el.scrollLeft += delta;
      e.preventDefault();
    };
    el.addEventListener('wheel', onWheel, { passive: false });
    return () => el.removeEventListener('wheel', onWheel);
  }, [hasTabs]);

  const idleTabs = idleSessionTabIds(tabs, sessionMap);
  if (!hasTabs) {
    return null;
  }

  const getIndicatorStyle = (idx) => {
    if (!tabBarRef.current) return { left: 4 };
    const containerRect = tabBarRef.current.getBoundingClientRect();
    const tabElements = Array.from(tabBarRef.current.querySelectorAll('.session-tab'));
    if (tabElements.length === 0 || idx <= 0) {
      return { left: 4 };
    }
    if (idx < tabElements.length) {
      const r = tabElements[idx].getBoundingClientRect();
      return { left: Math.max(4, r.left - containerRect.left - 2) };
    }
    const last = tabElements[tabElements.length - 1].getBoundingClientRect();
    return { left: Math.max(4, last.right - containerRect.left + 2) };
  };

  return (
    <>
    <div
      ref={tabBarRef}
      role="tablist"
      aria-label={`會話分頁列 ${groupIndex + 1}`}
      className="session-tab-bar group/tabbar shrink-0 flex items-center overflow-x-auto [scrollbar-width:none] [&::-webkit-scrollbar]:hidden flex-nowrap bg-ra-sunken border-b border-ra-line-soft px-1.5 pt-1.5 gap-1 select-none relative"
      onDragOver={(e) => {
        if (!isSessionDragTypes(e.dataTransfer.types)) return;
        e.preventDefault();
        e.dataTransfer.dropEffect = 'move';
        const tabElements = tabBarRef.current ? Array.from(tabBarRef.current.querySelectorAll('.session-tab')) : [];
        const tabRects = tabElements.map((el) => {
          const r = el.getBoundingClientRect();
          return { left: r.left, width: r.width };
        });
        const dragSource = typeof window !== 'undefined' ? window.__miniapp_drag_source : null;
        // 被拖會話若已在本列（不論來自分頁或側欄），索引排除自身，與 reducer 'move' 語意一致
        const draggedIndex = dragSource ? tabs.indexOf(dragSource.sessionId) : -1;
        const insertIdx = calculateTabInsertIndex(tabRects, e.clientX, draggedIndex);
        if (dropInsertIndex !== insertIdx) {
          setDropInsertIndex(insertIdx);
        }
      }}
      onDragLeave={(e) => {
        if (tabBarRef.current && !tabBarRef.current.contains(e.relatedTarget)) {
          setDropInsertIndex(null);
        }
      }}
      onDrop={(e) => {
        if (!isSessionDragTypes(e.dataTransfer.types)) return;
        e.preventDefault();
        setDropInsertIndex(null);
        if (typeof window !== 'undefined') {
          window.__miniapp_drag_source = null;
        }
        const data = parseSessionDragData(e.dataTransfer);
        if (!data || !data.sessionId) return;
        if (!allSessions.some((s) => s.id === data.sessionId)) return;

        const tabElements = tabBarRef.current ? Array.from(tabBarRef.current.querySelectorAll('.session-tab')) : [];
        const tabRects = tabElements.map((el) => {
          const r = el.getBoundingClientRect();
          return { left: r.left, width: r.width };
        });
        const draggedIndex = tabs.indexOf(data.sessionId);
        const insertIdx = calculateTabInsertIndex(tabRects, e.clientX, draggedIndex);

        onDropOnTabBar?.(data.sessionId, data.fromGroup, insertIdx);
      }}
    >
      {tabs.map((tabId) => {
        const s = sessionMap.get(tabId) || { id: tabId, name: '未命名', agent_type: 'claude' };
        const isActive = tabId === activeTab;
        const dotClass = sessionStatusDotClass(s);
        const statusLabel = sessionStatusLabel(s);
        const agentType = sessionDisplayAgent(s);

        return (
          <div
            key={tabId}
            data-session-tab-id={tabId}
            {...bindTabMenu({ sessionId: tabId })}
            title={s.name || tabId}
            draggable={isWideScreen ? true : undefined}
            onDragStart={(e) => {
              if (!isWideScreen) {
                e.preventDefault();
                return;
              }
              const payload = JSON.stringify({ sessionId: tabId, fromGroup: groupIndex });
              e.dataTransfer.setData(typeof MINIAPP_SESSION_MIME !== 'undefined' ? MINIAPP_SESSION_MIME : 'application/x-miniapp-session', payload);
              e.dataTransfer.effectAllowed = 'move';
              if (typeof window !== 'undefined') {
                window.__miniapp_drag_source = { sessionId: tabId, fromGroup: groupIndex };
              }
            }}
            onDragEnd={() => {
              if (typeof window !== 'undefined') {
                window.__miniapp_drag_source = null;
              }
              setDropInsertIndex(null);
            }}
            onClick={() => onSelectTab?.(tabId)}
            onAuxClick={(e) => {
              if (e.button === 1) {
                e.preventDefault();
                onCloseTab?.(tabId);
              }
            }}
            onMouseDown={(e) => {
              if (e.button === 1) e.preventDefault();
            }}
            className={`session-tab group/tab relative flex items-center gap-1.5 px-3 py-1.5 rounded-t-lg text-xs cursor-pointer border-t border-x transition-colors shrink-0 max-w-[200px] min-w-[90px] ${
              isActive
                ? 'bg-ra-card text-ra-t1 font-medium border-ra-line shadow-sm'
                : 'bg-transparent text-ra-t4 hover:bg-ra-bg hover:text-ra-t2 border-transparent'
            }`}
          >
            {/* 作用中分頁上方指示線：聚焦時為亮紫，非聚焦時為深紫/灰 */}
            {isActive && (
              <span
                className={`absolute top-0 left-0 right-0 h-0.5 rounded-t transition-colors ${
                  isFocused ? 'bg-violet-500' : 'bg-gray-500'
                }`}
                aria-hidden
              />
            )}

            <button
              type="button"
              role="tab"
              aria-selected={isActive}
              tabIndex={isActive ? 0 : -1}
              className="flex items-center gap-1.5 flex-1 min-w-0 text-left rounded-sm"
              onKeyDown={(e) => {
                if (e.ctrlKey || e.metaKey || e.altKey || e.isComposing || e.keyCode === 229) return;
                const targetId = sessionTabKeyboardTarget(tabs, tabId, e.key);
                if (targetId === null) return;
                e.preventDefault();
                onSelectTab?.(targetId);
                const targetIndex = tabs.indexOf(targetId);
                const target = tabBarRef.current?.querySelectorAll('[role="tab"]')[targetIndex];
                target?.focus();
                target?.scrollIntoView({ block: 'nearest', inline: 'nearest' });
              }}
            >
            {/* Agent 圖示 */}
            <span
              className={`inline-flex items-center justify-center w-4 h-4 shrink-0 rounded text-[10px] ${getAgentBadgeClass(
                agentType
              )}`}
              title={AGENT_LABEL[agentType] || agentType}
            >
              <AgentBadgeIcon agentType={agentType} session={s} />
            </span>

            {/* 背景/前景會話狀態點（執行中/待授權） */}
            {dotClass ? (
              <span
                className={`${dotClass} shrink-0`}
                title={statusLabel}
                aria-label={statusLabel}
              />
            ) : null}

            {/* 會話名稱 */}
            <span className="truncate flex-1 min-w-0">
              {s.name || '未命名'}
            </span>
            </button>

            {/* 關閉按鈕 */}
            <button
              type="button"
              onClick={(e) => {
                e.stopPropagation();
                onCloseTab?.(tabId);
                focusSessionTabAfterClose(tabBarRef);
              }}
              tabIndex={isActive ? 0 : -1}
              aria-label={`關閉 ${s.name || '分頁'}`}
              title="關閉分頁"
              className={`p-0.5 rounded text-ra-t5 hover:text-red-400 hover:bg-ra-raised transition-opacity shrink-0 ${
                isActive ? 'opacity-80 hover:opacity-100' : 'opacity-0 group-hover/tab:opacity-100 group-focus-within/tabbar:opacity-100'
              }`}
            >
              <svg
                viewBox="0 0 16 16"
                width="12"
                height="12"
                fill="none"
                stroke="currentColor"
                strokeWidth="2"
                strokeLinecap="round"
                strokeLinejoin="round"
                aria-hidden
              >
                <line x1="4" y1="4" x2="12" y2="12" />
                <line x1="12" y1="4" x2="4" y2="12" />
              </svg>
            </button>
          </div>
        );
      })}

      {/* 分頁列放置位置指示線 */}
      {dropInsertIndex !== null && (
        <div
          className="session-tab-drop-indicator pointer-events-none absolute top-1 bottom-1 w-[3px] bg-violet-500 rounded-full z-30 shadow-[0_0_8px_rgba(139,92,246,0.8)]"
          style={getIndicatorStyle(dropInsertIndex)}
        />
      )}
    </div>
    <ContextMenu menu={tabs.includes(tabMenu?.sessionId) ? tabMenu : null} onClose={closeTabMenu} label="分頁操作" items={[
      ...(isWideScreen && onMoveTab ? [{ label: '移到另一個窗格', disabled: groupCount === 1 && tabs.length < 2,
        reason: '至少需要兩個分頁才能分割', onSelect: () => {
          const id = tabMenu.sessionId;
          onMoveTab(id);
          requestAnimationFrame(() => Array.from(document.querySelectorAll('.session-tab'))
            .find(element => element.dataset.sessionTabId === id)?.querySelector('[role="tab"]')?.focus());
        } }, { separator: true }] : []),
      { label: '關閉分頁', onSelect: () => closeTabs([tabMenu.sessionId]) },
      { label: '關閉閒置分頁', disabled: idleTabs.length === 0, reason: idleTabs.length === 0 ? '此窗格沒有閒置分頁' : '',
        onSelect: () => closeTabs(idleTabs) },
      { label: '關閉其他分頁', disabled: tabs.length < 2, onSelect: () => closeTabs(contextTabCloseIds(tabs, tabMenu.sessionId, 'others')) },
      { label: '關閉右側分頁', disabled: tabs.indexOf(tabMenu?.sessionId) === tabs.length - 1,
        onSelect: () => closeTabs(contextTabCloseIds(tabs, tabMenu.sessionId, 'right')) },
    ]} />
    </>
  );
}

/**
 * 群組外框元件：上方分頁列、下方作用中會話內容（ChatView 或空佔位）
 */
function SessionGroupFrame({
  groupIndex = 0,
  group = null,
  isFocused = false,
  allSessions = [],
  groupCount = 1,
  isWideScreen = true,
  onSelectTab,
  onCloseTab,
  onMoveTab,
  onFocusGroup,
  onDropOnTabBar,
  onDropOnContent,
  children,
}) {
  const contentRef = useRef(null);
  const [dropPreviewRegion, setDropPreviewRegion] = useState(null);

  const tabs = group?.tabs || [];
  const activeTab = group?.active || null;

  useEffect(() => {
    const handleKeyDown = (e) => {
      if (e.key === 'Escape') {
        setDropPreviewRegion(null);
      }
    };
    const handleDragEnd = () => {
      setDropPreviewRegion(null);
    };
    window.addEventListener('keydown', handleKeyDown);
    window.addEventListener('dragend', handleDragEnd);
    window.addEventListener('drop', handleDragEnd);
    return () => {
      window.removeEventListener('keydown', handleKeyDown);
      window.removeEventListener('dragend', handleDragEnd);
      window.removeEventListener('drop', handleDragEnd);
    };
  }, []);

  const getPreviewOverlayStyle = (region) => {
    switch (region) {
      case 'left':
        return { top: 6, left: 6, bottom: 6, width: 'calc(50% - 9px)' };
      case 'right':
        return { top: 6, right: 6, bottom: 6, width: 'calc(50% - 9px)' };
      case 'top':
        return { top: 6, left: 6, right: 6, height: 'calc(50% - 9px)' };
      case 'bottom':
        return { bottom: 6, left: 6, right: 6, height: 'calc(50% - 9px)' };
      case 'center':
      default:
        return { top: 6, left: 6, right: 6, bottom: 6 };
    }
  };

  return (
    <div
      className="session-group-frame h-full w-full min-w-0 min-h-0 flex flex-col overflow-hidden relative"
      onClickCapture={onFocusGroup}
    >
      <SessionTabBar
        groupIndex={groupIndex}
        tabs={tabs}
        activeTab={activeTab}
        isFocused={isFocused}
        allSessions={allSessions}
        isWideScreen={isWideScreen}
        onSelectTab={onSelectTab}
        onCloseTab={onCloseTab}
        onMoveTab={onMoveTab}
        groupCount={groupCount}
        onDropOnTabBar={onDropOnTabBar}
      />
      <div
        ref={contentRef}
        className="session-group-content flex-1 min-w-0 min-h-0 relative overflow-hidden"
        onDragOver={(e) => {
          if (!isSessionDragTypes(e.dataTransfer.types)) return;
          e.preventDefault();
          e.dataTransfer.dropEffect = 'move';
          if (!contentRef.current) return;
          const rect = contentRef.current.getBoundingClientRect();
          const region = calculateDropRegion(rect, e.clientX, e.clientY, groupCount);
          if (dropPreviewRegion !== region) {
            setDropPreviewRegion(region);
          }
        }}
        onDragLeave={(e) => {
          if (contentRef.current && !contentRef.current.contains(e.relatedTarget)) {
            setDropPreviewRegion(null);
          }
        }}
        onDrop={(e) => {
          if (!isSessionDragTypes(e.dataTransfer.types)) return;
          e.preventDefault();
          setDropPreviewRegion(null);
          if (typeof window !== 'undefined') {
            window.__miniapp_drag_source = null;
          }
          const data = parseSessionDragData(e.dataTransfer);
          if (!data || !data.sessionId) return;
          if (!allSessions.some((s) => s.id === data.sessionId)) return;

          const rect = contentRef.current ? contentRef.current.getBoundingClientRect() : null;
          const region = calculateDropRegion(rect, e.clientX, e.clientY, groupCount);

          onDropOnContent?.(data.sessionId, data.fromGroup, region);
        }}
      >
        {children}

        {/* 拖放區域預覽覆蓋層 */}
        {dropPreviewRegion && (
          <div
            className="session-drop-preview absolute pointer-events-none z-50 bg-violet-500/20 border-2 border-violet-500 border-dashed rounded-lg transition-all duration-75"
            data-drop-region={dropPreviewRegion}
            style={getPreviewOverlayStyle(dropPreviewRegion)}
          />
        )}
      </div>
    </div>
  );
}

