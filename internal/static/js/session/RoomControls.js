function RoomMemberActions({ member }) {
  const session = { ...member, id: member.session_id, name: member.session_name };
  // 只在成員需要回應時連線。sync_messages=0 不帶歷史，isFocused 為 false 不標已讀，也不送 room_drain。
  // 連上仍會廣播 presence、收到該 session 的額度／佇列快照，額度快取過期時伺服器可能補刷新；
  // 該 session 仍有效的授權或問答會被還原。閒置成員不連線。
  const socket = useChatSocket({ session, agentType: member.agent_type || 'claude',
    showPermModeSelect: false, showEffortSelect: false, isFocused: false, syncMessages: false });
  const pending = ['AWAITING_CONFIRM', 'AWAITING_INPUT', 'AWAITING_SHELL_CONFIRM', 'SHELL_AWAITING_APPROVAL'].includes(socket.state);
  if (!pending && socket.connected) return null;
  return (
    <section id={'room-action-' + member.session_id} tabIndex={-1} className="room-wrap rounded-xl border border-ra-line bg-ra-card p-3" aria-label={`${member.session_name || '成員'} 的授權與回答`}>
      <h2 className="mb-2 text-sm font-semibold text-ra-t1">{member.session_name || '成員'} · {ROOM_ROLE_LABEL[member.role]}</h2>
      {!socket.connected ? <p role="status" className="mb-2 text-xs text-ra-t3">正在連線，連線恢復後即可回應。</p> : null}
      <SessionActionCards socket={socket} canAct={member.agent_type !== 'kiro'} />
    </section>
  );
}

function RoomComposer({ roomId, pm, onAccepted }) {
  const [draft, setDraft] = useState('');
  const [sending, setSending] = useState(false);
  const [files, setFiles] = useState([]);
  const [feedback, setFeedback] = useState(null);
  const attempt = useRef(null);
  const operation = useRef(null);
  const targetRef = useRef(pm.session_id);
  const draftRef = useRef('');
  const filesRef = useRef([]);
  const [switchPM, setSwitchPM] = useState(null);
  draftRef.current = draft;
  filesRef.current = files;
  useEffect(() => {
    if (pm.session_id === targetRef.current) return;
    if (draftRef.current.trim() || filesRef.current.length) setSwitchPM(pm);
    else targetRef.current = pm.session_id;
  }, [pm.session_id, pm.session_name]);
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
      const target = targetRef.current;
      for (const item of files) {
        if (item.upload?.pmId === target) {
          attachmentIds.push(item.upload.id);
          continue;
        }
        const updateFile = (data) => setFiles((previous) => previous.map((a) => a.id === item.id ? { ...a, ...data } : a));
        updateFile({ status: 'uploading' });
        try {
          const attachment = await uploadChatAttachment(target, item.file, controller.signal);
          attachmentIds.push(attachment.id);
          updateFile({ status: 'ready', upload: { id: attachment.id, pmId: target } });
        } catch (err) {
          if (controller.signal.aborted) return;
          updateFile({ status: 'error', error: err.message });
          throw new Error(`${item.name}：${err.message}`);
        }
      }
      const signature = JSON.stringify([pm.session_id, text, attachmentIds]);
      const requestId = attempt.current?.signature === signature ? attempt.current.id : newInputRequestID();
      attempt.current = { signature, id: requestId };
      const input = { text, request_id: requestId, pm_id: target, attachment_ids: attachmentIds };
      const res = await apiFetch(roomUrl(roomId) + '/messages', {
        method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(input), signal: controller.signal,
      });
      if (!res.ok) {
        const message = await roomApiError(res, '暫時無法送出，請重試。');
        if (res.status === 409 && message.includes('PM')) setSwitchPM(pm);
        throw new Error(message);
      }
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

  const pmState = roomMemberState(pm);
  return (
    <form className="room-composer shrink-0 border-t border-ra-line-soft pt-2.5" onSubmit={submit} aria-labelledby={labelId + '-label'}>
      <div className="mx-auto max-w-3xl">
        <label id={labelId + '-label'} htmlFor={labelId} className="mb-1 block truncate text-xs text-ra-t3">
          傳訊息給 <strong className="font-semibold text-ra-t1">{(switchPM ? '目前草稿仍對著上一位 PM' : (pm.session_name || 'PM'))}</strong>
        </label>
        <p className="room-pm-state mb-1.5 text-xs text-ra-t2" role="status">
          {pmState.primary}{pmState.extra ? `。${pmState.extra}` : ''}
        </p>
        {switchPM ? <div className="mb-2 rounded-lg border border-ra-line bg-ra-card p-2 text-xs text-ra-t2" role="status">
          <p>PM 已換成「{pm.session_name || '新 PM'}」。這則還沒送出的訊息不會自動轉過去。</p>
          <button type="button" className="mt-1 min-h-11 rounded-lg bg-violet-600 px-3 text-sm font-semibold text-white"
            onClick={() => { targetRef.current = pm.session_id; setSwitchPM(null); }}>改傳給這位 PM</button>
        </div> : null}
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

function RoomWorkBoard({ roomId, members, tab, onTab, onOpenSession }) {
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
  const jobs = roomJobsSplit(board?.jobs);
  const panel = tab === 'memo' || tab === 'members' ? tab : 'jobs';

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
  const jobButton = (job) => (
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
  );
  return (
    <aside id="room-work-board" className="room-board app-scroll min-h-0 min-w-0 flex-col overflow-y-auto bg-ra-card px-4 pb-5 pt-3" aria-label="工作概況">
      <div className="flex flex-wrap gap-1 border-b border-ra-line-soft pb-2" role="group" aria-label="工作概況分頁">
        {[['jobs', '派工'], ['memo', '備忘'], ['members', '成員']].map(([id, label]) => (
          <button key={id} type="button" aria-pressed={panel === id} onClick={() => onTab(id)}
            className={'min-h-11 rounded-lg px-3 text-sm font-semibold ' + (panel === id ? 'bg-ra-raised text-ra-t1' : 'text-ra-t3 hover:bg-ra-raised')}>{label}</button>
        ))}
      </div>
      {error ? <p className="mt-3 text-xs text-rose-400" role="alert">{error}</p> : null}
      {!board && !error ? <p className="mt-3 text-sm text-ra-t4">載入中…</p> : null}
      {board && panel === 'jobs' ? <>
        <h2 className="mt-3 text-sm font-semibold text-ra-t1">進行中與需要處理</h2>
        {jobs.open.length ? <ul className="mt-1">{jobs.open.map(jobButton)}</ul> : <p className="mt-2 text-sm text-ra-t4">沒有進行中的派工</p>}
        {jobs.closed.length ? <details className="mt-3">
          <summary className="min-h-11 cursor-pointer text-sm font-semibold text-ra-t2">完成紀錄 {jobs.closed.length}</summary>
          <ul>{jobs.closed.map(jobButton)}</ul>
        </details> : null}
      </> : null}
      {board && panel === 'memo' ? <>
        <div className="mt-3 flex min-h-11 items-center justify-between gap-2">
          <h2 className="text-sm font-semibold text-ra-t1">備忘</h2>
          {!edit ? <button type="button" className="min-h-11 rounded-lg px-2 text-sm font-medium text-ra-t2 hover:bg-ra-raised"
            onClick={() => { setSaveError(''); setEdit({ text: board.memo, previous: board.memo }); }}>編輯備忘</button> : null}
        </div>
        {edit ? <form onSubmit={saveMemo} aria-labelledby={memoInputId + '-label'}>
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
      </> : null}
      {panel === 'members' ? <ul className="mt-3 space-y-2">
        {(members || []).map((member) => {
          const chip = roomChip(member);
          return (
            <li key={member.session_id} className="rounded-lg border border-ra-line-soft p-2">
              <RoomMemberChip member={member} onOpen={onOpenSession} />
              {chip.lastError ? <details className="mt-1">
                <summary className="min-h-11 cursor-pointer text-xs text-ra-t3">錯誤紀錄</summary>
                <p className="room-wrap text-xs text-ra-t2">{chip.lastError}</p>
                {chip.lastErrorAt ? <p className="text-[11px] text-ra-t4">{chip.lastErrorAt}</p> : null}
              </details> : null}
            </li>
          );
        })}
      </ul> : null}
    </aside>
  );
}
