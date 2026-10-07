// ── 新建 Session 畫面（顯示於右側對話區，聊天式起手輸入） ─────────────────────────
const NEW_SESSION_AGENT_OPTIONS = [
  { value: 'claude', label: 'Claude' },
  { value: 'cursor', label: 'Cursor' },
  { value: 'kiroacp', label: 'Kiro ACP' },
  { value: 'codex', label: 'Codex' },
];

// 沒填名稱時的自動命名：有訊息就取首行，否則用 runner + 時間
function autoSessionName(agentType, message) {
  const text = String(message || '').trim();
  if (text) return text.split('\n')[0].slice(0, 40);
  const label = AGENT_LABEL[agentType] || agentType || 'Session';
  const time = new Date().toLocaleTimeString('zh-TW', { hour: '2-digit', minute: '2-digit', second: '2-digit' });
  return `${label} · ${time}`;
}

function NewSessionComposer({ prefill, onCreated, onCancel }) {
  const newForm = useNewSessionForm(prefill || {});
  const [message, setMessage] = useState(prefill?.message || '');
  const [loading, setLoading] = useState(false);
  const [errText, setErrText] = useState('');
  const [advancedOpen, setAdvancedOpen] = useState(false);
  const messageInputRef = useRef(null);

  useEffect(() => {
    messageInputRef.current?.focus();
  }, []);

  // codex/kiro 沒有可切換的權限模式（固定行為），不顯示切換器；kiroacp 有互動/全自動兩種
  const showPermMode = !['codex', 'kiro'].includes(newForm.agent);

  const submit = async () => {
    if (loading) return;
    setLoading(true);
    setErrText('');
    try {
      const finalName = newForm.name.trim() || autoSessionName(newForm.agent, message);
      const res = await apiFetch('/sessions', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(newForm.buildCreatePayload({ name: finalName })),
      });
      if (!res.ok) {
        const j = await res.json().catch(() => ({}));
        throw new Error(j.error || `建立失敗 (${res.status})`);
      }
      const created = await res.json();
      const trimmedMsg = message.trim();
      if (trimmedMsg) {
        await sendFirstPromptOrDraft(created.id, trimmedMsg);
      }
      onCreated(created);
    } catch (e) {
      setErrText(e.message || String(e));
    } finally {
      setLoading(false);
    }
  };

  const handleMessageKeyDown = (e) => {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault();
      submit();
    }
  };

  return (
    <div className="h-full overflow-y-auto app-scroll flex flex-col items-center justify-center px-4 py-10 bg-[oklch(0.15_0.02_264)]">
      <div className="w-full max-w-xl space-y-4">
        <div className="flex items-center justify-between">
          <div className="ra-display text-lg text-[oklch(0.94_0.01_264)]">新建 Session</div>
          <button
            type="button"
            onClick={onCancel}
            aria-label="取消"
            title="取消"
            className="text-[oklch(0.5_0.01_264)] hover:text-gray-300 text-xl leading-none px-1"
          >
            ×
          </button>
        </div>

        {/* Runner 選擇 */}
        <div className="flex flex-wrap gap-1.5">
          {NEW_SESSION_AGENT_OPTIONS.map((opt) => {
            const active = newForm.agent === opt.value;
            return (
              <button
                key={opt.value}
                type="button"
                onClick={() => newForm.setAgent(opt.value)}
                className={`inline-flex items-center gap-1.5 rounded-[9px] border px-3 py-1.5 text-xs font-semibold transition-colors ${
                  active
                    ? 'border-violet-500/60 bg-violet-500/15 text-violet-200'
                    : 'border-[oklch(0.28_0.02_264)] bg-[oklch(0.19_0.02_264)] text-[oklch(0.6_0.01_264)] hover:text-[oklch(0.85_0.01_264)]'
                }`}
              >
                <span className={`inline-flex items-center justify-center w-[18px] h-[18px] rounded-[5px] ${getAgentBadgeClass(opt.value)}`}>
                  <AgentBadgeIcon agentType={opt.value} />
                </span>
                {opt.label}
              </button>
            );
          })}
        </div>

        {/* 權限模式：僅該 runner 有可切換的模式時顯示 */}
        {showPermMode && (
          <div>
            <div className="text-[10px] uppercase tracking-wide text-gray-500 mb-1">權限模式</div>
            <PermModeSwitch agentType={newForm.agent} value={newForm.mode} onChange={newForm.setMode} />
          </div>
        )}

        {/* Session 名稱（選填，留空自動產生） */}
        <input
          value={newForm.name}
          onChange={(e) => newForm.setName(e.target.value)}
          placeholder="Session 名稱（選填，留空自動產生）"
          className="w-full bg-gray-800 border border-gray-700 rounded-lg px-3 py-2 text-sm text-gray-200 placeholder-gray-500 focus:outline-none focus:border-violet-600"
        />

        {/* 中央輸入：第一則訊息（chat-input 風格，選填） */}
        <div className="rounded-2xl border border-[oklch(0.28_0.02_264)] bg-[oklch(0.19_0.02_264)] px-4 py-3 focus-within:border-violet-600 transition-colors">
          <textarea
            ref={messageInputRef}
            value={message}
            onChange={(e) => setMessage(e.target.value)}
            onKeyDown={handleMessageKeyDown}
            placeholder="跟這個新 Session 說些什麼…（選填，留空只建立空白對話）"
            rows={3}
            className="w-full resize-none bg-transparent border-0 p-0 text-[15px] leading-relaxed text-[oklch(0.94_0.01_264)] placeholder-[oklch(0.5_0.01_264)] focus:outline-none"
          />
        </div>

        <WorkDirPicker value={newForm.workDir} onChange={newForm.setWorkDir} />

        {/* 進階設定（預設收合） */}
        <div className="rounded-lg border border-[oklch(0.26_0.02_264)]">
          <button
            type="button"
            onClick={() => setAdvancedOpen((v) => !v)}
            className="w-full flex items-center justify-between px-3 py-2 text-xs text-gray-400 hover:text-gray-200"
          >
            <span>進階設定</span>
            <span className={`transition-transform ${advancedOpen ? 'rotate-180' : ''}`} aria-hidden>▾</span>
          </button>
          {advancedOpen && (
            <div className="px-3 pb-3 space-y-2.5 border-t border-[oklch(0.26_0.02_264)] pt-2.5">
              {newForm.agent !== 'antigravity' && (
                <div>
                  <div className="text-[10px] uppercase tracking-wide text-gray-500 mb-1">Model（選填，寫入 --model）</div>
                  <input
                    value={newForm.model}
                    onChange={(e) => newForm.setModel(e.target.value)}
                    placeholder="例如 sonnet、claude-sonnet-4.6、auto"
                    className="w-full bg-gray-800 border border-gray-700 rounded-lg px-3 py-2 text-sm text-gray-200 placeholder-gray-500 font-mono focus:outline-none focus:border-violet-600"
                  />
                </div>
              )}
              {newForm.agent !== 'cursor' && (
                <div>
                  <div className="text-[10px] uppercase tracking-wide text-gray-500 mb-1">Effort（選填）</div>
                  <EffortSelect
                    value={newForm.effort}
                    onChange={newForm.setEffort}
                    id="new-session-effort"
                    className="w-full py-2 text-sm"
                  />
                </div>
              )}
              {newForm.agent === 'claude' && (
                <div>
                  <div className="text-[10px] uppercase tracking-wide text-gray-500 mb-1">自訂 CLI 引數（選填）</div>
                  <textarea
                    value={newForm.cliExtra}
                    onChange={(e) => newForm.setCliExtra(e.target.value)}
                    placeholder={'每行一個引數，例如：\n--plugin-dir\n./.claude/plugins/crm'}
                    rows={4}
                    spellCheck={false}
                    className="w-full bg-gray-800 border border-gray-700 rounded-lg px-3 py-2 text-sm text-gray-200 placeholder-gray-500 font-mono leading-relaxed overflow-y-auto app-scroll focus:outline-none focus:border-violet-600"
                  />
                </div>
              )}
            </div>
          )}
        </div>

        {errText ? <div className="text-xs text-red-400">{errText}</div> : null}

        <div className="flex justify-end gap-2">
          <button
            type="button"
            onClick={onCancel}
            className="px-3 py-2 bg-gray-700 hover:bg-gray-600 text-gray-300 rounded-lg text-sm"
          >
            取消
          </button>
          <button
            type="button"
            onClick={submit}
            disabled={loading}
            className="px-4 py-2 bg-violet-600 hover:bg-violet-500 disabled:opacity-40 text-white rounded-lg text-sm font-medium"
          >
            {loading ? '建立中…' : message.trim() ? '建立並送出' : '建立'}
          </button>
        </div>
      </div>
    </div>
  );
}
