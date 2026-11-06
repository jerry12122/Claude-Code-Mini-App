const assert = require('node:assert');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');

// 桌面更新的純函式（無 React 依賴）：快照 → 顯示文字／按鈕模型。
const targetFile = path.join(__dirname, '../internal/static/js/ui/desktop-update.js');
const source = fs.readFileSync(targetFile, 'utf8');
const context = vm.createContext({
  window: {},
  console: console,
});
vm.runInContext(source, context);

const {
  desktopUpdateStateLabel,
  desktopUpdateErrorText,
  desktopUpdateBusyReasonText,
  desktopUpdateProgressPercent,
  desktopUpdateView,
  desktopUpdateShortSha,
  desktopUpdateNotesBody,
  desktopUpdateRenderNotes,
} = context;

// vm 上下文的陣列與主 realm 的陣列原型不同，這裡用 Array.from 轉成主 realm 陣列再比較。
function actionIds(view) {
  return Array.from(view.actions, (a) => a.id);
}

function findAction(view, id) {
  return view.actions.find((a) => a.id === id);
}

// 1. 狀態文字：每個 Go 端 State 都有對應文字，未知值不會拋錯。
{
  const states = ['idle', 'checking', 'up-to-date', 'available', 'downloading', 'verifying', 'ready', 'restarting', 'error'];
  for (const s of states) {
    const label = desktopUpdateStateLabel(s);
    assert.ok(label && label !== '未知狀態', `state ${s} should have a label`);
  }
  assert.strictEqual(desktopUpdateStateLabel('no-such-state'), '未知狀態');
}

// 2. 錯誤碼文字：每個設計中的錯誤碼都有人讀得懂的文字；未知碼回退為通用文字。
{
  const codes = [
    'check_failed',
    'asset_missing',
    'checksum_missing',
    'checksum_mismatch',
    'download_failed',
    'install_not_writable',
    'update_busy',
    'update_in_progress',
    'helper_failed',
    'update_check_failed',
  ];
  for (const c of codes) {
    assert.ok(desktopUpdateErrorText(c).length > 0, `error ${c} needs text`);
  }
  assert.strictEqual(desktopUpdateErrorText('mystery'), '更新失敗');
}

// 3. 進度百分比：總量未知或為 0 時為 0，超量時截在 100。
{
  assert.strictEqual(desktopUpdateProgressPercent(0, 0), 0);
  assert.strictEqual(desktopUpdateProgressPercent(50, 0), 0);
  assert.strictEqual(desktopUpdateProgressPercent(50, 200), 25);
  assert.strictEqual(desktopUpdateProgressPercent(200, 200), 100);
  assert.strictEqual(desktopUpdateProgressPercent(300, 200), 100);
  assert.strictEqual(desktopUpdateProgressPercent(undefined, undefined), 0);
}

// 4. SHA 顯示：取前 7 碼，空值顯示破折號（與設定頁版號旁的顯示一致）。
{
  assert.strictEqual(desktopUpdateShortSha('abcdef1234567'), 'abcdef1');
  assert.strictEqual(desktopUpdateShortSha(''), '—');
  assert.strictEqual(desktopUpdateShortSha(undefined), '—');
}

// 5. 狀態 → 動作模型。
{
  const idle = desktopUpdateView({ state: 'idle' }, '');
  assert.deepStrictEqual(actionIds(idle), ['check']);
  assert.strictEqual(findAction(idle, 'check').disabled, false);

  const upToDate = desktopUpdateView({ state: 'up-to-date' }, '');
  assert.deepStrictEqual(actionIds(upToDate), ['check']);

  const available = desktopUpdateView({ state: 'available', availableVersion: '1.0.1' }, '');
  assert.deepStrictEqual(actionIds(available), ['download', 'later', 'check']);
  assert.strictEqual(findAction(available, 'download').disabled, false);
  assert.strictEqual(available.offerVisible, true);
  assert.strictEqual(available.showNotes, true);
}

// 6. 忙碌狀態：下載與驗證中不可再次檢查或下載；ready 時檢查也停用（後端回 ErrBusy）。
{
  for (const state of ['checking', 'downloading', 'verifying', 'ready']) {
    const v = desktopUpdateView({ state, availableVersion: '1.0.1' }, '');
    assert.strictEqual(findAction(v, 'check').disabled, true, `check disabled in ${state}`);
    assert.ok(!actionIds(v).includes('download'), `no download action in ${state}`);
    assert.strictEqual(v.busy, true, `busy in ${state}`);
  }
}

// 7. ready 的重啟：可點（送出前由 DesktopUpdateSection 二次確認）；其他狀態不出現。
{
  const v = desktopUpdateView({ state: 'ready', availableVersion: '1.0.1' }, '');
  const restart = findAction(v, 'restart');
  assert.ok(restart, 'restart action shown in ready');
  assert.strictEqual(restart.disabled, false);
  assert.strictEqual(restart.label, '重新啟動並更新');
  for (const state of ['idle', 'available', 'downloading', 'verifying', 'restarting', 'error', 'up-to-date']) {
    const other = desktopUpdateView({ state, availableVersion: '1.0.1' }, '');
    assert.ok(!actionIds(other).includes('restart'), `no restart action in ${state}`);
  }
}

// 7b. restarting：後端已取得閘門並呼叫 Restart，前端一律忙碌、不可再檢查或下載，文字顯示重新啟動中。
{
  const v = desktopUpdateView({ state: 'restarting', availableVersion: '1.0.1' }, '');
  assert.strictEqual(v.busy, true, 'restarting is busy');
  assert.strictEqual(v.stateLabel, '重新啟動中…');
  assert.strictEqual(findAction(v, 'check').disabled, true, 'check disabled while restarting');
  assert.ok(!actionIds(v).includes('download'), 'no download while restarting');
  assert.ok(!actionIds(v).includes('restart'), 'no second restart while restarting');
}

// 7c. 忙碌拒絕（update_busy）：仍是 ready，重啟可再按；錯誤與原因文字都有內容。
{
  const v = desktopUpdateView({ state: 'ready', availableVersion: '1.0.1', errorCode: 'update_busy', busyReason: 'queued' }, '');
  assert.strictEqual(findAction(v, 'restart').disabled, false, 'restart can be retried after busy refusal');
  assert.ok(desktopUpdateErrorText('update_busy').includes('進行中的工作'), 'update_busy explains busy work');
  assert.ok(desktopUpdateErrorText('update_check_failed').length > 0, 'update_check_failed has text');
  assert.ok(desktopUpdateErrorText('helper_failed').includes('可再試'), 'helper_failed says retry is possible');
  assert.strictEqual(desktopUpdateBusyReasonText('queued'), '有排隊中的訊息');
  assert.strictEqual(desktopUpdateBusyReasonText('running_task'), '有任務執行中或仍在收尾');
  assert.strictEqual(desktopUpdateBusyReasonText('shell_task'), '有終端機指令執行中');
  assert.strictEqual(desktopUpdateBusyReasonText('pending_confirm'), '有等待授權或確認的操作');
  assert.strictEqual(desktopUpdateBusyReasonText('no-such-reason'), '');
  assert.strictEqual(desktopUpdateBusyReasonText(undefined), '');
}

// 8. 錯誤後重試：有可用版本時重試下載，否則重試檢查。
{
  const afterDownload = desktopUpdateView({ state: 'error', availableVersion: '1.0.1', errorCode: 'download_failed' }, '');
  assert.ok(actionIds(afterDownload).includes('download'), 'retry download after download failure');
  assert.strictEqual(findAction(afterDownload, 'download').label, '重試');

  const afterCheck = desktopUpdateView({ state: 'error', availableVersion: '', errorCode: 'check_failed' }, '');
  assert.ok(!actionIds(afterCheck).includes('download'), 'no download after check failure');
  assert.strictEqual(findAction(afterCheck, 'check').label, '重試');
  assert.strictEqual(actionIds(afterCheck).filter((id) => id === 'check').length, 1, 'retry replaces the plain check button');
}

// 9. 稍後：只影響顯示，同一版本延後後不再顯示下載提示，可再次顯示。
{
  const deferred = desktopUpdateView({ state: 'available', availableVersion: '1.0.1' }, '1.0.1');
  assert.strictEqual(deferred.deferred, true);
  assert.strictEqual(deferred.offerVisible, false);
  assert.strictEqual(deferred.showNotes, false);
  assert.ok(!actionIds(deferred).includes('download'), 'deferred offer hides download');
  assert.ok(actionIds(deferred).includes('showOffer'), 'deferred offer can be shown again');

  // 新版本不會被舊的延後版本吃掉。
  const newer = desktopUpdateView({ state: 'available', availableVersion: '1.0.2' }, '1.0.1');
  assert.strictEqual(newer.deferred, false);
  assert.strictEqual(newer.offerVisible, true);
}

// 10. 沒有快照（尚未收到 Go 端狀態）視為 idle，不拋錯。
{
  const v = desktopUpdateView(null, '');
  assert.strictEqual(v.stateLabel, desktopUpdateStateLabel('idle'));
  assert.ok(actionIds(v).includes('check'));
}

// 11. 只有 Go 端定義的動作 id 會出現；絕不出現由呼叫者指定 URL 或路徑的動作。
{
  const states = ['idle', 'checking', 'up-to-date', 'available', 'downloading', 'verifying', 'ready', 'restarting', 'error'];
  const allowed = new Set(['check', 'download', 'later', 'showOffer', 'restart']);
  for (const s of states) {
    for (const v of [desktopUpdateView({ state: s, availableVersion: '1.0.1' }, ''), desktopUpdateView({ state: s, availableVersion: '1.0.1' }, '1.0.1')]) {
      for (const id of actionIds(v)) {
        assert.ok(allowed.has(id), `unexpected action ${id} in ${s}`);
      }
    }
  }
}

// 13. 更新說明：相容更新內容在前與舊版下載在前的格式，只顯示 changelog。
{
  const body = '## 下載\n\n| 平台 | 檔案 |\n|---|---|\n| Windows | a.zip |\n\n---\n\n## 更新內容\n\n### 新增\n- 功能 A\n';
  assert.strictEqual(desktopUpdateNotesBody(body), '### 新增\n- 功能 A');
  const changesFirst = '## 更新內容\n\n### 新增\n- 功能 A\n\n## 下載\n\n下載表\n\n## 桌面版應用程式內更新（Windows）\n更新教學\n';
  assert.strictEqual(desktopUpdateNotesBody(changesFirst), '### 新增\n- 功能 A');
  assert.strictEqual(desktopUpdateNotesBody(changesFirst.replace(/\n/g, '\r\n')), '### 新增\n- 功能 A');
  assert.strictEqual(desktopUpdateNotesBody('### 變更\n- 只有 changelog'), '### 變更\n- 只有 changelog', '沒有標記就用整段');
  assert.strictEqual(desktopUpdateNotesBody(''), '');
  assert.strictEqual(desktopUpdateNotesBody(null), '');
  assert.strictEqual(desktopUpdateNotesBody('  \n '), '');
  // 只取第一個標記；之後出現的同名標題屬於內容的一部分
  assert.strictEqual(desktopUpdateNotesBody('x\n## 更新內容\n- a\n## 更新內容\n- b'), '- a\n## 更新內容\n- b');
}

// 14. Markdown 子集：標題、清單、行內樣式、程式碼區塊、引用、分隔線。
{
  const html = desktopUpdateRenderNotes('### 新增\n- 第一項 **粗體** 與 `code`\n- 第二項 *斜體*\n\n### 變更\n1. 步驟一\n2. 步驟二\n\n一般段落\n同一段第二行\n\n> 引用文字\n\n---\n\n```go\nx := a < b\n```\n');
  assert.ok(/<h4 class="ra-notes-h4">新增<\/h4>/.test(html), 'h3 標題 → h4');
  assert.ok(/<ul class="ra-notes-ul"><li>第一項 <strong>粗體<\/strong> 與 <code class="ra-notes-code">code<\/code><\/li><li>第二項 <em>斜體<\/em><\/li><\/ul>/.test(html), html);
  assert.ok(/<ol class="ra-notes-ol"><li>步驟一<\/li><li>步驟二<\/li><\/ol>/.test(html), '有序清單');
  assert.ok(/<p class="ra-notes-p">一般段落 同一段第二行<\/p>/.test(html), '連續行合成一段');
  assert.ok(/<blockquote class="ra-notes-bq">引用文字<\/blockquote>/.test(html), '引用');
  assert.ok(/<hr class="ra-notes-hr">/.test(html), '分隔線');
  assert.ok(/<pre class="ra-notes-pre"><code>x := a &lt; b<\/code><\/pre>/.test(html), '程式碼區塊內容要跳脫');
  assert.strictEqual(desktopUpdateRenderNotes(''), '');
  assert.strictEqual(desktopUpdateRenderNotes(null), '');
  // 連結只留文字，不產生 <a>／href（桌面視窗點連結會直接導覽離開首頁）；圖片語法同理
  const link = desktopUpdateRenderNotes('看 [說明](https://example.com/x) 與 ![圖](https://example.com/i.png)');
  assert.ok(!/<a[\s>]/i.test(link) && !/href/i.test(link) && !/<img/i.test(link), link);
  assert.ok(link.includes('說明'), '連結文字保留');
}

// 15. 安全：任何輸入的輸出都只能含白名單標籤，屬性只能是我們自己的 class。
{
  const hostile = [
    '<script>alert(1)</script>',
    '<img src=x onerror=alert(1)>',
    '- <b onmouseover="x()">hi</b>',
    '`<svg onload=alert(1)>`',
    '```\n</code></pre><script>alert(1)</script>\n```',
    '[x](javascript:alert(1))',
    '[x](" onclick="alert(1))',
    '> <iframe src=//evil></iframe>',
    '### <style>*{display:none}</style>',
    '**<a href=//evil>x</a>**',
    '&lt;script&gt; &amp; "quoted" \'single\'',
    '1. <details open ontoggle=alert(1)>',
    '`unbalanced <img src=x onerror=alert(1)>',
  ];
  const allowed = new Set(['h3', 'h4', 'h5', 'p', 'ul', 'ol', 'li', 'strong', 'em', 'code', 'pre', 'blockquote', 'hr']);
  const ours = /<\/?[a-z0-9]+( class="ra-notes-[a-z0-9-]+")?>/g;
  for (const input of hostile) {
    const out = desktopUpdateRenderNotes(input);
    const tags = out.match(/<\/?[a-zA-Z][^>]*>/g) || [];
    for (const t of tags) {
      const name = t.match(/^<\/?([a-zA-Z0-9]+)/)[1].toLowerCase();
      assert.ok(allowed.has(name), `不允許的標籤 ${t}（輸入：${input}）`);
      assert.ok(/^<\/?[a-z0-9]+( class="ra-notes-[a-z0-9-]+")?>$/.test(t), `標籤帶了不明屬性 ${t}（輸入：${input}）`);
    }
    // 去掉我們產生的標籤後，不得殘留任何 < 或 >（使用者文字必須全被跳脫）
    const rest = out.replace(ours, '');
    assert.ok(!/[<>]/.test(rest), `殘留未跳脫的 < 或 >：${rest}（輸入：${input}）`);
  }
  assert.ok(desktopUpdateRenderNotes('<script>alert(1)</script>').includes('&lt;script&gt;'));
}

console.log('desktop-update tests passed');
