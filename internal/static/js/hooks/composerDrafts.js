// 輸入框草稿與「待送出訊息」的持久化（localStorage + IndexedDB 附件備份），以及每個 session 的輸入模式。
// 純副作用與狀態，不含 UI；元件請改用這裡的函式。測試：scripts/ux-reliability.test.cjs。

const INPUT_MODE_STORAGE_PREFIX = 'cc_input_mode:';

/** 舊版單一全域鍵，僅在該 session 尚無專用紀錄時作為向後相容讀取 */
const INPUT_MODE_LEGACY_KEY = 'cc_input_mode';

function inputModeStorageKey(sessionId) {
  return INPUT_MODE_STORAGE_PREFIX + String(sessionId || '');
}

const DRAFT_INPUT_STORAGE_PREFIX = 'cc_draft_input:';

// Text uses localStorage; attachment Files are backed up in IndexedDB.
const composerDrafts = new Map();

function restoreComposerDraft(sessionId, text, attachments = [], mentions = []) {
  const current = composerDrafts.get(sessionId) || {};
  let existing = current.text || '';
  try {
    existing = localStorage.getItem(draftInputStorageKey(sessionId)) ?? existing;
  } catch (_) {}
  const restored = existing === text ? text : [text, existing].filter(Boolean).join('\n\n');
  try { localStorage.setItem(draftInputStorageKey(sessionId), restored); } catch (_) {}
  composerDrafts.set(sessionId, {
    ...current,
    text: restored,
    attachments: [...new Map([...attachments, ...(current.attachments || [])].map(a => [a.id, a])).values()],
    mentions: [...new Map([...mentions, ...(current.mentions || [])].map(s => [s.id, s])).values()],
  });
  // Do not overwrite a backup this page has not successfully read yet.
  if (current.filesRestored) {
    writeComposerFiles(draftInputStorageKey(sessionId), composerDrafts.get(sessionId).attachments)
      .catch(() => showToast('附件草稿尚未備份，請保留此頁並稍後重試', { error: true, duration: 5000 }));
  }
  window.dispatchEvent(new CustomEvent('miniapp:draft-restored', { detail: { sessionId, composer: true, text: restored } }));
}

function draftInputStorageKey(sessionId) {
  const scope = isGuestMode ? `share:${SHARE_TOKEN_FROM_URL}:${readGuestAuth()?.nickname || ''}:` : '';
  return DRAFT_INPUT_STORAGE_PREFIX + scope + String(sessionId || '');
}

function pendingInputStorageKey(sessionId) {
  return draftInputStorageKey(sessionId) + ':pending';
}

function recoverPendingInput(sessionId) {
  if (composerDrafts.get(sessionId)?.pending) return false;
  try {
    const stored = localStorage.getItem(pendingInputStorageKey(sessionId));
    if (!stored) return false;
    let record;
    try { record = JSON.parse(stored); } catch (_) {}
    if (record?.requestId && typeof record.data === 'string' && Array.isArray(record.attachmentIds)) {
      composerDrafts.set(sessionId, { ...composerDrafts.get(sessionId), pending: record });
    } else {
      // Legacy drafts have no stable ID, so only their text can be recovered.
      localStorage.removeItem(pendingInputStorageKey(sessionId));
      restoreComposerDraft(sessionId, stored);
    }
    return true;
  } catch (_) { return false; }
}

function newInputRequestID() {
  return `input-${Date.now()}-${Math.random().toString(36).slice(2)}`;
}

function notifyPendingInput(sessionId) {
  window.dispatchEvent(new CustomEvent('miniapp:pending-input', { detail: { sessionId } }));
}

function beginPendingInput(sessionId, { data, text = data, attachments = [], mentions = [] }) {
  recoverPendingInput(sessionId);
  const previous = composerDrafts.get(sessionId)?.pending;
  const attachmentIds = attachments.map(a => a.attachmentId);
  if (previous) {
    if (previous.data === data && JSON.stringify(previous.attachmentIds) === JSON.stringify(attachmentIds)) return previous;
    throw new Error('此會話尚有訊息未確認，請先在聊天室重試確認');
  }
  const record = { requestId: newInputRequestID(), data, text, attachmentIds, attachments, mentions };
  // File/Blob previews stay in memory. Uploaded IDs and the exact payload survive reload.
  const stored = { ...record, attachments: attachments.map(({ file, previewUrl, ...a }) => a) };
  composerDrafts.set(sessionId, { ...composerDrafts.get(sessionId), pending: record });
  try { localStorage.setItem(pendingInputStorageKey(sessionId), JSON.stringify(stored)); } catch (_) {}
  notifyPendingInput(sessionId);
  return record;
}

async function sendPendingInput(sessionId, record, submit) {
  if (record.inFlight) throw new Error('訊息正在確認中');
  record.inFlight = true;
  notifyPendingInput(sessionId);
  const clear = () => {
    if (composerDrafts.get(sessionId)?.pending?.requestId !== record.requestId) return;
    delete composerDrafts.get(sessionId).pending;
    try {
      const key = pendingInputStorageKey(sessionId);
      if (JSON.parse(localStorage.getItem(key))?.requestId === record.requestId) localStorage.removeItem(key);
    } catch (_) {}
  };
  try {
    await submit(record.data, record.attachmentIds, record.requestId);
    clear();
    record.attachments?.forEach(a => a.previewUrl && URL.revokeObjectURL(a.previewUrl));
  } catch (err) {
    if (err.rejected) {
      clear();
      restoreComposerDraft(sessionId, record.text, record.attachments, record.mentions);
    }
    throw err;
  } finally {
    record.inFlight = false;
    notifyPendingInput(sessionId);
  }
}

function clearDraftInputForSession(sessionId) {
  try {
    localStorage.removeItem(draftInputStorageKey(sessionId));
  } catch (_) {}
}

/** 依 session 從 localStorage 讀取輸入模式；該 session 未設定或無效時預設 agent */
function readInputModeForSession(sessionId) {
  try {
    const v = localStorage.getItem(inputModeStorageKey(sessionId));
    if (v === 'shell' || v === 'agent') return v;
    const legacy = localStorage.getItem(INPUT_MODE_LEGACY_KEY);
    if (legacy === 'shell' || legacy === 'agent') return legacy;
  } catch (_) {}
  return 'agent';
}
