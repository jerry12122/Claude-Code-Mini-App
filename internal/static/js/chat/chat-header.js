/** Slash 命令選單：顯示於輸入框上方，鍵盤 ↑↓、Enter、點擊選取 */
function SlashCommandMenu({ items, activeIndex, onSelect }) {
  const listRef = useRef(null);
  useLayoutEffect(() => {
    const root = listRef.current;
    const el = root?.querySelector?.(`[data-slash-idx="${activeIndex}"]`);
    if (el && typeof el.scrollIntoView === 'function') {
      el.scrollIntoView({ block: 'nearest' });
    }
  }, [activeIndex, items]);
  return (
    <div
      ref={listRef}
      role="listbox"
      aria-label="Slash 命令"
      className="absolute bottom-full left-0 right-0 z-20 mb-1 max-h-48 overflow-y-auto app-scroll rounded-lg border border-gray-700 bg-gray-800/98 py-1 shadow-lg backdrop-blur-sm"
    >
      {items.map((c, i) => (
        <button
          key={c.command}
          type="button"
          role="option"
          aria-selected={i === activeIndex}
          data-slash-idx={i}
          onMouseDown={(e) => e.preventDefault()}
          onClick={() => onSelect(c.command)}
          className={`flex w-full items-start gap-2 px-3 py-2 text-left text-sm transition-colors hover:bg-gray-700/80 ${
            i === activeIndex ? 'bg-gray-700' : ''
          }`}
        >
          <span className="shrink-0 font-mono text-violet-300">{c.command}</span>
          <span className="min-w-0 flex-1 text-gray-400">{c.description}</span>
        </button>
      ))}
    </div>
  );
}

// ── 聊天畫面 ─────────────────────────────────────────────────────────────────

/** 「開啟 VSCode」「開啟目錄」icon button：呼叫伺服器端在本機開啟，僅 shell.enabled 且有 work_dir 時顯示。 */
function OpenInHostButton({ sessionId, kind }) {
  const [busy, setBusy] = useState(false);
  const isVscode = kind === 'vscode';
  const title = isVscode ? '在伺服器端開啟 VSCode' : '在伺服器端開啟目錄';
  const path = isVscode ? 'open-vscode' : 'open-folder';

  const handleClick = async () => {
    if (busy) return;
    setBusy(true);
    try {
      const res = await apiFetch(`/sessions/${sessionId}/${path}`, { method: 'POST' });
      if (!res.ok) {
        const data = await res.json().catch(() => ({}));
        showToast(data.error || '開啟失敗', { error: true });
      }
    } catch (_) {
      showToast('開啟失敗', { error: true });
    } finally {
      setBusy(false);
    }
  };

  return (
    <button
      type="button"
      onClick={handleClick}
      disabled={busy}
      title={title}
      aria-label={title}
      className="shrink-0 inline-flex items-center justify-center w-8 h-8 rounded-md text-[oklch(0.65_0.01_264)] hover:text-violet-300 hover:bg-[oklch(0.2_0.02_264)] disabled:opacity-40"
    >
      {isVscode ? (
        <svg viewBox="0 0 24 24" width="16" height="16" fill="currentColor" aria-hidden>
          <path d="M17.5 2.4l-6.8 6.4L5.6 4.6 3.3 5.8l5 5.9-5 6 2.3 1.2 5.1-4.7 6.8 6.5 3.2-1.5V4l-3.2-1.6zm-.6 5.1v9l-4.6-4.4 4.6-4.6zM12 12l-4.4 4.2-1.5-.8 3.5-3.4-3.5-3.4 1.5-.8L12 12z"/>
        </svg>
      ) : (
        <svg viewBox="0 0 24 24" width="16" height="16" fill="currentColor" aria-hidden>
          <path d="M10 4H4a2 2 0 0 0-2 2v12a2 2 0 0 0 2 2h16a2 2 0 0 0 2-2V8a2 2 0 0 0-2-2h-8l-2-2z"/>
        </svg>
      )}
    </button>
  );
}

/** 擁有者頂欄的「分享」按鈕：開啟分享設定彈窗。 */
function ShareButton({ onClick }) {
  return (
    <button
      type="button"
      onClick={onClick}
      title="分享這個聊天室"
      aria-label="分享這個聊天室"
      className="shrink-0 inline-flex items-center gap-1 h-8 px-2 rounded-md text-xs text-[oklch(0.7_0.01_264)] hover:text-violet-300 hover:bg-[oklch(0.2_0.02_264)]"
    >
      <svg viewBox="0 0 24 24" width="15" height="15" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden>
        <circle cx="18" cy="5" r="3" /><circle cx="6" cy="12" r="3" /><circle cx="18" cy="19" r="3" />
        <path d="m8.6 13.5 6.8 4M15.4 6.5l-6.8 4" />
      </svg>
      分享
    </button>
  );
}

/** 訪客頂欄的身分角標：顯示自己的暱稱與權限（唯讀快照／唯讀／可編輯）。 */
function GuestRoleChip({ guest }) {
  const label = guest.mode === 'snapshot' ? '唯讀快照' : guest.role === 'editor' ? '可編輯' : '唯讀';
  return (
    <span className="shrink-0 inline-flex items-center gap-1 text-[11px] font-semibold px-2 py-1 rounded-md bg-violet-500/15 text-violet-200" title={`你的暱稱：${guest.nickname}`}>
      {guest.nickname} · {label}
    </span>
  );
}

function ChatSessionHeader({
  session,
  showBack,
  onBack,
  agentType,
  state,
  activityHint,
  sessionModel,
  quota,
  quotaRefreshing,
  onQuotaRefresh,
  guest = null,
  onShare = null,
  isSplit = false,
  onToggleSplit = null,
  splitOrientation = 'horizontal',
  onToggleSplitOrientation = null,
  isSecondaryPane = false,
  onClosePane = null,
}) {
  const isGuest = !!guest;
  const displayAgent = sessionDisplayAgent(session) || agentType;
  const { shellEnabled } = useServerConfig();
  const showOpenButtons = !isGuest && shellEnabled && !!(session.work_dir && String(session.work_dir).trim());
  const repoLabel = workDirGroupShortLabel(session.work_dir);
  const modelLabel = (sessionModel && typeof sessionModel === 'object')
    ? (sessionModel.display_text || '')
    : (sessionModel || '');
  const modelOk = modelLabel && modelLabel !== '—';
  const quotaText = !isGuest && quota && quota.display_text && quota.display_text !== '—' ? quota.display_text : '';
  const quotaBars = !isGuest && !!resolveQuotaBars(quota, agentType);
  const subParts = [
    repoLabel !== '（未設定工作目錄）' ? repoLabel : null,
    modelOk ? modelLabel : null,
  ].filter(Boolean);
  const subLine = subParts.join(' · ');

  return (
    <div
      className="shrink-0 border-b border-[oklch(0.26_0.02_264)] bg-[oklch(0.15_0.02_264)]"
      style={{ paddingTop: 'calc(0.5rem + env(safe-area-inset-top) + var(--tg-content-safe-top))' }}
    >
      {/* 手機：標題列 + badge 列（設計 2b） */}
      <div className="sm:hidden px-4 pt-2 pb-3">
        <div className="flex items-center gap-2.5 min-w-0">
          {showBack && !isGuest && (
            <button type="button" onClick={onBack}
              className="shrink-0 text-[oklch(0.7_0.01_264)] hover:text-[oklch(0.94_0.01_264)] text-lg leading-none px-0.5">
              ←
            </button>
          )}
          <div className="min-w-0 flex-1 font-bold text-base text-[oklch(0.94_0.01_264)] truncate" title={session.name}>
            {session.name || '未命名'}
          </div>
          <SessionStateChip state={state} activityHint={activityHint} className="shrink-0" />
          <GitBranchBadge branch={session.git_branch} compact />
          {isGuest && <GuestRoleChip guest={guest} />}
          {!isGuest && onShare && <ShareButton onClick={onShare} />}
        </div>
        <div className="flex items-center gap-1.5 mt-2.5 min-w-0 overflow-x-auto [scrollbar-width:none] [&::-webkit-scrollbar]:hidden flex-wrap">
          <span className={`inline-flex items-center gap-1 shrink-0 text-[11.5px] font-semibold px-2 py-1 rounded-md ${getAgentBadgeClass(displayAgent, session)}`}>
            <AgentBadgeIcon agentType={displayAgent} session={session} />
            {AGENT_LABEL[displayAgent] || displayAgent}
          </span>
          {!isGuest && <QuotaBadge quota={quota} compact agentType={agentType} onRefresh={onQuotaRefresh} refreshing={quotaRefreshing} />}
        </div>
      </div>

      {/* 桌面：Row 1 標題＋分支（左）｜ 操作控制（右）。Row 2 只放細的 repo/model/quota 副標，不跟控制項搶版面。 */}
      <div className="hidden sm:block px-7 py-4">
        <div className="flex items-center justify-between gap-x-4 gap-y-2 flex-wrap">
          <div className="flex items-center gap-2 min-w-0">
            <div className="text-base font-bold text-[oklch(0.94_0.01_264)] truncate" title={session.name}>
              {session.name || '未命名'}
            </div>
            <SessionStateChip state={state} activityHint={activityHint} className="shrink-0" />
            <GitBranchBadge branch={session.git_branch} compact />
            {isGuest && <GuestRoleChip guest={guest} />}
          </div>
          <div className="flex items-center gap-2 flex-wrap justify-end">
            {!isGuest && isSplit && onToggleSplitOrientation && (
              <SplitOrientationButton
                orientation={splitOrientation}
                onClick={onToggleSplitOrientation}
              />
            )}
            {!isGuest && isSecondaryPane && onClosePane && (
              <button
                type="button"
                onClick={onClosePane}
                title="關閉副欄"
                aria-label="關閉副欄"
                className="shrink-0 inline-flex items-center justify-center w-8 h-8 rounded-md text-[oklch(0.7_0.01_264)] hover:text-red-400 hover:bg-red-500/15 transition-colors"
              >
                <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden>
                  <line x1="18" y1="6" x2="6" y2="18" />
                  <line x1="6" y1="6" x2="18" y2="18" />
                </svg>
              </button>
            )}
            {!isGuest && onShare && <ShareButton onClick={onShare} />}
            {showOpenButtons ? (
              <div className="flex items-center gap-0.5 pr-1 border-r border-[oklch(0.26_0.02_264)]">
                <OpenInHostButton sessionId={session.id} kind="vscode" />
                <OpenInHostButton sessionId={session.id} kind="folder" />
              </div>
            ) : null}
          </div>
        </div>
        <div className="flex items-center gap-2 mt-1.5 min-w-0 flex-wrap">
          {subLine ? (
            <span className="text-xs text-[oklch(0.55_0.01_264)] ra-mono truncate">{subLine}</span>
          ) : null}
          {quotaBars ? (
            <span className="text-xs ra-mono shrink-0">
              {subLine ? <span className="text-gray-500">· </span> : null}
              <QuotaUsage quota={quota} agentType={agentType} />
            </span>
          ) : quotaText ? (
            <span className="text-xs ra-mono truncate">
              {subLine ? <span className="text-gray-500">· </span> : null}
              <QuotaSegments quota={quota} text={quotaText} />
            </span>
          ) : null}
          {quotaBars || quotaText ? (
            <button
              type="button"
              onClick={onQuotaRefresh}
              disabled={quotaRefreshing}
              className={'text-xs ra-mono ' + (quotaRefreshing ? 'text-violet-400 animate-spin' : 'text-[oklch(0.55_0.01_264)] hover:text-violet-300')}
              title="刷新帳戶用量"
              aria-label="刷新帳戶用量"
            >
              ↻
            </button>
          ) : null}
        </div>
      </div>
    </div>
  );
}
