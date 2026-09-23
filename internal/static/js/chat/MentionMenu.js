/** @mention：標記可詢問／討論的其他 session（不是 Forward）
 *  UI：輸入框上方 chips；文字區不塞 uuid token。
 */

/** 游標前尚未完成的 `@query`；沒有則 null */
function mentionQueryAtCursor(text, cursor) {
  const src = String(text || '');
  const pos = Math.max(0, Math.min(Number(cursor) || 0, src.length));
  const before = src.slice(0, pos);
  const m = before.match(/(^|[\s])@([^\s@[]*)$/);
  if (!m) return null;
  const query = m[2];
  return { atStart: pos - query.length - 1, query, pos };
}

/** 選完後清掉 `@query`，不插入工程字串 */
function consumeMentionQuery(text, cursor) {
  const src = String(text || '');
  const pos = Math.max(0, Math.min(Number(cursor) || 0, src.length));
  const hit = mentionQueryAtCursor(src, pos);
  if (!hit) return { text: src, cursor: pos };
  return { text: src.slice(0, hit.atStart) + src.slice(hit.pos), cursor: hit.atStart };
}

function filterMentionSessions(sessions, currentId, query, excludeIds) {
  const q = String(query || '').toLowerCase();
  const excluded = new Set([currentId, ...(Array.isArray(excludeIds) ? excludeIds : [])].filter(Boolean));
  const list = Array.isArray(sessions) ? sessions : [];
  return list.filter((s) => {
    if (!s || excluded.has(s.id)) return false;
    if (!q) return true;
    const hay = [s.name, s.work_dir, s.agent_type, s.git_branch, s.id]
      .filter(Boolean)
      .join(' ')
      .toLowerCase();
    return hay.includes(q);
  });
}

function formatMiniappSessionLine(kind, s) {
  const name = (s && s.name) ? s.name : '未命名';
  const agent = (s && s.agent_type) ? s.agent_type : 'claude';
  const id = (s && s.id) ? s.id : '';
  return `${kind}: session_id=${id} name=${name} agent=${agent}`;
}

/** 送出前依 chips 展開 [miniapp] 自介；文字區保持乾淨 */
function expandMentionPrompt(text, self, mentionedSessions) {
  const src = String(text || '');
  const list = Array.isArray(mentionedSessions) ? mentionedSessions : [];
  if (!list.length) return src;
  const lines = ['[miniapp]', formatMiniappSessionLine('self', self || {})];
  const seen = new Set();
  for (const s of list) {
    if (!s || !s.id || seen.has(s.id)) continue;
    seen.add(s.id);
    lines.push(formatMiniappSessionLine('mention', s));
  }
  if (seen.size === 0) return src;
  lines.push('上述 mention 標記的是可詢問／討論的對象。請用 miniapp MCP send_message(session_id=<mention>, from_session_id=<self>, text=...) 向對方提問，再用 get_status 讀回覆。');
  lines.push('[/miniapp]', '', src);
  return lines.join('\n');
}

function MentionChips({ items, onRemove }) {
  if (!Array.isArray(items) || items.length === 0) return null;
  return (
    <div className="flex flex-wrap gap-1.5 mb-2" aria-label="已標記要詢問的 session">
      {items.map((s) => {
        const shortDir = workDirGroupShortLabel(s.work_dir);
        return (
          <span
            key={s.id}
            className={`inline-flex max-w-full items-center gap-1 rounded-full border border-violet-500/35 bg-violet-500/10 pl-1.5 pr-1 py-0.5 text-xs text-violet-100`}
            title={(s.work_dir || '') + (s.id ? `\n${s.id}` : '')}
          >
            <span
              className={`inline-flex items-center gap-0.5 shrink-0 text-[10px] px-1 py-0.5 rounded-full font-mono uppercase ${getAgentBadgeClass(s.agent_type)}`}
            >
              <AgentBadgeIcon agentType={s.agent_type} />
              {s.agent_type || 'claude'}
            </span>
            <span className="min-w-0 truncate font-medium">{s.name || '未命名'}</span>
            <span className="shrink-0 max-w-[7rem] truncate font-mono text-[10px] text-violet-300/70">
              {shortDir}
            </span>
            <button
              type="button"
              onMouseDown={(e) => e.preventDefault()}
              onClick={() => onRemove(s.id)}
              aria-label={`移除 ${s.name || '未命名'}`}
              className="shrink-0 inline-flex items-center justify-center w-5 h-5 rounded-full text-violet-300/80 hover:bg-violet-500/25 hover:text-violet-50"
            >
              ×
            </button>
          </span>
        );
      })}
    </div>
  );
}

function MentionMenu({ items, activeIndex, onSelect }) {
  const listRef = useRef(null);
  useLayoutEffect(() => {
    const root = listRef.current;
    const el = root?.querySelector?.(`[data-mention-idx="${activeIndex}"]`);
    if (el && typeof el.scrollIntoView === 'function') {
      el.scrollIntoView({ block: 'nearest' });
    }
  }, [activeIndex, items]);
  return (
    <div
      ref={listRef}
      role="listbox"
      aria-label="提及 session"
      className="absolute bottom-full left-0 right-0 z-20 mb-1 max-h-[min(50vh,16rem)] overflow-y-auto app-scroll rounded-lg border border-gray-700 bg-gray-800/98 py-1 shadow-lg backdrop-blur-sm"
    >
      {items.length === 0 ? (
        <div className="px-3 py-2 text-xs text-gray-500">沒有可標記的 session</div>
      ) : items.map((s, i) => {
        const shortDir = workDirGroupShortLabel(s.work_dir);
        const name = s.name || '未命名';
        return (
          <button
            key={s.id}
            type="button"
            role="option"
            aria-selected={i === activeIndex}
            data-mention-idx={i}
            onMouseDown={(e) => e.preventDefault()}
            onClick={() => onSelect(s)}
            className={`flex w-full flex-col items-stretch gap-0.5 px-3 py-2.5 text-left transition-colors hover:bg-gray-700/80 ${
              i === activeIndex ? 'bg-gray-700' : ''
            }`}
          >
            <span className="text-sm leading-snug text-slate-100 break-words whitespace-normal">
              {name}
            </span>
            <span className="flex min-w-0 items-center gap-1.5">
              <span
                className={`inline-flex items-center gap-0.5 shrink-0 text-[10px] px-1.5 py-0.5 rounded-full font-mono uppercase ${getAgentBadgeClass(s.agent_type)}`}
              >
                <AgentBadgeIcon agentType={s.agent_type} />
                {s.agent_type || 'claude'}
              </span>
              <span className="min-w-0 flex-1 truncate font-mono text-[10px] text-slate-500" title={s.work_dir || ''}>
                {shortDir}
              </span>
            </span>
          </button>
        );
      })}
    </div>
  );
}
