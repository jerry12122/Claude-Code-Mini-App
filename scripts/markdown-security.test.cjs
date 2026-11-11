// Uses the real browser DOM and the vendored sanitizer; no npm dependencies.
const { test } = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');
const { pathToFileURL } = require('node:url');
const { spawnSync } = require('node:child_process');

const browser = [process.env.BROWSER_BIN,
  'C:/Program Files (x86)/Microsoft/Edge/Application/msedge.exe',
  'C:/Program Files/Google/Chrome/Application/chrome.exe',
  '/usr/bin/chromium', '/usr/bin/google-chrome',
].find(p => p && fs.existsSync(p));

test('Markdown strips executable HTML and preserves code, tables and Mermaid', { skip: !browser }, () => {
  const root = path.resolve(__dirname, '../internal/static');
  const source = fs.readFileSync(path.join(root, 'js/core/core.js'), 'utf8');
  const parser = source.slice(source.indexOf('marked.setOptions('), source.indexOf('/** 顯示用：permission_mode'));
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'miniapp-markdown-'));
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
          document.body.dataset.securityResult = 'passed';
        } catch (err) { document.body.dataset.securityResult = 'failed:' + err.message; }
      });
    };
    const preview = `<script src="${pathToFileURL(path.join(root, 'js/chat/HTMLPreview.js'))}"></script>`;
    const html = `${scripts}${preview}<body><script>${parser}\ntry { (${checks})(); } catch (err) { document.body.dataset.securityResult = 'failed:' + err.message; }</script></body>`;
    const file = path.join(dir, 'test.html'); fs.writeFileSync(file, html);
    const result = spawnSync(browser, ['--headless', '--disable-gpu', '--no-first-run',
      `--user-data-dir=${path.join(dir, 'profile')}`, '--virtual-time-budget=1000', '--dump-dom', pathToFileURL(file).href],
      { encoding: 'utf8', windowsHide: true, timeout: 30000, maxBuffer: 2 << 20 });
    assert.equal(result.error, undefined, String(result.error));
    assert.match(result.stdout, /data-security-result="passed"/, result.stderr.slice(-2000) + result.stdout.slice(-2000));
  } finally {
    assert.equal(path.dirname(dir), os.tmpdir());
    fs.rmSync(dir, { recursive: true, force: true, maxRetries: 10, retryDelay: 100 });
  }
});
