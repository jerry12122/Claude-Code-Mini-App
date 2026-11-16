// 自訂 CLI 引數（每行一個 argv）的解析與 --model 的取出／合併。純函式。

/**
 * 建立 Session 時「自訂 CLI 引數」：每行一個 argv，trim 後略過空行。
 * 路徑含空格時請整段寫在同一行（勿用空格拆成多段）。
 */
function parseCliExtraArgs(text) {
  if (text == null || !String(text).trim()) return [];
  return String(text)
    .split(/\r?\n/)
    .map((line) => line.trim())
    .filter((line) => line.length > 0);
}

function extractModelFromCliArgs(parts) {
  if (!Array.isArray(parts)) return '';
  for (let i = 0; i < parts.length; i++) {
    if (parts[i] === '--model' || parts[i] === '-m') return String(parts[i + 1] || '').trim();
  }
  return '';
}

function cliArgsWithoutModel(parts) {
  if (!Array.isArray(parts)) return [];
  const out = [];
  for (let i = 0; i < parts.length; i++) {
    if (parts[i] === '--model' || parts[i] === '-m') {
      i++;
      continue;
    }
    out.push(parts[i]);
  }
  return out;
}

function mergeModelIntoCliExtraLines(cliText, modelVal) {
  const baseParts = parseCliExtraArgs(cliText);
  const stripped = [];
  for (let i = 0; i < baseParts.length; i++) {
    if (baseParts[i] === '--model' || baseParts[i] === '-m') {
      i++;
      continue;
    }
    stripped.push(baseParts[i]);
  }
  const m = String(modelVal || '').trim();
  if (m) {
    stripped.push('--model', m);
  }
  return stripped.join('\n');
}
