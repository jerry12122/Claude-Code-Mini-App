/** 新增／編輯工作室：名稱、從既有 session 勾選成員、逐一指定 PM 或執行者。
 *  room 為 null＝新增（POST /rooms）；否則編輯（PUT /rooms/:id，成員整批取代）。
 *  可另行建立 session 加入；不啟動 agent，不改動或刪除既有 session。 */
function RoomFormModal({ room, sessions, onClose, onSaved }) {
  const dialogRef = useRef(null);
  const [form, setForm] = useState(() => {
    const init = roomFormInit(room);
    // 成員的 session 已不存在時不帶進表單，否則存檔會被後端以「session 不存在」擋下。
    return { ...init, members: init.members.filter((m) => sessions.some((s) => s.id === m.session_id)) };
  });
  const [error, setError] = useState('');
  const [saving, setSaving] = useState(false);
  const [query, setQuery] = useState('');
  const newForm = useNewSessionForm();
  const [createOpen, setCreateOpen] = useState(false);
  const [creating, setCreating] = useState(false);
  const [createError, setCreateError] = useState('');
  const [createdSessions, setCreatedSessions] = useState([]);
  const creatingRef = useRef(false);
  const createToggleRef = useRef(null);
  const searchId = React.useId();
  const allSessions = [...new Map([...createdSessions, ...sessions].map((s) => [s.id, s])).values()];
  const needle = query.trim().toLowerCase();
  const visibleSessions = allSessions.filter((s) => String(s.name || '').toLowerCase().includes(needle) || String(s.id).toLowerCase().includes(needle));
  const picked = new Map(form.members.map((m) => [m.session_id, m.role]));
  const previousPM = room?.members.find((m) => m.role === 'pm');
  const nextPM = form.members.find((m) => m.role === 'pm');
  const handoff = previousPM && nextPM && previousPM.session_id !== nextPM.session_id;
  const preview = room?.handoff;
  const blocked = !!(handoff && preview?.busy);

  useLayoutEffect(() => {
    const dialog = dialogRef.current;
    dialog.showModal();
    dialog.focus({ preventScroll: true });
  }, []);

  useLayoutEffect(() => {
    if (createdSessions.length) createToggleRef.current?.focus();
  }, [createdSessions]);

  const submit = async (e) => {
    e.preventDefault();
    if (createOpen) return;
    const bad = roomFormError(form);
    if (bad) { setError(bad); return; }
    if (!nextPM) { setError('請指定一位 PM'); return; }
    if (handoff && preview?.busy) { setError('現任 PM 正在執行，或還有未完成的授權。先處理完才能交接。'); return; }
    setSaving(true); setError('');
    try {
      const res = await apiFetch(room ? roomUrl(room.id) : roomsUrl(), {
        method: room ? 'PUT' : 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ ...roomFormBody(form), previous_pm_id: room ? previousPM?.session_id || '' : undefined }),
      });
      if (!res.ok) { setError(await readApiError(res, room ? '儲存工作室失敗' : '建立工作室失敗')); setSaving(false); return; }
      const saved = await res.json();
      onSaved(saved);
      dialogRef.current?.close();
    } catch (_) {
      setError('網路錯誤，請稍後再試');
      setSaving(false);
    }
  };

  const closeCreate = () => {
    setCreateOpen(false);
    createToggleRef.current?.focus();
  };
  const createSession = async () => {
    if (creatingRef.current) return;
    if (!newForm.name.trim()) { setCreateError('請填寫新會話名稱'); return; }
    creatingRef.current = true;
    setCreating(true); setCreateError('');
    try {
      const res = await apiFetch('/sessions', {
        method: 'POST', headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(newForm.buildCreatePayload()),
      });
      if (!res.ok) throw new Error(await readApiError(res, '建立 session 失敗'));
      const created = await res.json();
      if (typeof created?.id !== 'string' || !created.id) throw new Error('建立 session 回應缺少 ID');
      setCreatedSessions((list) => [created, ...list.filter((s) => s.id !== created.id)]);
      setForm((f) => ({ ...f, members: f.members.some((m) => m.session_id === created.id) ? f.members : [...f.members, { session_id: created.id, role: 'worker' }] }));
      setQuery('');
      closeCreate();
      newForm.reset();
      window.dispatchEvent(new Event('miniapp:sessions-refresh'));
    } catch (err) {
      setCreateError(err.message || '建立 session 失敗，請重試');
    } finally {
      creatingRef.current = false;
      setCreating(false);
    }
  };

  const title = room ? '編輯工作室' : '新增工作室';
  return (
    <dialog
      ref={dialogRef}
      tabIndex={-1}
      className="ra-dialog ra-dialog-room overflow-hidden rounded-2xl border border-ra-line bg-ra-bg shadow-2xl shadow-black/50 focus:outline-none"
      aria-labelledby="room-form-title"
      onClose={onClose}
      onClick={(e) => {
        if (e.target !== e.currentTarget) return;
        const r = e.currentTarget.getBoundingClientRect();
        if (e.clientX < r.left || e.clientX > r.right || e.clientY < r.top || e.clientY > r.bottom) e.currentTarget.close();
      }}
    >
      <form onSubmit={submit} className="flex min-h-0 flex-1 flex-col">
        <header className="flex shrink-0 items-center gap-2 border-b border-ra-line-soft px-4 py-3">
          <h2 id="room-form-title" className="min-w-0 flex-1 truncate text-sm font-semibold text-ra-t1">{title}</h2>
          <button type="button" aria-label="關閉" onClick={() => dialogRef.current?.close()}
            className="flex h-9 w-9 shrink-0 items-center justify-center rounded-lg text-ra-t2 hover:bg-ra-raised">
            <svg viewBox="0 0 24 24" className="h-5 w-5" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" aria-hidden="true"><path d="m6 6 12 12M6 18 18 6" /></svg>
          </button>
        </header>
        <div className="min-h-0 flex-1 overflow-y-auto app-scroll px-4 py-3">
          <label htmlFor="room-name" className="block text-xs font-semibold text-ra-t3">名稱</label>
          <input id="room-name" value={form.name} maxLength={ROOM_NAME_MAX} autoComplete="off"
            onChange={(e) => setForm((f) => ({ ...f, name: e.target.value }))}
            className="mt-1 w-full rounded-lg border border-ra-line bg-ra-card px-3 py-2 text-sm text-ra-t1 focus:border-violet-500 focus:outline-none" />
          <fieldset className="mt-4 min-w-0">
            <legend className="text-xs font-semibold text-ra-t3">成員（已選 {form.members.length}）</legend>
            <p className="mt-0.5 text-xs text-ra-t4">每個工作室有一位 PM。選另一位 PM 即可交接，原 PM 會改為執行者，名稱不會被改寫。</p>
            {handoff ? <div className="mt-2 rounded-lg border border-ra-line-soft bg-ra-card p-2 text-xs text-ra-t2">
              <p>現任 {preview?.pm_name || previousPM.session_name || 'PM'}，接任 {allSessions.find((s) => s.id === nextPM.session_id)?.name || '新 PM'}。</p>
              {preview ? <p>將移交群聊輸入 {preview.inputs} 則、回報 {preview.reports} 則。新 PM 自己的暫停與私人佇列維持原狀。</p>
                : <p>尚未取得移交數量。送出時仍由伺服器檢查。</p>}
              {preview?.busy ? <p>現任 PM 正在執行，或還有未完成的授權。先處理完才能交接，不會被中斷。</p> : null}
              {preview?.paused && !preview.busy ? <p>現任 PM 的佇列已暫停{preview.queue_length ? `，還有 ${preview.queue_length} 則` : ''}。這樣也可以交接。</p> : null}
            </div> : null}
            <button ref={createToggleRef} type="button" aria-expanded={createOpen} aria-controls={searchId + '-create'}
              disabled={creating} onClick={() => createOpen ? closeCreate() : setCreateOpen(true)}
              className="mt-2 rounded-lg px-3 py-2 text-sm text-ra-t2 hover:bg-ra-raised">建立 session</button>
            {createOpen ? <section id={searchId + '-create'} aria-label="建立 session" className="mt-2 rounded-lg border border-ra-line-soft bg-ra-card p-3">
              <fieldset disabled={creating} className="min-w-0 space-y-2" onKeyDown={(e) => {
                if (e.target.tagName === 'INPUT' && e.key === 'Enter' && !e.nativeEvent.isComposing && e.keyCode !== 229) e.preventDefault();
              }}>
                <legend className="text-sm font-semibold text-ra-t1">新 session</legend>
                <label htmlFor={searchId + '-name'} className="block text-xs text-ra-t3">新 session 名稱</label>
                <input id={searchId + '-name'} value={newForm.name} onChange={(e) => newForm.setName(e.target.value)}
                  className="w-full rounded-lg border border-ra-line bg-ra-bg px-3 py-2 text-base text-ra-t1 focus:border-violet-500 focus:outline-none" />
                <label htmlFor={searchId + '-agent'} className="block text-xs text-ra-t3">載具</label>
                <select id={searchId + '-agent'} value={newForm.agent} onChange={(e) => newForm.setAgent(e.target.value)}
                  className="w-full rounded-lg border border-ra-line bg-ra-bg px-3 py-2 text-base text-ra-t1">
                  {NEW_SESSION_AGENT_OPTIONS.map((opt) => <option key={opt.value} value={opt.value}>{opt.label}</option>)}
                </select>
                <WorkDirPicker value={newForm.workDir} onChange={newForm.setWorkDir} />
                <label htmlFor={searchId + '-model'} className="block text-xs text-ra-t3">Model</label>
                <ModelSelect agentType={newForm.agent} value={newForm.model} onChange={newForm.setModel} id={searchId + '-model'} />
                <label htmlFor={searchId + '-mode'} className="block text-xs text-ra-t3">權限模式</label>
                <PermModeSelect agentType={newForm.agent} value={newForm.mode} onChange={newForm.setMode}
                  disabled={['codex', 'kiro'].includes(newForm.agent)} id={searchId + '-mode'} />
                <p className="text-xs text-ra-t4">取消工作室不會刪除已建立的 session。</p>
                {createError ? <p role="alert" className="text-xs text-red-300">{createError}</p> : null}
                <div className="flex flex-wrap gap-2">
                  <button type="button" onClick={createSession} className="rounded-lg bg-violet-600 px-3 py-2 text-sm text-white">{creating ? '建立中…' : '確認建立 session'}</button>
                  <button type="button" onClick={closeCreate} className="rounded-lg px-3 py-2 text-sm text-ra-t3 hover:bg-ra-raised">取消建立 session</button>
                </div>
              </fieldset>
            </section> : null}
            <search className="mt-2">
              <label htmlFor={searchId} className="block text-xs font-semibold text-ra-t3">搜尋 session（名稱或 ID）</label>
              <input id={searchId} name="session-search" type="search" value={query} placeholder="輸入名稱或 ID"
                autoComplete="off" autoCapitalize="none" autoCorrect="off" spellCheck={false} enterKeyHint="search"
                onChange={(e) => setQuery(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === 'Enter' && !e.nativeEvent.isComposing && e.keyCode !== 229) e.preventDefault();
                }}
                className="mt-1 w-full rounded-lg border border-ra-line bg-ra-card px-3 py-3 text-base text-ra-t1 focus:border-violet-500 focus:outline-none focus-visible:ring-2 focus-visible:ring-violet-500/50" />
            </search>
            {allSessions.length === 0 ? <div className="py-4 text-center text-xs text-ra-t4">目前沒有 session 可選</div> : visibleSessions.length === 0 ?
              <p role="status" className="py-4 text-center text-xs text-ra-t4">沒有符合搜尋的 session</p> : (
              <ul className="mt-2 space-y-1">
                {visibleSessions.map((s) => {
                  const role = picked.get(s.id);
                  const on = role !== undefined;
                  return (
                    <li key={s.id} className={'flex items-center gap-2 rounded-lg border px-2 py-1.5 ' + (on ? 'border-violet-500/40 bg-violet-500/10' : 'border-ra-line-soft')}>
                      <label className="flex min-w-0 flex-1 cursor-pointer items-center gap-2">
                        <input type="checkbox" checked={on} disabled={s.id === previousPM?.session_id} className="h-4 w-4 shrink-0 accent-violet-500"
                          onChange={() => setForm((f) => ({ ...f, members: roomFormToggle(f.members, s.id) }))} />
                        <span className={`inline-flex h-[20px] w-[20px] shrink-0 items-center justify-center rounded ${getAgentBadgeClass(s.agent_type, s)}`} aria-hidden="true">
                          <AgentBadgeIcon agentType={s.agent_type} session={s} />
                        </span>
                        <span className="min-w-0 truncate text-[13px] text-ra-t2">{s.name || '未命名'}</span>
                      </label>
                      {on ? (
                        <select aria-label={`${s.name || '未命名'} 的角色`} value={role}
                          onChange={(e) => setForm((f) => ({ ...f, members: roomFormSetRole(f.members, s.id, e.target.value) }))}
                          className="shrink-0 rounded-md border border-ra-line bg-ra-card px-1.5 py-1 text-xs text-ra-t1">
                          <option value="pm" disabled={s.agent_type === 'telegram' || s.input_mode === 'shell'}>{ROOM_ROLE_LABEL.pm}</option>
                          <option value="worker">{ROOM_ROLE_LABEL.worker}</option>
                        </select>
                      ) : null}
                    </li>
                  );
                })}
              </ul>
            )}
          </fieldset>
        </div>
        <footer className="shrink-0 border-t border-ra-line-soft px-4 py-3">
          {error ? <div role="alert" className="mb-2 text-xs text-red-300">{error}</div> : null}
          <div className="flex justify-end gap-2">
            <button type="button" onClick={() => dialogRef.current?.close()}
              className="rounded-lg px-3 py-2 text-sm text-ra-t3 hover:bg-ra-raised">取消</button>
            <button type="submit" disabled={saving || blocked || createOpen}
              className="rounded-lg bg-violet-600 px-4 py-2 text-sm font-semibold text-white hover:bg-violet-500 disabled:opacity-60">
              {saving ? '儲存中…' : (handoff ? '交接 PM 並儲存' : room ? '儲存' : '建立')}
            </button>
          </div>
        </footer>
      </form>
    </dialog>
  );
}
