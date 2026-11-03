// ── 桌面更新區塊（設定頁「更新」分類；系統匣「檢查更新」開啟的也是同一區）────
// 只在桌面 bridge 存在時由 SettingsModal 渲染。其他文字以 React 文字節點呈現；release notes 是唯一用
// innerHTML 的地方，且只吃 desktopUpdateRenderNotes 的輸出：使用者文字先逐字跳脫、標籤只由程式產生
// （白名單與屬性見 desktop-update.js，另有不變量測試），所以 notes 內的 HTML／script 只會顯示成文字。
// 純邏輯（狀態文字、動作模型、notes 渲染）在 desktop-update.js。

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

  return (
    <div className="divide-y divide-ra-line-soft [&>*]:py-6 [&>:first-child]:pt-0 [&>:last-child]:pb-0">
      <div>
        <div className="text-sm font-semibold text-ra-t1 mb-1">桌面程式更新</div>
        <div className="text-[11px] text-ra-t5 mb-3 ra-mono">
          目前版本 v{st.currentVersion || '—'} · SHA {desktopUpdateShortSha(st.currentSha)}
        </div>

        <div className="text-xs text-ra-t2">
          狀態：{view.stateLabel}
          {st.availableVersion ? <span className="ml-2 ra-mono">可用版本 v{st.availableVersion}</span> : null}
        </div>

        {inProgress ? (
          <div className="mt-3">
            <div className="h-1.5 w-full rounded-full bg-ra-raised overflow-hidden">
              <div className="h-full bg-violet-500 transition-[width]" style={{ width: percent + '%' }} />
            </div>
            <div className="text-[11px] text-ra-t5 mt-1 ra-mono">{percent}%</div>
          </div>
        ) : null}

        {view.showNotes && notesHtml ? (
          <div className="mt-3">
            <div className="text-[11px] text-ra-t4 mb-1">更新內容</div>
            <div
              className="ra-notes rounded-lg border border-ra-line bg-ra-card px-3 py-2 max-h-64 overflow-y-auto"
              dangerouslySetInnerHTML={{ __html: notesHtml }}
            />
          </div>
        ) : null}

        {st.errorCode ? (
          <div className="mt-3 text-xs text-red-400">
            {desktopUpdateErrorText(st.errorCode)}
            {st.busyReason ? <div className="text-[11px] text-ra-t3 mt-0.5">原因：{desktopUpdateBusyReasonText(st.busyReason)}</div> : null}
            {st.errorMessage ? <div className="text-[11px] text-ra-t5 mt-0.5 break-words">{st.errorMessage}</div> : null}
          </div>
        ) : null}

        <div className="mt-4 flex flex-wrap items-center gap-2">
          {view.actions.map((a) => (
            <button
              key={a.id}
              type="button"
              disabled={a.disabled}
              onClick={() => run(a)}
              className={`px-3 py-1.5 rounded-lg text-xs font-medium border disabled:opacity-40 ${
                a.id === 'download' || a.id === 'restart'
                  ? 'bg-violet-600 hover:bg-violet-500 border-violet-500 text-white'
                  : 'bg-ra-raised hover:bg-ra-raised border-ra-line-strong text-ra-t1'
              }`}
            >
              {a.label}
            </button>
          ))}
        </div>

        {showConfirm ? (
          <div className="mt-3 rounded-lg border border-violet-500/40 bg-violet-950/20 px-3 py-3 text-xs text-ra-t1">
            <div>確定要關閉桌面程式並套用更新嗎？</div>
            <div className="text-[11px] text-ra-t4 mt-1">
              只有在沒有進行中的工作時才會套用；若仍有工作，更新會被拒絕並保留，之後可再試。
            </div>
            <div className="mt-3 flex flex-wrap items-center gap-2">
              <button
                type="button"
                onClick={confirmRestart}
                className="px-3 py-1.5 rounded-lg text-xs font-medium border bg-violet-600 hover:bg-violet-500 border-violet-500 text-white"
              >
                確定重新啟動並更新
              </button>
              <button
                type="button"
                onClick={() => setConfirming(false)}
                className="px-3 py-1.5 rounded-lg text-xs font-medium border bg-ra-raised hover:bg-ra-raised border-ra-line-strong text-ra-t1"
              >
                取消
              </button>
            </div>
          </div>
        ) : null}
      </div>
    </div>
  );
}
