const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const read = name => fs.readFileSync(path.join(__dirname, '../internal/static/js', name), 'utf8').replace(/\r\n/g, '\n');

function formHarness(defaults = {}) {
  const slots = [];
  let cursor = 0;
  const context = vm.createContext({
    useState(initial) {
      const index = cursor++;
      if (!(index in slots)) slots[index] = initial;
      return [slots[index], value => { slots[index] = typeof value === 'function' ? value(slots[index]) : value; }];
    },
    normalizePermMode: (_agent, value) => value,
    extractModelFromCliArgs: () => '', cliArgsWithoutModel: value => value,
    mergeModelIntoCliExtraLines: value => value,
    parseCliExtraArgs: value => value.split('\n').filter(Boolean),
  });
  vm.runInContext(read('hooks/useNewSessionForm.js'), context);
  return () => { cursor = 0; return context.useNewSessionForm(defaults); };
}

function handler(source, start, end, globals, name) {
  const context = vm.createContext({ ...globals });
  const offset = source.indexOf(start);
  assert.ok(offset >= 0, start);
  vm.runInContext(source.slice(offset, source.indexOf(end, offset)) + `\nglobalThis.handler = ${name};`, context);
  return context.handler;
}

async function main() {
  const instruction = '角色：reviewer\n回覆繁體中文，保留 "引號" 與 C:\\work';
  for (const agent of ['claude', 'codex', 'antigravity']) {
    const render = formHarness({ agent, systemInstruction: instruction });
    let form = render();
    assert.equal(form.systemInstruction, instruction, `${agent} prefill`);
    form.setSystemInstruction(`  ${instruction}\n  `);
    form = render();
    const payload = form.buildCreatePayload();
    assert.equal(payload.system_instruction, instruction, `${agent} trimmed separate configuration`);
    assert.deepEqual(Array.from(payload.cli_extra_args), []);
    assert.equal('prompt' in payload, false);
    assert.equal('message' in payload, false);
    assert.equal('system_instruction' in form.buildCreatePayload({ input_mode: 'shell' }), false);
  }
  const render = formHarness();
  let form = render();
  assert.equal('system_instruction' in form.buildCreatePayload(), false, 'empty keeps old payload');
  form.setSystemInstruction(instruction);
  form = render();
  for (const agent of ['codex', 'antigravity', 'claude']) {
    form.setAgent(agent);
    form = render();
    assert.equal(form.systemInstruction, instruction, 'supported switches preserve draft');
  }
  for (const agent of ['cursor', 'kiro', 'kiroacp', 'telegram']) {
    const unsupported = formHarness({ agent, systemInstruction: instruction });
    assert.equal(unsupported().systemInstruction, '', `${agent} rejects prefill`);
    form.setAgent(agent);
    form = render();
    assert.equal(form.systemInstruction, '');
    assert.equal('system_instruction' in form.buildCreatePayload(), false);
    form.setAgent('claude');
    form = render();
    assert.equal(form.systemInstruction, '', 'returning to supported does not restore cleared draft');
    form.setSystemInstruction(instruction);
    form = render();
  }
  form.reset({ systemInstruction: instruction });
  form = render();
  assert.equal(form.systemInstruction, '', 'reset always clears instruction');
  for (const agent_type of ['claude', 'codex', 'antigravity', 'cursor', 'kiroacp']) {
    form.applyFromSession({ agent_type, system_instruction: instruction });
    form = render();
    assert.equal(form.systemInstruction, ['claude', 'codex', 'antigravity'].includes(agent_type) ? instruction : '');
  }
  form.applyFromSession({ agent_type: 'claude', system_instruction: instruction });
  form = render();
  form.setSystemInstruction('edited copy');
  form = render();
  assert.equal(form.buildCreatePayload().system_instruction, 'edited copy');

  const composer = read('session/NewSessionComposer.js');
  const forwarded = read('chat/ForwardModal.js');
  // The real DOM/keyboard check also runs in browser acceptance. These assertions
  // guard that the instruction textarea never receives the first-message submit handler.
  for (const source of [composer, forwarded]) {
    const textarea = [...source.matchAll(/<textarea\b[\s\S]*?\/>/g)].map(match => match[0]).find(markup => markup.includes('value={newForm.systemInstruction}'));
    assert.ok(textarea, 'instruction has its own multiline input');
    assert.doesNotMatch(textarea, /onKeyDown|onKeyPress|onKeyUp/);
    const id = textarea.match(/\bid="([^"]+)"/)[1];
    const helpId = textarea.match(/aria-describedby="([^"]+)"/)[1];
    assert.ok(source.includes(`htmlFor="${id}"`));
    assert.ok(source.includes(`id="${helpId}"`));
    assert.ok(source.includes('專屬指令（選填）'));
    assert.ok(source.includes('建立後固定，調整請另建會話'));
    assert.ok(source.includes('角色、回覆風格、工作規則'));
  }
  assert.ok(composer.indexOf('value={newForm.name}') < composer.indexOf('value={newForm.systemInstruction}'));
  assert.ok(composer.indexOf('value={newForm.systemInstruction}') < composer.indexOf('value={message}'));
  assert.match(forwarded, /<option value="antigravity">Antigravity<\/option>/);

  const requests = [], messages = [];
  const globals = {
    loading: false, newForm: form, message: 'first user message',
    setLoading() {}, setErrText() {}, autoSessionName: () => 'new',
    apiFetch: async (url, options) => { requests.push({ url, options }); return { ok: true, json: async () => ({ id: 'created' }) }; },
    window: { dispatchEvent() {} }, Event: class {}, onCreated() {},
    sendFirstPromptOrDraft: async (id, message) => { messages.push({ id, message }); return true; },
  };
  await handler(composer, '  const submit = async', '\n  const handleMessageKeyDown', globals, 'submit')();
  assert.equal(JSON.parse(requests[0].options.body).system_instruction, 'edited copy');
  assert.deepEqual(messages, [{ id: 'created', message: 'first user message' }]);

  const forwardGlobals = {
    ...globals, note: 'note', messageContent: 'forwarded answer', payload: { messageKey: 'm' },
    selectedId: 'new', setBusy() {}, onClose() {}, onForwarded() {},
    buildForwardUserPrompt: (note, content) => `${note}\n${content}`,
    sessionsList: [{ id: 'existing', system_instruction: 'destination role' }],
    sendPromptViaEphemeralWS: async (id, message) => { messages.push({ id, message }); },
  };
  form.setName('new forward');
  forwardGlobals.newForm = render();
  await handler(forwarded, '  const runForward = async', '\n  return (', forwardGlobals, 'runForward')();
  assert.equal(JSON.parse(requests[1].options.body).system_instruction, 'edited copy');
  assert.deepEqual(messages[1], { id: 'created', message: 'note\nforwarded answer' });
  forwardGlobals.selectedId = 'existing';
  await handler(forwarded, '  const runForward = async', '\n  return (', forwardGlobals, 'runForward')();
  assert.equal(requests.length, 2, 'existing forward does not create or patch session');
  assert.deepEqual(messages[2], { id: 'existing', message: 'note\nforwarded answer' });
  assert.equal(forwardGlobals.sessionsList[0].system_instruction, 'destination role');

  let prefill;
  handler(read('chat/ChatView.js'), '  const openHandoffComposer =', '\n  const agentRunning', {
    handoffInfo: { agent_type: 'codex', system_instruction: instruction },
    onOpenComposer: value => { prefill = value; }, messages: [],
    extractModelFromCliArgs: () => '', cliArgsWithoutModel: value => value,
    agentTypeForCreate: value => value, normalizePermMode: (_agent, value) => value,
    buildHandoffDraftMessage: () => 'handoff summary',
  }, 'openHandoffComposer')();
  assert.equal(prefill.systemInstruction, instruction);
  assert.equal(prefill.message, 'handoff summary');
  console.log('session instructions: state, payload, copy/reset, field wiring, creation/forward isolation, handoff passed');
}

main().catch(error => { console.error(error); process.exitCode = 1; });
