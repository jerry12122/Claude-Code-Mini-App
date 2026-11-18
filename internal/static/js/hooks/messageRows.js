// 訊息列轉換與時間格式：API／sync 的 row → ChatView 用的物件，以及 SQLite datetime 的解析。

/** 解析 API 回傳的 last_active（SQLite datetime 字串）為毫秒時間戳 */
function sessionLastActiveMs(s) {
  if (!s || !s.last_active) return 0;
  const raw = String(s.last_active).trim();
  const t = Date.parse(raw.includes('T') ? raw : raw.replace(' ', 'T'));
  return Number.isNaN(t) ? 0 : t;
}

/** 舊版 API 沒有未讀數時，沿用活動時間判斷是否有未讀。 */
function sessionUnreadCount(s) {
  if (typeof s.unread_count === 'number' && s.unread_count >= 0) return s.unread_count;
  const active = sessionLastActiveMs(s);
  if (!active) return 0;
  const read = s.last_read_at ? Date.parse(String(s.last_read_at).trim().replace(' ', 'T')) : 0;
  return active > (Number.isNaN(read) ? 0 : read) ? 1 : 0;
}

/**
 * 產生 optimistic update 用的「現在」字串，格式對齊 SQLite datetime('now')（YYYY-MM-DD HH:MM:SS）。
 * 用本地時間（不加時區標記），因為 sessionLastActiveMs／sessionUnreadCount 都是以 Date.parse 忽略時區的方式解析，
 * 需與既有解析假設一致，否則本地樂觀值與後端值的比較會偏差。
 */
function formatSqliteNow() {
  const d = new Date();
  const pad = (n) => String(n).padStart(2, '0');
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`;
}

/** 解析訊息 created_at（SQLite datetime / ISO）為 Date；無效則回 null */
function parseMessageCreatedAt(raw) {
  if (raw == null || raw === '') return null;
  const s = String(raw).trim();
  // SQLite datetime('now') 為 UTC 且無時區後綴；補 Z 以免被當成本地時間
  let normalized = s;
  if (!s.includes('T')) normalized = s.replace(' ', 'T');
  if (!/[zZ]|[+-]\d{2}:?\d{2}$/.test(normalized)) normalized += 'Z';
  const t = Date.parse(normalized);
  if (Number.isNaN(t)) return null;
  return new Date(t);
}

/** 聊天氣泡下方時間：今日僅 HH:mm；昨日「昨天 HH:mm」；同年 M/D HH:mm；跨年 YYYY/M/D HH:mm */
function formatMessageTime(raw) {
  const d = parseMessageCreatedAt(raw);
  if (!d) return '';
  const now = new Date();
  const pad = (n) => String(n).padStart(2, '0');
  const hm = `${pad(d.getHours())}:${pad(d.getMinutes())}`;
  const startOfDay = (x) => new Date(x.getFullYear(), x.getMonth(), x.getDate()).getTime();
  const dayDiff = Math.round((startOfDay(now) - startOfDay(d)) / 86400000);
  if (dayDiff === 0) return hm;
  if (dayDiff === 1) return `昨天 ${hm}`;
  if (d.getFullYear() === now.getFullYear()) {
    return `${d.getMonth() + 1}/${d.getDate()} ${hm}`;
  }
  return `${d.getFullYear()}/${d.getMonth() + 1}/${d.getDate()} ${hm}`;
}

function parseShellExitCode(content) {
  const m = content.match(/\[exit (-?\d+)\]\n?$/);
  return m ? parseInt(m[1], 10) : null;
}

/** 將 API / sync 的訊息列轉成 ChatView 用的物件（含 pending 串流） */
function mapMessageRow(m) {
  const role = (m.role === 'assistant' || m.role === 'telegram') ? 'claude' : m.role;
  const status = m.status || 'done';
  const pending = status === 'pending';
  const resultText = m.result_text || '';
  const content = (m.content || '').trim() || resultText;
  return {
    id: m.id,
    role,
    content,
    attachments: Array.isArray(m.attachments) ? m.attachments : [],
    resultText,
    status,
    createdAt: m.created_at || m.createdAt || null,
    author: m.author || '', // 分享聊天室／Telegram：空字串＝擁有者；有值＝說話者暱稱
    // Telegram 時間線：穩定識別、送達狀態與取代關係（其他 agent 為空值）。
    telegram: m.telegram || null,
    delivery: m.delivery || '',
    clientMessageId: m.client_message_id || m.clientMessageId || '',
    replacesClientMessageId: m.replaces_client_message_id || m.replacesClientMessageId || '',
    deliveryError: m.deliveryError || '',
    html: role === 'claude' ? (content ? parseMarkdown(content) : null) : null,
    streaming: pending,
    exitCode: role === 'shell' ? parseShellExitCode(content) : undefined,
  };
}
