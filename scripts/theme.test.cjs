const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');

const source = fs.readFileSync(path.join(__dirname, '../internal/static/js/core/core.js'), 'utf8');
const start = source.indexOf("const THEME_KEY = 'cc_theme_v1'");
const end = source.indexOf('// head inline 已同步 data-theme', start);
assert.ok(start >= 0 && end > start, 'theme block must exist in core.js');

const context = vm.createContext({
  localStorage: {
    _data: {},
    getItem(k) { return Object.prototype.hasOwnProperty.call(this._data, k) ? this._data[k] : null; },
    setItem(k, v) { this._data[k] = String(v); },
    removeItem(k) { delete this._data[k]; },
  },
  window: {
    matchMediaAvailable: true,
    prefersDark: true,
    events: [],
    dispatchEvent(ev) {
      this.events.push(ev);
      return true;
    },
  },
  document: {
    documentElement: { dataset: {}, style: {} },
    getElementById() { return null; },
  },
});
context.window.matchMedia = (q) => ({
  matches: context.window.matchMediaAvailable ? context.window.prefersDark : false,
  addEventListener() {},
  removeEventListener() {},
});
context.CustomEvent = class CustomEvent {
  constructor(type, init) {
    this.type = type;
    this.detail = init && init.detail;
  }
};

vm.runInContext(source.slice(start, end), context);

assert.match(source, /const THEME_KEY = 'cc_theme_v1'/);
const resolveTheme = context.resolveTheme;
const readThemePref = context.readThemePref;
const setThemePref = context.setThemePref;
assert.equal(typeof resolveTheme, 'function');

// Resolution table (spec)
assert.equal(resolveTheme('dark', false), 'dark');
assert.equal(resolveTheme('light', true), 'light');
assert.equal(resolveTheme('system', false), 'light');
assert.equal(resolveTheme('system', true), 'dark');
assert.equal(resolveTheme('garbage', false), 'dark');
assert.equal(resolveTheme(undefined, false), 'dark');
assert.equal(resolveTheme(null, false), 'dark');

// Default when nothing stored → dark effective (via read + resolve)
context.localStorage._data = {};
assert.equal(readThemePref(), 'dark');
assert.equal(resolveTheme(readThemePref(), false), 'dark');

context.localStorage.setItem('cc_theme_v1', 'light');
assert.equal(readThemePref(), 'light');
assert.equal(resolveTheme(readThemePref(), true), 'light');

context.localStorage.setItem('cc_theme_v1', 'system');
assert.equal(readThemePref(), 'system');

context.localStorage.setItem('cc_theme_v1', 'not-a-theme');
assert.equal(readThemePref(), 'dark');

// system + matchMedia unavailable → dark
assert.equal(resolveTheme('system', null), 'dark');
assert.equal(resolveTheme('system', undefined), 'dark');

// localStorage unavailable
const throwStorage = {
  getItem() { throw new Error('denied'); },
  setItem() { throw new Error('denied'); },
  removeItem() { throw new Error('denied'); },
};
const ctx2 = vm.createContext({
  localStorage: throwStorage,
  window: context.window,
  document: context.document,
  CustomEvent: context.CustomEvent,
});
vm.runInContext(source.slice(start, end), ctx2);
assert.equal(ctx2.readThemePref(), 'dark');
assert.doesNotThrow(() => ctx2.setThemePref('light'));

console.log('theme checks passed');

// applyAppearance：預設值走 removeProperty，自訂才 setProperty
const appStart = source.indexOf('const APPEARANCE_DEFAULTS = {');
const appEnd = source.indexOf('/** PUT /settings/appearance', appStart);
assert.ok(appStart >= 0 && appEnd > appStart, 'applyAppearance block must exist in core.js');

function makeAppearanceDoc() {
  const styleEl = { textContent: '' };
  const styleLog = { setProperty: [], removeProperty: [] };
  const style = {
    setProperty(k, v) { styleLog.setProperty.push([k, v]); },
    removeProperty(k) { styleLog.removeProperty.push(k); },
  };
  const document = {
    documentElement: { style },
    getElementById: (id) => (id === 'ra-custom-css' ? styleEl : null),
  };
  return { document, styleLog };
}

const doc1 = makeAppearanceDoc();
const appCtx = vm.createContext({ document: doc1.document });
vm.runInContext(source.slice(appStart, appEnd), appCtx);
const applyAppearance = appCtx.applyAppearance;
assert.equal(typeof applyAppearance, 'function');

const defaults = { ...appCtx.APPEARANCE_DEFAULTS };
applyAppearance(defaults);
assert.equal(doc1.styleLog.setProperty.length, 0, 'defaults must not setProperty md colors');
assert.deepEqual(
  doc1.styleLog.removeProperty.sort(),
  ['--md-bold-color', '--md-code-color', '--md-heading-color'],
  'defaults must remove all three md CSS vars',
);

const doc2 = makeAppearanceDoc();
const appCtx2 = vm.createContext({ document: doc2.document });
vm.runInContext(source.slice(appStart, appEnd), appCtx2);
const applyAppearance2 = appCtx2.applyAppearance;
applyAppearance2({ ...defaults, headingColor: 'oklch(0.5 0.2 30)' });
assert.equal(doc2.styleLog.setProperty.length, 1, 'custom heading only one setProperty');
assert.equal(doc2.styleLog.setProperty[0][0], '--md-heading-color');
assert.ok(doc2.styleLog.removeProperty.includes('--md-bold-color'));
assert.ok(doc2.styleLog.removeProperty.includes('--md-code-color'));

console.log('applyAppearance checks passed (3 new assertion groups)');
