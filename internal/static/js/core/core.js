const { useState, useEffect, useLayoutEffect, useRef, useCallback, useMemo } = React;

// ── Telegram WebApp 初始化 ────────────────────────────────────────────────────
const tgApp = window.Telegram?.WebApp;
// 判斷是否在 Telegram WebView 裡（即使 initData 暫時為空也算）
const isTMA = !!(tgApp);
const initData = isTMA ? (tgApp.initData || '') : '';
// 有 initData 才走 TG 驗證，否則 fallback 到密碼驗證
const isTelegram = isTMA && initData !== '';

if (isTMA) {
  tgApp.ready();
  tgApp.expand();
  tgApp.disableVerticalSwipes?.();
  const updateContentSafeArea = () => {
    const top = tgApp.contentSafeAreaInset?.top ?? 0;
    document.documentElement.style.setProperty('--tg-content-safe-top', `${top}px`);
  };
  updateContentSafeArea();
  tgApp.onEvent('contentSafeAreaChanged', updateContentSafeArea);
}

/** Web 登入 session token（localStorage；後端仍驗證於伺服器端 store） */
const WEB_SESSION_STORAGE_KEY = 'cc_web_session_token';

/** 桌面版側欄寬度（localStorage，px；與原 max-w-[48vw] 上限一致） */
const SIDEBAR_WIDTH_STORAGE_KEY = 'cc_sidebar_width_px';
const SIDEBAR_WIDTH_DEFAULT = 340;
const SIDEBAR_WIDTH_MIN = 260;
/** 收合後的 icon rail 寬度（px） */
const SIDEBAR_RAIL_WIDTH = 56;

/**
 * 外觀設定（Settings 彈窗）：Markdown 顏色 + 自訂 CSS。
 * localStorage（cc_appearance_v1）當快取；伺服器 SQLite 為準（GET/PUT /settings/appearance）。
 */
const APPEARANCE_STORAGE_KEY = 'cc_appearance_v1';
const APPEARANCE_DEFAULTS = {
  headingColor: 'oklch(0.78 0.15 65)',
  boldColor: 'oklch(0.72 0.07 145)',
  codeColor: 'oklch(0.63 0.12 275)',
  customCss: '',
};
const APPEARANCE_CSS_VAR = {
  headingColor: '--md-heading-color',
  boldColor: '--md-bold-color',
  codeColor: '--md-code-color',
};

function appearanceFromPayload(data) {
  if (!data || typeof data !== 'object') return { ...APPEARANCE_DEFAULTS };
  const { stored: _stored, ...rest } = data;
  return { ...APPEARANCE_DEFAULTS, ...rest };
}

function hasLocalAppearance() {
  try {
    return localStorage.getItem(APPEARANCE_STORAGE_KEY) != null;
  } catch (_) {
    return false;
  }
}

function readStoredAppearance() {
  try {
    const raw = localStorage.getItem(APPEARANCE_STORAGE_KEY);
    if (raw) {
      const parsed = JSON.parse(raw);
      if (parsed && typeof parsed === 'object') return { ...APPEARANCE_DEFAULTS, ...parsed };
    }
  } catch (_) {}
  return { ...APPEARANCE_DEFAULTS };
}

function saveStoredAppearance(appearance) {
  try {
    const { stored: _stored, ...rest } = appearance || {};
    localStorage.setItem(APPEARANCE_STORAGE_KEY, JSON.stringify({ ...APPEARANCE_DEFAULTS, ...rest }));
  } catch (_) {}
}

/** 把外觀設定套到頁面：CSS 變數 + 自訂 CSS <style> 標籤內容。App mount 時與 Settings 儲存後都要呼叫。 */
function applyAppearance(appearance) {
  const a = { ...APPEARANCE_DEFAULTS, ...(appearance || {}) };
  const root = document.documentElement.style;
  for (const key of Object.keys(APPEARANCE_CSS_VAR)) {
    root.setProperty(APPEARANCE_CSS_VAR[key], a[key] || APPEARANCE_DEFAULTS[key]);
  }
  const styleEl = document.getElementById('ra-custom-css');
  if (styleEl) styleEl.textContent = a.customCss || '';
}

/** PUT /settings/appearance；成功回 appearance 物件，失敗 throw Error。 */
async function putAppearance(appearance) {
  const body = {
    headingColor: appearance.headingColor ?? APPEARANCE_DEFAULTS.headingColor,
    boldColor: appearance.boldColor ?? APPEARANCE_DEFAULTS.boldColor,
    codeColor: appearance.codeColor ?? APPEARANCE_DEFAULTS.codeColor,
    customCss: appearance.customCss ?? '',
  };
  const res = await apiFetch('/settings/appearance', {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  });
  if (!res.ok) {
    let msg = '伺服器未存到';
    try {
      const j = await res.json();
      if (j && j.error) msg = j.error;
    } catch (_) {}
    throw new Error(msg);
  }
  return appearanceFromPayload(await res.json());
}

/**
 * 一般設定（Settings 彈窗 → 一般頁）：目前只有 vscodeNoAdmin 開關。
 * 不做 localStorage 快取（跟 appearance 不同，這是影響伺服器端行為的設定，
 * 每次開設定頁直接打 GET /settings/general 即可，資料量小、不需離線快取）。
 */
const GENERAL_DEFAULTS = { vscodeNoAdmin: false };

async function getGeneralSettings() {
  try {
    const res = await apiFetch('/settings/general');
    if (!res.ok) return { ...GENERAL_DEFAULTS };
    const data = await res.json();
    return { ...GENERAL_DEFAULTS, ...(data || {}) };
  } catch (_) {
    return { ...GENERAL_DEFAULTS };
  }
}

/** PUT /settings/general；成功回設定物件，失敗 throw Error。 */
async function putGeneralSettings(general) {
  const res = await apiFetch('/settings/general', {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ vscodeNoAdmin: !!general.vscodeNoAdmin }),
  });
  if (!res.ok) {
    let msg = '伺服器未存到';
    try {
      const j = await res.json();
      if (j && j.error) msg = j.error;
    } catch (_) {}
    throw new Error(msg);
  }
  return { ...GENERAL_DEFAULTS, ...(await res.json()) };
}

/**
 * 從伺服器 hydrate 外觀：已存過 → 套用並寫回本機；
 * 從未存過且本機有舊設定 → PUT 一次當種子；失敗維持本機。
 */
async function hydrateAppearanceFromServer() {
  try {
    const res = await apiFetch('/settings/appearance');
    if (!res.ok) return;
    const data = await res.json();
    if (data && data.stored) {
      const appearance = appearanceFromPayload(data);
      saveStoredAppearance(appearance);
      applyAppearance(appearance);
      return;
    }
    if (hasLocalAppearance()) {
      const local = readStoredAppearance();
      const saved = await putAppearance(local);
      saveStoredAppearance(saved);
      applyAppearance(saved);
    }
  } catch (_) {}
}

function readStoredSidebarWidthPx() {
  try {
    const raw = localStorage.getItem(SIDEBAR_WIDTH_STORAGE_KEY);
    const n = parseInt(raw, 10);
    if (Number.isFinite(n) && n > 0) return n;
  } catch (_) {}
  return SIDEBAR_WIDTH_DEFAULT;
}

/** Session 列表「依工作目錄分組」開關（localStorage） */
const SESSION_LIST_GROUP_BY_DIR_KEY = 'cc_session_list_group_by_dir';

function readStoredGroupByDir() {
  try {
    const raw = localStorage.getItem(SESSION_LIST_GROUP_BY_DIR_KEY);
    if (raw === '1' || raw === 'true') return true;
    if (raw === '0' || raw === 'false') return false;
  } catch (_) {}
  return true; /* RemoteAgent 設計預設依目錄分組 */
}

function clampSidebarWidthPx(w) {
  const max = Math.max(SIDEBAR_WIDTH_MIN, Math.floor(window.innerWidth * 0.48));
  return Math.min(Math.max(Math.round(w), SIDEBAR_WIDTH_MIN), max);
}

function sidebarWidthMaxPx() {
  return Math.max(SIDEBAR_WIDTH_MIN, Math.floor(window.innerWidth * 0.48));
}

/** 桌面版側欄收合狀態（localStorage） */
const SIDEBAR_COLLAPSED_KEY = 'cc_sidebar_collapsed';

function readStoredSidebarCollapsed() {
  try {
    return localStorage.getItem(SIDEBAR_COLLAPSED_KEY) === '1';
  } catch (_) {
    return false;
  }
}

/**
 * 正規化 API base path：無結尾 /；根為 ''；一律以 / 開頭（非空時）。
 */
function normalizeApiBasePath(raw) {
  if (raw == null) return null;
  let b = String(raw).trim();
  if (b === '' || b === '/') return '';
  b = b.replace(/\/+$/, '');
  if (!b.startsWith('/')) b = `/${b}`;
  return b;
}

/**
 * 可選覆寫：反代若無法由網址自動對齊 API 節點時使用。
 * - <meta name="cc-api-base" content="/myapp">（需在 bundle 執行前存在於 DOM）
 * - 或於任何 script 以前：window.__CC_API_BASE__ = '/myapp'
 */
function readApiBaseOverride() {
  try {
    const el = document.querySelector('meta[name="cc-api-base"]');
    if (el) {
      const c = el.getAttribute('content');
      if (c != null && String(c).trim() !== '') {
        return normalizeApiBasePath(c);
      }
    }
  } catch (_) {}
  if (typeof window.__CC_API_BASE__ === 'string' && window.__CC_API_BASE__.trim() !== '') {
    return normalizeApiBasePath(window.__CC_API_BASE__);
  }
  return null;
}

/**
 * 與 Go Fiber 同掛一條反代前綴時：目前 index 所在目錄為 API 前綴。
 * 若在 …/Nexus/、…/Focus/、…/Enterprise/、…/RemoteAgent/、…/v1/ 測試主題底下，再剝除該目錄，與同層主介面共用前綴。
 */
function inferApiBaseFromPathname() {
  let p = window.location.pathname || '/';
  if (/\/[^/]+\.html$/i.test(p)) {
    p = p.replace(/\/[^/]+\.html$/i, '');
  }
  p = p.replace(/\/+$/, '');
  p = p.replace(/\/nexus$/i, '').replace(/\/focus$/i, '').replace(/\/enterprise$/i, '').replace(/\/remoteagent$/i, '').replace(/\/v1$/i, '');
  p = p.replace(/\/+$/, '');
  if (p === '' || p === '/') return '';
  return p;
}

function getSpaBasePath() {
  const over = readApiBaseOverride();
  if (over !== null) return over;
  return inferApiBaseFromPathname();
}

/** API 路徑（必須以 / 開頭）加上 base，供反代剝離前綴時瀏覽器仍打對外完整路徑。 */
function appPath(path) {
  const rel = path.startsWith('/') ? path : `/${path}`;
  const base = getSpaBasePath();
  if (!base) return rel;
  return `${base}${rel}`;
}

function resolveApiUrl(url) {
  if (/^https?:\/\//i.test(url)) return url;
  if (url.startsWith('/')) return appPath(url);
  return url;
}

console.log(
  '[init] isTMA:', isTMA,
  'isTelegram:', isTelegram,
  'initData len:', initData.length,
  'protocol:', location.protocol,
  'spaBase:', getSpaBasePath() || '(root)',
);

// Web session 過期時統一登出（僅 Web 密碼登入，TMA 不受影響）
let onSessionExpired = null;
const registerSessionExpiredHandler = (fn) => { onSessionExpired = fn; };

const clearWebSession = () => {
  try {
    const t = localStorage.getItem(WEB_SESSION_STORAGE_KEY);
    localStorage.removeItem(WEB_SESSION_STORAGE_KEY);
    if (t) {
      fetch(resolveApiUrl('/auth/logout'), {
        method: 'POST',
        headers: { Authorization: `Bearer ${t}` },
      }).catch(() => {});
    }
  } catch (_) {}
  onSessionExpired?.();
};

const handleUnauthorized = (res) => {
  if (!isTelegram && res.status === 401) {
    clearWebSession();
    return true;
  }
  return false;
};

// 統一 fetch：TMA 帶 initData；Web 帶 Bearer（localStorage），舊版仍相容後端 Cookie
const apiFetch = async (url, opts = {}) => {
  const headers = { ...(opts.headers || {}) };
  if (isTelegram) {
    headers['X-Telegram-Init-Data'] = initData;
  } else {
    try {
      const t = localStorage.getItem(WEB_SESSION_STORAGE_KEY);
      if (t) headers['Authorization'] = `Bearer ${t}`;
    } catch (_) {}
  }
  const res = await fetch(resolveApiUrl(url), { ...opts, headers });
  handleUnauthorized(res);
  return res;
};

// WebSocket：同源 ws/wss；TMA 用 query initData；Web 用 query token（瀏覽器無法自訂 WS Header）
const wsURL = (sessionId) => {
  const proto = location.protocol === 'https:' ? 'wss' : 'ws';
  let url = `${proto}://${location.host}${appPath(`/sessions/${sessionId}/ws`)}`;
  const sep = (u) => (u.includes('?') ? '&' : '?');
  if (isTelegram) {
    return `${url}${sep(url)}initData=${encodeURIComponent(initData)}`;
  }
  try {
    const t = localStorage.getItem(WEB_SESSION_STORAGE_KEY);
    if (t) return `${url}${sep(url)}token=${encodeURIComponent(t)}`;
  } catch (_) {}
  return url;
};

// marked v5+ 已移除同步 `highlight` option（改走 extension/後處理），此 setOptions 的 highlight
// 已對新版 CDN marked 無效果；改在 parseMarkdown 輸出後手動跑 hljs（見下方）。
marked.setOptions({
  breaks: true,
});

// agent 回覆的連結一律新分頁開啟，避免點擊後導離聊天室；rel 防止 tab-nabbing。
marked.use({
  renderer: {
    link({ href, title, tokens }) {
      const text = this.parser.parseInline(tokens);
      const titleAttr = title ? ` title="${title}"` : '';
      return `<a href="${href}"${titleAttr} target="_blank" rel="noopener noreferrer">${text}</a>`;
    },
  },
});

const parseMarkdown = (text) => {
  const html = marked.parse(text);
  return html
    .replace(/<table>/g, '<div class="table-wrap"><table>')
    .replace(/<\/table>/g, '</table></div>')
    // <pre><code class="language-xxx">raw text</code></pre> → 用 hljs 上色 + 提升 data-lang 到 <pre> + 加複製按鈕。
    .replace(/<pre><code class="language-([\w+-]+)">([\s\S]*?)<\/code><\/pre>/g, (m, lang, inner) => {
      const raw = inner.replace(/&amp;/g, '&').replace(/&lt;/g, '<').replace(/&gt;/g, '>').replace(/&quot;/g, '"').replace(/&#39;/g, "'");
      let out;
      try {
        out = hljs.getLanguage(lang) ? hljs.highlight(raw, { language: lang }).value : hljs.highlightAuto(raw).value;
      } catch (_) {
        out = inner; // 高亮失敗時保留原輸出，不讓整段訊息渲染中斷。
      }
      // 原始文字以 base64 存進 data 屬性，避免複製鍵讀取時再度處理 HTML entity escape。
      const b64 = btoa(unescape(encodeURIComponent(raw)));
      const copyBtn = `<button type="button" class="code-copy-btn" data-copy-b64="${b64}" aria-label="複製程式碼" title="複製">`
        + `<svg class="icon-copy" viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" stroke-width="2"><rect x="9" y="9" width="12" height="12" rx="2"/><path d="M5 15V5a2 2 0 0 1 2-2h10"/></svg>`
        + `<svg class="icon-check" viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" stroke-width="2" style="display:none"><path d="M20 6 9 17l-5-5"/></svg>`
        + `</button>`;
      return `<pre data-lang="${lang}">${copyBtn}<code class="language-${lang} hljs">${out}</code></pre>`;
    })
    // agent 傳回的圖片（如截圖）先縮成縮圖，點擊由 ChatView 的 handleProseClick 開燈箱放大。
    .replace(/<img /g, '<img class="chat-img" loading="lazy" ');
};

/** 顯示用：permission_mode 值 → 中文標籤。包含 Claude/Cursor 與 Gemini 兩套值。 */
const PERM_MODE_LABEL = {
  default: '預設',
  acceptEdits: '自動編輯',
  bypassPermissions: '⚠️ 跳過授權',
  auto_edit: '自動編輯',
  yolo: '⚠️ YOLO（全自動）',
  plan: '規劃模式',
};

const AGENT_LABEL = { claude: 'Claude', cursor: 'Cursor', codex: 'Codex', antigravity: 'Antigravity', kiro: 'Kiro', kiroacp: 'Kiro ACP' };

/** 聊天室頂欄狀態角標：中文短標（避免與權限列搶注意力） */
const SESSION_STATE_LABEL = {
  IDLE: '待命',
  THINKING: '思考中',
  STREAMING: '輸出中',
  AWAITING_CONFIRM: '待授權',
  SHELL_IDLE: '待命',
  SHELL_AWAITING_APPROVAL: '等待確認',
  SHELL_RUNNING: '執行中',
  AWAITING_SHELL_CONFIRM: '待確認 Shell',
  SHELL_EXEC: 'Shell 執行中',
};

const CHAT_HEADER_COLLAPSED_KEY = 'cc_chat_header_collapsed';

function readChatHeaderCollapsedPref() {
  try {
    const v = localStorage.getItem(CHAT_HEADER_COLLAPSED_KEY);
    if (v === '1') return true;
    if (v === '0') return false;
  } catch (_) {}
  return null;
}

function writeChatHeaderCollapsedPref(collapsed) {
  try {
    localStorage.setItem(CHAT_HEADER_COLLAPSED_KEY, collapsed ? '1' : '0');
  } catch (_) {}
}

function useMediaQuery(query) {
  const [matches, setMatches] = useState(() =>
    typeof window !== 'undefined' ? window.matchMedia(query).matches : false
  );
  useEffect(() => {
    if (typeof window === 'undefined') return undefined;
    const mq = window.matchMedia(query);
    const onChange = () => setMatches(mq.matches);
    mq.addEventListener('change', onChange);
    onChange();
    return () => mq.removeEventListener('change', onChange);
  }, [query]);
  return matches;
}

/**
 * 伺服器全域設定（GET /config）：目前只有 shell_enabled，用來決定「開啟 VSCode／開啟目錄」
 * 等依賴伺服器端本機程序的功能是否顯示。模組層快取單一 promise，避免每個用到的元件各 fetch 一次。
 */
let _serverConfigPromise = null;
function fetchServerConfig() {
  if (!_serverConfigPromise) {
    _serverConfigPromise = apiFetch('/config')
      .then((res) => (res.ok ? res.json() : {}))
      .catch(() => ({}));
  }
  return _serverConfigPromise;
}

/** 回傳 { shellEnabled }；初始 false，fetch 完成後更新。 */
function useServerConfig() {
  const [shellEnabled, setShellEnabled] = useState(false);
  useEffect(() => {
    fetchServerConfig().then((cfg) => setShellEnabled(!!(cfg && cfg.shell_enabled)));
  }, []);
  return { shellEnabled };
}

/** 手機預設收合；偏好寫入 localStorage */
function useChatHeaderCollapsed() {
  const isMobile = useMediaQuery('(max-width: 639px)');
  const [collapsed, setCollapsedState] = useState(() => {
    const saved = readChatHeaderCollapsedPref();
    if (saved != null) return saved;
    return typeof window !== 'undefined' && window.matchMedia('(max-width: 639px)').matches;
  });
  const setCollapsed = useCallback((v) => {
    setCollapsedState(v);
    writeChatHeaderCollapsedPref(v);
  }, []);
  return {
    collapsed: isMobile && collapsed,
    setCollapsed,
    isMobile,
    toggle: () => setCollapsed(!collapsed),
  };
}

function formatKiroCreditUsed(displayText) {
  const pct = String(displayText).match(/Credits\s+([\d.]+)\s*%/);
  const frac = String(displayText).match(/\(([\d.]+)\s*\/\s*([\d.]+)\)/);
  if (!pct && !frac) return '';
  const fmt = (n) => (Math.abs(n - Math.round(n)) < 0.05 ? String(Math.round(n)) : n.toFixed(1));
  if (pct && frac) return pct[1] + '% (' + fmt(parseFloat(frac[1])) + '/' + fmt(parseFloat(frac[2])) + ')';
  if (pct) return pct[1] + '%';
  return fmt(parseFloat(frac[1])) + '/' + fmt(parseFloat(frac[2]));
}

function formatQuotaCompact(displayText, agentType) {
  if (!displayText || displayText === '—') return '';
  if (String(agentType || '').toLowerCase() === 'kiro' || String(agentType || '').toLowerCase() === 'kiroacp') {
    const kiro = formatKiroCreditUsed(displayText);
    if (kiro) return kiro;
  }
  const first = String(displayText).split(' · ')[0].trim();
  if (first.length <= 11) return first;
  const m = first.match(/(\d+(?:\.\d+)?)\s*%/);
  return m ? m[0] : first.slice(0, 10) + '…';
}

function permModeLabelFor(agentType, value) {
  const hit = permModeOptionsFor(agentType).find((o) => o.value === value);
  return hit ? hit.label : value;
}

function permModeIsDanger(value) {
  return value === 'bypassPermissions' || value === 'yolo';
}

function sessionStateChipClass(state) {
  // 與 session 列表下方方塊 badge 同構：無邊框、軟底色
  if (state === 'IDLE' || state === 'SHELL_IDLE') return 'bg-[oklch(0.7_0.15_155_/16%)] text-[oklch(0.7_0.15_155)]';
  if (state === 'THINKING') return 'bg-violet-500/20 text-violet-300';
  if (state === 'STREAMING') return 'bg-sky-500/20 text-sky-300';
  if (state === 'SHELL_EXEC' || state === 'SHELL_RUNNING') return 'bg-amber-500/20 text-amber-200';
  if (state === 'AWAITING_SHELL_CONFIRM' || state === 'SHELL_AWAITING_APPROVAL' || state === 'AWAITING_CONFIRM') return 'bg-orange-500/20 text-orange-200';
  return 'bg-amber-500/20 text-amber-200';
}

