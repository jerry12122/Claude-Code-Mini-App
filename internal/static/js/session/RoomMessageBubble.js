/** 以發訊者與收件者呈現工作室訊息。助理正文沿用已消毒的 Markdown。 */

// dangerouslySetInnerHTML 插入的 DOM 沒有 React 事件：用事件委派處理程式碼區塊的複製鍵（同 ChatView 的作法）。
function roomProseClick(e) {
  const btn = e.target.closest ? e.target.closest('.code-copy-btn') : null;
  if (!btn) return;
  e.stopPropagation();
  let text = '';
  try { text = decodeURIComponent(escape(atob(btn.getAttribute('data-copy-b64') || ''))); } catch (_) { return; }
  copyText(text).then((ok) => showToast(ok ? '已複製程式碼' : '複製失敗', ok ? undefined : { error: true }));
}

function RoomMessageBody({ text, full, pending, expanded, children }) {
  const lines = String(text || '').split('\n');
  const long = text.length > 500 || lines.length > 3;
  if (pending || !long) return children;
  if (expanded) {
    return (
      <div className="room-wrap text-sm">
        <button type="button" className="mb-1 min-h-11 rounded-md px-2 text-xs font-semibold underline-offset-4 hover:underline"
          onClick={() => copyText(full || text).then((ok) => showToast(ok ? '已複製全文' : '複製失敗', ok ? undefined : { error: true }))}>複製全文</button>
        {children}
      </div>
    );
  }
  const preview = lines.slice(0, 3).join('\n');
  return (
    <details className="group room-wrap text-sm">
      <summary className="min-h-11 cursor-pointer py-1">
        <span className="text-xs font-semibold"><span className="group-open:hidden">展開全文</span><span className="hidden group-open:inline">收合全文</span></span>
        <span className="mt-1 block whitespace-pre-wrap text-sm leading-relaxed group-open:hidden">{preview}{lines.length > 3 || text.length > preview.length ? '…' : ''}</span>
      </summary>
      <div className="mt-2 pt-1">
        <button type="button" className="mb-1 min-h-11 rounded-md px-2 text-xs font-semibold underline-offset-4 hover:underline"
          onClick={() => copyText(full || text).then((ok) => showToast(ok ? '已複製全文' : '複製失敗', ok ? undefined : { error: true }))}>複製全文</button>
        {children}
      </div>
    </details>
  );
}

/** 排隊輸入的原地編輯表單。原生 textarea：Enter 換行、IME 組字照原生；儲存只由按鈕／表單送出。 */
function RoomQueueEditor({ edit, note, reveal, api }) {
  const id = 'room-queue-edit-' + String(edit.msgId).replace('-', 'n');
  const formRef = useRef(null);
  const inputRef = useRef(null);
  const saving = edit.status === 'saving';
  const bytes = roomUtf8Bytes(edit.draft);
  useEffect(() => { if (inputRef.current) inputRef.current.focus(); }, [edit.msgId, edit.focusTick]);
  useEffect(() => { if (reveal && formRef.current) formRef.current.scrollIntoView({ block: 'nearest' }); }, [reveal]);
  return (
    <form ref={formRef} data-room-queue-edit={String(edit.msgId)} noValidate
      onSubmit={(event) => { event.preventDefault(); api.onSave(); }}
      className="room-read w-full max-w-full min-w-0 rounded-lg border border-ra-line-soft bg-ra-card px-3 py-2.5 text-sm text-ra-t2">
      {note ? <p className="mb-2 text-xs text-amber-300" role="status">{note}</p> : null}
      <label htmlFor={id} className="block text-xs font-semibold text-ra-t2">編輯排隊訊息</label>
      <textarea id={id} ref={inputRef} name="content" rows={4} value={edit.draft} readOnly={saving}
        aria-invalid={edit.invalid ? 'true' : undefined} aria-describedby={id + '-count' + (edit.error ? ' ' + id + '-error' : '')}
        onChange={(event) => api.onDraft(event.target.value)}
        className="mt-1 w-full resize-y rounded-lg border border-ra-line bg-ra-bg p-2 text-base text-ra-t1" />
      <p id={id + '-count'} className={'mt-1 text-xs ' + (bytes > ROOM_QUEUE_EDIT_MAX_BYTES ? 'text-rose-400' : 'text-ra-t3')}>{bytes} / {ROOM_QUEUE_EDIT_MAX_BYTES} bytes（UTF-8）</p>
      {edit.error ? <p id={id + '-error'} role="alert" className="my-1 text-sm text-rose-400">{edit.error}</p> : null}
      <div className="mt-2 flex flex-wrap gap-1.5">
        <button type="submit" disabled={saving} style={{ minWidth: 44 }} className="min-h-[44px] rounded-md bg-violet-600 px-3 text-sm text-white disabled:opacity-60">{saving ? '儲存中…' : '儲存修改'}</button>
        <button type="button" disabled={saving} style={{ minWidth: 44 }} className="min-h-[44px] px-3 text-sm text-ra-t2 disabled:opacity-60" onClick={api.onCancel}>取消</button>
        {edit.error && !saving ? <button type="button" disabled={edit.reloading} style={{ minWidth: 44 }} className="min-h-[44px] px-3 text-sm text-ra-t2 disabled:opacity-60" onClick={api.onReload}>{edit.reloading ? '載入中…' : '重新載入'}</button> : null}
      </div>
    </form>
  );
}

// editPm：目前可開始編輯的 PM session（正在編輯別則時傳空字串）；editState 只傳給正在編輯的那一則，避免其他氣泡跟著重繪。
const RoomMessageBubble = React.memo(function RoomMessageBubble({ m, expanded, onPreview, editPm, editState, editApi }) {
  const meta = roomMessageMeta(m);
  const html = useMemo(
    () => (meta.side === 'assistant' && m.text ? parseMarkdown(m.text, { streaming: meta.pending }) : null),
    [m.text, meta.side, meta.pending]
  );
  const side = meta.side === 'system' ? 'center' : meta.mine ? 'right' : 'left';
  if (editState) {
    return (
      <article className="flex w-full min-w-0 justify-end" data-room-msg-id={m.id} data-side={meta.side} data-align="right" data-queued="true" data-editing="true">
        <div className="room-message flex w-full min-w-0 flex-col items-end"><RoomQueueEditor edit={editState} api={editApi} /></div>
      </article>
    );
  }
  const canEdit = !!editPm && roomQueueEditTarget(m, editPm) !== null;
  if (side === 'center') {
    return (
      <div className="flex justify-center" data-room-msg-id={m.id} data-side={meta.side} data-align={side}>
        <div className="room-message room-wrap rounded-lg bg-ra-sunken px-3 py-1.5 text-center text-xs text-ra-t4">{m.text}</div>
      </div>
    );
  }
  const assistant = meta.side === 'assistant';
  // 版型對齊一般聊天：自己的訊息是右側氣泡，助理是左側「頭像＋名稱列＋無邊框正文」，時間放在氣泡下方。
  const shell = meta.mine
    ? 'bubble-user text-white px-4 py-3 text-sm w-fit max-w-full min-w-0'
    : assistant ? 'bubble-claude w-fit max-w-full min-w-0 flex flex-col gap-3'
      : 'room-read w-fit max-w-full min-w-0 rounded-lg border border-ra-line-soft bg-ra-card px-3 py-2.5 text-sm text-ra-t2';
  const recipient = meta.recipient ? (
    <span className={'room-wrap text-xs leading-snug ' + (meta.mine ? 'font-semibold text-white/70' : 'text-ra-t3')}>
      <span aria-hidden="true">→ </span><span className="sr-only">傳給 </span>{meta.recipient}
    </span>
  ) : null;
  return (
    <article className={'flex w-full min-w-0 ' + (meta.mine ? 'justify-end' : 'justify-start')}
      data-room-msg-id={m.id} data-side={meta.side} data-align={side}
      data-request-id={m.input_request_id || undefined} data-status={m.status || undefined}
      data-queued={m.queued ? 'true' : undefined} data-delivery={m.local ? (m.delivery || 'sending') : undefined}>
      <div className={'room-message flex min-w-0 flex-col ' + (meta.mine ? 'items-end' : 'items-start')}>
        <div className={shell}>
          {meta.mine ? (
            <div className="mb-1 flex flex-wrap items-baseline gap-x-2">
              <strong className="sr-only">{meta.sender}</strong>
              {recipient}
            </div>
          ) : (
            <header className="flex min-w-0 flex-wrap items-center gap-x-2 gap-y-0.5">
              {assistant ? (
                <span className={'inline-flex h-[22px] w-[22px] shrink-0 items-center justify-center rounded-md ' + getAgentBadgeClass(m.agent_type)} aria-hidden="true">
                  <AgentBadgeIcon agentType={m.agent_type} />
                </span>
              ) : null}
              <strong className="room-wrap text-[13px] font-bold leading-snug text-ra-t2">{meta.sender}</strong>
              {recipient}
              {meta.sourceRoom ? <span className="room-wrap text-xs text-ra-t3">來自 {meta.sourceRoom}</span> : null}
            </header>
          )}
          {meta.body ? <RoomMessageBody text={meta.body} full={m.text} pending={meta.pending} expanded={expanded}>
            {html ? (
              <div className="room-wrap prose text-ra-t2 text-sm leading-[1.8]" onClick={roomProseClick} dangerouslySetInnerHTML={{ __html: html }} />
            ) : (
              <div className="room-wrap whitespace-pre-wrap leading-relaxed">{meta.body}</div>
            )}
          </RoomMessageBody> : null}
          {meta.transfer ? <details className="mt-2">
            <summary className="min-h-11 cursor-pointer text-xs font-semibold text-ra-t3">傳遞資訊</summary>
            <pre className="room-wrap whitespace-pre-wrap text-xs text-ra-t3">{meta.transfer}</pre>
            {meta.sourceSessionID ? <p className="mt-1 text-xs text-ra-t4">已記錄的來源 session：{meta.sourceSessionID}</p>
              : <p className="mt-1 text-xs text-ra-t4">這段文字來自訊息原文，沒有經過驗證的來源紀錄。</p>}
          </details> : null}
          {m.attachments?.length ? <div className={m.text ? 'mt-2' : ''}><MessageAttachments items={m.attachments} onPreview={onPreview} /></div> : null}
          {meta.pending && !html ? <div className="flex gap-1 py-1" aria-label="串流中"><span className="dot" /><span className="dot" /><span className="dot" /></div> : null}
          {meta.pending && html ? <span className="streaming-tail select-none" aria-hidden="true">...</span> : null}
          {(m.local || m.queued) ? <div className="mt-1 flex items-center gap-2">
            <p className={'text-[11px] ' + (m.delivery === 'failed' ? 'text-red-300' : meta.mine ? 'text-white/70' : 'text-ra-t3')} role="status">{m.delivery === 'failed' ? '傳送失敗' : m.delivery === 'sending' ? '傳送中' : '已排隊'}</p>
            {canEdit ? <button type="button" aria-label="編輯排隊訊息" title="編輯排隊訊息" onClick={() => editApi.onBegin(m)} style={{ minWidth: 24, minHeight: 24 }} className="inline-flex h-6 w-6 shrink-0 items-center justify-center rounded-md text-white hover:bg-white/10">
              <svg aria-hidden="true" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" className="h-4 w-4"><path d="m16 3 5 5M4 20l4-1L21 6l-5-5L3 14l-1 7Z" /></svg></button> : null}
          </div> : null}
        </div>
        {meta.interrupted ? <div className="mt-2 text-xs text-amber-500">已中斷</div> : null}
        {meta.time ? <time className="ra-msg-time mt-0.5 px-0.5 text-[10px] leading-none text-ra-t5 tabular-nums select-none" dateTime={m.created_at}>{meta.time}</time> : null}
      </div>
    </article>
  );
});
