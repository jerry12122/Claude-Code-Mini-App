// ── 檔案瀏覽對話框的純邏輯（無 JSX、無 React）──────────────────────────────
// 路徑一律是相對於 session work_dir 的 `/` 分隔字串，空字串代表根。驗證與安全邊界都在後端（internal/fsbrowse），
// 這裡只負責組字串與顯示換算，不嘗試判斷路徑是否安全。

function fileBrowserChild(dir, name) {
  return dir ? `${dir}/${name}` : name;
}

function fileBrowserParent(p) {
  const i = p.lastIndexOf('/');
  return i < 0 ? '' : p.slice(0, i);
}

function fileBrowserCrumbs(p) {
  const out = [{ label: '根目錄', path: '' }];
  let acc = '';
  for (const seg of p.split('/').filter(Boolean)) {
    acc = fileBrowserChild(acc, seg);
    out.push({ label: seg, path: acc });
  }
  return out;
}

function fileBrowserSize(n) {
  if (typeof n !== 'number' || !isFinite(n)) return '';
  if (n < 1024) return `${n} B`;
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`;
  return `${(n / 1024 / 1024).toFixed(1)} MB`;
}

/** kind：'list'（列目錄）或 'content'（讀文字檔）。 */
function fileBrowserUrl(sessionId, kind, p) {
  return `/sessions/${encodeURIComponent(sessionId)}/files${kind === 'content' ? '/content' : ''}?path=${encodeURIComponent(p)}`;
}
