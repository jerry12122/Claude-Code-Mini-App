// ── 桌面自我更新：純函式（無 React 依賴）────────────────────────────────────
// 狀態由 Go 端以 desktop-update-state 事件推送（見 internal/desktopupdate/script.go）。
// 這裡只把「快照 → 顯示文字／按鈕模型」轉成資料，實際渲染在 DesktopUpdateSection.js。
// 測試：scripts/desktop-update.test.cjs（node --test scripts/*.test.cjs）。
//
// 安全邊界：動作 id 只會是 check / download / later / showOffer / restart，
// 沒有任何由呼叫者指定 URL 或路徑的動作。restart 只在 ready 時出現，且必須經使用者二次確認（見 DesktopUpdateSection.js）。

/** 狀態 → 中文標籤。 */
function desktopUpdateStateLabel(state) {
  switch (state) {
    case 'idle': return '尚未檢查';
    case 'checking': return '檢查中…';
    case 'up-to-date': return '已是最新版本';
    case 'available': return '有可用更新';
    case 'downloading': return '下載中';
    case 'verifying': return '驗證中';
    case 'ready': return '已下載，等待套用';
    case 'restarting': return '重新啟動中…';
    case 'error': return '更新失敗';
    default: return '未知狀態';
  }
}

/** 錯誤碼 → 人讀得懂的說明；未知碼回退為通用文字。 */
function desktopUpdateErrorText(code) {
  switch (code) {
    case 'check_failed': return '無法取得更新資訊，請稍後重試';
    case 'asset_missing': return '此正式版沒有可用的桌面程式檔';
    case 'checksum_missing': return '此正式版缺少校驗檔，已拒絕更新';
    case 'checksum_mismatch': return '下載檔案校驗不符，已拒絕安裝';
    case 'download_failed': return '下載失敗，可重試';
    case 'install_not_writable': return '安裝目錄不可寫入，請改用手動方式更新';
    case 'update_busy': return '仍有進行中或待處理的工作，暫不能重新啟動更新；請依下方會話明細處理後重試（已下載的更新會保留）';
    case 'update_in_progress': return '目前有更新程序進行中，請稍後再試';
    case 'update_check_failed': return '無法確認是否仍有進行中的工作，已取消重新啟動（已下載的更新仍保留）';
    case 'helper_failed': return '套用更新的程序失敗，已保留已下載的更新，可再試一次';
    default: return '更新失敗';
  }
}

/** 忙碌原因分類 → 人讀得懂的說明（Go 端 ws 的 BusyError.Reason）；未知回傳空字串。 */
function desktopUpdateBusyReasonText(reason) {
  switch (reason) {
    case 'running_task': return '有任務執行中或仍在收尾';
    case 'shell_task': return '有終端機指令執行中';
    case 'pending_confirm': return '有等待授權或確認的操作';
    case 'queued': return '有排隊中的訊息（包含暫停佇列）';
    case 'maintenance': return '另一個更新程序進行中';
    default: return '';
  }
}

/** 下載進度百分比（0–100）；總量未知或為 0 時回 0。 */
function desktopUpdateProgressPercent(downloaded, total) {
  const d = Number(downloaded) || 0;
  const t = Number(total) || 0;
  if (t <= 0 || d <= 0) return 0;
  return Math.min(100, Math.floor((d / t) * 100));
}

/** SHA 顯示用的短碼（與設定頁版號旁一致）。 */
function desktopUpdateShortSha(sha) {
  return sha ? String(sha).slice(0, 7) : '—';
}

/** 狀態是否屬於忙碌中（此時不可再要求檢查或下載）。 */
function desktopUpdateIsBusy(state) {
  return state === 'checking' || state === 'downloading' || state === 'verifying' || state === 'ready' || state === 'restarting';
}

/**
 * 依快照與「已延後的版本」決定顯示內容與可用動作。
 * 回傳 { stateLabel, busy, deferred, offerVisible, showNotes, actions }；
 * actions 每項為 { id, label, disabled }。
 * laterVersion 等於目前可用版本時視為已延後（只影響顯示，不改後端狀態）。
 */
function desktopUpdateView(status, laterVersion) {
  const st = (status && status.state) || 'idle';
  const avail = (status && status.availableVersion) || '';
  const busy = desktopUpdateIsBusy(st);
  const deferred = st === 'available' && avail !== '' && laterVersion === avail;
  const offerVisible = st === 'available' && !deferred;
  const actions = [];

  if (offerVisible) {
    actions.push({ id: 'download', label: '下載更新', disabled: false });
    actions.push({ id: 'later', label: '稍後', disabled: false });
  }
  if (deferred) {
    actions.push({ id: 'showOffer', label: '顯示更新', disabled: false });
  }
  if (st === 'error') {
    // 下載失敗時保留了可用版本 → 重試下載；檢查失敗時沒有版本 → 重試檢查。
    if (avail) {
      actions.push({ id: 'download', label: '重試', disabled: false });
    } else {
      actions.push({ id: 'check', label: '重試', disabled: false });
    }
  }
  if (st === 'ready') {
    // 只在 ready 出現且可點；實際送出前由 DesktopUpdateSection 要求二次確認。
    actions.push({ id: 'restart', label: '重新啟動並更新', disabled: false });
  }
  if (!(st === 'error' && !avail)) {
    actions.push({
      id: 'check',
      label: st === 'checking' ? '檢查中…' : '檢查更新',
      disabled: busy,
    });
  }

  return {
    stateLabel: desktopUpdateStateLabel(st),
    busy,
    deferred,
    offerVisible,
    showNotes: avail !== '' && !deferred,
    actions,
  };
}

// ── 更新說明（release notes）渲染 ───────────────────────────────────────────
// 更新視窗是有桌面 bridge 的特權頁面，所以不能用 marked 這類「原始 HTML 直通」的渲染器。
// 這裡自己做一個最小的 Markdown 子集：使用者文字一律先逐字跳脫，標籤只由程式產生
// （白名單：h3 h4 h5 p ul ol li strong em code pre blockquote hr，屬性只有我們自己的 class），
// 連結與圖片只留文字（桌面視窗點連結會直接導覽離開首頁）。測試：scripts/desktop-update.test.cjs。

/** 取第一個「## 更新內容」後、下載章節前的 changelog，相容舊版下載在前的格式；沒有標記就用整段。 */
function desktopUpdateNotesBody(raw) {
  const text = typeof raw === 'string' ? raw.replace(/\r\n?/g, '\n') : '';
  const m = /^## 更新內容[ \t]*$/m.exec(text);
  const body = m ? text.slice(m.index + m[0].length).split(/^## 下載[ \t]*$/m)[0] : text;
  return body.trim();
}

function desktopUpdateEscapeHtml(s) {
  return String(s)
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/'/g, '&#39;');
}

/** 行內：`code`、**粗體**、*斜體*、[文字](網址)→只留文字、![替代文字](網址)→只留替代文字。 */
function desktopUpdateInline(line) {
  const parts = String(line).split('`');
  let out = '';
  for (let i = 0; i < parts.length; i++) {
    const isCode = i % 2 === 1 && i < parts.length - 1; // 沒有成對的反引號就當一般文字
    if (isCode) {
      out += '<code class="ra-notes-code">' + desktopUpdateEscapeHtml(parts[i]) + '</code>';
      continue;
    }
    let t = desktopUpdateEscapeHtml(parts[i]);
    if (i % 2 === 1) t = '`' + t; // 落單的反引號
    t = t
      .replace(/!?\[([^\]]*)\]\([^)]*\)/g, '$1')
      .replace(/\*\*([^*\n]+?)\*\*/g, '<strong>$1</strong>')
      .replace(/(^|[^*\w])\*([^*\s][^*\n]*?)\*(?![*\w])/g, '$1<em>$2</em>');
    out += t;
  }
  return out;
}

/** 更新說明 → 安全的 HTML 字串（給 dangerouslySetInnerHTML 用；空輸入回空字串）。 */
function desktopUpdateRenderNotes(raw) {
  const text = typeof raw === 'string' ? raw.replace(/\r\n?/g, '\n') : '';
  if (text.trim() === '') return '';
  const lines = text.split('\n');
  const out = [];
  let para = [];
  let list = null; // { tag: 'ul' | 'ol', items: [] }
  let quote = [];
  const flushPara = () => {
    if (para.length) out.push('<p class="ra-notes-p">' + para.map(desktopUpdateInline).join(' ') + '</p>');
    para = [];
  };
  const flushList = () => {
    if (list) {
      out.push('<' + list.tag + ' class="ra-notes-' + list.tag + '">' + list.items.map((x) => '<li>' + desktopUpdateInline(x) + '</li>').join('') + '</' + list.tag + '>');
    }
    list = null;
  };
  const flushQuote = () => {
    if (quote.length) out.push('<blockquote class="ra-notes-bq">' + quote.map(desktopUpdateInline).join(' ') + '</blockquote>');
    quote = [];
  };
  const flushAll = () => { flushPara(); flushList(); flushQuote(); };

  for (let i = 0; i < lines.length; i++) {
    const line = lines[i];
    const fence = /^\s*```/.exec(line);
    if (fence) {
      flushAll();
      const code = [];
      i++;
      while (i < lines.length && !/^\s*```/.test(lines[i])) { code.push(lines[i]); i++; }
      out.push('<pre class="ra-notes-pre"><code>' + desktopUpdateEscapeHtml(code.join('\n')) + '</code></pre>');
      continue;
    }
    if (line.trim() === '') { flushAll(); continue; }
    const heading = /^(#{1,6})\s+(.*)$/.exec(line);
    if (heading) {
      flushAll();
      const level = heading[1].length;
      const tag = level <= 2 ? 'h3' : level === 3 ? 'h4' : 'h5';
      out.push('<' + tag + ' class="ra-notes-' + tag + '">' + desktopUpdateInline(heading[2].trim()) + '</' + tag + '>');
      continue;
    }
    if (/^\s*([-*_])(\s*\1){2,}\s*$/.test(line)) { flushAll(); out.push('<hr class="ra-notes-hr">'); continue; }
    const q = /^\s*>\s?(.*)$/.exec(line);
    if (q) { flushPara(); flushList(); quote.push(q[1]); continue; }
    const ul = /^\s*[-*+]\s+(.*)$/.exec(line);
    const ol = /^\s*\d+[.)]\s+(.*)$/.exec(line);
    if (ul || ol) {
      flushPara(); flushQuote();
      const tag = ul ? 'ul' : 'ol';
      if (list && list.tag !== tag) flushList();
      if (!list) list = { tag, items: [] };
      list.items.push((ul || ol)[1]);
      continue;
    }
    flushList(); flushQuote();
    para.push(line.trim());
  }
  flushAll();
  return out.join('');
}
