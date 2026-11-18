function RoomComposer({ roomId, pm, onAccepted }) {
  const [draft, setDraft] = useState('');
  const [sending, setSending] = useState(false);
  const [files, setFiles] = useState([]);
  const [feedback, setFeedback] = useState(null);
  const attempt = useRef(null);
  const operation = useRef(null);
  useEffect(() => () => operation.current?.abort(), []);
  const labelId = React.useId();
  const filesId = React.useId();

  function pickFiles(event) {
    const picked = Array.from(event.currentTarget.files);
    event.currentTarget.value = '';
    if (!picked.length || operation.current) return;
    if (files.length + picked.length > 20) {
      setFeedback({ error: true, text: '每則訊息最多 20 個附件' });
      return;
    }
    setFeedback(null);
    setFiles((previous) => [...previous, ...picked.map((file) => ({
      file, id: newInputRequestID(), name: file.name, size: file.size, status: 'ready',
    }))]);
  }

  async function submit(event) {
    event.preventDefault();
    const text = draft.trim();
    if ((!text && !files.length) || operation.current) return;
    const controller = new AbortController();
    operation.current = controller;
    setSending(true);
    setFeedback(null);
    try {
      const attachmentIds = [];
      for (const item of files) {
        if (item.upload?.pmId === pm.session_id) {
          attachmentIds.push(item.upload.id);
          continue;
        }
        const updateFile = (data) => setFiles((previous) => previous.map((a) => a.id === item.id ? { ...a, ...data } : a));
        updateFile({ status: 'uploading' });
        try {
          const attachment = await uploadChatAttachment(pm.session_id, item.file, controller.signal);
          attachmentIds.push(attachment.id);
          updateFile({ status: 'ready', upload: { id: attachment.id, pmId: pm.session_id } });
        } catch (err) {
          if (controller.signal.aborted) return;
          updateFile({ status: 'error', error: err.message });
          throw new Error(`${item.name}：${err.message}`);
        }
      }
      const signature = JSON.stringify([pm.session_id, text, attachmentIds]);
      const requestId = attempt.current?.signature === signature ? attempt.current.id : newInputRequestID();
      attempt.current = { signature, id: requestId };
      const input = { text, request_id: requestId, pm_id: pm.session_id, attachment_ids: attachmentIds };
      const res = await apiFetch(roomUrl(roomId) + '/messages', {
        method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(input), signal: controller.signal,
      });
      if (!res.ok) throw new Error(await roomApiError(res, '暫時無法送出，請重試。'));
      attempt.current = null;
      setDraft('');
      setFiles([]);
      setFeedback({ text: '已送到 PM 的佇列' });
      onAccepted();
    } catch (err) {
      if (!controller.signal.aborted) setFeedback({ error: true, text: err.message || '暫時無法送出，請重試。' });
    } finally {
      operation.current = null;
      if (!controller.signal.aborted) setSending(false);
    }
  }

  return (
    <form className="room-composer shrink-0 border-t border-ra-line-soft bg-ra-bg pt-2.5" onSubmit={submit} aria-labelledby={labelId + '-label'}>
      <div className="mx-auto max-w-3xl">
        <label id={labelId + '-label'} htmlFor={labelId} className="mb-1.5 block truncate text-xs text-ra-t3">
          傳訊息給 <strong className="font-semibold text-ra-t1">{pm.session_name || 'PM'}</strong>
        </label>
        {files.length ? <div className="app-scroll mt-1 max-h-28 overflow-y-auto">
          <AttachmentChips items={files} disabled={sending} onRemove={(id) => {
            setFiles((previous) => previous.filter((item) => item.id !== id));
            setFeedback(null);
          }} />
        </div> : null}
        <div className="ra-cmd-bar">
          <div className="relative">
            <input id={filesId} name="files" type="file" multiple disabled={sending} className="room-file-input sr-only" onChange={pickFiles} />
            <label htmlFor={filesId} title="加入附件" className="room-file-label ra-tools-icon">
              <svg aria-hidden="true" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><path d="m21.44 11.05-9.19 9.19a6 6 0 0 1-8.49-8.49l8.57-8.57A4 4 0 1 1 18 8.84l-8.59 8.57a2 2 0 0 1-2.83-2.83l8.49-8.48" /></svg>
              <span className="sr-only">加入附件</span>
            </label>
          </div>
          <textarea id={labelId} name="text" rows={2} value={draft} readOnly={sending} placeholder="輸入訊息"
            onChange={(event) => { setDraft(event.target.value); setFeedback(null); }}
            onKeyDown={(event) => {
              if (event.key !== 'Enter' || event.shiftKey || event.nativeEvent.isComposing || event.keyCode === 229) return;
              event.preventDefault();
              event.currentTarget.form.requestSubmit();
            }}
            className="min-h-11 w-full min-w-0 resize-y self-center bg-transparent px-2 py-2 text-base leading-6 text-ra-t1 outline-none placeholder:text-ra-t4" />
          <button type="submit" disabled={sending || (!draft.trim() && !files.length)}
            aria-label="傳給 PM" title={sending ? '傳送中…' : '傳給 PM'}
            className="room-send flex h-11 w-11 shrink-0 items-center justify-center rounded-lg text-white hover:brightness-110 disabled:opacity-30">
            <svg aria-hidden="true" viewBox="0 0 24 24" className="h-5 w-5" fill="none" stroke="currentColor" strokeWidth="2.2" strokeLinecap="round" strokeLinejoin="round"><path d="M12 19V5m-7 7 7-7 7 7" /></svg>
          </button>
        </div>
        {feedback ? <p className={'mt-1 text-xs ' + (feedback.error ? 'text-rose-400' : 'text-ra-t3')}
          role={feedback.error ? 'alert' : 'status'}>{feedback.text}</p> : null}
      </div>
    </form>
  );
}

const ROOM_JOB_STATUS = {
  queued: '等待啟動', running: '執行中', blocked: '待處理', slow: '超過 3 分鐘',
  done: '已完成', failed: '失敗', interrupted: '已中斷',
};

function RoomWorkBoard({ roomId, onOpenSession }) {
  const [board, setBoard] = useState(null);
  const [error, setError] = useState('');
  const [edit, setEdit] = useState(null);
  const [saveError, setSaveError] = useState('');
  const [saving, setSaving] = useState(false);
  const memoInputId = React.useId();
  useEffect(() => {
    const controller = new AbortController();
    const load = async () => {
      try {
        const res = await apiFetch(roomUrl(roomId) + '/board', { signal: controller.signal });
        if (!res.ok) throw new Error(await roomApiError(res, '無法載入備忘與待辦'));
        const data = await res.json();
        if (!controller.signal.aborted) {
          setBoard(data);
          setError('');
        }
      } catch (err) {
        if (!controller.signal.aborted) setError(err.message || '無法載入備忘與待辦');
      }
    };
    load();
    const stop = startRoomPoller({ tick: load });
    return () => { controller.abort(); stop(); };
  }, [roomId]);

  const memo = useMemo(() => board?.memo ? parseMarkdown(board.memo, { taskMarkers: true }) : '', [board?.memo]);

  async function saveMemo(event) {
    event.preventDefault();
    if (saving) return;
    setSaving(true);
    setSaveError('');
    try {
      const res = await apiFetch(roomUrl(roomId) + '/board', {
        method: 'PUT', headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ text: edit.text, previous: edit.previous }),
      });
      if (!res.ok) throw new Error(await roomApiError(res, '無法儲存備忘'));
      setBoard((current) => ({ ...current, memo: edit.text.trim() }));
      setEdit(null);
    } catch (err) {
      setSaveError(err.message || '無法儲存備忘');
    } finally {
      setSaving(false);
    }
  }
  return (
    <aside id="room-work-board" className="room-board app-scroll min-h-0 min-w-0 flex-col overflow-y-auto bg-ra-card px-4 pb-5 pt-3" aria-label="備忘與待辦">
      <div className="flex min-h-12 items-center justify-between gap-2 border-b border-ra-line-soft pb-1">
        <h2 className="text-base font-bold text-ra-t1">備忘與待辦</h2>
        {board && !edit ? <button type="button" className="min-h-11 rounded-lg px-2 text-sm font-medium text-ra-t2 hover:bg-ra-raised"
          onClick={() => { setSaveError(''); setEdit({ text: board.memo, previous: board.memo }); }}>編輯備忘</button> : null}
      </div>
      {error ? <p className="mt-3 text-xs text-rose-400" role="alert">{error}</p> : null}
      {!board && !error ? <p className="mt-3 text-sm text-ra-t4">載入中…</p> : null}
      {board ? <>
        {edit ? <form className="mt-3" onSubmit={saveMemo} aria-labelledby={memoInputId + '-label'}>
          <label id={memoInputId + '-label'} htmlFor={memoInputId} className="block text-sm text-ra-t2">備忘（支援 Markdown）</label>
          <textarea id={memoInputId} name="memo" rows={7} value={edit.text} readOnly={saving}
            onChange={(event) => setEdit({ ...edit, text: event.target.value })}
            className="mt-1 w-full resize-y rounded-lg border border-ra-line bg-ra-bg p-2 text-base text-ra-t1" />
          {saveError ? <p role="alert" className="my-1 text-sm text-rose-400">{saveError}</p> : null}
          <div className="flex flex-wrap gap-1.5">
            <button type="submit" disabled={saving} className="min-h-[44px] rounded-md bg-violet-600 px-2 text-sm text-white">儲存備忘</button>
            <button type="button" disabled={saving} className="min-h-[44px] px-2 text-sm text-ra-t2" onClick={() => setEdit(null)}>取消</button>
            {board.memo !== edit.previous ? <button type="button" disabled={saving} className="min-h-[44px] px-2 text-sm text-ra-t2"
              onClick={() => { setEdit({ ...edit, previous: board.memo }); setSaveError(''); }}>已核對下方新版備忘</button> : null}
          </div>
        </form> : null}
        {memo ? <div className="room-wrap prose my-3 text-sm text-ra-t2" onClick={roomProseClick} dangerouslySetInnerHTML={{ __html: memo }} /> :
          <p className="my-3 text-sm text-ra-t4">尚無備忘</p>}
        <h3 className="border-t border-ra-line-soft pt-3 text-sm font-semibold text-ra-t1">派工進度</h3>
        {board.jobs.length ? <ul className="mt-1">
          {board.jobs.map((job) => (
            <li key={job.id} className="border-b border-ra-line-soft">
              <button type="button" onClick={() => onOpenSession(job.worker_id)}
                className="flex min-h-11 w-full items-center gap-2 rounded px-1 text-left text-sm text-ra-t2 hover:bg-ra-raised">
                <span className="shrink-0 text-xs tabular-nums text-ra-t4">#{job.id}</span>
                <span className="min-w-0 flex-1 truncate">{job.worker_name}</span>
                <span className="room-job-status shrink-0 rounded-md bg-ra-sunken px-1.5 py-0.5 text-xs font-medium text-ra-t3"
                  data-tone={['blocked', 'slow', 'failed'].includes(job.status) ? 'warn' : job.status}>
                  {ROOM_JOB_STATUS[job.status] || job.status}
                </span>
              </button>
            </li>
          ))}
        </ul> : <p className="mt-2 text-sm text-ra-t4">尚無派工</p>}
      </> : null}
    </aside>
  );
}
