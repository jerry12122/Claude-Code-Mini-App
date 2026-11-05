function useChatComposer(sessionId, submitInput) {
  const [input, setInputState] = useState('');
  const inputRef = useRef('');
  const setInput = useCallback(update => {
    const text = typeof update === 'function' ? update(inputRef.current) : update;
    inputRef.current = text;
    setInputState(text);
    composerDrafts.set(sessionId, { ...composerDrafts.get(sessionId), text });
    try { localStorage.setItem(draftInputStorageKey(sessionId), text); } catch (_) {}
  }, [sessionId]);
  const [mentionChips, setMentionChipsState] = useState([]);
  const mentionChipsRef = useRef([]);
  const setMentionChips = useCallback(update => {
    const mentions = typeof update === 'function' ? update(mentionChipsRef.current) : update;
    mentionChipsRef.current = mentions;
    setMentionChipsState(mentions);
    composerDrafts.set(sessionId, { ...composerDrafts.get(sessionId), mentions });
  }, [sessionId]);
  const [attachments, setAttachments] = useState([]);
  const attachmentItemsRef = useRef([]);
  const uploadControllersRef = useRef(new Map());
  const composerSessionRef = useRef(sessionId);
  composerSessionRef.current = sessionId;
  const pendingSendRef = useRef(null);
  const updateAttachments = useCallback((update) => {
    const next = update(attachmentItemsRef.current);
    attachmentItemsRef.current = next;
    setAttachments(next);
    composerDrafts.set(sessionId, { ...composerDrafts.get(sessionId), attachments: next });
  }, [sessionId]);
  const [draftRestoring, setDraftRestoring] = useState(true);
  const [draftSaving, setDraftSaving] = useState(false);
  const [draftSaveError, setDraftSaveError] = useState('');
  const [draftRestoreFailed, setDraftRestoreFailed] = useState(false);
  const retryDraftFilesRef = useRef(() => {});
  const filesReadyRef = useRef(false);
  const filesSaveGenerationRef = useRef(0);
  const saveFiles = async () => {
    const target = sessionId;
    const generation = ++filesSaveGenerationRef.current;
    setDraftSaving(true);
    try {
      await writeComposerFiles(draftInputStorageKey(target), attachmentItemsRef.current);
      if (composerSessionRef.current === target && generation === filesSaveGenerationRef.current) setDraftSaveError('');
    } catch (_) {
      if (composerSessionRef.current === target && generation === filesSaveGenerationRef.current) setDraftSaveError('附件草稿尚未存妥，請勿重整或關閉此頁');
    } finally {
      if (composerSessionRef.current === target && generation === filesSaveGenerationRef.current) setDraftSaving(false);
    }
  };
  useEffect(() => {
    if (filesReadyRef.current && !draftRestoring) saveFiles();
  }, [attachments, draftRestoring]);
  useEffect(() => {
    if (!draftSaving) return undefined;
    const warn = e => { e.preventDefault(); e.returnValue = ''; };
    window.addEventListener('beforeunload', warn);
    return () => window.removeEventListener('beforeunload', warn);
  }, [draftSaving]);
  const [pendingInput, setPendingInput] = useState(null);
  const [sending, setSending] = useState(false);

  useEffect(() => {
    recoverPendingInput(sessionId);
    const saved = composerDrafts.get(sessionId);
    let text = saved?.text || '';
    try { text = localStorage.getItem(draftInputStorageKey(sessionId)) ?? text; } catch (_) {}
    setInput(text);
    setMentionChips(saved?.mentions || []);
    updateAttachments(() => saved?.attachments || []);
    let cancelled = false;
    filesReadyRef.current = false;
    setDraftRestoring(true);
    setDraftSaving(false);
    setDraftRestoreFailed(false);
    setDraftSaveError('');
    ++filesSaveGenerationRef.current;
    const restoreFiles = async () => {
      if (cancelled) return;
      setDraftRestoring(true);
      try {
        const stored = saved?.filesRestored ? [] : await readComposerFiles(draftInputStorageKey(sessionId));
        if (cancelled) return;
        const current = attachmentItemsRef.current;
        const existing = new Set(current.map(a => a.id));
        const pending = composerDrafts.get(sessionId)?.pending;
        const outgoing = new Set(pending?.attachmentIds || []);
        updateAttachments(() => [...current, ...restoreComposerFiles(stored.filter(a => !existing.has(a.id) && !outgoing.has(a.attachmentId)))]);
        composerDrafts.get(sessionId).filesRestored = true;
        filesReadyRef.current = true;
        setDraftRestoreFailed(false);
        setDraftSaveError('');
      } catch (_) {
        if (!cancelled) {
          setDraftRestoreFailed(true);
          setDraftSaveError('無法還原附件草稿，請重試；目前附件尚未備份');
        }
      } finally {
        if (!cancelled) setDraftRestoring(false);
      }
    };
    retryDraftFilesRef.current = restoreFiles;
    restoreFiles();
    const syncPending = (e) => {
      if (e && e.detail?.sessionId !== sessionId) return;
      const pending = composerDrafts.get(sessionId)?.pending || null;
      setPendingInput(pending);
      setSending(!!pending?.inFlight);
    };
    const onRestored = (e) => {
      if (e.detail?.sessionId !== sessionId) return;
      const draft = composerDrafts.get(sessionId);
      setInput(draft?.text || '');
      setMentionChips(draft?.mentions || []);
      updateAttachments(() => draft?.attachments || []);
    };
    pendingSendRef.current = null;
    syncPending();
    window.addEventListener('miniapp:draft-restored', onRestored);
    window.addEventListener('miniapp:pending-input', syncPending);
    return () => {
      cancelled = true;
      filesReadyRef.current = false;
      window.removeEventListener('miniapp:draft-restored', onRestored);
      window.removeEventListener('miniapp:pending-input', syncPending);
      uploadControllersRef.current.forEach(controller => controller.abort());
      uploadControllersRef.current.clear();
      composerDrafts.set(sessionId, {
        ...composerDrafts.get(sessionId), text: inputRef.current,
        attachments: attachmentItemsRef.current.map(a => a.status === 'uploading' ? { ...a, status: 'error', error: '上傳已暫停，請重試' } : a),
        mentions: mentionChipsRef.current,
      });
    };
  }, [sessionId]);

  useEffect(() => () => { composerSessionRef.current = null; }, []);

  const sendComposerInput = async (record) => {
    if (record.inFlight || pendingSendRef.current) return;
    pendingSendRef.current = record;
    try {
      await sendPendingInput(sessionId, record, submitInput);
    } catch (err) {
      if (composerSessionRef.current === sessionId) showToast(err.message || '送出未確認，內容已保留', { error: true, duration: 5000 });
    } finally {
      if (composerSessionRef.current === sessionId && pendingSendRef.current === record) pendingSendRef.current = null;
    }
  };

  return {
    input, setInput, mentionChips, setMentionChips,
    attachments, updateAttachments, attachmentItemsRef, uploadControllersRef,
    composerSessionRef, pendingSendRef, pendingInput, sending, sendComposerInput,
    draftRestoring, draftSaving, draftSaveError, draftRestoreFailed, saveFiles,
    retryDraftFiles: () => retryDraftFilesRef.current(),
  };
}
