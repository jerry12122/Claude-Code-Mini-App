function useChatComposer(sessionId, submitInput) {
  const [input, setInputState] = useState('');
  const inputRef = useRef('');
  const setInput = useCallback(update => {
    const text = typeof update === 'function' ? update(inputRef.current) : update;
    inputRef.current = text;
    setInputState(text);
    composerDrafts.set(sessionId, { ...composerDrafts.get(sessionId), text });
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
  };
}
