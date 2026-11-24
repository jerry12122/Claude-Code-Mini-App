const assert = require('node:assert/strict');
const crypto = require('node:crypto');
const fs = require('node:fs');
const http = require('node:http');
const os = require('node:os');
const path = require('node:path');
const zlib = require('node:zlib');
const { spawn } = require('node:child_process');

const repo = path.resolve(__dirname, '..');
const root = path.join(repo, 'internal/static/js');
const chrome = 'C:\\Program Files\\Google\\Chrome\\Application\\chrome.exe';
const files = [
  'hooks/useGitStatus.js', 'chat/GitControls.js', 'hooks/tabGroups.js', 'ui/SplitLayout.js',
  'core/toast.js', 'hooks/composerDrafts.js', 'hooks/cliArgs.js', 'hooks/messageRows.js',
  'hooks/quotaMetrics.js', 'hooks/forwardPrompt.js', 'ui/uiAtoms.js', 'hooks/useChatComposer.js',
  'chat/AttachmentCards.js', 'chat/ChatHeader.js', 'chat/ComposerTools.js', 'chat/SessionActionCards.js',
  'hooks/quotaResumeBanner.js', 'chat/QuotaResumeBanner.js', 'chat/VirtualMessages.js',
  'hooks/useChatSocket.js', 'chat/ChatView.js', 'hooks/rooms.js', 'hooks/roomPoller.js',
  'hooks/useRoomTimeline.js', 'session/RoomMessageBubble.js', 'session/RoomControls.js', 'session/RoomView.js',
].filter((file) => fs.existsSync(path.join(root, file)));

function pngChunk(type, data) {
  const body = Buffer.concat([Buffer.from(type), data]);
  const len = Buffer.alloc(4);
  len.writeUInt32BE(data.length);
  const crc = Buffer.alloc(4);
  crc.writeUInt32BE(zlib.crc32(body) >>> 0);
  return Buffer.concat([len, body, crc]);
}
function onePixelPNG() {
  const ihdr = Buffer.alloc(13);
  ihdr.writeUInt32BE(1, 0);
  ihdr.writeUInt32BE(1, 4);
  ihdr[8] = 8;
  ihdr[9] = 2;
  const raw = Buffer.from([0, 255, 0, 0]);
  return Buffer.concat([
    Buffer.from([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a]),
    pngChunk('IHDR', ihdr),
    pngChunk('IDAT', zlib.deflateSync(raw)),
    pngChunk('IEND', Buffer.alloc(0)),
  ]);
}
const png = onePixelPNG();

function wsAccept(key) {
  return crypto.createHash('sha1').update(key + '258EAFA5-E914-47DA-95CA-C5AB0DC85B11').digest('base64');
}
function wsSend(socket, text) {
  const payload = Buffer.from(text);
  const head = payload.length < 126 ? Buffer.from([0x81, payload.length]) : Buffer.from([0x81, 126, payload.length >> 8, payload.length & 255]);
  socket.write(Buffer.concat([head, payload]));
}
function consumeFrames(socket) {
  let buf = Buffer.alloc(0);
  socket.on('data', (chunk) => {
    buf = Buffer.concat([buf, chunk]);
    while (buf.length >= 2) {
      const len0 = buf[1] & 127;
      let start = 2;
      let len = len0;
      if (len0 === 126) {
        if (buf.length < 4) return;
        len = buf.readUInt16BE(2);
        start = 4;
      } else if (len0 === 127) return;
      const mask = (buf[1] & 128) !== 0;
      const frame = start + (mask ? 4 : 0) + len;
      if (buf.length < frame) return;
      buf = buf.subarray(frame);
    }
  });
}

const attachment = (id, name) => ({ id, name, mime_type: 'image/png', size: png.length, url: '/sessions/demo/uploads/' + id });
const claudeMsg = { id: 1, role: 'claude', content: '即時圖', status: 'done', created_at: '2026-11-24 05:42:51', attachments: [attachment('live', 'live.png')] };
const systemMsg = { id: 2, role: 'system', content: '系統圖', status: 'done', created_at: '2026-11-24 05:42:52', attachments: [attachment('system', 'system.png')] };
const roomMsg = { id: 9, session_id: 'worker', session_name: 'worker', member_role: 'worker', agent_type: 'claude', role: 'claude', text: '工作室圖', body: '工作室圖', status: 'done', created_at: '2026-11-24 05:42:53', attachments: [attachment('room', 'room.png')] };
let sockets = 0;
let releaseHold = () => {};
const hold = new Promise((resolve) => { releaseHold = resolve; });

const server = http.createServer((req, res) => {
  const url = req.url.split('?')[0];
  if (url.startsWith('/sessions/demo/uploads/')) {
    res.writeHead(200, { 'Content-Type': 'image/png', 'X-Content-Type-Options': 'nosniff', 'Cache-Control': 'private, no-store' });
    res.end(png);
    return;
  }
  if (url === '/rooms/room-1') {
    res.writeHead(200, { 'Content-Type': 'application/json' });
    res.end(JSON.stringify({ id: 'room-1', name: '工作室', pm_id: 'pm', members: [
      { session_id: 'pm', session_name: 'PM', role: 'pm', agent_type: 'claude', status: 'idle' },
      { session_id: 'worker', session_name: 'worker', role: 'worker', agent_type: 'claude', status: 'idle' },
    ] }));
    return;
  }
  if (url.startsWith('/rooms/room-1/timeline')) {
    res.writeHead(200, { 'Content-Type': 'application/json' });
    res.end(JSON.stringify({ messages: [roomMsg], has_more: false, next_cursor: '' }));
    return;
  }
  if (url.startsWith('/rooms/room-1/')) {
    res.writeHead(200, { 'Content-Type': 'application/json' });
    res.end(JSON.stringify({ messages: [], queue: [] }));
    return;
  }
  if (url === '/release') { releaseHold(); res.end('ok'); return; }
  if (url === '/hold') { hold.then(() => res.end(png)); return; }
  if (url === '/bundle.js') { res.writeHead(200, { 'Content-Type': 'text/javascript; charset=utf-8' }); res.end(bundle); return; }
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
  consumeFrames(socket);
  sockets += 1;
  if (sockets === 1) {
    wsSend(socket, JSON.stringify({ type: 'sync', value: 'IDLE' }));
    setTimeout(() => {
      wsSend(socket, JSON.stringify({ type: 'message', ...claudeMsg }));
      wsSend(socket, JSON.stringify({ type: 'message', ...systemMsg }));
    }, 120);
  } else {
    wsSend(socket, JSON.stringify({ type: 'sync', value: 'IDLE', messages: [claudeMsg, systemMsg], history_has_more: false }));
  }
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
function parseMarkdown(text){return text||''}
function apiFetch(url,opts){return fetch(url,opts)}
function wsURL(id){return (location.protocol==='https:'?'wss':'ws')+'://'+location.host+'/sessions/'+id+'/ws?history_limit=100'}
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
const AGENT_LABEL={claude:'Claude'};
const SESSION_STATE_LABEL={IDLE:'閒置'};
`;
const boot = `
const host=document.getElementById('host');
const root=ReactDOM.createRoot(host);
const session={id:'demo',name:'圖片',agent_type:'claude',permission_mode:'default',input_mode:'agent',work_dir:'repo'};
const out=document.getElementById('out');
const errors=[];
window.addEventListener('error',e=>errors.push(String(e.message)));
function mountChat(){root.render(React.createElement(ChatView,{session,onBack(){}}));}
function width(alt){const img=document.querySelector('img[alt="'+alt+'"]');return img&&img.complete?img.naturalWidth:0}
async function waitFor(alts){
  const start=Date.now();
  while(Date.now()-start<6000){
    if(alts.every(alt=>width(alt)>0)) return;
    await new Promise(r=>setTimeout(r,50));
  }
}
(async()=>{
  mountChat();
  await waitFor(['live.png','system.png']);
  const live={claude:width('live.png'),system:width('system.png')};
  root.render(null);
  await new Promise(r=>setTimeout(r,30));
  mountChat();
  await waitFor(['live.png','system.png']);
  const history={claude:width('live.png'),system:width('system.png')};
  root.render(React.createElement(RoomView,{roomId:'room-1',sessions:[],onClose(){},onOpenSession(){}}));
  await waitFor(['room.png']);
  const room=width('room.png');
  out.textContent=JSON.stringify({live,history,room,errors});
  fetch('/release');
})();
`;
const page = `<!doctype html><html lang="zh-TW"><head><meta charset="utf-8">
<link rel="stylesheet" href="/vendor/tailwind.css">
<style>.h-app{height:800px}html,body{margin:0;background:#111}</style></head><body class="h-app">
<img src="/hold" alt="" width="1" height="1">
<div id="host" class="h-app"></div><pre id="out"></pre>
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
  } catch (_) {
    return;
  }
  const rel = path.relative(tempRoot, resolved);
  if (!rel || rel.startsWith('..') || path.isAbsolute(rel) || !path.basename(resolved).startsWith('image-ui-')) return;
  try { fs.rmSync(resolved, { recursive: true, force: true }); } catch (_) {}
}

function launch() {
  const port = server.address().port;
  const profile = fs.mkdtempSync(path.join(os.tmpdir(), 'image-ui-'));
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
    if (!child.pid || child.exitCode !== null) {
      cleanup();
      return;
    }
    const killer = spawn('taskkill', ['/PID', String(child.pid), '/T', '/F'], { stdio: 'ignore' });
    let childDone = false;
    let killDone = false;
    const both = () => { if (childDone && killDone) cleanup(); };
    child.once('exit', () => { childDone = true; both(); });
    killer.once('exit', () => { killDone = true; both(); });
    if (child.exitCode !== null) { childDone = true; both(); }
  }
  child.stdout.on('data', (chunk) => { stdout += chunk; });
  child.on('exit', () => {
    const match = stdout.match(/<pre id="out">([\s\S]*?)<\/pre>/);
    let data = null;
    try { data = JSON.parse(match ? match[1] : ''); } catch (_) {}
    console.log('UI_PORT:' + port);
    console.log(match ? match[1] : stdout.slice(0, 800));
    try {
      assert.ok(data, '沒有前端結果');
      assert.equal(data.errors.length, 0, data.errors.join('\n'));
      assert.ok(data.live.claude > 0, '即時 claude 圖沒有載入');
      assert.ok(data.live.system > 0, '即時 system 圖沒有載入');
      assert.ok(data.history.claude > 0 && data.history.system > 0, '重整後圖片沒有留下');
      assert.ok(data.room > 0, '工作室時間軸圖片沒有載入');
      console.log('UI_EXIT:0');
      finish(0);
    } catch (err) {
      console.log('UI_EXIT:1');
      finish(1, err.message);
    }
  });
}
