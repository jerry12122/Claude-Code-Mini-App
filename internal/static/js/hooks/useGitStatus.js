/** 標題列 Git 資料與固定操作，沒有背景輪詢。null sessionId 用於訪客。 */
function useGitStatus(sessionId, state) {
  const [status, setStatus] = useState(null);
  const [branches, setBranches] = useState([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [action, setAction] = useState('');
  const [result, setResult] = useState('');
  const request = useRef(0);
  const activeAction = useRef(null);
  const lastState = useRef({ sessionId, state });

  const reload = useCallback(async () => {
    if (!sessionId) { setLoading(false); return; }
    const version = ++request.current;
    setLoading(true);
    setError('');
    const read = async kind => {
      const res = await apiFetch(`/sessions/${encodeURIComponent(sessionId)}/git/${kind}`, { timeoutMs: 45000 });
      const data = await res.json();
      if (!res.ok) throw new Error(data.error || `Git 查詢失敗 (${res.status})`);
      return data;
    };
    try {
      const next = await read('status');
      if (version !== request.current) return;
      setStatus(next);
      if (next.is_repo) {
        const local = await read('branches');
        if (version !== request.current) return;
        setBranches(local.branches || []);
      } else {
        setBranches([]);
      }
    } catch (e) {
      if (version === request.current) setError(e.message || '無法取得 Git 狀態');
    } finally {
      if (version === request.current) setLoading(false);
    }
  }, [sessionId]);

  useEffect(() => {
    const previous = lastState.current;
    lastState.current = { sessionId, state };
    if (sessionId && previous.sessionId === sessionId && state === 'IDLE' && previous.state && previous.state !== 'IDLE') reload();
  }, [sessionId, state, reload]);

  const run = async (kind, branch) => {
    if (activeAction.current) return;
    const token = {};
    activeAction.current = token;
    setAction(kind);
    setError('');
    setResult('');
    try {
      const res = await apiFetch(`/sessions/${encodeURIComponent(sessionId)}/git/${kind}`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        ...(kind === 'checkout' ? { body: JSON.stringify({ branch }) } : {}),
        // ponytail: 給伺服器的前置查詢、寫入期限與收尾留足時間，不另做重試。
        timeoutMs: kind === 'checkout' ? 50000 : 125000,
      });
      const data = await res.json();
      if (activeAction.current !== token) return;
      if (!res.ok) throw new Error(data.error || `Git 操作失敗 (${res.status})`);
      setResult(data.output || 'Git 操作完成');
      await reload();
    } catch (e) {
      if (activeAction.current === token) setError(e.message || 'Git 操作失敗');
    } finally {
      if (activeAction.current === token) {
        activeAction.current = null;
        setAction('');
      }
    }
  };

  useEffect(() => {
    setStatus(null);
    setBranches([]);
    setAction('');
    setResult('');
    reload();
    return () => { request.current++; activeAction.current = null; };
  }, [reload]);

  return { status, branches, loading, error, action, result, reload, run };
}
