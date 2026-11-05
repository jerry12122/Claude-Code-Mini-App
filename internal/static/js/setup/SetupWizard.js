// 首次設定精靈。表單規則在 setupForm.js；這裡只畫步驟與呼叫 API。

function SetupGate({ guest }) {
  const [state, setState] = useState({ pending: true });
  const load = () => {
    setState({ pending: true });
    fetchWithTimeout(resolveApiUrl('/setup/status'))
      .then((res) => (res.ok ? res.json() : Promise.reject(new Error('status ' + res.status))))
      .then((status) => setState({ pending: false, status }))
      .catch((error) => setState({ pending: false, error }));
  };
  useEffect(load, []);
  const view = setupBootView(state);
  if (view === 'pending') {
    return <div className="flex items-center justify-center h-app text-gray-400 text-sm">正在確認是否需要首次設定…</div>;
  }
  if (view === 'error') {
    return (
      <div className="flex items-center justify-center h-app">
        <div className="w-80 space-y-3 text-center">
          <div className="text-gray-300 text-sm">無法確認設定狀態，沒有進入首次設定。</div>
          <button type="button" onClick={load} className="px-4 py-2 bg-violet-600 hover:bg-violet-500 text-white rounded-lg text-sm">重試</button>
        </div>
      </div>
    );
  }
  if (view === 'wizard') return <SetupWizard status={state.status} />;
  return guest ? <GuestApp /> : <App />;
}

function SetupWizard({ status }) {
  const steps = setupWizardSteps();
  const [step, setStep] = useState(0);
  const [form, setForm] = useState(() => defaultSetupForm(status && status.port ? status.port : 8080));
  const [token, setToken] = useState('');
  const [tools, setTools] = useState([]);
  const [cliNote, setCliNote] = useState('');
  const [saving, setSaving] = useState(false);
  const [outcome, setOutcome] = useState(null);

  useEffect(() => {
    const found = readSetupToken(location.search);
    setToken(found);
    if (found) history.replaceState(null, '', stripSetupToken(location.href));
  }, []);

  useEffect(() => {
    if (!token) return undefined;
    let cancel = false;
    fetchWithTimeout(resolveApiUrl('/setup/cli-check'), { headers: { 'X-Setup-Token': token } })
      .then((res) => (res.ok ? res.json() : Promise.reject(new Error('cli'))))
      .then((data) => { if (!cancel) setTools(data.tools || []); })
      .catch(() => { if (!cancel) setCliNote('無法檢查本機的代理程式，仍可繼續設定。'); });
    return () => { cancel = true; };
  }, [token]);

  const name = steps[step];
  const advisory = setupCliAdvisory(tools);
  const errors = validateSetupForm(form).errors;
  const locked = outcome && outcome.resend === false;
  const setField = (patch) => setForm((prev) => Object.assign({}, prev, patch));
  const setMini = (patch) => setForm((prev) => Object.assign({}, prev, { miniapp: Object.assign({}, prev.miniapp, patch) }));

  const ready = name === 'welcome' || (name === 'account' && setupAccountReady(form)) || (name === 'miniapp' && (!form.miniapp.enabled || (!errors['miniapp.bot_token'] && !errors['miniapp.whitelist_tg_ids']))) || (name === 'review' && setupCanSave(form) && !!token && !locked);

  const save = async () => {
    if (!setupCanSave(form) || !token || saving || locked) return;
    setSaving(true);
    try {
      const res = await fetchWithTimeout(resolveApiUrl('/setup/save'), {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', 'X-Setup-Token': token },
        body: JSON.stringify(buildSetupPayload(form)),
      });
      const body = await res.json().catch(() => ({}));
      const out = setupSaveOutcome(res.status, body);
      if (out.kind === 'switched') {
        location.assign(out.url || '/');
        return;
      }
      setOutcome(out);
    } catch (_) {
      setOutcome({ kind: 'error', error: '連線失敗', resend: true });
    } finally {
      setSaving(false);
    }
  };

  return (
    <div className="flex items-center justify-center h-app p-4">
      <div className="w-full max-w-md space-y-4">
        <div className="text-center">
          <div className="ra-display text-2xl" style={{ color: 'oklch(var(--ra-t1))' }}>首次設定</div>
          <div className="text-gray-500 text-sm">{step + 1} / {steps.length}</div>
        </div>
        {name === 'welcome' && (
          <div className="space-y-3">
            <p className="text-gray-300 text-sm">歡迎。下面是這台電腦上的代理程式，只供參考，不影響儲存。</p>
            {!token && <p className="text-amber-300 text-sm">{setupTokenHint('')}</p>}
            {cliNote && <p className="text-gray-400 text-sm">{cliNote}</p>}
            {advisory.recommend && <p className="text-gray-300 text-sm">{advisory.message}</p>}
            <ul className="space-y-2 text-sm text-gray-300">
              {tools.map((tool) => (
                <li key={tool.agent_type || tool.command}>
                  {(tool.label || tool.command) + '：' + (tool.found ? (tool.version || '已找到') : '未安裝')}
                  {tool.install_url && <a className="ml-2 text-violet-400 underline" href={tool.install_url} target="_blank" rel="noreferrer">安裝說明</a>}
                  {tool.note && <span className="block text-gray-500 text-xs">{tool.note}</span>}
                </li>
              ))}
            </ul>
          </div>
        )}
        {name === 'account' && (
          <div className="space-y-3">
            <label className="block text-sm text-gray-400">登入密碼
              <input type="text" value={form.password} onChange={(e) => setField({ password: e.target.value })} className="mt-1 w-full bg-gray-800 border border-gray-700 rounded-lg px-4 py-2.5 text-sm text-gray-200" />
            </label>
            {errors.password && <div className="text-red-400 text-xs">{errors.password}</div>}
            <button type="button" onClick={() => setForm(setupApplyGeneratedPassword(form))} className="text-sm text-violet-400">產生隨機密碼</button>
            <label className="block text-sm text-gray-400">連接埠
              <input type="number" value={form.port} onChange={(e) => setField({ port: e.target.value === '' ? '' : Number(e.target.value) })} className="mt-1 w-full bg-gray-800 border border-gray-700 rounded-lg px-4 py-2.5 text-sm text-gray-200" />
            </label>
            {errors.port && <div className="text-red-400 text-xs">{errors.port}</div>}
            <label className="block text-sm text-gray-400">資料庫路徑
              <input type="text" value={form.db_path} onChange={(e) => setField({ db_path: e.target.value })} className="mt-1 w-full bg-gray-800 border border-gray-700 rounded-lg px-4 py-2.5 text-sm text-gray-200" />
            </label>
            {errors.db_path && <div className="text-red-400 text-xs">{errors.db_path}</div>}
            <label className="flex items-center gap-2 text-sm text-gray-300">
              <input type="checkbox" checked={form.allow_lan === true} onChange={(e) => setField({ allow_lan: e.target.checked })} />
              允許區網裝置登入
            </label>
          </div>
        )}
        {name === 'miniapp' && (
          <div className="space-y-3">
            <label className="flex items-center gap-2 text-sm text-gray-300">
              <input type="checkbox" checked={form.miniapp.enabled === true} onChange={(e) => setMini({ enabled: e.target.checked })} />
              啟用 Telegram Mini App
            </label>
            {form.miniapp.enabled && (
              <div className="space-y-3">
                <p className="text-gray-400 text-sm">Bot 需要一個從外部打得到的 HTTPS 網址（例如隧道）。這個精靈不會建立它。</p>
                <label className="block text-sm text-gray-400">Bot token
                  <input type="text" value={form.miniapp.bot_token} onChange={(e) => setMini({ bot_token: e.target.value })} className="mt-1 w-full bg-gray-800 border border-gray-700 rounded-lg px-4 py-2.5 text-sm text-gray-200" />
                </label>
                {errors['miniapp.bot_token'] && <div className="text-red-400 text-xs">{errors['miniapp.bot_token']}</div>}
                <label className="block text-sm text-gray-400">白名單 Telegram ID（逗號或換行）
                  <textarea value={form.miniapp.whitelist_text} onChange={(e) => setMini({ whitelist_text: e.target.value })} className="mt-1 w-full bg-gray-800 border border-gray-700 rounded-lg px-4 py-2.5 text-sm text-gray-200" rows={3} />
                </label>
                {errors['miniapp.whitelist_tg_ids'] && <div className="text-red-400 text-xs">{errors['miniapp.whitelist_tg_ids']}</div>}
              </div>
            )}
          </div>
        )}
        {name === 'review' && (
          <div className="space-y-2 text-sm text-gray-300">
            <div>連接埠 {form.port}</div>
            <div>資料庫 {String(form.db_path).trim()}</div>
            <div>區網登入 {form.allow_lan ? '開' : '關'}</div>
            <div>Mini App {form.miniapp.enabled ? '開' : '關'}</div>
            {!token && <p className="text-amber-300">{setupTokenHint('')}</p>}
            {outcome && outcome.kind === 'restart' && <p className="text-amber-300">連接埠已改，請重新啟動程式後開啟 {outcome.url}</p>}
            {outcome && outcome.kind === 'start_failed' && <p className="text-red-400">設定已寫入，但沒有切換成功：{outcome.error}。請修正 config.yaml 後重新啟動，不要再送一次。</p>}
            {outcome && outcome.kind === 'invalid' && <p className="text-red-400">{outcome.error}</p>}
            {outcome && outcome.kind === 'forbidden' && <p className="text-red-400">{outcome.error || setupTokenHint('')}</p>}
            {outcome && outcome.kind === 'exists' && <p className="text-amber-300">{outcome.error}。請重新啟動。</p>}
            {outcome && (outcome.kind === 'write_failed' || outcome.kind === 'error') && <p className="text-red-400">{outcome.error}</p>}
          </div>
        )}
        <div className="flex gap-2">
          {step > 0 && <button type="button" disabled={saving || locked} onClick={() => setStep(step - 1)} className="flex-1 py-2.5 border border-gray-700 disabled:opacity-40 text-gray-300 rounded-lg text-sm">上一步</button>}
          {name !== 'review'
            ? <button type="button" disabled={!ready} onClick={() => ready && setStep(step + 1)} className="flex-1 py-2.5 bg-violet-600 hover:bg-violet-500 disabled:opacity-40 text-white rounded-lg text-sm">下一步</button>
            : <button type="button" disabled={!ready || saving} onClick={save} className="flex-1 py-2.5 bg-violet-600 hover:bg-violet-500 disabled:opacity-40 text-white rounded-lg text-sm">{saving ? '儲存中…' : '儲存'}</button>}
        </div>
      </div>
    </div>
  );
}
