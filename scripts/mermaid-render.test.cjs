const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');

// 1.1 CDN on-demand script loading and dark theme initialization
const mermaidBlockPath = path.join(__dirname, '../internal/static/js/chat/MermaidBlock.js');
assert.ok(fs.existsSync(mermaidBlockPath), 'MermaidBlock.js must exist');

const definedCustomElements = {};
const source = fs.readFileSync(mermaidBlockPath, 'utf8');

// Mock DOM environment for custom elements
class MockElement {
  constructor() {
    this.attributes = {};
    this.children = [];
    this.classList = {
      _classes: new Set(),
      add: (c) => this.classList._classes.add(c),
      remove: (c) => this.classList._classes.delete(c),
      toggle: (c, force) => {
        if (force === undefined) {
          if (this.classList._classes.has(c)) this.classList._classes.delete(c);
          else this.classList._classes.add(c);
        } else if (force) {
          this.classList._classes.add(c);
        } else {
          this.classList._classes.delete(c);
        }
      },
      contains: (c) => this.classList._classes.has(c),
    };
    this.listeners = {};
    this._elements = {};
  }
  getAttribute(name) { return this.attributes[name] || null; }
  setAttribute(name, val) {
    const old = this.attributes[name];
    this.attributes[name] = String(val);
    if (typeof this.attributeChangedCallback === 'function') {
      this.attributeChangedCallback(name, old, String(val));
    }
  }
  set innerHTML(val) { this._innerHTML = val; }
  get innerHTML() { return this._innerHTML || ''; }
  querySelector(sel) {
    if (!this._elements[sel]) {
      this._elements[sel] = new MockElement();
    }
    return this._elements[sel];
  }
  addEventListener(event, fn) {
    this.listeners[event] = this.listeners[event] || [];
    this.listeners[event].push(fn);
  }
  dispatchEvent(evt) {
    const list = this.listeners[evt.type] || [];
    for (const fn of list) fn(evt);
  }
}

class MockCustomEvent {
  constructor(type, opts) {
    this.type = type;
    this.bubbles = opts?.bubbles;
    this.detail = opts?.detail;
  }
}

const context = vm.createContext({
  window: { CustomEvent: MockCustomEvent },
  CustomEvent: MockCustomEvent,
  document: {
    createElement: (tag) => new MockElement(),
    head: { appendChild: () => {} },
  },
  HTMLElement: MockElement,
  customElements: {
    define: (name, cls) => { definedCustomElements[name] = cls; },
    get: (name) => definedCustomElements[name] || null,
  },
  btoa: (s) => Buffer.from(s, 'binary').toString('base64'),
  atob: (s) => Buffer.from(s, 'base64').toString('binary'),
  escape: (s) => encodeURIComponent(s),
  unescape: (s) => decodeURIComponent(s),
});
vm.runInContext(source, context);

const { getMermaidConfig } = context.window;
assert.equal(typeof getMermaidConfig, 'function', 'getMermaidConfig should be exported on window');

const config = getMermaidConfig();
assert.equal(config.securityLevel, 'strict', 'securityLevel must be strict');
assert.equal(config.theme, 'dark', 'theme must be dark');
assert.equal(config.startOnLoad, false, 'startOnLoad must be false');
assert.equal(config.suppressErrorRendering, true, 'suppressErrorRendering must be true');
assert.equal(config.themeVariables.darkMode, true, 'themeVariables.darkMode must be true');

console.log('✓ Task 1.1 tests passed');

// 1.2 Web Component encapsulation for Mermaid blocks
const RaMermaid = context.customElements.get('ra-mermaid');
assert.ok(RaMermaid, '<ra-mermaid> custom element must be defined');

const indexHtml = fs.readFileSync(path.join(__dirname, '../internal/static/index.html'), 'utf8');
assert.ok(indexHtml.includes('MermaidBlock.js'), 'index.html must include MermaidBlock.js');

console.log('✓ Task 1.2 tests passed');

// 2.1 parseMarkdown expands language-mermaid to <ra-mermaid>
const coreSource = fs.readFileSync(path.join(__dirname, '../internal/static/js/core/core.js'), 'utf8');
const coreContext = vm.createContext({
  React: { useState: () => {}, useEffect: () => {}, useLayoutEffect: () => {}, useRef: () => {}, useCallback: () => {}, useMemo: () => {} },
  window: { location: { pathname: '', protocol: 'http:', host: 'localhost' } },
  location: { pathname: '', protocol: 'http:', host: 'localhost' },
  sessionStorage: { getItem: () => null, setItem: () => {}, removeItem: () => {} },
  localStorage: { getItem: () => null, setItem: () => {}, removeItem: () => {} },
  document: { documentElement: { style: { setProperty: () => {} } } },
  marked: {
    setOptions: () => {},
    use: () => {},
    parse: (text) => {
      if (text.includes('```mermaid')) {
        const inner = text.replace(/```mermaid\n?([\s\S]*?)```/, '$1').trim();
        return `<pre><code class="language-mermaid">${inner}</code></pre>`;
      }
      return text;
    },
  },
  hljs: {
    getLanguage: () => false,
    highlightAuto: (code) => ({ value: code }),
  },
  DOMPurify: { sanitize: (s) => s },
  btoa: (s) => Buffer.from(s, 'binary').toString('base64'),
  atob: (s) => Buffer.from(s, 'base64').toString('binary'),
  escape: (s) => encodeURIComponent(s),
  unescape: (s) => decodeURIComponent(s),
});
vm.runInContext(coreSource, coreContext);

const { parseMarkdown } = coreContext;
assert.equal(typeof parseMarkdown, 'function', 'parseMarkdown should be a function');

const markdownDiagram = '```mermaid\nflowchart TD\n    A[Start] --> B[Finish]\n```';
const parsedNonStreaming = parseMarkdown(markdownDiagram);
assert.ok(parsedNonStreaming.includes('<ra-mermaid'), 'parsed html should contain <ra-mermaid>');
assert.ok(parsedNonStreaming.includes('data-streaming="false"'), 'parsed html should have data-streaming="false" when not streaming');
assert.ok(parsedNonStreaming.includes('data-code-b64='), 'parsed html should have data-code-b64');

const parsedStreaming = parseMarkdown(markdownDiagram, { streaming: true });
assert.ok(parsedStreaming.includes('data-streaming="true"'), 'parsed html should have data-streaming="true" when streaming');

console.log('✓ Task 2.1 tests passed');

// 2.2 Streaming protection and deferred rendering
(async () => {
  let renderCalled = false;
  context.window.mermaid = {
    render: async (id, text) => {
      renderCalled = true;
      return { svg: '<svg id="' + id + '"><g>test</g></svg>' };
    },
  };

  const el = new RaMermaid();
  el.setAttribute('data-code-b64', Buffer.from('graph TD\nA-->B').toString('base64'));
  el.setAttribute('data-streaming', 'true');
  el.connectedCallback();

  assert.equal(renderCalled, false, 'renderDiagram must not be called while data-streaming is true');

  // Now transition streaming to false
  el.setAttribute('data-streaming', 'false');
  await new Promise((resolve) => setTimeout(resolve, 50));

  assert.equal(renderCalled, true, 'renderDiagram must be called when data-streaming transitions to false');
  console.log('✓ Task 2.2 tests passed');

  // 3.1 Graceful fallback on syntax error
  context.window.mermaid = {
    render: async () => {
      throw new Error('Parse error on line 2: Syntax error');
    },
  };

  const errEl = new RaMermaid();
  const invalidSyntax = 'graph TD\nA[Unclosed label --> B';
  errEl.setAttribute('data-code-b64', Buffer.from(invalidSyntax).toString('base64'));
  errEl.setAttribute('data-streaming', 'false');
  errEl.connectedCallback();

  await new Promise((resolve) => setTimeout(resolve, 50));

  assert.equal(errEl._hasError, true, 'Element should record hasError flag');
  assert.equal(errEl._viewMode, 'code', 'Element should fall back to code view mode on error');
  assert.equal(errEl._codeTextEl.textContent, invalidSyntax, 'Original code should be preserved in code element');
  assert.equal(errEl._errorBarEl.classList.contains('hidden'), false, 'Error warning bar should be visible');

  console.log('✓ Task 3.1 tests passed');

  // 3.2 Interactive toolbar with code toggle and lightbox zoom
  context.window.mermaid = {
    render: async (id) => ({ svg: '<svg id="' + id + '"><text>flow</text></svg>' }),
  };

  const toolEl = new RaMermaid();
  toolEl.setAttribute('data-code-b64', Buffer.from('graph TD\nA-->B').toString('base64'));
  toolEl.setAttribute('data-streaming', 'false');
  toolEl.connectedCallback();
  await new Promise((resolve) => setTimeout(resolve, 50));

  assert.equal(toolEl._viewMode, 'diagram', 'Initial view mode should be diagram');

  // Toggle to code
  toolEl.toggleView('code');
  assert.equal(toolEl._viewMode, 'code', 'Toggled view mode should be code');
  assert.equal(toolEl._diagramEl.classList.contains('hidden'), true, 'Diagram element should be hidden in code view');
  assert.equal(toolEl._codeEl.classList.contains('hidden'), false, 'Code element should be visible in code view');

  // Toggle back to diagram
  toolEl.toggleView('diagram');
  assert.equal(toolEl._viewMode, 'diagram', 'Toggled back view mode should be diagram');
  assert.equal(toolEl._diagramEl.classList.contains('hidden'), false, 'Diagram element should be visible');

  // Lightbox zoom event dispatch
  let zoomEventReceived = null;
  toolEl.addEventListener('ra-mermaid-zoom', (e) => {
    zoomEventReceived = e.detail;
  });
  toolEl.zoomDiagram();

  assert.ok(zoomEventReceived, 'Zoom event should be dispatched');
  assert.ok(zoomEventReceived.src.startsWith('data:image/svg+xml'), 'Zoom event src should be valid SVG data URI');
  assert.ok(zoomEventReceived.svg.includes('<svg'), 'Zoom event should contain SVG markup');

  console.log('✓ Task 3.2 tests passed');
  console.log('✓ Task 4.1 tests passed: all mermaid integration tests passed');
})().catch((err) => {
  console.error(err);
  process.exit(1);
});
