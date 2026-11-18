/** 新增／編輯工作室：名稱、從既有 session 勾選成員、逐一指定 PM 或執行者。
 *  room 為 null＝新增（POST /rooms）；否則編輯（PUT /rooms/:id，成員整批取代）。
 *  只存「誰在這個工作室」，不會建立、改動或刪除任何 session。 */
function RoomFormModal({ room, sessions, onClose, onSaved }) {
  const dialogRef = useRef(null);
  const [form, setForm] = useState(() => {
    const init = roomFormInit(room);
    // 成員的 session 已不存在時不帶進表單，否則存檔會被後端以「session 不存在」擋下。
    return { ...init, members: init.members.filter((m) => sessions.some((s) => s.id === m.session_id)) };
  });
  const [error, setError] = useState('');
  const [saving, setSaving] = useState(false);
  const picked = new Map(form.members.map((m) => [m.session_id, m.role]));
  const previousPM = room?.members.find((m) => m.role === 'pm');
  const nextPM = form.members.find((m) => m.role === 'pm');
  const handoff = previousPM && nextPM && previousPM.session_id !== nextPM.session_id;

  useLayoutEffect(() => {
    const dialog = dialogRef.current;
    dialog.showModal();
    dialog.focus({ preventScroll: true });
  }, []);

  const submit = async (e) => {
    e.preventDefault();
    const bad = roomFormError(form);
    if (bad) { setError(bad); return; }
    if (!nextPM) { setError('請指定一位 PM'); return; }
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
            <p className="mt-0.5 text-xs text-ra-t4">每個工作室有一位 PM。選另一位 PM 即可交接，原 PM 會改為執行者。</p>
            {sessions.length === 0 ? <div className="py-4 text-center text-xs text-ra-t4">目前沒有 session 可選</div> : (
              <ul className="mt-2 space-y-1">
                {sessions.map((s) => {
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
            <button type="submit" disabled={saving}
              className="rounded-lg bg-violet-600 px-4 py-2 text-sm font-semibold text-white hover:bg-violet-500 disabled:opacity-60">
              {saving ? '儲存中…' : (handoff ? '交接 PM 並儲存' : room ? '儲存' : '建立')}
            </button>
          </div>
        </footer>
      </form>
    </dialog>
  );
}
