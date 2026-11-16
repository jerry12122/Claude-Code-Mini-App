// Uses the real browser DOM and the vendored sanitizer; no npm dependencies.
//
// The page reports its verdict back through an image beacon to a loopback server
// instead of --dump-dom: as of Edge 157 that flag exits 0 while printing nothing
// (Chrome exits 21), which turned this suite red with an empty diff.
//
// The browser is shut down over CDP (Browser.close), not by PID. On Windows the
// spawned msedge.exe is only a launcher: it exits with code 0 within ~60ms and
// the renderer, GPU and utility processes keep running detached, so the child's
// exit event says nothing about the page and taskkill /T cannot reach them.
// Killing by PID leaked a full process tree per run and kept the profile locked.
const { test } = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');
const http = require('node:http');
const { pathToFileURL } = require('node:url');
const { spawn } = require('node:child_process');

const browser = [process.env.BROWSER_BIN,
  'C:/Program Files (x86)/Microsoft/Edge/Application/msedge.exe',
  'C:/Program Files/Google/Chrome/Application/chrome.exe',
  '/usr/bin/chromium', '/usr/bin/google-chrome',
].find(p => p && fs.existsSync(p));

// Beacon deadline. The page only has to sanitize strings and load one srcdoc
// iframe (measured ~1.4s cold on Edge 157), so reaching this means the script never ran.
const TIMEOUT_MS = 30000;
const sleep = ms => new Promise(resolve => setTimeout(resolve, ms));

/** Serves the page's verdict on GET /done?r=<result>. */
async function listenForVerdict() {
  let settle;
  const verdict = new Promise(resolve => { settle = resolve; });
  const server = http.createServer((req, res) => {
    res.writeHead(204).end();
    const url = new URL(req.url, 'http://127.0.0.1');
    if (url.pathname === '/done') settle(url.searchParams.get('r') ?? '(no result)');
  });
  await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
  return { server, verdict, port: server.address().port };
}

/**
 * Asks the browser to close its whole process tree over CDP. The endpoint comes
 * from DevToolsActivePort, which Chromium writes into the profile once
 * --remote-debugging-port=0 has picked a port. Returns false if it never could.
 * The socket closing only means the request landed; processes take longer to exit.
 */
async function closeBrowser(profile) {
  let port, wsPath;
  for (let i = 0; i < 50 && !wsPath; i++) {
    try { [port, wsPath] = fs.readFileSync(path.join(profile, 'DevToolsActivePort'), 'utf8').trim().split('\n'); }
    catch { await sleep(100); }
  }
  if (!wsPath) return false;
  try {
    const ws = new WebSocket(`ws://127.0.0.1:${port}${wsPath}`);
    await new Promise((resolve, reject) => { ws.onopen = resolve; ws.onerror = reject; });
    const closed = new Promise(resolve => { ws.onclose = resolve; });
    ws.send(JSON.stringify({ id: 1, method: 'Browser.close' }));
    await Promise.race([closed, sleep(5000)]);
    return true;
  } catch {
    return false;
  }
}

/**
 * Removes the temp dir once the browser lets go of it. After Browser.close the
 * process tree needs ~1-2s to exit and keeps profile files open until then
 * (EBUSY / ENOTEMPTY on Windows). fs.rmSync's own retries back off linearly and
 * spent ~40s here, so poll on a flat interval with a total deadline instead.
 */
async function removeWhenReleased(dir, deadlineMs = 15000) {
  const until = Date.now() + deadlineMs;
  for (;;) {
    try { fs.rmSync(dir, { recursive: true, force: true }); return; }
    catch (err) {
      if (!['EBUSY', 'ENOTEMPTY', 'EPERM'].includes(err.code) || Date.now() > until) throw err;
      await sleep(250);
    }
  }
}

test('Markdown strips executable HTML and preserves code, tables and Mermaid', { skip: !browser }, async () => {
  const root = path.resolve(__dirname, '../internal/static');
  const source = fs.readFileSync(path.join(root, 'js/core/core.js'), 'utf8');
  const parser = source.slice(source.indexOf('marked.setOptions('), source.indexOf('/** 顯示用：permission_mode'));
  assert.ok(parser.includes('parseMarkdown'), 'could not slice parseMarkdown out of core.js');
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'miniapp-markdown-'));
  const profile = path.join(dir, 'profile');
  const { server, verdict, port } = await listenForVerdict();
  let launched = false;
  try {
    const scripts = ['marked.min.js', 'purify.min.js', 'highlight.min.js'].map(name =>
      `<script src="${pathToFileURL(path.join(root, 'js/vendor', name))}"></script>`).join('');
    const checks = () => {
      const check = (v, msg) => { if (!v) throw Error(msg); };
      const dirty = '<img src=x onerror="window.xss=1"><svg onload="window.xss=1"></svg>'
        + '<iframe srcdoc="x"></iframe><a href="javascript:alert(1)">bad</a>'
        + '<form id="DOMPurify"><input name="sanitize"></form>';
      const clean = parseMarkdown(dirty);
      const host = document.createElement('div'); host.innerHTML = clean; document.body.append(host);
      check(!host.querySelector('form,input,script'), 'active tags survived');
      for (const el of host.querySelectorAll('*')) for (const attr of el.attributes) {
        check(!/^on/i.test(attr.name), 'event handler survived');
        check(!/javascript:/i.test(attr.value), 'script URL survived');
      }
      // The payload above opens with <svg>, and parseMarkdown routes any block
      // containing <svg / <style / style= into the sandboxed <ra-html> preview
      // without calling DOMPurify at all. So the assertions above never exercise
      // the main sanitizer path or its FORBID_TAGS list. Cover that path with a
      // payload that has none of those triggers.
      const plain = document.createElement('div');
      plain.innerHTML = parseMarkdown('<form id="DOMPurify"><input name="sanitize"><textarea>t</textarea>'
        + '<select><option>o</option></select></form><img src=x onerror="window.xss=1">');
      check(!plain.querySelector('ra-html'), 'plain payload unexpectedly took the sandbox path');
      check(!plain.querySelector('form,input,textarea,select,script'), 'FORBID_TAGS not enforced on the main path');
      for (const el of plain.querySelectorAll('*')) for (const attr of el.attributes) {
        check(!/^on/i.test(attr.name), 'event handler survived the main path');
      }
      check(parseMarkdown('```js\nconst x = 1;\n```').includes('code-copy-btn'), 'code lost');
      check(parseMarkdown('| a | b |\n| - | - |\n| 1 | 2 |').includes('table-wrap'), 'table lost');
      const diagram = parseMarkdown('```mermaid\nflowchart TD\nA-->B\n```');
      check(diagram.includes('ra-mermaid') && diagram.includes('data-code-b64'), 'Mermaid lost');
      check(parseMarkdown('[safe](https://example.com)').includes('rel="noopener noreferrer"'), 'link protection lost');
      // Stable prefixes must not be sanitized again on every streamed delta.
      const sanitize = DOMPurify.sanitize;
      let calls = 0;
      DOMPurify.sanitize = (...args) => { calls++; return sanitize(...args); };
      const prefix = Array.from({length:1000}, (_,i) => `paragraph ${i} **bold**\n\n`).join('');
      parseMarkdown(prefix, {streaming:true});
      calls = 0;
      parseMarkdown(prefix + 'tail', {streaming:true});
      check(calls <= 2, 'stream re-sanitized the entire history: ' + calls);
      const code = '```html\n' + '<img src=x onerror="window.xss=1">'.repeat(3000);
      calls = 0;
      const codeHTML = parseMarkdown(code, {streaming:true});
      check(calls === 0 && !codeHTML.includes('<img src=x'), 'streamed code was not safely escaped');
      DOMPurify.sanitize = sanitize;
      check(parseMarkdown('[x][ref]\n\n[ref]: https://example.com').includes('href="https://example.com"'), 'reference link lost');
      check(!parseMarkdown('[x][ref]\n\n[ref]: javascript:alert(1)').includes('href="javascript:'), 'reference cache reused unsafe link');

      const styled = document.createElement('div');
      styled.innerHTML = parseMarkdown('<style>.box{color:rgb(255, 0, 0)}</style><div class="box" style="padding:12px">Styled</div>');
      document.body.append(styled);
      const preview = styled.querySelector('ra-html');
      check(preview, 'styled HTML lost its isolated preview');
      const frame = preview.querySelector('iframe');
      check(frame?.srcdoc.includes('padding:12px') && frame.srcdoc.includes('.box'), 'styles lost');
      check(frame.sandbox.contains('allow-scripts') && !frame.sandbox.contains('allow-same-origin'), 'preview has console privileges');
      const height = frame.style.height;
      preview.onHeight({source:window,data:{previewHeight:9999}});
      check(frame.style.height === height, 'untrusted window resized a preview');
      window.addEventListener('message', event => {
        if (event.source !== frame.contentWindow) return;
        try {
          let isolated = false;
          try { void frame.contentWindow.document; } catch { isolated = true; }
          check(isolated, 'preview can share console origin');
          check(frame.style.height !== '150px', 'preview height did not adapt');
          check(!window.xss, 'script executed in console');
          report('passed');
        } catch (err) { report('failed:' + err.message); }
      });
    };
    const preview = `<script src="${pathToFileURL(path.join(root, 'js/chat/HTMLPreview.js'))}"></script>`;
    // report() keeps writing the dataset so the verdict stays visible when
    // debugging the page by hand, and pings the beacon for the test runner.
    const report = `const report = (r) => {
      if (document.body.dataset.securityResult) return;
      document.body.dataset.securityResult = r;
      new Image().src = 'http://127.0.0.1:${port}/done?r=' + encodeURIComponent(r);
    };`;
    const html = `${scripts}${preview}<body><script>${report}\n${parser}\ntry { (${checks})(); } catch (err) { report('failed:' + err.message); }</script></body>`;
    const file = path.join(dir, 'test.html'); fs.writeFileSync(file, html);
    const stderr = [];
    const child = spawn(browser, ['--headless', '--disable-gpu', '--no-first-run', '--remote-debugging-port=0',
      `--user-data-dir=${profile}`, pathToFileURL(file).href],
      { windowsHide: true, stdio: ['ignore', 'ignore', 'pipe'] });
    launched = true;
    child.stderr.on('data', b => stderr.push(b));
    // Only a launch failure is decisive. A normal exit is not: on Windows the
    // launcher exits 0 almost at once while the page keeps running elsewhere.
    const failedToStart = new Promise(resolve => child.on('error', err => resolve('browser failed to start: ' + err.message)));
    let timer;
    const timedOut = new Promise(resolve => { timer = setTimeout(() => resolve(`no verdict within ${TIMEOUT_MS}ms`), TIMEOUT_MS); });
    const result = await Promise.race([verdict, failedToStart, timedOut]);
    clearTimeout(timer);
    assert.equal(result, 'passed', result + '\n' + Buffer.concat(stderr).toString().slice(-2000));
  } finally {
    if (launched) await closeBrowser(profile);
    await new Promise(resolve => server.close(resolve));
    assert.equal(path.dirname(dir), os.tmpdir());
    await removeWhenReleased(dir);
  }
});
