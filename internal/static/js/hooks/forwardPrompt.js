// 轉送與「新建 session 後送第一則訊息」：以短命 WebSocket 送出並等待確認。
// 連線生命週期由此檔擁有（見 docs/spec/frontend-architecture.md 第 2 節）。測試：scripts/ux-reliability.test.cjs。

function buildForwardUserPrompt(note, originalBody) {
  const n = String(note || '').trim();
  const o = String(originalBody || '').trim();
  if (!n) return o;
  return `${n}\n\n---\n（以下為規劃內容）\n\n${o}`;
}

function sendPromptViaEphemeralWS(sessionId, text) {
  const record = beginPendingInput(sessionId, { data: text });
  return sendPendingInput(sessionId, record, (data, attachmentIds, requestId) => new Promise((resolve, reject) => {
    let settled = false;
    const settle = (fn) => {
      if (settled) return;
      settled = true;
      clearTimeout(timer);
      fn();
      try { ws.close(); } catch (_) {}
    };
    const ws = new WebSocket(wsURL(sessionId, { historyLimit: 1 }));
    const timer = setTimeout(() => {
      settle(() => reject(new Error('尚未收到送出確認，請確認目標會話後再重試')));
    }, 20000);
    ws.onopen = () => {
      try {
        ws.send(JSON.stringify({ type: 'input', data, attachment_ids: attachmentIds, request_id: requestId }));
      } catch (e) {
        settle(() => reject(e));
        try {
          ws.close();
        } catch (_) {}
        return;
      }
    };
    ws.onmessage = (event) => {
      let msg;
      try { msg = JSON.parse(event.data); } catch (_) { return; }
      if (msg.request_id === requestId && msg.type === 'input_accepted') settle(resolve);
      else if (msg.type === 'error' || (msg.request_id === requestId && msg.type === 'input_rejected')) {
        settle(() => reject(Object.assign(new Error(msg.content || '訊息未送出'), { rejected: msg.type === 'input_rejected' && !['update_in_progress', 'retry_input'].includes(msg.code) })));
      }
    };
    ws.onerror = () => settle(() => reject(new Error('WebSocket 錯誤')));
    ws.onclose = () => {
      settle(() => reject(new Error('連線中斷，尚未確認訊息送出')));
    };
  }));
}

/**
 * 新建 session 後送第一則訊息。失敗時 session 已存在，不能讓使用者重按（會重複建立）也不能丟訊息，
 * 保留原訊息與 ID，進聊天室後可安全重試確認。回傳是否送成功。
 */
async function sendFirstPromptOrDraft(sessionId, text) {
  try {
    await sendPromptViaEphemeralWS(sessionId, text);
    return true;
  } catch (err) {
    showToast(`${err.message || '送出未確認'}；內容已保留，請進聊天室重試確認`, { error: true, duration: 5000 });
    return false;
  }
}

// 接手草稿：MCP 讀原歷史，否則帶入有限歷史；最後需求始終完整。
function buildHandoffDraftMessage(handoff, historyMessages) {
  const h = handoff || {};
  const lastPrompt = String(h.last_prompt || '').trim();
  const reason = String(h.reason || '').trim();
  const head = `原會話（miniapp session_id=${h.old_session_id || ''}，名稱「${h.old_name || ''}」）的 ${h.agent_type || ''} 這次執行失敗（${reason}），需要你接手。\n接手前請先確認 git／檔案現況，避免重做已完成的步驟。`;

  let historyBlock;
  if (h.mcp_enabled) {
    historyBlock = `有 miniapp MCP：請先呼叫 get_messages（session_id="${h.old_session_id || ''}"）讀取舊會話完整歷史，弄清楚進度。`;
  } else {
    const all = (Array.isArray(historyMessages) ? historyMessages : [])
      .filter((m) => m && (m.role === 'user' || m.role === 'claude') && String(m.content || m.resultText || '').trim());
    const countTruncated = all.length > 20;
    const lines = all.slice(-20);
    if (lines.length === 0) {
      historyBlock = 'MCP 未啟用，且目前讀不到舊會話的歷史訊息，請只依下方「最後需求」處理。';
    } else {
      let remaining = 20000;
      let charTruncated = false;
      const kept = [];
      // ponytail: 最近 20 則、文字最多 20,000 字元，優先保留最新尾段；完整歷史由 MCP 讀取。
      for (let i = lines.length - 1; i >= 0; i--) {
        if (remaining === 0) {
          charTruncated = true;
          break;
        }
        const m = lines[i];
        const full = String(m.role === 'claude' ? (m.resultText || m.content || '') : (m.content || '')).trim();
        if (!full) continue;
        charTruncated ||= full.length > remaining;
        const text = full.slice(-remaining);
        remaining -= text.length;
        kept.unshift(`[${m.role === 'user' ? '使用者' : h.agent_type || 'agent'}] ${text}`);
      }
      const noteParts = [];
      if (countTruncated) noteParts.push(`僅最近 20 則，共 ${all.length} 則，有限裁切`);
      if (charTruncated) noteParts.push('已達字數上限，較舊內容已截斷');
      const note = noteParts.length ? `（${noteParts.join('；')}）` : '';
      historyBlock = `MCP 未啟用，附上現有聊天室的有限歷史${note}：\n---\n${kept.join('\n---\n')}\n---`;
    }
  }

  const tail = lastPrompt
    ? `請完成使用者最後這則需求：\n---\n${lastPrompt}`
    : '原會話沒有可接續的最後需求文字。';

  return `${head}\n${historyBlock}\n${tail}`;
}
