// ── 桌面更新區塊（設定頁「更新」分類；系統匣「檢查更新」開啟的也是同一區）────
// 只在桌面 bridge 存在時由 SettingsModal 渲染。其他文字以 React 文字節點呈現；release notes 是唯一用
// innerHTML 的地方，且只吃 desktopUpdateRenderNotes 的輸出：使用者文字先逐字跳脫、標籤只由程式產生
// （白名單與屬性見 desktopUpdate.js，另有不變量測試），所以 notes 內的 HTML／script 只會顯示成文字。
// 純邏輯（狀態文字、動作模型、notes 渲染）在 desktopUpdate.js。

function DesktopUpdateSection({ status, laterVersion = '', request, later, showOffer }) {
  const st = status || { state: 'idle' };
  const view = desktopUpdateView(st, laterVersion);
  const percent = desktopUpdateProgressPercent(st.downloadedBytes, st.totalBytes);
  const inProgress = st.state === 'downloading' || st.state === 'verifying';
  // 只取「## 更新內容」之後的 changelog，再以安全的 Markdown 子集渲染（不含原始 HTML）。
  const notesHtml = useMemo(() => desktopUpdateRenderNotes(desktopUpdateNotesBody(st.releaseNotes)), [st.releaseNotes]);

  // 重新啟動需要二次確認：第一次點擊只展開確認面板，按下「確定」才會送出 restart。
  const [confirming, setConfirming] = useState(false);
  useEffect(() => {
    if (st.state !== 'ready') setConfirming(false);
  }, [st.state]);
  const restartEnabled = view.actions.some((a) => a.id === 'restart' && !a.disabled);
  const showConfirm = confirming && restartEnabled;

  const run = (action) => {
    if (action.disabled) return;
    if (action.id === 'later') return later(st.availableVersion);
    if (action.id === 'showOffer') return showOffer();
    if (action.id === 'restart') return setConfirming(true);
    request(action.id);
  };

  const confirmRestart = () => {
    setConfirming(false);
    request('restart');
  };

  const updatePrimaryCls = 'inline-flex min-h-12 items-center justify-center rounded-lg bg-ra-t1 px-3 text-sm font-medium text-ra-bg hover:bg-ra-t2 focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ra-accent-text disabled:opacity-40 sm:min-h-10';
  const updateSecondaryCls = 'inline-flex min-h-12 items-center justify-center rounded-lg border border-ra-line bg-ra-card px-3 text-sm font-medium text-ra-t1 hover:bg-ra-raised focus-visible:outline focus-visible:outline-2 focus-visible:outline-ra-accent-text disabled:opacity-40 sm:min-h-10';

  return (
    <div className="flex flex-col gap-4">
      <div>
        <h3 className="text-[15px] font-medium leading-6 text-ra-t1">桌面程式更新</h3>
        <p className="mt-1 text-[13px] leading-5 text-ra-t3 ra-mono">
          目前版本 v{st.currentVersion || '—'} · SHA {desktopUpdateShortSha(st.currentSha)}
        </p>
      </div>

      <p className="text-sm leading-6 text-ra-t2">
        狀態：{view.stateLabel}
        {st.availableVersion ? <span className="ml-2 ra-mono">可用版本 v{st.availableVersion}</span> : null}
      </p>

      {inProgress ? (
        <div>
          <div className="h-1.5 w-full overflow-hidden rounded-full bg-ra-sunken">
            <div className="h-full bg-[var(--ra-accent)] transition-[width]" style={{ width: percent + '%' }} />
          </div>
          <p className="mt-1 text-[13px] leading-5 text-ra-t3 ra-mono">{percent}%</p>
        </div>
      ) : null}

      {view.showNotes && notesHtml ? (
        <div>
          <h4 className="mb-2 text-sm font-medium text-ra-t2">更新內容</h4>
          <div
            className="ra-notes max-h-64 overflow-y-auto overscroll-contain rounded-lg border border-ra-line bg-ra-card px-3 py-3"
            dangerouslySetInnerHTML={{ __html: notesHtml }}
          />
        </div>
      ) : null}

      {st.errorCode ? (
        <div role="alert" className="text-[13px] leading-5 text-red-400">
          {desktopUpdateErrorText(st.errorCode)}
          {st.busyReason ? <p className="mt-1 text-ra-t3">原因：{desktopUpdateBusyReasonText(st.busyReason)}</p> : null}
          {st.errorMessage ? <p className="mt-1 whitespace-pre-line break-words text-ra-t3">{st.errorMessage}</p> : null}
        </div>
      ) : null}

      <div className="flex flex-wrap items-center gap-2">
        {view.actions.map((a) => (
          <button
            key={a.id}
            type="button"
            disabled={a.disabled}
            onClick={() => run(a)}
            className={a.id === 'download' || a.id === 'restart' ? updatePrimaryCls : updateSecondaryCls}
          >
            {a.label}
          </button>
        ))}
      </div>

      {showConfirm ? (
        <div className="rounded-lg border border-ra-line bg-ra-sunken px-4 py-4 text-sm leading-6 text-ra-t1">
          <p>確定要關閉桌面程式並套用更新嗎？</p>
          <p className="mt-1 text-[13px] leading-5 text-ra-t3">
            只有在沒有進行中的工作時才會套用；若仍有工作，更新會被拒絕並保留，之後可再試。
          </p>
          <div className="mt-3 flex flex-wrap items-center gap-2">
            <button type="button" onClick={confirmRestart} className={updatePrimaryCls}>
              確定重新啟動並更新
            </button>
            <button type="button" onClick={() => setConfirming(false)} className={updateSecondaryCls}>
              取消
            </button>
          </div>
        </div>
      ) : null}
    </div>
  );
}

// Browser/Telegram deliberately have no install, download or restart controls.
function WebUpdateSection({ status }) {
  const st = status || {};
  const newer = st.availableVersion;
  return (
    <div className="flex flex-col gap-3 text-sm leading-6 text-ra-t2">
      <h3 className="text-lg font-medium text-ra-t1">版本資訊</h3>
      <p>主機版本 v{st.currentVersion || '—'}{newer ? ` · 最新正式版 v${newer}` : ''}</p>
      <p>{st.reloadRequired ? '主機已更新。草稿會保留，請自行重新整理載入新版。' : newer ? '有新版可用，請由主機管理者更新程式。網頁版僅提供通知。' : '每小時檢查新版；此頁不會安裝或重新啟動主機程式。'}</p>
      {newer && <a className="w-fit text-ra-accent-text underline underline-offset-2" href={`https://github.com/jerry12122/Claude-Code-Mini-App/releases/tag/v${newer}`} target="_blank" rel="noopener noreferrer" referrerPolicy="no-referrer">查看正式版</a>}
    </div>
  );
}
