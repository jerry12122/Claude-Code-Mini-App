function attachmentSize(size) {
  if (!Number.isFinite(size)) return '';
  if (size < 1024) return `${size} B`;
  if (size < 1024 * 1024) return `${Math.round(size / 1024)} KB`;
  return `${(size / (1024 * 1024)).toFixed(1)} MB`;
}

function attachmentMessageParts(message, sessionId) {
  const attachments = Array.isArray(message.attachments) ? [...message.attachments] : [];
  let text = message.content || '';
  // Old uploads used a UUID session directory and 32-character random filename.
  // Only transform the system's prefix, never paths in ordinary message text.
  if (message.role === 'user' && attachments.length === 0) {
    const prefix = /^\[附件\] ((?:[a-z]:[\\/]|\/)(?:[^\r\n]*[\\/])?workspace[\\/]uploads[\\/]([a-f0-9-]{36})[\\/]([a-f0-9]{32}\.(?:png|jpg|jpeg|webp|gif|pdf|txt|md|log|json|csv)))(?:\r?\n|$)/i;
    let match;
    while ((match = text.match(prefix)) && (!sessionId || match[2] === sessionId)) {
      attachments.push({ id: `legacy-${attachments.length}`, name: match[3], legacy: true, path: match[1] });
      text = text.slice(match[0].length);
    }
  }
  return { text, attachments };
}

function attachmentMessageCopy(message, sessionId) {
  const parts = attachmentMessageParts(message, sessionId);
  return [...parts.attachments.map(a => `[附件] ${a.name}`), parts.text].filter(Boolean).join('\n');
}

function attachmentQueueSummary(message, sessionId) {
  const parts = attachmentMessageParts({ ...message, role: 'user' }, sessionId);
  return [parts.text, parts.attachments.length ? `${parts.attachments.length} 個附件 · ${parts.attachments.map(a => a.name).join('、')}` : ''].filter(Boolean).join(' · ');
}

function AttachmentFileIcon({ name }) {
  return <span aria-hidden="true" className="flex h-10 w-10 shrink-0 items-center justify-center rounded-md bg-white/10 text-[10px] font-semibold uppercase">{(name.split('.').pop() || 'file').slice(0, 4)}</span>;
}

function AttachmentChips({ items, onRemove, onRetry, disabled }) {
  if (!items.length) return null;
  return (
    <div className="flex w-full flex-wrap gap-2 pb-1" aria-label="待送出的附件" aria-live="polite">
      {items.map(a => (
        <div key={a.id} className={`relative flex items-center gap-2 rounded-lg border p-2 pr-7 max-w-full sm:max-w-[18rem] ${a.status === 'error' ? 'border-red-400/50 bg-red-950/20' : 'border-white/10 bg-white/5'}`}>
          {a.previewUrl ? <img src={a.previewUrl} alt="" className="h-10 w-10 shrink-0 rounded-md object-cover" /> : <AttachmentFileIcon name={a.name} />}
          <div className="min-w-0 text-xs">
            <div className="truncate text-gray-200" title={a.name}>{a.name}</div>
            <div className={`mt-1 text-[11px] ${a.status === 'error' ? 'text-red-300' : 'text-gray-400'}`}>
              {attachmentSize(a.size)} · {a.status === 'uploading' ? <span role="status">上傳中…</span> : a.status === 'error' ? a.error || '上傳失敗' : '已就緒'}
            </div>
            {a.status === 'error' && <button type="button" disabled={disabled} onClick={() => onRetry(a.id)} className="mt-1 text-violet-300 hover:underline disabled:opacity-40">重試</button>}
          </div>
          <button type="button" disabled={disabled} onClick={() => onRemove(a.id)} aria-label={`移除附件 ${a.name}`} className="absolute right-0 top-0 flex h-7 w-7 items-center justify-center rounded-md text-gray-400 hover:text-red-300 disabled:opacity-40">×</button>
        </div>
      ))}
    </div>
  );
}

// All authenticated image reads share a small queue, including historical images.
const attachmentReads = { active: 0, waiting: [] };
function readAttachmentBlob(url, signal) {
  return new Promise((resolve, reject) => {
    const job = { run: null };
    const abort = () => {
      const index = attachmentReads.waiting.indexOf(job);
      if (index >= 0) { attachmentReads.waiting.splice(index, 1); reject(new DOMException('Aborted', 'AbortError')); }
    };
    job.run = async () => {
      signal?.removeEventListener('abort', abort);
      attachmentReads.active += 1;
      try {
        if (signal?.aborted) throw new DOMException('Aborted', 'AbortError');
        const res = await apiFetch(url, { signal, timeoutMs: 120000 });
        if (!res.ok) throw new Error('附件無法讀取');
        resolve(await res.blob());
      } catch (err) { reject(err); }
      finally {
        attachmentReads.active -= 1;
        const next = attachmentReads.waiting.shift();
        if (next) next.run();
      }
    };
    if (signal?.aborted) { reject(new DOMException('Aborted', 'AbortError')); return; }
    if (attachmentReads.active < 3) job.run();
    else { attachmentReads.waiting.push(job); signal?.addEventListener('abort', abort, { once: true }); }
  });
}

function MessageAttachmentCard({ attachment: a, onPreview }) {
  const cardRef = useRef(null);
  const [visible, setVisible] = useState(false);
  const [preview, setPreview] = useState('');
  const [error, setError] = useState('');
  const [retry, setRetry] = useState(0);
  const [busy, setBusy] = useState(false);
  const [details, setDetails] = useState(null);
  const actionControllerRef = useRef(null);
  const isImage = String(a.mime_type || '').startsWith('image/');

  useEffect(() => {
    if (!window.IntersectionObserver) { setVisible(true); return; }
    const observer = new IntersectionObserver(entries => setVisible(entries[0].isIntersecting), { rootMargin: '200px' });
    if (cardRef.current) observer.observe(cardRef.current);
    return () => observer.disconnect();
  }, []);

  useEffect(() => {
    if (!visible || !isImage || !a.url) return;
    const controller = new AbortController();
    let objectUrl = '';
    setError('');
    readAttachmentBlob(a.url, controller.signal).then(blob => {
      if (controller.signal.aborted) return;
      objectUrl = URL.createObjectURL(blob);
      setPreview(objectUrl);
    }).catch(err => { if (err.name !== 'AbortError') setError('附件無法讀取'); });
    return () => { controller.abort(); if (objectUrl) URL.revokeObjectURL(objectUrl); setPreview(''); };
  }, [visible, isImage, a.url, retry]);

  useEffect(() => () => actionControllerRef.current?.abort(), []);

  const download = async () => {
    if (busy) return;
    setBusy(true);
    setError('');
    const controller = new AbortController();
    actionControllerRef.current = controller;
    try {
      const blob = await readAttachmentBlob(`${a.url}?download=1`, controller.signal);
      if (controller.signal.aborted) return;
      const url = URL.createObjectURL(blob);
      const link = document.createElement('a');
      link.href = url; link.download = a.name; document.body.appendChild(link); link.click(); link.remove();
      setTimeout(() => URL.revokeObjectURL(url), 1000);
    } catch (err) { if (!controller.signal.aborted) setError('附件無法讀取'); }
    finally { if (!controller.signal.aborted) setBusy(false); }
  };

  const showDetails = async () => {
    if (details) { setDetails(null); return; }
    if (a.legacy) { setDetails(a.path); return; }
    if (busy) return;
    setBusy(true);
    const controller = new AbortController();
    actionControllerRef.current = controller;
    try {
      const res = await apiFetch(`${a.url}/details`, { signal: controller.signal });
      if (!res.ok) throw new Error('附件無法讀取');
      const data = await res.json();
      if (!controller.signal.aborted) setDetails(data.path);
    } catch (err) { if (!controller.signal.aborted) setError('附件無法讀取'); }
    finally { if (!controller.signal.aborted) setBusy(false); }
  };

  return (
    <div ref={cardRef} className="rounded-lg border border-white/20 bg-black/10 p-2 min-w-0 max-w-full" onPointerDown={e => e.stopPropagation()} onTouchStart={e => e.stopPropagation()} onClick={e => e.stopPropagation()} onContextMenu={e => e.stopPropagation()}>
      {isImage && (
        <button type="button" onClick={() => preview && onPreview(preview)} disabled={!preview} aria-label={`放大 ${a.name}`} className="mb-2 block w-full text-left disabled:cursor-default">
          {preview ? <img src={preview} alt={a.name} onError={() => setError('圖片無法預覽，仍可嘗試下載')} className="max-h-48 max-w-full rounded-md object-contain" /> : <div className="flex h-24 items-center justify-center rounded-md bg-white/5 text-xs text-white/70">{error || '載入圖片…'}</div>}
        </button>
      )}
      <div className="flex items-center gap-2 min-w-0">
        {!isImage && <AttachmentFileIcon name={a.name} />}
        <div className="min-w-0 flex-1">
          <div className="truncate text-xs font-medium" title={a.name}>{a.name}</div>
          <div className="mt-0.5 text-[11px] text-white/70">{a.legacy ? '舊附件 · 原始檔名未保存' : attachmentSize(a.size)}</div>
        </div>
      </div>
      <div className="mt-2 flex flex-wrap gap-3 text-[11px]">
        {!a.legacy && <button type="button" onClick={download} disabled={busy} className="hover:underline disabled:opacity-40">{busy ? '讀取中…' : '下載'}</button>}
        {!isGuestMode && <button type="button" onClick={showDetails} disabled={busy} className="text-white/70 hover:underline disabled:opacity-40">{details ? '收起詳細資訊' : '附件詳細資訊'}</button>}
        {error && isImage && <button type="button" onClick={() => { setError(''); setRetry(n => n + 1); }} className="hover:underline">重試預覽</button>}
      </div>
      {error && <div role="status" className="mt-1 text-[11px] text-red-200">{error}</div>}
      {details && <div className="mt-2 break-all text-[11px] text-white/70 select-text">{details}</div>}
    </div>
  );
}

function MessageAttachments({ items, onPreview }) {
  if (!items.length) return null;
  return <div className="mb-2 grid gap-2 min-w-0" aria-label="訊息附件">{items.map(a => <MessageAttachmentCard key={a.id} attachment={a} onPreview={onPreview} />)}</div>;
}
