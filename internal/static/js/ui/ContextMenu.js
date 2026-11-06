function contextMenuPosition(x, y, width, height, viewportWidth, viewportHeight) {
  const edge = 8;
  return {
    left: Math.max(edge, Math.min(x, viewportWidth - width - edge)),
    top: Math.max(edge, Math.min(y, viewportHeight - height - edge)),
  };
}

function contextMenuNext(items, current, key) {
  const enabled = items.map((item, i) => !item.separator && !item.disabled ? i : -1).filter(i => i >= 0);
  if (!enabled.length) return -1;
  if (key === 'Home') return enabled[0];
  if (key === 'End') return enabled[enabled.length - 1];
  if (key !== 'ArrowDown' && key !== 'ArrowUp') return -1;
  const index = enabled.indexOf(current);
  if (index < 0) return key === 'ArrowDown' ? enabled[0] : enabled[enabled.length - 1];
  return enabled[(index + (key === 'ArrowDown' ? 1 : -1) + enabled.length) % enabled.length];
}

function contextTabCloseIds(tabs, id, mode) {
  const index = tabs.indexOf(id);
  if (index < 0) return [];
  return mode === 'others' ? tabs.filter(tab => tab !== id) : tabs.slice(index + 1);
}

function contextMenuNativeTarget(event) {
  if (event.shiftKey || event.target.closest?.('a, input, textarea, select, pre, code, img, [contenteditable="true"], .code-copy-btn')) return true;
  const selection = window.getSelection?.();
  if (selection?.toString() && event.currentTarget.contains &&
      (!event.currentTarget.contains(selection.anchorNode) || !event.currentTarget.contains(selection.focusNode))) return true;
  const button = event.target.closest?.('button');
  return !!button && button !== event.currentTarget && button.getAttribute('role') !== 'tab';
}

function readableMessageText(node) {
  if (!node) return '';
  if (node.nodeType === 3) return node.nodeValue || '';
  if (node.matches?.('button, script, style, .code-copy-btn')) return '';
  if (node.nodeName === 'BR') return '\n';
  const text = Array.from(node.childNodes || [], readableMessageText).join('');
  return /^(P|DIV|LI|PRE|H[1-6]|BLOCKQUOTE|TR)$/.test(node.nodeName) ? text + '\n' : text;
}

function openContextPayload(event, payload, touch = false) {
  const el = event.currentTarget;
  const rect = el.getBoundingClientRect();
  const selection = window.getSelection?.();
  const selectedText = selection && el.contains(selection.anchorNode) && el.contains(selection.focusNode) ? selection.toString() : '';
  return {
    ...payload, el, touch, selectedText,
    plainText: readableMessageText(el.querySelector?.('.prose')).trim() || payload.text,
    x: event.clientX || rect.left, y: event.clientY || rect.bottom,
    rect: { left: rect.left, top: rect.top, right: rect.right, bottom: rect.bottom, width: rect.width, height: rect.height },
  };
}

function ContextMenu({ menu, items, onClose, label = '操作選單' }) {
  const ref = useRef(null);
  const [position, setPosition] = useState(null);
  const close = () => {
    onClose();
    const target = menu?.el?.querySelector?.('[role="tab"]') || menu?.el;
    if (target?.isConnected) target.focus?.({ preventScroll: true });
  };
  useLayoutEffect(() => {
    if (!menu || !ref.current) return;
    const element = ref.current;
    const measure = () => {
      const next = contextMenuPosition(menu.x, menu.y, element.offsetWidth, element.offsetHeight, window.innerWidth, window.innerHeight);
      setPosition(previous => previous?.left === next.left && previous?.top === next.top ? previous : next);
    };
    measure();
    element.querySelector('button:not(:disabled)')?.focus({ preventScroll: true });
    const observer = typeof ResizeObserver === 'undefined' ? null : new ResizeObserver(measure);
    observer?.observe(element);
    return () => observer?.disconnect();
  }, [menu]);
  useEffect(() => {
    if (!menu) return;
    const outside = event => { if (!ref.current?.contains(event.target)) close(); };
    const key = event => { if (event.key === 'Escape') { event.preventDefault(); event.stopPropagation(); close(); } };
    document.addEventListener('pointerdown', outside, true);
    window.addEventListener('scroll', outside, true);
    window.addEventListener('resize', close);
    window.addEventListener('blur', close);
    window.addEventListener('keydown', key);
    return () => {
      document.removeEventListener('pointerdown', outside, true);
      window.removeEventListener('scroll', outside, true);
      window.removeEventListener('resize', close);
      window.removeEventListener('blur', close);
      window.removeEventListener('keydown', key);
    };
  }, [menu, onClose]);
  if (!menu) return null;
  return ReactDOM.createPortal(
    <>
      {menu.touch && <div className="fixed inset-0 z-[149] bg-black/40" aria-hidden="true" />}
      <div ref={ref} role="menu" aria-label={label}
        className="fixed z-[150] w-[240px] max-w-[calc(100vw-16px)] max-h-[calc(100dvh-16px)] overflow-auto app-scroll rounded-xl border border-ra-line-strong bg-ra-raised p-1 shadow-xl text-ra-t1"
        style={{ left: position?.left ?? menu.x, top: position?.top ?? menu.y, visibility: position ? 'visible' : 'hidden' }}
        onClick={event => event.stopPropagation()} onContextMenu={event => event.preventDefault()}
        onKeyDown={event => {
          if (event.key === 'Tab') { close(); return; }
          const current = Number(document.activeElement?.dataset.menuIndex);
          const next = contextMenuNext(items, current, event.key);
          if (next < 0) return;
          event.preventDefault(); event.stopPropagation();
          ref.current.querySelector(`[data-menu-index="${next}"]`)?.focus();
        }}>
        {items.map((item, index) => item.separator ? <div key={index} role="separator" className="my-1 border-t border-ra-line" /> : (
          <button key={index} type="button" role="menuitem" tabIndex={-1} data-menu-index={index}
            disabled={!!item.disabled} title={item.reason || ''}
            className={`w-full rounded-lg px-3 py-2 text-left text-[13px] min-h-[36px] ${menu.touch ? 'min-h-[44px]' : ''} hover:bg-ra-active focus:bg-ra-active focus:outline-none disabled:opacity-40 ${item.danger ? 'text-red-300' : ''}`}
            onClick={() => { close(); item.onSelect(); }}>
            {item.label}
            {item.disabled && item.reason && <span className="block text-[11px] text-ra-t4">{item.reason}</span>}
          </button>
        ))}
      </div>
    </>, document.body
  );
}
