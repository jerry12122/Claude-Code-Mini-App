/**
 * 測試：process A session/new → kill → process B session/load → prompt
 */
const { spawn } = require("child_process");
const workDir = process.argv[2] || process.cwd();

function runAcp() {
  const proc = spawn("kiro-cli", ["acp", "--trust-all-tools"], {
    cwd: workDir,
    stdio: ["pipe", "pipe", "pipe"],
  });
  let buf = "";
  let nextId = 1;
  const pending = new Map();

  proc.stdout.on("data", (chunk) => {
    buf += chunk.toString("utf8");
    let idx;
    while ((idx = buf.indexOf("\n")) >= 0) {
      const line = buf.slice(0, idx);
      buf = buf.slice(idx + 1);
      if (!line.trim()) continue;
      let msg;
      try {
        msg = JSON.parse(line);
      } catch {
        continue;
      }
      if (msg.method && msg.id !== undefined && msg.result === undefined && msg.error === undefined) {
        proc.stdin.write(
          JSON.stringify({
            jsonrpc: "2.0",
            id: msg.id,
            error: { code: -32601, message: "not implemented" },
          }) + "\n"
        );
        continue;
      }
      if (msg.id !== undefined && pending.has(msg.id)) {
        const p = pending.get(msg.id);
        pending.delete(msg.id);
        if (msg.error) p.reject(new Error(JSON.stringify(msg.error)));
        else p.resolve(msg);
      }
    }
  });

  function request(method, params, timeoutMs = 60000) {
    return new Promise((resolve, reject) => {
      const id = nextId++;
      proc.stdin.write(JSON.stringify({ jsonrpc: "2.0", id, method, params }) + "\n");
      const timer = setTimeout(() => {
        pending.delete(id);
        reject(new Error("timeout " + method));
      }, timeoutMs);
      pending.set(id, {
        resolve: (m) => {
          clearTimeout(timer);
          resolve(m);
        },
        reject: (e) => {
          clearTimeout(timer);
          reject(e);
        },
      });
    });
  }

  return {
    proc,
    request,
    initialized: async () => {
      await request("initialize", {
        protocolVersion: 1,
        clientCapabilities: { fs: { readTextFile: false, writeTextFile: false } },
        clientInfo: { name: "load-cross-proc", version: "0.1" },
      });
      proc.stdin.write(JSON.stringify({ jsonrpc: "2.0", method: "initialized", params: {} }) + "\n");
    },
    kill: () => {
      try {
        proc.kill();
      } catch {}
    },
  };
}

(async () => {
  const a = runAcp();
  await a.initialized();
  const created = await a.request("session/new", { cwd: workDir, mcpServers: [] });
  const sessionId = created.result.sessionId;
  console.log("created", sessionId);
  await a.request("session/prompt", {
    sessionId,
    prompt: [{ type: "text", text: "用一個英文單字打招呼" }],
  });
  console.log("first prompt done");
  a.kill();

  await new Promise((r) => setTimeout(r, 1000));

  const b = runAcp();
  await b.initialized();
  try {
    const loaded = await b.request("session/load", { sessionId, cwd: workDir }, 45000);
    console.log("load result keys", Object.keys(loaded.result || {}), "model", loaded.result?.models?.currentModelId);
    const p2 = await b.request("session/prompt", {
      sessionId,
      prompt: [{ type: "text", text: "上一個單字是什麼？只回那個字。" }],
    });
    console.log("second prompt ok", JSON.stringify(p2.result).slice(0, 200));
  } catch (e) {
    console.error("RESUME FAIL", e.message);
    process.exitCode = 1;
  } finally {
    b.kill();
  }
})().catch((e) => {
  console.error(e);
  process.exit(1);
});
