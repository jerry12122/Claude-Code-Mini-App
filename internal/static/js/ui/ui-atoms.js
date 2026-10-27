/** 狀態標籤的活動提示（如執行中的指令）上限字數，超過以 `...` 截斷，避免擠到右側圖示 */
const CHIP_ACTIVITY_HINT_MAX_CHARS = 16;

function trimChipActivityHint(text) {
  const chars = [...String(text || '')];
  return chars.length > CHIP_ACTIVITY_HINT_MAX_CHARS
    ? chars.slice(0, CHIP_ACTIVITY_HINT_MAX_CHARS).join('') + '...'
    : chars.join('');
}

function SessionStateChip({ state, activityHint, compact, className = '' }) {
  const label = compact
    ? (state === 'THINKING' && activityHint ? '思考' : (SESSION_STATE_LABEL[state] || state))
    : (state === 'THINKING' && activityHint ? trimChipActivityHint(activityHint) : (SESSION_STATE_LABEL[state] || state));
  const dot =
    state === 'IDLE' || state === 'SHELL_IDLE' ? 'bg-[oklch(0.7_0.15_155)]' :
    state === 'THINKING' || state === 'STREAMING' || state === 'SHELL_EXEC' || state === 'SHELL_RUNNING' ? 'bg-[oklch(0.75_0.15_70)]' :
    'bg-[oklch(0.75_0.15_70)]';
  return (
    <span
      title={state}
      className={'inline-flex items-center gap-1.5 shrink-0 text-[11px] px-[7px] py-[3px] rounded-md font-semibold ' + sessionStateChipClass(state) + ' ' + className}
    >
      <span className={'w-1.5 h-1.5 rounded-full shrink-0 ' + dot} aria-hidden />
      {label}
    </span>
  );
}

function AgentIconChip({ agentType, className = '' }) {
  const t = String(agentType || 'claude').toLowerCase();
  return (
    <span
      className={'inline-flex items-center justify-center w-5 h-5 shrink-0 rounded-md border border-gray-700/55 ' + getAgentBadgeClass(agentType) + ' ' + className}
      title={AGENT_LABEL[t] || t}
    >
      <AgentBadgeIcon agentType={agentType} />
    </span>
  );
}

const INPUT_MODE_STORAGE_PREFIX = 'cc_input_mode:';
/** 舊版單一全域鍵，僅在該 session 尚無專用紀錄時作為向後相容讀取 */
const INPUT_MODE_LEGACY_KEY = 'cc_input_mode';

/** 輸入框 `/` 觸發之 slash 命令清單（可選 `modes` 限定 inputMode，省略則全部顯示） */
const SLASH_COMMANDS = [
  {
    command: '/reset',
    description: '清除對話 context（等同 /clear）',
  },
  {
    command: '/clear',
    description: '清除對話 context（等同 /reset）',
  },
];

function inputModeStorageKey(sessionId) {
  return INPUT_MODE_STORAGE_PREFIX + String(sessionId || '');
}

const DRAFT_INPUT_STORAGE_PREFIX = 'cc_draft_input:';

function draftInputStorageKey(sessionId) {
  return DRAFT_INPUT_STORAGE_PREFIX + String(sessionId || '');
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

/**
 * 依 agent_type 回傳可用的 permission_mode 下拉選項。
 * - Claude / Cursor: default / acceptEdits / bypassPermissions
 * - Antigravity: default / acceptEdits / plan / bypassPermissions（對應 --mode／--dangerously-skip-permissions）
 * - Codex: 暫不支援
 * 所有值共用 DB 欄位 `permission_mode`，後端各 runner 自行解釋。
 */
function permModeOptionsFor(agentType) {
  const t = String(agentType || 'claude').toLowerCase();
  if (t === 'antigravity') {
    return [
      { value: 'default', label: '預設' },
      { value: 'acceptEdits', label: '自動編輯' },
      { value: 'plan', label: '規劃模式' },
      { value: 'bypassPermissions', label: '跳過授權 ⚠️' },
    ];
  }
  if (t === 'kiroacp') {
    return [
      { value: 'default', label: '互動授權' },
      { value: 'bypassPermissions', label: '全自動放行 ⚠️' },
    ];
  }
  if (t === 'kiro' || t === 'codex' || t === 'telegram') {
    return [{ value: 'default', label: '預設（固定）' }];
  }
  return [
    { value: 'default', label: '預設' },
    { value: 'acceptEdits', label: '自動編輯' },
    { value: 'bypassPermissions', label: '跳過授權 ⚠️' },
  ];
}

/**
 * 建立 Session 時「自訂 CLI 引數」：每行一個 argv，trim 後略過空行。
 * 路徑含空格時請整段寫在同一行（勿用空格拆成多段）。
 */
function parseCliExtraArgs(text) {
  if (text == null || !String(text).trim()) return [];
  return String(text)
    .split(/\r?\n/)
    .map((line) => line.trim())
    .filter((line) => line.length > 0);
}

function extractModelFromCliArgs(parts) {
  if (!Array.isArray(parts)) return '';
  for (let i = 0; i < parts.length; i++) {
    if (parts[i] === '--model' || parts[i] === '-m') return String(parts[i + 1] || '').trim();
  }
  return '';
}

function cliArgsWithoutModel(parts) {
  if (!Array.isArray(parts)) return [];
  const out = [];
  for (let i = 0; i < parts.length; i++) {
    if (parts[i] === '--model' || parts[i] === '-m') {
      i++;
      continue;
    }
    out.push(parts[i]);
  }
  return out;
}

function mergeModelIntoCliExtraLines(cliText, modelVal) {
  const baseParts = parseCliExtraArgs(cliText);
  const stripped = [];
  for (let i = 0; i < baseParts.length; i++) {
    if (baseParts[i] === '--model' || baseParts[i] === '-m') {
      i++;
      continue;
    }
    stripped.push(baseParts[i]);
  }
  const m = String(modelVal || '').trim();
  if (m) {
    stripped.push('--model', m);
  }
  return stripped.join('\n');
}

function buildForwardUserPrompt(note, originalBody) {
  const n = String(note || '').trim();
  const o = String(originalBody || '').trim();
  if (!n) return o;
  return `${n}\n\n---\n（以下為規劃內容）\n\n${o}`;
}

function sendPromptViaEphemeralWS(sessionId, text) {
  return new Promise((resolve, reject) => {
    let settled = false;
    const settle = (fn) => {
      if (settled) return;
      settled = true;
      clearTimeout(timer);
      fn();
    };
    let opened = false;
    const ws = new WebSocket(wsURL(sessionId));
    const timer = setTimeout(() => {
      try {
        ws.close();
      } catch (_) {}
      settle(() => reject(new Error('連線逾時')));
    }, 20000);
    ws.onopen = () => {
      opened = true;
      try {
        ws.send(JSON.stringify({ type: 'input', data: text }));
      } catch (e) {
        settle(() => reject(e));
        try {
          ws.close();
        } catch (_) {}
        return;
      }
      setTimeout(() => {
        try {
          ws.close();
        } catch (_) {}
      }, 80);
    };
    ws.onerror = () => settle(() => reject(new Error('WebSocket 錯誤')));
    ws.onclose = () => {
      if (opened) settle(() => resolve());
      else settle(() => reject(new Error('無法連線')));
    };
  });
}

/**
 * 新建 session 後送第一則訊息。失敗時 session 已存在，不能讓使用者重按（會重複建立）也不能丟訊息，
 * 所以改放進新 session 的草稿，進聊天室後自己重送。回傳是否送成功。
 */
async function sendFirstPromptOrDraft(sessionId, text) {
  try {
    await sendPromptViaEphemeralWS(sessionId, text);
    return true;
  } catch (_) {
    try { localStorage.setItem(draftInputStorageKey(sessionId), text); } catch (__) {}
    showToast('Session 已建立，但首則訊息未送出，已放入輸入框', { error: true, duration: 4000 });
    return false;
  }
}

/** 切換 agent_type 時，若舊的 mode 在新 agent 下不合法，退回 default。 */
function normalizePermMode(agentType, mode) {
  const valid = permModeOptionsFor(agentType).map((o) => o.value);
  return valid.includes(mode) ? mode : 'default';
}

/**
 * 權限模式分段開關（多態切換，取代下拉選單）。
 * danger 值（跳過授權 / YOLO）選中時以琥珀色強調。
 */
function PermModeSwitch({ agentType, value, onChange, disabled, title }) {
  const opts = permModeOptionsFor(agentType);
  const t = String(agentType || 'claude').toLowerCase();
  const ariaLabel =
    title || (t === 'antigravity' ? 'Antigravity 核准模式' : '權限模式');
  return (
    <div
      role="radiogroup"
      aria-label={ariaLabel}
      className={`inline-flex h-9 max-w-full flex-wrap items-stretch gap-0.5 rounded-lg border border-gray-700 bg-gray-800 p-[3px] ${
        disabled ? 'pointer-events-none opacity-70' : ''
      }`}
    >
      {opts.map((opt) => {
        const active = value === opt.value;
        const danger = opt.value === 'bypassPermissions' || opt.value === 'yolo';
        const label = opt.label;
        return (
          <button
            key={opt.value}
            type="button"
            role="radio"
            aria-checked={active}
            disabled={disabled}
            onClick={() => onChange(opt.value)}
            className={[
              'h-full flex items-center justify-center shrink-0 grow-0 px-3 text-xs font-semibold rounded-[7px] transition-[color,background-color] duration-150 whitespace-nowrap',
              'focus:outline-none focus-visible:ring-2 focus-visible:ring-violet-500/50',
              active
                ? danger
                  ? 'bg-[oklch(0.75_0.15_70_/16%)] text-[oklch(0.75_0.15_70)] font-bold'
                  : 'bg-[oklch(0.62_0.19_275_/18%)] text-[oklch(0.85_0.08_275)]'
                : 'text-[oklch(0.55_0.01_264)] hover:text-[oklch(0.85_0.01_264)]',
            ].join(' ')}
          >
            {label}
          </button>
        );
      })}
    </div>
  );
}

/** 下拉選單右側的自訂 chevron（蓋掉瀏覽器原生箭頭，appearance-none 後手畫一個） */
function SelectChevron({ danger, compact }) {
  return (
    <svg
      viewBox="0 0 20 20"
      fill="currentColor"
      aria-hidden
      className={`pointer-events-none absolute top-1/2 -translate-y-1/2 ${compact ? 'right-1.5 w-3 h-3' : 'right-2.5 w-3.5 h-3.5'} ${danger ? 'text-amber-400/80' : 'text-gray-500'}`}
    >
      <path fillRule="evenodd" d="M5.23 7.21a.75.75 0 011.06.02L10 11.168l3.71-3.94a.75.75 0 111.08 1.04l-4.25 4.5a.75.75 0 01-1.08 0l-4.25-4.5a.75.75 0 01.02-1.06z" clipRule="evenodd" />
    </svg>
  );
}

const EFFORT_OPTIONS = ['low', 'medium', 'high', 'xhigh', 'max'];

/** 下拉選單共用樣式：appearance-none + 自畫 chevron，邊框/圓角/hover 跟 PermModeSwitch 對齊 */
const selectBaseClass = (danger) => [
  'w-full min-w-0 h-9 appearance-none cursor-pointer rounded-lg pl-3 pr-8 py-1.5 text-sm font-medium truncate',
  'focus:outline-none disabled:opacity-45 disabled:cursor-not-allowed',
  'transition-[color,background-color,border-color,box-shadow] duration-150',
  danger
    ? 'border border-amber-600/75 bg-amber-950/45 text-amber-50 shadow-sm shadow-amber-950/35 focus:border-amber-500 focus:ring-1 focus:ring-amber-600/35'
    : 'border border-gray-700 bg-gray-800 text-gray-200 hover:border-gray-600 hover:bg-gray-700/60 focus:border-violet-600 focus:ring-1 focus:ring-violet-600/30',
].join(' ').replace(/\s+/g, ' ').trim();

/** 輸入列用的精簡 chip 樣式（compact prop）：無邊框、h-8、小字，仍是原生 <select> */
const selectCompactClass = (danger) => [
  'h-8 max-w-[9rem] appearance-none cursor-pointer rounded-md pl-2 pr-5 text-xs font-medium truncate',
  'focus:outline-none focus-visible:ring-1 focus-visible:ring-violet-500/50 disabled:opacity-45 disabled:cursor-not-allowed',
  'transition-colors duration-150',
  danger
    ? 'bg-amber-950/45 text-amber-300 hover:bg-amber-950/65'
    : 'bg-gray-800/80 text-gray-300 hover:bg-gray-700/80 hover:text-gray-100',
].join(' ').replace(/\s+/g, ' ').trim();

/** compact 時的外層/select class；未傳 compact 維持既有輸出 */
const selectWrapClass = (compact, className) => compact ? 'relative shrink-0 min-w-0' : `relative min-w-0 ${className || 'w-full'}`;
const selectClass = (compact, danger, className) => compact ? selectCompactClass(danger) : `${selectBaseClass(danger)} ${className || ''}`;

/** 權限模式：窄螢幕（app 版）用下拉選單，與 PermModeSwitch 選項一致。danger 值會套用琥珀邊框（與分段按鈕語意一致）。 */
function PermModeSelect({ agentType, value, onChange, disabled, title, id, className, compact }) {
  const opts = permModeOptionsFor(agentType);
  const t = String(agentType || 'claude').toLowerCase();
  const ariaLabel =
    title || (t === 'antigravity' ? 'Antigravity 核准模式' : '權限模式');
  const selectId = id || `perm-mode-select-${t}`;
  const danger = value === 'bypassPermissions' || value === 'yolo';
  return (
    <div className={selectWrapClass(compact, className)}>
      <select
        id={selectId}
        aria-label={ariaLabel}
        value={value}
        disabled={disabled}
        onChange={(e) => onChange(e.target.value)}
        className={selectClass(compact, danger, className)}
      >
        {opts.map((opt) => (
          <option key={opt.value} value={opt.value}>{opt.label}</option>
        ))}
      </select>
      <SelectChevron danger={danger} compact={compact} />
    </div>
  );
}

/** Model 下拉選單：選項打 GET /model-options/:agentType，固定含「預設（不指定）」。 */
function ModelSelect({ agentType, value, onChange, disabled, id, className, compact }) {
  const [opts, setOpts] = useState([]);
  useEffect(() => {
    let cancelled = false;
    apiFetch(`/model-options/${encodeURIComponent(agentType)}`)
      .then((res) => (res.ok ? res.json() : []))
      .then((list) => { if (!cancelled) setOpts(Array.isArray(list) ? list : []); })
      .catch(() => { if (!cancelled) setOpts([]); });
    return () => { cancelled = true; };
  }, [agentType]);
  return (
    <div className={selectWrapClass(compact, className)}>
      <select
        id={id || `model-select-${agentType}`}
        aria-label="Model"
        value={value || ''}
        disabled={disabled}
        onChange={(e) => onChange(e.target.value)}
        className={selectClass(compact, false, className)}
      >
        <option value="">預設</option>
        {opts.map((opt) => (
          <option key={opt.model_id} value={opt.model_id}>{opt.label}</option>
        ))}
      </select>
      <SelectChevron compact={compact} />
    </div>
  );
}

/** Effort 下拉選單：固定 low/medium/high/xhigh/max，含「預設（不指定）」。Cursor 不支援，呼叫端不應渲染。 */
function EffortSelect({ value, onChange, disabled, id, className, compact }) {
  return (
    <div className={selectWrapClass(compact, className)}>
      <select
        id={id || 'effort-select'}
        aria-label="Effort"
        value={value || ''}
        disabled={disabled}
        onChange={(e) => onChange(e.target.value)}
        className={selectClass(compact, false, className)}
      >
        <option value="">預設</option>
        {EFFORT_OPTIONS.map((v) => (
          <option key={v} value={v}>{v}</option>
        ))}
      </select>
      <SelectChevron compact={compact} />
    </div>
  );
}

/** 依 CLI runner（agent_type）回傳 badge 的 Tailwind 底色／文字色 */

function sessionStatusDotClass(s) {
  const st = String(s && s.status || '').toLowerCase();
  if (st === 'running') return 'ra-status-dot running';
  if (st === 'awaiting_confirm' || st === 'awaiting_input') return 'ra-status-dot awaiting';
  return '';
}

function sessionStatusLabel(s) {
  const st = String(s && s.status || '').toLowerCase();
  if (st === 'running') return '執行中';
  if (st === 'awaiting_confirm') return '待授權';
  if (st === 'awaiting_input') return '待回答';
  return '';
}

/** 回傳 session 顯示用的 agent_type（若為 Telegram 偽裝會話，回傳偽裝的 agent）。 */
function sessionDisplayAgent(s) {
  if (!s) return 'claude';
  if (typeof s === 'string') return s;
  if (s.agent_type === 'telegram' && s.disguise_agent) {
    return s.disguise_agent;
  }
  return s.agent_type || 'claude';
}

function getAgentBadgeClass(agentType, session) {
  let t = String(agentType || 'claude').toLowerCase();
  if (session && session.agent_type === 'telegram' && session.disguise_agent) {
    t = session.disguise_agent.toLowerCase();
  }
  const map = {
    claude: 'bg-orange-500/20 text-orange-300',
    cursor: 'bg-emerald-500/20 text-emerald-300',
    codex: 'bg-slate-500/20 text-slate-300',
    antigravity: 'bg-sky-500/20 text-sky-300',
    kiro: 'bg-fuchsia-500/20 text-fuchsia-300',
    kiroacp: 'bg-fuchsia-500/20 text-fuchsia-300',
    telegram: 'bg-sky-500/20 text-sky-300',
  };
  return map[t] || 'bg-[oklch(0.24_0.02_264)] text-[oklch(0.65_0.01_264)]';
}

/**
 * 廠商圖示：Simple Icons CDN（https://cdn.simpleicons.org/{slug}/{hex}）
 * slug 見 https://simpleicons.org/ ；色碼與 badge 文字色對齊（無 #）
 */
const AGENT_BADGE_ICON_CDN = {
  claude: { slug: 'claude', color: 'fed7aa' },
  cursor: { slug: 'cursor', color: '6ee7b7' },
  antigravity: { slug: 'google', color: '7dd3fc' },
  telegram: { slug: 'telegram', color: '38bdf8' },
};

/** Simple Icons 已移除 openai / amazonwebservices，改內嵌 path（simple-icons@14，CC0） */
const AGENT_BADGE_INLINE_SVG = {
  codex: {
    viewBox: '0 0 24 24',
    color: '#e2e8f0',
    path: 'M22.2819 9.8211a5.9847 5.9847 0 0 0-.5157-4.9108 6.0462 6.0462 0 0 0-6.5098-2.9A6.0651 6.0651 0 0 0 4.9807 4.1818a5.9847 5.9847 0 0 0-3.9977 2.9 6.0462 6.0462 0 0 0 .7427 7.0966 5.98 5.98 0 0 0 .511 4.9107 6.051 6.051 0 0 0 6.5146 2.9001A5.9847 5.9847 0 0 0 13.2599 24a6.0557 6.0557 0 0 0 5.7718-4.2058 5.9894 5.9894 0 0 0 3.9977-2.9001 6.0557 6.0557 0 0 0-.7475-7.0729zm-9.022 12.6081a4.4755 4.4755 0 0 1-2.8764-1.0408l.1419-.0804 4.7783-2.7582a.7948.7948 0 0 0 .3927-.6813v-6.7369l2.02 1.1686a.071.071 0 0 1 .038.052v5.5826a4.504 4.504 0 0 1-4.4945 4.4944zm-9.6607-4.1254a4.4708 4.4708 0 0 1-.5346-3.0137l.142.0852 4.783 2.7582a.7712.7712 0 0 0 .7806 0l5.8428-3.3685v2.3324a.0804.0804 0 0 1-.0332.0615L9.74 19.9502a4.4992 4.4992 0 0 1-6.1408-1.6464zM2.3408 7.8956a4.485 4.485 0 0 1 2.3655-1.9728V11.6a.7664.7664 0 0 0 .3879.6765l5.8144 3.3543-2.0201 1.1685a.0757.0757 0 0 1-.071 0l-4.8303-2.7865A4.504 4.504 0 0 1 2.3408 7.872zm16.5963 3.8558L13.1038 8.364 15.1192 7.2a.0757.0757 0 0 1 .071 0l4.8303 2.7913a4.4944 4.4944 0 0 1-.6765 8.1042v-5.6772a.79.79 0 0 0-.407-.667zm2.0107-3.0231l-.142-.0852-4.7735-2.7818a.7759.7759 0 0 0-.7854 0L9.409 9.2297V6.8974a.0662.0662 0 0 1 .0284-.0615l4.8303-2.7866a4.4992 4.4992 0 0 1 6.6802 4.66zM8.3065 12.863l-2.02-1.1638a.0804.0804 0 0 1-.038-.0567V6.0742a4.4992 4.4992 0 0 1 7.3757-3.4537l-.142.0805L8.704 5.459a.7948.7948 0 0 0-.3927.6813zm1.0976-2.3654l2.602-1.4998 2.6069 1.4998v2.9994l-2.5974 1.4997-2.6067-1.4997Z',
  },
  // Kiro 幽靈（lobe-icons，單色）；眼睛是挖空的，需搭配 fillRule evenodd
  kiro: {
    viewBox: '0 0 24 24',
    color: '#d8b4fe',
    fillRule: 'evenodd',
    path: 'M4.594 6.677C6.67-2.226 18.746-2.211 21.16 6.632c.353 1.297 1.725 7.582-1.673 13.747-1.545 2.797-5.841 5.49-6.99 1.883C8.6 25.477 3.315 24.1 5.789 18.609l-.318.143c-3.57 1.305-3.863-1.208-3.173-2.513.45-.84.727-1.335.937-1.897.353-.975.458-1.568.593-2.498.27-1.837.277-3.607.765-5.167zm8.37.01a.92.92 0 00-.81.428c-.217.323-.33.825-.33 1.462 0 .705.15 1.89 1.14 1.89h.008c.757 0 1.214-.705 1.214-1.89 0-.622-.127-1.125-.367-1.455a1.014 1.014 0 00-.855-.435zm4.08 0a.92.92 0 00-.81.428c-.217.323-.33.825-.33 1.462 0 .705.15 1.89 1.14 1.89h.008c.757 0 1.215-.705 1.215-1.89 0-.622-.128-1.125-.368-1.455a1.014 1.014 0 00-.855-.435z',
  },
};

function AgentBadgeIcon({ agentType, session }) {
  let t0 = String(agentType || 'claude').toLowerCase();
  if (session && session.agent_type === 'telegram' && session.disguise_agent) {
    t0 = session.disguise_agent.toLowerCase();
  }
  const t = t0 === 'kiroacp' ? 'kiro' : t0;
  const inline = AGENT_BADGE_INLINE_SVG[t];
  if (inline) {
    return (
      <svg
        viewBox={inline.viewBox}
        width={14}
        height={14}
        aria-hidden="true"
        className="w-3.5 h-3.5 shrink-0 opacity-95"
        fill={inline.color}
        fillRule={inline.fillRule}
      >
        <path d={inline.path} />
      </svg>
    );
  }
  const cfg = AGENT_BADGE_ICON_CDN[t] || { slug: 'anthropic', color: 'd1d5db' };
  const src = `https://cdn.simpleicons.org/${cfg.slug}/${cfg.color}`;
  return (
    <img
      src={src}
      alt=""
      width={14}
      height={14}
      loading="lazy"
      decoding="async"
      referrerPolicy="no-referrer"
      className="w-3.5 h-3.5 shrink-0 opacity-95 object-contain [content-visibility:auto]"
      onError={(e) => {
        e.currentTarget.removeAttribute('src');
        e.currentTarget.style.display = 'none';
      }}
    />
  );
}

function formatQuotaAge(iso) {
  if (!iso) return '';
  const t = Date.parse(iso);
  if (!Number.isFinite(t)) return '';
  const sec = Math.max(0, Math.floor((Date.now() - t) / 1000));
  if (sec < 60) return '剛剛更新';
  const min = Math.floor(sec / 60);
  if (min < 60) return min + ' 分鐘前';
  return Math.floor(min / 60) + ' 小時前';
}

/** display_text 內取最大的 N% 數字（無則回 null） */
function extractMaxQuotaPercent(text) {
  const matches = String(text || '').match(/(\d+(?:\.\d+)?)%/g);
  if (!matches) return null;
  return Math.max(...matches.map((m) => parseFloat(m)));
}

/** quota 文字顏色：error > 100%/80% 警示 > stale > 預設。segmentText 未帶 % 時退回整體 quota.display_text 判斷 */
function quotaTextColorClass(quota, segmentText) {
  const pct = extractMaxQuotaPercent(segmentText !== undefined ? segmentText : quota && quota.display_text);
  return quota.error
    ? 'text-amber-400'
    : pct !== null && pct >= 100
    ? 'text-red-400'
    : pct !== null && pct >= 80
    ? 'text-orange-400'
    : quota.stale
    ? 'text-gray-500'
    : 'text-gray-400';
}

/** 依 ' · ' 拆段落，各段依自己的 % 上色（例如 Cursor 的 Total/Auto/API 各自獨立） */
function QuotaSegments({ quota, text, className = '' }) {
  const segments = String(text).split(' · ');
  return segments.map((seg, i) => (
    <React.Fragment key={i}>
      {i > 0 ? <span className="text-gray-500"> · </span> : null}
      <span className={quotaTextColorClass(quota, seg) + ' ' + className}>{seg}</span>
    </React.Fragment>
  ));
}

/**
 * 結構化用量（quota.windows）：各 runner 的版型設定表。bars 依 window key 對應，對不到的略過；
 * tip 是 tooltip 要列的窗口（含不畫條的，例如 Cursor 的 Auto/API）；showPct 控制條旁是否帶標籤與百分比。
 */
const QUOTA_LAYOUTS = (() => {
  const fiveWeek = { groups: [{ bars: [{ key: 'session', label: '5h' }, { key: 'weekly', label: 'Week' }] }], tip: [['session', '5h'], ['weekly', 'Week']], showPct: true };
  const kiro = { groups: [{ bars: [{ key: 'credits', label: 'Credits' }] }], tip: [['credits', 'Credits']], showPct: true, showUsed: true };
  return {
    claude: fiveWeek,
    codex: fiveWeek,
    cursor: { groups: [{ bars: [{ key: 'billing_total', label: 'Total' }] }], tip: [['billing_total', 'Total'], ['billing_auto', 'Auto'], ['billing_api', 'API']], showPct: true },
    kiro,
    kiroacp: kiro,
    antigravity: {
      groups: [
        { label: 'Gemini', bars: [{ key: 'session', label: '5h' }, { key: 'weekly', label: 'Week' }] },
        { label: 'Claude/GPT', bars: [{ key: 'session_claude', label: '5h' }, { key: 'weekly_claude', label: 'Week' }] },
      ],
      tip: [['session', 'Gemini 5h'], ['weekly', 'Gemini Week'], ['session_claude', 'Claude/GPT 5h'], ['weekly_claude', 'Claude/GPT Week']],
      showPct: false,
    },
  };
})();

function quotaLayoutFor(agentType) {
  const t = String(agentType || '').toLowerCase();
  return QUOTA_LAYOUTS[t === 'gemini' ? 'antigravity' : t] || null;
}

function quotaWinPercent(w) {
  if (!w) return null;
  if (typeof w.percent === 'number' && Number.isFinite(w.percent)) return w.percent;
  if (typeof w.used === 'number' && typeof w.limit === 'number' && w.limit > 0) return (w.used / w.limit) * 100;
  return null;
}

function quotaFmtNum(n) {
  return Math.abs(n - Math.round(n)) < 0.05 ? String(Math.round(n)) : n.toFixed(1);
}

/** 對應 layout 的窗口；windows 缺失／空／全是未知 key 時回 null（呼叫端退回文字顯示） */
function resolveQuotaBars(quota, agentType) {
  const layout = quotaLayoutFor(agentType);
  if (!layout || !quota || !Array.isArray(quota.windows)) return null;
  const byKey = {};
  quota.windows.forEach((w) => { if (w && w.key && !(w.key in byKey)) byKey[w.key] = w; });
  const groups = layout.groups
    .map((g) => ({
      label: g.label,
      bars: g.bars
        .map((b) => ({ ...b, win: byKey[b.key], pct: quotaWinPercent(byKey[b.key]) }))
        .filter((b) => b.pct !== null),
    }))
    .filter((g) => g.bars.length > 0);
  return groups.length ? { layout, groups, byKey } : null;
}

/** 進度條顏色：error 琥珀 > ≥100 紅 > ≥80 橘 > stale 灰 > 預設（與文字版 quotaTextColorClass 同順序） */
function quotaBarTone(quota, pct) {
  if (quota.error) return { bg: 'bg-amber-400', text: 'text-amber-400' };
  if (pct >= 100) return { bg: 'bg-red-400', text: 'text-red-400' };
  if (pct >= 80) return { bg: 'bg-orange-400', text: 'text-orange-400' };
  if (quota.stale) return { bg: 'bg-gray-500', text: 'text-gray-500' };
  return { bg: 'bg-violet-400', text: 'text-gray-400' };
}

/** resets_at（ISO）→ "Resets in 3 hr 25 min"；解析失敗或已過期回空字串 */
function quotaResetCountdown(iso) {
  const t = Date.parse(iso || '');
  if (!Number.isFinite(t)) return '';
  const mins = Math.round((t - Date.now()) / 60000);
  if (mins <= 0) return '';
  if (mins < 60) return 'Resets in ' + mins + ' min';
  const h = Math.floor(mins / 60);
  const m = mins % 60;
  return 'Resets in ' + h + ' hr' + (m ? ' ' + m + ' min' : '');
}

/** tooltip：各窗口名稱／百分比／倒數，最後附完整 display_text */
function quotaTooltip(quota, agentType) {
  const layout = quotaLayoutFor(agentType);
  const lines = [];
  if (layout && Array.isArray(quota.windows)) {
    layout.tip.forEach(([key, name]) => {
      const w = quota.windows.find((x) => x && x.key === key);
      const pct = quotaWinPercent(w);
      if (pct === null) return;
      let line = name + ' ' + Math.round(pct * 10) / 10 + '%';
      if (typeof w.used === 'number' && typeof w.limit === 'number') line += ' (' + quotaFmtNum(w.used) + '/' + quotaFmtNum(w.limit) + ')';
      const left = quotaResetCountdown(w.resets_at);
      if (left) line += ' · ' + left;
      lines.push(line);
    });
  }
  if (quota.plan) lines.push('方案 ' + quota.plan);
  const age = formatQuotaAge(quota.updated_at);
  if (age) lines.push(age);
  if (quota.error) lines.push(quota.error);
  if (quota.display_text && quota.display_text !== '—') lines.push(quota.display_text);
  return lines.join('\n');
}

function QuotaBar({ quota, pct, bar, className = 'w-12' }) {
  const tone = quotaBarTone(quota, pct);
  return (
    <span data-quota-bar={bar} data-pct={Math.round(pct)} className={'inline-block h-1 rounded-full bg-gray-700/70 overflow-hidden shrink-0 ' + className}>
      <span className={'block h-full rounded-full ' + tone.bg} style={{ width: Math.max(0, Math.min(100, pct)) + '%' }} />
    </span>
  );
}

/** 依 runner 版型顯示的精簡用量條；resolveQuotaBars 為 null 時不渲染（呼叫端先判斷、退回文字） */
function QuotaUsage({ quota, agentType }) {
  const res = resolveQuotaBars(quota, agentType);
  if (!res) return null;
  const { layout, groups } = res;
  return (
    <span data-quota-usage className="inline-flex items-center gap-2 whitespace-nowrap shrink-0 text-[10px] ra-mono" title={quotaTooltip(quota, agentType)}>
      {groups.map((g, gi) => (
        <span key={gi} className="inline-flex items-center gap-1">
          {g.label ? <span className="text-gray-500">{g.label}</span> : null}
          {g.bars.map((b) => (
            <span key={b.key} className="inline-flex items-center gap-1">
              {layout.showPct ? <span className="text-gray-500">{b.label}</span> : null}
              <QuotaBar quota={quota} pct={b.pct} bar={b.key} className={layout.showPct ? 'w-12' : 'w-7'} />
              {layout.showPct ? <span className={quotaBarTone(quota, b.pct).text}>{Math.round(b.pct)}%</span> : null}
              {layout.showUsed && b.win && typeof b.win.used === 'number' && typeof b.win.limit === 'number' ? (
                <span className="text-gray-500">{quotaFmtNum(b.win.used)}/{quotaFmtNum(b.win.limit)}</span>
              ) : null}
            </span>
          ))}
        </span>
      ))}
    </span>
  );
}

/** 手機晶片用：版型內所有已知窗口中 percent 最高者；對不到回 null */
function quotaPeakPercent(quota, agentType) {
  const layout = quotaLayoutFor(agentType);
  if (!layout || !quota || !Array.isArray(quota.windows)) return null;
  const pcts = layout.tip.map(([key]) => quotaWinPercent(quota.windows.find((x) => x && x.key === key))).filter((p) => p !== null);
  return pcts.length ? Math.max(...pcts) : null;
}

/** 帳戶 quota %（後端已組好 display_text） */
function QuotaBadge({ quota, onRefresh, refreshing, className = '', compact = false, agentType = '' }) {
  const peak = compact ? quotaPeakPercent(quota, agentType) : null;
  const text = quota && quota.display_text;
  if (peak === null && (!text || text === '—')) return null;
  const shown = compact ? formatQuotaCompact(text, agentType) : text;
  if (peak === null && !shown) return null;
  const age = formatQuotaAge(quota.updated_at);
  const title = peak !== null ? quotaTooltip(quota, agentType) : [text, age, quota.error].filter(Boolean).join(' · ');
  const compactMaxW = ['kiro', 'kiroacp'].includes(String(agentType || '').toLowerCase()) ? 'max-w-[6.5rem]' : 'max-w-[4.5rem]';
  const chipCls = compact
    ? 'inline-flex items-center shrink-0 ' + compactMaxW + ' px-2 py-1 rounded-md bg-[oklch(0.24_0.02_264)] '
    : 'inline-flex items-center gap-0.5 min-w-0 max-w-full ';
  // 手機（compact）沒有獨立的刷新鍵，整個晶片可點擊來刷新
  const tappable = compact && !!onRefresh;
  const Chip = tappable ? 'button' : 'span';
  return (
    <Chip
      {...(tappable ? { type: 'button', onClick: onRefresh, disabled: refreshing } : {})}
      className={chipCls + className + (tappable ? ' cursor-pointer' : '') + (refreshing ? ' animate-pulse' : '')}
      title={tappable ? title + ' · 點擊刷新' : title}
    >
      <span className="text-[10px] font-mono truncate">
        {peak !== null ? (
          <span className="inline-flex items-center gap-1">
            <QuotaBar quota={quota} pct={peak} bar="peak" className="w-8" />
            <span className={quotaBarTone(quota, peak).text}>{Math.round(peak)}%</span>
          </span>
        ) : (
          <QuotaSegments quota={quota} text={shown} />
        )}
      </span>
      {!compact && onRefresh ? (
        <button
          type="button"
          onClick={onRefresh}
          disabled={refreshing}
          className={'shrink-0 text-[10px] px-0.5 ' + (refreshing ? 'text-violet-400 animate-spin' : 'text-gray-500 hover:text-gray-300')}
          title="刷新帳戶用量"
        >
          ↻
        </button>
      ) : null}
    </Chip>
  );
}

/** Session 目前 model（後端已組好 display_text） */
function ModelBadge({ model, className = '' }) {
  const text = model && model.display_text;
  if (!text || text === '—') return null;
  const title = [text, model.source, model.updated_at].filter(Boolean).join(' · ');
  return (
    <span className={'inline-flex items-center min-w-0 max-w-full ' + className} title={title}>
      <span className="text-[10px] sm:text-xs font-mono truncate text-violet-300/90">{text}</span>
    </span>
  );
}

/** Git 分支角標（compact：單行截斷；展開：truncate 上限避免撐高 header）。風格對齊 SessionStateChip：軟底色、無邊框、font-semibold */
function GitBranchBadge({ branch, compact = false }) {
  if (!branch) return null;
  return (
    <span
      className={
        'inline-flex items-center gap-1 min-w-0 text-[11px] font-semibold px-[7px] py-[3px] rounded-md bg-amber-500/20 text-amber-200 shrink-0 ' +
        (compact ? 'max-w-[5.5rem]' : 'max-w-[min(12rem,42vw)] sm:max-w-[14rem]')
      }
      title={'Git 分支：' + branch}
    >
      <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 16 16" fill="currentColor" className="w-2.5 h-2.5 shrink-0" aria-hidden>
        <path d="M4 1.5a1.5 1.5 0 11-1 2.83v5.34a1.5 1.5 0 11-1 0V4.33A1.5 1.5 0 014 1.5zm7.25 6.5a1.5 1.5 0 11-1 2.83V9c0-.83-.67-1.5-1.5-1.5H6.31a1.5 1.5 0 110-1h2.44A2.5 2.5 0 0111.25 9v-.17A1.5 1.5 0 1110.25 8z" />
      </svg>
      <span className="truncate">{branch}</span>
    </span>
  );
}

/** 解析 API 回傳的 last_active（SQLite datetime 字串）為毫秒時間戳 */
function sessionLastActiveMs(s) {
  if (!s || !s.last_active) return 0;
  const raw = String(s.last_active).trim();
  const t = Date.parse(raw.includes('T') ? raw : raw.replace(' ', 'T'));
  return Number.isNaN(t) ? 0 : t;
}

/**
 * 產生 optimistic update 用的「現在」字串，格式對齊 SQLite datetime('now')（YYYY-MM-DD HH:MM:SS）。
 * 用本地時間（不加時區標記），因為 sessionLastActiveMs／isUnread 都是以 Date.parse 忽略時區的方式解析，
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

/** 目錄分組標頭：顯示路徑最後兩段，避免過長 */
function workDirGroupShortLabel(dirKey) {
  const unset = '（未設定工作目錄）';
  if (!dirKey || dirKey === unset) return unset;
  const p = String(dirKey).replace(/[/\\]+$/, '');
  const parts = p.split(/[/\\]/).filter(Boolean);
  if (parts.length === 0) return dirKey;
  if (parts.length === 1) return parts[0];
  return `${parts[parts.length - 2]}/${parts[parts.length - 1]}`;
}

/** 路徑最後一段，作為預設 Session 名稱 */
function workDirBasename(dirKey) {
  const unset = '（未設定工作目錄）';
  if (!dirKey || dirKey === unset) return '';
  const p = String(dirKey).replace(/[/\\]+$/, '');
  const parts = p.split(/[/\\]/).filter(Boolean);
  return parts.length ? parts[parts.length - 1] : '';
}

/** 新建 session 用的工作目錄選擇：讀獨立目錄清單，不隨 session 刪除消失。 */
function WorkDirPicker({ value, onChange }) {
  const [dirs, setDirs] = useState([]);
  const [customDir, setCustomDir] = useState(false);

  useEffect(() => {
    let cancelled = false;
    apiFetch('/work-dirs')
      .then((res) => (res.ok ? res.json() : []))
      .then((data) => {
        if (!cancelled) setDirs(Array.isArray(data) ? data.filter(Boolean) : []);
      })
      .catch(() => { if (!cancelled) setDirs([]); });
    return () => { cancelled = true; };
  }, []);

  const showInput = customDir || dirs.length === 0;
  const options = value && !dirs.includes(value) ? [value, ...dirs] : dirs;
  const selectValue = options.includes(value) ? value : '';

  return (
    <div className="space-y-1.5">
      <div className="flex items-center justify-between">
        <div className="text-[10px] uppercase tracking-wide text-gray-500">工作目錄</div>
        {dirs.length > 0 && (
          <button
            type="button"
            onClick={() => setCustomDir((v) => !v)}
            className="text-[11px] text-violet-400 hover:text-violet-300"
          >
            {showInput ? '改選既有目錄' : '自訂目錄…'}
          </button>
        )}
      </div>
      {showInput ? (
        <input
          value={value}
          onChange={(e) => onChange(e.target.value)}
          placeholder="工作目錄（選填，如 /home/user/project）"
          className="w-full bg-gray-800 border border-gray-700 rounded-lg px-3 py-2 text-sm text-gray-200 placeholder-gray-500 font-mono focus:outline-none focus:border-violet-600"
        />
      ) : (
        <select
          value={selectValue}
          onChange={(e) => onChange(e.target.value)}
          className="w-full bg-gray-800 border border-gray-700 rounded-lg px-3 py-2 text-sm text-gray-200"
        >
          <option value="">（未設定工作目錄）</option>
          {options.map((d) => (
            <option key={d} value={d}>{workDirGroupShortLabel(d)}</option>
          ))}
        </select>
      )}
    </div>
  );
}

/** 依工作目錄排序用：空白路徑置底，其餘依路徑 localeCompare（numeric）。 */
function cmpSessionWorkDir(a, b) {
  const da = String(a.work_dir != null ? String(a.work_dir).trim() : '').toLowerCase();
  const db = String(b.work_dir != null ? String(b.work_dir).trim() : '').toLowerCase();
  if (!da && db) return 1;
  if (da && !db) return -1;
  return da.localeCompare(db, undefined, { numeric: true, sensitivity: 'base' });
}

function sortSessionsByWorkDirThenName(sessions) {
  if (!Array.isArray(sessions)) return [];
  return [...sessions].sort((a, b) => {
    const wd = cmpSessionWorkDir(a, b);
    if (wd !== 0) return wd;
    return String(a.name || '').localeCompare(String(b.name || ''), 'zh-Hant');
  });
}

function parseShellExitCode(content) {
  const m = content.match(/\[exit (-?\d+)\]\n?$/);
  return m ? parseInt(m[1], 10) : null;
}

function ShellOutput({ content, exitCode, streaming }) {
  const strippedContent = content.replace(/\n?\[exit -?\d+\]\n?$/, '');
  const resolvedExitCode = exitCode != null ? exitCode : (!streaming ? parseShellExitCode(content) : null);
  const lines = strippedContent.replace(/\r\n/g, '\n').replace(/\r/g, '\n').split('\n');
  return (
    <div>
      <pre className="text-xs font-mono whitespace-pre-wrap break-words leading-relaxed overflow-x-auto max-w-full">
        {lines.map((line, i) => {
          const nl = i < lines.length - 1 ? '\n' : '';
          if (line.startsWith('[stderr] ')) return <span key={i} className="text-red-400">{line.slice(9)}{nl}</span>;
          if (line.startsWith('[error] '))  return <span key={i} className="text-red-400">{line}{nl}</span>;
          if (line === '[interrupted]')     return <span key={i} className="text-yellow-400">{line}{nl}</span>;
          return <span key={i} className="text-gray-200">{line}{nl}</span>;
        })}
        {streaming && <span className="text-amber-400 animate-pulse">▌</span>}
      </pre>
      {!streaming && resolvedExitCode != null && (
        <div className={`mt-1.5 inline-flex items-center gap-1 text-[10px] font-mono px-1.5 py-0.5 rounded ${resolvedExitCode === 0 ? 'bg-green-900/60 text-green-300' : 'bg-red-900/60 text-red-300'}`}>
          {resolvedExitCode === 0 ? '✓' : '✗'} exit {resolvedExitCode}
        </div>
      )}
    </div>
  );
}

function InputModeTab({ value, onChange, disabled }) {
  const tabs = [{ v: 'agent', label: 'AI 代理' }, { v: 'shell', label: 'Shell' }];
  return (
    <div
      role="radiogroup"
      aria-label="輸入模式"
      className={`inline-flex max-w-full flex-wrap items-stretch gap-0.5 rounded-xl border border-gray-700/90 bg-gray-900/85 p-0.5 shadow-[inset_0_1px_0_rgba(255,255,255,0.04)] ${disabled ? 'pointer-events-none opacity-45' : ''}`}
    >
      {tabs.map(({ v, label }) => {
        const active = value === v;
        const isShell = v === 'shell';
        return (
          <button
            key={v}
            type="button"
            role="radio"
            aria-checked={active}
            disabled={disabled}
            onClick={() => onChange(v)}
            className={[
              'min-h-[2rem] shrink-0 grow-0 px-2 sm:px-2.5 py-1.5 text-center text-[10px] sm:text-xs font-medium leading-tight rounded-lg transition-[color,background-color,box-shadow] duration-150 whitespace-nowrap',
              'focus:outline-none focus-visible:ring-2 focus-visible:ring-violet-500 focus-visible:ring-offset-1 focus-visible:ring-offset-gray-900',
              active
                ? isShell
                  ? 'bg-amber-950/95 text-amber-100 ring-1 ring-amber-600/60 shadow-sm'
                  : 'bg-violet-600 text-white shadow-sm shadow-violet-950/40'
                : 'text-gray-400 hover:bg-gray-800/85 hover:text-gray-200 border border-transparent',
            ].join(' ')}
          >
            {label}
          </button>
        );
      })}
    </div>
  );
}

function ModeToggleBtn({ value, onChange, disabled, showLabel = false, agentLabel = 'Claude' }) {
  const isShell = value === 'shell';
  return (
    <button
      type="button"
      onClick={() => onChange(isShell ? 'agent' : 'shell')}
      disabled={disabled}
      title={isShell ? '切換到 AI 代理模式' : `${agentLabel}（點擊切換到 Shell 模式）`}
      aria-label={isShell ? 'Shell 模式，點擊切換至 AI 代理' : 'AI 代理模式，點擊切換至 Shell'}
      className={'ra-cmd-badge' + (isShell ? ' shell' : '') + (disabled ? ' opacity-40 pointer-events-none' : '')}
    >
      {isShell ? ('>_ ' + (showLabel ? 'Shell' : '')) : ('✦' + (showLabel ? (' ' + agentLabel) : ''))}
    </button>
  );
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

/**
 * 以 document.execCommand('copy') 複製純文字（相容非安全上下文，不使用 navigator.clipboard）。
 */
function copyTextExecCommand(text) {
  const t = text == null ? '' : String(text);
  if (!t) return false;
  let ta;
  try {
    ta = document.createElement('textarea');
    ta.value = t;
    ta.setAttribute('readonly', '');
    ta.style.position = 'fixed';
    ta.style.left = '-9999px';
    ta.style.top = '0';
    document.body.appendChild(ta);
    // 16px 避免 iOS 聚焦時自動放大；iOS 的 select() 對 textarea 不一定生效，需再補 setSelectionRange。
    ta.style.fontSize = '16px';
    ta.focus();
    ta.select();
    ta.setSelectionRange(0, t.length);
    return document.execCommand('copy');
  } catch (e) {
    return false;
  } finally {
    if (ta && ta.parentNode) ta.parentNode.removeChild(ta);
  }
}

/**
 * 複製純文字：安全環境（HTTPS／Telegram）優先用 Clipboard API，失敗再退回 execCommand。
 * 回傳 Promise<boolean>。
 */
async function copyText(text) {
  const t = text == null ? '' : String(text);
  if (!t) return false;
  if (navigator.clipboard && window.isSecureContext) {
    try {
      await navigator.clipboard.writeText(t);
      return true;
    } catch (_) { /* 權限被拒或失去手勢，改走 fallback */ }
  }
  return copyTextExecCommand(t);
}

/** 輕量 Toast（不需 Provider）：畫面下方浮出一行字，預設約 1.6 秒後淡出。
 *  action={label,onClick} 會多一顆可點的按鈕（如「復原」），此時 duration 通常要拉長。 */
function showToast(message, { error = false, action = null, duration = 1600 } = {}) {
  const el = document.createElement('div');
  el.setAttribute('role', 'status');
  const text = document.createElement('span');
  text.textContent = message;
  el.appendChild(text);
  const dismiss = () => {
    el.style.opacity = '0';
    setTimeout(() => { if (el.parentNode) el.parentNode.removeChild(el); }, 220);
  };
  if (action) {
    const btn = document.createElement('button');
    btn.type = 'button';
    btn.textContent = action.label;
    Object.assign(btn.style, { marginLeft: '12px', fontWeight: '600', color: 'oklch(0.8 0.12 275)', cursor: 'pointer' });
    btn.onclick = () => { dismiss(); action.onClick(); };
    el.appendChild(btn);
  }
  Object.assign(el.style, {
    position: 'fixed',
    left: '50%',
    bottom: 'calc(88px + env(safe-area-inset-bottom))',
    transform: 'translateX(-50%)',
    zIndex: '300',
    maxWidth: '80vw',
    padding: '8px 14px',
    borderRadius: '999px',
    fontSize: '13px',
    lineHeight: '1.3',
    color: error ? 'oklch(0.9 0.08 25)' : 'oklch(0.95 0.01 264)',
    background: error ? 'oklch(0.3 0.1 25 / 0.95)' : 'oklch(0.28 0.02 264 / 0.95)',
    border: '1px solid oklch(0.38 0.02 264)',
    boxShadow: '0 4px 16px rgba(0,0,0,0.4)',
    pointerEvents: action ? 'auto' : 'none',
    opacity: '0',
    transition: 'opacity 0.18s',
  });
  document.body.appendChild(el);
  requestAnimationFrame(() => { el.style.opacity = '1'; });
  setTimeout(dismiss, duration);
}

/** 觸覺回饋：Telegram 內用原生 Haptic，其餘退回 vibrate（iOS Safari 不支援則無動作）。 */
function hapticTap() {
  try {
    const h = window.Telegram?.WebApp?.HapticFeedback;
    if (h && typeof h.impactOccurred === 'function') { h.impactOccurred('medium'); return; }
    if (navigator.vibrate) navigator.vibrate(15);
  } catch (_) { /* 無觸覺裝置，忽略 */ }
}

/** 訊息氣泡用複製按鈕（複製 m.content 原始文字） */
function MessageCopyButton({ text, className }) {
  const [copied, setCopied] = useState(false);
  const empty = !String(text || '').trim();
  const onClick = async (e) => {
    e.stopPropagation();
    if (empty) return;
    if (await copyText(text)) {
      setCopied(true);
      showToast('已複製');
      setTimeout(() => setCopied(false), 2000);
    } else {
      showToast('複製失敗', { error: true });
    }
  };
  return (
    <button
      type="button"
      onClick={onClick}
      disabled={empty}
      title={copied ? '已複製' : '複製'}
      aria-label={copied ? '已複製到剪貼簿' : '複製訊息'}
      className={`${className || ''} inline-flex items-center justify-center min-w-[32px] min-h-[32px]`}
    >
      {copied ? (
        <span className="text-[10px] font-medium leading-none">已複製</span>
      ) : (
        <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="currentColor" className="w-3.5 h-3.5 opacity-70 hover:opacity-100" aria-hidden>
          <path d="M16 1H4c-1.1 0-2 .9-2 2v14h2V3h12V1zm3 4H8c-1.1 0-2 .9-2 2v14c0 1.1.9 2 2 2h11c1.1 0 2-.9 2-2V7c0-1.1-.9-2-2-2zm0 16H8V7h11v14z" />
        </svg>
      )}
    </button>
  );
}

// 接手草稿：MCP 讀原歷史，否則帶入有限歷史；最後需求始終完整。
function buildHandoffDraftMessage(handoff, historyMessages) {
  const h = handoff || {};
  const lastPrompt = String(h.last_prompt || '').trim();
  const reason = String(h.reason || '').trim();
  const head = `原會話（miniapp session_id=${h.old_session_id || ''}，名稱「${h.old_name || ''}」）的 ${h.agent_type || ''} 這次執行失敗（${reason}），需要你接手。\n接手前請先確認 git／檔案現況，避免重做已完成的步驟。`;

  let historyBlock;
  if (h.mcp_enabled) {
    historyBlock = `有 miniapp MCP：請先呼叫 get_messages（session_id="${h.old_session_id || ''}"）讀取舊會話完整歷史，弄清楚進度。`;
  } else {
    const all = (Array.isArray(historyMessages) ? historyMessages : [])
      .filter((m) => m && (m.role === 'user' || m.role === 'claude') && String(m.content || m.resultText || '').trim());
    const countTruncated = all.length > 20;
    const lines = all.slice(-20);
    if (lines.length === 0) {
      historyBlock = 'MCP 未啟用，且目前讀不到舊會話的歷史訊息，請只依下方「最後需求」處理。';
    } else {
      let remaining = 20000;
      let charTruncated = false;
      const kept = [];
      // ponytail: 最近 20 則、文字最多 20,000 字元，優先保留最新尾段；完整歷史由 MCP 讀取。
      for (let i = lines.length - 1; i >= 0; i--) {
        if (remaining === 0) {
          charTruncated = true;
          break;
        }
        const m = lines[i];
        const full = String(m.role === 'claude' ? (m.resultText || m.content || '') : (m.content || '')).trim();
        if (!full) continue;
        charTruncated ||= full.length > remaining;
        const text = full.slice(-remaining);
        remaining -= text.length;
        kept.unshift(`[${m.role === 'user' ? '使用者' : h.agent_type || 'agent'}] ${text}`);
      }
      const noteParts = [];
      if (countTruncated) noteParts.push(`僅最近 20 則，共 ${all.length} 則，有限裁切`);
      if (charTruncated) noteParts.push('已達字數上限，較舊內容已截斷');
      const note = noteParts.length ? `（${noteParts.join('；')}）` : '';
      historyBlock = `MCP 未啟用，附上現有聊天室的有限歷史${note}：\n---\n${kept.join('\n---\n')}\n---`;
    }
  }

  const tail = lastPrompt
    ? `請完成使用者最後這則需求：\n---\n${lastPrompt}`
    : '原會話沒有可接續的最後需求文字。';

  return `${head}\n${historyBlock}\n${tail}`;
}
