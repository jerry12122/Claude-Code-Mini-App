// ── 設定彈窗（左選單 + 右內容，仿 Claude Desktop）───────────────────────────
// 目前有「一般」「外觀」兩個分類：
//   一般：伺服器端本機行為開關（目前僅 vscodeNoAdmin）。
//   外觀：Markdown 顏色（heading/bold/inline code）+ 自訂 CSS。
// 外觀本機 localStorage 當快取；伺服器 SQLite 為準（見 core.js putAppearance / hydrateAppearanceFromServer）。
// 一般設定不做本機快取，見 GeneralSection。

const SETTINGS_SECTIONS = [
  { id: 'general', label: '一般' },
  { id: 'appearance', label: '外觀' },
];

/** 把任意合法 CSS 顏色轉成 <input type="color"> 要的 #rrggbb；解不出就回 fallback。 */
function cssColorToHex(css, fallback = '#808080') {
  const s = String(css || '').trim();
  if (/^#[0-9a-fA-F]{6}$/.test(s)) return s.toLowerCase();
  if (/^#[0-9a-fA-F]{8}$/.test(s)) return s.slice(0, 7).toLowerCase();
  if (/^#[0-9a-fA-F]{3}$/.test(s)) {
    return '#' + [...s.slice(1)].map((c) => c + c).join('').toLowerCase();
  }
  try {
    const ctx = document.createElement('canvas').getContext('2d');
    ctx.fillStyle = '#010203';
    ctx.fillStyle = s;
    const out = String(ctx.fillStyle);
    if (out === '#010203' && s.toLowerCase() !== '#010203') return fallback;
    if (/^#[0-9a-fA-F]{6}$/.test(out)) return out.toLowerCase();
    if (/^#[0-9a-fA-F]{3}$/.test(out)) {
      return '#' + [...out.slice(1)].map((c) => c + c).join('').toLowerCase();
    }
    const m = out.match(/rgba?\(\s*([\d.]+)[,\s]+([\d.]+)[,\s]+([\d.]+)/i);
    if (m) {
      const hex = (n) => Math.max(0, Math.min(255, Math.round(Number(n)))).toString(16).padStart(2, '0');
      return '#' + hex(m[1]) + hex(m[2]) + hex(m[3]);
    }
  } catch (_) {}
  return fallback;
}

/** 顏色輸入列：點色塊開系統調色盤，文字欄仍可貼 oklch / #hex。 */
function ColorField({ label, value, onChange, placeholder }) {
  const hex = cssColorToHex(value || placeholder);
  return (
    <div className="flex items-center gap-3">
      <div className="w-24 shrink-0 text-[11px] text-[oklch(0.6_0.01_264)]">{label}</div>
      <div className="flex-1 min-w-0 flex items-center gap-2">
        <label className="relative shrink-0 w-7 h-7 cursor-pointer" title="開啟調色盤">
          <span
            className="absolute inset-0 rounded-full border border-[oklch(0.32_0.02_264)]"
            style={{ background: value || placeholder }}
            aria-hidden
          />
          <input
            type="color"
            value={hex}
            onChange={(e) => onChange(e.target.value)}
            className="absolute inset-0 w-full h-full opacity-0 cursor-pointer"
            aria-label={`${label} 調色盤`}
          />
        </label>
        <input
          type="text"
          value={value}
          onChange={(e) => onChange(e.target.value)}
          placeholder={placeholder}
          spellCheck={false}
          className="flex-1 min-w-0 bg-[oklch(0.16_0.02_264)] border border-[oklch(0.28_0.02_264)] rounded-lg px-2.5 py-1.5 text-xs font-mono text-[oklch(0.9_0.01_264)] placeholder-[oklch(0.45_0.01_264)] focus:outline-none focus:border-violet-600"
        />
      </div>
    </div>
  );
}

/** 「一般」設定頁：目前僅 vscodeNoAdmin 一個開關，即時 PUT（不走外層儲存流程，跟 appearance 分開存）。 */
function GeneralSection() {
  const [general, setGeneral] = useState(() => ({ ...GENERAL_DEFAULTS }));
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState('');

  useEffect(() => {
    let alive = true;
    getGeneralSettings().then((g) => {
      if (alive) {
        setGeneral(g);
        setLoading(false);
      }
    });
    return () => { alive = false; };
  }, []);

  const toggle = async (checked) => {
    setError('');
    setSaving(true);
    const next = { ...general, vscodeNoAdmin: checked };
    setGeneral(next);
    try {
      const saved = await putGeneralSettings(next);
      setGeneral(saved);
    } catch (e) {
      setGeneral(general);
      setError((e && e.message) || '伺服器未存到');
    } finally {
      setSaving(false);
    }
  };

  return (
    <div className="space-y-5">
      <div>
        <div className="text-sm font-semibold text-[oklch(0.92_0.01_264)] mb-1">開啟 VSCode</div>
        <label className="flex items-start gap-3 cursor-pointer">
          <input
            type="checkbox"
            checked={general.vscodeNoAdmin}
            disabled={loading || saving}
            onChange={(e) => toggle(e.target.checked)}
            className="mt-0.5 w-4 h-4 accent-violet-600 shrink-0"
          />
          <div className="min-w-0">
            <div className="text-sm text-[oklch(0.9_0.01_264)]">以一般使用者權限開啟 VSCode</div>
            <div className="text-[11px] text-[oklch(0.55_0.01_264)] mt-0.5">
              當伺服器程式本身以系統管理員身分執行時，直接開啟 VSCode 會因權限不一致出現
              「Another instance of Code is already running as administrator」錯誤。開啟此選項後，
              伺服器改用 <code className="ra-mono">runas /trustlevel</code> 以一般使用者權限啟動 VSCode（僅 Windows 有效）。
            </div>
          </div>
        </label>
        {error ? <div className="text-xs text-red-400 mt-2">{error}</div> : null}
      </div>
    </div>
  );
}

function AppearanceSection({ draft, setDraft }) {
  const set = (key) => (val) => setDraft((prev) => ({ ...prev, [key]: val }));
  return (
    <div className="space-y-5">
      <div>
        <div className="text-sm font-semibold text-[oklch(0.92_0.01_264)] mb-1">Markdown 顏色</div>
        <div className="text-[11px] text-[oklch(0.55_0.01_264)] mb-3">
          點色塊開調色盤，或直接貼 <code className="ra-mono">oklch(...)</code>、<code className="ra-mono">#hex</code>。
        </div>
        <div className="flex flex-col gap-3">
          <ColorField label="標題（H1–H3）" value={draft.headingColor} onChange={set('headingColor')} placeholder={APPEARANCE_DEFAULTS.headingColor} />
          <ColorField label="粗體" value={draft.boldColor} onChange={set('boldColor')} placeholder={APPEARANCE_DEFAULTS.boldColor} />
          <ColorField label="行內程式碼" value={draft.codeColor} onChange={set('codeColor')} placeholder={APPEARANCE_DEFAULTS.codeColor} />
        </div>
      </div>

      <div>
        <div className="text-sm font-semibold text-[oklch(0.92_0.01_264)] mb-1">自訂 CSS</div>
        <div className="text-[11px] text-[oklch(0.55_0.01_264)] mb-2">
          即時套用到整個頁面；留空即不生效。範例：<code className="ra-mono">.prose blockquote {'{'} color: hotpink; {'}'}</code>
        </div>
        <textarea
          value={draft.customCss}
          onChange={(e) => setDraft((prev) => ({ ...prev, customCss: e.target.value }))}
          placeholder=".prose h1 { text-decoration: underline; }"
          rows={8}
          spellCheck={false}
          className="w-full bg-[oklch(0.16_0.02_264)] border border-[oklch(0.28_0.02_264)] rounded-lg px-3 py-2 text-xs font-mono leading-relaxed text-[oklch(0.9_0.01_264)] placeholder-[oklch(0.45_0.01_264)] focus:outline-none focus:border-violet-600 resize-y"
        />
      </div>
    </div>
  );
}

function SettingsModal({ open, onClose }) {
  const [section, setSection] = useState('general');
  const [draft, setDraft] = useState(() => readStoredAppearance());
  const [savedFlash, setSavedFlash] = useState(false);
  const [saveError, setSaveError] = useState('');
  const [saving, setSaving] = useState(false);

  // 開啟時重新從 localStorage 載入，避免上次關閉未存的草稿殘留。
  useEffect(() => {
    if (open) {
      setDraft(readStoredAppearance());
      setSaveError('');
      setSavedFlash(false);
    }
  }, [open]);

  useEffect(() => {
    if (!open) return;
    const prev = document.body.style.overflow;
    document.body.style.overflow = 'hidden';
    const h = (e) => { if (e.key === 'Escape') onClose(); };
    window.addEventListener('keydown', h);
    return () => {
      document.body.style.overflow = prev;
      window.removeEventListener('keydown', h);
    };
  }, [open, onClose]);

  if (!open) return null;

  const handleSave = async () => {
    setSaveError('');
    saveStoredAppearance(draft);
    applyAppearance(draft);
    setSaving(true);
    try {
      const saved = await putAppearance(draft);
      saveStoredAppearance(saved);
      setDraft(saved);
      setSavedFlash(true);
      setTimeout(() => setSavedFlash(false), 1500);
    } catch (e) {
      setSaveError((e && e.message) || '伺服器未存到');
    } finally {
      setSaving(false);
    }
  };

  const handleReset = () => {
    setDraft({ ...APPEARANCE_DEFAULTS });
    setSaveError('');
  };

  return (
    <div
      className="fixed inset-0 z-[100] flex items-center justify-center p-4 bg-black/60 backdrop-blur-[2px]"
      onClick={onClose}
      role="presentation"
    >
      <div
        className="w-full max-w-2xl h-[min(85vh,34rem)] flex overflow-hidden rounded-2xl border border-[oklch(0.28_0.02_264)] bg-[oklch(0.15_0.02_264)] shadow-2xl shadow-black/50"
        onClick={(e) => e.stopPropagation()}
        role="dialog"
        aria-modal="true"
        aria-labelledby="settings-modal-title"
      >
        {/* 左側選單 */}
        <div className="w-40 shrink-0 border-r border-[oklch(0.26_0.02_264)] bg-[oklch(0.13_0.02_264)] flex flex-col">
          <div id="settings-modal-title" className="px-4 pt-4 pb-3 text-sm font-semibold text-[oklch(0.92_0.01_264)]">
            設定
          </div>
          <div className="flex-1 px-2 space-y-0.5">
            {SETTINGS_SECTIONS.map((s) => (
              <button
                key={s.id}
                type="button"
                onClick={() => setSection(s.id)}
                className={`w-full text-left rounded-lg px-2.5 py-2 text-sm transition-colors ${
                  section === s.id
                    ? 'bg-violet-500/15 text-violet-200'
                    : 'text-[oklch(0.65_0.01_264)] hover:bg-[oklch(0.19_0.02_264)] hover:text-[oklch(0.85_0.01_264)]'
                }`}
              >
                {s.label}
              </button>
            ))}
          </div>
        </div>

        {/* 右側內容 */}
        <div className="flex-1 min-w-0 flex flex-col">
          <div className="flex-1 overflow-y-auto app-scroll px-5 py-5">
            {section === 'general' && <GeneralSection />}
            {section === 'appearance' && <AppearanceSection draft={draft} setDraft={setDraft} />}
          </div>
          <div className="shrink-0 flex items-center justify-between gap-2 px-5 py-3 border-t border-[oklch(0.26_0.02_264)]">
            <button
              type="button"
              onClick={handleReset}
              className="text-xs text-[oklch(0.55_0.01_264)] hover:text-[oklch(0.8_0.01_264)] transition-colors"
            >
              還原預設
            </button>
            <div className="flex items-center gap-2">
              {saveError && <span className="text-xs text-red-400">{saveError}</span>}
              {!saveError && savedFlash && <span className="text-xs text-emerald-400">已儲存</span>}
              <button
                type="button"
                onClick={onClose}
                className="px-3 py-1.5 bg-[oklch(0.22_0.02_264)] hover:bg-[oklch(0.26_0.02_264)] text-[oklch(0.85_0.01_264)] rounded-lg text-xs font-medium transition-colors"
              >
                關閉
              </button>
              <button
                type="button"
                onClick={handleSave}
                disabled={saving}
                className="px-3 py-1.5 bg-violet-600 hover:bg-violet-500 disabled:opacity-60 text-white rounded-lg text-xs font-medium transition-colors"
              >
                {saving ? '儲存中…' : '儲存'}
              </button>
            </div>
          </div>
        </div>
      </div>
    </div>
  );
}
