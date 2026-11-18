/** 工作室時間軸的一則唯讀氣泡。user／assistant／system 三種外觀：
 *  user＝框起來的氣泡、assistant＝agent 頭像＋文件流正文、system＝置中的淡色提示。
 *  安全：只有 assistant 訊息走 parseMarkdown（內建 DOMPurify 消毒）；user 與 system 一律是 React 文字節點。 */

// dangerouslySetInnerHTML 插入的 DOM 沒有 React 事件：用事件委派處理程式碼區塊的複製鍵（同 ChatView 的作法）。
function roomProseClick(e) {
  const btn = e.target.closest ? e.target.closest('.code-copy-btn') : null;
  if (!btn) return;
  e.stopPropagation();
  let text = '';
  try { text = decodeURIComponent(escape(atob(btn.getAttribute('data-copy-b64') || ''))); } catch (_) { return; }
  copyText(text).then((ok) => showToast(ok ? '已複製程式碼' : '複製失敗', ok ? undefined : { error: true }));
}

const RoomMessageBubble = React.memo(function RoomMessageBubble({ m }) {
  const meta = roomMessageMeta(m);
  const html = useMemo(
    () => (meta.side === 'assistant' && m.text ? parseMarkdown(m.text, { streaming: meta.pending }) : null),
    [m.text, meta.side, meta.pending]
  );
  const source = (
    <span className="flex min-w-0 items-center gap-1.5 text-[11px] text-ra-t4">
      <span className="min-w-0 truncate font-semibold text-ra-t3" title={meta.sourceName}>{meta.sourceName}</span>
      <span className="shrink-0">· {meta.agentLabel}</span>
      {meta.memberRoleLabel ? <span className="shrink-0 rounded bg-ra-raised px-1 text-[10px] text-ra-t3">{meta.memberRoleLabel}</span> : null}
      {meta.time ? <time className="ml-auto shrink-0 text-ra-t5" dateTime={m.created_at}>{meta.time}</time> : null}
    </span>
  );

  if (meta.side === 'system') {
    return (
      <div className="flex justify-center" data-room-msg-id={m.id} data-side="system">
        <div className="max-w-[92%] min-w-0 rounded-lg border border-dashed border-ra-line bg-ra-card px-3 py-1.5">
          {source}
          <div className="mt-0.5 whitespace-pre-wrap break-words text-xs text-ra-t3"><span className="font-semibold text-amber-300/90">{meta.roleLabel}</span>　{m.text}</div>
        </div>
      </div>
    );
  }
  if (meta.side === 'user') {
    return (
      <div className="flex" data-room-msg-id={m.id} data-side="user">
        <div className="w-fit max-w-full min-w-0 rounded-xl border border-violet-500/30 bg-violet-500/10 px-3 py-2">
          {source}
          <div className="mt-1 whitespace-pre-wrap break-words text-sm leading-[1.7] text-ra-t1"><span className="mr-1.5 text-[11px] font-semibold text-violet-300">{meta.roleLabel}</span>{m.text}</div>
        </div>
      </div>
    );
  }
  return (
    <div className="flex flex-col gap-1.5 min-w-0" data-room-msg-id={m.id} data-side="assistant">
      <div className="flex items-center gap-2 min-w-0">
        <span className={`inline-flex h-[22px] w-[22px] shrink-0 items-center justify-center rounded-md ${getAgentBadgeClass(m.agent_type)}`} aria-hidden="true">
          <AgentBadgeIcon agentType={m.agent_type} />
        </span>
        <span className="shrink-0 text-[13px] font-bold text-ra-t2">{meta.roleLabel}</span>
        <div className="min-w-0 flex-1">{source}</div>
      </div>
      {html ? (
        <div className="prose text-ra-t2 text-sm leading-[1.8] min-w-0 pl-[30px]" onClick={roomProseClick} dangerouslySetInnerHTML={{ __html: html }} />
      ) : meta.pending ? (
        <div className="flex gap-1 py-1 pl-[30px]" aria-label="串流中"><span className="dot" /><span className="dot" /><span className="dot" /></div>
      ) : null}
      {meta.pending && html ? <span className="streaming-tail pl-[30px] select-none" aria-hidden="true">...</span> : null}
      {meta.interrupted ? <div className="pl-[30px] text-[11px] text-amber-400">已中斷</div> : null}
    </div>
  );
});
