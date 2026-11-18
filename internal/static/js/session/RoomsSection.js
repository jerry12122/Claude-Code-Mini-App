/** 側欄的「工作室」區塊：列表、新增、更多操作（編輯／刪除）。訪客看不到（也打不到 /rooms，後端一律 403）。
 *  刪除工作室只移除分組與工作室資料，不會刪除成員 session。 */

function RoomDeleteDialog({ room, onClose, onDeleted }) {
  const dialogRef = useRef(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');

  useLayoutEffect(() => {
    dialogRef.current.showModal();
  }, []);

  const confirmDelete = async () => {
    setBusy(true); setError('');
    try {
      const res = await apiFetch(roomUrl(room.id), { method: 'DELETE' });
      if (!res.ok && res.status !== 404) { setError(await readApiError(res, '刪除工作室失敗')); setBusy(false); return; }
      onDeleted(room);
      dialogRef.current?.close();
    } catch (_) {
      setError('網路錯誤，請稍後再試');
      setBusy(false);
    }
  };

  return (
    <dialog
      ref={dialogRef}
      tabIndex={-1}
      className="ra-dialog ra-dialog-confirm overflow-hidden rounded-2xl border border-ra-line bg-ra-bg shadow-2xl shadow-black/50 focus:outline-none"
      aria-labelledby="room-delete-title"
      aria-describedby="room-delete-desc"
      onClose={onClose}
    >
      <div className="px-5 py-4">
        <h2 id="room-delete-title" className="text-sm font-semibold text-ra-t1">刪除工作室「{room.name}」？</h2>
        <p id="room-delete-desc" className="mt-2 text-xs leading-relaxed text-ra-t3">
          只會移除這個工作室與成員分組，<strong className="text-ra-t1">不會刪除</strong>其中任何 session 或對話紀錄。此動作無法復原。
        </p>
        {error ? <div role="alert" className="mt-2 text-xs text-red-300">{error}</div> : null}
        <div className="mt-4 flex justify-end gap-2">
          {/* 破壞性確認：預設焦點放在安全的「取消」 */}
          <button type="button" autoFocus onClick={() => dialogRef.current?.close()}
            className="rounded-lg px-3 py-2 text-sm text-ra-t2 hover:bg-ra-raised">取消</button>
          <button type="button" onClick={confirmDelete} disabled={busy}
            className="rounded-lg bg-red-600 px-4 py-2 text-sm font-semibold text-white hover:bg-red-500 disabled:opacity-60">
            {busy ? '刪除中…' : '確認刪除'}
          </button>
        </div>
      </div>
    </dialog>
  );
}

function RoomsSection({ sessions, activeRoomId, onOpenRoom, onRoomDeleted, touch }) {
  const [rooms, setRooms] = useState([]);
  const [loaded, setLoaded] = useState(false);
  const [error, setError] = useState('');
  const [menu, setMenu] = useState(null);
  const [form, setForm] = useState(null);       // { room: null | detail }
  const [deleting, setDeleting] = useState(null);
  const closeMenu = useCallback(() => setMenu(null), []);
  const generation = useRef(0);

  const load = useCallback(async () => {
    const mine = ++generation.current;
    try {
      const res = await apiFetch(roomsUrl());
      if (mine !== generation.current) return;
      if (!res.ok) { setError(await readApiError(res, '載入工作室失敗')); return; }
      const data = await res.json();
      if (mine !== generation.current) return;
      setRooms(Array.isArray(data) ? data : []);
      setError(''); setLoaded(true);
    } catch (_) {
      if (mine === generation.current) setError('載入工作室失敗');
    }
  }, []);

  useEffect(() => {
    if (isGuestMode) return undefined;
    load();
    window.addEventListener(ROOMS_CHANGED_EVENT, load);
    return () => { ++generation.current; window.removeEventListener(ROOMS_CHANGED_EVENT, load); };
  }, [load]);

  // session 被刪除會連帶移除成員，成員數要跟著更新。
  const sessionCount = sessions.length;
  useEffect(() => { if (!isGuestMode && loaded) load(); }, [sessionCount]);

  if (isGuestMode) return null;

  const notify = () => window.dispatchEvent(new Event(ROOMS_CHANGED_EVENT));
  const menuRoom = menu ? rooms.find((r) => r.id === menu.roomId) : null;

  const openEdit = async (room) => {
    try {
      const res = await apiFetch(roomUrl(room.id));
      if (!res.ok) { showToast(await readApiError(res, '載入工作室失敗'), { error: true }); return; }
      setForm({ room: await res.json() });
    } catch (_) {
      showToast('載入工作室失敗', { error: true });
    }
  };

  const menuItems = menuRoom ? [
    { label: '編輯／交接 PM', onSelect: () => openEdit(menuRoom) },
    { label: '刪除', danger: true, onSelect: () => setDeleting(menuRoom) },
  ] : [];

  return (
    <section className="mt-2" aria-label="工作室">
      <div className="flex items-center gap-1 px-2 py-1.5">
        <h2 className="flex min-w-0 flex-1 items-center gap-1.5 text-[12.5px] font-bold text-ra-t3">
          <svg viewBox="0 0 24 24" className="h-3.5 w-3.5 shrink-0" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true"><path d="M4 18v-1a4 4 0 0 1 4-4h4a4 4 0 0 1 4 4v1m2-5a4 4 0 0 1 3 4v1M13 7a3 3 0 1 1-6 0 3 3 0 0 1 6 0Zm4-3a3 3 0 0 1 0 6" /></svg>
          工作室
        </h2>
        <button type="button" onClick={() => setForm({ room: null })} aria-label="新增工作室" title="新增工作室"
          className="shrink-0 rounded-md px-1.5 py-1 text-[13px] leading-none font-semibold text-violet-400 hover:bg-violet-500/10 hover:text-violet-300 focus:outline-none focus-visible:ring-2 focus-visible:ring-violet-500/50">＋</button>
      </div>
      {error ? <div role="alert" className="px-2 pb-1 text-[11px] text-red-300">{error}</div> : null}
      {loaded && rooms.length === 0 && !error ? <div className="px-3 pb-2 text-[11px] text-ra-t5">尚無工作室，點 ＋ 建立</div> : null}
      <ul className="ml-[18px] border-l border-ra-line-soft pl-2.5">
        {rooms.map((r) => {
          const active = r.id === activeRoomId;
          return (
            <li key={r.id} className={'ra-session-card group/card' + (active ? ' active' : '')}>
              <div className="flex min-w-0 items-center gap-2">
                <button type="button" onClick={() => onOpenRoom(r.id)} aria-current={active ? 'true' : undefined}
                  title={`${r.name}（${r.member_count} 位成員）`}
                  className="flex min-w-0 flex-1 items-center gap-2 text-left focus:outline-none focus-visible:ring-2 focus-visible:ring-violet-400 rounded">
                  <span className="min-w-0 flex-1 truncate text-[13.5px] font-medium text-ra-t3">{r.name}</span>
                  <span className="shrink-0 text-[11px] text-ra-t5">{r.member_count}</span>
                </button>
                <button type="button" data-context-surface aria-label={`更多操作：${r.name}`} aria-haspopup="menu"
                  className="shrink-0 min-w-[32px] min-h-[32px] rounded-lg text-ra-t4 hover:bg-ra-raised hover:text-ra-t1 focus-visible:ring-2 focus-visible:ring-violet-400"
                  onClick={(e) => { e.stopPropagation(); setMenu(openContextPayload(e, { roomId: r.id }, touch)); }}>⋯</button>
              </div>
            </li>
          );
        })}
      </ul>
      <ContextMenu menu={menuRoom ? menu : null} items={menuItems} onClose={closeMenu} label="工作室操作" />
      {form ? (
        <RoomFormModal room={form.room} sessions={sessions} onClose={() => setForm(null)} onSaved={notify} />
      ) : null}
      {deleting ? (
        <RoomDeleteDialog room={deleting} onClose={() => setDeleting(null)}
          onDeleted={(r) => { notify(); onRoomDeleted(r.id); showToast('已刪除工作室，session 未受影響'); }} />
      ) : null}
    </section>
  );
}
