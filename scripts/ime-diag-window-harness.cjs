const assert = require('node:assert/strict');
const fs = require('node:fs');
const http = require('node:http');
const os = require('node:os');
const path = require('node:path');
const { spawn } = require('node:child_process');

const chrome = 'C:\\Program Files\\Google\\Chrome\\Application\\chrome.exe';
const recorderPath = path.join(__dirname, '../internal/imediag/recorder.js');
const html = `<!doctype html><meta charset="utf-8"><textarea id="SECRET-DOM-ID">SECRET-VALUE</textarea><pre id="out"></pre>
<script src="/recorder.js"></script>
<script>
document.querySelector('textarea').focus();
installImeDiag(window, document, function () {});
window.dispatchEvent(new Event('blur'));
window.dispatchEvent(new Event('focus'));
document.getElementById('out').textContent = JSON.stringify(window.__imeDiagFreeze());
</script>`;

const server = http.createServer((req, res) => {
  if (req.url === '/recorder.js') {
    res.setHeader('content-type', 'text/javascript; charset=utf-8');
    res.end(fs.readFileSync(recorderPath));
    return;
  }
  res.setHeader('content-type', 'text/html; charset=utf-8');
  res.end(html);
});

const profile = fs.mkdtempSync(path.join(os.tmpdir(), 'ime-diag-window-'));
let chromeProc = null;

function stop() {
  server.close();
  if (chromeProc && chromeProc.pid && chromeProc.exitCode === null) {
    spawn('taskkill', ['/PID', String(chromeProc.pid), '/T', '/F'], { stdio: 'ignore' });
  }
  const resolvedProfile = fs.realpathSync(profile);
  if (path.dirname(resolvedProfile) !== fs.realpathSync(os.tmpdir()) || !path.basename(resolvedProfile).startsWith('ime-diag-window-')) throw new Error('Unexpected temporary profile path');
  fs.rmSync(resolvedProfile, { recursive: true, force: true, maxRetries: 5, retryDelay: 100 });
}

server.listen(0, '127.0.0.1', () => {
  const port = server.address().port;
  const url = `http://127.0.0.1:${port}/`;
  chromeProc = spawn(chrome, [
    '--headless=new',
    '--disable-gpu',
    '--no-first-run',
    '--no-default-browser-check',
    `--user-data-dir=${profile}`,
    '--dump-dom',
    url,
  ], { stdio: ['ignore', 'pipe', 'pipe'] });
  let stdout = '';
  let stderr = '';
  chromeProc.stdout.on('data', (chunk) => { stdout += chunk; });
  chromeProc.stderr.on('data', (chunk) => { stderr += chunk; });
  const timer = setTimeout(() => {
    stop();
    console.error('chrome harness timed out');
    process.exit(1);
  }, 20000);
  chromeProc.on('exit', (code) => {
    clearTimeout(timer);
    stop();
    try {
      const match = stdout.match(/<pre id="out">([\s\S]*?)<\/pre>/);
      assert.ok(match, 'export missing from real document');
      const dumped = match[1];
      const parsed = JSON.parse(dumped);
      const kinds = parsed.events.map((event) => event.kind);
      assert.ok(kinds.includes('window-blur') && kinds.includes('window-focus'), kinds.join(','));
      for (const event of parsed.events) {
        if (event.kind === 'window-blur' || event.kind === 'window-focus') {
          assert.equal(typeof event.documentHasFocus, 'boolean');
          assert.equal(event.tag, 'TEXTAREA');
        }
      }
      assert.equal(dumped.includes('SECRET-DOM-ID'), false);
      assert.equal(dumped.includes('SECRET-VALUE'), false);
      console.log('ime-diag window dom harness ok');
      process.exit(0);
    } catch (err) {
      console.error(String(err && err.stack || err));
      console.error(stderr.slice(0, 500));
      process.exit(code || 1);
    }
  });
});
