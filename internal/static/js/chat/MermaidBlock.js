// MermaidBlock.js: 負責 Mermaid 函式庫載入、主題配置與 <ra-mermaid> 自定義 Web Component
(function() {
  function getMermaidConfig() {
    return {
      startOnLoad: false,
      securityLevel: 'strict',
      theme: 'dark',
      suppressErrorRendering: true,
      themeVariables: {
        darkMode: true,
        background: 'oklch(0.16 0.02 264)',
        primaryColor: 'oklch(0.62 0.19 275)',
        primaryTextColor: 'oklch(0.94 0.01 264)',
        primaryBorderColor: 'oklch(0.35 0.02 264)',
        lineColor: 'oklch(0.62 0.19 275)',
        secondaryColor: 'oklch(0.24 0.02 264)',
        tertiaryColor: 'oklch(0.19 0.02 264)',
        fontFamily: 'Manrope, JetBrains Mono, sans-serif',
        fontSize: '13px',
      },
    };
  }

  let mermaidLoadPromise = null;

  function ensureMermaid() {
    if (typeof window === 'undefined') return Promise.reject(new Error('no window'));
    if (window.mermaid) return Promise.resolve(window.mermaid);
    if (mermaidLoadPromise) return mermaidLoadPromise;

    mermaidLoadPromise = new Promise((resolve, reject) => {
      const script = document.createElement('script');
      script.src = 'https://cdn.jsdelivr.net/npm/mermaid@11/dist/mermaid.min.js';
      script.async = true;
      script.onload = () => {
        if (window.mermaid) {
          try {
            window.mermaid.initialize(getMermaidConfig());
          } catch (_) {}
          resolve(window.mermaid);
        } else {
          reject(new Error('Mermaid script loaded but window.mermaid is undefined'));
        }
      };
      script.onerror = (err) => {
        mermaidLoadPromise = null;
        reject(err || new Error('Failed to load Mermaid from CDN'));
      };
      if (document && document.head && document.head.appendChild) {
        document.head.appendChild(script);
      }
    });

    return mermaidLoadPromise;
  }

  let mermaidCounter = 0;

  class RaMermaidElement extends HTMLElement {
    static get observedAttributes() {
      return ['data-streaming', 'data-code-b64'];
    }

    constructor() {
      super();
      this._rendered = false;
      this._rendering = false;
      this._viewMode = 'diagram'; // 'diagram' | 'code'
      this._rawCode = '';
      this._svgContent = '';
      this._hasError = false;
    }

    connectedCallback() {
      this._updateRawCode();
      this._renderDOM();
      if (this.getAttribute('data-streaming') !== 'true') {
        this.renderDiagram();
      }
    }

    attributeChangedCallback(name, oldValue, newValue) {
      if (oldValue === newValue) return;
      if (name === 'data-code-b64') {
        this._updateRawCode();
        if (this._rendered) {
          this._rendered = false;
          this.renderDiagram();
        }
      } else if (name === 'data-streaming') {
        if (newValue !== 'true' && !this._rendered && !this._rendering) {
          this.renderDiagram();
        }
      }
    }

    _updateRawCode() {
      const b64 = this.getAttribute('data-code-b64');
      if (b64) {
        try {
          this._rawCode = decodeURIComponent(escape(atob(b64)));
          return;
        } catch (_) {}
      }
      this._rawCode = '';
    }

    _renderDOM() {
      if (this._hasRenderedDOM) return;
      this._hasRenderedDOM = true;

      this.className = 'ra-mermaid-container block my-3 rounded-lg border border-[oklch(0.28_0.02_264)] bg-[oklch(0.16_0.02_264)] overflow-hidden text-left';
      this.innerHTML = `
        <div class="ra-mermaid-toolbar flex items-center justify-between px-3 py-1.5 bg-[oklch(0.19_0.02_264)] border-b border-[oklch(0.28_0.02_264)] text-xs text-[oklch(0.7_0.02_264)] select-none">
          <div class="flex items-center gap-1.5 font-semibold text-[oklch(0.85_0.01_264)]">
            <svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" stroke-width="2"><line x1="6" y1="3" x2="6" y2="15"></line><circle cx="18" cy="6" r="3"></circle><circle cx="6" cy="18" r="3"></circle><path d="M18 9a9 9 0 0 1-9 9"></path></svg>
            <span>Mermaid</span>
          </div>
          <div class="flex items-center gap-1">
            <button type="button" class="btn-toggle-view px-2 py-0.5 rounded hover:bg-[oklch(0.25_0.02_264)] text-[oklch(0.8_0.01_264)] transition-colors">原始碼</button>
            <button type="button" class="btn-zoom-view p-1 rounded hover:bg-[oklch(0.25_0.02_264)] text-[oklch(0.8_0.01_264)] transition-colors" title="放大檢視" aria-label="放大檢視">
              <svg viewBox="0 0 24 24" width="13" height="13" fill="none" stroke="currentColor" stroke-width="2"><circle cx="11" cy="11" r="8"></circle><line x1="21" y1="21" x2="16.65" y2="16.65"></line><line x1="11" y1="8" x2="11" y2="14"></line><line x1="8" y1="11" x2="14" y2="11"></line></svg>
            </button>
            <button type="button" class="btn-copy-code p-1 rounded hover:bg-[oklch(0.25_0.02_264)] text-[oklch(0.8_0.01_264)] transition-colors" title="複製原始碼" aria-label="複製">
              <svg class="icon-copy" viewBox="0 0 24 24" width="13" height="13" fill="none" stroke="currentColor" stroke-width="2"><rect x="9" y="9" width="12" height="12" rx="2"/><path d="M5 15V5a2 2 0 0 1 2-2h10"/></svg>
              <svg class="icon-check" viewBox="0 0 24 24" width="13" height="13" fill="none" stroke="currentColor" stroke-width="2" style="display:none;color:oklch(0.75 0.15 145)"><path d="M20 6 9 17l-5-5"/></svg>
            </button>
          </div>
        </div>
        <div class="ra-mermaid-error hidden text-amber-300 text-xs px-3 py-1 bg-amber-950/40 border-b border-amber-900/50 flex items-center gap-1.5">
          <span>⚠️ Mermaid 語法解析失敗，已切換回原始碼</span>
        </div>
        <div class="ra-mermaid-diagram p-4 overflow-x-auto flex items-center justify-center min-h-[80px] cursor-zoom-in">
          <span class="text-xs text-[oklch(0.55_0.01_264)] italic">繪製圖表中...</span>
        </div>
        <div class="ra-mermaid-code hidden p-3 overflow-x-auto text-xs ra-mono bg-[oklch(0.14_0.02_264)]">
          <pre class="m-0 p-0 bg-transparent border-0"><code class="language-mermaid hljs"></code></pre>
        </div>
      `;

      this._errorBarEl = this.querySelector('.ra-mermaid-error');
      this._diagramEl = this.querySelector('.ra-mermaid-diagram');
      this._codeEl = this.querySelector('.ra-mermaid-code');
      this._codeTextEl = this.querySelector('.language-mermaid');
      this._btnToggleEl = this.querySelector('.btn-toggle-view');

      this.addEventListener('click', (e) => {
        const t = e.target.closest ? e.target.closest('button, .ra-mermaid-diagram') : null;
        if (!t) return;
        if (t.classList.contains('btn-toggle-view')) {
          this.toggleView();
        } else if (t.classList.contains('btn-copy-code')) {
          e.stopPropagation();
          this.copyCode(t);
        } else if (t.classList.contains('btn-zoom-view') || (t.classList.contains('ra-mermaid-diagram') && this._svgContent && !this._hasError && this._viewMode === 'diagram')) {
          e.stopPropagation();
          this.zoomDiagram();
        }
      });
    }

    toggleView(targetMode) {
      this._viewMode = targetMode || (this._viewMode === 'diagram' ? 'code' : 'diagram');
      const showCode = this._viewMode === 'code' || this._hasError;
      if (this._diagramEl) this._diagramEl.classList.toggle('hidden', showCode);
      if (this._codeEl) this._codeEl.classList.toggle('hidden', !showCode);
      if (this._btnToggleEl) this._btnToggleEl.textContent = showCode ? '圖表' : '原始碼';
    }

    copyCode(btn) {
      if (!this._rawCode) return false;
      const success = typeof window.copyTextExecCommand === 'function' ? window.copyTextExecCommand(this._rawCode) : false;
      if (btn && success && btn.querySelector) {
        const iconCopy = btn.querySelector('.icon-copy');
        const iconCheck = btn.querySelector('.icon-check');
        if (iconCopy) iconCopy.style.display = 'none';
        if (iconCheck) iconCheck.style.display = '';
        clearTimeout(btn._copyTimer);
        btn._copyTimer = setTimeout(() => {
          if (iconCopy) iconCopy.style.display = '';
          if (iconCheck) iconCheck.style.display = 'none';
        }, 2000);
      }
      return success;
    }

    zoomDiagram() {
      if (!this._svgContent || this._hasError) return;
      const dataUri = 'data:image/svg+xml;charset=utf-8,' + encodeURIComponent(this._svgContent);
      this.dispatchEvent(new CustomEvent('ra-mermaid-zoom', {
        bubbles: true,
        composed: true,
        detail: { src: dataUri, svg: this._svgContent },
      }));
    }

    async renderDiagram() {
      if (this._rendered || this._rendering) return;
      this._renderDOM();
      this._updateRawCode();
      if (!this._rawCode || !this._rawCode.trim()) return;

      if (this._codeTextEl) {
        this._codeTextEl.textContent = this._rawCode;
      }

      this._rendering = true;
      try {
        const mermaid = await ensureMermaid();
        const id = `mermaid-svg-${Date.now()}-${++mermaidCounter}`;
        const { svg } = await mermaid.render(id, this._rawCode.trim());
        this._svgContent = svg;
        this._hasError = false;
        this._rendered = true;

        if (this._diagramEl) {
          this._diagramEl.innerHTML = svg;
          this._diagramEl.classList.remove('hidden');
        }
        if (this._errorBarEl) {
          this._errorBarEl.classList.add('hidden');
        }
        this.toggleView('diagram');
      } catch (err) {
        this._hasError = true;
        this._rendered = true;
        if (this._errorBarEl) {
          this._errorBarEl.classList.remove('hidden');
        }
        this.toggleView('code');
      } finally {
        this._rendering = false;
      }
    }
  }

  if (typeof customElements !== 'undefined' && !customElements.get('ra-mermaid')) {
    customElements.define('ra-mermaid', RaMermaidElement);
  }

  if (typeof window !== 'undefined') {
    window.getMermaidConfig = getMermaidConfig;
    window.ensureMermaid = ensureMermaid;
    window.RaMermaidElement = RaMermaidElement;
  }
})();
