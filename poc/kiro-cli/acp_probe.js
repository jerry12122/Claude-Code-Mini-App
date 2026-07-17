// 實驗：直接用 JSON-RPC over stdio 跟 `kiro-cli acp` 對話，
// 觀察 session/update 的 AgentMessageChunk / ToolCall 是否真的把工具呼叫跟文字分開，
// 以及文字內容是否含有正常的 ``` code fence（不像 chat --no-interactive 那樣被 TTY 格式化過）。
const { spawn } = require("child_process");

const proc = spawn("kiro-cli", ["acp", "--trust-all-tools"], {
  cwd: process.argv[2] || process.cwd(),
  stdio: ["pipe", "pipe", "pipe"],
});

let buf = "";
let nextId = 0;
const pending = new Map();

function send(method, params, isRequest = true) {
  const id = isRequest ? nextId++ : undefined;
  const msg = { jsonrpc: "2.0", method, params };
  if (isRequest) msg.id = id;
  proc.stdin.write(JSON.stringify(msg) + "\n");
  return id;
}

function request(method, params) {
  return new Promise((resolve, reject) => {
    const id = send(method, params, true);
    pending.set(id, { resolve, reject });
  });
}

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
    } catch (e) {
      console.log("[non-json stdout]", line);
      continue;
    }
    if (msg.method === "session/update") {
      const u = msg.params?.update;
      console.log("[session/update] type=" + (u?.sessionUpdate || u?.type) + " => " + JSON.stringify(u).slice(0, 300));
    } else if (msg.id !== undefined && pending.has(msg.id)) {
      pending.get(msg.id).resolve(msg);
      pending.delete(msg.id);
    } else {
      console.log("[other]", JSON.stringify(msg).slice(0, 300));
    }
  }
});

proc.stderr.on("data", (d) => console.error("[stderr]", d.toString()));

(async () => {
  const init = await request("initialize", {
    protocolVersion: 1,
    clientCapabilities: { fs: { readTextFile: true, writeTextFile: true }, terminal: true },
    clientInfo: { name: "acp-probe", version: "0.0.1" },
  });
  console.log("initialize result:", JSON.stringify(init.result));

  const sess = await request("session/new", { cwd: process.argv[2] || process.cwd(), mcpServers: [] });
  console.log("session/new result:", JSON.stringify(sess.result));
  const sessionId = sess.result.sessionId;

  const prompt = await request("session/prompt", {
    sessionId,
    content: [
      {
        type: "text",
        text: "讀取這個資料夾裡的其中一個 .go 檔案，貼出裡面某個函式的簽名（用 code block），並用一句話說明它做什麼。",
      },
    ],
  });
  console.log("session/prompt final result:", JSON.stringify(prompt.result));

  proc.kill();
  process.exit(0);
})().catch((e) => {
  console.error("ERROR", e);
  proc.kill();
  process.exit(1);
});
