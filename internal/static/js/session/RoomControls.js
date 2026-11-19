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

function roomLocalAttachment(item) {
  if (item.upload?.url) {
    return {
      id: item.upload.id,
      name: item.upload.name || item.name,
      mime_type: item.upload.mime_type || item.file?.type || '',
      size: Number(item.upload.size ?? item.size) || 0,
      url: item.upload.url,
    };
  }
  return {
    id: item.id,
    name: item.name,
    mime_type: item.file?.type || '',
    size: Number(item.size) || 0,
    preview_url: item.previewUrl || '',
    local_preview: true,
  };
}

function RoomComposer({ roomId, pm, members, sessions, onAccepted, onDelivery, onDeliveryPatch, onDeliveryDrop }) {
  let loadedDraft;
  try {
    loadedDraft = roomDraftParse(localStorage.getItem(roomDraftStorageKey(roomId)));
  } catch (_) {
    loadedDraft = { ok: false, text: '', mentions: [], targetPmId: '' };
  }
  const [draft, setDraft] = useState(loadedDraft.ok ? loadedDraft.text : '');
  const [mentions, setMentions] = useState(loadedDraft.ok ? loadedDraft.mentions : []);
  const [sending, setSending] = useState(false);
  const [files, setFiles] = useState([]);
  const [feedback, setFeedback] = useState(loadedDraft.ok ? null : { error: true, text: '草稿無法讀取，請重試' });
  const [revokeGen, setRevokeGen] = useState(0);
  const [mentionOpen, setMentionOpen] = useState(false);
  const [mentionItems, setMentionItems] = useState([]);
  const [mentionActive, setMentionActive] = useState(0);
  const [fileRestoreFailed, setFileRestoreFailed] = useState(false);
  const [draftReadFailed, setDraftReadFailed] = useState(!loadedDraft.ok);
  const attempt = useRef(null);
  const operation = useRef(null);
  const targetRef = useRef(loadedDraft.ok && loadedDraft.targetPmId ? loadedDraft.targetPmId : pm.session_id);
  const holdTarget = useRef(roomDraftHoldTarget(loadedDraft.ok ? loadedDraft : null, pm.session_id, true));
  const draftRef = useRef('');
  const filesRef = useRef([]);
  const mentionsRef = useRef(mentions);
  const revokeQueue = useRef([]);
  const handedPreviews = useRef(new Set());
  const filesReady = useRef(false);
  const draftBroken = useRef(!loadedDraft.ok);
  mentionsRef.current = mentions;
  const [switchPM, setSwitchPM] = useState(null);
  draftRef.current = draft;
  filesRef.current = files;
  useEffect(() => {
    if (pm.session_id === targetRef.current) return;
    if (holdTarget.current || draftRef.current.trim() || filesRef.current.length || mentionsRef.current.length) setSwitchPM(pm);
    else targetRef.current = pm.session_id;
  }, [pm.session_id, pm.session_name]);
  function rememberDraft(text, nextMentions, targetPmId) {
    if (draftBroken.current) return '';
    try {
      localStorage.setItem(roomDraftStorageKey(roomId), roomDraftSerialize(text, nextMentions, targetPmId));
      return '';
    } catch (_) {
      setFeedback({ error: true, text: '草稿無法保存，請保留此頁並稍後重試' });
      return '草稿無法保存，請保留此頁並稍後重試';
    }
  }
  function retryDraft() {
    try {
      const next = roomDraftParse(localStorage.getItem(roomDraftStorageKey(roomId)));
      if (!next.ok) {
        draftBroken.current = true;
        setDraftReadFailed(true);
        setFeedback({ error: true, text: '草稿無法讀取，請重試' });
      } else {
        draftBroken.current = false;
        setDraftReadFailed(false);
        setDraft(next.text);
        setMentions(next.mentions);
        if (next.targetPmId) targetRef.current = next.targetPmId;
        setFeedback(null);
      }
    } catch (_) {
      draftBroken.current = true;
      setDraftReadFailed(true);
      setFeedback({ error: true, text: '草稿無法讀取，請重試' });
    }
    restoreFiles();
  }
  function restoreFiles() {
    let cancelled = false;
    filesReady.current = false;
    setFileRestoreFailed(false);
    readComposerFiles(roomDraftStorageKey(roomId)).then((stored) => {
      if (cancelled) return;
      const unique = roomDraftUniqueFiles(stored);
      setFiles(restoreComposerFiles(unique));
      const hold = roomDraftHoldTarget({
        text: draftRef.current, mentions: mentionsRef.current, targetPmId: targetRef.current,
      }, pm.session_id, false) || (targetRef.current !== pm.session_id && unique.length > 0);
      holdTarget.current = hold;
      if (hold) setSwitchPM(pm);
      else if (targetRef.current !== pm.session_id) {
        targetRef.current = pm.session_id;
        setSwitchPM(null);
      }
      filesReady.current = true;
    }).catch(() => {
      if (cancelled) return;
      setFileRestoreFailed(true);
      setFeedback({ error: true, text: '無法還原附件草稿，請重試；目前附件尚未備份' });
    });
    return () => { cancelled = true; };
  }
  useEffect(() => restoreFiles(), [roomId]);
  useEffect(() => {
    if (!filesReady.current) return undefined;
    let cancelled = false;
    writeComposerFiles(roomDraftStorageKey(roomId), files).catch(() => {
      if (!cancelled) setFeedback({ error: true, text: '附件草稿尚未存妥，請勿重整或關閉此頁' });
    });
    return () => { cancelled = true; };
  }, [files, roomId]);
  useEffect(() => {
    const urls = revokeQueue.current.splice(0);
    urls.forEach((url) => URL.revokeObjectURL(url));
  }, [revokeGen]);
  useEffect(() => () => {
    operation.current?.abort();
    filesRef.current.forEach((item) => {
      if (!item?.previewUrl || handedPreviews.current.has(item.previewUrl)) return;
      releaseFile(item);
    });
    revokeQueue.current.forEach((url) => URL.revokeObjectURL(url));
    handedPreviews.current.forEach((url) => URL.revokeObjectURL(url));
  }, []);
  const labelId = React.useId();
  const filesId = React.useId();
  const inputRef = useRef(null);
  useLayoutEffect(() => {
    const el = inputRef.current;
    if (!el) return;
    el.style.blockSize = '40px';
    // 空草稿固定單行高：窄寬時 placeholder 換行會把 scrollHeight 撐高。
    if (!draft) return;
    const limit = Math.min(160, Math.round((window.innerHeight || 800) * 0.3));
    el.style.blockSize = Math.min(Math.max(el.scrollHeight, 40), limit) + 'px';
  }, [draft]);

  function releaseFile(item) {
    if (item && item.previewUrl) URL.revokeObjectURL(item.previewUrl);
  }

  function addFiles(picked) {
    if (!picked.length || operation.current) return;
    if (files.length + picked.length > 20) {
      setFeedback({ error: true, text: '每則訊息最多 20 個附件' });
      return;
    }
    setFeedback(null);
    setFiles((previous) => [...previous, ...picked.map((file) => ({
      file, id: newInputRequestID(), name: file.name || 'paste', size: file.size, status: 'ready',
      previewUrl: String(file.type || '').startsWith('image/') ? URL.createObjectURL(file) : '',
    }))]);
  }

  function pickFiles(event) {
    const picked = Array.from(event.currentTarget.files || []);
    event.currentTarget.value = '';
    addFiles(picked);
  }

  function pasteFiles(event) {
    const picked = clipboardPasteFiles(event.clipboardData);
    if (!picked.length) return;
    event.preventDefault();
    addFiles(picked);
  }

  function syncMention(value, cursor) {
    if (isGuestMode) {
      setMentionOpen(false);
      setMentionItems([]);
      return;
    }
    const hit = mentionQueryAtCursor(value, cursor);
    if (!hit) {
      setMentionOpen(false);
      setMentionItems([]);
      return;
    }
    setMentionOpen(true);
    setMentionItems(roomMentionChoices(sessions, targetRef.current, hit.query, mentionsRef.current));
    setMentionActive(0);
  }

  function selectMention(session) {
    const chosen = roomDraftMention(session);
    if (!chosen) return;
    const cursor = inputRef.current ? inputRef.current.selectionStart : draftRef.current.length;
    const next = consumeMentionQuery(draftRef.current, cursor);
    const nextMentions = mentionsRef.current.some((item) => item.id === chosen.id) ? mentionsRef.current : [...mentionsRef.current, chosen];
    draftBroken.current = false;
    setDraft(next.text);
    setMentions(nextMentions);
    setMentionOpen(false);
    setMentionItems([]);
    rememberDraft(next.text, nextMentions, targetRef.current);
    requestAnimationFrame(() => {
      const box = inputRef.current;
      if (!box) return;
      box.focus();
      box.setSelectionRange(next.cursor, next.cursor);
    });
  }

  async function submit(event) {
    event.preventDefault();
    const text = draft.trim();
    if ((!text && !files.length && !mentions.length) || operation.current) return;
    const cursor = inputRef.current ? inputRef.current.selectionStart : draft.length;
    if (mentionQueryAtCursor(draft, cursor)) {
      setFeedback({ error: true, text: '請先選擇要標記的 session，或刪掉 @' });
      return;
    }
    const controller = new AbortController();
    operation.current = controller;
    setSending(true);
    setFeedback(null);
    const requestHint = newInputRequestID();
    const sent = { text: draft, files, mentions };
    const targetMember = (members || []).find((member) => member.session_id === targetRef.current) || { session_id: targetRef.current, session_name: '', agent_type: '' };
    const body = roomMentionBody(text, targetMember, mentions);
    const local = {
      id: 'local:' + requestHint,
      local: true,
      input_request_id: requestHint,
      role: 'user',
      text: body, body, sender: '你', mine: true, source_kind: 'user', status: 'done',
      delivery: 'sending',
      created_at: '',
      session_name: pm.session_name || 'PM',
      recipient: pm.session_name || 'PM',
      attachments: files.map(roomLocalAttachment),
    };
    files.forEach((item) => { if (item.previewUrl) handedPreviews.current.add(item.previewUrl); });
    onDelivery?.(local);
    try {
      const attachmentIds = [];
      const target = targetRef.current;
      const staged = files.map((item) => ({ ...item }));
      for (const item of staged) {
        if (item.upload?.pmId === target && item.upload.url) {
          attachmentIds.push(item.upload.id);
          continue;
        }
        const updateFile = (data) => setFiles((previous) => previous.map((a) => a.id === item.id ? { ...a, ...data } : a));
        updateFile({ status: 'uploading' });
        try {
          const attachment = await uploadChatAttachment(target, item.file, controller.signal);
          attachmentIds.push(attachment.id);
          item.upload = {
            id: attachment.id, pmId: target, name: attachment.name, mime_type: attachment.mime_type,
            size: attachment.size, url: attachment.url,
          };
          updateFile({ status: 'ready', upload: item.upload });
          onDeliveryPatch?.(requestHint, { attachments: staged.map(roomLocalAttachment) });
        } catch (err) {
          if (controller.signal.aborted) return;
          updateFile({ status: 'error', error: err.message });
          throw new Error(`${item.name}：${err.message}`);
        }
      }
      const signature = JSON.stringify([target, text, attachmentIds, mentions.map((item) => item.id)]);
      const requestId = attempt.current?.signature === signature ? attempt.current.id : requestHint;
      if (requestId !== requestHint) onDeliveryPatch?.(requestHint, { id: 'local:' + requestId, input_request_id: requestId });
      attempt.current = { signature, id: requestId };
      local.input_request_id = requestId;
      const input = { text: body, request_id: requestId, pm_id: target, attachment_ids: attachmentIds };
      const res = await apiFetch(roomUrl(roomId) + '/messages', {
        method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(input), signal: controller.signal,
      });
      if (!res.ok) {
        const message = await roomApiError(res, '暫時無法送出，請重試。');
        if (res.status === 409 && message.includes('PM')) setSwitchPM(pm);
        throw new Error(message);
      }
      attempt.current = null;
      staged.forEach((item) => {
        if (!item.previewUrl) return;
        revokeQueue.current.push(item.previewUrl);
        handedPreviews.current.delete(item.previewUrl);
      });
      setRevokeGen((n) => n + 1);
      if (roomDraftSameSnapshot({ text: draftRef.current, files: filesRef.current, mentions: mentionsRef.current }, sent)) {
        holdTarget.current = false;
        draftBroken.current = false;
        setDraft('');
        setMentions([]);
        setFiles([]);
        const saveError = rememberDraft('', [], targetRef.current);
        writeComposerFiles(roomDraftStorageKey(roomId), []).catch(() => setFeedback({ error: true, text: '附件草稿尚未清掉，請重試' }));
        if (!saveError) setFeedback({ text: '已排隊' });
      } else setFeedback({ text: '已排隊' });
      onDeliveryPatch?.(requestId, { delivery: 'queued', attachments: staged.map(roomLocalAttachment) });
      onAccepted();
    } catch (err) {
      if (!controller.signal.aborted) {
        setFeedback({ error: true, text: err.message || '暫時無法送出，請重試。' });
        onDeliveryPatch?.(local.input_request_id, { delivery: 'failed' });
      } else onDeliveryDrop?.(local.input_request_id);
    } finally {
      operation.current = null;
      if (!controller.signal.aborted) setSending(false);
    }
  }

  const pmState = roomMemberState(pm);
  return (
    <form className="room-composer shrink-0 border-t border-ra-line-soft" onSubmit={submit} aria-labelledby={labelId + '-label'}>
      <div className="mx-auto max-w-3xl">
        <p className="room-composer-meta mb-1 flex min-w-0 items-baseline gap-2 text-xs">
          <label id={labelId + '-label'} htmlFor={labelId} className="min-w-0 truncate text-ra-t3">
            傳訊息給 <strong className="font-semibold text-ra-t1">{(switchPM ? '目前草稿仍對著上一位 PM' : (pm.session_name || 'PM'))}</strong>
          </label>
          <span className="room-pm-state shrink-0 text-ra-t2" role="status">
            {pmState.primary}{pmState.extra ? `。${pmState.extra}` : ''}
          </span>
        </p>
        {switchPM ? <div className="mb-2 rounded-lg border border-ra-line bg-ra-card p-2 text-xs text-ra-t2" role="status">
          <p>PM 已換成「{pm.session_name || '新 PM'}」。這則還沒送出的訊息不會自動轉過去。</p>
          <button type="button" className="mt-1 min-h-11 rounded-lg bg-violet-600 px-3 text-sm font-semibold text-white"
            onClick={() => { targetRef.current = pm.session_id; holdTarget.current = false; setSwitchPM(null); rememberDraft(draftRef.current, mentionsRef.current, pm.session_id); }}>改傳給這位 PM</button>
        </div> : null}
        {(draftReadFailed || fileRestoreFailed) ? <button type="button" className="mb-1 min-h-11 text-xs font-semibold text-rose-300" onClick={retryDraft}>重試還原草稿</button> : null}
        <MentionChips items={mentions} onRemove={(id) => {
          const nextMentions = mentions.filter((item) => item.id !== id);
          setMentions(nextMentions);
          rememberDraft(draft, nextMentions, targetRef.current);
        }} />
        {files.length ? <div className="app-scroll mt-1 max-h-28 overflow-y-auto">
          <AttachmentChips items={files} disabled={sending} onRetry={(id) => {
            setFiles((previous) => previous.map((item) => (item.id === id ? { ...item, status: 'ready', error: '' } : item)));
            setFeedback(null);
          }} onRemove={(id) => {
            setFiles((previous) => {
              const item = previous.find((entry) => entry.id === id);
              if (item?.previewUrl && !handedPreviews.current.has(item.previewUrl)) releaseFile(item);
              return previous.filter((entry) => entry.id !== id);
            });
            setFeedback(null);
          }} />
        </div> : null}
        <div className="relative">
          {mentionOpen ? <MentionMenu items={mentionItems} activeIndex={mentionActive} onSelect={selectMention} /> : null}
        <div className="ra-cmd-bar">
          <div className="relative">
            <input id={filesId} name="files" type="file" multiple disabled={sending} className="room-file-input sr-only" onChange={pickFiles} />
            <label htmlFor={filesId} title="加入附件" className="room-file-label ra-tools-icon">
              <svg aria-hidden="true" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><path d="m21.44 11.05-9.19 9.19a6 6 0 0 1-8.49-8.49l8.57-8.57A4 4 0 1 1 18 8.84l-8.59 8.57a2 2 0 0 1-2.83-2.83l8.49-8.48" /></svg>
              <span className="sr-only">加入附件</span>
            </label>
          </div>
          <textarea id={labelId} ref={inputRef} name="text" rows={1} value={draft} readOnly={sending} placeholder="輸入訊息，@ 可標記 session"
            onPaste={pasteFiles}
            onChange={(event) => {
              const value = event.target.value;
              draftBroken.current = false;
              setDraftReadFailed(false);
              setDraft(value);
              setFeedback(null);
              rememberDraft(value, mentionsRef.current, targetRef.current);
              syncMention(value, event.target.selectionStart);
            }}
            onKeyUp={(event) => {
              if (event.nativeEvent.isComposing || event.keyCode === 229) return;
              if (event.key === 'Enter' || event.key === 'Escape' || event.key === 'ArrowUp' || event.key === 'ArrowDown') return;
              syncMention(event.currentTarget.value, event.currentTarget.selectionStart);
            }}
            onKeyDown={(event) => {
              const action = roomComposerEnterAction({
                key: event.key, shiftKey: event.shiftKey, isComposing: event.nativeEvent.isComposing, keyCode: event.keyCode,
              }, { open: mentionOpen, count: mentionItems.length });
              if (action === 'ime' || action === 'type') return;
              event.preventDefault();
              if (action === 'next') setMentionActive((index) => Math.min(index + 1, Math.max(mentionItems.length - 1, 0)));
              else if (action === 'prev') setMentionActive((index) => Math.max(index - 1, 0));
              else if (action === 'close') setMentionOpen(false);
              else if (action === 'select' && mentionItems[mentionActive]) selectMention(mentionItems[mentionActive]);
              else if (action === 'send') event.currentTarget.form.requestSubmit();
            }}
            className="room-composer-input w-full min-w-0 resize-none self-center bg-transparent px-2 text-base leading-6 text-ra-t1 outline-none placeholder:text-ra-t4" />
          <button type="submit" disabled={sending || (!draft.trim() && !files.length && !mentions.length)}
            aria-label="傳給 PM" title={sending ? '傳送中…' : '傳給 PM'}
            className="room-send flex h-11 w-11 shrink-0 items-center justify-center rounded-lg text-white hover:brightness-110 disabled:opacity-30">
            <svg aria-hidden="true" viewBox="0 0 24 24" className="h-5 w-5" fill="none" stroke="currentColor" strokeWidth="2.2" strokeLinecap="round" strokeLinejoin="round"><path d="M12 19V5m-7 7 7-7 7 7" /></svg>
          </button>
        </div>
        {feedback ? <p className={'mt-1 text-xs ' + (feedback.error ? 'text-rose-400' : 'text-ra-t3')}
          role={feedback.error ? 'alert' : 'status'}>{feedback.text}</p> : null}
        </div>
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
