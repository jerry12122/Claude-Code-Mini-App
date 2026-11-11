const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');

const source = fs.readFileSync(path.join(__dirname, '../internal/static/js/core/core.js'), 'utf8');
const init = source.slice(0, source.indexOf('/** Web 登入 session token'));
function boot(platform, isFullscreen) {
  const events = {};
  const root = { dataset: {}, style: { setProperty(key, value) { this[key] = value; } } };
  const app = platform == null ? undefined : {
    platform, isFullscreen, initData: '', contentSafeAreaInset: { top: 56 },
    ready() {}, expand() {}, onEvent(name, callback) { events[name] = callback; },
  };
  vm.runInNewContext(init, { React: {}, window: { Telegram: app && { WebApp: app } }, document: { documentElement: root } });
  return { app, root, events };
}

assert.equal(boot(null).root.dataset.tgDesktopFullscreen, undefined, 'ordinary browsers do not opt in');
for (const platform of ['tdesktop', 'macos', 'web', 'weba', 'webk']) {
  const { app, root, events } = boot(platform, false);
  assert.equal(root.dataset.tgDesktopFullscreen, 'false');
  app.isFullscreen = true;
  events.fullscreenChanged();
  assert.equal(root.dataset.tgDesktopFullscreen, 'true', platform);
  app.contentSafeAreaInset.top = 72;
  events.contentSafeAreaChanged();
  assert.equal(root.style['--tg-content-safe-top'], '72px');
  app.isFullscreen = false;
  events.fullscreenChanged();
  assert.equal(root.dataset.tgDesktopFullscreen, 'false', 'exiting fullscreen restores the existing layout');
  assert.equal(boot(platform, true).root.dataset.tgDesktopFullscreen, 'true', 'already fullscreen at startup');
  assert.equal(boot(platform, undefined).root.dataset.tgDesktopFullscreen, 'false', 'older clients stay unchanged');
}
for (const platform of ['android', 'ios', 'unknown', '']) {
  assert.equal(boot(platform, true).root.dataset.tgDesktopFullscreen, 'false', platform);
}
console.log('Telegram desktop fullscreen opt-in checks passed');
