/** @mention：標記可詢問／討論的其他 session（不是 Forward） */

const SESSION_MENTION_RE = /@\[([^\]]*)\]\(session:([0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12})\)/g;

function sessionMentionToken(s) {
  const name = String(s && s.name ? s.name : '未命名').replace(/[\[\]]/g, '');
  return `@[${name}](session:${s.id})`;
}

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

function filterMentionSessions(sessions, currentId, query) {
  const q = String(query || '').toLowerCase();
  const list = Array.isArray(sessions) ? sessions : [];
  return list.filter((s) => {
    if (!s || s.id === currentId) return false;
    if (!q) return true;
    const hay = [s.name, s.work_dir, s.agent_type, s.git_branch, s.id]
      .filter(Boolean)
      .join(' ')
      .toLowerCase();
    return hay.includes(q);
  });
}

function insertMentionToken(text, cursor, session) {
  const src = String(text || '');
  const token = sessionMentionToken(session);
  const hit = mentionQueryAtCursor(src, cursor);
  if (!hit) {
    const pos = Math.max(0, Math.min(Number(cursor) || 0, src.length));
    const next = src.slice(0, pos) + token + ' ' + src.slice(pos);
    return { text: next, cursor: pos + token.length + 1 };
  }
  const next = src.slice(0, hit.atStart) + token + ' ' + src.slice(hit.pos);
  return { text: next, cursor: hit.atStart + token.length + 1 };
}

function formatMiniappSessionLine(kind, s) {
  const name = (s && s.name) ? s.name : '未命名';
  const agent = (s && s.agent_type) ? s.agent_type : 'claude';
  const dir = (s && s.work_dir) ? s.work_dir : '';
  const id = (s && s.id) ? s.id : '';
  return `${kind}: session_id=${id} name=${name} agent=${agent} work_dir=${dir}`;
}

/** 送出前把 @mention token 展開成 [miniapp] 自介，給當前 agent 走 MCP 詢問／討論 */
function expandMentionPrompt(text, self, sessions) {
  const src = String(text || '');
  const found = [];
  const re = new RegExp(SESSION_MENTION_RE.source, 'g');
  let m;
  while ((m = re.exec(src))) {
    found.push({ name: m[1], id: m[2] });
  }
  if (!found.length) return src;
  const byId = new Map();
  for (const s of Array.isArray(sessions) ? sessions : []) {
    if (s && s.id) byId.set(s.id, s);
  }
  const lines = ['[miniapp]', formatMiniappSessionLine('self', self || {})];
  const seen = new Set();
  for (const hit of found) {
    if (seen.has(hit.id)) continue;
    seen.add(hit.id);
    const s = byId.get(hit.id) || { id: hit.id, name: hit.name };
    lines.push(formatMiniappSessionLine('mention', s));
  }
  lines.push('上述 mention 標記的是可詢問／討論的對象，不是轉寄。請用 miniapp MCP send_message(session_id=<mention>, from_session_id=<self>, text=...) 向對方提問，再用 get_status 讀回覆。');
  lines.push('[/miniapp]', '', src);
  return lines.join('\n');
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
      className="absolute bottom-full left-0 right-0 z-20 mb-1 max-h-48 overflow-y-auto app-scroll rounded-lg border border-gray-700 bg-gray-800/98 py-1 shadow-lg backdrop-blur-sm"
    >
      {items.length === 0 ? (
        <div className="px-3 py-2 text-xs text-gray-500">沒有可標記的 session</div>
      ) : items.map((s, i) => {
        const shortDir = workDirGroupShortLabel(s.work_dir);
        return (
          <button
            key={s.id}
            type="button"
            role="option"
            aria-selected={i === activeIndex}
            data-mention-idx={i}
            onMouseDown={(e) => e.preventDefault()}
            onClick={() => onSelect(s)}
            className={`flex w-full items-center gap-2 px-3 py-2 text-left text-sm transition-colors hover:bg-gray-700/80 ${
              i === activeIndex ? 'bg-gray-700' : ''
            }`}
          >
            <span
              className={`inline-flex items-center gap-0.5 shrink-0 text-[10px] px-1.5 py-0.5 rounded-full font-mono uppercase ${getAgentBadgeClass(s.agent_type)}`}
            >
              <AgentBadgeIcon agentType={s.agent_type} />
              {s.agent_type || 'claude'}
            </span>
            <span className="min-w-0 flex-1 truncate text-slate-200">{s.name || '未命名'}</span>
            <span className="shrink-0 max-w-[40%] truncate font-mono text-[10px] text-slate-500" title={s.work_dir || ''}>
              {shortDir}
            </span>
          </button>
        );
      })}
    </div>
  );
}
