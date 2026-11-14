// A pending history.back must finish before another composer can own the top entry.
let composerToolsBackPending = false;

function ComposerToolChoice({ id, label, value, options, onChange, disabled, danger, sheet }) {
  const name = React.useId();
  const selected = options.find(option => option.value === value);
  const choices = selected ? options : [...options, { value, label: value || '預設' }];
  if (!sheet.page) return (
    <button type="button" className={'ra-tool-row' + (danger ? ' danger' : '')}
      data-tool={id} aria-label={label} disabled={disabled}
      onClick={() => sheet.setPage({ id, label })}>
      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
        {label === '模型' ? <><path d="m12 3 9 5-9 5-9-5 9-5ZM3 12l9 5 9-5M3 16l9 5 9-5" /></>
          : label === '思考強度' ? <><path d="M9 18h6M10 21h4M8 14a6 6 0 1 1 8 0c-1 .8-1 2-1 2H9s0-1.2-1-2Z" /></>
          : label === '權限' ? <path d="m12 3 8 3v6c0 5-8 9-8 9s-8-4-8-9V6l8-3Zm-4 9 3 3 5-6" />
          : <path d="m12 3 2.5 6.5L21 12l-6.5 2.5L12 21l-2.5-6.5L3 12l6.5-2.5L12 3Z" />}
      </svg>
      <span>{label}</span><span className="ra-tool-value">{selected?.label || value || '預設'}</span>
      <span aria-hidden="true">›</span>
    </button>
  );
  if (sheet.page.id !== id) return null;
  return (
    <fieldset className="ra-tool-choices" disabled={disabled}>
      <legend className="sr-only">選擇{label}</legend>
      {choices.map(option => (
        <label key={option.value} className={'ra-tool-row' + (['bypassPermissions', 'yolo'].includes(option.value) ? ' danger' : '')}>
          <span>{option.label}</span>
          <input type="radio" name={name} value={option.value} checked={value === option.value}
            onChange={() => { onChange(option.value); sheet.back(); }}
            onClick={() => { if (value === option.value) sheet.back(); }} />
          <span className="ra-tool-check" aria-hidden="true">✓</span>
        </label>
      ))}
    </fieldset>
  );
}

function ComposerTools({ open, setOpen, enabled, children }) {
  const compact = useMediaQuery('(max-width: 639px), (max-width: 1023px) and (max-height: 500px) and (pointer: coarse)');
  const desktop = useMediaQuery('(min-width: 1024px), (min-width: 640px) and (pointer: fine)');
  const mobile = compact || (open && !desktop);
  const [page, setPage] = useState(null);
  const plusRef = useRef(null);
  const dialogRef = useRef(null);
  const originRef = useRef(null);
  const stateRef = useRef({});
  const token = React.useId();
  const titleId = React.useId();
  const close = (restoreFocus = true) => {
    dialogRef.current?.close();
    originRef.current = null;
    setOpen(false);
    setPage(null);
    if (restoreFocus && plusRef.current?.isConnected) plusRef.current.focus({ preventScroll: true });
  };
  const back = () => {
    if (page) {
      originRef.current = page.id;
      setPage(null);
    } else close();
  };
  stateRef.current = { open, page, close, back };

  const releaseHistory = () => {
    if (composerToolsBackPending || history.state?.miniappComposerTools !== token) return;
    composerToolsBackPending = true;
    window.addEventListener('popstate', () => { composerToolsBackPending = false; }, { once: true });
    history.back();
  };
  useEffect(() => {
    const onPop = () => {
      const current = stateRef.current;
      if (!current.open) {
        // Consume our inactive Forward entry, so the next Back still leaves normally.
        releaseHistory();
        return;
      }
      if (history.state?.miniappComposerTools === token) return;
      if (current.page) {
        current.back();
        history.pushState({ ...history.state, miniappComposerTools: token }, '', location.href);
      } else current.close();
    };
    window.addEventListener('popstate', onPop);
    return () => {
      window.removeEventListener('popstate', onPop);
      releaseHistory();
    };
  }, []);

  useLayoutEffect(() => {
    if (!open || !mobile || !enabled || desktop) {
      if (open) close(false);
      return undefined;
    }
    const dialog = dialogRef.current;
    dialog.showModal();
    dialog.querySelector('[data-tools-close]').focus({ preventScroll: true });
    history.pushState({ ...history.state, miniappComposerTools: token }, '', location.href);
    const position = () => {
      const viewport = window.visualViewport;
      dialog.style.setProperty('--tools-height', `${Math.min(viewport?.height || innerHeight, window.Telegram?.WebApp?.viewportHeight || Infinity) * .7}px`);
      dialog.style.bottom = `${Math.max(0, innerHeight - ((viewport?.offsetTop || 0) + (viewport?.height || innerHeight)))}px`;
    };
    position();
    window.addEventListener('resize', position);
    window.visualViewport?.addEventListener('resize', position);
    window.visualViewport?.addEventListener('scroll', position);
    const tg = window.Telegram?.WebApp;
    const button = tg?.isVersionAtLeast?.('6.1') ? tg.BackButton : null;
    const wasVisible = button?.isVisible;
    const onBack = () => stateRef.current.back();
    button?.show();
    button?.onClick(onBack);
    tg?.onEvent?.('viewportChanged', position);
    return () => {
      dialog.close();
      releaseHistory();
      window.removeEventListener('resize', position);
      window.visualViewport?.removeEventListener('resize', position);
      window.visualViewport?.removeEventListener('scroll', position);
      button?.offClick(onBack);
      if (button && !wasVisible) button.hide();
      tg?.offEvent?.('viewportChanged', position);
    };
  }, [open, mobile, enabled, desktop]);

  useLayoutEffect(() => {
    const dialog = dialogRef.current;
    if (!open || !dialog?.open) return;
    dialog.querySelector('.ra-tools-body').scrollTop = 0;
    if (page) dialog.querySelector('input:checked')?.focus({ preventScroll: true });
    else if (originRef.current) {
      Array.from(dialog.querySelectorAll('[data-tool]')).find(el => el.dataset.tool === originRef.current)?.focus({ preventScroll: true });
      originRef.current = null;
    }
  }, [page, open]);

  if (!mobile) return <div className="ra-composer-tools">{children(null)}</div>;
  const sheet = { page, setPage, back, close };
  return (
    <>
      <button type="button" ref={plusRef} className="ra-composer-plus" aria-label="聊天工具"
        aria-haspopup="dialog" aria-expanded={open} aria-controls={titleId + '-dialog'} disabled={!enabled}
        onClick={() => { if (!open && !composerToolsBackPending) { setPage(null); setOpen(true); } }}>
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" aria-hidden="true"><path d="M12 5v14M5 12h14" /></svg>
      </button>
      {ReactDOM.createPortal(
        <dialog ref={dialogRef} id={titleId + '-dialog'} className="ra-composer-sheet" aria-labelledby={titleId}
          onCancel={event => { event.preventDefault(); back(); }}
          onKeyDown={event => { if (event.key === 'Escape') { event.preventDefault(); event.stopPropagation(); back(); } }}
          onClick={event => {
            if (event.target !== event.currentTarget) return;
            const rect = event.currentTarget.getBoundingClientRect();
            if (event.clientY < rect.top || event.clientY > rect.bottom || event.clientX < rect.left || event.clientX > rect.right) close();
          }}>
          <header className="ra-tools-header">
            {page && <button type="button" className="ra-tools-icon" aria-label="返回聊天工具" onClick={back}>
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true"><path d="m14 6-6 6 6 6" /></svg>
            </button>}
            <h2 id={titleId}>{page ? '選擇' + page.label : '聊天工具'}</h2>
            <button type="button" data-tools-close className="ra-tools-icon" aria-label="關閉聊天工具" onClick={() => close()}>
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" aria-hidden="true"><path d="m6 6 12 12M6 18 18 6" /></svg>
            </button>
          </header>
          <div className="ra-tools-body">{children(sheet)}</div>
        </dialog>, document.body)}
    </>
  );
}
