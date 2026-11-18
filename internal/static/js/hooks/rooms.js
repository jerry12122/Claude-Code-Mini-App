// ── 工作室的純邏輯（無 JSX、無 React）──────────────────────
// 資料來自 GET /rooms、/rooms/:id、/rooms/:id/timeline。訊息的排序、游標與分頁都以後端為準：
// 這裡只負責「以 id 合併、舊在上新在下」與顯示用的換算，不自己組游標、不猜時間順序。

const ROOM_POLL_MS = 3000;
const ROOM_PAGE_SIZE = 50;
const ROOM_NAME_MAX = 100;
const ROOM_MEMBERS_MAX = 50;
const ROOM_STICK_PX = 48; // 距離底部多近算「停在底部」（會自動跟隨新訊息）
const ROOM_ROLE_LABEL = { pm: 'PM', worker: '執行者' };

function roomsUrl() { return '/rooms'; }
function roomUrl(id) { return `/rooms/${encodeURIComponent(id)}`; }
/** before 是後端回的不透明游標，原樣編碼帶回。 */
function roomTimelineUrl(id, before, limit) {
  return `${roomUrl(id)}/timeline?limit=${limit}${before ? `&before=${encodeURIComponent(before)}` : ''}`;
}

function roomTimelineEmpty() {
  return { messages: [], hasMore: false, nextCursor: '', loaded: false };
}

function roomMessageBefore(a, b) {
  const ta = String(a.created_at || '');
  const tb = String(b.created_at || '');
  if (ta !== tb) return ta < tb ? -1 : 1;
  return a.id - b.id;
}

/** 以 id 合併：同 id 以 incoming 為準（pending 就地更新），內容沒變就沿用舊物件；結果舊在上、新在下。 */
function roomMergeMessages(existing, incoming) {
  const byId = new Map(existing.map((m) => [m.id, m]));
  for (const m of incoming) {
    const old = byId.get(m.id);
    if (old && old.text === m.text && old.status === m.status && old.sender === m.sender &&
        old.recipient === m.recipient && old.member_role === m.member_role && old.session_name === m.session_name) continue;
    byId.set(m.id, m);
  }
  return [...byId.values()].sort(roomMessageBefore);
}

/**
 * 套用一頁時間軸。mode：
 *  - 'latest'：初次載入與輪詢拿到的最新一頁。游標與 hasMore 只在初次載入（或接不上時重來）才採用，
 *    否則會把使用者已經往上翻到的游標蓋掉。
 *  - 'older'：用 next_cursor 取得的更早一頁，直接接在前面並更新游標。
 */
function roomTimelineApply(state, page, mode) {
  const incoming = page && Array.isArray(page.messages) ? page.messages : [];
  const hasMore = !!(page && page.has_more);
  const nextCursor = (page && page.next_cursor) || '';
  if (mode === 'older') {
    return { messages: roomMergeMessages(state.messages, incoming), hasMore, nextCursor: hasMore ? nextCursor : '', loaded: true };
  }
  const known = new Set(state.messages.map((m) => m.id));
  const overlaps = incoming.some((m) => known.has(m.id));
  // 初次載入，或兩次輪詢之間新訊息多到一頁裝不下（跟已載入的接不起來）：以這一頁為準重來，避免中間缺一塊。
  if (!state.loaded || state.messages.length === 0 || (!overlaps && hasMore)) {
    return { messages: roomMergeMessages([], incoming), hasMore, nextCursor: hasMore ? nextCursor : '', loaded: true };
  }
  const merged = { ...state, messages: roomMergeMessages(state.messages, incoming), loaded: true };
  // 最新頁已涵蓋全部歷史：不會再有更早的了
  if (!hasMore) { merged.hasMore = false; merged.nextCursor = ''; }
  return merged;
}

function roomAgentLabel(agentType) {
  return AGENT_LABEL[agentType] || agentType || 'Claude';
}

/** 額度顯示：後端對「從未取得」也標 stale，這時只有佔位 —，不該呈現成「過期的數字」。 */
function roomQuotaView(q) {
  const raw = q && typeof q.display_text === 'string' ? q.display_text.trim() : '';
  if (!raw || raw === '—') return { text: '—', stale: false, title: '尚無額度資料' };
  const at = q.updated_at ? formatMessageTime(q.updated_at) : '';
  const when = at ? `（更新於 ${at}）` : '';
  if (q.stale) return { text: raw, stale: true, title: `額度資料已過期，數字可能不準${when}` };
  return { text: raw, stale: false, title: `額度${when}` };
}

/** 標頭晶片所需欄位。狀態點與狀態文字沿用 ui 層的 sessionStatusDotClass／sessionStatusLabel，所以這裡只帶原始 status。 */
function roomChip(member) {
  const q = roomQuotaView(member.quota);
  return {
    sessionId: member.session_id,
    name: member.session_name || '未命名',
    agentType: member.agent_type,
    agentLabel: roomAgentLabel(member.agent_type),
    role: member.role,
    roleLabel: ROOM_ROLE_LABEL[member.role] || member.role,
    status: member.status || 'idle',
    quotaText: q.text,
    quotaStale: q.stale,
    quotaTitle: q.title,
  };
}

/** 時間軸氣泡所需欄位。side 決定外觀：user／assistant／system 三種一眼分得出來。 */
function roomMessageMeta(m) {
  const side = m.role === 'user' ? 'user' : m.role === 'system' ? 'system' : 'assistant';
  const agentLabel = roomAgentLabel(m.agent_type);
  return {
    side,
    // 助理訊息在資料庫一律是 claude，所以標籤看 agent 類型；使用者訊息 author 空字串＝擁有者
    roleLabel: side === 'user' ? (m.author || '使用者') : side === 'system' ? '系統' : agentLabel,
    sourceName: m.session_name || '未命名',
    sender: m.sender || (side === 'user' ? (m.author || '你') : side === 'system' ? '系統' : m.session_name || agentLabel),
    recipient: m.recipient || '',
    mine: side === 'user' && (m.mine ?? !m.author),
    agentLabel,
    memberRoleLabel: ROOM_ROLE_LABEL[m.member_role] || m.member_role || '',
    time: formatMessageTime(m.created_at),
    pending: m.status === 'pending',
    interrupted: m.status === 'interrupted',
  };
}

function roomIsNearBottom(el) {
  return el.scrollHeight - el.scrollTop - el.clientHeight <= ROOM_STICK_PX;
}

// ── 新增／編輯表單 ──
function roomFormInit(room) {
  return {
    name: room ? room.name : '',
    members: room ? room.members.map((m) => ({ session_id: m.session_id, role: m.role })) : [],
  };
}

/** 勾選／取消勾選成員；新成員預設 worker，順序就是勾選順序。 */
function roomFormToggle(members, sessionId) {
  return members.some((m) => m.session_id === sessionId)
    ? members.filter((m) => m.session_id !== sessionId)
    : [...members, { session_id: sessionId, role: 'worker' }];
}

function roomFormSetRole(members, sessionId, role) {
  if (!ROOM_ROLE_LABEL[role]) return members;
  return members.map((m) => {
    if (m.session_id === sessionId) return { ...m, role };
    return role === 'pm' && m.role === 'pm' ? { ...m, role: 'worker' } : m;
  });
}

function roomFormError(form) {
  const name = form.name.trim();
  if (!name) return '請輸入工作室名稱';
  if (name.length > ROOM_NAME_MAX) return `工作室名稱最多 ${ROOM_NAME_MAX} 個字`;
  if (form.members.length > ROOM_MEMBERS_MAX) return `成員最多 ${ROOM_MEMBERS_MAX} 位`;
  return '';
}

function roomFormBody(form) {
  return {
    name: form.name.trim(),
    members: form.members.map((m) => ({ session_id: m.session_id, role: m.role })),
  };
}
