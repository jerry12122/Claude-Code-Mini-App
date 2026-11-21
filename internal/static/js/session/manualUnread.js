// 手動提醒的觀察、合併與送出。沒有持久化的啟動紀錄；重訪只留在這個分頁的記憶體。

const MANUAL_UNREAD_KEYS = ['manual_unread', 'manual_unread_revision', 'read_message_ids'];
const manualUnreadLeft = new Set();
const manualUnreadRevisit = new Map();
let manualUnreadActivation = 0;
let manualUnreadFocusedId = null;
let manualUnreadFocusReady = false;

function observedReadIds(session) {
  const raw = Array.isArray(session && session.read_message_ids) ? session.read_message_ids : [];
  return raw.filter((id) => Number.isInteger(id) && id > 0);
}

function mergeSessionRecord(prev, next) {
  if (!next) return prev || null;
  if (!prev) return next;
  const merged = { ...prev, ...next };
  for (const key of MANUAL_UNREAD_KEYS) {
    if (!Object.prototype.hasOwnProperty.call(next, key)) merged[key] = prev[key];
  }
  return merged;
}

function mergeListedSessions(prev, nextList) {
  const prior = Array.isArray(prev) ? prev : [];
  return (Array.isArray(nextList) ? nextList : []).map((row) => mergeSessionRecord(prior.find((item) => item.id === row.id), row));
}

function noteManualUnreadFocus(id, revision) {
  const next = id || null;
  if (!manualUnreadFocusReady) {
    manualUnreadFocusReady = true;
    manualUnreadFocusedId = next;
    return;
  }
  if (next === manualUnreadFocusedId) {
    const grant = next ? manualUnreadRevisit.get(next) : null;
    if (grant && Number.isInteger(revision) && revision !== grant.revision) manualUnreadRevisit.delete(next);
    return;
  }
  manualUnreadActivation += 1;
  if (manualUnreadFocusedId) manualUnreadLeft.add(manualUnreadFocusedId);
  manualUnreadFocusedId = next;
  if (!next || !manualUnreadLeft.has(next) || !Number.isInteger(revision) || revision < 0) return;
  manualUnreadRevisit.set(next, { activation: manualUnreadActivation, revision });
}

function manualUnreadRevisitGrant(id) {
  return manualUnreadRevisit.get(id) || null;
}

function dropManualUnreadRevisit(id) {
  manualUnreadRevisit.delete(id);
}

function announceManualUnread(message) {
  if (!message) return;
  showToast(message, { error: true, duration: 5000 });
  let el = document.getElementById('manual-unread-alert');
  if (!el) {
    el = document.createElement('div');
    el.id = 'manual-unread-alert';
    el.setAttribute('role', 'alert');
    el.className = 'sr-only';
    document.body.appendChild(el);
  }
  el.textContent = '';
  setTimeout(() => { el.textContent = message; }, 0);
}

function manualUnreadLabel(session) {
  const count = sessionUnreadCount(session);
  const marked = session && session.manual_unread === true;
  if (count <= 0 && !marked) return null;
  const text = count > 99 ? '99+' : String(count);
  if (count > 0) return { text, label: `未讀訊息 ${text} 則${marked ? '，待回看' : ''}` };
  return { text: '●', label: '待回看' };
}

function ManualUnreadBadge({ session, rail = false }) {
  const view = manualUnreadLabel(session);
  if (!view) return null;
  const className = rail
    ? 'absolute -top-1 -right-1 min-w-[16px] h-4 px-1 rounded-full bg-violet-600 text-white text-[10px] font-bold flex items-center justify-center leading-none pointer-events-none ring-2 ring-ra-bg'
    : 'ra-unread-badge sm:group-hover/card:opacity-0 transition-opacity';
  return <span className={className} title={view.label} aria-label={view.label}>{view.text}</span>;
}

async function sendManualUnread(id) {
  dropManualUnreadRevisit(id);
  try {
    const res = await apiFetch(`/sessions/${id}`, {
      method: 'PATCH', headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ manual_unread: true }),
    });
    const data = await res.json().catch(() => ({}));
    if (!res.ok) return { ok: false, message: '手動提醒更新失敗：' + (data.error || res.status) };
    return { ok: true, session: data };
  } catch (_) {
    return { ok: false, message: '手動提醒更新失敗' };
  }
}

async function sendObservedClear(id, revision, ids) {
  const list = observedReadIds({ read_message_ids: ids });
  let res;
  try {
    res = await apiFetch(`/sessions/${id}`, {
      method: 'PATCH', headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ manual_unread: false, manual_unread_revision: revision, read_message_ids: list.slice(0, 1000) }),
    });
  } catch (_) {
    return { ok: false, message: '手動提醒更新失敗' };
  }
  const data = await res.json().catch(() => ({}));
  const ackRest = async () => {
    for (let i = 1000; i < list.length; i += 1000) {
      const ack = await apiFetch(`/sessions/${id}`, {
        method: 'PATCH', headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ mark_read: true, read_message_ids: list.slice(i, i + 1000) }),
      });
      if (!ack.ok) return false;
    }
    return true;
  };
  if (res.status === 409) {
    await ackRest();
    return { ok: false, conflict: true, message: data.error || '手動提醒已更新', session: data };
  }
  if (!res.ok) return { ok: false, message: '手動提醒更新失敗：' + (data.error || res.status) };
  if (!await ackRest()) return { ok: false, message: '手動提醒更新失敗' };
  return { ok: true, session: data };
}

function snapshotReadAll(list) {
  const entries = [];
  const rest = [];
  for (const row of Array.isArray(list) ? list : []) {
    const revision = row.manual_unread_revision;
    if (!Number.isInteger(revision) || revision < 0 || !row.id) continue;
    const ids = observedReadIds(row);
    entries.push({ id: row.id, manual_unread_revision: revision, read_message_ids: ids.slice(0, 1000) });
    for (let i = 1000; i < ids.length; i += 1000) rest.push({ id: row.id, ids: ids.slice(i, i + 1000) });
  }
  return { entries, rest };
}

async function submitObservedReadAll(list) {
  const { entries, rest } = snapshotReadAll(list);
  let res;
  try {
    res = await apiFetch('/sessions/read-all', {
      method: 'POST', headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ sessions: entries }),
    });
  } catch (_) {
    return { ok: false, message: '全部已讀失敗' };
  }
  const data = await res.json().catch(() => ({}));
  const ackRest = async () => {
    for (const extra of rest) {
      const ack = await apiFetch(`/sessions/${extra.id}`, {
        method: 'PATCH', headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ mark_read: true, read_message_ids: extra.ids }),
      });
      if (!ack.ok) return false;
    }
    return true;
  };
  if (res.status === 409) {
    await ackRest();
    return { ok: false, conflict: true, message: data.error || '手動提醒已更新', sessions: data.sessions || [] };
  }
  if (!res.ok) return { ok: false, message: '全部已讀失敗：' + (data.error || res.status) };
  if (!await ackRest()) return { ok: false, message: '全部已讀失敗' };
  return { ok: true, sessions: data.sessions || [] };
}
