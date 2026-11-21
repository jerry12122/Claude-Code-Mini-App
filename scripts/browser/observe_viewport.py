"""Android 08 的有界視窗觀測。只讀 DOM，不改斷言。無 Playwright 依賴。"""
import json
from pathlib import Path

TOLERANCE_PX = 1.0
STABLE_AFTER_MS = 100.0
LATE_MS = 1400.0
DEADLINE_MS = 1500.0
OBSERVATION_NAME = "08-viewport-observation.json"
DISTURBANCE = (
    "ARM 返回前只做 blur 與掛上 listener、rAF、deadline，不同步讀版面。"
    "排定的 rAF 仍可能在 capture RPC 之前讀版面，不是截圖前零讀取。"
    "原 blur 的同步 focusout 在 listener 之前，沒有被觀測到；只記錄 blur 前後時間。"
    "這輪沒裁切只記 0/1，不改原斷言。"
)
_CSS_VARS = {"--tg-viewport-height", "--ra-vvh", "--ra-vvt", "--ra-safe-bottom"}
_ALLOW = {
    "category", "reason", "reproduced", "disturbance", "cssnote", "fallbackassumed",
    "windowcomplete", "earlystablepairisconvergence", "terminal", "nonterminalearlypair",
    "deadlinems", "armedutcms", "elapsedms", "stopreason", "baseline", "samples", "events",
    "stage13", "stage13snapshot", "sampling", "blur", "tms", "utcms", "innerwidth",
    "innerheight", "scrolly", "visibility", "vv", "active", "rakbopen", "vars", "happ",
    "happs", "main", "body", "html", "textarea", "composer", "ancestors", "type", "tag",
    "inputtype", "classname", "classtokens", "x", "y", "width", "height", "bottom",
    "computedheight", "minheight", "paddingbottom", "offsettop", "offsetleft", "root",
    "recorded", "vvheight", "vvoffsettop", "controly", "controlheight", "controlbottom",
    "clipped", "overflowpx", "shellheight", "shellminusinner", "cssvarsrecorded", "cssvars",
    "compared", "ydelta", "scrolldelta13minusobs", "scrollexplainsydelta", "cssvarsequal",
    "innerheightsmatch", "conflict", "strategy", "cap", "droppedearly", "tbeforems",
    "tafterms", "utcbeforems", "utcafterms", "originalfocusoutobserved",
}
_STABLE_KEYS = ("inner_height", "vv_height", "scroll_y", "bottom")

ARM_JS = r"""() => {
  const origin = performance.now();
  const utcBefore = Date.now();
  const blurBefore = performance.now();
  document.activeElement?.blur();
  const blurAfter = performance.now();
  const utcAfter = Date.now();
  const blurInfo = {
    t_before_ms: blurBefore - origin,
    t_after_ms: blurAfter - origin,
    utc_before_ms: utcBefore,
    utc_after_ms: utcAfter,
    original_focusout_observed: false
  };
  try {
    if (window.__cmaAndroidObs) return null;
    const t0 = performance.now();
    const utc0 = Date.now();
    const samples = [];
    const events = [];
    let dropped = 0;
    let raf = 0;
    let timer = 0;
    let done = false;
    function stamp() { return { t_ms: performance.now() - t0, utc_ms: Date.now() }; }
    function pushEvent(type, extra) {
      if (events.length >= 40) return;
      const row = stamp();
      row.type = type;
      if (extra) { row.tag = extra.tag || ""; row.inputType = extra.inputType || ""; }
      events.push(row);
    }
    function onResize() { pushEvent("resize"); }
    function onVv() { pushEvent("visualViewport.resize"); }
    function onFocusOut(ev) {
      const node = ev && ev.target;
      pushEvent("focusout", {
        tag: node && node.tagName ? node.tagName : "",
        inputType: node && node.getAttribute ? (node.getAttribute("type") || "") : ""
      });
    }
    function varsOf(el) {
      if (!el || !el.tagName) return null;
      const style = getComputedStyle(el);
      return {
        "--tg-viewport-height": style.getPropertyValue("--tg-viewport-height"),
        "--ra-vvh": style.getPropertyValue("--ra-vvh"),
        "--ra-vvt": style.getPropertyValue("--ra-vvt"),
        "--ra-safe-bottom": style.getPropertyValue("--ra-safe-bottom")
      };
    }
    function box(el) {
      if (!el || !el.tagName) return null;
      const rect = el.getBoundingClientRect();
      const style = getComputedStyle(el);
      return {
        tag: el.tagName,
        className: (el.className && String(el.className).slice(0, 80)) || "",
        x: rect.x, y: rect.y, width: rect.width, height: rect.height, bottom: rect.bottom,
        computedHeight: style.height, minHeight: style.minHeight, paddingBottom: style.paddingBottom
      };
    }
    function remember(row) {
      while (samples.length >= 96) { samples.shift(); dropped += 1; }
      samples.push(row);
    }
    function sample(reason) {
      const root = document.documentElement;
      const body = document.body;
      const main = document.querySelector("main");
      const ta = document.querySelector("textarea[data-chat-input]");
      const active = document.activeElement;
      const vv = window.visualViewport;
      const ancestors = [];
      let node = ta;
      for (let i = 0; node && i < 12; i += 1) { ancestors.push(box(node)); node = node.parentElement; }
      const apps = [];
      const found = document.querySelectorAll(".h-app");
      for (let i = 0; i < found.length && i < 6; i += 1) apps.push(box(found[i]));
      remember({
        reason: reason,
        t_ms: performance.now() - t0,
        utc_ms: Date.now(),
        innerWidth: window.innerWidth,
        innerHeight: window.innerHeight,
        scrollY: window.scrollY,
        visibility: document.visibilityState,
        vv: vv ? { height: vv.height, offsetTop: vv.offsetTop, offsetLeft: vv.offsetLeft } : null,
        active: active && active.tagName ? { tag: active.tagName, inputType: active.getAttribute ? (active.getAttribute("type") || "") : "" } : null,
        raKbOpen: !!(root && root.classList && root.classList.contains("ra-kb-open")),
        vars: { root: varsOf(root), body: varsOf(body), main: varsOf(main) },
        hApp: box(document.querySelector(".h-app")),
        hApps: apps,
        main: box(main),
        body: box(body),
        html: box(root),
        textarea: box(ta),
        composer: box(document.querySelector(".ra-chat-composer")),
        ancestors: ancestors
      });
    }
    function finish(reason) {
      if (done) return window.__cmaAndroidObs.result;
      done = true;
      cancelAnimationFrame(raf);
      clearTimeout(timer);
      if (window.__cmaAndroidObs && window.__cmaAndroidObs.unlisten) window.__cmaAndroidObs.unlisten();
      pushEvent("stop", { tag: reason, inputType: "" });
      const result = {
        armed_utc_ms: utc0, elapsed_ms: performance.now() - t0, stop_reason: reason,
        samples: samples, events: events, blur: blurInfo,
        sampling: { strategy: "ring-keep-tail", cap: 96, dropped_early: dropped }
      };
      window.__cmaAndroidObs.result = result;
      const callback = window.__cmaAndroidObs.whenDone;
      window.__cmaAndroidObs.whenDone = null;
      if (callback) callback(result);
      return result;
    }
    function frame() {
      if (done) return;
      if (performance.now() - t0 >= 1500) { sample("deadline"); finish("deadline"); return; }
      sample("raf");
      raf = requestAnimationFrame(frame);
    }
    function unlisten() {
      window.removeEventListener("resize", onResize);
      if (window.visualViewport) window.visualViewport.removeEventListener("resize", onVv);
      document.removeEventListener("focusout", onFocusOut, true);
    }
    window.__cmaAndroidObs = { finish: finish, whenDone: null, result: null, unlisten: unlisten };
    window.addEventListener("resize", onResize);
    if (window.visualViewport) window.visualViewport.addEventListener("resize", onVv);
    document.addEventListener("focusout", onFocusOut, true);
    timer = setTimeout(function () { if (!done) sample("deadline"); finish("deadline"); }, 1500);
    raf = requestAnimationFrame(frame);
  } catch (err) {
    if (window.__cmaAndroidObs && window.__cmaAndroidObs.unlisten) window.__cmaAndroidObs.unlisten();
    window.__cmaAndroidObs = { result: { error: "arm-failed", samples: [], events: [] }, finish: function () { return window.__cmaAndroidObs.result; }, whenDone: null };
  }
  return null;
}"""

COLLECT_JS = r"""() => {
  const obs = window.__cmaAndroidObs;
  if (!obs) return { missing: true, samples: [], events: [] };
  if (obs.result) return obs.result;
  return new Promise((resolve) => {
    const cap = setTimeout(() => resolve(obs.finish("collect-deadline")), 1600);
    obs.whenDone = (result) => { clearTimeout(cap); resolve(result); };
  });
}"""

STAGE13_JS = r"""() => {
  const root = document.documentElement;
  const body = document.body;
  const main = document.querySelector("main");
  const ta = document.querySelector("textarea[data-chat-input]");
  const rect = ta ? ta.getBoundingClientRect() : null;
  const style = ta ? getComputedStyle(ta) : null;
  function varsOf(el) {
    if (!el || !el.tagName) return null;
    const computed = getComputedStyle(el);
    return {
      "--tg-viewport-height": computed.getPropertyValue("--tg-viewport-height"),
      "--ra-vvh": computed.getPropertyValue("--ra-vvh"),
      "--ra-vvt": computed.getPropertyValue("--ra-vvt"),
      "--ra-safe-bottom": computed.getPropertyValue("--ra-safe-bottom")
    };
  }
  const vv = window.visualViewport;
  return {
    utc_ms: Date.now(),
    innerWidth: window.innerWidth,
    innerHeight: window.innerHeight,
    scrollY: window.scrollY,
    raKbOpen: !!(root && root.classList && root.classList.contains("ra-kb-open")),
    classTokens: (function (el) {
      if (!el || !el.classList || !el.classList.contains) return "";
      const names = ["ra-kb-open", "h-app", "ra-body"];
      const kept = [];
      for (let i = 0; i < names.length; i += 1) if (el.classList.contains(names[i])) kept.push(names[i]);
      return kept.join(" ");
    })(root),
    vv: vv ? { height: vv.height, offsetTop: vv.offsetTop, offsetLeft: vv.offsetLeft } : null,
    textarea: rect ? { x: rect.x, y: rect.y, width: rect.width, height: rect.height, bottom: rect.bottom, computedHeight: style.height } : null,
    vars: { root: varsOf(root), body: varsOf(body), main: varsOf(main) }
  };
}"""


def _keep_key(key):
    text = str(key)
    if text in _CSS_VARS:
        return True
    return text.lower().replace("_", "") in _ALLOW


def sanitize(node):
    if isinstance(node, dict):
        clean = {}
        for key, value in node.items():
            if not _keep_key(key):
                continue
            clean[key] = sanitize(value)
        return clean
    if isinstance(node, list):
        return [sanitize(item) for item in node]
    if isinstance(node, str) and len(node) > 160:
        return node[:160]
    return node


def _num(frame, key):
    value = frame.get(key) if isinstance(frame, dict) else None
    if isinstance(value, bool) or not isinstance(value, (int, float)):
        return None
    return float(value)


def _px(text):
    if not isinstance(text, str) or not text.endswith("px"):
        return None
    try:
        return float(text[:-2])
    except ValueError:
        return None


def frames_from_samples(samples):
    frames = []
    for sample in samples or []:
        if not isinstance(sample, dict):
            continue
        textarea = sample.get("textarea") or {}
        shell = sample.get("hApp") or {}
        visual = sample.get("vv") or {}
        frames.append({
            "t_ms": sample.get("t_ms"),
            "inner_height": sample.get("innerHeight"),
            "vv_height": visual.get("height") if isinstance(visual, dict) else None,
            "scroll_y": sample.get("scrollY"),
            "bottom": textarea.get("bottom") if isinstance(textarea, dict) else None,
            "y": textarea.get("y") if isinstance(textarea, dict) else None,
            "shell_height": _px(shell.get("computedHeight")) if isinstance(shell, dict) else None,
            "css_vars": sample.get("vars") if isinstance(sample.get("vars"), dict) else None,
            "reason": sample.get("reason"),
        })
    return frames


def _delta(left, right, key):
    a, b = _num(left, key), _num(right, key)
    if a is None or b is None:
        return None
    return abs(a - b)


def _stable_pair(left, right):
    # 合成幀不帶 reason。實測只有連續兩個 rAF 能當穩定配對，deadline 樣本不能冒充。
    if left.get("reason") not in (None, "raf") or right.get("reason") not in (None, "raf"):
        return False
    if (_num(right, "t_ms") or -1) < STABLE_AFTER_MS:
        return False
    for key in _STABLE_KEYS:
        delta = _delta(left, right, key)
        if delta is None or delta > TOLERANCE_PX:
            return False
    if _num(left, "shell_height") is not None and _num(right, "shell_height") is not None:
        if _delta(left, right, "shell_height") > TOLERANCE_PX:
            return False
    return True


def _moves(anchor, frame):
    for key in _STABLE_KEYS:
        delta = _delta(anchor, frame, key)
        if delta is None or delta > TOLERANCE_PX:
            return True
    return False


def _clipped(frame):
    bottom, height = _num(frame, "bottom"), _num(frame, "inner_height")
    if bottom is None or height is None:
        return None
    return bottom > height + TOLERANCE_PX


def _terminal(frames):
    found = None
    early = None
    for index in range(len(frames) - 1):
        left, right = frames[index], frames[index + 1]
        if not _stable_pair(left, right):
            continue
        if any(_moves(right, later) for later in frames[index + 2:]):
            early = early or {"t_ms": _num(right, "t_ms")}
            continue
        if (_num(right, "t_ms") or 0) < LATE_MS:
            early = early or {"t_ms": _num(right, "t_ms")}
            continue
        found = right
    return found, early


def _times_ok(frames):
    times = [_num(frame, "t_ms") for frame in frames]
    if any(item is None for item in times):
        return False
    return all(times[index] <= times[index + 1] for index in range(len(times) - 1))


def _geometry(frame):
    if not frame:
        return None
    inner = _num(frame, "inner_height")
    bottom = _num(frame, "bottom")
    shell = _num(frame, "shell_height")
    return {
        "t_ms": _num(frame, "t_ms"),
        "bottom": bottom,
        "inner_height": inner,
        "overflow_px": None if bottom is None or inner is None else bottom - inner,
        "scroll_y": _num(frame, "scroll_y"),
        "y": _num(frame, "y"),
        "shell_height": shell,
        "shell_minus_inner": None if shell is None or inner is None else shell - inner,
        "css_vars_recorded": isinstance(frame.get("css_vars"), dict),
        "fallback_assumed": False,
    }


def classify_frames(frames, baseline_clipped=None, stage13=None):
    """合成幀或實測幀皆可。空的 CSS 變數保持空，不改寫成 100svh。"""
    result = {
        "early_stable_pair_is_convergence": False,
        "fallback_assumed": False,
        "window_complete": False,
        "terminal": None,
        "nonterminal_early_pair": None,
    }
    if not _times_ok(frames):
        result["category"] = "證據矛盾"
        result["reason"] = "時間序不單調或缺少 t_ms"
        return result
    last_t = _num(frames[-1], "t_ms") if frames else None
    result["window_complete"] = last_t is not None and last_t >= LATE_MS
    terminal, early = _terminal(frames)
    result["nonterminal_early_pair"] = early
    result["terminal"] = _geometry(terminal)
    sample_clip = any(_clipped(frame) is True for frame in frames)
    seen_clip = sample_clip or baseline_clipped is True
    if terminal is None or not result["window_complete"]:
        if result["window_complete"] and not seen_clip and baseline_clipped is False:
            result["category"] = "整輪未重現"
            result["reproduced"] = "0/1"
            return result
        result["category"] = "1500ms未穩定"
        result["reason"] = "沒有跑到末段仍成立的穩定配對"
        return result
    if _clipped(terminal) is None:
        result["category"] = "證據矛盾"
        result["reason"] = "末段缺 bottom 或 innerHeight"
        return result
    if _clipped(terminal):
        result["category"] = "穩定仍裁切"
    elif seen_clip:
        result["category"] = "只瞬態裁切"
    else:
        result["category"] = "整輪未重現"
        result["reproduced"] = "0/1"
    comparison = _compare_stage13(terminal, stage13)
    result["stage13"] = comparison
    if comparison.get("conflict"):
        result["category"] = "證據矛盾"
        result["reason"] = "末段與 13 的 scroll、CSS 變數都已記錄且相同，底邊仍不一致"
    return result


def _vars_equal(left, right):
    if not isinstance(left, dict) or not isinstance(right, dict):
        return None
    keys = ("root", "body", "main")
    if any(not isinstance(left.get(key), dict) or not isinstance(right.get(key), dict) for key in keys):
        return None
    return all(left[key] == right[key] for key in keys)


def _compare_stage13(terminal, stage13):
    if not isinstance(stage13, dict):
        return {"compared": False, "reason": "13 尚未量測", "fallback_assumed": False}
    scroll = _num(terminal, "scroll_y")
    scroll13 = stage13.get("scrollY")
    if isinstance(scroll13, bool) or not isinstance(scroll13, (int, float)):
        return {"compared": False, "reason": "scroll 未記錄", "fallback_assumed": False}
    box = stage13.get("textarea") if isinstance(stage13.get("textarea"), dict) else {}
    y13 = box.get("y")
    y = _num(terminal, "y")
    if y is None or isinstance(y13, bool) or not isinstance(y13, (int, float)):
        return {"compared": False, "reason": "y 未記錄", "fallback_assumed": False}
    inner13 = stage13.get("innerHeight")
    bottom13 = box.get("bottom")
    css_equal = _vars_equal(terminal.get("css_vars"), stage13.get("vars"))
    explains = abs((y - float(y13)) - (float(scroll13) - scroll)) <= 2
    inner_match = (
        _num(terminal, "inner_height") is not None
        and isinstance(inner13, (int, float))
        and abs(_num(terminal, "inner_height") - float(inner13)) <= TOLERANCE_PX
    )
    stage13_clipped = (
        isinstance(bottom13, (int, float))
        and isinstance(inner13, (int, float))
        and float(bottom13) > float(inner13) + TOLERANCE_PX
    )
    conflict = (
        css_equal is True
        and not explains
        and inner_match
        and _clipped(terminal) is True
        and stage13_clipped is False
    )
    return {
        "compared": True,
        "y_delta": y - float(y13),
        "scroll_delta_13_minus_obs": float(scroll13) - scroll,
        "scroll_explains_y_delta": explains,
        "css_vars_recorded": css_equal is not None,
        "css_vars_equal": css_equal,
        "inner_heights_match": inner_match,
        "conflict": conflict,
        "fallback_assumed": False,
    }


def _baseline(folder):
    path = Path(folder) / "08-chat-empty-dark.json"
    if not path.exists():
        return {"clipped": None, "public": {"recorded": False}}
    raw = json.loads(path.read_text(encoding="utf-8"))
    control = raw.get("control") if isinstance(raw.get("control"), dict) else {}
    visual = raw.get("visualViewport") if isinstance(raw.get("visualViewport"), dict) else {}
    height = raw.get("height")
    bottom = None
    if isinstance(control.get("y"), (int, float)) and isinstance(control.get("height"), (int, float)):
        bottom = float(control["y"]) + float(control["height"])
    clipped = None
    if bottom is not None and isinstance(height, (int, float)):
        clipped = bottom > float(height) + TOLERANCE_PX
    return {
        "clipped": clipped,
        "public": {
            "recorded": True,
            "inner_width": raw.get("width"),
            "inner_height": height,
            "vv_height": visual.get("height"),
            "vv_offset_top": visual.get("offsetTop"),
            "control_y": control.get("y"),
            "control_height": control.get("height"),
            "control_bottom": bottom,
            "clipped": clipped,
        },
    }


def _dump(folder, report):
    path = Path(folder) / OBSERVATION_NAME
    path.write_text(json.dumps(sanitize(report), ensure_ascii=False, indent=2), encoding="utf-8")
    return path


def write_report(folder, raw):
    raw = raw if isinstance(raw, dict) else {"error": "missing", "samples": [], "events": []}
    base = _baseline(folder)
    frames = frames_from_samples(raw.get("samples"))
    if raw.get("missing") or raw.get("error"):
        report = {
            "category": "證據矛盾",
            "reason": "arm-failed" if raw.get("error") else "觀測未掛上",
            "early_stable_pair_is_convergence": False,
            "fallback_assumed": False,
            "window_complete": False,
        }
    else:
        report = classify_frames(frames, baseline_clipped=base["clipped"])
    report.update({
        "disturbance": DISTURBANCE,
        "deadline_ms": DEADLINE_MS,
        "armed_utc_ms": raw.get("armed_utc_ms"),
        "elapsed_ms": raw.get("elapsed_ms"),
        "stop_reason": raw.get("stop_reason"),
        "baseline": base["public"],
        "samples": raw.get("samples") or [],
        "events": raw.get("events") or [],
        "blur": raw.get("blur"),
        "sampling": raw.get("sampling"),
        "stage13": report.get("stage13") or {"compared": False, "reason": "13 尚未量測", "fallback_assumed": False},
        "css_note": "computed 變數以讀到的字串為準；空字串不代表未賦值，也不改寫成 100svh。",
        "fallback_assumed": False,
    })
    _dump(folder, report)
    return report


def add_stage13(folder, snap):
    path = Path(folder) / OBSERVATION_NAME
    current = json.loads(path.read_text(encoding="utf-8")) if path.exists() else {}
    if current.get("reason") in ("collect-failed", "arm-failed", "觀測未掛上") and not current.get("samples"):
        current["stage13"] = {
            "compared": False,
            "reason": "08 觀測失敗，不拿 13 改判" if isinstance(snap, dict) else "13 讀取失敗",
            "fallback_assumed": False,
        }
        if isinstance(snap, dict):
            current["stage13_snapshot"] = snap
        _dump(folder, current)
        return current
    frames = frames_from_samples(current.get("samples"))
    base_clipped = (current.get("baseline") or {}).get("clipped")
    report = classify_frames(frames, baseline_clipped=base_clipped, stage13=snap if isinstance(snap, dict) else None)
    if not isinstance(snap, dict):
        report = classify_frames(frames, baseline_clipped=base_clipped)
        report["stage13"] = {"compared": False, "reason": "13 讀取失敗", "fallback_assumed": False}
    report.update({
        "disturbance": DISTURBANCE,
        "deadline_ms": DEADLINE_MS,
        "armed_utc_ms": current.get("armed_utc_ms"),
        "elapsed_ms": current.get("elapsed_ms"),
        "stop_reason": current.get("stop_reason"),
        "baseline": current.get("baseline"),
        "samples": current.get("samples") or [],
        "events": current.get("events") or [],
        "blur": current.get("blur"),
        "sampling": current.get("sampling"),
        "stage13_snapshot": snap if isinstance(snap, dict) else None,
        "css_note": "computed 變數以讀到的字串為準；空字串不代表未賦值，也不改寫成 100svh。",
        "fallback_assumed": False,
    })
    _dump(folder, report)
    return report


def mark_stage13_unread(folder):
    add_stage13(folder, None)


def write_failure(folder, code):
    _dump(Path(folder), {
        "category": "證據矛盾",
        "reason": code,
        "early_stable_pair_is_convergence": False,
        "fallback_assumed": False,
        "disturbance": DISTURBANCE,
        "samples": [],
        "events": [],
    })


def _frame(t_ms, bottom, inner=697, scroll=0, vv=697, shell=697, y=None, css=True):
    return {
        "t_ms": t_ms,
        "inner_height": inner,
        "vv_height": vv,
        "scroll_y": scroll,
        "bottom": bottom,
        "y": bottom - 38 if y is None else y,
        "shell_height": shell,
        "css_vars": {"root": {"--tg-viewport-height": ""}, "body": {"--tg-viewport-height": ""}, "main": {"--tg-viewport-height": ""}} if css else None,
    }


def _selfcheck():
    stable = classify_frames([
        _frame(200, 733.711, shell=734),
        _frame(1484, 733.711, shell=734),
        _frame(1500, 733.711, shell=734),
    ], baseline_clipped=True)
    assert stable["category"] == "穩定仍裁切"
    assert stable["terminal"]["shell_minus_inner"] == 37
    assert stable["early_stable_pair_is_convergence"] is False
    assert "100svh" not in json.dumps(stable)

    transient = classify_frames([
        _frame(20, 733.711),
        _frame(1484, 677.533),
        _frame(1500, 677.533),
    ], baseline_clipped=True)
    assert transient["category"] == "只瞬態裁切"

    moving = [_frame(t, 700 + t / 100) for t in (0, 200, 400, 800, 1100, 1500)]
    deadline = classify_frames(moving, baseline_clipped=True)
    assert deadline["category"] == "1500ms未穩定"
    assert deadline["terminal"] is None

    early_only = classify_frames([
        _frame(100, 733.711),
        _frame(116, 733.711),
        _frame(800, 680),
        _frame(1500, 660),
    ], baseline_clipped=True)
    assert early_only["category"] == "1500ms未穩定"
    assert early_only["nonterminal_early_pair"] is not None
    assert early_only["category"] != "穩定仍裁切"

    absent = classify_frames([
        _frame(1484, 677.533),
        _frame(1500, 677.533),
    ], baseline_clipped=False)
    assert absent["category"] == "整輪未重現"
    assert absent["reproduced"] == "0/1"

    deadline_only = classify_frames([
        _frame(1484, 733.711),
        dict(_frame(1500, 733.711), reason="deadline"),
    ], baseline_clipped=True)
    assert deadline_only["category"] == "1500ms未穩定"
    assert deadline_only["terminal"] is None

    broken = classify_frames([_frame(1500, 700), _frame(10, 700)])
    assert broken["category"] == "證據矛盾"

    empty_css = frames_from_samples([{
        "t_ms": 1500, "innerHeight": 697, "scrollY": 0,
        "vv": {"height": 697}, "textarea": {"y": 639, "bottom": 677},
        "hApp": {"computedHeight": ""},
        "vars": {"root": {"--tg-viewport-height": ""}, "body": {"--tg-viewport-height": ""}, "main": {"--tg-viewport-height": ""}},
    }])
    assert empty_css[0]["shell_height"] is None
    assert empty_css[0]["css_vars"]["root"]["--tg-viewport-height"] == ""
    dumped = json.dumps(classify_frames(empty_css, baseline_clipped=False), ensure_ascii=False)
    assert "100svh" not in dumped

    conflict = classify_frames([
        _frame(1484, 733.711, y=695.711, shell=734),
        _frame(1500, 733.711, y=695.711, shell=734),
    ], baseline_clipped=True, stage13={
        "innerHeight": 697, "scrollY": 0,
        "textarea": {"y": 639.533, "bottom": 677.533},
        "vars": {"root": {"--tg-viewport-height": ""}, "body": {"--tg-viewport-height": ""}, "main": {"--tg-viewport-height": ""}},
    })
    assert conflict["category"] == "證據矛盾"
    assert conflict["stage13"]["scroll_explains_y_delta"] is False
    assert conflict["fallback_assumed"] is False

    secret = sanitize({
        "password": "hidden", "draft": "x", "cookie": "a", "url": "https://secret.example/a?q=1",
        "value": "typed", "text": "hello", "query": "q", "token": "t", "headers": {"a": "b"},
        "innerHeight": 697,
        "body": {"bottom": 12, "password": "no"},
        "vars": {"body": {"--tg-viewport-height": "3px", "token": "abc"}},
    })
    assert secret["innerHeight"] == 697
    assert secret["body"] == {"bottom": 12}
    assert secret["vars"]["body"] == {"--tg-viewport-height": "3px"}
    blob = json.dumps(secret)
    assert "hidden" not in blob and "secret.example" not in blob and "typed" not in blob
    assert "截圖前不讀版面" not in DISTURBANCE and "零干擾" not in DISTURBANCE
    assert "rAF" in DISTURBANCE and "focusout" in DISTURBANCE

    import subprocess
    import tempfile
    vars_ok = {
        "root": {"--tg-viewport-height": "9px", "--ra-vvh": "", "--ra-vvt": "", "--ra-safe-bottom": ""},
        "body": {"--tg-viewport-height": "9px", "--ra-vvh": "", "--ra-vvt": "", "--ra-safe-bottom": ""},
        "main": {"--tg-viewport-height": "9px", "--ra-vvh": "", "--ra-vvt": "", "--ra-safe-bottom": ""},
    }
    sample = {
        "reason": "raf", "t_ms": 1490, "innerWidth": 384, "innerHeight": 697, "scrollY": 0,
        "vv": {"height": 697, "offsetTop": 0, "offsetLeft": 0},
        "textarea": {"x": 1, "y": 650, "width": 10, "height": 38, "bottom": 688},
        "hApp": {"computedHeight": "697px"},
        "body": {"tag": "BODY", "bottom": 697, "computedHeight": "697px"},
        "vars": vars_ok, "password": "nope", "url": "https://secret.example/a?q=1",
    }
    snap = {
        "utc_ms": 2, "innerWidth": 384, "innerHeight": 697, "scrollY": 4, "raKbOpen": False,
        "classTokens": "", "vv": {"height": 697, "offsetTop": 0, "offsetLeft": 0},
        "textarea": {"x": 1, "y": 639, "width": 10, "height": 38, "bottom": 677},
        "vars": vars_ok, "cookie": "sid", "value": "draft text",
    }
    with tempfile.TemporaryDirectory() as folder:
        write_report(folder, {
            "samples": [sample, dict(sample, t_ms=1500)], "events": [], "elapsed_ms": 1500,
            "stop_reason": "deadline",
            "blur": {"t_before_ms": 0, "t_after_ms": 0.2, "original_focusout_observed": False},
            "sampling": {"strategy": "ring-keep-tail", "cap": 96, "dropped_early": 3},
        })
        add_stage13(folder, snap)
        saved = json.loads((Path(folder) / OBSERVATION_NAME).read_text(encoding="utf-8"))
    assert saved["samples"][0]["body"]["bottom"] == 697
    assert saved["samples"][0]["vars"]["body"]["--tg-viewport-height"] == "9px"
    assert saved["stage13_snapshot"]["scrollY"] == 4
    assert saved["stage13_snapshot"]["vars"]["body"]["--tg-viewport-height"] == "9px"
    assert saved["stage13_snapshot"]["textarea"]["bottom"] == 677
    assert saved["stage13_snapshot"]["classTokens"] == ""
    assert saved["stage13"]["css_vars_equal"] is True
    saved_blob = json.dumps(saved)
    assert "nope" not in saved_blob and "secret.example" not in saved_blob and "sid" not in saved_blob
    assert saved["blur"]["original_focusout_observed"] is False

    def clock(hz, use_raf):
        script = """
let now=0, seq=0; const frames=new Map(), timers=new Map(), docL=new Map(), winL=new Map(), vvL=new Map();
global.window=global; global.performance={now:()=>now};
const el={tagName:'TEXTAREA',className:'',parentElement:null,getAttribute:()=>'',getBoundingClientRect:()=>({x:0,y:650,width:300,height:38,bottom:688}),classList:{contains:()=>false}};
global.document={activeElement:{blur(){const cb=docL.get('focusout'); if(cb) cb({target:el});}},documentElement:Object.assign({},el,{classList:{contains:()=>false}}),body:el,visibilityState:'visible',querySelector:()=>el,querySelectorAll:()=>[el],addEventListener:(n,cb)=>docL.set(n,cb),removeEventListener:(n)=>docL.delete(n)};
window.innerWidth=384; window.innerHeight=697; window.scrollY=0;
window.addEventListener=(n,cb)=>winL.set(n,cb); window.removeEventListener=(n)=>winL.delete(n);
window.visualViewport={height:697,offsetTop:0,offsetLeft:0,addEventListener:(n,cb)=>vvL.set(n,cb),removeEventListener:(n)=>vvL.delete(n)};
global.getComputedStyle=()=>({height:'697px',minHeight:'0px',paddingBottom:'0px',getPropertyValue:()=>''});
global.requestAnimationFrame=(cb)=>{frames.set(++seq,cb);return seq;};
global.cancelAnimationFrame=(id)=>frames.delete(id);
global.setTimeout=(cb)=>{timers.set(++seq,cb);return seq;};
global.clearTimeout=(id)=>timers.delete(id);
(""" + ARM_JS + """)();
const useRaf = """ + ("true" if use_raf else "false") + """;
const hz = """ + str(hz) + """;
if (useRaf) {
  const steps = Math.round(1500 * hz / 1000);
  for (let i = 1; i <= steps; i += 1) {
    now = i * 1000 / hz;
    const pending = [...frames.values()];
    frames.clear();
    for (const cb of pending) cb(now);
  }
} else now = 1500;
for (const timer of [...timers.values()]) timer();
const result = window.__cmaAndroidObs.result;
console.log(JSON.stringify({
  blur: result.blur, sampling: result.sampling, samples: result.samples.map(s => ({t_ms: s.t_ms, reason: s.reason})),
  focusout: (result.events || []).filter(e => e.type === 'focusout').length,
  listeners: {doc: docL.size, win: winL.size, vv: vvL.size}
}));
"""
        with tempfile.TemporaryDirectory() as folder:
            path = Path(folder) / "clock.js"
            path.write_text(script, encoding="utf-8")
            proc = subprocess.run(["node", str(path)], capture_output=True, text=True, check=True)
        return json.loads(proc.stdout)

    for hz in (60, 120, 144):
        ran = clock(hz, True)
        samples = ran["samples"]
        assert len(samples) <= 96
        assert samples[-1]["t_ms"] >= 1400
        assert samples[-1]["reason"] == "deadline"
        late = [row for row in samples if row["reason"] == "raf" and row["t_ms"] >= 1400]
        assert len(late) >= 2
        gap = max(late[i + 1]["t_ms"] - late[i]["t_ms"] for i in range(len(late) - 1))
        assert gap <= (1000 / hz) * 1.2
        assert ran["sampling"]["strategy"] == "ring-keep-tail"
        assert ran["sampling"]["dropped_early"] == (0 if hz == 60 else ran["sampling"]["dropped_early"])
        assert ran["sampling"]["dropped_early"] > 0 if hz != 60 else True
        assert ran["blur"]["original_focusout_observed"] is False
        assert ran["focusout"] == 0
        assert ran["blur"]["t_after_ms"] >= ran["blur"]["t_before_ms"]
        assert ran["listeners"] == {"doc": 0, "win": 0, "vv": 0}
    quiet = clock(120, False)
    assert [row["reason"] for row in quiet["samples"]] == ["deadline"]
    assert quiet["listeners"] == {"doc": 0, "win": 0, "vv": 0}
    quiet_frames = frames_from_samples([{
        "t_ms": 1500, "reason": "deadline", "innerHeight": 697, "scrollY": 0,
        "vv": {"height": 697}, "textarea": {"y": 695.711, "bottom": 733.711},
        "hApp": {"computedHeight": "734px"}, "vars": vars_ok,
    }])
    quiet_class = classify_frames(quiet_frames, baseline_clipped=True)
    assert quiet_class["terminal"] is None and quiet_class["category"] == "1500ms未穩定"
    print("observe_viewport selfcheck ok")


if __name__ == "__main__":
    _selfcheck()
