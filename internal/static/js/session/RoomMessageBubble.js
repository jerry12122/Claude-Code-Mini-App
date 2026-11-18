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

function RoomMessageBody({ text, pending, children }) {
  if (pending || (text.length <= 500 && text.split('\n', 9).length < 9)) return children;
  const preview = Array.from(text.trimStart().slice(0, 240).replace(/\s+/gu, ' ')).slice(0, 120).join('');
  return (
    <details className="room-wrap text-sm">
      <summary className="cursor-pointer py-1">
        <span className="text-xs font-semibold">展開全文</span>
        <span className="mt-1 block text-sm leading-relaxed">{preview}…</span>
      </summary>
      <div className="mt-2 pt-1">
        <button type="button" className="mb-1 min-h-9 rounded-md px-2 text-xs font-semibold underline-offset-4 hover:underline"
          onClick={() => copyText(text).then((ok) => showToast(ok ? '已複製全文' : '複製失敗', ok ? undefined : { error: true }))}>複製全文</button>
        {children}
      </div>
    </details>
  );
}

const RoomMessageBubble = React.memo(function RoomMessageBubble({ m, onPreview }) {
  const meta = roomMessageMeta(m);
  const html = useMemo(
    () => (meta.side === 'assistant' && m.text ? parseMarkdown(m.text, { streaming: meta.pending }) : null),
    [m.text, meta.side, meta.pending]
  );
  const side = meta.side === 'system' ? 'center' : meta.mine ? 'right' : 'left';
  if (side === 'center') {
    return (
      <div className="flex justify-center" data-room-msg-id={m.id} data-side={meta.side} data-align={side}>
        <div className="room-message room-wrap rounded-lg bg-ra-sunken px-3 py-1.5 text-center text-xs text-ra-t4">{m.text}</div>
      </div>
    );
  }
  return (
    <article className={'flex min-w-0 ' + (meta.mine ? 'justify-end' : 'justify-start')}
      data-room-msg-id={m.id} data-side={meta.side} data-align={side}>
      {!meta.mine && meta.side === 'assistant' ? (
        <span className={'mr-2 inline-flex h-[22px] w-[22px] shrink-0 items-center justify-center rounded-md ' + getAgentBadgeClass(m.agent_type)} aria-hidden="true">
          <AgentBadgeIcon agentType={m.agent_type} />
        </span>
      ) : null}
      <div className="room-message min-w-0">
        <header className={'mb-1.5 flex flex-wrap items-baseline gap-x-2 gap-y-0.5 ' + (meta.mine ? 'justify-end' : '')}>
          <strong className="room-wrap text-base font-bold leading-snug text-ra-t1">{meta.sender}</strong>
          {meta.recipient ? <span className="room-wrap text-xs leading-snug text-ra-t3">
            <span aria-hidden="true">→ </span><span className="sr-only">傳給 </span>{meta.recipient}
          </span> : null}
          {meta.time ? <time className="ra-msg-time shrink-0 tabular-nums text-ra-t4" dateTime={m.created_at}>{meta.time}</time> : null}
        </header>
        <div className={'min-w-0 ' + (meta.mine
          ? 'bubble-user px-4 py-2.5'
          : meta.side === 'assistant' ? 'text-ra-t2' : 'rounded-lg border border-ra-line-soft bg-ra-card px-3 py-2.5 text-sm text-ra-t2')}>
          {m.text ? <RoomMessageBody text={m.text} pending={meta.pending}>
            {html ? (
              <div className="room-wrap prose text-ra-t2 text-sm leading-[1.7]" onClick={roomProseClick} dangerouslySetInnerHTML={{ __html: html }} />
            ) : (
              <div className="room-wrap whitespace-pre-wrap leading-relaxed">{m.text}</div>
            )}
          </RoomMessageBody> : null}
          {m.attachments?.length ? <div className={m.text ? 'mt-2' : ''}><MessageAttachments items={m.attachments} onPreview={onPreview} /></div> : null}
          {meta.pending && !html ? <div className="flex gap-1 py-1" aria-label="串流中"><span className="dot" /><span className="dot" /><span className="dot" /></div> : null}
          {meta.pending && html ? <span className="streaming-tail select-none" aria-hidden="true">...</span> : null}
          {meta.interrupted ? <div className="text-[11px] text-amber-400">已中斷</div> : null}
        </div>
      </div>
    </article>
  );
});
