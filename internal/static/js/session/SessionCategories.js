// 工作目錄內的單層分類。名稱用字碼數，不用 UTF-16 length。不把分類存進 localStorage。

const CATEGORY_PIN_GROUP = '釘選';
const CATEGORY_TG_GROUP = 'Telegram 聊天室';
const CATEGORY_UNSET_GROUP = '（未設定工作目錄）';

function categoryCodePoints(value) {
  return Array.from(String(value ?? '')).length;
}

function categoryNameError(raw) {
  const name = String(raw ?? '').trim();
  const count = categoryCodePoints(name);
  if (count < 1) return '分類名稱不可為空';
  if (count > 80) return '分類名稱最多 80 個字';
  return '';
}

function virtualCategoryDir(dirKey) {
  return dirKey === CATEGORY_PIN_GROUP || dirKey === CATEGORY_TG_GROUP || dirKey === CATEGORY_UNSET_GROUP;
}

function sessionCategoryDirs(sessions) {
  const dirs = [];
  for (const s of sessions || []) {
    if (s.agent_type === 'telegram' && !s.disguise_agent) continue;
    const key = String(s.work_dir ?? '').trim();
    if (key && !dirs.includes(key)) dirs.push(key);
  }
  return dirs;
}

function categoryWorkDirs(categories) {
  const dirs = [];
  for (const c of categories || []) {
    const key = String(c.work_dir ?? '').trim();
    if (key && !dirs.includes(key)) dirs.push(key);
  }
  return dirs;
}

// 無搜尋時，已有分類的實體目錄即使會話全部釘選、或目錄裡已無會話，也要留在分組裡。
function appendUnseenCategoryDirs(map, categories, searching) {
  if (searching) return;
  for (const key of categoryWorkDirs(categories)) {
    if (!map.has(key)) map.set(key, []);
  }
}

async function categorySourceDirs(sessions, signal) {
  const dirs = sessionCategoryDirs(sessions);
  const res = await apiFetch('/work-dirs', { signal });
  if (!res.ok) return dirs;
  const data = await res.json().catch(() => []);
  if (!Array.isArray(data)) return dirs;
  for (const item of data) {
    const key = String(item ?? '').trim();
    if (key && !dirs.includes(key)) dirs.push(key);
  }
  return dirs;
}

async function readCategoryError(res, fallback) {
  const body = await res.json().catch(() => ({}));
  return body.error || fallback;
}

async function fetchSessionCategories(dirs, signal) {
  const lists = await Promise.all(dirs.map(async (dir) => {
    const res = await apiFetch('/session-categories?work_dir=' + encodeURIComponent(dir), { signal });
    if (!res.ok) throw new Error(await readCategoryError(res, '分類載入失敗'));
    const data = await res.json();
    if (!Array.isArray(data)) throw new Error('無法讀取分類');
    return data;
  }));
  return lists.flat();
}

async function createSessionCategory(workDir, name) {
  const res = await apiFetch('/session-categories', {
    method: 'POST', headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ work_dir: workDir, name }),
  });
  if (res.status !== 201) throw new Error(await readCategoryError(res, '建立分類失敗'));
}

async function renameSessionCategory(id, name) {
  const res = await apiFetch('/session-categories/' + encodeURIComponent(id), {
    method: 'PATCH', headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ name }),
  });
  if (!res.ok) throw new Error(await readCategoryError(res, '重新命名分類失敗'));
}

async function deleteSessionCategory(id) {
  const res = await apiFetch('/session-categories/' + encodeURIComponent(id), { method: 'DELETE' });
  if (res.status !== 204) throw new Error(await readCategoryError(res, '刪除分類失敗'));
}

async function assignSessionCategory(sessionId, categoryId) {
  const res = await apiFetch('/sessions/' + encodeURIComponent(sessionId), {
    method: 'PATCH', headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ category_id: categoryId }),
  });
  if (!res.ok) throw new Error(await readCategoryError(res, '歸類失敗'));
}

function sessionCategoryMenuItems(session, categories, onAssign) {
  if (!session || (session.agent_type === 'telegram' && !session.disguise_agent)) return [];
  const dir = String(session.work_dir ?? '').trim();
  if (!dir) return [];
  const cats = (categories || []).filter((c) => c.work_dir === dir)
    .slice().sort((a, b) => a.name.localeCompare(b.name, 'zh-Hant'));
  return [
    { separator: true },
    { label: '未分類', disabled: !session.category_id, onSelect: () => onAssign(session, null) },
    ...cats.map((c) => ({
      label: '歸類：' + c.name,
      disabled: session.category_id === c.id,
      onSelect: () => onAssign(session, c.id),
    })),
  ];
}

function categorySections(dirKey, dirSessions, categories, searching) {
  const named = (categories || []).filter((c) => c.work_dir === dirKey)
    .slice().sort((a, b) => a.name.localeCompare(b.name, 'zh-Hant'));
  const known = new Set(named.map((c) => c.id));
  const buckets = new Map(named.map((c) => [c.id, []]));
  const loose = [];
  for (const s of dirSessions) {
    if (s.category_id && known.has(s.category_id)) buckets.get(s.category_id).push(s);
    else loose.push(s);
  }
  const sections = named
    .filter((c) => !searching || buckets.get(c.id).length)
    .map((c) => ({ key: c.id, label: c.name, category: c, sessions: buckets.get(c.id) }));
  if (loose.length) sections.push({ key: 'uncategorized:' + dirKey, label: '未分類', category: null, sessions: loose });
  return sections;
}

function SessionDirectoryGroups({
  groups, searching, collapsedDirs, onToggleDir, onCreateInDir, categories,
  renderSession, isWideScreen, onOpenDirMenu, onOpenCategoryMenu,
}) {
  return groups.map(([dirKey, dirSessions]) => {
    const expanded = searching || !collapsedDirs.has(dirKey);
    const virtual = virtualCategoryDir(dirKey);
    const shortLabel = workDirGroupShortLabel(dirKey);
    const dirBranch = virtual ? '' : (dirSessions.find((s) => s.git_branch) || {}).git_branch;
    const sections = virtual ? [] : categorySections(dirKey, dirSessions, categories, searching);
    return (
      <div key={dirKey} className="space-y-0">
        <div className="group/dir flex w-full min-w-0 items-center gap-1 px-2 py-1.5 mt-2 transition-colors hover:bg-violet-500/[0.06]">
          <button
            type="button"
            onClick={() => onToggleDir(dirKey)}
            className="flex min-w-0 flex-1 items-center gap-1.5 rounded-md px-1 py-1 text-left focus:outline-none focus-visible:ring-2 focus-visible:ring-violet-500/50"
          >
            <span className="shrink-0 text-ra-t5 w-3 text-center text-[10px]" aria-hidden>{expanded ? '▾' : '▸'}</span>
            <span className="shrink-0 text-[13px] leading-none" aria-hidden>{dirKey === CATEGORY_PIN_GROUP ? '📌' : '📁'}</span>
            <span className="min-w-0 flex-1 truncate text-[12.5px] text-ra-t3 font-bold" title={dirKey + (expanded ? '' : `（${dirSessions.length}）`)}>{shortLabel}</span>
          </button>
          {dirBranch ? <span className="ra-mono shrink-0 max-w-[8rem] truncate text-[11px] text-ra-t5" title={dirBranch}>{dirBranch}</span> : null}
          {!virtual && (
            <button
              type="button"
              aria-label={`管理 ${dirKey} 的分類`}
              title="管理分類"
              className="inline-flex min-h-11 min-w-11 shrink-0 items-center justify-center rounded-md px-2 text-[12px] font-semibold text-violet-300 hover:bg-violet-500/10 focus:outline-none focus-visible:ring-2 focus-visible:ring-violet-500/50"
              onClick={(event) => onOpenDirMenu(openContextPayload(event, { dirKey }, !isWideScreen))}
            >分類</button>
          )}
          {dirKey !== CATEGORY_PIN_GROUP && (
            <button
              type="button"
              onClick={() => onCreateInDir(dirKey)}
              aria-label="在此目錄建立新 Session"
              title="在此目錄建立新 Session"
              className="shrink-0 rounded-md px-1.5 py-1 text-[13px] leading-none font-semibold text-violet-400 hover:bg-violet-500/10 focus:outline-none focus-visible:ring-2 focus-visible:ring-violet-500/50 sm:opacity-0 sm:group-hover/dir:opacity-100 transition-opacity"
            >＋</button>
          )}
        </div>
        {expanded && (
          <div className="ml-[18px] border-l border-ra-line-soft pl-2.5">
            {virtual ? dirSessions.map((s) => renderSession(s)) : sections.map((section) => (
              <div key={section.key} className="mt-1">
                <div className="flex min-w-0 items-center gap-1 px-1">
                  <span data-category-label={section.label} className="min-w-0 flex-1 truncate text-[12px] font-semibold text-ra-t4">{section.label}</span>
                  {section.category && (
                    <button
                      type="button"
                      aria-label={`分類操作：${section.label}`}
                      className="inline-flex min-h-11 min-w-11 shrink-0 items-center justify-center rounded-md text-ra-t4 hover:bg-ra-raised focus:outline-none focus-visible:ring-2 focus-visible:ring-violet-500/50"
                      onClick={(event) => onOpenCategoryMenu(openContextPayload(event, { category: section.category }, !isWideScreen))}
                    >⋯</button>
                  )}
                </div>
                {section.sessions.map((s) => renderSession(s))}
              </div>
            ))}
          </div>
        )}
      </div>
    );
  });
}

function CategoryNameDialog({ mode, initial, onClose, onSubmit }) {
  const dialogRef = useRef(null);
  const [value, setValue] = useState(initial || '');
  const [error, setError] = useState('');
  const [saving, setSaving] = useState(false);
  useLayoutEffect(() => {
    const dialog = dialogRef.current;
    dialog.showModal();
    dialog.querySelector('input')?.focus();
  }, []);
  const submit = async (event) => {
    event.preventDefault();
    const bad = categoryNameError(value);
    if (bad) { setError(bad); return; }
    setSaving(true);
    setError('');
    try {
      await onSubmit(value.trim());
      dialogRef.current?.close();
    } catch (err) {
      setError(err.message || '分類儲存失敗');
      setSaving(false);
    }
  };
  return (
    <dialog
      ref={dialogRef}
      className="ra-dialog w-[min(100vw-2rem,24rem)] rounded-2xl border border-ra-line bg-ra-bg p-0 text-ra-t1 shadow-2xl focus:outline-none"
      aria-labelledby="category-name-title"
      onClose={onClose}
      onClick={(event) => {
        if (event.target !== event.currentTarget) return;
        const rect = event.currentTarget.getBoundingClientRect();
        if (event.clientX < rect.left || event.clientX > rect.right || event.clientY < rect.top || event.clientY > rect.bottom) event.currentTarget.close();
      }}
    >
      <form onSubmit={submit} className="space-y-3 p-4">
        <h2 id="category-name-title" className="text-sm font-semibold">{mode === 'rename' ? '重新命名分類' : '建立分類'}</h2>
        <input aria-label="分類名稱" value={value} autoFocus autoComplete="off" onChange={(event) => setValue(event.target.value)}
          className="w-full rounded-lg border border-ra-line bg-ra-card px-3 py-2 text-sm text-ra-t1 focus:outline-none" />
        {error ? <div role="alert" className="text-xs text-red-300">{error}</div> : null}
        <div className="flex justify-end gap-2">
          <button type="button" className="min-h-11 rounded-lg px-3 text-sm text-ra-t3" onClick={() => dialogRef.current?.close()}>取消</button>
          <button type="submit" className="min-h-11 rounded-lg bg-violet-600 px-3 text-sm text-white disabled:opacity-40" disabled={saving}>{mode === 'rename' ? '儲存' : '建立'}</button>
        </div>
      </form>
    </dialog>
  );
}

function CategoryDeleteDialog({ category, onClose, onConfirm }) {
  const dialogRef = useRef(null);
  const [error, setError] = useState('');
  const [saving, setSaving] = useState(false);
  useLayoutEffect(() => {
    const dialog = dialogRef.current;
    dialog.showModal();
    dialog.querySelector('[data-safe-close]')?.focus();
  }, []);
  const confirm = async () => {
    setSaving(true);
    setError('');
    try {
      await onConfirm();
      dialogRef.current?.close();
    } catch (err) {
      setError(err.message || '刪除分類失敗');
      setSaving(false);
    }
  };
  return (
    <dialog
      ref={dialogRef}
      className="ra-dialog w-[min(100vw-2rem,24rem)] rounded-2xl border border-ra-line bg-ra-bg p-0 text-ra-t1 shadow-2xl focus:outline-none"
      aria-labelledby="category-delete-title"
      onClose={onClose}
    >
      <div className="space-y-3 p-4">
        <h2 id="category-delete-title" className="text-sm font-semibold">刪除分類</h2>
        <p className="text-sm text-ra-t3">刪除分類「{category.name}」後，裡面的會話會變成未分類，不會刪除會話。</p>
        {error ? <div role="alert" className="text-xs text-red-300">{error}</div> : null}
        <div className="flex justify-end gap-2">
          <button type="button" data-safe-close autoFocus className="min-h-11 rounded-lg px-3 text-sm" onClick={() => dialogRef.current?.close()}>取消</button>
          <button type="button" className="min-h-11 rounded-lg px-3 text-sm text-red-300" disabled={saving} onClick={confirm}>刪除分類</button>
        </div>
      </div>
    </dialog>
  );
}
