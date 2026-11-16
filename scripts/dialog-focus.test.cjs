// 四個浮層都必須用原生 <dialog> + showModal()，不得自己重建對話框語意與焦點管理。
// 規則見 docs/spec/frontend-architecture.md 第 5 節（來源：modern-web-guidance「dialog」）。
// 原本此檔測試 useDialogFocus（手寫焦點陷阱）；該 hook 已隨改用原生 dialog 刪除。
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const test = require('node:test');

const JS = path.join(__dirname, '../internal/static/js');
const read = (file) => fs.readFileSync(path.join(JS, file), 'utf8');
// 整行註解不算（規範本身會在註解裡提到這些 token 當反例提醒）
const code = (file) => read(file).split('\n').filter((l) => !/^\s*(\/\/|\/\*|\*)/.test(l)).join('\n');

const MODALS = ['ui/SettingsModal.js', 'share/ShareModal.js', 'chat/ForwardModal.js', 'ui/QuickSwitcher.js'];

test('用原生 <dialog> 與 showModal()，而不是自製浮層', () => {
  for (const file of MODALS) {
    const src = code(file);
    assert.match(src, /<dialog\b/, `${file} 應使用原生 <dialog>`);
    assert.match(src, /\.showModal\(\)/, `${file} 應以 showModal() 開啟（不可用 show() 或 open 屬性）`);
    assert.doesNotMatch(src, /\bopen=\{/, `${file} 不可用 open 屬性開啟 modal`);
  }
});

test('不重建瀏覽器已提供的語意與行為', () => {
  for (const file of MODALS) {
    const src = code(file);
    assert.doesNotMatch(src, /role="dialog"/, `${file} 原生 modal 不需要 role="dialog"`);
    assert.doesNotMatch(src, /aria-modal/, `${file} 原生 modal 不需要 aria-modal`);
    assert.doesNotMatch(src, /key === 'Escape'/, `${file} 不需要自寫 Esc handler`);
    assert.doesNotMatch(src, /body\.style\.overflow/, `${file} 不可用 body overflow 鎖滾動`);
  }
});

test('每個對話框都有可及名稱，且關閉由 onClose 同步回 React state', () => {
  for (const file of MODALS) {
    const src = code(file);
    assert.match(src, /aria-label(ledby)?=/, `${file} 缺少 aria-label 或 aria-labelledby`);
    assert.match(src, /onClose=\{/, `${file} 需要 onClose 把關閉同步回 state`);
  }
});

test('light dismiss 比對指標座標，而非 event.target', () => {
  // <dialog> 自身也是它 padding 與子元素間隙的 target，用 target 判斷會誤關
  for (const file of MODALS) {
    const src = code(file);
    assert.match(src, /getBoundingClientRect\(\)/, `${file} 應以 rect 比對指標位置`);
    assert.match(src, /clientX < r\.left/, `${file} 應檢查指標是否落在對話框外`);
  }
});

test('手寫焦點陷阱已完全移除', () => {
  const atoms = read('ui/uiAtoms.js');
  assert.doesNotMatch(atoms, /useDialogFocus|DIALOG_FOCUSABLE/, 'uiAtoms.js 仍留有手寫焦點陷阱');
  for (const file of MODALS) {
    assert.doesNotMatch(code(file), /useDialogFocus/, `${file} 仍在呼叫 useDialogFocus`);
  }
});

test('設定頁手機第二層用 cancel 攔截 Esc，而不是自寫鍵盤事件', () => {
  const src = code('ui/SettingsModal.js');
  assert.match(src, /onCancel=\{/, 'SettingsModal 應以 onCancel 攔截 Esc／返回手勢');
  assert.match(src, /preventDefault\(\)/, 'onCancel 需要 preventDefault 才能留在對話框內');
});

test('對話框內的按鈕走原生 close()，不直接呼叫 onClose', () => {
  // 直接呼叫 onClose 會讓 React 先卸載還開著的 <dialog>，瀏覽器就不會把焦點還給開啟者（實測掉到 body）。
  // onClose 只能接在 <dialog onClose> 上，由 close 事件觸發。
  for (const file of MODALS) {
    const src = code(file);
    assert.doesNotMatch(src, /onClick=\{onClose\}/, `${file} 的按鈕應呼叫 dialog.close()，不可直接 onClose`);
    const direct = src.split('\n').filter((l) => /\bonClose\(\)/.test(l));
    assert.deepEqual(direct, [], `${file} 不可直接呼叫 onClose()：\n${direct.join('\n')}`);
  }
});

test('modal 開著時 toast 掛進最上層 dialog，關閉後移回 body', () => {
  // 掛在 body 會被 top layer 蓋住且變成 inert：看不見，「復原」之類的動作鈕也按不到。
  const toast = read('core/toast.js');
  assert.match(toast, /querySelectorAll\('dialog:modal'\)/, 'showToast 應找出開著的 modal');
  assert.match(toast, /addEventListener\('close'/, 'modal 關閉後應把 toast 移回 body，避免跟著卸載');
});

console.log('native dialog tests passed');
