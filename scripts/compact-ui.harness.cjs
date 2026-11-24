// 真 UI harness：既有 slash 清單選 /compact，fake WS 回原生結果。不呼叫真 provider。
// 檔名不是 *.test.cjs，所以 node --test scripts/*.test.cjs 不會啟動 Chrome。
const assert = require('node:assert/strict');
const crypto = require('node:crypto');
const fs = require('node:fs');
const http = require('node:http');
const os = require('node:os');
const path = require('node:path');
const { spawn } = require('node:child_process');

const repo = path.resolve(__dirname, '..');
const root = path.join(repo, 'internal/static/js');
const chrome = 'C:\\Program Files\\Google\\Chrome\\Application\\chrome.exe';
const files = [
  'hooks/useGitStatus.js', 'hooks/composerDrafts.js', 'hooks/cliArgs.js', 'hooks/messageRows.js',
  'hooks/quotaMetrics.js', 'hooks/forwardPrompt.js', 'ui/uiAtoms.js', 'hooks/useChatComposer.js',
  'chat/AttachmentCards.js', 'chat/ChatHeader.js', 'chat/ComposerTools.js', 'chat/SessionActionCards.js',
  'hooks/quotaResumeBanner.js', 'chat/QuotaResumeBanner.js', 'chat/VirtualMessages.js',
  'hooks/useChatSocket.js', 'chat/ChatView.js',
].filter((file) => fs.existsSync(path.join(root, file)));

const frames = [];
let failNext = false;
function wsAccept(key) {
  return crypto.createHash('sha1').update(key + '258EAFA5-E914-47DA-95CA-C5AB0DC85B11').digest('base64');
}
function wsSend(socket, text) {
  const payload = Buffer.from(text);
  const head = payload.length < 126
    ? Buffer.from([0x81, payload.length])
    : Buffer.from([0x81, 126, payload.length >> 8, payload.length & 255]);
  socket.write(Buffer.concat([head, payload]));
}
function consumeFrames(socket, url) {
  let buf = Buffer.alloc(0);
  socket.on('data', (chunk) => {
    buf = Buffer.concat([buf, chunk]);
    while (buf.length >= 2) {
      const opcode = buf[0] & 15;
      const len0 = buf[1] & 127;
      let start = 2;
      let len = len0;
      if (len0 === 126) {
        if (buf.length < 4) return;
        len = buf.readUInt16BE(2);
        start = 4;
      } else if (len0 === 127) {
        return;
      }
      const masked = (buf[1] & 128) !== 0;
      const frameEnd = start + (masked ? 4 : 0) + len;
      if (buf.length < frameEnd) return;
      let payload = buf.subarray(start + (masked ? 4 : 0), frameEnd);
      if (masked) {
        const mask = buf.subarray(start, start + 4);
        payload = Buffer.from(payload);
        for (let i = 0; i < payload.length; i++) payload[i] ^= mask[i % 4];
      }
      buf = buf.subarray(frameEnd);
      if (opcode === 9) {
        socket.write(Buffer.concat([Buffer.from([0x8a, payload.length]), payload]));
        continue;
      }
      if (opcode !== 1) continue;
      let msg;
      try { msg = JSON.parse(payload.toString()); } catch (_) { continue; }
      frames.push({ url, msg });
      if (msg.type !== 'compact') continue;
      if (failNext) {
        wsSend(socket, JSON.stringify({ type: 'compact', value: 'failed', content: '沒有收到壓縮確認' }));
        continue;
      }
      failNext = true;
      wsSend(socket, JSON.stringify({ type: 'status', value: 'COMPACTING' }));
      wsSend(socket, JSON.stringify({ type: 'compact', value: 'running', content: '正在壓縮上下文' }));
      wsSend(socket, JSON.stringify({ type: 'compact', value: 'done', content: '已壓縮上下文' }));
      wsSend(socket, JSON.stringify({ type: 'status', value: 'IDLE' }));
    }
  });
}

let releaseHold = () => {};
const hold = new Promise((resolve) => { releaseHold = resolve; });
const history = [{ id: 1, role: 'user', content: 'keep-history', status: 'done', created_at: '2026-11-24 05:42:51' }];
const server = http.createServer((req, res) => {
  const url = req.url.split('?')[0];
  if (url === '/frames') {
    res.writeHead(200, { 'Content-Type': 'application/json' });
    res.end(JSON.stringify(frames));
    return;
  }
  if (url === '/release') { releaseHold(); res.end('ok'); return; }
  if (url === '/hold') { hold.then(() => res.end('ok')); return; }
  if (url === '/bundle.js') {
    res.writeHead(200, { 'Content-Type': 'text/javascript; charset=utf-8' });
    res.end(bundle);
    return;
  }
  if (url.startsWith('/vendor/')) {
    res.writeHead(200, { 'Content-Type': 'text/javascript; charset=utf-8' });
    res.end(fs.readFileSync(path.join(root, 'vendor', path.basename(url))));
    return;
  }
  res.writeHead(200, { 'Content-Type': 'text/html; charset=utf-8' });
  res.end(page);
});
server.on('upgrade', (req, socket) => {
  const key = req.headers['sec-websocket-key'];
  if (!key) { socket.destroy(); return; }
  socket.write('HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: ' + wsAccept(key) + '\r\n\r\n');
  consumeFrames(socket, req.url);
  wsSend(socket, JSON.stringify({ type: 'sync', value: 'IDLE', messages: history, history_has_more: false, input_mode: 'agent' }));
});

const source = files.map((file) => fs.readFileSync(path.join(root, file), 'utf8')).join('\n');
const compiled = spawn('go', ['run', 'github.com/evanw/esbuild/cmd/esbuild', '--loader=jsx', '--target=es2020'], { cwd: repo, stdio: ['pipe', 'pipe', 'pipe'] });
let bundle = '';
let compileErr = '';
compiled.stdin.end(source);
compiled.stdout.on('data', (chunk) => { bundle += chunk; });
compiled.stderr.on('data', (chunk) => { compileErr += chunk; });
compiled.on('exit', (code) => {
  if (code !== 0) {
    console.error(compileErr);
    process.exit(code || 1);
  }
  server.listen(0, '127.0.0.1', launch);
});

const stubs = `
const {useState,useRef,useCallback,useMemo,useEffect,useLayoutEffect,useId,useReducer}=React;
var isTelegram=false,isGuestMode=false,SHARE_TOKEN_FROM_URL='';
const toasts=[];
function showToast(message, opts){toasts.push({message:String(message), error:!!(opts&&opts.error)});}
function parseMarkdown(text){return text||''}
function apiFetch(url,opts){return fetch(url,opts)}
function wsURL(id){return (location.protocol==='https:'?'wss':'ws')+'://'+location.host+'/sessions/'+id+'/ws'}
function useMediaQuery(){return false}
function useShareRemaining(){return null}
function useLongPress(){return ()=>({})}
function readComposerFiles(){return Promise.resolve([])}
function writeComposerFiles(){return Promise.resolve()}
function restoreComposerFiles(a){return a}
function socketPayloadError(){return ''}
function expandMentionPrompt(text){return text}
function workDirGroupShortLabel(){return 'repo'}
function filterMentionSessions(){return []}
function mentionQueryAtCursor(){return null}
function readGuestAuth(){return null}
function sessionStateChipClass(){return ''}
function ensureTelegramSession(){return Promise.resolve('')}
const ShareStatusBar=()=>null,ForwardModal=()=>null,MessageActionsHost=()=>null,SelectionQuotePopover=()=>null,MentionChips=()=>null,GuestReadOnlyBar=()=>null,MentionMenu=()=>null;
const AGENT_LABEL={claude:'Claude',cursor:'Cursor'};
const SESSION_STATE_LABEL={IDLE:'閒置',COMPACTING:'壓縮中'};
`;
const boot = `
const host=document.getElementById('host');
const root=ReactDOM.createRoot(host);
const out=document.getElementById('out');
const errors=[];
window.addEventListener('error',e=>errors.push(String(e.message||e.error)));
function setText(value){
  const ta=document.querySelector('[data-chat-input]');
  const proto=Object.getOwnPropertyDescriptor(window.HTMLTextAreaElement.prototype,'value');
  proto.set.call(ta,value);
  ta.dispatchEvent(new Event('input',{bubbles:true}));
  return ta;
}
function option(command){
  return [...document.querySelectorAll('[role="option"]')].find(el=>el.textContent.includes(command));
}
async function waitFor(pred,label){
  const start=Date.now();
  while(Date.now()-start<6000){
    if(pred()) return;
    await new Promise(r=>setTimeout(r,40));
  }
  throw new Error(label||'timeout');
}
function pressEnter(ta, composing){
  const ev=new KeyboardEvent('keydown',{key:'Enter',bubbles:true,cancelable:true});
  Object.defineProperty(ev,'keyCode',{get:()=>composing?229:13});
  Object.defineProperty(ev,'isComposing',{get:()=>!!composing});
  ta.dispatchEvent(ev);
}
(async()=>{
  let report={errors};
  try{
    composerDrafts.set('demo',{text:'',attachments:[{id:'draft-file',name:'keep.png',status:'ready',attachmentId:'att-1'}],mentions:[],filesRestored:true});
    const session={id:'demo',name:'壓縮',agent_type:'claude',agent_session_id:'provider-session',permission_mode:'acceptEdits',input_mode:'agent',work_dir:'repo'};
    root.render(React.createElement(ChatView,{session,onBack(){}}));
    await waitFor(()=>document.body.textContent.includes('keep-history')&&document.body.textContent.includes('keep.png')&&document.querySelector('[data-chat-input]')&&!document.querySelector('[data-chat-input]').disabled,'claude ready');
    setText('/');
    await waitFor(()=>option('/compact')&&option('/reset'),'slash menu');
    for (let i=0;i<8 && !toasts.some(t=>t.message==='已壓縮上下文');i++){
      const n=toasts.length;
      option('/compact').click();
      await new Promise(r=>setTimeout(r,300));
      if(toasts.some(t=>t.message==='已壓縮上下文')) break;
      if(!toasts.slice(n).some(t=>t.message.includes('連線中斷'))) break;
      if(!option('/compact')){setText('/'); await waitFor(()=>option('/compact'),'slash again');}
    }
    await waitFor(()=>toasts.some(t=>t.message==='已壓縮上下文'),'success toast');
    const afterSuccess=await (await fetch('/frames')).json();
    const compactFrames=afterSuccess.filter(f=>f.msg&&f.msg.type==='compact');
    const ta=setText('/compact');
    await waitFor(()=>option('/compact'),'compact only');
    pressEnter(ta,true);
    await new Promise(r=>setTimeout(r,200));
    const afterIME=await (await fetch('/frames')).json();
    pressEnter(document.querySelector('[data-chat-input]'),false);
    await waitFor(()=>toasts.some(t=>t.message==='沒有收到壓縮確認'),'failure toast');
    const claudeText=document.getElementById('host').textContent;
    root.render(null);
    await new Promise(r=>setTimeout(r,40));
    root.render(React.createElement(ChatView,{session:{id:'cursor-demo',name:'Cursor',agent_type:'cursor',agent_session_id:'provider-session',permission_mode:'acceptEdits',input_mode:'agent',work_dir:'repo'},onBack(){}}));
    await waitFor(()=>document.body.textContent.includes('Cursor')&&document.querySelector('[data-chat-input]')&&!document.querySelector('[data-chat-input]').disabled,'cursor ready');
    setText('/');
    await waitFor(()=>option('/compact'),'cursor menu');
    const cursorReason=option('/compact').textContent;
    option('/compact').click();
    await waitFor(()=>toasts.some(t=>t.message==='此 runner 尚未驗證，不能壓縮'),'cursor toast');
    root.render(null);
    await new Promise(r=>setTimeout(r,40));
    root.render(React.createElement(ChatView,{session:{id:'guest-demo',name:'分享',agent_type:'claude',agent_session_id:'provider-session',permission_mode:'acceptEdits',input_mode:'agent',work_dir:'repo'},guest:{role:'editor',mode:'live',nickname:'編輯',expires_at:Date.now()+3600000},onBack(){}}));
    await waitFor(()=>document.body.textContent.includes('編輯')&&document.querySelector('[data-chat-input]')&&!document.querySelector('[data-chat-input]').disabled,'guest ready');
    setText('/compact');
    await waitFor(()=>option('/compact'),'guest menu');
    option('/compact').click();
    await waitFor(()=>toasts.some(t=>t.message==='分享連線不能壓縮'),'guest toast');
    const all=await (await fetch('/frames')).json();
    report={
      errors,
      toasts,
      compactFrames:compactFrames.map(f=>f.msg),
      imeCount:afterIME.filter(f=>f.msg&&f.msg.type==='compact').length,
      cursorReason,
      claudeKept:{history:claudeText.includes('keep-history'),file:claudeText.includes('keep.png'),summary:claudeText.includes('summary')},
      laterCompact:all.filter(f=>f.msg&&f.msg.type==='compact').map(f=>({url:f.url.split('?')[0],type:f.msg.type})),
    };
  }catch(err){
    report={errors,toasts,error:String(err&&err.stack||err)};
  }
  out.textContent=JSON.stringify(report);
  fetch('/release');
})();
`;
const page = `<!doctype html><html lang="zh-TW"><head><meta charset="utf-8"></head><body>
<img src="/hold" alt="" width="1" height="1">
<div id="host"></div><pre id="out"></pre>
<script src="/vendor/react.production.min.js"></script>
<script src="/vendor/react-dom.production.min.js"></script>
<script>${stubs}</script><script src="/bundle.js"></script><script>${boot}</script>
</body></html>`;

function removeOwnProfile(profile) {
  let tempRoot;
  let resolved;
  try {
    tempRoot = fs.realpathSync(os.tmpdir());
    resolved = fs.realpathSync(profile);
  } catch (_) { return; }
  const rel = path.relative(tempRoot, resolved);
  if (!rel || rel.startsWith('..') || path.isAbsolute(rel) || !path.basename(resolved).startsWith('compact-ui-')) return;
  try { fs.rmSync(resolved, { recursive: true, force: true }); } catch (_) {}
}
function launch() {
  const port = server.address().port;
  const profile = fs.mkdtempSync(path.join(os.tmpdir(), 'compact-ui-'));
  const child = spawn(chrome, [
    '--headless=new', '--disable-gpu', '--no-first-run', '--disable-extensions',
    `--user-data-dir=${profile}`, '--window-size=1280,900', '--dump-dom', `http://127.0.0.1:${port}/`,
  ], { stdio: ['ignore', 'pipe', 'pipe'] });
  let stdout = '';
  let settled = false;
  const timer = setTimeout(() => finish(1, 'chrome timeout'), 45000);
  function finish(exit, extra) {
    if (settled) return;
    settled = true;
    clearTimeout(timer);
    const cleanup = () => {
      server.close();
      removeOwnProfile(profile);
      if (extra) console.error(extra);
      process.exit(exit);
    };
    if (!child.pid || child.exitCode !== null) { cleanup(); return; }
    const killer = spawn('taskkill', ['/PID', String(child.pid), '/T', '/F'], { stdio: 'ignore' });
    let childDone = false;
    let killDone = false;
    const both = () => { if (childDone && killDone) cleanup(); };
    child.once('exit', () => { childDone = true; both(); });
    killer.once('exit', () => { killDone = true; both(); });
  }
  child.stdout.on('data', (chunk) => { stdout += chunk; });
  child.on('exit', () => {
    const match = stdout.match(/<pre id="out">([\s\S]*?)<\/pre>/);
    let data = null;
    try { data = JSON.parse(match ? match[1] : ''); } catch (_) {}
    console.log('UI_PORT:' + port);
    console.log(match ? match[1] : stdout.slice(0, 1200));
    try {
      assert.ok(data && !data.error, data && (data.error || '沒有前端結果'));
      assert.deepEqual(data.errors, []);
      assert.equal(data.compactFrames.length, 1);
      assert.equal(data.compactFrames[0].type, 'compact');
      assert.equal(data.imeCount, 1);
      assert.equal(data.claudeKept.history, true, 'history missing');
      assert.equal(data.claudeKept.file, true, 'attachment draft missing');
      assert.equal(data.claudeKept.summary, false, 'summary visible');
      assert.ok(data.toasts.some((t) => t.message === '已壓縮上下文' && !t.error));
      assert.ok(data.toasts.some((t) => t.message === '沒有收到壓縮確認' && t.error));
      assert.ok(String(data.cursorReason).includes('尚未驗證'));
      assert.ok(data.toasts.some((t) => t.message === '此 runner 尚未驗證，不能壓縮'));
      assert.ok(data.toasts.some((t) => t.message === '分享連線不能壓縮'));
      assert.ok(data.laterCompact.every((f) => f.url === '/sessions/demo/ws'), JSON.stringify(data.laterCompact));
      console.log('UI_EXIT:0');
      finish(0);
    } catch (err) {
      console.log('UI_EXIT:1');
      finish(1, err.message);
    }
  });
}
