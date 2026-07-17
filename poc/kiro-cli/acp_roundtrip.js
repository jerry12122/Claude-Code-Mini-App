/**
 * ACP roundtrip POC：驗證 sessionId / model / 文字·工具分離 / markdown fence。
 *
 * Usage:
 *   node poc/kiro-cli/acp_roundtrip.js [workDir]
 *
 * Exit 0 = 成功標準全過；非 0 = 有失敗項（見 stdout 摘要與 samples_acp_report.json）
 */
const { spawn } = require("child_process");
const fs = require("fs");
const path = require("path");

const workDir = process.argv[2] || process.cwd();
const outDir = path.join(__dirname);
const reportPath = path.join(outDir, "samples_acp_report.json");
const chunksPath = path.join(outDir, "samples_acp_chunks.md");

const PROMPT =
  "請用 fs 工具讀取目前目錄裡任意一個 .go 或 .md 檔案的一小段內容，" +
  "然後在回覆裡用 markdown code fence（三個反引號）貼出其中 3~10 行，" +
  "並用一句話說明。不要只描述，一定要有 ``` 程式碼區塊。";

function main() {
  const proc = spawn("kiro-cli", ["acp", "--trust-all-tools"], {
    cwd: workDir,
    stdio: ["pipe", "pipe", "pipe"],
  });

  let buf = "";
  let nextId = 1;
  const pending = new Map();
  const updates = [];
  const stderrChunks = [];
  let sessionId = "";
  let currentModelId = "";
  let availableModels = [];

  function send(method, params, isNotification = false) {
    const msg = { jsonrpc: "2.0", method, params };
    if (!isNotification) {
      msg.id = nextId++;
      proc.stdin.write(JSON.stringify(msg) + "\n");
      return msg.id;
    }
    proc.stdin.write(JSON.stringify(msg) + "\n");
    return undefined;
  }

  function request(method, params, timeoutMs = 120000) {
    return new Promise((resolve, reject) => {
      const id = send(method, params);
      const timer = setTimeout(() => {
        pending.delete(id);
        reject(new Error(`timeout waiting for ${method} id=${id}`));
      }, timeoutMs);
      pending.set(id, {
        resolve: (msg) => {
          clearTimeout(timer);
          resolve(msg);
        },
        reject: (err) => {
          clearTimeout(timer);
          reject(err);
        },
      });
    });
  }

  function handleLine(line) {
    if (!line.trim()) return;
    let msg;
    try {
      msg = JSON.parse(line);
    } catch {
      stderrChunks.push("[non-json stdout] " + line.slice(0, 200));
      return;
    }

    if (msg.method === "session/update") {
      updates.push(msg.params);
      return;
    }

    // ACP may send client requests (fs/read etc). Decline minimally.
    if (msg.method && msg.id !== undefined && !msg.result && !msg.error) {
      proc.stdin.write(
        JSON.stringify({
          jsonrpc: "2.0",
          id: msg.id,
          error: { code: -32601, message: `client method not implemented: ${msg.method}` },
        }) + "\n"
      );
      return;
    }

    if (msg.id !== undefined && pending.has(msg.id)) {
      const p = pending.get(msg.id);
      pending.delete(msg.id);
      if (msg.error) p.reject(new Error(JSON.stringify(msg.error)));
      else p.resolve(msg);
    }
  }

  proc.stdout.on("data", (chunk) => {
    buf += chunk.toString("utf8");
    let idx;
    while ((idx = buf.indexOf("\n")) >= 0) {
      const line = buf.slice(0, idx);
      buf = buf.slice(idx + 1);
      handleLine(line);
    }
  });
  proc.stderr.on("data", (d) => stderrChunks.push(d.toString()));

  (async () => {
    const report = {
      workDir,
      checks: {},
      sessionId: null,
      currentModelId: null,
      updateTypes: [],
      agentText: "",
      toolCallCount: 0,
      hasFence: false,
      stderrTail: "",
      error: null,
    };

    try {
      const init = await request("initialize", {
        protocolVersion: 1,
        clientCapabilities: {
          fs: { readTextFile: false, writeTextFile: false },
        },
        clientInfo: { name: "acp-roundtrip-poc", version: "0.1.0" },
      });
      report.checks.initialize = !!init.result;

      // Some ACP agents expect an initialized notification
      send("initialized", {}, true);

      const sess = await request("session/new", { cwd: workDir, mcpServers: [] });
      sessionId = sess.result?.sessionId || "";
      currentModelId = sess.result?.models?.currentModelId || "";
      availableModels = sess.result?.models?.availableModels || [];
      report.sessionId = sessionId;
      report.currentModelId = currentModelId;
      report.availableModels = availableModels;
      report.checks.sessionId = !!sessionId;
      report.checks.model = !!currentModelId;

      const promptResult = await request(
        "session/prompt",
        {
          sessionId,
          prompt: [{ type: "text", text: PROMPT }],
        },
        180000
      );
      report.promptResult = promptResult.result || promptResult.error || null;
      report.checks.promptReturned = true;

      // Collect typed updates
      const typeSet = new Set();
      let agentText = "";
      let toolCalls = 0;
      for (const p of updates) {
        const u = p.update || p;
        const t = u.sessionUpdate || u.type || "unknown";
        typeSet.add(t);
        // Agent message chunk shapes vary; try common fields
        if (/agent_message|AgentMessage|message_chunk|agent_message_chunk/i.test(t) || t === "agent_message_chunk") {
          const content = u.content || u.message || u;
          if (typeof content === "string") agentText += content;
          else if (content?.text) agentText += content.text;
          else if (Array.isArray(content)) {
            for (const c of content) {
              if (typeof c === "string") agentText += c;
              else if (c?.text) agentText += c.text;
            }
          } else if (u.text) agentText += u.text;
        }
        // Also catch nested content.type === text
        if (u.content?.type === "text" && u.content.text) {
          agentText += u.content.text;
        }
        if (/tool_call/i.test(t)) toolCalls++;
      }

      // Fallback: dump raw updates if text empty — parse more liberally
      if (!agentText) {
        for (const p of updates) {
          const raw = JSON.stringify(p);
          // pull "text":"..." fragments from agent message updates only
          if (/agent_message/i.test(raw) || /message_chunk/i.test(raw)) {
            const re = /"text"\s*:\s*"((?:\\.|[^"\\])*)"/g;
            let m;
            while ((m = re.exec(raw))) {
              try {
                agentText += JSON.parse('"' + m[1] + '"');
              } catch {
                agentText += m[1];
              }
            }
          }
        }
      }

      report.updateTypes = [...typeSet];
      report.agentText = agentText;
      report.toolCallCount = toolCalls;
      report.rawUpdateCount = updates.length;
      report.rawUpdatesSample = updates.slice(0, 8);

      const hasNarration =
        /Reading file:|Successfully read|Completed in \d|Searching for:/.test(agentText);
      const hasFence = /```/.test(agentText);
      report.hasFence = hasFence;
      report.hasToolNarrationInText = hasNarration;
      report.checks.textToolSeparated = toolCalls > 0 ? !hasNarration : !hasNarration;
      report.checks.hasFence = hasFence;
      report.checks.gotUpdates = updates.length > 0;

      // Soft resume check: load same session
      try {
        const loaded = await request("session/load", { sessionId, cwd: workDir }, 30000);
        report.checks.sessionLoad = !!(loaded.result || loaded.result === null || !loaded.error);
        report.loadResultKeys = loaded.result ? Object.keys(loaded.result) : [];
      } catch (e) {
        report.checks.sessionLoad = false;
        report.sessionLoadError = String(e.message || e);
      }
    } catch (e) {
      report.error = String(e.message || e);
    } finally {
      report.stderrTail = stderrChunks.join("").slice(-1500);
      fs.writeFileSync(reportPath, JSON.stringify(report, null, 2), "utf8");
      fs.writeFileSync(
        chunksPath,
        `# ACP agent text\n\n${report.agentText || "(empty)"}\n`,
        "utf8"
      );

      const required = ["sessionId", "model", "gotUpdates", "textToolSeparated", "hasFence"];
      const failed = required.filter((k) => !report.checks[k]);
      console.log(JSON.stringify({ checks: report.checks, failed, sessionId: report.sessionId, model: report.currentModelId, toolCalls: report.toolCallCount, textLen: (report.agentText || "").length, updateTypes: report.updateTypes }, null, 2));
      console.log("report:", reportPath);
      console.log("chunks:", chunksPath);

      try {
        proc.kill();
      } catch {}
      process.exit(failed.length || report.error ? 1 : 0);
    }
  })();
}

main();
