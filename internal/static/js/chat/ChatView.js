const EMPTY_SUGGESTIONS = ['看一下目前 git 狀態與最近的變更', '概覽這個專案的結構', '跑測試並整理失敗原因'];

function LiveActivity({ activity, state, connected, runStartedAt }) {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const timer = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(timer);
  }, []);
  const elapsed = runStartedAt == null ? null : Math.max(0, Math.floor((now - runStartedAt) / 1000));
  const label = !connected ? '連線中斷，等待重連' : activity?.kind === 'thinking' ? '思考中' : activity?.kind === 'tool' ? '執行中' : state === 'STREAMING' ? '正在撰寫回覆' : '處理中';
  const text = connected ? (activity?.content || '').trim() : '';
  // ponytail: 只顯示目前快照；思考預覽取尾端，讓新進度持續可見。
  const preview = activity?.kind === 'thinking' ? text.slice(-180) : text;
  return (
    <section aria-label="即時執行狀態" className="shrink-0 bg-ra-bg px-4 sm:px-7 pt-3 pb-2 text-xs text-ra-t3">
      <div className="flex items-center gap-2 min-w-0">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round"
          className={`h-3.5 w-3.5 shrink-0 text-ra-accent-text ${connected ? 'animate-spin motion-reduce:animate-none' : ''}`} aria-hidden="true">
          <path d="M21 12a9 9 0 1 1-6.22-8.56" />
        </svg>
        <span role="status" className="min-w-0 truncate font-medium">{label}</span>
        {elapsed != null && <span role="timer" aria-live="off" className="ml-auto shrink-0 tabular-nums text-ra-t4" title="任務開始後的經過時間（包含等待授權）">{elapsed} 秒</span>}
      </div>
      {text && (
        <details className="group mt-1 ml-[22px] min-w-0">
          <summary aria-label="展開或收起目前活動詳情" className="flex cursor-pointer list-none items-center gap-2 rounded text-ra-t4 focus:outline-none focus-visible:ring-2 focus-visible:ring-violet-400 [&::-webkit-details-marker]:hidden">
            <span className="min-w-0 flex-1 truncate group-open:hidden">{preview}</span>
            <span className="hidden flex-1 group-open:inline">收起詳細內容</span>
            <span className="shrink-0 group-open:rotate-180" aria-hidden="true">⌄</span>
          </summary>
          <pre className="ra-mono mt-2 max-h-32 overflow-auto app-scroll whitespace-pre-wrap break-words text-xs leading-relaxed text-ra-t3">{text}</pre>
        </details>
      )}
    </section>
  );
}

/**
 * Telegram 較早訊息的頂部控制：按需載入（不自動連抓）、載入中、可重試的錯誤、到頂停止。
 * 429 時在 retryAt 前停用並於到期後自動恢復。
 */
function TelegramOlderControl({ paging, onLoad }) {
  const [, setTick] = useState(0);
  const wait = paging.retryAt ? paging.retryAt - Date.now() : 0;
  useEffect(() => {
    if (!(wait > 0)) return undefined;
    const timer = setTimeout(() => setTick((n) => n + 1), wait + 50);
    return () => clearTimeout(timer);
  }, [paging.retryAt]);
  if (!paging.needsInit && paging.hasMore === false) {
    return <div role="status" data-older-state="done" className="shrink-0 text-center text-xs text-ra-t5">已載入最早的訊息</div>;
  }
  const blocked = wait > 0;
  const label = paging.loading ? '載入中…' : blocked ? '請稍候再試' : paging.error ? '重試' : paging.needsInit ? '重新載入' : '載入較早訊息';
  return (
    <div data-older-control className="shrink-0 flex flex-col items-center gap-1.5">
      {paging.error && <div role="alert" data-older-error className="text-xs text-red-300">{paging.error.message}</div>}
      <button
        type="button"
        onClick={onLoad}
        disabled={paging.loading || blocked}
        aria-busy={paging.loading}
        data-older-state={paging.loading ? 'loading' : paging.error ? 'error' : 'idle'}
        className="rounded-full border border-ra-line-strong bg-ra-raised px-3 py-1 text-xs text-ra-t2 hover:bg-ra-raised disabled:opacity-60 disabled:cursor-default focus:outline-none focus-visible:ring-2 focus-visible:ring-violet-400"
      >
        {label}
      </button>
    </div>
  );
}

function QuestionCard({ question, error, canAct, connected, send }) {
  const [answers, setAnswers] = useState(() => question.questions.map(() => ({ selected: [], text: '' })));
  const [submitting, setSubmitting] = useState(false);
  const [localError, setLocalError] = useState('');
  const remoteError = error?.request_id === question.request_id ? error.message : '';
  useEffect(() => { setSubmitting(false); }, [error, connected, question]);
  const update = (i, value) => setAnswers(prev => prev.map((answer, n) => n === i ? value : answer));
  const complete = answers.every(a => a.selected.length > 0 || a.text.trim());
  const disabled = !canAct || !connected || submitting;
  const submit = (event) => {
    event.preventDefault();
    if (disabled || !complete) return;
    setLocalError('');
    if (send({ type: 'question_answer', request_id: question.request_id, answers })) setSubmitting(true);
    else setLocalError('連線中斷，請重連後再送出');
  };
  return (
    <form onSubmit={submit} className="rounded-xl border border-blue-700 bg-blue-950/30 px-4 py-3 text-sm">
      <div className="text-blue-300 font-semibold mb-3">Claude 需要你的回答</div>
      {question.questions.map((q, i) => (
        <fieldset key={i} disabled={disabled} className="mb-4 min-w-0">
          <legend className="font-medium text-gray-100 mb-2 whitespace-pre-wrap">{q.question} <span className="text-xs text-gray-400">（{q.multiSelect ? '可複選' : '單選'}）</span></legend>
          {q.options.map((option, n) => (
            <label key={n} className="flex items-start gap-2 p-2 rounded hover:bg-blue-900/30 cursor-pointer">
              <input type={q.multiSelect ? 'checkbox' : 'radio'} name={`${question.request_id}-${i}`}
                checked={answers[i].selected.includes(option.label)} className="mt-1"
                onChange={event => update(i, { text: q.multiSelect ? answers[i].text : '', selected: q.multiSelect
                  ? event.target.checked ? [...answers[i].selected, option.label] : answers[i].selected.filter(v => v !== option.label)
                  : [option.label] })} />
              <span className="min-w-0 break-words"><span className="text-gray-100">{option.label}</span>
                {option.description && <span className="block text-xs text-gray-400 whitespace-pre-wrap">{option.description}</span>}
                {option.preview && <pre className="mt-1 text-xs whitespace-pre-wrap break-words">{option.preview}</pre>}
              </span>
            </label>
          ))}
          <label className="block mt-2 text-xs text-gray-400">其他回答{q.multiSelect ? '（可補充）' : ''}
            <textarea value={answers[i].text} maxLength={16000} rows={2}
              onChange={event => update(i, { selected: q.multiSelect ? answers[i].selected : [], text: event.target.value })}
              className="block mt-1 w-full rounded bg-gray-900 border border-gray-600 p-2 text-sm text-gray-100" />
          </label>
        </fieldset>
      ))}
      {(remoteError || localError) && <div role="alert" className="text-red-300 mb-2">{remoteError || localError}</div>}
      {!canAct ? <p className="text-gray-400 text-xs">等待有權限的人回答…</p> : (
        <button type="submit" disabled={disabled || !complete} className="px-3 py-2 bg-blue-700 hover:bg-blue-600 text-white rounded-lg disabled:opacity-40">
          {submitting ? '送出中…' : '送出回答'}
        </button>
      )}
    </form>
  );
}

function ChatView({ session, onBack, showBack = true, fullHeight = true, usePermModeDropdown = false, onJumpToSession, allSessions = [], guest = null, onOpenComposer, handoffInfo = null, onHandoffChange, isSplit = false, onToggleSplit = null, splitOrientation = 'horizontal', onToggleSplitOrientation = null, isSecondaryPane = false, onClosePane = null, isFocused = true }) {
  const jumpToSession = typeof onJumpToSession === 'function' ? onJumpToSession : () => {};
  const agentType = session.agent_type || 'claude';
  const agentReadOnly = agentType === 'kiro';
  const displayAgent = sessionDisplayAgent(session) || agentType;
  // Claude / Cursor / Antigravity 皆支援 mode 切換（Codex / Telegram 暫無對應概念）
  const showPermModeSelect = agentType !== 'codex' && agentType !== 'kiro' && agentType !== 'telegram';
  const showEffortSelect = agentType !== 'cursor' && agentType !== 'telegram';
  const chatScrollRef = useRef(null);
  const {
    messages,
    state,
    permTools, setPermTools, permRetryTools,
    question, questionError,
    mode, setMode,
    modelSel, setModelSel,
    effortSel, setEffortSel,
    histLoaded,
    tgPaging, historyPaging, loadOlder,
    connected,
    inputMode, setInputMode,
    shellType,
    shellPendingCmd, setShellPendingCmd,
    shellRequest,
    quota, quotaRefreshing,
    sessionModel,
    liveActivity,
    runStartedAt,
    queue, queuePaused, lingering,
    send,
    submitInput,
    flushPendingModes,
    handleQuotaRefresh,
    commitPermMode,
    online,
    shareEnded,
  } = useChatSocket({ session, agentType, showPermModeSelect, showEffortSelect, guest, onHandoffChange, isFocused, scrollRef: chatScrollRef });

  // ── 分享聊天室 ──
  // guest 為 null＝擁有者；否則是訪客（viewer 唯讀、editor 可輸入、snapshot 只能看歷史）。
  const isGuest = !!guest;
  const isSnapshot = isGuest && guest.mode === 'snapshot';
  const guestReadOnly = isGuest && (guest.role !== 'editor' || isSnapshot);
  const canAct = !guestReadOnly && !agentReadOnly; // 可以回應授權／Shell 確認等操作的人
  const [shareOpen, setShareOpen] = useState(false);
  const [toolsOpen, setToolsOpen] = useState(false);
  const toolsSessionRef = useRef(session.id);
  const updateToolsOpen = (open) => { toolsSessionRef.current = session.id; setToolsOpen(open); };
  const shareRemainingMs = useShareRemaining(isGuest ? guest.expires_at : null);
  const shareOver = isGuest && (!!shareEnded || (shareRemainingMs != null && shareRemainingMs <= 0));
  // 斷線超過 1.5 秒才顯示橫幅，切換 session 時的初次連線不閃一下；snapshot 沒有 WS、分享已結束也不顯示。
  const wantOfflineBar = !connected && !isSnapshot && !shareOver;
  const [offlineShown, setOfflineShown] = useState(false);
  useEffect(() => {
    if (!wantOfflineBar) { setOfflineShown(false); return undefined; }
    const t = setTimeout(() => setOfflineShown(true), 1500);
    return () => clearTimeout(t);
  }, [wantOfflineBar]);

  const {
    input, setInput, mentionChips, setMentionChips,
    attachments, updateAttachments, attachmentItemsRef, uploadControllersRef,
    composerSessionRef, pendingSendRef, pendingInput, sending, sendComposerInput,
    draftRestoring, draftSaving, draftSaveError, draftRestoreFailed, saveFiles, retryDraftFiles,
  } = useChatComposer(session.id, submitInput);
  const [quoteSelection, setQuoteSelection] = useState(null);
  const [lightboxSrc, setLightboxSrc] = useState(null);
  const [forwardModal, setForwardModal] = useState(null);
  const [forwardHints, setForwardHints] = useState({});
  // 長按選單的狀態放在 MessageActionsHost，這裡只拿開啟函式，避免長按時整串訊息重繪。
  const openMsgSheetRef = useRef(null);
  const handleLongPress = useCallback((payload) => {
    if (openMsgSheetRef.current) openMsgSheetRef.current(payload);
  }, []);
  const bindLongPress = useLongPress(handleLongPress);
  const [slashMenuItems, setSlashMenuItems] = useState([]);
  const [slashActiveIdx, setSlashActiveIdx] = useState(0);
  const [mentionOpen, setMentionOpen] = useState(false);
  const [mentionItems, setMentionItems] = useState([]);
  const [mentionActiveIdx, setMentionActiveIdx] = useState(0);
  // 已送出優雅停止：只驅動呼吸燈與「再按一次強制停止」文案，不鎖按鈕。任務停或切換 session 時重設。
  const [interrupting, setInterrupting] = useState(false);
  const interruptSentAtRef = useRef(0);
  const [shellConfirmPending, setShellConfirmPending] = useState(false);
  const [dragOver, setDragOver] = useState(false);
  const chatRootRef = useRef(null);
  const dragDepthRef = useRef(0);
  const bottomRef = useRef(null);
  const chatNearBottomRef = useRef(true);
  const [showJumpLatest, setShowJumpLatest] = useState(false);
  const [showAllMessages, setShowAllMessages] = useState(false);
  // Telegram 時間線：閱讀舊內容時的可見訊息錨點、到頂手勢、新訊息提示。
  const isTGChat = agentType === 'telegram';
  const gestureRef = useRef(false);
  const sendFollowUntilRef = useRef(0); // 本機使用者剛明確送出：待傳項目出現時才強制到底（與跨裝置 Out 區分）
  const lastKeyRef = useRef(null);
  const [hasNewBelow, setHasNewBelow] = useState(false);
  const chatInputRef = useRef(null);
  const focusedPaneRef = useRef(isFocused);
  focusedPaneRef.current = isFocused;
  const slashInputWrapRef = useRef(null);
  const composerWrapRef = useRef(null);

  const syncChatNearBottom = useCallback(() => {
    setQuoteSelection(null);
    const el = chatScrollRef.current;
    if (!el) return;
    const gap = el.scrollHeight - el.scrollTop - el.clientHeight;
    chatNearBottomRef.current = gap <= 2;
    setShowJumpLatest(gap >= 200);
  }, []);

  const jumpToLatest = () => {
    chatNearBottomRef.current = true;
    setHasNewBelow(false);
    bottomRef.current?.scrollIntoView({ behavior: messages.length > 200 && !showAllMessages ? 'auto' : 'smooth' });
  };

  const handleLoadOlder = useCallback(() => {
    chatNearBottomRef.current = false; // 使用者要看舊內容：補頁後不要把視圖拉回底部
    Promise.resolve(loadOlder()).then(() => requestAnimationFrame(() => requestAnimationFrame(syncChatNearBottom)));
  }, [loadOlder, syncChatNearBottom]);

  const markGesture = (event) => {
    gestureRef.current = true;
    if (event.type !== 'wheel' || event.deltaY < 0) {
      chatNearBottomRef.current = false;
      sendFollowUntilRef.current = 0;
      // Stop an in-flight scrollIntoView animation before it can undo the user's scroll.
      const scroll = chatScrollRef.current;
      if (scroll) scroll.scrollTo({ top: scroll.scrollTop, behavior: 'instant' });
    }
  };

  const handleChatScroll = useCallback(() => {
    syncChatNearBottom();
    if (!isTGChat) return;
    if (chatNearBottomRef.current) setHasNewBelow(false);
    const el = chatScrollRef.current;
    // 到頂且是使用者手勢才取一頁；補頁後需要新的手勢才取下一頁（不因 sentinel 一直可見而連抓）。
    if (el && el.scrollTop <= 40 && gestureRef.current) {
      gestureRef.current = false;
      handleLoadOlder();
    }
  }, [isTGChat, syncChatNearBottom, handleLoadOlder]);

  /** 進入會話或使用者明確操作時聚焦；不改變既有游標或其他控制項的焦點。 */
  const focusChatInput = useCallback((automatic = false) => {
    requestAnimationFrame(() => {
      requestAnimationFrame(() => {
        const el = chatInputRef.current;
        if (!el || el.disabled || document.hidden || composerSessionRef.current !== session.id) return;
        const active = document.activeElement;
        if (active === el) return;
        if (automatic) {
          if (window.matchMedia('(max-width: 639px)').matches) return;
          if (!focusedPaneRef.current || !chatNearBottomRef.current || window.getSelection()?.isCollapsed === false) return;
          if (active && active !== document.body && active !== document.documentElement) return;
        } else if (active?.closest?.('input, textarea, select, [contenteditable="true"]')) return;
        try {
          el.focus({ preventScroll: true });
        } catch (_) {
          el.focus();
        }
      });
    });
  }, [session.id]);

  /** 將游標放回輸入框並移動至文字末尾（引言插入後專用） */
  const focusChatInputToEnd = useCallback(() => {
    requestAnimationFrame(() => {
      requestAnimationFrame(() => {
        const el = chatInputRef.current;
        if (!el || el.disabled) return;
        try {
          el.focus({ preventScroll: true });
          const len = typeof el.value === 'string' ? el.value.length : 0;
          el.setSelectionRange(len, len);
        } catch (_) {
          el.focus();
        }
      });
    });
  }, []);

  /** 處理選取文字引言回覆：格式化為 Markdown 引用、注入輸入框、聚焦並清除瀏覽器選取 */
  const handleQuoteReply = useCallback(() => {
    if (!quoteSelection || !quoteSelection.text) return;
    const textToQuote = quoteSelection.text;
    setInput((prev) => injectMarkdownQuote(prev, textToQuote));
    setQuoteSelection(null);
    try {
      const sel = window.getSelection();
      if (sel) sel.removeAllRanges();
    } catch (_) {}
    focusChatInputToEnd();
  }, [quoteSelection, focusChatInputToEnd]);

  /** 監聽訊息捲動容器中的文字框選事件 */
  useEffect(() => {
    const checkSelection = () => {
      const sel = window.getSelection();
      const detected = typeof detectMessageSelection === 'function'
        ? detectMessageSelection(sel, chatScrollRef.current)
        : null;
      setQuoteSelection(detected);
    };

    const handleMouseUp = () => {
      requestAnimationFrame(checkSelection);
    };

    const handleSelectionChange = () => {
      const sel = window.getSelection();
      if (!sel || sel.isCollapsed) {
        setQuoteSelection(null);
      }
    };

    const container = chatScrollRef.current;
    if (container) {
      container.addEventListener('mouseup', handleMouseUp);
      container.addEventListener('keyup', handleMouseUp);
    }
    document.addEventListener('selectionchange', handleSelectionChange);

    return () => {
      if (container) {
        container.removeEventListener('mouseup', handleMouseUp);
        container.removeEventListener('keyup', handleMouseUp);
      }
      document.removeEventListener('selectionchange', handleSelectionChange);
    };
  }, [session.id]);

  // 捲到底
  useEffect(() => {
    // 使用者往上翻舊內容時不強制拉回；自己剛送出的訊息例外
    const last = messages[messages.length - 1];
    // Telegram：只有尾端新增才跟隨／提示；向上補頁、送達狀態變化不得把視圖拉到底部。
    const lastKey = last ? (last.id != null ? `m-${last.id}` : `i-${messages.length}`) : null;
    const appended = lastKey !== lastKeyRef.current;
    // Telegram 不分方向（入站、他裝置送出的 Out 都一樣）：只有已在底部才跟隨，否則給提示。
    // 例外只有本機使用者剛明確送出（待傳項目帶 client_message_id）才強制到底。
    const ownSend = isTGChat && appended && !!last?.clientMessageId && Date.now() < sendFollowUntilRef.current;
    if (ownSend) { sendFollowUntilRef.current = 0; chatNearBottomRef.current = true; }
    if (isTGChat) {
      if (appended && lastKeyRef.current != null && !chatNearBottomRef.current) setHasNewBelow(true);
    }
    lastKeyRef.current = lastKey;
    if (chatNearBottomRef.current && (!isTGChat || appended)) {
      bottomRef.current?.scrollIntoView({ behavior: messages.length > 200 && !showAllMessages ? 'auto' : 'smooth' });
    }
    let id2;
    const id1 = requestAnimationFrame(() => {
      id2 = requestAnimationFrame(() => {
        syncChatNearBottom();
      });
    });
    return () => {
      cancelAnimationFrame(id1);
      if (id2 != null) cancelAnimationFrame(id2);
    };
  }, [messages, syncChatNearBottom]);

  // 待授權／Shell 確認區塊出現時捲到底（此狀態常不經 messages 更新而單獨出現）
  useEffect(() => {
    const showPermPanel =
      (state === 'AWAITING_CONFIRM' && permTools.length > 0) || !!question ||
      (state === 'SHELL_AWAITING_APPROVAL' && shellPendingCmd) ||
      (state === 'AWAITING_SHELL_CONFIRM' && shellRequest);
    if (!showPermPanel || !chatNearBottomRef.current) return;
    let id2;
    const id1 = requestAnimationFrame(() => {
      id2 = requestAnimationFrame(() => {
        if (chatNearBottomRef.current) bottomRef.current?.scrollIntoView({ behavior: messages.length > 200 && !showAllMessages ? 'auto' : 'smooth' });
      });
    });
    return () => {
      cancelAnimationFrame(id1);
      if (id2 != null) cancelAnimationFrame(id2);
    };
  }, [state, permTools.length, question, shellPendingCmd, shellRequest]);

  // 離開頁面確認（任務進行中或等待授權時）
  useEffect(() => {
    const handler = (e) => {
      if (state === 'THINKING' || state === 'STREAMING' || state === 'AWAITING_CONFIRM' || state === 'AWAITING_INPUT' || state === 'SHELL_RUNNING' || state === 'SHELL_AWAITING_APPROVAL' || state === 'AWAITING_SHELL_CONFIRM' || state === 'SHELL_EXEC') {
        e.preventDefault();
        e.returnValue = '';
      }
    };
    window.addEventListener('beforeunload', handler);
    return () => window.removeEventListener('beforeunload', handler);
  }, [state]);

  // session 切換時重置輸入框草稿與 composer 選單（訊息／連線狀態由 useChatSocket 自行重置）
  useEffect(() => {
    chatNearBottomRef.current = true;
    setShowJumpLatest(false);
    setShowAllMessages(false);
    setHasNewBelow(false);
    lastKeyRef.current = null;
    gestureRef.current = false;
    setSlashMenuItems([]);
    setSlashActiveIdx(0);
    setMentionOpen(false);
    setMentionItems([]);
    setMentionActiveIdx(0);
    setInterrupting(false);
    interruptSentAtRef.current = 0;
    setShellConfirmPending(false);
    setLightboxSrc(null);
    setDragOver(false);
    dragDepthRef.current = 0;
  }, [session.id]);

  // 歷史載入完成＝畫面就緒，將 focus 預設在輸入框
  useEffect(() => {
    if (!histLoaded || draftRestoring || !chatNearBottomRef.current) return;
    focusChatInput(true);
  }, [histLoaded, session.id, focusChatInput, draftRestoring]);

  // 切換會話、歷史載入完成：直接跳到最底。平滑捲動在長歷史上會被後續排版（圖片、程式碼上色）撐高而停在半路，
  // 所以用瞬間跳轉，並在排版穩定後（下一幀、150ms）再補跳一次。
  useEffect(() => {
    if (!histLoaded) return;
    const jump = () => {
      if (!chatNearBottomRef.current) return;
      const el = chatScrollRef.current;
      if (el) el.scrollTop = el.scrollHeight;
      chatNearBottomRef.current = true;
      setShowJumpLatest(false);
    };
    jump();
    const raf = requestAnimationFrame(jump);
    const timer = setTimeout(jump, 150);
    return () => {
      cancelAnimationFrame(raf);
      clearTimeout(timer);
    };
  }, [histLoaded, session.id]);

  // 監聽 Mermaid 圖表放大事件，傳入燈箱放大顯示
  useEffect(() => {
    const handleMermaidZoom = (e) => {
      // 避免 SplitLayout（左右分割視窗）時兩側 ChatView 同時響應全域事件，導致開啟雙層燈箱（需按兩次關閉）
      if (chatRootRef.current && e.target instanceof Node && !chatRootRef.current.contains(e.target)) {
        return;
      }
      if (e.detail && (e.detail.svg || e.detail.src)) {
        setLightboxSrc({ svg: e.detail.svg, src: e.detail.src });
      }
    };
    document.addEventListener('ra-mermaid-zoom', handleMermaidZoom);
    return () => {
      document.removeEventListener('ra-mermaid-zoom', handleMermaidZoom);
    };
  }, []);

  // code block 複製鍵、圖片點擊放大：dangerouslySetInnerHTML 插入的 DOM 沒有 React 事件，
  // 用外層 onClick 事件委派抓 .code-copy-btn / .chat-img（冒泡到這裡才處理，不影響一般點擊/選字）。
  const handleProseClick = (e) => {
    const img = e.target.closest ? e.target.closest('.chat-img') : null;
    if (img) {
      e.stopPropagation();
      setLightboxSrc(img.getAttribute('src') || '');
      return;
    }
    const btn = e.target.closest ? e.target.closest('.code-copy-btn') : null;
    if (!btn) return;
    e.stopPropagation();
    const b64 = btn.getAttribute('data-copy-b64') || '';
    let text = '';
    try {
      text = decodeURIComponent(escape(atob(b64)));
    } catch (_) {
      return;
    }
    if (!copyTextExecCommand(text)) return;
    btn.classList.add('copied');
    const iconCopy = btn.querySelector('.icon-copy');
    const iconCheck = btn.querySelector('.icon-check');
    if (iconCopy) iconCopy.style.display = 'none';
    if (iconCheck) iconCheck.style.display = '';
    clearTimeout(btn._copyResetTimer);
    btn._copyResetTimer = setTimeout(() => {
      btn.classList.remove('copied');
      if (iconCopy) iconCopy.style.display = '';
      if (iconCheck) iconCheck.style.display = 'none';
    }, 2000);
  };

  const closeComposerMenus = () => {
    setSlashMenuItems([]);
    setMentionOpen(false);
    setMentionItems([]);
    setMentionActiveIdx(0);
  };

  const syncComposerMenus = (value, cursor, mode) => {
    if (mode === 'shell') {
      closeComposerMenus();
      return;
    }
    if (String(value || '').startsWith('/')) {
      const q = String(value).toLowerCase();
      const filtered = SLASH_COMMANDS.filter(
        (c) => (!c.modes || c.modes.includes(mode)) && c.command.toLowerCase().startsWith(q)
      );
      setSlashMenuItems(filtered);
      setSlashActiveIdx(0);
      setMentionOpen(false);
      setMentionItems([]);
      return;
    }
    setSlashMenuItems([]);
    const hit = mentionQueryAtCursor(value, cursor);
    if (!hit || isGuest) { // 訪客看不到其他 session，不提供 @ 標記

      setMentionOpen(false);
      setMentionItems([]);
      return;
    }
    const excludeIds = mentionChips.map((s) => s.id);
    const filtered = filterMentionSessions(allSessions, session.id, hit.query, excludeIds);
    setMentionOpen(true);
    setMentionItems(filtered);
    setMentionActiveIdx(0);
  };

  const handleMentionSelect = (s) => {
    const el = chatInputRef.current;
    const cursor = el ? el.selectionStart : input.length;
    const next = consumeMentionQuery(input, cursor);
    setInput(next.text);
    try {
      localStorage.setItem(draftInputStorageKey(session.id), next.text);
    } catch (_) {}
    setMentionChips((prev) => (prev.some((x) => x.id === s.id) ? prev : [...prev, s]));
    setMentionOpen(false);
    setMentionItems([]);
    requestAnimationFrame(() => {
      const ta = chatInputRef.current;
      if (!ta) return;
      ta.focus();
      ta.setSelectionRange(next.cursor, next.cursor);
    });
  };

  const handleMentionChipRemove = (id) => {
    setMentionChips((prev) => prev.filter((s) => s.id !== id));
  };

  // ── 訊息長按選單的動作（開關與複製由 MessageActionsHost 處理） ──
  const handleSheetForward = (p) => {
    setForwardModal({ messageKey: p.messageKey, messageContent: p.text });
  };
  // WS 斷線時 send 會回 false；不能讓操作靜默消失（面板被清掉、輸入被清空卻什麼都沒送出）。
  const sendOrToast = (obj) => {
    if (send(obj)) return true;
    showToast('連線中斷，操作未送出，請稍後再試', { error: true, duration: 3000 });
    return false;
  };
  const flushOrToast = () => {
    if (flushPendingModes()) return true;
    showToast('連線中斷，操作未送出，請稍後再試', { error: true, duration: 3000 });
    return false;
  };
  const handleSend = async (overrideText) => {
    if (pendingInput || pendingSendRef.current || isDisabled) return;
    const raw = overrideText !== undefined && overrideText !== null ? String(overrideText) : input;
    const trimmed = raw.trim();
    const selected = inputMode === 'shell' ? [] : attachmentItemsRef.current;
    if ((!trimmed && selected.length === 0) || selected.some(a => a.status !== 'ready')) return;
    const sizeError = socketPayloadError({ type: inputMode === 'shell' ? 'shell_exec' : 'input', data: expandMentionPrompt(trimmed, session, mentionChips), attachment_ids:selected.map(a=>a.attachmentId), request_id:'00000000-0000-0000-0000-000000000000' });
    if (sizeError) { showToast(sizeError, {error:true,duration:5000}); return; }
    if (trimmed === '/reset' || trimmed === '/clear') {
      if (state !== 'IDLE' && state !== 'SHELL_IDLE') return;
      if (!flushOrToast()) return;
      if (!sendOrToast({ type: 'reset_context' })) return;
      clearDraftInputForSession(session.id);
      setInput('');
      closeComposerMenus();
      return;
    }
    if (inputMode === 'shell') {
      if (state !== 'SHELL_IDLE' && state !== 'IDLE') return;
      if (!flushOrToast()) return;
      if (!sendOrToast({ type: 'shell_exec', data: trimmed })) return;
      clearDraftInputForSession(session.id);
      setInput('');
      closeComposerMenus();
      return;
    }
    const idle = state === 'IDLE' || state === 'SHELL_IDLE';
    if (!idle && !canQueue) return;
    // 排隊時不 flush 模式：set_mode 等會廣播 idle 狀態，執行中送會讓 UI 誤判完成。
    if (idle && !flushOrToast()) return;
    let record;
    try {
      record = beginPendingInput(session.id, {
        data: expandMentionPrompt(trimmed, session, mentionChips), text: raw,
        attachments: selected, mentions: mentionChips,
      });
    } catch (err) {
      showToast(err.message, { error: true });
      return;
    }
    setInput('');
    clearDraftInputForSession(session.id);
    updateAttachments(() => []);
    setMentionChips([]);
    closeComposerMenus();
    jumpToLatest();
    if (isTGChat) sendFollowUntilRef.current = Date.now() + 15000;
    await sendComposerInput(record);
  };

  const applySuggestion = (text) => {
    setInput(text);
    try { localStorage.setItem(draftInputStorageKey(session.id), text); } catch (_) {}
    focusChatInput();
  };

  const removeAttachment = (id) => {
    if (pendingSendRef.current) return;
    uploadControllersRef.current.get(id)?.abort();
    uploadControllersRef.current.delete(id);
    updateAttachments((prev) => {
      const hit = prev.find((a) => a.id === id);
      if (hit && hit.previewUrl) URL.revokeObjectURL(hit.previewUrl);
      return prev.filter((a) => a.id !== id);
    });
  };

  const handleSlashSelect = (command) => {
    setInput(command);
    closeComposerMenus();
    handleSend(command);
  };

  const handleKeyDown = (e) => {
    // 中文輸入法選字時的 Enter 是確認候選字，不能當成送出（keyCode 229 為舊版 WebView 的 IME 標記）
    if (e.nativeEvent.isComposing || e.keyCode === 229) return;
    const slashMenuOpenNow = slashMenuItems.length > 0;
    if (slashMenuOpenNow) {
      if (e.key === 'ArrowDown') {
        e.preventDefault();
        setSlashActiveIdx((i) => Math.min(i + 1, slashMenuItems.length - 1));
        return;
      }
      if (e.key === 'ArrowUp') {
        e.preventDefault();
        setSlashActiveIdx((i) => Math.max(i - 1, 0));
        return;
      }
      if (e.key === 'Enter' && !e.shiftKey) {
        e.preventDefault();
        const item = slashMenuItems[slashActiveIdx];
        if (item) handleSlashSelect(item.command);
        return;
      }
      if (e.key === 'Escape') {
        e.preventDefault();
        setSlashMenuItems([]);
        return;
      }
    }
    if (mentionOpen) {
      if (e.key === 'ArrowDown') {
        e.preventDefault();
        setMentionActiveIdx((i) => Math.min(i + 1, Math.max(mentionItems.length - 1, 0)));
        return;
      }
      if (e.key === 'ArrowUp') {
        e.preventDefault();
        setMentionActiveIdx((i) => Math.max(i - 1, 0));
        return;
      }
      if (e.key === 'Enter' && !e.shiftKey) {
        if (mentionItems.length > 0) {
          e.preventDefault();
          const item = mentionItems[mentionActiveIdx];
          if (item) handleMentionSelect(item);
          return;
        }
      }
      if (e.key === 'Escape') {
        e.preventDefault();
        setMentionOpen(false);
        setMentionItems([]);
        return;
      }
    }
    // 觸控裝置 Enter 換行，用送出鈕送出；桌面 Enter 送出、Shift+Enter 換行
    if (e.key === 'Enter' && !e.shiftKey && !window.matchMedia('(hover: none) and (pointer: coarse)').matches) {
      e.preventDefault();
      handleSend();
    }
  };

  const handleAllowOnce = () => {
    if (!sendOrToast({ type: 'allow_once', tools: permTools.map(t => t.tool_name) })) return;
    setPermTools([]);
  };

  const handleDenyOnce = () => {
    if (!sendOrToast({ type: 'deny_once' })) return;
    setPermTools([]);
  };

  /** 僅更新畫面選擇，送出訊息時才會送 set_mode */
  const handlePermModeDraftChange = (newMode) => {
    setMode(normalizePermMode(agentType, newMode));
  };
  /** 僅更新畫面選擇，送出訊息時才會送 set_model / set_effort */
  const handleModelDraftChange = (newModel) => setModelSel(newModel);
  const handleEffortDraftChange = (newEffort) => setEffortSel(newEffort);
  /** 授權面板「允許並記住」：須立即寫入後端並重試 */
  const handlePermModeCommitNow = (newMode) => {
    if (!commitPermMode(newMode)) {
      showToast('連線中斷，操作未送出，請稍後再試', { error: true, duration: 3000 });
      return;
    }
    setPermTools([]);
  };

  const handleInterrupt = () => {
    const now = Date.now();
    // 只擋誤觸連點；刻意再按（約 300ms 後）會送第二次 interrupt，後端才強制停止。
    if (now - interruptSentAtRef.current < 300) return;
    if (!sendOrToast({ type: 'interrupt' })) return;
    interruptSentAtRef.current = now;
    setInterrupting(true);
  };

  // 點了之後等伺服器回應：避免對 Shell 允許／拒絕連點（這三顆不會立刻收起面板）
  const sendShellConfirm = (type) => {
    if (shellConfirmPending) return;
    if (sendOrToast({ type })) setShellConfirmPending(true);
  };

  const fileInputRef = useRef(null);
  const pickerRequestRef = useRef(null);
  const uploadAllowedRef = useRef(false);
  useLayoutEffect(() => {
    pickerRequestRef.current = null;
  }, [session.id, canAct, shareOver]);
  const openAttachmentPicker = (sheet) => {
    if (!uploadAllowedRef.current || pendingSendRef.current) return;
    pickerRequestRef.current = { sessionId: session.id, mobile: !!sheet, close: sheet?.close };
    fileInputRef.current?.click();
  };
  const uploading = attachments.some(a => a.status === 'uploading');
  const uploadAttachment = async (item, sessionId) => {
    if (!attachmentItemsRef.current.some(a => a.id === item.id) || composerSessionRef.current !== sessionId) return;
    const controller = new AbortController();
    uploadControllersRef.current.set(item.id, controller);
    updateAttachments(prev => prev.map(a => a.id === item.id ? { ...a, status: 'uploading', error: '' } : a));
    try {
      if (!item.file) throw new Error('請移除附件後重新選取原檔');
      if (!item.file.size) throw new Error('檔案是空的');
      const fd = new FormData();
      fd.append('file', item.file);
      const res = await apiFetch(`/sessions/${sessionId}/uploads`, { method: 'POST', body: fd, signal: controller.signal, timeoutMs: 0 });
      const data = await res.json().catch(() => ({}));
      if (!res.ok) throw new Error(data.error || '上傳失敗');
      if (!data.attachment?.id) throw new Error('附件資訊無法讀取');
      if (controller.signal.aborted || composerSessionRef.current !== sessionId) return;
      updateAttachments(prev => prev.map(a => a.id === item.id ? { ...a, attachmentId: data.attachment.id, size: data.attachment.size, name: data.attachment.name, status: 'ready' } : a));
    } catch (err) {
      if (controller.signal.aborted || composerSessionRef.current !== sessionId) return;
      updateAttachments(prev => prev.map(a => a.id === item.id ? { ...a, status: 'error', error: err.message || '上傳失敗' } : a));
    } finally {
      if (uploadControllersRef.current.get(item.id) === controller) uploadControllersRef.current.delete(item.id);
    }
  };
  // Insert all placeholders immediately; serial uploads keep mobile memory bounded.
  const uploadFiles = async (files, picker = null) => {
    if (!files.length || pendingSendRef.current || !uploadAllowedRef.current) return;
    const sessionId = session.id;
    const items = files.map(file => ({ id: `local-${Date.now()}-${Math.random().toString(36).slice(2)}`, file, name: file.name, size: file.size, previewUrl: file.type.startsWith('image/') ? URL.createObjectURL(file) : '', status: 'uploading' }));
    updateAttachments(prev => [...prev, ...items]);
    if (picker?.mobile) picker.close();
    else focusChatInput();
    for (const item of items) await uploadAttachment(item, sessionId);
  };
  const retryAttachment = (id) => {
    if (pendingSendRef.current || !uploadAllowedRef.current) return;
    const item = attachmentItemsRef.current.find(a => a.id === id);
    if (item?.status === 'error') uploadAttachment(item, session.id);
  };
  const handleFilesPicked = (e) => {
    const files = Array.from(e.target.files || []);
    e.target.value = '';
    const picker = pickerRequestRef.current;
    pickerRequestRef.current = null;
    if (!picker || picker.sessionId !== composerSessionRef.current || !uploadAllowedRef.current) return;
    uploadFiles(files, picker);
  };
  // 貼上剪貼簿的截圖／檔案；純文字貼上不攔。截圖常沒有副檔名（後端依副檔名比白名單），依 MIME 補上。
  const handlePaste = (e) => {
    if (inputMode === 'shell') return;
    const files = Array.from(e.clipboardData?.files || []);
    if (!files.length) return;
    e.preventDefault();
    uploadFiles(files.map((f) => {
      if (/\.[a-z0-9]+$/i.test(f.name)) return f;
      const ext = (f.type.split('/')[1] || '').replace('jpeg', 'jpg').replace('plain', 'txt');
      return new File([f], `paste.${ext}`, { type: f.type });
    }));
  };

  // 桌面拖放檔案：用計數器處理子元素 enter/leave 交錯造成的閃爍
  const hasFiles = (e) => Array.from(e.dataTransfer?.types || []).includes('Files');
  const handleDragEnter = (e) => {
    if (!canAct || inputMode === 'shell' || !hasFiles(e)) return;
    e.preventDefault();
    dragDepthRef.current += 1;
    setDragOver(true);
  };
  const handleDragOver = (e) => {
    if (inputMode === 'shell' || !hasFiles(e)) return;
    e.preventDefault();
  };
  const handleDragLeave = (e) => {
    if (inputMode === 'shell' || !hasFiles(e)) return;
    dragDepthRef.current = Math.max(0, dragDepthRef.current - 1);
    if (dragDepthRef.current === 0) setDragOver(false);
  };
  const handleDrop = (e) => {
    if (inputMode === 'shell' || !hasFiles(e)) return;
    e.preventDefault();
    dragDepthRef.current = 0;
    setDragOver(false);
    uploadFiles(Array.from(e.dataTransfer.files || []));
  };

  const handleInputModeChange = (newMode) => {
    if (pendingSendRef.current) return;
    const busy = ['THINKING', 'STREAMING', 'AWAITING_CONFIRM', 'AWAITING_INPUT', 'SHELL_RUNNING', 'SHELL_AWAITING_APPROVAL', 'SHELL_EXEC', 'AWAITING_SHELL_CONFIRM'].includes(state);
    if (busy) return;
    setInputMode(newMode);
    try {
      localStorage.setItem(inputModeStorageKey(session.id), newMode);
    } catch (_) {}
    const el = chatInputRef.current;
    syncComposerMenus(input, el ? el.selectionStart : input.length, newMode);
  };

  const handleShellApprove = () => {
    if (!sendOrToast({ type: 'shell_approve' })) return;
    setShellPendingCmd(null);
  };

  const handleShellCancel = () => {
    if (!sendOrToast({ type: 'shell_cancel' })) return;
    setShellPendingCmd(null);
  };

  const openHandoffComposer = () => {
    if (!handoffInfo || typeof onOpenComposer !== 'function') return;
    const h = handoffInfo;
    const cliArgs = Array.isArray(h.cli_extra_args) ? h.cli_extra_args : [];
    // 舊 model 可能只存在 --model 引數；統一交給表單的 model 欄位處理。
    const model = h.model || extractModelFromCliArgs(cliArgs);
    onOpenComposer({
      name: `${h.old_name || ''}（接手）`,
      workDir: h.old_work_dir || '',
      agent: h.agent_type,
      mode: normalizePermMode(agentTypeForCreate(h.agent_type), h.permission_mode || 'default'),
      model,
      cliExtra: cliArgsWithoutModel(cliArgs).join('\n'),
      effort: h.effort || '',
      systemInstruction: h.system_instruction || '',
      message: buildHandoffDraftMessage(h, messages),
    });
  };

  const agentRunning = state === 'THINKING' || state === 'STREAMING';
  const timelineMessages = useMemo(() => messages.filter(m => agentType === 'telegram' || m.role !== 'claude' ||
    m.status === 'interrupted' || m.content || m.html || m.attachments?.length), [messages, agentType]);
  const taskRunning = state === 'AWAITING_INPUT' || agentRunning || lingering || state === 'SHELL_RUNNING' || state === 'SHELL_EXEC';
  // Agent 執行中／等授權時仍可送出：後端排入佇列，前一輪成功後依序執行。
  const canQueue = inputMode !== 'shell' && (agentRunning || lingering || state === 'AWAITING_CONFIRM' || state === 'AWAITING_INPUT');
  const isDisabled = !canAct || draftRestoring || (!canQueue && (state === 'THINKING' || state === 'STREAMING' || state === 'AWAITING_CONFIRM' || state === 'AWAITING_INPUT' || state === 'SHELL_RUNNING' || state === 'SHELL_AWAITING_APPROVAL' || state === 'SHELL_EXEC' || state === 'AWAITING_SHELL_CONFIRM'));
  uploadAllowedRef.current = canAct && !shareOver && !sending && !isDisabled && inputMode !== 'shell';
  const modeSwitchDisabled = ['THINKING', 'STREAMING', 'AWAITING_CONFIRM', 'AWAITING_INPUT', 'SHELL_RUNNING', 'SHELL_AWAITING_APPROVAL', 'SHELL_EXEC', 'AWAITING_SHELL_CONFIRM'].includes(state);
  const coarsePointer = window.matchMedia('(hover: none) and (pointer: coarse)').matches;
  useLayoutEffect(() => {
    if (toolsSessionRef.current !== session.id || !canAct || shareOver || !isFocused || (taskRunning && !canQueue)) setToolsOpen(false);
  }, [session.id, canAct, shareOver, isFocused, taskRunning, canQueue]);
  const slashMenuOpen = slashMenuItems.length > 0;
  const composerMenuOpen = slashMenuOpen || mentionOpen;

  useEffect(() => {
    if (isDisabled) closeComposerMenus();
  }, [isDisabled]);

  // 中斷呼吸燈：任務停下來就解除。Shell 確認：狀態或請求一變就解除，另有 10 秒保底，避免伺服器沒回應時按鈕永遠鎖住。
  useEffect(() => { if (!taskRunning) setInterrupting(false); }, [taskRunning]);
  useEffect(() => { setShellConfirmPending(false); }, [state, shellRequest]);
  useEffect(() => {
    if (!shellConfirmPending) return undefined;
    const t = setTimeout(() => setShellConfirmPending(false), 10000);
    return () => clearTimeout(t);
  }, [shellConfirmPending]);

  useEffect(() => {
    if (!composerMenuOpen) return;
    const onDocMouseDown = (e) => {
      const root = mentionOpen ? composerWrapRef.current : slashInputWrapRef.current;
      if (root && !root.contains(e.target)) {
        closeComposerMenus();
      }
    };
    document.addEventListener('mousedown', onDocMouseDown);
    return () => document.removeEventListener('mousedown', onDocMouseDown);
  }, [composerMenuOpen, mentionOpen]);

  /** 聊天輸入框：依內容動態增高；未達上限不出現卷軸，超出上限平滑啟用卷軸 */
  useLayoutEffect(() => {
    const el = chatInputRef.current;
    if (!el) return;
    el.style.height = 'auto';
    let cap = 240;
    try {
      const maxStr = getComputedStyle(el).maxHeight;
      const maxPx = parseFloat(maxStr);
      if (Number.isFinite(maxPx) && maxPx > 0) cap = maxPx;
    } catch (_) {}
    const needed = el.scrollHeight;
    if (needed > cap) {
      el.style.height = `${cap}px`;
      el.style.overflowY = 'auto';
    } else {
      el.style.height = `${needed}px`;
      el.style.overflowY = 'hidden';
    }
  }, [input]);

  useEffect(() => {
    const handleResize = () => {
      const el = chatInputRef.current;
      if (!el) return;
      el.style.height = 'auto';
      let cap = 240;
      try {
        const maxStr = getComputedStyle(el).maxHeight;
        const maxPx = parseFloat(maxStr);
        if (Number.isFinite(maxPx) && maxPx > 0) cap = maxPx;
      } catch (_) {}
      const needed = el.scrollHeight;
      if (needed > cap) {
        el.style.height = `${cap}px`;
        el.style.overflowY = 'auto';
      } else {
        el.style.height = `${needed}px`;
        el.style.overflowY = 'hidden';
      }
    };
    window.addEventListener('resize', handleResize);
    return () => window.removeEventListener('resize', handleResize);
  }, []);

  return (
    <div
      ref={chatRootRef}
      data-chat-pane={isSecondaryPane ? 'secondary' : 'primary'}
      data-chat-focused={isFocused}
      tabIndex={-1}
      className={`flex flex-col ${fullHeight ? 'h-app' : 'h-full'}`}
      onDragEnter={handleDragEnter}
      onDragOver={handleDragOver}
      onDragLeave={handleDragLeave}
      onDrop={handleDrop}
    >
      <ChatSessionHeader
        session={session}
        showBack={showBack}
        onBack={onBack}
        agentType={agentType}
        state={state}
        sessionModel={sessionModel}
        quota={quota}
        quotaRefreshing={quotaRefreshing}
        onQuotaRefresh={handleQuotaRefresh}
        guest={guest}
        onShare={isGuest || isTGChat ? null : () => setShareOpen(true)}
        isSplit={isSplit}
        onToggleSplit={onToggleSplit}
        splitOrientation={splitOrientation}
        onToggleSplitOrientation={onToggleSplitOrientation}
        isSecondaryPane={isSecondaryPane}
        onClosePane={onClosePane}
      />

      <ShareStatusBar guest={guest} online={online} remainingMs={shareRemainingMs} ended={shareOver} endReason={shareEnded} />

      {offlineShown && (
        <div role="status" className="shrink-0 bg-amber-500/10 text-amber-200 border-b border-amber-700/30 px-4 py-1.5 text-xs">
          連線中…（訊息暫時無法送出）
        </div>
      )}

      {/* 訊息列表 */}
      <div className="relative flex-1 min-h-0 flex flex-col">
      <div
        ref={chatScrollRef}
        onScroll={handleChatScroll}
        onWheel={markGesture}
        onTouchMove={markGesture}
        onPointerDown={markGesture}
        onKeyDown={markGesture}
        style={{ overflowAnchor: 'none' }} /* Telegram：錨點由上面的 JS 負責，各瀏覽器行為一致（含無原生 scroll anchoring 的 iOS WebView） */
        className="flex-1 min-h-0 overflow-y-auto app-scroll px-4 py-[18px] sm:px-8 sm:py-7 flex flex-col gap-5"
      >
        {!histLoaded && (
          <div className="mt-16 text-center text-sm text-ra-t5">載入對話中…</div>
        )}
        {isTGChat && histLoaded && tgPaging && tgPaging.syncState === 'offline' && (
          <div role="status" data-sync-state="offline" className="shrink-0 text-center text-xs text-amber-300/80">
            Telegram 未連線
          </div>
        )}
        {isTGChat && histLoaded && tgPaging && <TelegramOlderControl paging={tgPaging} onLoad={handleLoadOlder} />}
        {!isTGChat && histLoaded && historyPaging && <TelegramOlderControl paging={historyPaging} onLoad={handleLoadOlder} />}
        {histLoaded && messages.length === 0 && !(isTGChat && tgPaging && tgPaging.needsInit && tgPaging.error) && (
          <div className="ra-chat-empty mt-16 flex flex-col items-center gap-4 text-center">
            <div className="text-sm text-ra-t4">{!canAct ? '目前還沒有訊息' : '輸入指令開始對話'}</div>
            {session.work_dir ? (
              <div className="max-w-full truncate ra-mono text-xs text-ra-t5" title={session.work_dir}>
                {workDirGroupShortLabel(session.work_dir)}{session.git_branch ? ` · ${session.git_branch}` : ''}
              </div>
            ) : null}
            {inputMode !== 'shell' && !isGuest && canAct && (
              <div className="flex flex-wrap justify-center gap-2">
                {EMPTY_SUGGESTIONS.map((t) => (
                  <button key={t} type="button" onClick={() => applySuggestion(t)}
                    className="rounded-full border border-ra-line bg-ra-card px-3 py-1.5 text-xs text-ra-t2 hover:border-violet-500/50 hover:text-violet-300 transition-colors">
                    {t}
                  </button>
                ))}
              </div>
            )}
          </div>
        )}
        {messages.length > 200 && <button type="button" onClick={() => setShowAllMessages(value => !value)}
          className="shrink-0 self-center rounded-full border border-ra-line px-3 py-1 text-xs text-ra-t4">
          {showAllMessages ? '恢復精簡顯示' : '顯示全部已載入訊息（搜尋／選取用）'}
        </button>}
        <VirtualMessages key={session.id} messages={timelineMessages} scrollRef={chatScrollRef} followRef={chatNearBottomRef}
          gestureRef={gestureRef} showAll={showAllMessages}>
        {(m, i) => {
          const userParts = m.role === 'user' ? attachmentMessageParts(m, session.id) : null;
          // 收到的訊息（Telegram 朋友傳的圖片／檔案）也要顯示附件卡片；純附件時內容為空，不能當成「思考中」。
          const claudeAtts = m.role === 'claude' && Array.isArray(m.attachments) && m.attachments.length ? attachmentMessageParts(m, session.id).attachments : [];
          const msgKey = m.id != null ? String(m.id) : `idx-${i}`;
          const forwardBody =
            m.role === 'claude' && String(m.resultText || '').trim() !== ''
              ? m.resultText
              : m.role === 'user' ? attachmentMessageCopy(m, session.id) : (m.content || '').trim();
          const canForwardShellOrAgent =
            !isGuest && (m.role === 'claude' || m.role === 'shell') && !m.streaming && !!String(forwardBody || '').trim();
          // 署名：訪客訊息顯示暱稱；訪客視角下沒有署名的使用者訊息是擁有者送的。
          const authorLabel = m.role === 'user' ? (m.author || (isGuest ? '擁有者' : '')) : '';
          const showStreamingTail =
            (state === 'THINKING' || state === 'STREAMING') &&
            m.role === 'claude' &&
            i === timelineMessages.length - 1;
          const timeLabel = formatMessageTime(m.createdAt);
          // 尚無內容的訊息不啟用長按選單。
          const lpText = String(forwardBody || '').trim();
          return (
          <div
            key={m.id != null ? `m-${m.id}` : i}
            data-mkey={m.id != null ? `m-${m.id}` : `i-${i}`}
            className={`flex w-full min-w-0 ${m.role === 'user' ? 'justify-end' : 'justify-start'} ${m.role === 'claude' || m.role === 'shell' ? 'group' : ''}`}
          >
            <div
              className={`flex flex-col min-w-0 ${lpText ? 'msg-lp' : ''} ${m.role === 'user' ? 'items-end max-w-[60%]' : m.role === 'shell' ? 'items-start max-w-[85%]' : 'items-start max-w-[78%]'}`}
              tabIndex={lpText ? 0 : undefined}
              {...(lpText ? bindLongPress({
                text: lpText,
                role: m.role,
                canForward: canForwardShellOrAgent,
                canShowForward: !isGuest && (m.role === 'claude' || m.role === 'shell'),
                canQuote: canAct && !shareOver,
                streaming: !!m.streaming,
                messageKey: msgKey,
                createdAt: m.createdAt,
              }) : {})}
            >
            {m.role === 'user' ? (
              <div className="bubble-user text-white px-4 py-3 text-sm w-fit max-w-full min-w-0">
                {authorLabel && <div className="mb-1 text-[11px] font-semibold text-white/70" data-msg-author>{authorLabel}</div>}
                <MessageAttachments items={userParts.attachments} onPreview={setLightboxSrc} />
                {userParts.text && <div className="whitespace-pre-wrap break-words leading-relaxed">{userParts.text}</div>}
                {m.delivery && m.delivery !== 'sent' && (
                  <div className={`mt-1 text-[11px] ${m.delivery === 'failed' ? 'text-red-300' : 'text-white/70'}`} data-delivery={m.delivery}>
                    {m.delivery === 'pending' ? '傳送中…' : m.delivery === 'failed' ? '傳送失敗' : '送出結果不明'}{m.deliveryError ? `：${m.deliveryError}` : ''}
                  </div>
                )}
              </div>
            ) : m.role === 'shell' ? (
              <div className="bubble-shell px-4 py-3 text-sm w-fit max-w-full min-w-0">
                <div className="flex items-start gap-2 mb-1.5 min-w-0">
                  <span className="inline-flex items-center gap-1 font-mono text-amber-500/80 text-xs">
                    <span>&gt;_</span>
                    <span>{shellType || 'shell'}</span>
                  </span>
                </div>
                <ShellOutput content={m.content || ''} exitCode={m.exitCode} streaming={m.streaming} />
                {forwardHints[msgKey] ? (
                  <div className="mt-2 pt-2 border-t border-gray-700/80 text-[11px] text-gray-500">
                    已轉發到「{forwardHints[msgKey].label}」
                    <button type="button" className="ml-1 text-violet-400 hover:text-violet-300 font-medium" onClick={() => jumpToSession(forwardHints[msgKey].session)}>前往查看 →</button>
                  </div>
                ) : null}
              </div>
            ) : (
              /* Claude — 設計 1a 文件流：頭像列 + 無邊框正文 */
              <div className="bubble-claude w-fit max-w-full min-w-0 flex flex-col gap-3">
                <div className="flex items-center gap-2 min-w-0">
                  <div className="flex items-center gap-2">
                    <span className={`inline-flex items-center justify-center w-[22px] h-[22px] rounded-md shrink-0 ${getAgentBadgeClass(displayAgent, session)}`} aria-hidden>
                      <AgentBadgeIcon agentType={displayAgent} session={session} />
                    </span>
                    <span className="text-[13px] font-bold text-ra-t2">{agentType === 'telegram' && m.author ? m.author : (AGENT_LABEL[displayAgent] || displayAgent || 'Claude')}</span>
                  </div>
                </div>
                {claudeAtts.length > 0 && <MessageAttachments items={claudeAtts} onPreview={setLightboxSrc} />}
                {(m.html || showStreamingTail) ? (
                  <div className="flex flex-row items-start gap-1">
                    {m.html && (
                      <div
                        className="prose text-ra-t2 text-sm leading-[1.8] flex-1 min-w-0"
                        dangerouslySetInnerHTML={{ __html: m.html }}
                        onClick={handleProseClick}
                      />
                    )}
                    {showStreamingTail && (
                      <span className="streaming-tail shrink-0 select-none" aria-hidden="true">...</span>
                    )}
                  </div>
                ) : claudeAtts.length > 0 || m.status === 'interrupted' ? null : (
                  <div className="flex gap-1 py-1"><span className="dot"/><span className="dot"/><span className="dot"/></div>
                )}
                {forwardHints[msgKey] ? (
                  <div className="pt-1 text-[11px] text-ra-t5">
                    已轉發到「{forwardHints[msgKey].label}」
                    <button type="button" className="ml-1 text-violet-400 hover:text-violet-300 font-medium" onClick={() => jumpToSession(forwardHints[msgKey].session)}>前往查看 →</button>
                  </div>
                ) : null}
              </div>
            )}
            {m.status === 'interrupted' && <div role="status" className="mt-2 text-xs text-amber-500">服務重啟，這段回覆已中斷；請確認內容後再繼續。</div>}
            {(timeLabel || m.role === 'claude' || m.role === 'shell') ? (
              <div className="mt-0.5 -mb-1 flex items-center gap-1 px-0.5">
                {timeLabel ? (
                  <span className="ra-msg-time text-[10px] leading-none text-ra-t5 tabular-nums select-none" title={String(m.createdAt || '')}>
                    {timeLabel}
                  </span>
                ) : null}
                {(m.role === 'claude' || m.role === 'shell') ? (
                  <div className="flex items-center gap-0.5">
                    <MessageCopyButton text={forwardBody} className="p-1 rounded-md text-gray-400 hover:text-gray-200" />
                    <button
                      type="button"
                      onClick={(e) => { e.stopPropagation(); setForwardModal({ messageKey: msgKey, messageContent: forwardBody }); }}
                      disabled={!canForwardShellOrAgent}
                      title="轉發到其他會話"
                      aria-label="轉發到其他會話"
                      className={`${isGuest ? 'hidden' : 'inline-flex'} items-center justify-center min-w-[32px] min-h-[32px] rounded-md text-gray-400 hover:text-cyan-400 disabled:opacity-30`}
                    >
                      <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" className="w-3.5 h-3.5" aria-hidden="true"><path d="m15 14 5-5-5-5" /><path d="M4 20v-7a4 4 0 0 1 4-4h12" /></svg>
                    </button>
                  </div>
                ) : null}
              </div>
            ) : null}
            </div>
          </div>
          );
        }}
        </VirtualMessages>

        {/* Shell 指令批准對話框 */}
        {state === 'SHELL_AWAITING_APPROVAL' && shellPendingCmd && canAct && (
          <div className="rounded-xl border border-amber-700/60 bg-amber-950/30 px-4 py-3 text-sm mr-8">
            <div className="text-amber-400 font-semibold mb-2">⚠️ 即將執行 Shell 指令</div>
            <div className="text-gray-400 text-xs mb-1">Shell：{shellPendingCmd.shell_type || shellType}</div>
            {shellPendingCmd.work_dir && (
              <div className="text-gray-400 text-xs mb-2">目錄：<span className="font-mono text-gray-300">{shellPendingCmd.work_dir}</span></div>
            )}
            <pre className="bg-gray-900/60 rounded px-3 py-2 text-xs font-mono text-gray-200 mb-3 whitespace-pre-wrap break-words">{shellPendingCmd.command}</pre>
            <div className="flex gap-2">
              <button onClick={handleShellApprove}
                className="px-3 py-1.5 bg-amber-700 hover:bg-amber-600 text-white rounded-lg text-xs font-medium">
                確認執行
              </button>
              <button onClick={handleShellCancel}
                className="px-3 py-1.5 bg-gray-700 hover:bg-gray-600 text-gray-300 rounded-lg text-xs">
                取消
              </button>
            </div>
          </div>
        )}

        {question && (
          <QuestionCard key={question.request_id} question={question} error={questionError}
            canAct={canAct} connected={connected} send={send} />
        )}

        {/* AI 授權確認 */}
        {state === 'AWAITING_CONFIRM' && permTools.length > 0 && (
          <div className="rounded-xl border border-yellow-700 bg-yellow-950/40 px-4 py-3 text-sm mr-8">
            <div className="text-yellow-400 font-semibold mb-2">需要授權</div>
            {permTools.map((t, i) => <PermToolDetail key={i} tool={t} />)}
            {permRetryTools && (
              <div className="mt-2 text-xs text-yellow-200/90">
                允許後會重試任務，並在本次重試中放行以上工具，不限於畫面上的單一操作。這次允許不會永久記住。
              </div>
            )}
            {!canAct && <div className="mt-2 text-xs text-yellow-200/70">等待有權限的人處理…</div>}
            <div className={canAct ? 'flex gap-2 mt-3' : 'hidden'}>
              <button onClick={handleAllowOnce}
                className="px-3 py-1.5 bg-yellow-700 hover:bg-yellow-600 text-white rounded-lg text-xs">
                {permRetryTools ? '允許工具並重試' : '允許此操作'}
              </button>
              {/* 切到 acceptEdits 只對編輯類工具有效；Bash 等會再被拒一次，所以只在全是編輯工具時顯示 */}
              {!isGuest && permTools.every((t) => EDIT_TOOL_NAMES.has(t.tool_name)) && (
                <button onClick={() => handlePermModeCommitNow('acceptEdits')}
                  className="px-3 py-1.5 bg-orange-700 hover:bg-orange-600 text-white rounded-lg text-xs">
                  允許並自動允許編輯
                </button>
              )}
              <button onClick={handleDenyOnce}
                className="px-3 py-1.5 bg-red-900 hover:bg-red-800 text-red-100 rounded-lg text-xs">
                拒絕
              </button>
            </div>
          </div>
        )}

        {state === 'AWAITING_SHELL_CONFIRM' && shellRequest && canAct && (
          <div className="rounded-xl border border-orange-700/90 bg-orange-950/35 px-4 py-3 text-sm mr-8">
            <div className="text-orange-300 font-semibold mb-2">允許執行 Shell 指令？</div>
            <div className="text-gray-200 text-xs font-mono break-words mb-1 whitespace-pre-wrap">{shellRequest.line}</div>
            <div className="text-gray-500 text-[10px] font-mono mb-2">
              指令名稱：<span className="text-orange-200/90">{shellRequest.command}</span>
              {shellRequest.workDirKey ? (
                <span className="block truncate mt-0.5" title={shellRequest.workDirKey}>目錄：{shellRequest.workDirKey}</span>
              ) : null}
            </div>
            <div className="flex flex-wrap gap-2 mt-2">
              <button type="button" onClick={() => sendShellConfirm('shell_allow_once')} disabled={shellConfirmPending}
                className="px-3 py-1.5 bg-orange-800 hover:bg-orange-700 text-white rounded-lg text-xs disabled:opacity-40">
                允許一次
              </button>
              <button type="button" onClick={() => sendShellConfirm('shell_allow_remember_workdir')} disabled={shellConfirmPending}
                className="px-3 py-1.5 bg-amber-900 hover:bg-amber-800 text-amber-100 rounded-lg text-xs disabled:opacity-40">
                允許並記住此目錄
              </button>
              <button type="button" onClick={() => sendShellConfirm('shell_deny')} disabled={shellConfirmPending}
                className="px-3 py-1.5 bg-gray-800 hover:bg-gray-700 text-gray-300 rounded-lg text-xs disabled:opacity-40">
                拒絕
              </button>
            </div>
          </div>
        )}

        {handoffInfo && !isGuest && (
          <div className="rounded-xl border border-rose-700/70 bg-rose-950/30 px-4 py-3 text-sm mr-8">
            <div className="text-rose-300 font-semibold mb-1">⚠️ 這個會話無法繼續，自動接手未成功</div>
            <div className="text-gray-400 text-xs mb-3">原因：{handoffInfo.reason}</div>
            <button type="button" onClick={openHandoffComposer}
              className="px-3 py-1.5 bg-rose-800 hover:bg-rose-700 text-white rounded-lg text-xs font-medium">
              建立接手會話
            </button>
          </div>
        )}

        <div ref={bottomRef} />
      </div>
      {isTGChat && hasNewBelow && (
        <button
          type="button"
          onClick={jumpToLatest}
          data-new-message-hint
          className="absolute bottom-3 left-1/2 -translate-x-1/2 rounded-full border border-violet-400/60 bg-violet-600 px-3 py-1.5 text-xs font-medium text-white shadow-lg hover:bg-violet-500 focus:outline-none focus-visible:ring-2 focus-visible:ring-violet-300"
        >
          有新訊息 ↓
        </button>
      )}
      {showJumpLatest && (
        <button
          type="button"
          onClick={jumpToLatest}
          aria-label="跳到最新訊息"
          title="跳到最新訊息"
          className="absolute bottom-3 right-4 flex h-9 w-9 items-center justify-center rounded-full border border-ra-line-strong bg-ra-raised text-ra-t2 shadow-lg hover:bg-ra-raised transition-colors"
        >
          <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" className="w-4 h-4" aria-hidden="true">
            <path d="M12 5v14" />
            <path d="m19 12-7 7-7-7" />
          </svg>
        </button>
      )}
      </div>

      {agentRunning && !agentReadOnly && !isSnapshot && !shareOver && (
        <LiveActivity key={session.id} activity={liveActivity} state={state} connected={connected} runStartedAt={runStartedAt} />
      )}

      {/* 輸入區：水平內距略小於訊息列表，讓輸入框可用寬度較大。唯讀訪客／分享已結束時改顯示提示列。 */}
      {agentReadOnly ? (
        <div role="status" className="shrink-0 border-t border-ra-line-soft bg-ra-bg px-4 py-4 text-center text-sm text-ra-t3">
          Kiro CLI（舊版）已停止支援，此會話僅供閱讀；請建立 Kiro ACP 新會話
        </div>
      ) : guestReadOnly || shareOver ? (
        <GuestReadOnlyBar snapshot={isSnapshot} ended={shareOver} />
      ) : (
      <div
        className={`ra-chat-composer shrink-0 px-3 sm:px-7 py-2 sm:py-4 border-t transition-colors duration-200 ${inputMode === 'shell' ? 'bg-ra-bg border-amber-900/40' : 'bg-ra-bg border-ra-line-soft'}`}
        style={{ paddingBottom: 'calc(0.75rem + var(--ra-safe-bottom, env(safe-area-inset-bottom)))' }}
      >
        {taskRunning && !canQueue ? (
          <button onClick={handleInterrupt}
            className={`w-full py-2 bg-red-900/60 hover:bg-red-800 text-red-300 rounded-lg text-sm ${interrupting ? 'animate-pulse motion-reduce:animate-none' : ''}`}>
            {interrupting ? '再按一次強制停止' : '中斷'}
          </button>
        ) : (
          <div className="w-full relative" ref={composerWrapRef}>
            {draftRestoring && <div role="status" className="mb-2 text-xs text-ra-t4">還原附件草稿中…</div>}
            {draftSaving && <div role="status" className="mb-2 text-xs text-ra-t4">保存附件草稿中…</div>}
            {draftSaveError && <div role="alert" className="mb-2 text-xs text-amber-400">
              {draftSaveError}<button type="button" onClick={draftRestoreFailed ? retryDraftFiles : saveFiles} disabled={draftSaving || draftRestoring} className="ml-2 underline disabled:opacity-40">{draftRestoreFailed ? '重試還原' : '重試保存'}</button>
            </div>}
            {pendingInput && <div role="status" className="mb-2 rounded-lg border border-amber-700/50 px-3 py-2 text-xs text-ra-t3">
              <div>{sending ? '訊息確認中…可繼續輸入下一則' : '上一則訊息尚未確認，已保留原內容'}</div>
              <div className="mt-1 line-clamp-2 whitespace-pre-wrap break-words">{pendingInput.text || '附件訊息'}</div>
              {!sending && <button type="button" disabled={!connected} onClick={() => sendComposerInput(pendingInput)}
                className="mt-2 rounded px-2 py-1 border border-ra-line-strong disabled:opacity-40">重試確認</button>}
            </div>}
            <QueuedMessages
              sessionId={session.id}
              items={queue}
              paused={queuePaused}
              canResume={queue.length > 0 && (queuePaused || state === 'IDLE' || state === 'SHELL_IDLE')}
              onRemove={(id) => sendOrToast({ type: 'queue_remove', id })}
              onResume={() => sendOrToast({ type: 'queue_resume' })}
            />
            {inputMode !== 'shell' && (
              <MentionChips items={mentionChips} onRemove={handleMentionChipRemove} />
            )}
            {mentionOpen && !slashMenuOpen && (
              <MentionMenu
                items={mentionItems}
                activeIndex={mentionActiveIdx}
                onSelect={handleMentionSelect}
              />
            )}
            <div className={(inputMode === 'shell' ? 'ra-cmd-bar shell' : 'ra-cmd-bar') + ' w-full' + (dragOver ? ' ring-2 ring-violet-500/70' : '')}>
              <div className="ra-composer-input relative flex w-full min-w-0 flex-col" ref={slashInputWrapRef}>
                {inputMode !== 'shell' && <AttachmentChips items={attachments} onRemove={removeAttachment} onRetry={retryAttachment} disabled={sending || draftRestoring} />}
                {slashMenuOpen && (
                  <SlashCommandMenu
                    items={slashMenuItems}
                    activeIndex={slashActiveIdx}
                    onSelect={handleSlashSelect}
                  />
                )}
                <textarea
                  ref={chatInputRef}
                  data-chat-input
                  aria-label="訊息或指令"
                  value={input}
                  onChange={(e) => {
                    const value = e.target.value;
                    setInput(value);
                    try {
                      localStorage.setItem(draftInputStorageKey(session.id), value);
                    } catch (_) {}
                    syncComposerMenus(value, e.target.selectionStart, inputMode);
                  }}
                  onInput={(e) => {
                    const el = e.target;
                    el.style.height = 'auto';
                    let cap = 240;
                    try {
                      const maxStr = getComputedStyle(el).maxHeight;
                      const maxPx = parseFloat(maxStr);
                      if (Number.isFinite(maxPx) && maxPx > 0) cap = maxPx;
                    } catch (_) {}
                    const needed = el.scrollHeight;
                    if (needed > cap) {
                      el.style.height = `${cap}px`;
                      el.style.overflowY = 'auto';
                    } else {
                      el.style.height = `${needed}px`;
                      el.style.overflowY = 'hidden';
                    }
                  }}
                  onSelect={(e) => syncComposerMenus(e.target.value, e.target.selectionStart, inputMode)}
                  onKeyDown={handleKeyDown}
                  onPaste={handlePaste}
                  disabled={isDisabled && !sending}
                  placeholder={inputMode === 'shell' ? `輸入 ${shellType || 'Shell'} 指令…` : canQueue ? '執行中…送出會排入佇列' : attachments.length ? '想請我如何處理這些附件？（可直接送出）' : '輸入指令… @ 標記 session'}
                  rows={1}
                  className={[
                    'w-full min-w-0 resize-none border-0 bg-transparent px-1 py-1.5 text-[13.5px] leading-relaxed placeholder-ra-t5 focus:outline-none disabled:opacity-40 min-h-[2rem] max-h-[240px] box-border app-scroll',
                    inputMode === 'shell' ? 'text-amber-50 font-mono placeholder-amber-900/60' : 'text-ra-t1',
                  ].join(' ')}
                />
              </div>
              <input ref={fileInputRef} type="file" multiple hidden onChange={handleFilesPicked} onCancel={() => { pickerRequestRef.current = null; }} />
              <ComposerTools key={session.id} open={toolsOpen && toolsSessionRef.current === session.id} setOpen={updateToolsOpen} enabled={canAct && !shareOver && isFocused}>
                {sheet => {
                  const modeControl = !isGuest && (
                    <ModeToggleBtn
                      sheet={sheet}
                      value={inputMode}
                      onChange={handleInputModeChange}
                      disabled={modeSwitchDisabled || sending}
                      agentLabel={AGENT_LABEL[displayAgent] || 'Claude'}
                    />
                  );
                  const attachmentControl = inputMode !== 'shell' && (
                    <button type="button" hidden={!!sheet?.page} onClick={() => openAttachmentPicker(sheet)}
                      disabled={sending || isDisabled}
                      aria-label="附加圖片或檔案"
                      title="附加圖片或檔案"
                      className={sheet ? 'ra-tool-row ra-tool-attach' : 'shrink-0 flex items-center justify-center w-8 h-8 rounded-[8px] text-ra-t3 hover:bg-ra-raised hover:text-violet-300 disabled:opacity-40 transition-colors'}>
                      {uploading ? (
                        <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" className="w-4 h-4 animate-spin motion-reduce:animate-none" aria-hidden="true">
                          <path d="M21 12a9 9 0 1 1-6.22-8.56" />
                        </svg>
                      ) : (
                        <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" className="w-4 h-4" aria-hidden="true">
                          <path d="m21.44 11.05-9.19 9.19a6 6 0 0 1-8.49-8.49l8.57-8.57A4 4 0 1 1 18 8.84l-8.59 8.57a2 2 0 0 1-2.83-2.83l8.49-8.48" />
                        </svg>
                      )}
                      {sheet && <span><strong>加入圖片或檔案</strong><small>選取後回到訊息草稿</small></span>}
                    </button>
                  );
                  const permissionControl = showPermModeSelect && (
                    <PermModeSelect
                      sheet={sheet}
                      compact
                      agentType={agentType}
                      value={mode}
                      onChange={handlePermModeDraftChange}
                      disabled={modeSwitchDisabled}
                      title={agentType === 'antigravity' ? 'Antigravity 權限模式' : agentType === 'cursor' ? '權限模式（bypass：Cursor 會加 --force）' : '權限模式'}
                      id="chat-perm-mode"
                    />
                  );
                  const modelControl = (
                    <ModelSelect
                      sheet={sheet}
                      compact
                      agentType={agentType}
                      value={modelSel}
                      onChange={handleModelDraftChange}
                      disabled={modeSwitchDisabled}
                      id="chat-model"
                    />
                  );
                  const effortControl = showEffortSelect && (
                    <EffortSelect
                      sheet={sheet}
                      compact
                      value={effortSel}
                      onChange={handleEffortDraftChange}
                      disabled={modeSwitchDisabled}
                      id="chat-effort"
                    />
                  );
                  return <>
                    {!sheet && modeControl}
                    {attachmentControl}
                    {sheet && modeControl}
                    {!isGuest && inputMode !== 'shell' && (sheet
                      ? <>{modelControl}{effortControl}{permissionControl}</>
                      : <>{permissionControl}{modelControl}{effortControl}</>)}
                    {sheet && !sheet.page && !isGuest && inputMode !== 'shell' && <p className="ra-tools-footnote">模型、思考強度與權限將於下次送出套用。</p>}
                  </>;
                }}
              </ComposerTools>
              {/* 把中斷／送出推到按鈕列右側 */}
              <div className="ra-composer-spacer flex-1" aria-hidden="true" />
              <div className="ra-composer-actions flex shrink-0 items-center gap-2">
                {(agentRunning || lingering) && (
                  <button type="button" onClick={handleInterrupt}
                    aria-label={interrupting ? '再按一次強制停止' : '中斷'}
                    title={interrupting ? '再按一次強制停止' : '中斷'}
                    className={`shrink-0 flex items-center justify-center w-8 h-8 rounded-[8px] bg-red-900/70 hover:bg-red-800 text-red-200 text-sm ${interrupting ? 'animate-pulse' : ''}`}>
                    <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="currentColor" className="w-3.5 h-3.5" aria-hidden="true"><rect x="5" y="5" width="14" height="14" rx="2" /></svg>
                  </button>
                )}
                <button type="button" onClick={() => handleSend()} disabled={!!pendingInput || sending || isDisabled || (!input.trim() && (inputMode === 'shell' || attachments.length === 0)) || (inputMode !== 'shell' && attachments.some(a => a.status !== 'ready'))}
                  aria-label={canQueue ? '排入佇列' : '送出'}
                  title={sending ? '送出中…' : inputMode !== 'shell' && attachments.some(a => a.status !== 'ready') ? '請等待附件上傳完成，或移除失敗的附件' : (canQueue ? '排入佇列' : '送出') + (coarsePointer ? '' : '（Enter）')}
                  className={`shrink-0 flex items-center justify-center w-8 h-8 rounded-[8px] ${inputMode === 'shell' ? 'bg-amber-700 hover:bg-amber-600' : 'bg-[oklch(0.62_0.19_275)] hover:brightness-110'} disabled:opacity-30 text-white transition-colors text-sm`}>
                  <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.2" strokeLinecap="round" strokeLinejoin="round" className="w-4 h-4" aria-hidden="true"><path d="M12 19V5" /><path d="m5 12 7-7 7 7" /></svg>
                </button>
              </div>
            </div>
          </div>
        )}
      </div>
      )}

      <ForwardModal
        payload={forwardModal}
        onClose={() => setForwardModal(null)}
        currentSessionId={session.id}
        allSessions={allSessions}
        usePermModeDropdown={usePermModeDropdown}
        onForwarded={({ messageKey, targetSession, jump, sent = true }) => {
          // 新建會話但訊息沒送出時不標「已轉發」，內容已放進對方的草稿
          if (sent) {
            setForwardHints((prev) => ({
              ...prev,
              [messageKey]: {
                label: `${targetSession.agent_type || 'claude'} / ${workDirGroupShortLabel(targetSession.work_dir)}`,
                session: targetSession,
              },
            }));
          }
          if (jump) jumpToSession(targetSession);
        }}
      />

      <MessageActionsHost
        openRef={openMsgSheetRef}
        onForward={handleSheetForward}
        sessionId={session.id}
        onQuote={text => {
          setInput(previous => injectMarkdownQuote(previous, text));
          setQuoteSelection(null);
          window.getSelection()?.removeAllRanges();
          focusChatInputToEnd();
        }}
      />

      <SelectionQuotePopover
        targetRect={canAct && quoteSelection ? quoteSelection.targetRect : null}
        onQuote={handleQuoteReply}
        onClose={() => setQuoteSelection(null)}
      />

      {lightboxSrc && (
        <ChatImageLightbox
          src={typeof lightboxSrc === 'string' ? lightboxSrc : (lightboxSrc ? lightboxSrc.src : '')}
          svg={typeof lightboxSrc === 'object' && lightboxSrc ? lightboxSrc.svg : null}
          onClose={() => setLightboxSrc(null)}
        />
      )}

      {shareOpen && !isGuest && (
        <ShareModal session={session} onClose={() => setShareOpen(false)} />
      )}
    </div>
  );
}

const EDIT_TOOL_NAMES = new Set(['Edit', 'Write', 'MultiEdit', 'NotebookEdit']);

/** 授權面板的單一工具：Bash 顯示完整指令，編輯類顯示檔名與內容，其餘退回格式化 JSON。長內容可捲動。 */
function PermToolDetail({ tool }) {
  const input = tool.tool_input && typeof tool.tool_input === 'object' ? tool.tool_input : null;
  const preCls = 'mt-1 max-h-40 overflow-auto app-scroll rounded bg-gray-900/60 px-2.5 py-2 text-xs font-mono text-gray-200 whitespace-pre-wrap break-words';
  let body = null;
  if (input && typeof input.command === 'string') {
    body = <pre className={preCls}>{input.command}</pre>;
  } else if (input && typeof input.file_path === 'string') {
    const text = input.new_string ?? input.content ?? '';
    body = (
      <>
        <div className="mt-1 text-xs font-mono text-gray-400 break-all">{input.file_path}</div>
        {text ? <pre className={preCls}>{String(text)}</pre> : null}
      </>
    );
  } else if (input) {
    body = <pre className={preCls}>{JSON.stringify(input, null, 2)}</pre>;
  }
  return (
    <div className="mb-2">
      <div className="text-gray-300 text-xs font-mono font-semibold">{tool.tool_name}</div>
      {body}
    </div>
  );
}

/** 排隊中的訊息：每則可移除；暫停（前一輪失敗／中斷／拒絕授權）時需手動繼續。 */
function QueuedMessages({ items, paused, canResume, onRemove, onResume, sessionId }) {
  if (!Array.isArray(items) || items.length === 0) return null;
  return (
    <div className="mb-2 rounded-lg border border-ra-line bg-ra-card px-2 py-1.5 text-xs" aria-label="排隊中的訊息">
      <div className="flex items-center justify-between mb-1 text-ra-t4">
        <span>{paused ? `⏸ 佇列已暫停（${items.length}）` : `佇列（${items.length}）· 完成後依序執行`}</span>
        {canResume && (
          <button type="button" onClick={onResume}
            className="px-2 py-0.5 rounded bg-[oklch(0.62_0.19_275)] hover:brightness-110 text-white">
            繼續
          </button>
        )}
      </div>
      <ol className="space-y-0.5">
        {items.map((q, i) => (
          <li key={q.id} className="flex items-center gap-1.5 text-ra-t2">
            <span className="shrink-0 font-mono text-[10px] text-ra-t5">{i + 1}.</span>
            <span className="min-w-0 flex-1 truncate" title={attachmentQueueSummary(q, sessionId)}>{attachmentQueueSummary(q, sessionId)}</span>
            <button type="button" onClick={() => onRemove(q.id)}
              aria-label="移除這則排隊訊息"
              className="shrink-0 px-1 text-ra-t4 hover:text-red-300">
              ×
            </button>
          </li>
        ))}
      </ol>
    </div>
  );
}

/** 聊天截圖與圖表燈箱：鎖背景捲動，支援雙指縮放／單指拖曳／雙擊切換縮放。 */
function ChatImageLightbox({ src, svg, onClose }) {
  const [{ scale, x, y }, setTransform] = useState({ scale: 1, x: 0, y: 0 });
  const transformRef = useRef({ scale: 1, x: 0, y: 0 });
  const pinchRef = useRef(null);
  const panRef = useRef(null);
  const lastTapRef = useRef(0);

  const applyTransform = useCallback((next) => {
    const scale = Math.min(4, Math.max(1, next.scale));
    const x = scale === 1 ? 0 : next.x;
    const y = scale === 1 ? 0 : next.y;
    const t = { scale, x, y };
    transformRef.current = t;
    setTransform(t);
  }, []);

  useEffect(() => {
    const prevOverflow = document.body.style.overflow;
    const prevTouchAction = document.body.style.touchAction;
    document.body.style.overflow = 'hidden';
    document.body.style.touchAction = 'none';
    const onKey = (e) => { if (e.key === 'Escape') onClose(); };
    // 非 passive，才能擋掉 iOS/Telegram WebView 把 touchmove 傳給底下聊天室。
    const blockScroll = (e) => { e.preventDefault(); };
    document.addEventListener('keydown', onKey);
    document.addEventListener('touchmove', blockScroll, { passive: false });
    return () => {
      document.body.style.overflow = prevOverflow;
      document.body.style.touchAction = prevTouchAction;
      document.removeEventListener('keydown', onKey);
      document.removeEventListener('touchmove', blockScroll);
    };
  }, [onClose]);

  const touchDistance = (a, b) => Math.hypot(a.clientX - b.clientX, a.clientY - b.clientY);

  const onTouchStart = (e) => {
    if (e.touches.length === 2) {
      panRef.current = null;
      pinchRef.current = {
        dist: touchDistance(e.touches[0], e.touches[1]),
        scale: transformRef.current.scale,
        x: transformRef.current.x,
        y: transformRef.current.y,
      };
      return;
    }
    if (e.touches.length === 1) {
      pinchRef.current = null;
      const now = Date.now();
      if (now - lastTapRef.current < 280) {
        lastTapRef.current = 0;
        const cur = transformRef.current;
        if (cur.scale > 1.05) {
          applyTransform({ scale: 1, x: 0, y: 0 });
        } else {
          applyTransform({ scale: 2.5, x: 0, y: 0 });
        }
        return;
      }
      lastTapRef.current = now;
      if (transformRef.current.scale > 1) {
        panRef.current = {
          x: e.touches[0].clientX,
          y: e.touches[0].clientY,
          ox: transformRef.current.x,
          oy: transformRef.current.y,
        };
      }
    }
  };

  const onTouchMove = (e) => {
    e.preventDefault();
    e.stopPropagation();
    if (e.touches.length === 2 && pinchRef.current) {
      const dist = touchDistance(e.touches[0], e.touches[1]);
      const ratio = dist / Math.max(1, pinchRef.current.dist);
      applyTransform({
        scale: pinchRef.current.scale * ratio,
        x: pinchRef.current.x,
        y: pinchRef.current.y,
      });
      return;
    }
    if (e.touches.length === 1 && panRef.current) {
      const dx = e.touches[0].clientX - panRef.current.x;
      const dy = e.touches[0].clientY - panRef.current.y;
      applyTransform({
        scale: transformRef.current.scale,
        x: panRef.current.ox + dx,
        y: panRef.current.oy + dy,
      });
    }
  };

  const onTouchEnd = (e) => {
    if (e.touches.length < 2) pinchRef.current = null;
    if (e.touches.length === 0) panRef.current = null;
    if (transformRef.current.scale <= 1.05) {
      applyTransform({ scale: 1, x: 0, y: 0 });
    }
  };

  const onWheel = (e) => {
    e.preventDefault();
    const cur = transformRef.current;
    const next = cur.scale * (e.deltaY < 0 ? 1.15 : 0.85);
    applyTransform({ scale: next, x: cur.x, y: cur.y });
  };

  return (
    <div
      className="fixed inset-0 z-[200] flex items-center justify-center bg-black/90 overscroll-none"
      style={{ touchAction: 'none' }}
      onClick={onClose}
      onTouchStart={onTouchStart}
      onTouchMove={onTouchMove}
      onTouchEnd={onTouchEnd}
      onWheel={onWheel}
      role="presentation"
    >
      <button
        type="button"
        onClick={(e) => {
          e.stopPropagation();
          onClose();
        }}
        className="absolute top-4 right-4 z-[210] flex items-center justify-center w-10 h-10 rounded-full bg-white/10 hover:bg-white/25 text-white transition-colors cursor-pointer select-none"
        aria-label="關閉"
        title="關閉 (Esc)"
      >
        <svg viewBox="0 0 24 24" width="20" height="20" fill="none" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round">
          <line x1="18" y1="6" x2="6" y2="18"></line>
          <line x1="6" y1="6" x2="18" y2="18"></line>
        </svg>
      </button>

      {svg ? (
        <div
          className="ra-lightbox-svg-container pointer-events-none flex items-center justify-center p-4 select-none"
          style={{
            width: 'min(92vw, 1200px)',
            height: '85vh',
            transform: `translate3d(${x}px, ${y}px, 0) scale(${scale})`,
            transformOrigin: 'center center',
            touchAction: 'none',
            cursor: scale > 1 ? 'grab' : 'zoom-in',
          }}
          onClick={(e) => {
            e.stopPropagation();
            if (Date.now() - lastTapRef.current < 280) {
              lastTapRef.current = 0;
              applyTransform(transformRef.current.scale > 1.05 ? { scale: 1, x: 0, y: 0 } : { scale: 2.2, x: 0, y: 0 });
            } else {
              lastTapRef.current = Date.now();
            }
          }}
          dangerouslySetInnerHTML={{ __html: svg }}
        />
      ) : (
        <img
          src={src}
          alt=""
          draggable={false}
          className="max-w-[95vw] max-h-[95vh] object-contain rounded-lg select-none"
          style={{
            transform: `translate3d(${x}px, ${y}px, 0) scale(${scale})`,
            transformOrigin: 'center center',
            touchAction: 'none',
            cursor: scale > 1 ? 'grab' : 'zoom-in',
          }}
          onClick={(e) => {
            e.stopPropagation();
            if (Date.now() - lastTapRef.current < 280) {
              lastTapRef.current = 0;
              applyTransform(transformRef.current.scale > 1.05 ? { scale: 1, x: 0, y: 0 } : { scale: 2.5, x: 0, y: 0 });
            } else {
              lastTapRef.current = Date.now();
            }
          }}
        />
      )}

      <div className="pointer-events-none absolute bottom-6 left-0 right-0 text-center text-[11px] text-white/55 px-4 select-none">
        雙指縮放 · 放大後可拖曳 · 雙擊切換 · 點空白或按 ✕ 關閉
      </div>
    </div>
  );
}
