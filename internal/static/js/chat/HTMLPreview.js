// Styled agent output lives in an opaque origin, never in the console's DOM.
customElements.define('ra-html', class extends HTMLElement {
  static get observedAttributes() { return ['data-html-b64']; }
  connectedCallback() { this.render(); }
  attributeChangedCallback() { if (this.isConnected) this.render(); }
  disconnectedCallback() { window.removeEventListener('message', this.onHeight); }
  render() {
    window.removeEventListener('message', this.onHeight);
    let raw;
    try { raw = decodeURIComponent(escape(atob(this.getAttribute('data-html-b64') || ''))); } catch { return; }
    let clean = DOMPurify.sanitize(raw, {
      FORCE_BODY: true, USE_PROFILES: { html: true, svg: true }, ADD_TAGS: ['style'],
      FORBID_TAGS: ['script', 'iframe', 'object', 'embed', 'form', 'input', 'textarea', 'select', 'base', 'link', 'meta'],
      FORBID_ATTR: ['nonce'],
    });
    const content = document.createElement('template');
    content.innerHTML = clean;
    for (const link of content.content.querySelectorAll('a')) {
      link.target = '_blank'; link.rel = 'noopener noreferrer';
    }
    clean = content.innerHTML;
    const frame = document.createElement('iframe');
    frame.title = '內容預覽';
    frame.setAttribute('sandbox', 'allow-scripts allow-popups allow-popups-to-escape-sandbox');
    frame.referrerPolicy = 'no-referrer';
    frame.style.cssText = 'display:block;width:100%;border:0;height:150px;color-scheme:normal';
    const nonce = crypto.randomUUID().replace(/-/g, '');
    const measure = `function report(){parent.postMessage({previewHeight:Math.ceil(document.body.getBoundingClientRect().height)},'*')}report();new ResizeObserver(report).observe(document.body);addEventListener('load',report)`;
    frame.srcdoc = '<!doctype html><html><head><meta charset="utf-8">'
      + `<meta http-equiv="Content-Security-Policy" content="default-src 'none'; script-src 'nonce-${nonce}'; style-src 'unsafe-inline'; img-src data: https: http: blob:; font-src data:; base-uri 'none'; form-action 'none'">`
      + `<style>html,body{margin:0;min-width:0;overflow-wrap:anywhere}body{font:14px/1.8 sans-serif;color:${getComputedStyle(this).color}}svg{max-width:100%;height:auto}</style>`
      + `</head><body>${clean}<script nonce="${nonce}">${measure}</script></body></html>`;
    this.onHeight = event => {
      if (event.source !== frame.contentWindow || !Number.isFinite(event.data?.previewHeight)) return;
      frame.style.height = Math.min(12000, Math.max(24, event.data.previewHeight + 2)) + 'px';
    };
    window.addEventListener('message', this.onHeight);
    this.replaceChildren(frame);
  }
});
