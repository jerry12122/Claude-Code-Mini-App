// 一般設定的送出與守門百分比草稿。執行：node scripts/general-settings-ui.test.cjs
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');

const root = path.join(__dirname, '..');
const core = fs.readFileSync(path.join(root, 'internal/static/js/core/core.js'), 'utf8');
const modal = fs.readFileSync(path.join(root, 'internal/static/js/ui/SettingsModal.js'), 'utf8');

function sliceBetween(source, startMark, endMark) {
  const start = source.indexOf(startMark);
  const end = source.indexOf(endMark, start + startMark.length);
  assert.ok(start >= 0 && end > start, `找不到區段 ${startMark}`);
  return source.slice(start, end);
}

const calls = [];
let nextResponse = { ok: true, error: '' };
const context = vm.createContext({
  calls,
  apiFetch: async (url, opts) => {
    calls.push({ url, body: opts && opts.body });
    if (!nextResponse.ok) {
      return { ok: false, json: async () => ({ error: nextResponse.error }) };
    }
    return { ok: true, json: async () => JSON.parse(opts.body) };
  },
});
vm.runInContext(
  sliceBetween(core, 'const GENERAL_DEFAULTS', 'async function hydrateAppearanceFromServer')
    + '\nthis.GENERAL_DEFAULTS = GENERAL_DEFAULTS;\n',
  context,
);

(async () => {
const full = {
  vscodeNoAdmin: false,
  claudeAutoAllowAsk: true,
  autoResumeOnQuota: true,
  quotaGuardEnabled: true,
  quotaGuardPercent: 0.5,
};
const saved = await context.putGeneralSettings(full);
const sent = JSON.parse(calls[0].body);
for (const key of Object.keys(full)) {
  assert.equal(sent[key], full[key], `PUT 應保留 ${key}`);
}
assert.equal(sent.quotaGuardPercent, 0.5);
assert.equal(saved.quotaGuardPercent, 0.5);
assert.equal(saved.claudeAutoAllowAsk, true);
assert.equal(saved.autoResumeOnQuota, true);

calls.length = 0;
nextResponse = { ok: false, error: 'quotaGuardPercent 必須大於 0 且小於等於 100' };
await assert.rejects(
  context.putGeneralSettings({ ...full, quotaGuardPercent: 150 }),
  (err) => err && err.name === 'Error' && err.message === 'quotaGuardPercent 必須大於 0 且小於等於 100',
);
assert.equal(calls.length, 1, '不合法值仍要送給伺服器，不能在客戶端悄悄擋下');
assert.equal(JSON.parse(calls[0].body).quotaGuardPercent, 150);

const modalContext = vm.createContext({});
vm.runInContext(
  sliceBetween(modal, 'function quotaGuardPercentEvent', 'function GeneralSection'),
  modalContext,
);
const event = modalContext.quotaGuardPercentEvent;
const settle = modalContext.settleGeneralSave;
assert.equal(typeof event, 'function');
assert.equal(typeof settle, 'function');

let puts = 0;
let draft = '';
for (const ch of '0.5') {
  draft += ch;
  const typing = event('input', draft, 90);
  assert.equal(typing.put, false);
  if (typing.put) puts += 1;
}
assert.equal(puts, 0, '輸入中的草稿不 PUT');
const enter = event('enter', draft, 90);
assert.equal(enter.put, false);
assert.equal(enter.blur, true, 'Enter 只離開欄位，不另外送出');
const blur = event('blur', draft, 90);
assert.equal(blur.put, true);
assert.equal(blur.value, 0.5);
assert.equal(event('blur', '0.5', 0.5).put, false);

const previous = { ...full, quotaGuardPercent: 90, autoResumeOnQuota: false };
const settled = await settle(previous, { ...previous, quotaGuardPercent: 150 }, async () => {
  throw new Error('quotaGuardPercent 必須大於 0 且小於等於 100');
});
assert.equal(settled.general, previous);
assert.equal(settled.error, 'quotaGuardPercent 必須大於 0 且小於等於 100');

assert.equal(context.GENERAL_DEFAULTS.claudeAutoAllowAsk, false, 'ask 預設關閉');
assert.match(modal, /generalPatch\(\s*general,\s*\{\s*claudeAutoAllowAsk:\s*checked\s*\}/);
assert.match(modal, /generalPatch\(\s*general,\s*\{\s*autoResumeOnQuota:\s*e\.target\.checked\s*\}/);
assert.match(modal, /generalPatch\(\s*general,\s*\{\s*quotaGuardEnabled:\s*e\.target\.checked\s*\}/);
assert.match(modal, /generalPatch\(\s*general,\s*\{\s*vscodeNoAdmin:\s*checked\s*\}/);
const patch = modalContext.generalPatch;
const start = {
  vscodeNoAdmin: true,
  claudeAutoAllowAsk: false,
  autoResumeOnQuota: true,
  quotaGuardEnabled: true,
  quotaGuardPercent: 75,
};
let confirmCalls = 0;
const cancelled = patch(start, { claudeAutoAllowAsk: true }, () => {
  confirmCalls += 1;
  return false;
});
assert.equal(confirmCalls, 1, '開啟 ask 要先確認');
assert.equal(cancelled.put, false, '取消確認不 PUT');
const callsBeforeCancel = calls.length;
assert.equal(calls.length, callsBeforeCancel);
const turnedOff = patch({ ...start, claudeAutoAllowAsk: true }, { claudeAutoAllowAsk: false }, () => {
  throw new Error('關閉 ask 不該再確認');
});
assert.equal(turnedOff.put, true);

calls.length = 0;
nextResponse = { ok: true, error: '' };
const askOn = patch(start, { claudeAutoAllowAsk: true }, () => true);
assert.equal(askOn.put, true);
await context.putGeneralSettings(askOn.next);
const askBody = JSON.parse(calls[0].body);
assert.equal(askBody.autoResumeOnQuota, true);
assert.equal(askBody.quotaGuardEnabled, true);
assert.equal(askBody.quotaGuardPercent, 75);
assert.equal(askBody.claudeAutoAllowAsk, true);
assert.equal(askBody.vscodeNoAdmin, true);

calls.length = 0;
const guardOff = patch(askOn.next, { quotaGuardEnabled: false }, () => true);
await context.putGeneralSettings(guardOff.next);
const guardBody = JSON.parse(calls[0].body);
assert.equal(guardBody.claudeAutoAllowAsk, true);
assert.equal(guardBody.quotaGuardPercent, 75);
assert.equal(guardBody.vscodeNoAdmin, true);
assert.equal(guardBody.autoResumeOnQuota, true);
assert.equal(guardBody.quotaGuardEnabled, false);

calls.length = 0;
const resumeOff = patch(guardOff.next, { autoResumeOnQuota: false }, () => true);
await context.putGeneralSettings(resumeOff.next);
const resumeBody = JSON.parse(calls[0].body);
assert.equal(resumeBody.claudeAutoAllowAsk, true);
assert.equal(resumeBody.quotaGuardPercent, 75);
assert.equal(resumeBody.vscodeNoAdmin, true);
assert.equal(resumeBody.autoResumeOnQuota, false);
assert.equal(resumeBody.quotaGuardEnabled, false);

const rolled = await settle(start, askOn.next, async () => {
  throw new Error('伺服器未存到');
});
assert.equal(rolled.general, start);
assert.equal(rolled.general.claudeAutoAllowAsk, false);
assert.equal(rolled.general.quotaGuardPercent, 75);
assert.equal(rolled.general.vscodeNoAdmin, true);
assert.equal(rolled.error, '伺服器未存到');

const section = sliceBetween(modal, 'function GeneralSection', 'const THEME_FIELD_OPTIONS');
const shown = section.match(/\{error\}/g) || [];
assert.equal(shown.length, 1, '一般設定錯誤只呈現一次');
assert.equal((section.match(/role="alert"/g) || []).length, 1);
assert.match(section, /aria-describedby=\{error \? '[^']*quota-guard-error'/);

console.log('general-settings-ui: ok');
})().catch((err) => {
  console.error(err);
  process.exit(1);
});
