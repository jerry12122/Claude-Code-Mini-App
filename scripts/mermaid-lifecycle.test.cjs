// Real Mermaid + browser DOM: catches theme initialization and leaked render nodes.
const { test } = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');
const { pathToFileURL } = require('node:url');
const { spawnSync } = require('node:child_process');

const browser = [process.env.BROWSER_BIN,
  'C:/Program Files/Google/Chrome/Application/chrome.exe',
  'C:/Program Files (x86)/Microsoft/Edge/Application/msedge.exe',
  '/usr/bin/chromium', '/usr/bin/google-chrome',
].find(p => p && fs.existsSync(p));

test('Mermaid initializes both themes and leaves no page-level SVG after a session switch', { skip: !browser }, async () => {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'miniapp-mermaid-'));
  try {
    const response = await fetch('https://cdn.jsdelivr.net/npm/mermaid@11.17.2/dist/mermaid.min.js', { signal: AbortSignal.timeout(30000) });
    assert.ok(response.ok, 'Mermaid fixture download failed');
    fs.writeFileSync(path.join(dir, 'mermaid.min.js'), await response.text());
    const checks = async () => {
      const errors = [];
      const check = (value, message) => { if (!value) errors.push(message); };
      for (const theme of ['light', 'dark']) {
        document.documentElement.dataset.theme = theme;
        try { mermaid.initialize(getMermaidConfig()); }
        catch (err) { errors.push(`${theme} initialization: ${err.message}`); }
      }
      const idle = async el => {
        for (let i = 0; i < 100 && el._rendering; i++) await new Promise(r => setTimeout(r, 10));
        check(!el._rendering, 'render did not settle');
      };
      const block = code => {
        const el = document.createElement('ra-mermaid');
        el.setAttribute('data-code-b64', btoa(code));
        el.setAttribute('data-streaming', 'false');
        document.body.append(el);
        return el;
      };
      const noLeak = () => check(!document.body.querySelector(':scope > div, :scope > svg'), 'page-level Mermaid SVG leaked');
      const valid = block('graph TD\nA[Start]-->B[Finish]');
      await idle(valid);
      check(valid.querySelector('.ra-mermaid-diagram svg') && !valid._hasError, 'valid diagram failed');
      valid.remove();
      noLeak();

      const nativeRender = mermaid.render.bind(mermaid);
      let calls = 0;
      mermaid.render = (...args) => { calls++; return nativeRender(...args); };
      const removedBeforeReady = block('graph TD\nA-->B');
      removedBeforeReady.remove(); // switch before ensureMermaid() resumes
      await idle(removedBeforeReady);
      check(calls === 0, 'detached block started rendering');
      document.body.append(removedBeforeReady);
      await idle(removedBeforeReady);
      check(calls === 1 && removedBeforeReady._rendered, 'returning to an unrendered block did not retry');
      removedBeforeReady.remove();
      mermaid.render = nativeRender;

      // Even if a diagram enables Mermaid's error SVG, keep it inside its block.
      const invalid = block('%%{init: {"suppressErrorRendering": false}}%%\ngraph TD\nA[Unclosed label --> B');
      await idle(invalid);
      check(invalid._hasError && invalid._viewMode === 'code', 'invalid diagram lost code fallback');
      check(!invalid.querySelector('.ra-mermaid-diagram svg'), 'error SVG was not cleared');
      invalid.remove();
      noLeak();

      for (const invalid of [false, true]) {
        let release;
        mermaid.render = (...args) => new Promise(resolve => { release = resolve; }).then(() => nativeRender(...args));
        const pending = block(invalid
          ? '%%{init: {"suppressErrorRendering": false}}%%\ngraph TD\nA[Unclosed label --> B'
          : 'graph TD\nA-->B');
        for (let i = 0; i < 10 && !release; i++) await Promise.resolve();
        if (!release) throw Error('render was not queued');
        pending.remove(); // switch sessions while Mermaid has a queued render
        release();
        await idle(pending);
        check(!pending._rendered, 'detached render was marked complete');
        noLeak();
        mermaid.render = nativeRender;
        document.body.append(pending); // returning to the session must retry
        await idle(pending);
        check(pending._rendered && pending._hasError === invalid, 'reconnected block did not recover');
        pending.remove();
        noLeak();
      }
      if (errors.length) throw Error(errors.join('; '));
      document.body.dataset.result = 'passed';
    };
    const src = path.resolve(__dirname, '../internal/static/js/chat/MermaidBlock.js');
    const file = path.join(dir, 'test.html');
    fs.writeFileSync(file, `<body><script src="${pathToFileURL(path.join(dir, 'mermaid.min.js'))}"></script><script src="${pathToFileURL(src)}"></script><script>(${checks})().catch(err => document.body.dataset.result = 'failed:' + err.message)</script></body>`);
    const result = spawnSync(browser, ['--headless', '--disable-gpu', '--no-first-run',
      `--user-data-dir=${path.join(dir, 'profile')}`, '--virtual-time-budget=5000', '--dump-dom', pathToFileURL(file).href],
      { encoding: 'utf8', windowsHide: true, timeout: 30000, maxBuffer: 2 << 20 });
    assert.equal(result.error, undefined, String(result.error));
    assert.equal(result.stdout.match(/data-result="([^"]*)/)?.[1], 'passed', result.stderr);
  } finally {
    assert.equal(path.dirname(dir), os.tmpdir());
    fs.rmSync(dir, { recursive: true, force: true, maxRetries: 10, retryDelay: 100 });
  }
});
