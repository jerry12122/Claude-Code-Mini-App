// 假的 claude CLI：依 prompt 關鍵字吐出 stream-json，供 chat-autoscroll.test.py 驅動真實 server 與前端。
// STREAM = 長輸出（含工具活動）；PERM = 結束時帶 permission_denials；沒有關鍵字則只吐幾行。
const args = process.argv.slice(2);
if (args.includes('--input-format')) process.exit(1); // 模型清單查詢：讓 server 走 fallback
let prompt = '';
process.stdin.on('data', d => (prompt += d));
process.stdin.on('end', async () => {
  const out = o => process.stdout.write(JSON.stringify(o) + '\n');
  const sleep = ms => new Promise(r => setTimeout(r, ms));
  const delta = text => out({ type: 'stream_event', event: { type: 'content_block_delta', delta: { type: 'text_delta', text } } });
  const long = /STREAM/.test(prompt) || args.includes('--resume');
  out({ type: 'system', subtype: 'init', session_id: 'fake-' + Date.now(), model: 'claude-sonnet-5-5' });
  out({ type: 'stream_event', event: { type: 'content_block_start', content_block: { type: 'text' } } });
  // ponytail: 明確 UI 測試 marker，最多存活 45 秒；測試仍須經既有 interrupt 清理自己的回合。
  const queueRunNow = prompt.match(/QUEUE_RUN_NOW_UI_([AC])/);
  if (queueRunNow) {
    delta('queue-run-now partial ' + queueRunNow[1]);
    await sleep(queueRunNow[1] === 'A' ? 45000 : 20000);
    out({ type: 'result', subtype: 'success', is_error: false, result: 'queue-run-now done', session_id: 'fake-queue-run-now', permission_denials: [] });
    return;
  }
  if (/ROOM_QUESTION/.test(prompt)) {
    const fs = require('node:fs');
    const config = JSON.parse(fs.readFileSync(args[args.indexOf('--mcp-config') + 1], 'utf8')).mcpServers.miniapp_perm;
    const headers = { ...config.headers, 'Content-Type': 'application/json', Accept: 'application/json, text/event-stream' };
    const rpc = (method, params, id) => fetch(config.url, { method: 'POST', headers,
      body: JSON.stringify({ jsonrpc: '2.0', ...(id ? { id } : {}), method, params }) });
    const init = await rpc('initialize', { protocolVersion: '2025-06-18', capabilities: {}, clientInfo: { name: 'room-fixture', version: '1' } }, 1);
    await init.text();
    if (init.headers.get('Mcp-Session-Id')) headers['Mcp-Session-Id'] = init.headers.get('Mcp-Session-Id');
    await rpc('notifications/initialized', {});
    const res = await rpc('tools/call', { name: 'claude_permission', arguments: {
      tool_name: 'AskUserQuestion', tool_use_id: 'room-question', input: { questions: [{
        question: '下一步要怎麼做？', header: '下一步', multiSelect: false,
        options: [{ label: '繼續', description: '繼續工作' }, { label: '停止', description: '停止工作' }],
      }] },
    } }, 2);
    const text = await res.text();
    if (!res.ok) throw new Error(text);
    const response = JSON.parse(text.startsWith('event:') || text.startsWith('data:')
      ? text.split('\n').filter(line => line.startsWith('data:')).at(-1).slice(5) : text);
    const decision = JSON.parse(response.result.content[0].text);
    delta('選擇結果：' + decision.updatedInput.answers['下一步要怎麼做？']);
    out({ type: 'result', subtype: 'success', is_error: false, result: '已收到回答', session_id: 'fake-question', permission_denials: [] });
    return;
  }
  for (let i = 1; i <= (long ? 220 : 6); i++) {
    delta(`第 ${i} 段：這是一段用來撐高聊天室的輸出文字，長度夠長會換行，持續串流時畫面應該一直貼著底部。\n\n`);
    if (i % 25 === 0) delta('```js\nconst a = 1;\nconst b = 2;\nconsole.log(a + b);\n```\n\n');
    if (long && i % 10 === 0) {
      const id = 'tool' + i;
      out({ type: 'stream_event', event: { type: 'content_block_start', content_block: { type: 'tool_use', name: 'Bash' } } });
      out({ type: 'assistant', message: { content: [{ type: 'tool_use', id, name: 'Bash', input: { command: 'echo ' + i } }] } });
      await sleep(120);
      out({ type: 'user', message: { content: [{ type: 'tool_result', tool_use_id: id, content: 'ok' }] } });
    }
    await sleep(40);
  }
  const denials = /PERM/.test(prompt) ? [{ tool_name: 'Bash', tool_use_id: 'tu1', tool_input: { command: 'ls -la' } }] : [];
  out({ type: 'result', subtype: 'success', is_error: false, result: 'done', session_id: 'fake-1', stop_reason: 'end_turn', permission_denials: denials });
});
