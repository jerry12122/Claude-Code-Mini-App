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
/** before 是後端回的不透明游標，原樣編碼帶回。member 空白＝總覽；有值時後端先篩選再分頁。 */
function roomTimelineUrl(id, before, limit, member) {
  return `${roomUrl(id)}/timeline?limit=${limit}${before ? `&before=${encodeURIComponent(before)}` : ''}${member ? `&member=${encodeURIComponent(member)}` : ''}`;
}

/** 篩選對象只認目前工作室的成員（以 id，不以名稱）。沒選、換室或成員被移出都是 null＝總覽。 */
function roomFilterTarget(room, sessionId) {
  const list = room && Array.isArray(room.members) ? room.members : [];
  const found = sessionId ? list.find((m) => m.session_id === sessionId) : null;
  return found ? { id: found.session_id, role: found.role, name: found.session_name || '未命名' } : null;
}

/** 時間軸資料的世代：換室、換成員、成員角色改變（交接）都會變。 */
function roomTimelineScope(roomId, target) {
  return `${roomId}
${target ? `${target.id}
${target.role}` : ''}`;
}

/** 換 scope：清掉別的篩選載入的訊息與游標，只留還沒送達的本地氣泡。 */
function roomTimelineReset(state, scope) {
  return { ...roomTimelineEmpty(), messages: (state.messages || []).filter((item) => item.local), scope };
}

/** 套用一頁；這頁所屬的 scope 已不是目前的（使用者已切到別人）就整頁丟棄。 */
function roomTimelineAccept(state, scope, page, mode) {
  return state.scope !== scope ? state : { ...roomTimelineDeliver(state, page, mode), scope };
}

/** 本地氣泡是送給 PM 的，看 worker 時不顯示（仍留在狀態裡，回總覽或 PM 會再看到）。 */
function roomFilterVisible(messages, role) {
  return role === 'worker' && messages.some((item) => item.local) ? messages.filter((item) => !item.local) : messages;
}

function roomFilterEmptyText(target) {
  if (!target) return '成員還沒有任何訊息。';
  return target.role === 'pm' ? '你與 PM 還沒有對話。' : `沒有與「${target.name}」相關的訊息。`;
}

function roomTimelineEmpty() {
  return { messages: [], hasMore: false, nextCursor: '', loaded: false };
}

function roomMessageBefore(a, b) {
  if (!!a.local !== !!b.local) return a.local ? 1 : -1;
  const ta = String(a.created_at || '');
  const tb = String(b.created_at || '');
  if (ta !== tb) return ta < tb ? -1 : 1;
  const ia = Number(a.id);
  const ib = Number(b.id);
  if (Number.isFinite(ia) && Number.isFinite(ib) && ia !== ib) return ia - ib;
  return String(a.id).localeCompare(String(b.id));
}

/** 剪貼簿有檔案才攔截。沒有副檔名的截圖依 MIME 補上，純文字原樣交給瀏覽器。 */
function clipboardPasteFiles(data) {
  const files = Array.from(data && data.files ? data.files : []);
  if (!files.length) return [];
  return files.map((file) => {
    const name = String(file.name || '');
    if (/\.[a-z0-9]+$/i.test(name)) return file;
    const subtype = String(file.type || '').split('/')[1] || 'bin';
    const ext = subtype.replace(/jpeg/i, 'jpg').replace(/plain/i, 'txt').replace(/[^a-z0-9]/gi, '') || 'bin';
    try {
      return new File([file], 'paste.' + ext, { type: file.type || '' });
    } catch (_) {
      return file;
    }
  });
}

function roomLatestBodyId(messages) {
  const list = Array.isArray(messages) ? messages : [];
  for (let i = list.length - 1; i >= 0; i -= 1) {
    const item = list[i];
    if (!item || item.role === 'system') continue;
    if (String(item.body || item.text || '').trim()) return item.id;
  }
  return null;
}

function roomReconcileLocals(persisted, locals) {
  const confirmed = new Set(persisted.map((item) => item.input_request_id).filter(Boolean));
  const pending = locals.filter((item) => item.local && item.input_request_id && !confirmed.has(item.input_request_id));
  return roomMergeMessages(persisted, pending);
}

/** 輪詢套用後把尚未被同一個 input_request_id 確認的本地氣泡接回去。 */
function roomTimelineDeliver(state, page, mode) {
  const locals = (state.messages || []).filter((item) => item.local);
  const base = roomTimelineApply({ ...state, messages: (state.messages || []).filter((item) => !item.local) }, page, mode);
  return { ...base, messages: roomReconcileLocals(base.messages, locals) };
}

function roomAdmitLocal(state, local) {
  const rest = (state.messages || []).filter((item) => {
    if (!item.local) return true;
    if (item.input_request_id === local.input_request_id) return false;
    return item.delivery !== 'failed';
  });
  return { ...state, messages: roomMergeMessages(rest, [local]) };
}

function roomDropLocal(state, requestId) {
  return { ...state, messages: (state.messages || []).filter((item) => !(item.local && item.input_request_id === requestId)) };
}

function roomPatchLocal(state, requestId, patch) {
  return {
    ...state,
    messages: (state.messages || []).map((item) => (item.local && item.input_request_id === requestId ? { ...item, ...patch } : item)),
  };
}

/** 以 id 合併：同 id 以 incoming 為準（pending 就地更新），內容沒變就沿用舊物件；結果舊在上、新在下。 */
function roomMergeMessages(existing, incoming) {
  const byId = new Map(existing.map((m) => [m.id, m]));
  for (const m of incoming) {
    const old = byId.get(m.id);
    if (old && old.text === m.text && old.status === m.status && old.sender === m.sender &&
        old.recipient === m.recipient && old.member_role === m.member_role && old.session_name === m.session_name &&
        old.body === m.body && old.transfer === m.transfer && old.source_kind === m.source_kind &&
        old.source_session_id === m.source_session_id && old.source_room === m.source_room &&
        old.queued === m.queued && old.input_request_id === m.input_request_id) continue;
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
  // 最新頁帶齊這個工作室尚未提升的輸入。已提升或不在這一頁的佇列投影要拿掉，避免跟正式訊息各留一顆。
  const liveQueue = new Set(incoming.filter((m) => m.queued).map((m) => m.id));
  const base = {
    ...state,
    messages: (state.messages || []).filter((m) => !m.queued || liveQueue.has(m.id)),
  };
  const known = new Set(base.messages.map((m) => m.id));
  const overlaps = incoming.some((m) => known.has(m.id));
  // 初次載入，或兩次輪詢之間新訊息多到一頁裝不下（跟已載入的接不起來）：以這一頁為準重來，避免中間缺一塊。
  if (!base.loaded || base.messages.length === 0 || (!overlaps && hasMore)) {
    return { messages: roomMergeMessages([], incoming), hasMore, nextCursor: hasMore ? nextCursor : '', loaded: true };
  }
  const merged = { ...base, messages: roomMergeMessages(base.messages, incoming), loaded: true };
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

/** 成員狀態只看後端欄位。額度百分比與錯誤文字都不決定是否暫停或需要處理。 */
function roomMemberState(member) {
  const m = member || {};
  const queue = Number(m.queue_length) || 0;
  // 暫停只擋已排隊的訊息；空佇列沒有東西可繼續，也沒有按鈕可按，不能顯示成卡住。
  const paused = !!m.queue_paused && queue > 0;
  const attention = m.attention === 'confirm' || m.attention === 'input' || m.attention === 'shell' ? m.attention : '';
  let primary = '閒置';
  if (attention === 'confirm') primary = '待你授權';
  else if (attention === 'input') primary = '待你回答';
  else if (attention === 'shell') primary = '待你確認指令';
  else if (paused) primary = `佇列已暫停，還有 ${queue} 則`;
  else if (m.status === 'running') primary = '執行中，活動資料未提供';
  const extra = [];
  if (attention && paused) extra.push(`佇列已暫停，還有 ${queue} 則`);
  else if (!paused && queue > 0) extra.push(`佇列 ${queue} 則`);
  return {
    primary, extra: extra.join('；'), attention, paused, queue,
    error: typeof m.last_error === 'string' ? m.last_error : '',
    errorAt: m.last_error_at || '',
  };
}

function roomJobsSplit(jobs) {
  const list = Array.isArray(jobs) ? jobs : [];
  const closed = new Set(['done', 'failed', 'interrupted']);
  return { open: list.filter((job) => !closed.has(job.status)), closed: list.filter((job) => closed.has(job.status)) };
}

/** 只補沒有 open job 的 worker 會話活動，不推定活動屬於哪一筆派工。 */
function roomUnboundWorkers(members, jobs) {
  const assigned = new Set(roomJobsSplit(jobs).open.map((job) => job.worker_id));
  return (Array.isArray(members) ? members : []).filter((member) => {
    if (!member || member.role !== 'worker' || !member.session_id || assigned.has(member.session_id)) return false;
    const state = roomMemberState(member);
    return member.status === 'running' || !!state.attention || state.queue > 0;
  });
}

/** 標頭晶片所需欄位。狀態文字來自 roomMemberState，不靠顏色點單獨表達。 */
function roomChip(member) {
  const q = roomQuotaView(member.quota);
  const state = roomMemberState(member);
  return {
    sessionId: member.session_id,
    name: member.session_name || '未命名',
    agentType: member.agent_type,
    agentLabel: roomAgentLabel(member.agent_type),
    role: member.role,
    roleLabel: ROOM_ROLE_LABEL[member.role] || member.role,
    status: member.status || 'idle',
    statePrimary: state.primary,
    stateExtra: state.extra,
    attention: state.attention,
    queuePaused: state.paused,
    lastError: state.error,
    lastErrorAt: state.errorAt,
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
    mine: m.source_kind ? m.source_kind === 'user' : side === 'user' && (m.mine ?? !m.author),
    body: m.body || m.text || '',
    transfer: m.transfer || '',
    sourceKind: m.source_kind || '',
    sourceRoom: m.source_room || '',
    sourceSessionID: m.source_session_id || '',
    agentLabel,
    memberRoleLabel: ROOM_ROLE_LABEL[m.member_role] || m.member_role || '',
    time: formatMessageTime(m.created_at),
    pending: m.status === 'pending',
    interrupted: m.status === 'interrupted',
  };
}

// ── 排隊輸入的原地編輯（PATCH /rooms/:id/messages/:queue_id）──
const ROOM_QUEUE_EDIT_MAX_BYTES = 64000; // 與後端一致：UTF-8 bytes，不是字元數

function roomQueueEditUrl(roomId, queueId) { return `/rooms/${encodeURIComponent(roomId)}/messages/${queueId}`; }

// 一般 PM 聊天室裡的佇列：私人訊息走原 WS；room-user 走既有工作室 PATCH。其他來源不放行。
function chatQueueEditRoute(item, guest) {
  if (guest || !item || item.batch_id || item.source_session_id) return null;
  const id = Number(item.id);
  if (!Number.isSafeInteger(id) || id <= 0) return null;
  const author = item.author || '';
  if (!author) return { kind: 'private', id };
  const prefix = 'room-user:';
  if (!author.startsWith(prefix)) return null;
  const roomId = author.slice(prefix.length);
  if (!roomId) return null;
  return { kind: 'room', id, roomId };
}

function chatQueueEditRequest(route, sessionId, previous, content) {
  if (!route) return null;
  if (route.kind === 'room') {
    return { channel: 'http', url: roomQueueEditUrl(route.roomId, route.id), body: { expected_pm_id: sessionId, previous, content } };
  }
  return { channel: 'ws', body: { type: 'queue_edit', id: route.id, previous, content } };
}

function roomUtf8Bytes(text) { return new TextEncoder().encode(String(text ?? '')).length; }

/**
 * 只有已接受、持久（負數 id＝-queue id）、本室使用者來源、且仍排在現任 PM 佇列的輸入可編輯。
 * expectedPm 固定取該項目所屬的 session：之後 PM 交接也不會被悄悄換成新 PM。
 */
function roomQueueEditTarget(m, pmId) {
  if (!m || m.batch_id || m.local || m.queued !== true || m.role !== 'user' || m.source_kind !== 'user') return null;
  const id = Number(m.id);
  if (!Number.isSafeInteger(id) || id >= 0 || !m.session_id || m.session_id !== pmId) return null;
  return { msgId: m.id, queueId: -id, expectedPm: m.session_id, previous: String(m.text ?? ''), hasAttachments: !!(m.attachments && m.attachments.length) };
}

// 專用 queue GET 的正 ID；scope 由讀取交易驗證，不能混用 timeline 負 ID。
function roomQueueRowTarget(q, pmId) {
  if (!q || q.batch_id || !Number.isSafeInteger(q.id) || q.id <= 0 || q.pm_id !== pmId) return null;
  return { msgId: q.id, queueId: q.id, expectedPm: q.pm_id, previous: q.content, hasAttachments: !!q.attachments?.length };
}

/** 送出前的本機檢查（後端仍會再驗）。空白但保留原附件可以送出。 */
function roomQueueEditInvalid(draft, hasAttachments) {
  const bytes = roomUtf8Bytes(draft);
  if (bytes > ROOM_QUEUE_EDIT_MAX_BYTES) return `文字最多 ${ROOM_QUEUE_EDIT_MAX_BYTES} bytes（UTF-8），目前 ${bytes} bytes，請縮短後再儲存。`;
  if (!String(draft).trim() && !hasAttachments) return '請輸入文字；沒有附件的訊息不能存成空白。';
  return '';
}

/** 失敗一律保留草稿、不自動重送：文字只說明發生什麼與使用者能做什麼。 */
function roomQueueEditFailure(status, serverMessage) {
  if (status === 409) return '這則排隊訊息已被取走、已被修改，或工作室的 PM 已交接。草稿已保留；可取消，或重新載入後再決定是否儲存。';
  if (status === 404) return '找不到這則排隊訊息或工作室（可能已被刪除）。草稿已保留。';
  if (status === 401 || status === 403) return '沒有編輯權限或登入已失效。草稿已保留。';
  return serverMessage || `儲存失敗（${status}）。草稿已保留。`;
}

function roomQueueEditNetworkError(err) {
  const timedOut = !!(err && /逾時/.test(String(err.message || '')));
  return `${timedOut ? '連線逾時' : '連線中斷'}，無法確認是否已儲存。草稿已保留；請重新載入確認後再決定，不會自動重送。`;
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

/** 工作室草稿鍵。跟一般 session 的 cc_draft_input 分開，房間之間也不共用。 */
function roomDraftStorageKey(roomId) {
  return 'cc_room_draft:' + String(roomId || '');
}

function roomDraftMention(session) {
  if (!session || !session.id) return null;
  return {
    id: session.id,
    name: session.name || '',
    work_dir: session.work_dir || '',
    agent_type: session.agent_type || '',
    disguise_agent: session.disguise_agent || '',
    git_branch: session.git_branch || '',
  };
}

function roomDraftParse(raw) {
  if (raw == null || raw === '') return { ok: true, text: '', mentions: [], targetPmId: '' };
  let parsed;
  try { parsed = JSON.parse(raw); } catch (_) { return { ok: false, text: '', mentions: [], targetPmId: '' }; }
  if (!parsed || typeof parsed !== 'object') return { ok: false, text: '', mentions: [], targetPmId: '' };
  return {
    ok: true,
    text: typeof parsed.text === 'string' ? parsed.text : '',
    mentions: (Array.isArray(parsed.mentions) ? parsed.mentions : []).map(roomDraftMention).filter(Boolean),
    targetPmId: typeof parsed.targetPmId === 'string' ? parsed.targetPmId : '',
  };
}

function roomDraftSerialize(text, mentions, targetPmId) {
  return JSON.stringify({
    text: String(text || ''),
    mentions: (Array.isArray(mentions) ? mentions : []).map(roomDraftMention).filter(Boolean),
    targetPmId: String(targetPmId || ''),
  });
}

function roomDraftUniqueFiles(items) {
  const seen = new Set();
  return (Array.isArray(items) ? items : []).filter((item) => {
    if (!item || !item.id || seen.has(item.id)) return false;
    seen.add(item.id);
    return true;
  });
}

/** 202 之後只清這一次送出的內容。期間若草稿已換成新的，留下新的。 */
function roomDraftSameSnapshot(current, sent) {
  const ids = (list) => (Array.isArray(list) ? list : []).map((item) => item && item.id).join('\n');
  return !!current && !!sent && current.text === sent.text && ids(current.files) === ids(sent.files) && ids(current.mentions) === ids(sent.mentions);
}

/** 附件還沒讀回來時，先守住舊 PM，避免檔案草稿被自動改發。 */
function roomDraftHoldTarget(draft, currentPmId, filesPending) {
  if (!draft || !draft.targetPmId || draft.targetPmId === currentPmId) return false;
  if (filesPending) return true;
  return !!(String(draft.text || '').trim() || (draft.mentions || []).length);
}

function roomMentionChoices(sessions, targetId, query, selected) {
  return filterMentionSessions(sessions, targetId, query, (selected || []).map((item) => item.id));
}

/** 送給 PM 的正文。沒有選取的 @ 不會被拿掉，也不會變成提及。 */
function roomMentionBody(text, pm, mentions) {
  const self = {
    id: (pm && (pm.session_id || pm.id)) || '',
    name: (pm && (pm.session_name || pm.name)) || '',
    agent_type: (pm && pm.agent_type) || '',
  };
  return expandMentionPrompt(String(text || ''), self, mentions);
}

function roomComposerEnterAction(event, mention) {
  const keyEvent = event || {};
  if (keyEvent.isComposing || keyEvent.keyCode === 229) return 'ime';
  const menu = mention || {};
  if (menu.open) {
    if (keyEvent.key === 'ArrowDown') return 'next';
    if (keyEvent.key === 'ArrowUp') return 'prev';
    if (keyEvent.key === 'Escape') return 'close';
    if (keyEvent.key === 'Enter' && !keyEvent.shiftKey) return (menu.count || 0) > 0 ? 'select' : 'hold';
  }
  if (keyEvent.key === 'Enter' && !keyEvent.shiftKey) return 'send';
  return 'type';
}
