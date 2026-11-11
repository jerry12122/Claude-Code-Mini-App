// ── 桌面自我更新：狀態訂閱與 bridge 呼叫 ─────────────────────────────────────
// 只有桌面端（WebView2 注入 window.__desktopUpdater）才會有 bridge；
// 瀏覽器只查詢公開版本與通知；不具備下載安裝或重啟的 bridge。
// 依賴 core.js 的 React hook 別名（useState、useEffect、useRef、useCallback）。

/** 目前頁面是否已有桌面更新 bridge。 */
function desktopUpdateBridgeReady() {
  const b = typeof window !== 'undefined' ? window.__desktopUpdater : null;
  return !!(b && typeof b.request === 'function');
}

function newerWebRelease(release, current) {
  if (!release || release.draft || release.prerelease) return '';
  const version = String(release.tag_name || '').replace(/^v/, '');
  const valid = v => /^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)$/.test(v) && v.length <= 64;
  if (!valid(version) || !valid(current)) return '';
  const a = version.split('.').map(BigInt), b = current.split('.').map(BigInt);
  for (let i = 0; i < 3; i++) { if (a[i] !== b[i]) return a[i] > b[i] ? version : ''; }
  return '';
}

// Browser checks are read-only and send neither application credentials nor URLs
// from release metadata. Storage failure falls back to this page's memory.
function startWebUpdateChecks({ onStatus, onNotice }) {
  const hour = 3600000, cacheKey = 'cc_web_release_cache', noticeKey = 'cc_web_release_notice';
  const read = key => { try { return JSON.parse(localStorage.getItem(key) || 'null'); } catch { return null; } };
  const save = (key, value) => { try { localStorage.setItem(key, JSON.stringify(value)); } catch {} };
  let stopped = false, checking = false, lastCheck = -Infinity, firstBuild = '', status = null;
  let cache = read(cacheKey) || {}, lastNotice = read(noticeKey) || {};
  const controller = new AbortController();
  const notify = () => {
    if (stopped || document.hidden || desktopUpdateBridgeReady() || !status) return;
    const version = status.reloadRequired ? 'reload:' + status.currentVersion + ':' + status.currentSha : status.availableVersion;
    if (!version) return;
    lastNotice = read(noticeKey) || lastNotice;
    if (lastNotice.version === version && Date.now() - lastNotice.at < 24 * hour) return;
    lastNotice = {version, at:Date.now()}; save(noticeKey, lastNotice);
    onNotice(status);
  };
  const check = async () => {
    if (stopped || checking || document.hidden || desktopUpdateBridgeReady()) return;
    if (Date.now() - lastCheck < hour) { notify(); return; }
    lastCheck = Date.now(); checking = true;
    try {
      const cfgRes = await apiFetch('/config', {signal:controller.signal,cache:'no-store'});
      if (!cfgRes.ok) return;
      const cfg = await cfgRes.json();
      const build = cfg.version + ':' + cfg.sha;
      if (!firstBuild) firstBuild = build;
      cache = read(cacheKey) || cache;
      if (!(cache.nextCheck > Date.now())) {
        try {
          const res = await fetchWithTimeout('https://api.github.com/repos/jerry12122/Claude-Code-Mini-App/releases/latest', {
            signal:controller.signal, credentials:'omit', referrerPolicy:'no-referrer',
          });
          if (res.ok) {
            cache = {release:await res.json(), nextCheck:Date.now()+hour};
          } else {
            const seconds = Number(res.headers.get('Retry-After'));
            const reset = Number(res.headers.get('X-RateLimit-Reset')) * 1000;
            cache.nextCheck = Math.max(Date.now()+hour, Number.isFinite(seconds) ? Date.now()+seconds*1000 : 0, Number.isFinite(reset) ? reset : 0);
          }
        } catch { cache.nextCheck = Date.now()+hour; }
        // Retain only metadata needed for comparison; never render remote HTML.
        if (cache.release) cache.release = {tag_name:cache.release.tag_name, draft:cache.release.draft, prerelease:cache.release.prerelease};
        save(cacheKey, cache);
      }
      if (stopped || desktopUpdateBridgeReady()) return;
      status = {currentVersion:cfg.version, currentSha:cfg.sha, availableVersion:newerWebRelease(cache.release,cfg.version), reloadRequired:firstBuild !== build};
      onStatus(status); notify();
    } catch { /* Background failure must not interrupt chat or erase the last known version. */ }
    finally { checking = false; }
  };
  const visible = () => { if (!document.hidden) { notify(); check(); } };
  const timer = setInterval(check, hour);
  document.addEventListener('visibilitychange', visible);
  window.addEventListener('focus', visible);
  check();
  return () => {
    stopped = true; controller.abort(); clearInterval(timer);
    document.removeEventListener('visibilitychange',visible); window.removeEventListener('focus',visible);
  };
}

/**
 * 訂閱 Go 端推送的狀態快照，並提供 request(action)。
 * - desktop-update-state：快照（detail 為 Status JSON）。
 * - desktop-update-bridge：bridge 剛注入（導航完成後），此時要求一次 status。
 * - desktop-update-open：托盤「檢查更新」要求開啟設定的更新區（onOpen 由 App 提供）。
 * laterVersion 只存在前端記憶體中，不寫回 Go 端。
 */
function useDesktopUpdate({ onOpen, enabled = false } = {}) {
  const [status, setStatus] = useState(null);
  const [bridge, setBridge] = useState(desktopUpdateBridgeReady);
  const [laterVersion, setLaterVersion] = useState('');
  const [openNonce, setOpenNonce] = useState(0);
  const [webStatus, setWebStatus] = useState(null);
  const onOpenRef = useRef(onOpen);
  onOpenRef.current = onOpen;

  const openUpdate = useCallback(() => {
    setOpenNonce(n => n + 1);
    onOpenRef.current?.();
  }, []);

  useEffect(() => {
    if (!enabled || bridge || isGuestMode) return;
    return startWebUpdateChecks({onStatus:setWebStatus,onNotice:st => {
      showToast(st.reloadRequired ? '主機已更新，可重新整理載入新版' : `有新版本 v${st.availableVersion}，請由主機管理者更新`, {
        duration:10000,action:{label:'查看更新',onClick:openUpdate},
      });
    }});
  }, [enabled, bridge, openUpdate]);

  useEffect(() => {
    const onState = (e) => {
      if (e && e.detail) setStatus(e.detail);
    };
    const onBridge = () => {
      setBridge(true);
      window.__desktopUpdater.request('status');
    };
    const onOpenEvent = () => {
      setOpenNonce((n) => n + 1);
      if (onOpenRef.current) onOpenRef.current();
    };
    window.addEventListener('desktop-update-state', onState);
    window.addEventListener('desktop-update-bridge', onBridge);
    window.addEventListener('desktop-update-open', onOpenEvent);
    if (desktopUpdateBridgeReady()) onBridge();
    return () => {
      window.removeEventListener('desktop-update-state', onState);
      window.removeEventListener('desktop-update-bridge', onBridge);
      window.removeEventListener('desktop-update-open', onOpenEvent);
    };
  }, []);

  // 只轉送白名單內的 action；bridge 不存在時回傳 false，不做任何事。
  const request = useCallback((action) => {
    if (!desktopUpdateBridgeReady()) return false;
    return window.__desktopUpdater.request(action);
  }, []);

  const later = useCallback((version) => setLaterVersion(version || ''), []);
  const showOffer = useCallback(() => setLaterVersion(''), []);

  const hasUpdate = bridge ? !!status?.availableVersion : !!(webStatus?.availableVersion || webStatus?.reloadRequired);
  return { bridge, status, request, laterVersion, later, showOffer, openNonce, openUpdate, hasUpdate, webStatus };
}
