/** Ctrl/Cmd+P 快速跳轉 session（類 VS Code Quick Open） */
function QuickSwitcher({ sessions, onSelect }) {
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState('');
  const [idx, setIdx] = useState(0);
  const listRef = useRef(null);
  const dialogRef = useRef(null);

  useEffect(() => {
    const onKey = (e) => {
      if ((e.ctrlKey || e.metaKey) && !e.shiftKey && !e.altKey && e.key.toLowerCase() === 'p') {
        e.preventDefault(); // 擋掉瀏覽器列印
        setQuery('');
        setIdx(0);
        setOpen(true); // 只開不切換（同 VS Code），Esc 才關
      }
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, []);

  const matches = open ? filterMentionSessions(sessions, null, query) : [];

  // 原生 modal：對話框語意、焦點容納、Esc 關閉與關閉後的焦點還原都由瀏覽器提供。
  // 不要加 role="dialog"／aria-modal／焦點陷阱／Esc handler（見 docs/spec/frontend-architecture.md 第 5 節）。
  useLayoutEffect(() => {
    if (!open) return undefined;
    const dialog = dialogRef.current;
    dialog.showModal();
    return () => dialog.close();
  }, [open]);

  useEffect(() => {
    listRef.current?.children[idx]?.scrollIntoView({ block: 'nearest' });
  }, [idx, open]);

  if (!open) return null;

  // 選了會話就把焦點送到聊天輸入框（選到同一個會話時 ChatView 不會重載，沒人幫忙 focus）。
  // 這是刻意的轉移，不是還原；Esc／點空白後的還原由瀏覽器處理。
  const pick = (s) => {
    if (!s) return;
    dialogRef.current.close();
    onSelect(s);
    const el = document.querySelector('[data-chat-input]');
    if (el && el.isConnected && !el.disabled) el.focus({ preventScroll: true });
  };

  const onInputKey = (e) => {
    if (e.key === 'ArrowDown') { e.preventDefault(); setIdx((i) => Math.min(i + 1, matches.length - 1)); }
    else if (e.key === 'ArrowUp') { e.preventDefault(); setIdx((i) => Math.max(i - 1, 0)); }
    else if (e.key === 'Enter' && !e.nativeEvent.isComposing) { // 輸入法選字的 Enter 不算
      // pick 會同步把焦點移到聊天輸入框，不擋掉的話這個 Enter 的預設動作會落在輸入框上變成換行
      e.preventDefault();
      pick(matches[idx]);
    }
  };

  return (
    <dialog
      ref={dialogRef}
      className="ra-quick-switcher rounded-xl border border-slate-700 bg-slate-950/95 shadow-2xl shadow-black/50 overflow-hidden"
      aria-label="快速跳轉 Session"
      onClose={() => setOpen(false)}
      onClick={(e) => {
        // 點 backdrop 時 target 也是 <dialog>（它的 padding 與子元素間隙同理），所以測指標位置而不是 target
        if (e.target !== e.currentTarget) return;
        const r = e.currentTarget.getBoundingClientRect();
        if (e.clientX < r.left || e.clientX > r.right || e.clientY < r.top || e.clientY > r.bottom) e.currentTarget.close();
      }}
    >
      <input
        autoFocus
        value={query}
        onChange={(e) => { setQuery(e.target.value); setIdx(0); }}
        onKeyDown={onInputKey}
        placeholder="搜尋 session（名稱 / 目錄 / 分支）"
        className="w-full bg-transparent border-b border-slate-700 px-4 py-3 text-sm text-slate-100 placeholder-slate-500 focus:outline-none"
      />
      <div ref={listRef} className="max-h-[50vh] overflow-y-auto app-scroll py-1">
        {matches.length === 0 && <div className="px-4 py-3 text-xs text-slate-500">沒有符合的 session</div>}
        {matches.map((s, i) => {
          const displayAgent = sessionDisplayAgent(s);
          return (
            <button
              key={s.id}
              type="button"
              onClick={() => pick(s)}
              onMouseMove={() => setIdx(i)}
              className={`w-full flex items-center gap-2 px-4 py-2 text-left ${i === idx ? 'bg-cyan-500/15' : ''}`}
            >
              <span
                className={`inline-flex shrink-0 items-center justify-center w-[22px] h-[22px] rounded-[6px] ${getAgentBadgeClass(displayAgent, s)}`}
                title={displayAgent}
              >
                <AgentBadgeIcon agentType={displayAgent} session={s} />
              </span>
              <span className="min-w-0 truncate text-sm text-slate-100">{s.name || '未命名'}</span>
              <span className="min-w-0 flex-1 truncate font-mono text-[11px] text-slate-500" title={s.work_dir || ''}>
                {workDirGroupShortLabel(s.work_dir)}
              </span>
            </button>
          );
        })}
      </div>
    </dialog>
  );
}
