// ── 設定彈窗（桌面左右分欄；手機分類清單 → 內容 → 返回）───────────────────
//   一般：伺服器端本機行為與全域指令。
//   外觀：Markdown 顏色（heading/bold/inline code）+ 自訂 CSS。
// 外觀本機 localStorage 當快取；伺服器 SQLite 為準（見 core.js putAppearance / hydrateAppearanceFromServer）。
// 一般設定不做本機快取，見 GeneralSection。

const SETTINGS_SECTIONS = [
  { id: 'general', label: '一般' },
  { id: 'appearance', label: '外觀' },
  { id: 'shares', label: '分享' },
  { id: 'system', label: '系統資源' },
  { id: 'logs', label: '日誌' },
];

// 設定頁控制項比例。只組既有 ra token；窄螢幕 48px／16px，sm 以上 40px。
const settingsLabelCls = 'text-[15px] font-medium leading-6 text-ra-t1';
const settingsHintCls = 'text-[13px] leading-5 text-ra-t3';
const settingsControlCls = 'min-h-12 rounded-lg border border-ra-line bg-ra-bg px-3 text-base text-ra-t1 placeholder:text-ra-t5 focus-visible:outline focus-visible:outline-2 focus-visible:outline-ra-accent-text disabled:opacity-40 sm:min-h-10';
const settingsBtnCls = 'inline-flex min-h-12 items-center justify-center rounded-lg border border-ra-line bg-ra-card px-3 text-sm font-medium text-ra-t1 hover:bg-ra-raised focus-visible:outline focus-visible:outline-2 focus-visible:outline-ra-accent-text disabled:opacity-40 sm:min-h-10';
const settingsPrimaryCls = 'inline-flex min-h-12 items-center justify-center rounded-lg bg-ra-t1 px-3 text-sm font-medium text-ra-bg hover:bg-ra-t2 focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ra-accent-text disabled:opacity-40 sm:min-h-10';
const settingsGhostCls = 'inline-flex min-h-12 items-center justify-center rounded-lg px-3 text-sm text-ra-t3 hover:bg-ra-raised hover:text-ra-t1 focus-visible:outline focus-visible:outline-2 focus-visible:outline-ra-accent-text disabled:opacity-40 sm:min-h-10';
// 分隔線在 margin 與 padding 之間：上組內容、線、下一標題各留 24px，legend 才不會貼線。
const settingsStackCls = 'flex flex-col [&>*+*]:mt-6 [&>*+*]:border-t [&>*+*]:border-ra-line-soft [&>*+*]:pt-6';

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
  const id = React.useId();
  const hex = cssColorToHex(value || placeholder);
  return (
    <div className="flex min-w-0 flex-col gap-2">
      <label htmlFor={id} className="text-sm font-medium leading-5 text-ra-t1">{label}</label>
      <div className="flex min-w-0 items-center gap-2">
        <label className="relative h-12 w-12 shrink-0 cursor-pointer rounded-lg border border-ra-line sm:h-10 sm:w-10" title="開啟調色盤">
          <span
            className="absolute inset-0 rounded-[7px]"
            style={{ background: value || placeholder }}
            aria-hidden
          />
          <input
            type="color"
            value={hex}
            onChange={(e) => onChange(e.target.value)}
            className="absolute inset-0 h-full w-full cursor-pointer opacity-0"
            aria-label={`${label} 調色盤`}
          />
        </label>
        <input
          id={id}
          type="text"
          value={value}
          onChange={(e) => onChange(e.target.value)}
          placeholder={placeholder}
          spellCheck={false}
          autoCorrect="off"
          autoCapitalize="none"
          className={`${settingsControlCls} min-w-0 flex-1 font-mono`}
        />
      </div>
    </div>
  );
}

function GlobalInstructionSettings() {
  const id = React.useId();
  const [instruction, setInstruction] = useState(null);
  const [saving, setSaving] = useState(false);
  const [status, setStatus] = useState('');

  useEffect(() => {
    const controller = new AbortController();
    apiFetch('/settings/global-instruction', { signal: controller.signal })
      .then((res) => {
        if (!res.ok) throw new Error();
        return res.json();
      })
      .then(setInstruction)
      .catch(() => {
        if (!controller.signal.aborted) setStatus('無法讀取全域指令設定');
      });
    return () => controller.abort();
  }, []);

  const update = (event) => {
    const { name, value } = event.target;
    setInstruction((current) => ({ ...current, [name]: value }));
    setStatus('');
  };

  const save = async (event) => {
    event.preventDefault();
    setSaving(true);
    setStatus('');
    try {
      const res = await apiFetch('/settings/global-instruction', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(instruction),
      });
      const result = await res.json();
      if (!res.ok) throw new Error(result.error || '無法儲存全域指令設定');
      setInstruction(result);
      setStatus('已儲存全域指令');
    } catch (err) {
      setStatus(err.message || '無法儲存全域指令設定');
    } finally {
      setSaving(false);
    }
  };

  return (
    <form onSubmit={save} aria-labelledby={`${id}-title`} className="flex flex-col gap-6">
      <div className="flex flex-col gap-2">
        <label id={`${id}-title`} htmlFor={`${id}-text`} className={settingsLabelCls}>全域指令</label>
        <p id={`${id}-help`} className={settingsHintCls}>套用到之後建立的 Agent 會話。最多 16,384 UTF-8 bytes。</p>
        <textarea
          id={`${id}-text`}
          name="text"
          aria-describedby={`${id}-help`}
          value={instruction?.text ?? ''}
          onChange={update}
          rows={4}
          disabled={!instruction || saving}
          className={`${settingsControlCls} w-full resize-y`}
        />
      </div>
      <fieldset disabled={!instruction || saving} className="flex min-w-0 flex-col gap-1 border-0 p-0">
        <legend className={settingsLabelCls}>注入模式</legend>
        {[
          ['first', '會話第一則訊息'],
          ['every', '每則訊息'],
          ['native', '僅原生支援（Claude、Codex、Antigravity）'],
        ].map(([value, label]) => (
          <div key={value} className="flex min-h-12 items-center gap-3 text-sm text-ra-t1 sm:min-h-10">
            <input type="radio" id={`${id}-${value}`} name="mode" value={value}
              checked={(instruction?.mode ?? 'native') === value} onChange={update} className="h-4 w-4" />
            <label htmlFor={`${id}-${value}`}>{label}</label>
          </div>
        ))}
      </fieldset>
      <div className="flex flex-wrap items-center gap-3">
        <button type="submit" disabled={!instruction || saving} className={settingsPrimaryCls}>
          {saving ? '儲存中…' : '儲存全域指令'}
        </button>
        <span role="status" className={settingsHintCls}>{status}</span>
      </div>
    </form>
  );
}

// 「一般」頁的核取列：標題＋勾選框＋說明。children 接在勾選列之後（例如守門的門檻輸入）。
function GeneralCheckbox({ title, label, hint, hintId, id, checked, disabled, onChange, children }) {
  return (
    <div className="flex flex-col gap-2">
      <div className={settingsLabelCls}>{title}</div>
      <label htmlFor={id} className="flex min-h-12 cursor-pointer items-start gap-3 sm:min-h-10">
        <input
          id={id}
          type="checkbox"
          checked={checked}
          disabled={disabled}
          onChange={onChange}
          className="mt-0.5 h-5 w-5 shrink-0"
        />
        <span className="min-w-0">
          <span className="block text-sm leading-5 text-ra-t1">{label}</span>
          {hint ? <span id={hintId} className={`mt-1 block ${settingsHintCls}`}>{hint}</span> : null}
        </span>
      </label>
      {children}
    </div>
  );
}

/** 「一般」設定頁：開關即時 PUT，與外觀設定分開存。 */
/** 「一般」設定頁：開關即時 PUT，與外觀設定分開存。 */
function formatQuotaPercent(value) {
  const n = Number(value);
  return Number.isFinite(n) ? String(n) : '';
}

// input 只改草稿。enter 不送出，只要求離開欄位；blur 才決定要不要 PUT。未完成的小數（結尾是 .）不送。
function quotaGuardPercentEvent(type, draft, accepted) {
  const text = String(draft);
  if (type === 'input') return { put: false, blur: false, draft: text };
  if (type === 'enter') return { put: false, blur: true, draft: text };
  const trimmed = text.trim();
  const acceptedNumber = Number(accepted);
  const acceptedText = Number.isFinite(acceptedNumber) ? String(acceptedNumber) : '';
  const unfinished = trimmed === '' || trimmed === '.' || trimmed === '-' || trimmed === '-.' || trimmed.endsWith('.');
  if (unfinished) return { put: false, blur: false, draft: acceptedText, value: acceptedNumber };
  const n = Number(trimmed);
  if (!Number.isFinite(n)) return { put: false, blur: false, draft: acceptedText, value: acceptedNumber };
  if (n === acceptedNumber) return { put: false, blur: false, draft: String(n), value: n };
  return { put: true, blur: false, draft: String(n), value: n };
}

async function settleGeneralSave(previous, next, put) {
  try {
    return { general: await put(next), error: '' };
  } catch (e) {
    return { general: previous, error: (e && e.message) || '伺服器未存到' };
  }
}

// 只改 patch 裡的欄位，其餘原樣保留。ask 由關變開要先確認；取消則不 PUT。關閉不確認。
function generalPatch(general, patch, confirm) {
  const turningAskOn = patch.claudeAutoAllowAsk === true && !general.claudeAutoAllowAsk;
  if (turningAskOn && !confirm()) return { put: false, next: general };
  return { put: true, next: { ...general, ...patch } };
}

function GeneralSection() {
  const [general, setGeneral] = useState(() => ({ ...GENERAL_DEFAULTS }));
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState('');
  const [refreshing, setRefreshing] = useState(false);
  const [refreshingModels, setRefreshingModels] = useState(false);
  const [percentDraft, setPercentDraft] = useState('');

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

  useEffect(() => {
    setPercentDraft(formatQuotaPercent(general.quotaGuardPercent));
  }, [general.quotaGuardPercent]);

  const saveGeneral = async (next) => {
    const previous = general;
    setError('');
    setSaving(true);
    setGeneral(next);
    const settled = await settleGeneralSave(previous, next, putGeneralSettings);
    setGeneral(settled.general);
    setError(settled.error);
    setSaving(false);
  };

  const commitPercent = () => {
    const decision = quotaGuardPercentEvent('blur', percentDraft, general.quotaGuardPercent);
    setPercentDraft(decision.draft);
    if (!decision.put || loading || saving) return;
    return saveGeneral({ ...general, quotaGuardPercent: decision.value });
  };

  const toggle = (checked) => {
    const decision = generalPatch(general, { vscodeNoAdmin: checked }, () => true);
    if (!decision.put) return;
    return saveGeneral(decision.next);
  };

  // 由關變開要先確認：這是唯一會讓 ask 規則直接放行的設定。關閉不確認。
  const toggleAutoAllow = (checked) => {
    const decision = generalPatch(
      general,
      { claudeAutoAllowAsk: checked },
      () => window.confirm('ask 規則原本是要先問你的指令（例如刪除檔案、對外連線）。開啟後，權限模式為「跳過授權」的 Claude 會話會直接放行這些詢問，不再跳出面板。確定要開啟？'),
    );
    if (!decision.put) return;
    return saveGeneral(decision.next);
  };

  // 瀏覽器沒有清 HTTP 快取的 API；cache:'reload' 會繞過並覆寫該 URL 的快取，之後 reload 就拿到新檔。
  const reloadFresh = async () => {
    setRefreshing(true);
    const urls = [location.href, ...[...document.querySelectorAll('script[src],link[rel=stylesheet][href]')]
      .map((el) => el.src || el.href)]
      .filter((u) => new URL(u, location.href).origin === location.origin);
    await Promise.all(urls.map((u) => fetchWithTimeout(u, { cache: 'reload' }).catch(() => {})));
    location.reload();
  };

  // 伺服器會等各 CLI 都抓完才回（數秒）；failed 是抓取失敗的 agent_type（kiro 與 kiroacp 共用同一次抓取，會一起列）。
  const refreshModels = async () => {
    setRefreshingModels(true);
    try {
      const res = await apiFetch('/model-options/refresh', { method: 'POST', timeoutMs: 120000 });
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
  const [deliveryDraft, setDeliveryDraft] = useState(general.queueDelivery === 'batch' ? 'batch' : 'each');
  useEffect(() => { setDeliveryDraft(general.queueDelivery === 'batch' ? 'batch' : 'each'); }, [general.queueDelivery]);
  const saveDelivery = (event) => {
    event.preventDefault();
    saveGeneral({ ...general, queueDelivery: deliveryDraft });
  };

  return (
    <div className={settingsStackCls}>
      <form onSubmit={saveDelivery}>
        <fieldset className="min-w-0" style={{ border: 0, padding: 0 }}>
          <legend className={settingsLabelCls}>佇列送出方式</legend>
          <p className={settingsHintCls}>聊天室與 Room 共用。只在回到閒置、即將取出下一輪時生效，不中斷執行中的回合，也不拆開已快照的批次。</p>
          <label htmlFor="queue-delivery-each" className="flex items-center gap-2 py-1 text-[15px]" style={{ minHeight: 44, minWidth: 44 }}><input id="queue-delivery-each" type="radio" name="queueDelivery" value="each" checked={deliveryDraft === 'each'} disabled={loading || saving} onChange={() => setDeliveryDraft('each')} />逐筆送出</label>
          <label htmlFor="queue-delivery-batch" className="flex items-center gap-2 py-1 text-[15px]" style={{ minHeight: 44, minWidth: 44 }}><input id="queue-delivery-batch" type="radio" name="queueDelivery" value="batch" checked={deliveryDraft === 'batch'} disabled={loading || saving} onChange={() => setDeliveryDraft('batch')} />一起送</label>
        </fieldset>
        <button type="submit" disabled={loading || saving} className="rounded border border-ra-line-strong px-3 py-2 text-[15px]" style={{ marginTop: 8, minHeight: 44, minWidth: 44 }}>儲存佇列送出方式</button>
      </form>
      <GlobalInstructionSettings />
      <GeneralCheckbox
        title="開啟 VSCode"
        id="vscode-no-admin"
        label="以一般使用者權限開啟 VSCode"
        hint={<>
          當伺服器程式本身以系統管理員身分執行時，直接開啟 VSCode 會因權限不一致出現
          「Another instance of Code is already running as administrator」錯誤。開啟此選項後，
          伺服器改透過 <code className="ra-mono">explorer.exe</code> 分派 <code className="ra-mono">vscode://</code> 連結以一般使用者權限啟動 VSCode（僅 Windows 有效）。
        </>}
        checked={general.vscodeNoAdmin}
        disabled={loading || saving}
        onChange={(e) => toggle(e.target.checked)}
      />
      <GeneralCheckbox
        title="Claude 完全跳過授權"
        id="claude-auto-allow-ask"
        label="自動允許 ask 規則詢問"
        hint={<>
          僅作用於權限模式為「跳過授權」的 Claude 會話。開啟後，命中 ask 規則的工具呼叫會由伺服器直接允許，
          並在該輪回覆留一行紀錄。deny 規則與 Claude 的提問不受影響。預設關閉。
        </>}
        checked={!!general.claudeAutoAllowAsk}
        disabled={loading || saving}
        onChange={(e) => {
              const want = e.target.checked;
              // 受控元件：取消確認時把瀏覽器先勾上的狀態彈回，且不送 PUT。
              if (want && !general.claudeAutoAllowAsk) e.target.checked = false;
              toggleAutoAllow(want);
            }}
      />
      <GeneralCheckbox
        title="額度用盡後自動繼續"
        label="額度重置後自動繼續"
        hint={<>
          5 小時額度重置後，伺服器會在無人看管時自動繼續執行。會話多半以跳過授權（bypassPermissions）運行，
          開啟等於沒有人在場時仍會改檔與執行指令。預設關閉。
        </>}
        hintId="auto-resume-hint"
        id="auto-resume-on-quota"
        checked={!!general.autoResumeOnQuota}
        disabled={loading || saving}
        onChange={(e) => {
              const decision = generalPatch(general, { autoResumeOnQuota: e.target.checked }, () => true);
              if (decision.put) saveGeneral(decision.next);
            }}
      />
      <GeneralCheckbox
        title="額度守門"
        label="自動啟動佇列前檢查額度"
        id="quota-guard-enabled"
        checked={!!general.quotaGuardEnabled}
        disabled={loading || saving}
        onChange={(e) => {
              const decision = generalPatch(general, { quotaGuardEnabled: e.target.checked }, () => true);
              if (decision.put) saveGeneral(decision.next);
            }}
      >
        <p id="quota-guard-hint" className={settingsHintCls}>
          任一有百分比的額度視窗已用達門檻時暫停佇列。直接送出與按「繼續」不受影響。守門關閉時不能改數字。
          離開欄位或按 Enter 才儲存。
        </p>
        <label htmlFor="quota-guard-percent" className="mt-1 block text-sm text-ra-t1">門檻百分比</label>
        <input
          id="quota-guard-percent"
          name="quotaGuardPercent"
          type="number"
          inputMode="decimal"
          step="any"
          value={percentDraft}
          disabled={loading || saving || !general.quotaGuardEnabled}
          aria-describedby={error ? 'quota-guard-hint quota-guard-error' : 'quota-guard-hint'}
          aria-invalid={error ? true : undefined}
          onChange={(e) => setPercentDraft(quotaGuardPercentEvent('input', e.target.value, general.quotaGuardPercent).draft)}
          onBlur={commitPercent}
          onKeyDown={(e) => {
            if (e.key !== 'Enter' || e.repeat || e.isComposing) return;
            const decision = quotaGuardPercentEvent('enter', percentDraft, general.quotaGuardPercent);
            if (!decision.blur) return;
            e.preventDefault();
            e.currentTarget.blur();
          }}
          className={`${settingsControlCls} mt-2 w-28`}
        />
        {error ? <div id="quota-guard-error" role="alert" className="text-[13px] leading-5 text-red-400">{error}</div> : null}
      </GeneralCheckbox>
      <div className="flex flex-col gap-2">
        <div className={settingsLabelCls}>模型清單</div>
        <p className={settingsHintCls}>
          程式啟動時會向各 CLI（Claude、Cursor、Codex、Kiro）取得可用模型。有新模型時按這裡即可更新，不必重啟。
        </p>
        <button
          type="button"
          onClick={refreshModels}
          disabled={refreshingModels}
          className={`${settingsBtnCls} mt-1 w-fit`}
        >
          {refreshingModels ? '更新中…（需數秒）' : '重新抓取模型清單'}
        </button>
      </div>
      <div className="flex flex-col gap-2">
        <div className={settingsLabelCls}>前端更新</div>
        <p className={settingsHintCls}>
          改了前端檔案卻沒生效時使用：強制重抓本站所有腳本與樣式（覆寫瀏覽器快取）後重新載入頁面。
        </p>
        <button
          type="button"
          onClick={reloadFresh}
          disabled={refreshing}
          className={`${settingsBtnCls} mt-1 w-fit`}
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
    <div className="flex flex-col gap-2">
      <div className={settingsLabelCls}>主題</div>
      <p className={settingsHintCls}>
        只影響這個裝置。跟隨系統會依作業系統的淺色或深色設定自動切換。點選立即套用，不經下方的儲存。
      </p>
      <div
        role="radiogroup"
        aria-label="主題"
        className="inline-flex h-auto max-w-full flex-wrap items-stretch gap-1 rounded-lg border border-ra-line bg-ra-sunken p-1"
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
                'flex min-h-12 flex-1 items-center justify-center whitespace-nowrap rounded-md px-3 text-sm font-medium sm:min-h-10 sm:flex-none',
                'focus-visible:outline focus-visible:outline-2 focus-visible:outline-ra-accent-text',
                active
                  ? 'bg-ra-card text-ra-accent-text'
                  : 'text-ra-t3 hover:text-ra-t1',
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
    <div className="flex flex-col gap-2">
      <div className="flex items-center justify-between gap-3">
        <label htmlFor="settings-chat-font" className={settingsLabelCls}>對話文字大小</label>
        <button
          type="button"
          onClick={() => change(CHAT_FS_DEFAULT)}
          disabled={fs === CHAT_FS_DEFAULT}
          className={settingsGhostCls}
        >
          重設
        </button>
      </div>
      <p id="settings-chat-font-help" className={settingsHintCls}>
        拖動立即套用，只影響這個裝置，不經下方的儲存。手機輸入框不會小於 16px。
      </p>
      <div className="flex items-center gap-3">
        <input
          id="settings-chat-font"
          type="range"
          min={CHAT_FS_MIN}
          max={CHAT_FS_MAX}
          step={1}
          value={fs}
          onChange={(e) => change(e.target.value)}
          aria-describedby="settings-chat-font-help"
          className="h-12 min-w-0 flex-1 sm:h-10"
        />
        <span className="w-12 shrink-0 text-right text-sm ra-mono text-ra-t2">{fs}px</span>
      </div>
      <div className="rounded-lg border border-ra-line bg-ra-sunken px-3 py-3 text-ra-t2" style={{ fontSize: fs + 'px', lineHeight: 1.8 }}>
        預覽：這是一段對話文字 The quick brown fox 0123
      </div>
    </div>
  );
}

function AppearanceSection({ draft, setDraft }) {
  const set = (key) => (val) => setDraft((prev) => ({ ...prev, [key]: val }));
  return (
    <div className={settingsStackCls}>
      <ThemeField />
      <ChatFontSizeField />

      <div className="min-w-0">
      <fieldset className="flex min-w-0 flex-col border-0 p-0">
        <legend className={settingsLabelCls}>Markdown 顏色</legend>
        <p className={`${settingsHintCls} mt-1 [&_code]:break-all`}>
          點色塊開調色盤，或直接貼 <code className="ra-mono">oklch(...)</code>、<code className="ra-mono">#hex</code>。改完按下方儲存。
        </p>
        <div className="mt-4 flex flex-col gap-4">
          <ColorField label="標題（H1–H3）" value={draft.headingColor} onChange={set('headingColor')} placeholder={APPEARANCE_DEFAULTS.headingColor} />
          <ColorField label="粗體" value={draft.boldColor} onChange={set('boldColor')} placeholder={APPEARANCE_DEFAULTS.boldColor} />
          <ColorField label="行內程式碼" value={draft.codeColor} onChange={set('codeColor')} placeholder={APPEARANCE_DEFAULTS.codeColor} />
        </div>
      </fieldset>
      </div>

      <div className="flex min-w-0 flex-col gap-2">
        <label htmlFor="settings-custom-css" className={settingsLabelCls}>自訂 CSS</label>
        <p id="settings-custom-css-help" className={`${settingsHintCls} [&_code]:break-all`}>
          按下儲存後套用到整個頁面；留空即不生效。範例：<code className="ra-mono">.prose blockquote {'{'} color: hotpink; {'}'}</code>
          {' '}僅在特定主題生效時可用 <code className="ra-mono">html[data-theme=&quot;light&quot;]</code> 或 <code className="ra-mono">html[data-theme=&quot;dark&quot;]</code> 限定，例如{' '}
          <code className="ra-mono">html[data-theme=&quot;light&quot;] .prose {'{'} color: #333; {'}'}</code>
        </p>
        <textarea
          id="settings-custom-css"
          aria-describedby="settings-custom-css-help"
          value={draft.customCss}
          onChange={(e) => setDraft((prev) => ({ ...prev, customCss: e.target.value }))}
          placeholder=".prose h1 { text-decoration: underline; }"
          rows={8}
          spellCheck={false}
          autoCorrect="off"
          autoCapitalize="none"
          className={`${settingsControlCls} w-full resize-y font-mono leading-relaxed`}
        />
      </div>
    </div>
  );
}

function SettingsModal({ open, onClose, onJumpToSession = null, desktopUpdate = null }) {
  const [section, setSection] = useState('general');
  const mobile = useMediaQuery('(max-width: 639px), (max-width: 1023px) and (pointer: coarse)');
  // ponytail: two modal levels only; keep navigation local rather than adding routes.
  const [mobileDetail, setMobileDetail] = useState(false);
  const backRef = useRef(null);
  // 桌面可安裝更新；瀏覽器／Telegram 只有版本資訊。
  const updateBridge = !!(desktopUpdate && desktopUpdate.bridge);
  const updateVisible = !!desktopUpdate;
  const updateOpenNonce = desktopUpdate ? desktopUpdate.openNonce : 0;
  const [draft, setDraft] = useState(() => readStoredAppearance());
  const [savedFlash, setSavedFlash] = useState(false);
  const [saveError, setSaveError] = useState('');
  const [saving, setSaving] = useState(false);
  const [buildInfo, setBuildInfo] = useState(null);

  // 開啟時重新從 localStorage 載入，避免上次關閉未存的草稿殘留。
  useEffect(() => {
    setMobileDetail(false);
    if (open) {
      setDraft(readStoredAppearance());
      setSaveError('');
      setSavedFlash(false);
      fetchServerConfig().then(setBuildInfo);
    }
  }, [open]);

  // 更新通知直接進入更新內容；一般開啟仍從手機分類清單開始。
  useEffect(() => {
    if (updateOpenNonce > 0) {
      setSection('update');
      setMobileDetail(true);
    }
  }, [updateOpenNonce]);

  const dialogRef = useRef(null);
  // 原生 modal 提供對話框語意、焦點容納、Esc 關閉與關閉後的焦點還原。
  // 不要加 role="dialog"／aria-modal／焦點陷阱／Esc handler（見 docs/spec/frontend-architecture.md 第 5 節）。
  // 手機第二層的 Esc 要「返回分類」而不是關閉，改由 onCancel 攔截。
  useLayoutEffect(() => {
    if (!open) return undefined;
    const dialog = dialogRef.current;
    dialog.showModal();
    return () => dialog.close();
  }, [open]);

  // 手機版的刻意焦點轉移（進第二層移到返回鍵、回第一層移到剛才的分類）。
  // 桌面不處理：原生 modal 自己會把焦點放進對話框。
  useEffect(() => {
    if (!open || !mobile) return;
    const dialog = dialogRef.current;
    const target = mobileDetail ? backRef.current : dialog?.querySelector(`[data-settings-section="${section}"]`);
    target?.focus({ preventScroll: true });
  }, [open, mobile, mobileDetail]);

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
  const sections = updateVisible ? [...SETTINGS_SECTIONS, { id: 'update', label: '更新' }] : SETTINGS_SECTIONS;
  const enterSection = (id) => { setSection(id); setMobileDetail(true); };
  const showAppearanceActions = section === 'appearance';
  const sectionLabel = sections.find((s) => s.id === section)?.label;

  return (
    <dialog
      ref={dialogRef}
      className={`ra-dialog ra-dialog-settings overflow-hidden bg-ra-bg text-ra-t1 accent-[var(--ra-accent)] shadow-2xl shadow-black/40 ${mobile ? 'mobile flex-col' : 'rounded-xl border border-ra-line'}`}
      style={mobile ? { paddingBottom: 'max(env(safe-area-inset-bottom, 0px), var(--tg-safe-area-inset-bottom, 0px))' } : undefined}
      aria-labelledby="settings-modal-title"
      onClose={onClose}
      onCancel={(e) => {
        // 手機第二層：Esc／返回手勢先回到分類清單，不關閉整個設定
        if (mobile && mobileDetail) { e.preventDefault(); setMobileDetail(false); }
      }}
      onClick={(e) => {
        // 點 backdrop 時 target 也是 <dialog>（它的 padding 與子元素間隙同理），所以測指標位置而不是 target
        if (e.target !== e.currentTarget) return;
        const r = e.currentTarget.getBoundingClientRect();
        if (e.clientX < r.left || e.clientX > r.right || e.clientY < r.top || e.clientY > r.bottom) e.currentTarget.close();
      }}
    >
        {mobile && <header className="flex shrink-0 items-center gap-2 border-b border-ra-line px-3 pb-2"
          style={{ paddingTop: 'calc(max(8px, env(safe-area-inset-top, 0px), var(--tg-safe-area-inset-top, 0px)) + var(--tg-content-safe-top, 0px))' }}>
          {mobileDetail && <button type="button" ref={backRef} aria-label="返回設定分類"
            className="flex h-12 w-12 shrink-0 items-center justify-center rounded-lg text-ra-t2 hover:bg-ra-raised focus-visible:outline focus-visible:outline-2 focus-visible:outline-ra-accent-text"
            onClick={() => setMobileDetail(false)}>
            <svg viewBox="0 0 24 24" className="h-5 w-5" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true"><path d="m14 6-6 6 6 6" /></svg>
          </button>}
          <h2 id="settings-modal-title" className="flex-1 px-2 text-lg font-medium text-ra-t1">{mobileDetail ? sectionLabel : '設定'}</h2>
          <button type="button" aria-label="關閉設定" onClick={() => dialogRef.current?.close()}
            className="flex h-12 w-12 shrink-0 items-center justify-center rounded-lg text-ra-t2 hover:bg-ra-raised focus-visible:outline focus-visible:outline-2 focus-visible:outline-ra-accent-text">
            <svg viewBox="0 0 24 24" className="h-5 w-5" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" aria-hidden="true"><path d="m6 6 12 12M6 18 18 6" /></svg>
          </button>
        </header>}
        {/* 桌面側欄／手機第一層分類 */}
        {(!mobile || !mobileDetail) && <div className={`${mobile ? 'min-h-0 flex-1 overflow-y-auto' : 'w-48 min-h-0 shrink-0 border-r border-ra-line'} flex flex-col bg-ra-sunken`}>
          {!mobile && <div id="settings-modal-title" className="px-4 pb-3 pt-5 text-lg font-medium text-ra-t1">
            設定
          </div>}
          <nav aria-label="設定分類" className={`flex min-h-0 flex-1 flex-col gap-1 overflow-y-auto overscroll-contain ${mobile ? 'p-3' : 'px-2'}`}>
            {sections.map((s) => (
              <button
                key={s.id}
                type="button"
                data-settings-section={s.id}
                onClick={() => enterSection(s.id)}
                className={`flex w-full items-center rounded-lg text-left text-sm focus-visible:outline focus-visible:outline-2 focus-visible:outline-ra-accent-text ${mobile ? 'min-h-12 px-3' : 'min-h-10 px-3'} ${
                  !mobile && section === s.id
                    ? 'bg-ra-card font-medium text-ra-t1 shadow-[inset_2px_0_0_var(--ra-accent)]'
                    : 'text-ra-t3 hover:bg-ra-card hover:text-ra-t1'
                }`}
              >
                {s.label}
                {s.id === 'update' && desktopUpdate?.hasUpdate && <span className="ml-2 inline-block h-2 w-2 rounded-full bg-[var(--ra-accent)]" role="img" aria-label="有可用更新" />}
                {mobile && <span className="ml-auto text-ra-t5" aria-hidden="true">›</span>}
              </button>
            ))}
          </nav>
          <div className="shrink-0 border-t border-ra-line-soft px-4 py-3 text-[13px] leading-5 font-mono text-ra-t3 select-text">
            <div>v{buildInfo?.version || '—'}</div>
            <div title={buildInfo?.sha || 'unknown'}>SHA {buildInfo?.sha?.slice(0, 7) || '—'}</div>
            {updateVisible ? (
              <button
                type="button"
                onClick={() => enterSection('update')}
                className={`${settingsGhostCls} mt-1 px-0 font-sans`}
              >
                前往更新
              </button>
            ) : null}
          </div>
        </div>}

        {/* 右側內容 */}
        {(!mobile || mobileDetail) && <div className="flex min-h-0 min-w-0 flex-1 flex-col">
          {!mobile && <div className="shrink-0 px-6 pt-6">
            <h3 className="text-lg font-medium leading-7 text-ra-t1">{sectionLabel}</h3>
          </div>}
          {/* 日誌頁自己管捲動（清單內捲、頂部工具列固定），所以外層不捲 */}
          <div className={`app-scroll min-h-0 min-w-0 flex-1 overscroll-contain px-6 ${section === 'logs' ? 'overflow-hidden py-4' : 'overflow-y-auto py-6'} ${mobile ? '[&_button]:min-h-12 [&_input[type=text]]:min-h-12 [&_input[type=number]]:min-h-12 [&_select]:min-h-12' : ''}`}>
            {section === 'general' && <GeneralSection />}
            {section === 'appearance' && <AppearanceSection draft={draft} setDraft={setDraft} />}
            {section === 'shares' && <SharesSection onJumpToSession={onJumpToSession} />}
            {section === 'system' && <SystemStatsSection />}
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
            {section === 'update' && updateVisible && !updateBridge && <WebUpdateSection status={desktopUpdate.webStatus} />}
          </div>
          <div className={`flex shrink-0 flex-wrap items-center justify-between gap-3 border-t border-ra-line px-6 py-3 ${mobile ? '[&_button]:min-h-12' : ''}`}>
            {showAppearanceActions && (
              <button type="button" onClick={handleReset} className={settingsGhostCls}>
                還原預設
              </button>
            )}
            <div className="ml-auto flex flex-wrap items-center justify-end gap-3">
              {showAppearanceActions && saveError && <span role="alert" className="min-w-0 text-[13px] leading-5 text-red-400">{saveError}</span>}
              {showAppearanceActions && !saveError && savedFlash && <span role="status" className="text-[13px] leading-5 text-ra-ok">已儲存</span>}
              <button
                type="button"
                // 走原生 close()，onClose 由 close 事件觸發，瀏覽器才會把焦點還給開啟者
                onClick={() => dialogRef.current?.close()}
                className={settingsBtnCls}
              >
                關閉
              </button>
              {showAppearanceActions && (
                <button type="button" onClick={handleSave} disabled={saving} className={settingsPrimaryCls}>
                  {saving ? '儲存中…' : '儲存'}
                </button>
              )}
            </div>
          </div>
        </div>}
    </dialog>
  );
}
