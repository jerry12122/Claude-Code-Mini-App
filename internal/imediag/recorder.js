var ringCap = 256;

function installImeDiag(win, doc, post) {
  if (win.__imeDiagInstalled) return { duplicate: true };
  Object.defineProperty(win, "__imeDiagInstalled", { value: true });
  var events = [];
  var seq = 0;
  var lastUpdate = 0;
  var tags = { TEXTAREA: 1, INPUT: 1, DIV: 1, BODY: 1 };

  function num(v) {
    return typeof v === "number" && isFinite(v) ? Math.round(v) : 0;
  }

  function meta(el) {
    var out = {
      tag: el && tags[el.tagName] ? el.tagName : "other",
      connected: !!(el && el.isConnected),
      editable: !!(el && (el.tagName === "TEXTAREA" || el.tagName === "INPUT" || el.isContentEditable === true)),
      vw: num(win.innerWidth),
      vh: num(win.innerHeight),
      dpr: typeof win.devicePixelRatio === "number" && isFinite(win.devicePixelRatio) ? win.devicePixelRatio : 0,
    };
    if (el && typeof el.getBoundingClientRect === "function") {
      var r = el.getBoundingClientRect();
      if (r) out.rect = { x: num(r.x), y: num(r.y), w: num(r.width), h: num(r.height) };
    }
    return out;
  }

  function push(kind, el, extra) {
    var ev = meta(el);
    ev.t = Date.now();
    ev.seq = ++seq;
    ev.kind = kind;
    if (extra) {
      ev.phase = extra.phase;
      ev.side = extra.side;
      ev.defaultPrevented = extra.defaultPrevented;
    }
    events.push(ev);
    if (events.length > ringCap) events.shift();
  }

  function onComp(e) {
    var kind = e && e.type;
    if (kind !== "compositionstart" && kind !== "compositionend" && kind !== "compositionupdate") return;
    if (kind === "compositionupdate") {
      var t = Date.now();
      if (t - lastUpdate < 250) return;
      lastUpdate = t;
    }
    push(kind, e.target);
  }

  function onKey(e) {
    if (!e) return;
    if (e.key !== "Shift" && e.code !== "ShiftLeft" && e.code !== "ShiftRight") return;
    var side = e.code === "ShiftLeft" ? "left" : e.code === "ShiftRight" ? "right" : "unspecified";
    push("shift", e.target, {
      phase: e.type === "keyup" ? "up" : "down",
      side: side,
      defaultPrevented: e.defaultPrevented === true,
    });
  }

  doc.addEventListener("focusin", function (e) { push("focus", e && e.target); });
  doc.addEventListener("focusout", function (e) { push("blur", e && e.target); });
  doc.addEventListener("compositionstart", onComp);
  doc.addEventListener("compositionend", onComp);
  doc.addEventListener("compositionupdate", onComp);
  doc.addEventListener("keydown", onKey);
  doc.addEventListener("keyup", onKey);
  win.addEventListener("resize", function () { push("resize", null); });
  function pushWindow(kind) {
    var ev = meta(doc.activeElement || null);
    ev.t = Date.now();
    ev.seq = ++seq;
    ev.kind = kind;
    ev.documentHasFocus = typeof doc.hasFocus === "function" && doc.hasFocus() === true;
    events.push(ev);
    if (events.length > ringCap) events.shift();
  }
  win.addEventListener("blur", function () { pushWindow("window-blur"); });
  win.addEventListener("focus", function () { pushWindow("window-focus"); });

  function freeze() {
    var payload = { frozenBeforeExportUI: true, events: events.slice() };
    var text = "ime-diag:" + JSON.stringify(payload);
    if (text.length > 65536) {
      payload = { frozenBeforeExportUI: true, truncated: true, events: payload.events.slice(-128) };
      text = "ime-diag:" + JSON.stringify(payload);
      if (text.length > 65536) {
        payload = { frozenBeforeExportUI: true, truncated: true, events: [] };
        text = "ime-diag:" + JSON.stringify(payload);
      }
    }
    try { post(text); } catch (err) {}
    return payload;
  }

  Object.defineProperty(win, "__imeDiagFreeze", { configurable: true, value: freeze });
  return { duplicate: false, freeze: freeze };
}

function imeDiagAuto(win) {
  if (!win || win.top !== win) return false;
  var loc = win.location || {};
  if (loc.pathname !== "/" || !win.__imeDiagOwner || loc.origin !== win.__imeDiagOwner) return false;
  var wv = win.chrome && win.chrome.webview;
  if (!wv || typeof wv.postMessage !== "function") return false;
  if (win.__imeDiagInstalled) return true;
  installImeDiag(win, win.document, function (payload) { wv.postMessage(payload); });
  return true;
}
