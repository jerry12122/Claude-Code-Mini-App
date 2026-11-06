function virtualMessageKey(message, index) {
  return message.id != null ? `id:${message.id}` : message.clientMessageId
    ? `client:${message.clientMessageId}` : `pending:${message.role}:${message.createdAt || index}`;
}

// Index of the row containing y, including exact boundaries and empty lists.
function virtualMessageIndex(offsets, y) {
  let low = 0, high = offsets.length - 1;
  while (low < high) {
    const mid = Math.floor((low + high + 1) / 2);
    if (offsets[mid] <= y) low = mid;
    else high = mid - 1;
  }
  return Math.max(0, Math.min(low, offsets.length - 2));
}

function virtualMessageWindow(offsets, top, height, windowed) {
  const count = offsets.length - 1;
  return {
    start: windowed ? virtualMessageIndex(offsets, Math.max(0, top - 800)) : 0,
    end: windowed ? Math.min(count, virtualMessageIndex(offsets, top + height + 800) + 1) : count,
  };
}

function VirtualMessages({ messages, scrollRef, followRef, gestureRef, showAll, children }) {
  const rootRef = useRef(null);
  const heightsRef = useRef(new Map());
  const anchorRef = useRef(null);
  const layoutRef = useRef(null);
  const [revision, setRevision] = useState(0);
  const [viewport, setViewport] = useState({ top: Infinity, height: 800 });
  const layout = useMemo(() => {
    const keys = messages.map(virtualMessageKey);
    const live = new Set(keys);
    for (const key of heightsRef.current.keys()) if (!live.has(key)) heightsRef.current.delete(key);
    const offsets = [0];
    for (const key of keys) offsets.push(offsets[offsets.length - 1] + (heightsRef.current.get(key) || 180));
    return { keys, offsets };
  }, [messages, revision]);
  layoutRef.current = layout;
  const { keys, offsets } = layout;
  const total = offsets[offsets.length - 1];
  let top = viewport.top;
  if (followRef.current || !Number.isFinite(top)) top = Math.max(0, total - viewport.height);
  else if (anchorRef.current) {
    const index = keys.indexOf(anchorRef.current.key);
    if (index >= 0) top = offsets[index] + anchorRef.current.delta;
  }
  // ponytail: prefix sums are O(n) per message batch; only visible rows mount.
  // Use an indexed height tree if loaded histories make this arithmetic expensive.
  const windowed = messages.length > 200 && !showAll && typeof ResizeObserver !== 'undefined';
  const { start, end } = virtualMessageWindow(offsets, top, viewport.height, windowed);

  useLayoutEffect(() => {
    const scroll = scrollRef.current, root = rootRef.current;
    if (!scroll || !root) return undefined;
    let frame = null;
    let width = scroll.clientWidth;
    const sample = () => {
      const origin = root.getBoundingClientRect().top - scroll.getBoundingClientRect().top + scroll.scrollTop;
      // Negative positions preserve the older-page controls above the list.
      const position = scroll.scrollTop - origin;
      const current = layoutRef.current;
      const index = virtualMessageIndex(current.offsets, position);
      anchorRef.current = current.keys.length ? { key: current.keys[index], delta: position - current.offsets[index] } : null;
      setViewport(previous => previous.top === position && previous.height === scroll.clientHeight
        ? previous : { top: position, height: scroll.clientHeight });
    };
    const schedule = () => { if (frame === null) frame = requestAnimationFrame(() => { frame = null; sample(); }); };
    // Capture before the parent can start a fast/cached older-page request.
    scroll.addEventListener('scroll', sample, { passive: true });
    const resize = typeof ResizeObserver === 'undefined' ? null : new ResizeObserver(() => {
      if (width !== scroll.clientWidth) {
        width = scroll.clientWidth;
        heightsRef.current.clear();
        setRevision(value => value + 1);
      }
      schedule();
    });
    resize?.observe(scroll);
    schedule();
    return () => {
      scroll.removeEventListener('scroll', sample);
      if (frame !== null) cancelAnimationFrame(frame);
      resize?.disconnect();
    };
  }, [scrollRef]);

  useLayoutEffect(() => {
    const scroll = scrollRef.current, root = rootRef.current;
    if (!scroll || !root) return undefined;
    const origin = root.getBoundingClientRect().top - scroll.getBoundingClientRect().top + scroll.scrollTop;
    const target = followRef.current ? scroll.scrollHeight : origin + top;
    if (Math.abs(scroll.scrollTop - Math.min(target, scroll.scrollHeight - scroll.clientHeight)) > 1) {
      gestureRef.current = false;
      scroll.scrollTop = target;
    }
    const measure = () => {
      let changed = false;
      for (const node of root.children) {
        const key = node.getAttribute('data-virtual-key');
        if (!key) continue;
        const height = node.getBoundingClientRect().height;
        if (height > 0 && Math.abs((heightsRef.current.get(key) || 0) - height) > 0.5) {
          heightsRef.current.set(key, height);
          changed = true;
        }
      }
      if (changed) setRevision(value => value + 1);
    };
    measure();
    const resize = typeof ResizeObserver === 'undefined' ? null : new ResizeObserver(measure);
    for (const node of root.children) if (node.hasAttribute('data-virtual-key')) resize?.observe(node);
    return () => resize?.disconnect();
  }, [layout, top, followRef, scrollRef, gestureRef]);

  return (
    <div ref={rootRef} role="list" aria-label="對話訊息" className="shrink-0 min-w-0" style={messages.length ? undefined : { display: 'none' }}>
      {start > 0 && <div aria-hidden="true" style={{ height: offsets[start] }} />}
      {messages.slice(start, end).map((message, localIndex) => {
        const index = start + localIndex;
        return <div key={keys[index]} data-virtual-key={keys[index]} role="listitem"
          aria-posinset={index + 1} aria-setsize={messages.length}
          style={{ paddingBottom: index === messages.length - 1 ? 0 : 20 }}>
          {children(message, index)}
        </div>;
      })}
      {end < messages.length && <div aria-hidden="true" style={{ height: total - offsets[end] }} />}
    </div>
  );
}
