// ── 會話分頁列與群組外框元件 (VSCode-style Session Tabs) ───────────────────────
// 純瀏覽器 React 元件，無 import/export，依賴全域作用域。

function scrollSessionTabIntoView(tab, options = {}) {
  const scroller = tab?.closest?.('[data-tab-zone]');
  if (!scroller) { tab?.scrollIntoView?.({ block: 'nearest', inline: 'nearest', ...options }); return; }
  const rect = tab.getBoundingClientRect();
  const viewport = scroller.getBoundingClientRect();
  const delta = options.inline === 'center'
    ? (rect.left + rect.right - viewport.left - viewport.right) / 2
    : rect.left < viewport.left ? rect.left - viewport.left : Math.max(0, rect.right - viewport.right);
  // Scroll this zone explicitly: older browsers ignore scrollIntoView's container option.
  scroller.scrollTo({ left: scroller.scrollLeft + delta, behavior: options.behavior || 'auto' });
}

function revealSessionTab(sessionId) {
  requestAnimationFrame(() => {
    const tab = Array.from(document.querySelectorAll('[data-session-tab-id]'))
      .find(element => element.dataset.sessionTabId === sessionId);
    if (!tab) return;
    const reduced = window.matchMedia('(prefers-reduced-motion: reduce)').matches;
    tab.querySelector('[role="tab"]')?.focus({ preventScroll: true });
    scrollSessionTabIntoView(tab, { inline: 'center', behavior: reduced ? 'auto' : 'smooth' });
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
    target?.focus({ preventScroll: true });
    scrollSessionTabIntoView(target?.closest?.('.session-tab'));
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
  pinnedTabs = [],
  isFocused = false,
  allSessions = [],
  isWideScreen = true,
  onSelectTab,
  onCloseTab,
  onTogglePin,
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
  const pinnedIds = tabs.filter(id => pinnedTabs.includes(id));
  const regularIds = tabs.filter(id => !pinnedTabs.includes(id));

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
    ids.filter(id => !pinnedTabs.includes(id)).forEach(id => onCloseTab?.(id));
    focusSessionTabAfterClose(tabBarRef);
  };
  useEffect(() => {
    const el = tabBarRef.current;
    if (!el) return undefined;
    const onWheel = (e) => {
      const zone = e.target.closest?.('[data-tab-zone]');
      if (!zone || e.ctrlKey || zone.scrollWidth <= zone.clientWidth) return;
      // 觸控板已有橫向位移時維持原生行為
      if (Math.abs(e.deltaX) >= Math.abs(e.deltaY)) return;
      // deltaMode: 0=pixel, 1=line, 2=page
      const delta = e.deltaMode === 1 ? e.deltaY * 32 : e.deltaMode === 2 ? e.deltaY * zone.clientWidth : e.deltaY;
      zone.scrollLeft += delta;
      e.preventDefault();
    };
    el.addEventListener('wheel', onWheel, { passive: false });
    return () => el.removeEventListener('wheel', onWheel);
  }, [hasTabs]);

  useEffect(() => {
    const tab = Array.from(tabBarRef.current?.querySelectorAll('.session-tab') || [])
      .find(element => element.dataset.sessionTabId === activeTab);
    if (tab) scrollSessionTabIntoView(tab);
  }, [activeTab, tabs, pinnedTabs]);

  const idleTabs = idleSessionTabIds(regularIds, sessionMap);
  const otherTabs = contextTabCloseIds(tabs, tabMenu?.sessionId, 'others').filter(id => !pinnedTabs.includes(id));
  const rightTabs = contextTabCloseIds(tabs, tabMenu?.sessionId, 'right').filter(id => !pinnedTabs.includes(id));
  const menuPinned = pinnedTabs.includes(tabMenu?.sessionId);
  const togglePin = id => { onTogglePin?.(id); revealSessionTab(id); };
  if (!hasTabs) {
    return null;
  }

  const getDropIndex = (event, sessionId) => {
    const pinned = pinnedTabs.includes(sessionId);
    const zone = tabBarRef.current?.querySelector(`[data-tab-zone="${pinned ? 'pinned' : 'regular'}"]`);
    const elements = Array.from(zone?.querySelectorAll('.session-tab') || [])
      .filter(element => element.dataset.sessionTabId !== sessionId);
    const rects = elements.map(element => element.getBoundingClientRect());
    const remaining = tabs.filter(id => id !== sessionId);
    const offset = pinned ? 0 : remaining.filter(id => pinnedTabs.includes(id)).length;
    const viewport = zone?.getBoundingClientRect();
    const index = viewport && event.clientX <= viewport.left ? 0
      : viewport && event.clientX >= viewport.right ? elements.length : calculateTabInsertIndex(rects, event.clientX);
    return sessionTabInsertIndex(remaining, pinnedTabs, sessionId, offset + index);
  };

  const getIndicatorStyle = ({ index, sessionId }) => {
    const pinned = pinnedTabs.includes(sessionId);
    const zone = tabBarRef.current?.querySelector(`[data-tab-zone="${pinned ? 'pinned' : 'regular'}"]`);
    const containerRect = tabBarRef.current.getBoundingClientRect();
    if (!zone) return { left: pinned ? 4 : containerRect.width - 7 };
    const zoneRect = zone.getBoundingClientRect();
    const elements = Array.from(zone.querySelectorAll('.session-tab')).filter(el => el.dataset.sessionTabId !== sessionId);
    const offset = pinned ? 0 : pinnedIds.filter(id => id !== sessionId).length;
    const idx = index - offset;
    const edge = elements[idx]?.getBoundingClientRect().left
      ?? elements[elements.length - 1]?.getBoundingClientRect().right ?? zoneRect.left;
    return { left: Math.max(zoneRect.left, Math.min(zoneRect.right - 3, edge)) - containerRect.left };
  };

  return (
    <>
    <div
      ref={tabBarRef}
      role="tablist"
      aria-label={`會話分頁列 ${groupIndex + 1}`}
      className="session-tab-bar group/tabbar shrink-0 min-w-0 flex items-stretch flex-nowrap bg-ra-sunken border-b border-ra-line-soft px-1.5 pt-1.5 gap-1 select-none relative"
      onDragOver={(e) => {
        if (!isSessionDragTypes(e.dataTransfer.types)) return;
        e.preventDefault();
        e.dataTransfer.dropEffect = 'move';
        const dragSource = typeof window !== 'undefined' ? window.__miniapp_drag_source : null;
        setDropInsertIndex({ index: getDropIndex(e, dragSource?.sessionId), sessionId: dragSource?.sessionId });
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

        const insertIdx = getDropIndex(e, data.sessionId);

        onDropOnTabBar?.(data.sessionId, data.fromGroup, insertIdx);
      }}
    >
      {[pinnedIds, regularIds].map((ids, zoneIndex) => ids.length > 0 && (
      <div key={zoneIndex} role="presentation" data-tab-zone={zoneIndex === 0 ? 'pinned' : 'regular'}
        className={`flex min-w-0 items-stretch gap-1 overflow-x-auto [scrollbar-width:none] [&::-webkit-scrollbar]:hidden ${zoneIndex === 0 ? 'flex-initial' : 'flex-1'}`}
        style={{ maxWidth: zoneIndex === 0 && regularIds.length > 0 ? '50%' : undefined }}>
      {ids.map((tabId) => {
        const s = sessionMap.get(tabId) || { id: tabId, name: '未命名', agent_type: 'claude' };
        const isActive = tabId === activeTab;
        const isPinned = pinnedTabs.includes(tabId);
        const dotClass = sessionStatusDotClass(s);
        const statusLabel = sessionStatusLabel(s);
        const agentType = sessionDisplayAgent(s);
        const unreadCount = sessionUnreadCount(s);
        const unreadBadgeText = unreadCount > 99 ? '99+' : String(unreadCount);
        const unreadLabel = unreadCount > 0 ? `未讀訊息 ${unreadBadgeText} 則` : '';

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
                if (!isPinned) onCloseTab?.(tabId);
              }
            }}
            onMouseDown={(e) => {
              if (e.button === 1) e.preventDefault();
            }}
            className={`session-tab group/tab relative flex items-center gap-1.5 px-3 py-1.5 rounded-t-lg text-xs cursor-pointer border-t border-x transition-colors shrink-0 max-w-[200px] min-w-[90px] ${isPinned ? 'w-[144px]' : ''} ${
              isActive
                ? 'bg-ra-card text-ra-t1 font-medium border-ra-line shadow-sm'
                : 'bg-transparent text-ra-t4 hover:bg-ra-bg hover:text-ra-t2 border-transparent'
            }`}
          >
            {/* 作用中分頁上方指示線：聚焦時為亮紫，非聚焦時為深紫/灰 */}
            {isActive && (
              <span
                className={`absolute top-0 left-0 right-0 border-t-2 rounded-t transition-colors ${
                  isFocused ? 'border-violet-500' : 'border-gray-500'
                }`}
                aria-hidden
              />
            )}

            <button
              type="button"
              role="tab"
              aria-selected={isActive}
              aria-description={[isPinned && '已釘選；請先取消釘選再關閉', unreadLabel].filter(Boolean).join('；') || undefined}
              aria-keyshortcuts={isPinned ? undefined : 'Delete'}
              tabIndex={isActive ? 0 : -1}
              className="flex items-center gap-1.5 flex-1 min-w-0 text-left rounded-sm"
              onKeyDown={(e) => {
                if (e.ctrlKey || e.metaKey || e.altKey || e.isComposing || e.keyCode === 229) return;
                if (e.key === 'Delete') {
                  e.preventDefault();
                  if (!isPinned) {
                    onCloseTab?.(tabId);
                    focusSessionTabAfterClose(tabBarRef);
                  }
                  return;
                }
                const targetId = sessionTabKeyboardTarget(tabs, tabId, e.key);
                if (targetId === null) return;
                e.preventDefault();
                onSelectTab?.(targetId);
                const targetIndex = tabs.indexOf(targetId);
                const target = tabBarRef.current?.querySelectorAll('[role="tab"]')[targetIndex];
                target?.focus({ preventScroll: true });
                scrollSessionTabIntoView(target?.closest('.session-tab'));
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
            {unreadLabel && (
              <span className="ra-unread-badge" title={unreadLabel} aria-hidden="true">{unreadBadgeText}</span>
            )}
            </button>

            {/* 關閉按鈕 */}
            <button
              type="button"
              onClick={(e) => {
                e.stopPropagation();
                if (isPinned) togglePin(tabId);
                else {
                  onCloseTab?.(tabId);
                  focusSessionTabAfterClose(tabBarRef);
                }
              }}
              onMouseDown={e => e.preventDefault()}
              // tablist 的子層只能是 tab：圖示鈕僅供指標；鍵盤以 Delete 關閉、右鍵選單取消釘選。
              tabIndex={-1}
              aria-hidden="true"
              data-context-surface
              title={isPinned ? '取消釘選分頁' : '關閉分頁'}
              className={`min-w-[24px] min-h-[24px] flex items-center justify-center rounded text-ra-t5 hover:text-ra-t1 hover:bg-ra-raised transition-opacity shrink-0 ${
                isPinned || isActive ? 'opacity-80 hover:opacity-100' : 'opacity-0 group-hover/tab:opacity-100 group-focus-within/tabbar:opacity-100'
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
                {isPinned ? <path d="M6 2h4l-1 4 3 3H4l3-3-1-4Zm2 7v5" /> : <>
                  <line x1="4" y1="4" x2="12" y2="12" />
                  <line x1="12" y1="4" x2="4" y2="12" />
                </>}
              </svg>
            </button>
          </div>
        );
      })}
      </div>
      ))}

      {/* 分頁列放置位置指示線 */}
      {dropInsertIndex !== null && (
        <div
          className="session-tab-drop-indicator pointer-events-none absolute top-1 bottom-1 w-[3px] bg-violet-500 rounded-full z-30 shadow-[0_0_8px_rgba(139,92,246,0.8)]"
          style={getIndicatorStyle(dropInsertIndex)}
        />
      )}
    </div>
    <ContextMenu menu={tabs.includes(tabMenu?.sessionId) ? tabMenu : null} onClose={closeTabMenu} label="分頁操作" items={[
      { label: menuPinned ? '取消釘選分頁' : '釘選分頁', onSelect: () => togglePin(tabMenu.sessionId) },
      { separator: true },
      ...(isWideScreen && onMoveTab ? [{ label: '移到另一個窗格', disabled: groupCount === 1 && tabs.length < 2,
        reason: '至少需要兩個分頁才能分割', onSelect: () => {
          const id = tabMenu.sessionId;
          onMoveTab(id);
          requestAnimationFrame(() => Array.from(document.querySelectorAll('.session-tab'))
            .find(element => element.dataset.sessionTabId === id)?.querySelector('[role="tab"]')?.focus());
        } }, { separator: true }] : []),
      { label: '關閉分頁', disabled: menuPinned, reason: menuPinned ? '請先取消釘選' : '', onSelect: () => closeTabs([tabMenu.sessionId]) },
      { label: '關閉閒置分頁', disabled: idleTabs.length === 0, reason: idleTabs.length === 0 ? '此窗格沒有未釘選的閒置分頁' : '',
        onSelect: () => closeTabs(idleTabs) },
      { label: '關閉其他分頁', disabled: otherTabs.length === 0, onSelect: () => closeTabs(otherTabs) },
      { label: '關閉右側分頁', disabled: rightTabs.length === 0, onSelect: () => closeTabs(rightTabs) },
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
  pinnedTabs = [],
  isFocused = false,
  allSessions = [],
  groupCount = 1,
  isWideScreen = true,
  onSelectTab,
  onCloseTab,
  onTogglePin,
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
        pinnedTabs={pinnedTabs}
        isFocused={isFocused}
        allSessions={allSessions}
        isWideScreen={isWideScreen}
        onSelectTab={onSelectTab}
        onCloseTab={onCloseTab}
        onTogglePin={onTogglePin}
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

