// ── 設定彈窗（左選單 + 右內容，仿 Claude Desktop）───────────────────────────
// 目前有「一般」「外觀」兩個分類：
//   一般：伺服器端本機行為開關（目前僅 vscodeNoAdmin）。
//   外觀：Markdown 顏色（heading/bold/inline code）+ 自訂 CSS。
// 外觀本機 localStorage 當快取；伺服器 SQLite 為準（見 core.js putAppearance / hydrateAppearanceFromServer）。
// 一般設定不做本機快取，見 GeneralSection。

const SETTINGS_SECTIONS = [
  { id: 'general', label: '一般' },
  { id: 'appearance', label: '外觀' },
  { id: 'shares', label: '分享' },
  { id: 'logs', label: '日誌' },
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
      <div className="w-24 shrink-0 text-[11px] text-ra-t4">{label}</div>
      <div className="flex-1 min-w-0 flex items-center gap-2">
        <label className="relative shrink-0 w-7 h-7 cursor-pointer" title="開啟調色盤">
          <span
            className="absolute inset-0 rounded-full border border-ra-line-strong"
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
          className="flex-1 min-w-0 bg-ra-card border border-ra-line rounded-lg px-2.5 py-1.5 text-xs font-mono text-ra-t1 placeholder-ra-t5 focus:outline-none focus:border-violet-600"
        />
      </div>
    </div>
  );
}

/** 「一般」設定頁：vscodeNoAdmin 開關即時 PUT（不走外層儲存流程，跟 appearance 分開存）；另有純前端的「清除快取並重新載入」。 */
function GeneralSection() {
  const [general, setGeneral] = useState(() => ({ ...GENERAL_DEFAULTS }));
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState('');
  const [refreshing, setRefreshing] = useState(false);
  const [refreshingModels, setRefreshingModels] = useState(false);

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

  // 瀏覽器沒有清 HTTP 快取的 API；cache:'reload' 會繞過並覆寫該 URL 的快取，之後 reload 就拿到新檔。
  const reloadFresh = async () => {
    setRefreshing(true);
    const urls = [location.href, ...[...document.querySelectorAll('script[src],link[rel=stylesheet][href]')]
      .map((el) => el.src || el.href)]
      .filter((u) => new URL(u, location.href).origin === location.origin);
    await Promise.all(urls.map((u) => fetch(u, { cache: 'reload' }).catch(() => {})));
    location.reload();
  };

  // 伺服器會等各 CLI 都抓完才回（數秒）；failed 是抓取失敗的 agent_type（kiro 與 kiroacp 共用同一次抓取，會一起列）。
  const refreshModels = async () => {
    setRefreshingModels(true);
    try {
      const res = await apiFetch('/model-options/refresh', { method: 'POST' });
      if (!res.ok) throw new Error(String(res.status));
      const { failed = [] } = await res.json();
      if (failed.length) {
        showToast(`已更新，但 ${[...new Set(failed)].join('、')} 抓取失敗（見日誌）`, { error: true, duration: 3500 });
      } else {
        showToast('模型清單已更新，重新開啟對話即可看到');
      }
    } catch (_) {
      showToast('更新模型清單失敗', { error: true });
    } finally {
      setRefreshingModels(false);
    }
  };

  return (
    <div className="divide-y divide-ra-line-soft [&>*]:py-6 [&>:first-child]:pt-0 [&>:last-child]:pb-0">
      <div>
        <div className="text-sm font-semibold text-ra-t1 mb-1">開啟 VSCode</div>
        <label className="flex items-start gap-3 cursor-pointer">
          <input
            type="checkbox"
            checked={general.vscodeNoAdmin}
            disabled={loading || saving}
            onChange={(e) => toggle(e.target.checked)}
            className="mt-0.5 w-4 h-4 accent-violet-600 shrink-0"
          />
          <div className="min-w-0">
            <div className="text-sm text-ra-t1">以一般使用者權限開啟 VSCode</div>
            <div className="text-[11px] text-ra-t5 mt-0.5">
              當伺服器程式本身以系統管理員身分執行時，直接開啟 VSCode 會因權限不一致出現
              「Another instance of Code is already running as administrator」錯誤。開啟此選項後，
              伺服器改透過 <code className="ra-mono">explorer.exe</code> 分派 <code className="ra-mono">vscode://</code> 連結以一般使用者權限啟動 VSCode（僅 Windows 有效）。
            </div>
          </div>
        </label>
        {error ? <div className="text-xs text-red-400 mt-2">{error}</div> : null}
      </div>
      <div>
        <div className="text-sm font-semibold text-ra-t1 mb-1">模型清單</div>
        <div className="text-[11px] text-ra-t5 mb-2">
          程式啟動時會向各 CLI（Claude、Cursor、Codex、Kiro）取得可用模型。有新模型時按這裡即可更新，不必重啟。
        </div>
        <button
          type="button"
          onClick={refreshModels}
          disabled={refreshingModels}
          className="px-3 py-1.5 rounded-lg text-xs font-medium bg-ra-raised hover:bg-ra-raised text-ra-t1 border border-ra-line-strong disabled:opacity-40"
        >
          {refreshingModels ? '更新中…（需數秒）' : '重新抓取模型清單'}
        </button>
      </div>
      <div>
        <div className="text-sm font-semibold text-ra-t1 mb-1">前端更新</div>
        <div className="text-[11px] text-ra-t5 mb-2">
          改了前端檔案卻沒生效時使用：強制重抓本站所有腳本與樣式（覆寫瀏覽器快取）後重新載入頁面。
        </div>
        <button
          type="button"
          onClick={reloadFresh}
          disabled={refreshing}
          className="px-3 py-1.5 rounded-lg text-xs font-medium bg-ra-raised hover:bg-ra-raised text-ra-t1 border border-ra-line-strong disabled:opacity-40"
        >
          {refreshing ? '更新中…' : '清除快取並重新載入'}
        </button>
      </div>
    </div>
  );
}

const THEME_FIELD_OPTIONS = [
  { value: 'system', label: '跟隨系統' },
  { value: 'dark', label: '深色' },
  { value: 'light', label: '淺色' },
];

/** 主題偏好：每裝置 localStorage，點選即 setThemePref，不走「儲存」。 */
function ThemeField() {
  const [pref, setPref] = useState(readThemePref);
  const pick = (value) => {
    setPref(value);
    setThemePref(value);
  };
  return (
    <div>
      <div className="text-sm font-semibold text-ra-t1 mb-1">主題</div>
      <div className="text-[11px] text-ra-t5 mb-3">
        只影響這個裝置。跟隨系統會依作業系統的淺色或深色設定自動切換。
      </div>
      <div
        role="radiogroup"
        aria-label="主題"
        className="inline-flex h-9 max-w-full flex-wrap items-stretch gap-0.5 rounded-lg border border-ra-line bg-ra-raised p-[3px]"
      >
        {THEME_FIELD_OPTIONS.map((opt) => {
          const active = pref === opt.value;
          return (
            <button
              key={opt.value}
              type="button"
              role="radio"
              aria-checked={active}
              onClick={() => pick(opt.value)}
              className={[
                'h-full flex items-center justify-center shrink-0 grow-0 px-3 text-xs font-semibold rounded-[7px] transition-[color,background-color] duration-150 whitespace-nowrap',
                'focus:outline-none focus-visible:ring-2 focus-visible:ring-violet-500/50',
                active
                  ? 'bg-[oklch(0.62_0.19_275_/18%)] text-ra-accent-text'
                  : 'text-ra-t5 hover:text-ra-t2',
              ].join(' ')}
            >
              {opt.label}
            </button>
          );
        })}
      </div>
    </div>
  );
}

/** 對話文字大小：拖動即時套用並存本機（這個裝置專用），不走下方的「儲存」。 */
function ChatFontSizeField() {
  const [fs, setFs] = useState(readChatFontSize);
  const change = (v) => setFs(setChatFontSize(Number(v)));
  return (
    <div>
      <div className="flex items-center justify-between mb-1">
        <div className="text-sm font-semibold text-ra-t1">對話文字大小</div>
        <button
          type="button"
          onClick={() => change(CHAT_FS_DEFAULT)}
          disabled={fs === CHAT_FS_DEFAULT}
          className="text-[11px] text-violet-400 hover:text-violet-300 disabled:opacity-40 disabled:hover:text-violet-400"
        >
          重設
        </button>
      </div>
      <div className="text-[11px] text-ra-t5 mb-3">
        即時套用，只影響這個裝置。手機輸入框不會小於 16px。
      </div>
      <div className="flex items-center gap-3">
        <input
          type="range"
          min={CHAT_FS_MIN}
          max={CHAT_FS_MAX}
          step={1}
          value={fs}
          onChange={(e) => change(e.target.value)}
          aria-label="對話文字大小"
          className="flex-1 min-w-0 accent-violet-500"
        />
        <span className="w-12 shrink-0 text-right text-xs ra-mono text-ra-t2">{fs}px</span>
      </div>
      <div className="mt-3 rounded-lg border border-ra-line bg-ra-card px-3 py-2 text-ra-t2" style={{ fontSize: fs + 'px', lineHeight: 1.8 }}>
        預覽：這是一段對話文字 The quick brown fox 0123
      </div>
    </div>
  );
}

function AppearanceSection({ draft, setDraft }) {
  const set = (key) => (val) => setDraft((prev) => ({ ...prev, [key]: val }));
  return (
    <div className="divide-y divide-ra-line-soft [&>*]:py-6 [&>:first-child]:pt-0 [&>:last-child]:pb-0">
      <ThemeField />
      <ChatFontSizeField />

      <div>
        <div className="text-sm font-semibold text-ra-t1 mb-1">Markdown 顏色</div>
        <div className="text-[11px] text-ra-t5 mb-3">
          點色塊開調色盤，或直接貼 <code className="ra-mono">oklch(...)</code>、<code className="ra-mono">#hex</code>。
        </div>
        <div className="flex flex-col gap-3">
          <ColorField label="標題（H1–H3）" value={draft.headingColor} onChange={set('headingColor')} placeholder={APPEARANCE_DEFAULTS.headingColor} />
          <ColorField label="粗體" value={draft.boldColor} onChange={set('boldColor')} placeholder={APPEARANCE_DEFAULTS.boldColor} />
          <ColorField label="行內程式碼" value={draft.codeColor} onChange={set('codeColor')} placeholder={APPEARANCE_DEFAULTS.codeColor} />
        </div>
      </div>

      <div>
        <div className="text-sm font-semibold text-ra-t1 mb-1">自訂 CSS</div>
        <div className="text-[11px] text-ra-t5 mb-2">
          即時套用到整個頁面；留空即不生效。範例：<code className="ra-mono">.prose blockquote {'{'} color: hotpink; {'}'}</code>
          {' '}僅在特定主題生效時可用 <code className="ra-mono">html[data-theme=&quot;light&quot;]</code> 或 <code className="ra-mono">html[data-theme=&quot;dark&quot;]</code> 限定，例如{' '}
          <code className="ra-mono">html[data-theme=&quot;light&quot;] .prose {'{'} color: #333; {'}'}</code>
        </div>
        <textarea
          value={draft.customCss}
          onChange={(e) => setDraft((prev) => ({ ...prev, customCss: e.target.value }))}
          placeholder=".prose h1 { text-decoration: underline; }"
          rows={8}
          spellCheck={false}
          className="w-full bg-ra-card border border-ra-line rounded-lg px-3 py-2 text-xs font-mono leading-relaxed text-ra-t1 placeholder-ra-t5 focus:outline-none focus:border-violet-600 resize-y"
        />
      </div>
    </div>
  );
}

function SettingsModal({ open, onClose, onJumpToSession = null, desktopUpdate = null }) {
  const [section, setSection] = useState('general');
  // 桌面 bridge 存在時才顯示「更新」分類與版號旁的更新入口；瀏覽器、Telegram、訪客皆不顯示。
  const updateBridge = !!(desktopUpdate && desktopUpdate.bridge);
  const updateOpenNonce = desktopUpdate ? desktopUpdate.openNonce : 0;
  const [draft, setDraft] = useState(() => readStoredAppearance());
  const [savedFlash, setSavedFlash] = useState(false);
  const [saveError, setSaveError] = useState('');
  const [saving, setSaving] = useState(false);
  const [buildInfo, setBuildInfo] = useState(null);

  // 托盤「檢查更新」會要求開啟更新區（openNonce 每次遞增）。
  useEffect(() => {
    if (updateOpenNonce > 0) setSection('update');
  }, [updateOpenNonce]);

  // 開啟時重新從 localStorage 載入，避免上次關閉未存的草稿殘留。
  useEffect(() => {
    if (open) {
      setDraft(readStoredAppearance());
      setSaveError('');
      setSavedFlash(false);
      fetchServerConfig().then(setBuildInfo);
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
        className={`w-full max-w-5xl h-[min(85vh,44rem)] flex overflow-hidden rounded-2xl border border-ra-line bg-ra-bg shadow-2xl shadow-black/50`}
        onClick={(e) => e.stopPropagation()}
        role="dialog"
        aria-modal="true"
        aria-labelledby="settings-modal-title"
      >
        {/* 左側選單 */}
        <div className="w-40 shrink-0 border-r border-ra-line-soft bg-ra-sunken flex flex-col">
          <div id="settings-modal-title" className="px-4 pt-4 pb-3 text-sm font-semibold text-ra-t1">
            設定
          </div>
          <div className="flex-1 px-2 space-y-0.5">
            {(updateBridge ? [...SETTINGS_SECTIONS, { id: 'update', label: '更新' }] : SETTINGS_SECTIONS).map((s) => (
              <button
                key={s.id}
                type="button"
                onClick={() => setSection(s.id)}
                className={`w-full text-left rounded-lg px-2.5 py-2 text-sm transition-colors ${
                  section === s.id
                    ? 'bg-violet-500/15 text-violet-200'
                    : 'text-ra-t4 hover:bg-ra-card hover:text-ra-t2'
                }`}
              >
                {s.label}
              </button>
            ))}
          </div>
          <div className="shrink-0 px-4 py-3 text-xs leading-5 font-mono text-ra-t4 select-text">
            <div>v{buildInfo?.version || '—'}</div>
            <div title={buildInfo?.sha || 'unknown'}>SHA {buildInfo?.sha?.slice(0, 7) || '—'}</div>
            {updateBridge ? (
              <button
                type="button"
                onClick={() => setSection('update')}
                className="mt-1 font-sans text-[11px] text-violet-400 hover:text-violet-300 transition-colors"
              >
                前往更新
              </button>
            ) : null}
          </div>
        </div>

        {/* 右側內容 */}
        <div className="flex-1 min-w-0 flex flex-col">
          {/* 日誌頁自己管捲動（清單內捲、頂部工具列固定），所以外層不捲 */}
          <div className={`flex-1 min-h-0 app-scroll px-5 ${section === 'logs' ? 'py-4 overflow-hidden' : 'py-5 overflow-y-auto'}`}>
            {section === 'general' && <GeneralSection />}
            {section === 'appearance' && <AppearanceSection draft={draft} setDraft={setDraft} />}
            {section === 'shares' && <SharesSection onJumpToSession={onJumpToSession} />}
            {section === 'logs' && <LogsSection />}
            {section === 'update' && updateBridge && (
              <DesktopUpdateSection
                status={desktopUpdate.status}
                laterVersion={desktopUpdate.laterVersion}
                request={desktopUpdate.request}
                later={desktopUpdate.later}
                showOffer={desktopUpdate.showOffer}
              />
            )}
          </div>
          <div className="shrink-0 flex items-center justify-between gap-2 px-5 py-3 border-t border-ra-line-soft">
            {/* 還原預設／儲存只對外觀有效，日誌頁不顯示 */}
            {section !== 'logs' && section !== 'shares' && section !== 'update' && (
              <button
                type="button"
                onClick={handleReset}
                className="text-xs text-ra-t5 hover:text-ra-t2 transition-colors"
              >
                還原預設
              </button>
            )}
            <div className="flex items-center gap-2 ml-auto">
              {saveError && <span className="text-xs text-red-400">{saveError}</span>}
              {!saveError && savedFlash && <span className="text-xs text-emerald-400">已儲存</span>}
              <button
                type="button"
                onClick={onClose}
                className="px-3 py-1.5 bg-ra-raised hover:bg-ra-raised text-ra-t2 rounded-lg text-xs font-medium transition-colors"
              >
                關閉
              </button>
              {section !== 'logs' && section !== 'shares' && section !== 'update' && (
                <button
                  type="button"
                  onClick={handleSave}
                  disabled={saving}
                  className="px-3 py-1.5 bg-violet-600 hover:bg-violet-500 disabled:opacity-60 text-white rounded-lg text-xs font-medium transition-colors"
                >
                  {saving ? '儲存中…' : '儲存'}
                </button>
              )}
            </div>
          </div>
        </div>
      </div>
    </div>
  );
}
