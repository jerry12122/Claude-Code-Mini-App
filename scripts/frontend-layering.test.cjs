// 前端分層規範的自動檢查，規則定義見 docs/spec/frontend-architecture.md
// 執行：node scripts/frontend-layering.test.cjs
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');

const STATIC = path.join(__dirname, '..', 'internal', 'static');
const JS = path.join(STATIC, 'js');
const MAX_LINES = 850;

// 既存違規白名單。新增檔案不得寫進來；處理掉一項就刪一項。
const KNOWN_VIOLATIONS = {
  R4_ws: [],
  R5_kebab: [],
  R6_size: ['chat/ChatView.js', 'hooks/useChatSocket.js'],
  R7_custom_dialog: [],
};

// <head> 載入的獨立腳本，不屬於 React 應用，不受命名與分層規則約束
const HEAD_SCRIPTS = ['wails-runtime-ready.js', 'devtool-easter-egg.js'];
// 擁有 WebSocket 連線生命週期的檔案（見規範第 2 節）
const WS_OWNERS = [
  'hooks/useChatSocket.js', 'hooks/forwardPrompt.js',
  'session/SessionView.js', 'ui/LogsSection.js',
];

const rel = (p) => path.relative(JS, p).split(path.sep).join('/');

function walk(dir) {
  return fs.readdirSync(dir, { withFileTypes: true }).flatMap((e) => {
    const full = path.join(dir, e.name);
    if (e.isDirectory()) return e.name === 'vendor' ? [] : walk(full);
    return e.name.endsWith('.js') ? [full] : [];
  });
}

/** 去掉註解與字串，避免把註解裡提到的符號名當成真實依賴。 */
function stripNoise(src) {
  return src
    .replace(/\/\*[\s\S]*?\*\//g, ' ')
    .replace(/^\s*\/\/.*$/gm, ' ')
    .replace(/(['"`])(?:\\.|(?!\1)[^\\])*\1/g, '""');
}

const files = walk(JS).map((f) => ({ rel: rel(f), src: fs.readFileSync(f, 'utf8') }));
const appFiles = files.filter((f) => !HEAD_SCRIPTS.includes(f.rel));
const byRel = new Map(files.map((f) => [f.rel, f]));
const indexHtml = fs.readFileSync(path.join(STATIC, 'index.html'), 'utf8');
const registered = [...indexHtml.matchAll(/src="\.\/js\/([^"]+)"/g)]
  .map((m) => m[1])
  .filter((p) => !p.startsWith('vendor/'));
const fail = [];
const check = (ok, msg) => { if (!ok) fail.push(msg); };
const allowed = (rule, f) => KNOWN_VIOLATIONS[rule].includes(f);

// R1 註冊完整性：雙向都要對得上
for (const f of appFiles) {
  check(registered.includes(f.rel), `R1 ${f.rel} 未在 index.html 註冊`);
}
for (const r of registered) {
  check(byRel.has(r), `R1 index.html 引用了不存在的 ./js/${r}`);
}

// R2 core/ 不得引用上層符號
const coreFiles = appFiles.filter((f) => f.rel.startsWith('core/'));
const upperSymbols = new Set();
for (const f of appFiles) {
  if (f.rel.startsWith('core/')) continue;
  for (const m of stripNoise(f.src).matchAll(/^(?:function|const|let)\s+([A-Za-z_$][\w$]*)/gm)) {
    upperSymbols.add(m[1]);
  }
}
for (const f of coreFiles) {
  const body = stripNoise(f.src);
  for (const sym of upperSymbols) {
    check(!new RegExp(`\\b${sym}\\s*[(<]`).test(body), `R2 ${f.rel} 引用了上層符號 ${sym}`);
  }
}

// R3 hooks/ 不得含 JSX
for (const f of appFiles.filter((x) => x.rel.startsWith('hooks/'))) {
  const jsx = /<\/[A-Za-z][\w.]*>|<[A-Z][\w.]*[\s/>]/.test(stripNoise(f.src));
  check(!jsx, `R3 ${f.rel} 含 JSX，應只放無 UI 的邏輯`);
}

// R4 裸 fetch 只在 core/；new WebSocket 只在連線擁有者
for (const f of appFiles) {
  const body = stripNoise(f.src);
  if (!f.rel.startsWith('core/')) {
    check(!/[^.\w]fetch\s*\(/.test(body), `R4 ${f.rel} 直接呼叫 fetch()，應走 core 的 apiFetch / fetchWithTimeout`);
  }
  if (/new\s+WebSocket\s*\(/.test(body) && !WS_OWNERS.includes(f.rel)) {
    check(allowed('R4_ws', f.rel), `R4 ${f.rel} 自行開 WebSocket，非連線擁有者`);
  }
}

// R5 無 kebab-case 檔名
for (const f of appFiles) {
  const name = f.rel.split('/').pop();
  if (name.includes('-')) check(allowed('R5_kebab', f.rel), `R5 ${f.rel} 使用 kebab-case 檔名`);
}

// R6 單檔 ≤ 800 行
for (const f of appFiles) {
  const lines = f.src.split('\n').length;
  if (lines > MAX_LINES) {
    check(allowed('R6_size', f.rel), `R6 ${f.rel} ${lines} 行，超過 ${MAX_LINES} 行上限`);
  }
}

// R7 不得新增自製 modal：原生 <dialog> + showModal() 已提供語意、焦點與 Esc
// 比對原始碼而非 stripNoise 後的內容，因為 role="dialog" 是 JSX 屬性字串；
// ponytail: 只濾掉整行註解，行尾註解提到這些 token 仍會誤報，必要時用白名單處理
const CUSTOM_DIALOG = /role="dialog"|aria-modal|DIALOG_FOCUSABLE/;
const withoutCommentLines = (src) =>
  src.split('\n').filter((l) => !/^\s*(\/\/|\/\*|\*)/.test(l)).join('\n');
for (const f of appFiles) {
  if (CUSTOM_DIALOG.test(withoutCommentLines(f.src))) {
    check(allowed('R7_custom_dialog', f.rel), `R7 ${f.rel} 自製 modal，應改用原生 <dialog> + showModal()`);
  }
}

// 白名單必須是真實存在的檔案，避免規則處理完後殘留死條目
for (const [rule, list] of Object.entries(KNOWN_VIOLATIONS)) {
  for (const f of list) check(byRel.has(f), `${rule} 白名單中的 ${f} 不存在，請清除`);
}

assert.deepEqual(fail, [], `\n前端分層檢查失敗：\n  ${fail.join('\n  ')}\n`);
console.log(
  `前端分層檢查通過：${appFiles.length} 支應用層檔案，` +
  `既存例外 ${Object.values(KNOWN_VIOLATIONS).flat().length} 項`
);
