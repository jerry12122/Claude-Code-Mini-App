/** 沿用標題列徽章與應用程式選單樣式；資料由 header 共用。 */
function GitControls({ session, state, git }) {
  const { status, branches, loading, action, reload, run } = git;
  const triggerRef = useRef(null);
  const popoverRef = useRef(null);
  const branchTriggerRef = useRef(null);
  const branchPopoverRef = useRef(null);
  const popoverId = React.useId();
  const [open, setOpen] = useState(false);
  const [branchOpen, setBranchOpen] = useState(false);
  const busy = session.status !== 'idle' || state !== 'IDLE';
  const reason = busy ? 'session 閒置後才能執行' : action ? 'Git 操作進行中' : '';
  const disabled = busy || !!action;
  const current = status?.branch || session.git_branch;
  const local = branches.includes(current) ? branches : [current, ...branches].filter(Boolean);
  const noUpstream = !status?.upstream || status?.detached;
  const close = (branch = false) => {
    (branch ? branchTriggerRef : triggerRef).current?.focus({ preventScroll: true });
    (branch ? branchPopoverRef : popoverRef).current?.hidePopover();
  };
  useEffect(() => {
    const cleanups = [[popoverRef, triggerRef, setOpen], [branchPopoverRef, branchTriggerRef, setBranchOpen]].map(([panel, trigger, setVisible]) => {
      const popover = panel.current;
      if (!popover) return () => {};
      // ponytail: 不支援 CSS anchor 的瀏覽器只需這段定位，不引入浮層套件。
      const place = () => {
        if (CSS.supports('position-area: block-end') || !popover.matches(':popover-open')) return;
        const r = trigger.current.getBoundingClientRect();
        const top = r.bottom + popover.offsetHeight + 4 <= innerHeight ? r.bottom + 4 : r.top - popover.offsetHeight - 4;
        popover.style.setProperty('--git-left', `${Math.max(8, Math.min(r.left, innerWidth - popover.offsetWidth - 8))}px`);
        popover.style.setProperty('--git-top', `${Math.max(8, top)}px`);
      };
      const toggle = event => {
        const visible = event.newState === 'open';
        setVisible(visible);
        if (visible) { place(); reload(); }
      };
      popover.addEventListener('toggle', toggle);
      window.addEventListener('resize', place);
      window.addEventListener('scroll', place, true);
      return () => {
        popover.removeEventListener('toggle', toggle);
        window.removeEventListener('resize', place);
        window.removeEventListener('scroll', place, true);
      };
    });
    return () => cleanups.forEach(cleanup => cleanup());
  }, [reload]);

  if (status?.is_repo === false) return null;
  return (
    <div className="inline-flex max-w-full shrink-0 flex-wrap items-center gap-1.5" aria-busy={!!action}>
      <button ref={branchTriggerRef} type="button" popovertarget={`${popoverId}-branch`} aria-label="切換分支" aria-expanded={branchOpen}
        disabled={disabled || loading} title={reason || `切換分支：${current}`}
        className="ra-git-trigger inline-flex min-h-6 min-w-6 items-center rounded-md hover:brightness-125 focus-visible:outline focus-visible:outline-2 focus-visible:outline-ra-t3 disabled:opacity-50">
        <GitBranchBadge branch={current} compact />
      </button>
      <span className="rounded-md bg-ra-card px-[7px] py-[3px] ra-mono text-[11px] font-semibold text-ra-t3" aria-label={status ? `上游同步：領先 ${status.ahead}，落後 ${status.behind}` : '正在取得同步狀態'}>
        ↑{status ? status.ahead : '…'} ↓{status ? status.behind : '…'}
      </span>
      <span className="inline-flex gap-1.5 rounded-md bg-ra-card px-[7px] py-[3px] ra-mono text-[11px] font-semibold" title="目前工作樹相對 HEAD 的未提交增刪行數，包含未追蹤文字檔">
        <span className="text-emerald-400">+{status?.additions ?? '…'}</span>
        <span className="text-red-400">-{status?.deletions ?? '…'}</span>
      </span>
      <button ref={triggerRef} type="button" popovertarget={popoverId} aria-label="Git 操作" aria-expanded={open}
        className="ra-git-trigger inline-flex min-h-6 min-w-6 items-center rounded-md text-ra-t3 focus-visible:outline focus-visible:outline-2 focus-visible:outline-ra-t3">
        <span className="inline-flex items-center gap-1 rounded-md bg-ra-card px-[7px] py-[3px] text-[11px] font-semibold hover:bg-ra-active">
          {action ? 'Git…' : 'Git'}
          <svg aria-hidden="true" viewBox="0 0 12 12" fill="none" className="h-2.5 w-2.5 text-ra-t4"><path d="m3 4.5 3 3 3-3" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" /></svg>
        </span>
      </button>
      <div id={`${popoverId}-branch`} ref={branchPopoverRef} popover="auto" className="ra-git-popover w-52 max-w-[calc(100vw-16px)] max-h-[min(20rem,calc(100dvh-16px))] overflow-auto app-scroll rounded-xl border border-ra-line-strong bg-ra-raised p-1 text-ra-t1 shadow-xl">
        <div role="group" aria-label="本機分支">
          <p className="px-3 py-1.5 text-[11px] font-semibold text-ra-t4">切換分支</p>
          {local.map(branch => <button key={branch} type="button" aria-current={branch === current ? 'true' : undefined}
            disabled={disabled || loading} title={reason || branch}
            className={`ra-git-option flex min-h-[36px] w-full items-center gap-2 rounded-lg px-3 py-2 text-left text-[13px] hover:bg-ra-active focus-visible:bg-ra-active focus-visible:outline-none disabled:opacity-40 ${branch === current ? 'bg-ra-active' : ''}`}
            onClick={() => { close(true); if (branch !== current) run('checkout', branch); }}>
            <svg aria-hidden="true" viewBox="0 0 16 16" fill="none" className="h-3.5 w-3.5 shrink-0 text-ra-t4"><circle cx="4" cy="3" r="1.5" stroke="currentColor" /><circle cx="4" cy="13" r="1.5" stroke="currentColor" /><circle cx="12" cy="4" r="1.5" stroke="currentColor" /><path d="M4 4.5v7M12 5.5v1A3.5 3.5 0 0 1 8.5 10H4" stroke="currentColor" /></svg>
            <span className="min-w-0 flex-1 truncate ra-mono">{branch}</span>
            {branch === current && <svg aria-hidden="true" viewBox="0 0 16 16" fill="none" className="h-3.5 w-3.5 shrink-0 text-ra-t2"><path d="m3 8 3 3 7-7" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" /></svg>}
          </button>)}
        </div>
      </div>
      <div id={popoverId} ref={popoverRef} popover="auto" className="ra-git-popover w-52 max-w-[calc(100vw-16px)] rounded-xl border border-ra-line-strong bg-ra-raised p-1 text-ra-t1 shadow-xl">
        <div role="group" aria-label="Git 操作">
          <p className="px-3 py-1.5 text-[11px] font-semibold text-ra-t4">Git 操作</p>
          {['pull', 'push'].map(kind => <button key={kind} type="button" disabled={disabled || noUpstream || !status}
            title={reason || (noUpstream ? '目前分支沒有可用的上游' : '')}
            onClick={() => { close(); run(kind); }}
            className="ra-git-option flex min-h-[36px] w-full items-center gap-2 rounded-lg px-3 py-2 text-left text-[13px] hover:bg-ra-active focus-visible:bg-ra-active focus-visible:outline-none disabled:opacity-40">
            <svg aria-hidden="true" viewBox="0 0 16 16" fill="none" className="h-3.5 w-3.5 text-ra-t4"><path d={kind === 'pull' ? 'M8 2v9m-3-3 3 3 3-3M3 13h10' : 'M8 11V2M5 5l3-3 3 3M3 13h10'} stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" /></svg>
            {kind === 'pull' ? 'Pull' : 'Push'}
          </button>)}
          {(reason || noUpstream) && <p className="px-3 py-1 text-xs text-ra-t4">{reason || '目前分支沒有可用的上游'}</p>}
        </div>
      </div>
    </div>
  );
}
