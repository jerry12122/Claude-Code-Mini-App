const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');

const modal = fs.readFileSync(path.join(__dirname, '../internal/static/js/share/ShareModal.js'), 'utf8');
const section = fs.readFileSync(path.join(__dirname, '../internal/static/js/share/SharesSection.js'), 'utf8');
const fn = modal.indexOf('function ShareRow');
const signature = modal.slice(fn, modal.indexOf(') {', fn));
const call = modal.indexOf('{active.map');
const dialogCall = modal.slice(call, modal.indexOf(')}', call));

assert.match(signature, /variant = 'dialog'/);
assert.doesNotMatch(dialogCall, /variant/);
assert.match(section, /<ShareRow[^>]*variant="settings"/);
assert.equal(section.split('variant="settings"').length, 2);
console.log('share-row-variant: default dialog, settings passes variant');
