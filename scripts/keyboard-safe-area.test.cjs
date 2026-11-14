const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const test = require('node:test');
const vm = require('node:vm');

const html = fs.readFileSync(path.join(__dirname, '../internal/static/index.html'), 'utf8');
const marker = html.indexOf('<!-- 軟鍵盤：');
const start = html.indexOf('<script>', marker) + '<script>'.length;
const end = html.indexOf('</script>', start);
assert.ok(marker >= 0 && end > start, 'keyboard script must exist in index.html');

function setup(coarse) {
  const classes = new Set();
  const handlers = {};
  const timers = [];
  const context = vm.createContext({
    matchMedia: () => ({ matches: coarse }),
    document: { documentElement: { classList: { add: (c) => classes.add(c), remove: (c) => classes.delete(c) } } },
    addEventListener: (type, fn) => { handlers[type] = fn; },
    setTimeout: (fn) => { timers.push(fn); return timers.length; },
    clearTimeout: (id) => { if (id) timers[id - 1] = null; },
  });
  vm.runInContext(html.slice(start, end), context);
  const flush = () => timers.splice(0).forEach((fn) => fn && fn());
  // 真正的選擇器比對由瀏覽器負責，這裡只用「是否為輸入元件」的假元件驗證事件流程
  const editable = { matches: () => true };
  const other = { matches: () => false };
  return { classes, handlers, flush, editable, other };
}

test('觸控裝置上輸入框取得焦點才加 ra-kb-open，失焦後移除', () => {
  const { classes, handlers, flush, editable } = setup(true);
  handlers.focusin({ target: editable });
  assert.ok(classes.has('ra-kb-open'));
  handlers.focusout({ target: editable });
  assert.ok(classes.has('ra-kb-open'), '失焦後要延遲移除，避免輸入框之間切換時閃動');
  flush();
  assert.ok(!classes.has('ra-kb-open'));
});

test('輸入框之間切換焦點時不會中途移除', () => {
  const { classes, handlers, flush, editable } = setup(true);
  handlers.focusin({ target: editable });
  handlers.focusout({ target: editable });
  handlers.focusin({ target: editable });
  flush();
  assert.ok(classes.has('ra-kb-open'));
});

test('桌面滑鼠環境與非輸入元件不會觸發', () => {
  const desktop = setup(false);
  desktop.handlers.focusin({ target: desktop.editable });
  assert.ok(!desktop.classes.has('ra-kb-open'));
  const touch = setup(true);
  touch.handlers.focusin({ target: touch.other });
  assert.ok(!touch.classes.has('ra-kb-open'));
});

test('選擇器排除不會叫出鍵盤的 input 類型', () => {
  const script = html.slice(start, end);
  for (const type of ['checkbox', 'radio', 'button', 'submit', 'file', 'range', 'color']) {
    assert.ok(script.includes(`[type=${type}]`), `${type} 應被排除`);
  }
});

test('輸入區下方間距改讀 --ra-safe-bottom，且鍵盤開啟時歸零', () => {
  assert.match(html, /html\.ra-kb-open\s*\{\s*--ra-safe-bottom:\s*0px;\s*\}/);
  const chat = fs.readFileSync(path.join(__dirname, '../internal/static/js/chat/ChatView.js'), 'utf8');
  assert.match(chat, /paddingBottom: 'calc\(0\.75rem \+ var\(--ra-safe-bottom, env\(safe-area-inset-bottom\)\)\)'/);
});
