/** 唯讀瀏覽 session 的 work_dir：麵包屑 + 清單 + 文字預覽。不提供編輯、上傳、刪除、下載。
 *  所有路徑驗證與受保護檔名判斷在後端（internal/fsbrowse）；內容一律以 React 文字節點顯示，不經 innerHTML。 */
function FileBrowserModal({ session, onClose }) {
  const dialogRef = useRef(null);
  const [path, setPath] = useState('');
  const [listing, setListing] = useState(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');
  const [preview, setPreview] = useState(null); // { name, path, loading, content, size, error }
  const listGen = useRef(0);
  const previewGen = useRef(0);
  const sid = session ? session.id : '';

  useEffect(() => { setPath(''); setPreview(null); }, [sid]);

  useEffect(() => {
    if (!sid) return undefined;
    const mine = ++listGen.current;
    setLoading(true);
    setError('');
    (async () => {
      try {
        const res = await apiFetch(fileBrowserUrl(sid, 'list', path));
        if (mine !== listGen.current) return;
        if (!res.ok) { setListing(null); setError(await readApiError(res, '載入目錄失敗')); return; }
        const data = await res.json();
        if (mine === listGen.current) setListing(data);
      } catch (_) {
        if (mine === listGen.current) { setListing(null); setError('載入目錄失敗'); }
      } finally {
        if (mine === listGen.current) setLoading(false);
      }
    })();
    return () => { ++listGen.current; };
  }, [sid, path]);

  // 原生 modal：焦點容納、Esc 關閉與關閉後的焦點還原都由瀏覽器處理（見 docs/spec/frontend-architecture.md 第 5 節）。
  useLayoutEffect(() => {
    if (!session) return undefined;
    const dialog = dialogRef.current;
    dialog.showModal();
    dialog.focus({ preventScroll: true }); // 清單載入時第一顆按鈕會被換掉，焦點放容器才不會掉到 body
    return () => { ++previewGen.current; dialog.close(); };
  }, [session]);

  if (!session) return null;

  const openEntry = async (e) => {
    const p = fileBrowserChild(path, e.name);
    if (e.is_dir) { setPreview(null); setPath(p); return; }
    const mine = ++previewGen.current;
    setPreview({ name: e.name, path: p, loading: true });
    try {
      const res = await apiFetch(fileBrowserUrl(sid, 'content', p));
      if (mine !== previewGen.current) return;
      if (!res.ok) { setPreview({ name: e.name, path: p, error: await readApiError(res, '讀取檔案失敗') }); return; }
      const data = await res.json();
      if (mine === previewGen.current) setPreview({ name: e.name, path: p, content: data.content, size: data.size });
    } catch (_) {
      if (mine === previewGen.current) setPreview({ name: e.name, path: p, error: '讀取檔案失敗' });
    }
  };

  const goto = (p) => { ++previewGen.current; setPreview(null); setPath(p); };
  const entries = listing ? listing.entries : [];
  const crumbBtn = 'min-h-8 px-1.5 rounded text-xs hover:bg-ra-raised hover:text-ra-t1';

  return (
    <dialog
      ref={dialogRef}
      tabIndex={-1}
      className="ra-dialog ra-dialog-files overflow-hidden rounded-2xl border border-ra-line bg-ra-bg shadow-2xl shadow-black/50 focus:outline-none"
      aria-labelledby="file-browser-title"
      onClose={onClose}
      onClick={(e) => {
        if (e.target !== e.currentTarget) return;
        const r = e.currentTarget.getBoundingClientRect();
        if (e.clientX < r.left || e.clientX > r.right || e.clientY < r.top || e.clientY > r.bottom) e.currentTarget.close();
      }}
    >
      <header className="flex shrink-0 items-center gap-2 border-b border-ra-line-soft px-4 py-3">
        <h2 id="file-browser-title" className="min-w-0 flex-1 truncate text-sm font-semibold text-ra-t1">
          瀏覽檔案<span className="ml-2 font-normal text-ra-t4">{session.name}</span>
        </h2>
        <button type="button" aria-label="關閉" onClick={() => dialogRef.current?.close()}
          className="flex h-9 w-9 shrink-0 items-center justify-center rounded-lg text-ra-t2 hover:bg-ra-raised">
          <svg viewBox="0 0 24 24" className="h-5 w-5" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" aria-hidden="true"><path d="m6 6 12 12M6 18 18 6" /></svg>
        </button>
      </header>
      <nav aria-label="目前位置" className="flex shrink-0 flex-wrap items-center gap-0.5 border-b border-ra-line-soft px-3 py-1.5 text-ra-t4">
        {fileBrowserCrumbs(path).map((c, i, all) => (
          <React.Fragment key={c.path}>
            {i > 0 && <span aria-hidden="true" className="text-ra-t5">/</span>}
            <button type="button" className={crumbBtn} aria-current={i === all.length - 1 ? 'location' : undefined} onClick={() => goto(c.path)}>{c.label}</button>
          </React.Fragment>
        ))}
      </nav>
      <div className="flex-1 min-h-0 overflow-y-auto app-scroll">
        {preview ? (
          <div className="p-4">
            <div className="mb-2 flex items-center gap-2">
              <button type="button" onClick={() => { ++previewGen.current; setPreview(null); }}
                className="px-2.5 py-1 rounded-lg text-xs border border-ra-line-strong bg-ra-raised text-ra-t1">返回清單</button>
              <div className="min-w-0 flex-1 truncate text-xs text-ra-t2 ra-mono" title={preview.path}>{preview.name}</div>
              {preview.size != null && <div className="shrink-0 text-[11px] text-ra-t5">{fileBrowserSize(preview.size)}</div>}
            </div>
            {preview.loading ? <div className="text-xs text-ra-t4">載入中…</div> : null}
            {preview.error ? <div className="text-xs text-red-400" role="alert">{preview.error}</div> : null}
            {preview.content != null ? (
              <pre tabIndex={0} aria-label={`${preview.name} 內容`} className="max-h-[60vh] overflow-auto rounded-lg border border-ra-line bg-ra-card p-3 text-xs leading-5 text-ra-t1 ra-mono whitespace-pre">{preview.content}</pre>
            ) : null}
          </div>
        ) : (
          <div className="p-2">
            {loading && !listing ? <div className="p-3 text-xs text-ra-t4">載入中…</div> : null}
            {error ? <div className="p-3 text-xs text-red-400" role="alert">{error}</div> : null}
            {listing && entries.length === 0 ? <div className="p-3 text-xs text-ra-t4">這個目錄是空的</div> : null}
            <ul>
              {entries.map((e) => (
                <li key={e.name}>
                  <button type="button" onClick={() => openEntry(e)} disabled={e.protected}
                    title={e.protected ? '受保護的檔案，不提供內容' : undefined}
                    className="flex min-h-11 w-full items-center gap-2 rounded-lg px-3 py-1.5 text-left text-sm hover:bg-ra-raised disabled:opacity-50 disabled:hover:bg-transparent">
                    <span className={`min-w-0 flex-1 truncate ra-mono ${e.is_dir ? 'text-violet-300' : 'text-ra-t1'}`}>{e.name}{e.is_dir ? '/' : ''}</span>
                    {e.protected ? <span className="shrink-0 text-[11px] text-amber-400">受保護</span> : null}
                    {!e.is_dir ? <span className="shrink-0 text-[11px] text-ra-t5">{fileBrowserSize(e.size)}</span> : null}
                  </button>
                </li>
              ))}
            </ul>
            {listing && listing.truncated ? <div className="p-3 text-[11px] text-ra-t5">此目錄項目很多，只顯示前 500 項。</div> : null}
          </div>
        )}
      </div>
    </dialog>
  );
}
