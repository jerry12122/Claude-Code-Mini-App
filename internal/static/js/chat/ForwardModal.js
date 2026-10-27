/** 將 assistant 訊息轉發到其他會話（Modal） */
function ForwardModal({
  payload,
  onClose,
  currentSessionId,
  onForwarded,
  allSessions,
  usePermModeDropdown = false,
}) {
  const allSessionsRef = useRef(allSessions);
  useEffect(() => { allSessionsRef.current = allSessions; }, [allSessions]);
  const [sessionsList, setSessionsList] = useState([]);
  const [loadingSessions, setLoadingSessions] = useState(false);
  const [selectedId, setSelectedId] = useState(null);
  const [copyFromId, setCopyFromId] = useState('');
  const [note, setNote] = useState('');
  const newForm = useNewSessionForm();
  const [busy, setBusy] = useState(false);
  const [errText, setErrText] = useState('');

  useEffect(() => {
    if (!payload) return;
    const prev = document.body.style.overflow;
    document.body.style.overflow = 'hidden';
    return () => { document.body.style.overflow = prev; };
  }, [payload]);

  useEffect(() => {
    if (!payload) return;
    setNote('');
    setErrText('');
    setBusy(false);
    setSessionsList([]);
    setSelectedId(null);
    setCopyFromId('');
    newForm.reset({
      name: `轉發·${new Date().toLocaleTimeString('zh-TW', { hour: '2-digit', minute: '2-digit', second: '2-digit' })}`,
    });
    const inherited = allSessionsRef.current;
    if (Array.isArray(inherited) && inherited.length > 0) {
      setSessionsList(inherited);
      const targets = inherited.filter((s) => s.id !== currentSessionId);
      setSelectedId(targets[0]?.id ?? 'new');
      setCopyFromId(inherited[0]?.id ?? '');
    } else {
      (async () => {
        setLoadingSessions(true);
        try {
          const res = await apiFetch('/sessions');
          if (!res.ok) throw new Error('無法載入會話列表');
          const data = await res.json();
          const list = Array.isArray(data) ? data : [];
          const sorted = sortSessionsByWorkDirThenName(list);
          setSessionsList(sorted);
          const targets = sorted.filter((s) => s.id !== currentSessionId);
          setSelectedId(targets[0]?.id ?? 'new');
          setCopyFromId(sorted[0]?.id ?? '');
        } catch (e) {
          setErrText(e.message || String(e));
        } finally {
          setLoadingSessions(false);
        }
      })();
    }
  }, [payload, currentSessionId]);

  useEffect(() => {
    if (!copyFromId || !sessionsList.length) return;
    const s = sessionsList.find((x) => x.id === copyFromId);
    if (!s) return;
    newForm.applyFromSession(s);
  }, [copyFromId, sessionsList]);

  useEffect(() => {
    if (!payload) return;
    const h = (e) => {
      if (e.key === 'Escape') onClose();
    };
    window.addEventListener('keydown', h);
    return () => window.removeEventListener('keydown', h);
  }, [payload, onClose]);

  if (!payload) return null;

  const targets = sessionsList.filter((s) => s.id !== currentSessionId);
  const messageContent = String(payload.messageContent || '');

  const runForward = async () => {
    const composed = buildForwardUserPrompt(note, messageContent);
    if (!String(composed || '').trim()) {
      setErrText('沒有可轉發的內容');
      return;
    }
    setBusy(true);
    setErrText('');
    try {
      if (selectedId !== 'new') {
        if (!selectedId) throw new Error('請選擇目標會話');
        await sendPromptViaEphemeralWS(selectedId, composed);
        const target = sessionsList.find((s) => s.id === selectedId);
        if (!target) throw new Error('找不到目標會話');
        onForwarded({ messageKey: payload.messageKey, targetSession: target, jump: false });
        onClose();
        return;
      }
      if (!newForm.name.trim()) {
        setErrText('請填寫新會話名稱');
        return;
      }
      const res = await apiFetch('/sessions', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(newForm.buildCreatePayload({ input_mode: 'agent' })),
      });
      if (!res.ok) {
        const j = await res.json().catch(() => ({}));
        throw new Error(j.error || `建立失敗 (${res.status})`);
      }
      const created = await res.json();
      const sent = await sendFirstPromptOrDraft(created.id, composed);
      onForwarded({ messageKey: payload.messageKey, targetSession: created, jump: true, sent });
      onClose();
    } catch (e) {
      setErrText(e.message || String(e));
    } finally {
      setBusy(false);
    }
  };

  return (
    <div
      className="fixed inset-0 z-[100] flex items-center justify-center p-4 bg-black/60 backdrop-blur-[2px]"
      onClick={onClose}
      role="presentation"
    >
      <div
        className="max-w-md sm:max-w-xl md:max-w-2xl w-full max-h-[min(90vh,32rem)] md:max-h-[min(92vh,48rem)] overflow-y-auto app-scroll rounded-2xl border border-slate-700 bg-slate-950/95 shadow-2xl shadow-black/50 p-4 md:p-6 space-y-3 md:space-y-4"
        onClick={(e) => e.stopPropagation()}
        role="dialog"
        aria-modal="true"
        aria-labelledby="forward-modal-title"
      >
        <div id="forward-modal-title" className="text-sm md:text-base font-semibold text-slate-100">
          Forward 到其他會話
        </div>
        {loadingSessions ? (
          <div className="text-xs text-slate-500 py-4 text-center">載入會話…</div>
        ) : (
          <>
            <div className="space-y-1.5">
              <div className="text-[10px] uppercase tracking-wide text-slate-500">目標會話</div>
              <div className="space-y-1 max-h-40 md:max-h-56 overflow-y-auto app-scroll pr-0.5">
                {targets.map((s) => {
                  const active = selectedId === s.id;
                  const shortDir = workDirGroupShortLabel(s.work_dir);
                  const displayAgent = sessionDisplayAgent(s);
                  return (
                    <button
                      key={s.id}
                      type="button"
                      onClick={() => setSelectedId(s.id)}
                      className={`w-full flex items-center gap-2 rounded-lg border px-2.5 py-2 text-left text-sm transition-colors ${
                        active
                          ? 'border-cyan-400/50 bg-cyan-500/10 ring-2 ring-cyan-400/35'
                          : 'border-slate-700/80 bg-slate-900/40 hover:bg-slate-800/50'
                      }`}
                    >
                      <span
                        className={`inline-flex items-center gap-0.5 shrink-0 text-[10px] px-1.5 py-0.5 rounded-full font-mono uppercase ${getAgentBadgeClass(displayAgent, s)}`}
                      >
                        <AgentBadgeIcon agentType={displayAgent} session={s} />
                        {displayAgent}
                      </span>
                      <span className="min-w-0 flex-1 truncate font-mono text-xs text-slate-300" title={s.work_dir || ''}>
                        {shortDir}
                      </span>
                    </button>
                  );
                })}
                <button
                  type="button"
                  onClick={() => setSelectedId('new')}
                  className={`w-full rounded-lg border px-2.5 py-2 text-left text-sm transition-colors ${
                    selectedId === 'new'
                      ? 'border-cyan-400/50 bg-cyan-500/10 ring-2 ring-cyan-400/35 text-cyan-200'
                      : 'border-dashed border-slate-600 bg-slate-900/30 text-slate-400 hover:border-cyan-500/30 hover:text-cyan-300'
                  }`}
                >
                  ＋ 新建會話
                </button>
              </div>
            </div>

            {selectedId === 'new' && (
              <div className="mt-2 p-3 rounded-lg bg-slate-800/50 border border-slate-700 space-y-2.5">
                <div className="space-y-1">
                  <label className="text-[10px] text-slate-500">複製自</label>
                  <select
                    value={copyFromId}
                    onChange={(e) => setCopyFromId(e.target.value)}
                    className="w-full bg-slate-900/80 border border-slate-600 rounded-lg px-2 py-2 text-xs text-slate-200"
                  >
                    {sessionsList.length === 0 ? (
                      <option value="">（尚無會話）</option>
                    ) : (
                      sessionsList.map((s) => (
                        <option key={s.id} value={s.id}>
                          {(sessionDisplayAgent(s)) + ' · ' + workDirGroupShortLabel(s.work_dir)}
                        </option>
                      ))
                    )}
                  </select>
                </div>
                <div className="space-y-1">
                  <label className="text-[10px] text-slate-500">Agent</label>
                  <select
                    value={newForm.agent}
                    onChange={(e) => newForm.setAgent(e.target.value)}
                    className="w-full bg-slate-900/80 border border-slate-600 rounded-lg px-2 py-2 text-xs text-slate-200"
                  >
                    <option value="claude">Claude</option>
                    <option value="cursor">Cursor</option>
                    <option value="kiroacp">Kiro ACP</option>
                    <option value="codex">Codex</option>
                  </select>
                </div>
                <div className="space-y-1">
                  <label className="text-[10px] text-slate-500">工作目錄</label>
                  <input
                    value={newForm.workDir}
                    onChange={(e) => newForm.setWorkDir(e.target.value)}
                    placeholder="/path/to/project"
                    className="w-full bg-slate-900/80 border border-slate-600 rounded-lg px-2 py-2 text-xs text-slate-100 font-mono placeholder-slate-600"
                  />
                </div>
                <div className="space-y-1">
                  <label className="text-[10px] text-slate-500">Model（選填，寫入 --model）</label>
                  <input
                    value={newForm.model}
                    onChange={(e) => newForm.setModel(e.target.value)}
                    placeholder="例如 sonnet"
                    className="w-full bg-slate-900/80 border border-slate-600 rounded-lg px-2 py-2 text-xs text-slate-100 placeholder-slate-600"
                  />
                </div>
                <div className="space-y-1">
                  <label className="text-[10px] text-slate-500">權限模式</label>
                  {usePermModeDropdown ? (
                    <PermModeSelect
                      agentType={newForm.agent}
                      value={newForm.mode}
                      onChange={newForm.setMode}
                      disabled={newForm.agent === 'codex' || newForm.agent === 'kiro'}
                      id="forward-new-perm-mode-classic"
                      className="w-full py-2 text-xs"
                    />
                  ) : (
                    <PermModeSwitch
                      agentType={newForm.agent}
                      value={newForm.mode}
                      onChange={newForm.setMode}
                      disabled={newForm.agent === 'codex' || newForm.agent === 'kiro'}
                    />
                  )}
                </div>
                {newForm.agent === 'claude' && (
                  <div className="space-y-1">
                    <label className="text-[10px] text-slate-500">自訂 CLI 引數（選填，不含 --model）</label>
                    <textarea
                      value={newForm.cliExtra}
                      onChange={(e) => newForm.setCliExtra(e.target.value)}
                      rows={3}
                      spellCheck={false}
                      className="w-full bg-slate-900/80 border border-slate-600 rounded-lg px-2 py-2 text-xs text-slate-100 font-mono placeholder-slate-600 app-scroll"
                    />
                  </div>
                )}
                <div className="space-y-1">
                  <label className="text-[10px] text-slate-500">新會話名稱（必填）</label>
                  <input
                    value={newForm.name}
                    onChange={(e) => newForm.setName(e.target.value)}
                    className="w-full bg-slate-900/80 border border-slate-600 rounded-lg px-2 py-2 text-xs text-slate-100"
                  />
                </div>
              </div>
            )}

            <div className="space-y-1">
              <label className="text-[10px] text-slate-500">備註（可選）</label>
              <textarea
                value={note}
                onChange={(e) => setNote(e.target.value)}
                rows={3}
                placeholder="加上備註給目標會話（可選）"
                className="w-full bg-slate-900/70 border border-slate-600 rounded-lg px-2 py-2 text-sm text-slate-100 placeholder-slate-500 focus:outline-none focus:border-cyan-500/45"
              />
            </div>

            {errText ? <div className="text-xs text-red-400">{errText}</div> : null}

            <div className="flex flex-wrap gap-2 justify-end pt-1">
              <button
                type="button"
                onClick={onClose}
                disabled={busy}
                className="px-3 py-2 rounded-xl text-xs font-medium bg-slate-800 hover:bg-slate-700 text-slate-300 border border-slate-600 disabled:opacity-40"
              >
                取消
              </button>
              <button
                type="button"
                onClick={runForward}
                disabled={busy || loadingSessions}
                className="px-4 py-2 rounded-xl text-xs font-semibold bg-gradient-to-r from-cyan-500 to-teal-600 text-slate-950 hover:brightness-110 disabled:opacity-40"
              >
                {busy ? '送出中…' : 'Forward'}
              </button>
            </div>
          </>
        )}
      </div>
    </div>
  );
}

