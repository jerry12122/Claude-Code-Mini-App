// 外觀才出現「還原預設／儲存」。一般設定維持各自的即時或表單儲存。
// 執行：node scripts/settings-appearance-actions.test.cjs
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');

const modal = fs.readFileSync(path.join(__dirname, '../internal/static/js/ui/SettingsModal.js'), 'utf8');

function sliceBetween(source, startMark, endMark) {
  const start = source.indexOf(startMark);
  const end = source.indexOf(endMark, start + startMark.length);
  assert.ok(start >= 0 && end > start, `找不到區段 ${startMark}`);
  return source.slice(start, end);
}

assert.match(modal, /const showAppearanceActions = section === 'appearance'/);
assert.match(modal, /showAppearanceActions && \(\s*<button type="button" onClick=\{handleReset\}/);
assert.match(modal, /showAppearanceActions && \(\s*<button type="button" onClick=\{handleSave\}/);
assert.match(modal, /onClick=\{\(\) => dialogRef\.current\?\.close\(\)\}/);
assert.match(modal, /data-settings-section=\{s\.id\}/);
assert.match(modal, /onCancel=\{/);
assert.match(modal, /if \(mobile && mobileDetail\) \{ e\.preventDefault\(\); setMobileDetail\(false\); \}/);

const general = sliceBetween(modal, 'function GeneralSection', 'const THEME_FIELD_OPTIONS');
assert.doesNotMatch(general, /handleSave|還原預設/);
assert.match(general, /<GlobalInstructionSettings\s*\/>/);
assert.match(sliceBetween(modal, 'function GlobalInstructionSettings', 'function GeneralCheckbox'), /儲存全域指令/);
assert.match(general, /generalPatch\(\s*general,\s*\{\s*vscodeNoAdmin:\s*checked\s*\}/);

console.log('settings appearance actions: ok');
