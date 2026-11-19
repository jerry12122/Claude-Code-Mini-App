function QuestionCard({ question, error, canAct, connected, send }) {
  const [answers, setAnswers] = useState(() => question.questions.map(() => ({ selected: [], text: '' })));
  const [submitting, setSubmitting] = useState(false);
  const [localError, setLocalError] = useState('');
  const lock = useRef(false);
  const remoteError = error?.request_id === question.request_id ? error.message : '';
  useEffect(() => { lock.current = false; setSubmitting(false); }, [error, connected, question]);
  const update = (i, value) => setAnswers(prev => prev.map((answer, n) => n === i ? value : answer));
  const complete = answers.every(a => a.selected.length > 0 || a.text.trim());
  const disabled = !canAct || !connected || submitting;
  const submit = (event) => {
    event.preventDefault();
    if (disabled || !complete || lock.current) return;
    lock.current = true;
    setLocalError('');
    if (send({ type: 'question_answer', request_id: question.request_id, answers })) setSubmitting(true);
    else { lock.current = false; setLocalError('連線中斷，請重連後再送出'); }
  };
  return (
    <form onSubmit={submit} className="rounded-xl border border-blue-700 bg-blue-950/30 px-4 py-3 text-sm">
      <div className="text-blue-300 font-semibold mb-3">Claude 需要你的回答</div>
      {question.questions.map((q, i) => (
        <fieldset key={i} disabled={disabled} className="mb-4 min-w-0">
          <legend className="font-medium text-gray-100 mb-2 whitespace-pre-wrap">{q.question} <span className="text-xs text-gray-400">（{q.multiSelect ? '可複選' : '單選'}）</span></legend>
          {q.options.map((option, n) => (
            <label key={n} className="flex min-h-11 items-start gap-2 p-2 rounded hover:bg-blue-900/30 cursor-pointer">
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
        <button type="submit" disabled={disabled || !complete} className="min-h-11 px-3 py-2 bg-blue-700 hover:bg-blue-600 text-white rounded-lg disabled:opacity-40">
          {submitting ? '送出中…' : '送出回答'}
        </button>
      )}
    </form>
  );
}

/** 聊天頁與工作室共用既有會話的授權／問答，不建立新的任務。 */
function SessionActionCards({ socket, canAct = true, isGuest = false }) {
  const { state, question, questionError, permTools, setPermTools, permRetryTools,
    shellPendingCmd, setShellPendingCmd, shellType, shellRequest, connected, send, commitPermMode } = socket;
  const [shellConfirmPending, setShellConfirmPending] = useState(false);
  const actionLock = useRef(false);
  const sendOrToast = (message) => {
    if (canAct && connected && send(message)) return true;
    showToast('連線中斷，操作未送出，請稍後再試', { error: true, duration: 3000 });
    return false;
  };
  const handleAllowOnce = () => {
    if (actionLock.current) return;
    actionLock.current = true;
    if (!sendOrToast({ type: 'allow_once', tools: permTools.map(t => t.tool_name) })) {
      actionLock.current = false;
      return;
    }
    setPermTools([]);
  };

  const handleDenyOnce = () => {
    if (actionLock.current) return;
    actionLock.current = true;
    if (!sendOrToast({ type: 'deny_once' })) {
      actionLock.current = false;
      return;
    }
    setPermTools([]);
  };

  /** 授權面板「允許並記住」：須立即寫入後端並重試 */
  const handlePermModeCommitNow = (newMode) => {
    if (actionLock.current) return;
    actionLock.current = true;
    if (!commitPermMode(newMode)) {
      actionLock.current = false;
      showToast('連線中斷，操作未送出，請稍後再試', { error: true, duration: 3000 });
      return;
    }
    setPermTools([]);
  };

  // 點了之後等伺服器回應：避免對 Shell 允許／拒絕連點（這三顆不會立刻收起面板）
  const sendShellConfirm = (type) => {
    if (!canAct || !connected || shellConfirmPending || actionLock.current) return;
    actionLock.current = true;
    if (sendOrToast({ type })) setShellConfirmPending(true);
    else actionLock.current = false;
  };

  const handleShellApprove = () => {
    if (actionLock.current) return;
    actionLock.current = true;
    if (!sendOrToast({ type: 'shell_approve' })) { actionLock.current = false; return; }
    setShellPendingCmd(null);
  };

  const handleShellCancel = () => {
    if (actionLock.current) return;
    actionLock.current = true;
    if (!sendOrToast({ type: 'shell_cancel' })) { actionLock.current = false; return; }
    setShellPendingCmd(null);
  };

  useEffect(() => { setShellConfirmPending(false); actionLock.current = false; }, [state, shellRequest, permTools.length]);
  // shell-confirm-timeout：同一請求先擋住連點；10 秒內狀態與請求都沒變，視為這次送出沒有完成，解鎖後可再送。
  useEffect(() => {
    if (!shellConfirmPending) return undefined;
    const t = setTimeout(() => {
      setShellConfirmPending(false);
      actionLock.current = false;
    }, 10000);
    return () => clearTimeout(t);
  }, [shellConfirmPending]);
  return (<>
        {/* Shell 指令批准對話框 */}
        {state === 'SHELL_AWAITING_APPROVAL' && shellPendingCmd && canAct && (
          <div className="rounded-xl border border-amber-700/60 bg-amber-950/30 px-4 py-3 text-sm">
            <div className="text-amber-400 font-semibold mb-2">⚠️ 即將執行 Shell 指令</div>
            <div className="text-gray-400 text-xs mb-1">Shell：{shellPendingCmd.shell_type || shellType}</div>
            {shellPendingCmd.work_dir && (
              <div className="text-gray-400 text-xs mb-2">目錄：<span className="font-mono text-gray-300">{shellPendingCmd.work_dir}</span></div>
            )}
            <pre className="bg-gray-900/60 rounded px-3 py-2 text-xs font-mono text-gray-200 mb-3 whitespace-pre-wrap break-words">{shellPendingCmd.command}</pre>
            <div className="flex flex-wrap gap-2">
              <button type="button" disabled={!connected} onClick={handleShellApprove}
                className="min-h-11 px-3 py-1.5 bg-amber-700 hover:bg-amber-600 text-white rounded-lg text-xs font-medium">
                確認執行
              </button>
              <button type="button" disabled={!connected} onClick={handleShellCancel}
                className="min-h-11 px-3 py-1.5 bg-gray-700 hover:bg-gray-600 text-gray-300 rounded-lg text-xs">
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
          <div className="rounded-xl border border-yellow-700 bg-yellow-950/40 px-4 py-3 text-sm">
            <div className="text-yellow-400 font-semibold mb-2">需要授權</div>
            {permTools.map((t, i) => <PermToolDetail key={i} tool={t} />)}
            {permRetryTools && (
              <div className="mt-2 text-xs text-yellow-200/90">
                允許後會重試任務，並在本次重試中放行以上工具，不限於畫面上的單一操作。這次允許不會永久記住。
              </div>
            )}
            {!canAct && <div className="mt-2 text-xs text-yellow-200/70">等待有權限的人處理…</div>}
            <div className={canAct ? 'flex flex-wrap gap-2 mt-3' : 'hidden'}>
              <button type="button" disabled={!connected} onClick={handleAllowOnce}
                className="min-h-11 px-3 py-1.5 bg-yellow-700 hover:bg-yellow-600 text-white rounded-lg text-xs">
                {permRetryTools ? '允許工具並重試' : '允許此操作'}
              </button>
              {/* 切到 acceptEdits 只對編輯類工具有效；Bash 等會再被拒一次，所以只在全是編輯工具時顯示 */}
              {!isGuest && permTools.every((t) => EDIT_TOOL_NAMES.has(t.tool_name)) && (
                <button type="button" disabled={!connected} onClick={() => handlePermModeCommitNow('acceptEdits')}
                  className="min-h-11 px-3 py-1.5 bg-orange-700 hover:bg-orange-600 text-white rounded-lg text-xs">
                  允許並自動允許編輯
                </button>
              )}
              <button type="button" disabled={!connected} onClick={handleDenyOnce}
                className="min-h-11 px-3 py-1.5 bg-red-900 hover:bg-red-800 text-red-100 rounded-lg text-xs">
                拒絕
              </button>
            </div>
          </div>
        )}

        {state === 'AWAITING_SHELL_CONFIRM' && shellRequest && canAct && (
          <div className="rounded-xl border border-orange-700/90 bg-orange-950/35 px-4 py-3 text-sm">
            <div className="text-orange-300 font-semibold mb-2">允許執行 Shell 指令？</div>
            <div className="text-gray-200 text-xs font-mono break-words mb-1 whitespace-pre-wrap">{shellRequest.line}</div>
            <div className="text-gray-500 text-[10px] font-mono mb-2">
              指令名稱：<span className="text-orange-200/90">{shellRequest.command}</span>
              {shellRequest.workDirKey ? (
                <span className="block truncate mt-0.5" title={shellRequest.workDirKey}>目錄：{shellRequest.workDirKey}</span>
              ) : null}
            </div>
            <div className="flex flex-wrap gap-2 mt-2">
              <button type="button" onClick={() => sendShellConfirm('shell_allow_once')} disabled={!connected || shellConfirmPending}
                className="min-h-11 px-3 py-1.5 bg-orange-800 hover:bg-orange-700 text-white rounded-lg text-xs disabled:opacity-40">
                允許一次
              </button>
              <button type="button" onClick={() => sendShellConfirm('shell_allow_remember_workdir')} disabled={!connected || shellConfirmPending}
                className="min-h-11 px-3 py-1.5 bg-amber-900 hover:bg-amber-800 text-amber-100 rounded-lg text-xs disabled:opacity-40">
                允許並記住此目錄
              </button>
              <button type="button" onClick={() => sendShellConfirm('shell_deny')} disabled={!connected || shellConfirmPending}
                className="min-h-11 px-3 py-1.5 bg-gray-800 hover:bg-gray-700 text-gray-300 rounded-lg text-xs disabled:opacity-40">
                拒絕
              </button>
            </div>
          </div>
        )}

  </>);
}
