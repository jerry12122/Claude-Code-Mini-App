// ── 系統資源顯示換算（純函式，無 JSX、無 React）──────────────────────────────
// 輸入是 GET /system/stats 的 JSON。cpu_percent 為 null 代表還沒有可比較的基準，要顯示佔位而不是 0%。

const SYSTEM_STATS_POLL_MS = 3000;

function formatSystemStats(s) {
  const clamp = (n) => Math.min(100, Math.max(0, n));
  const gb = (b) => (b / 1024 ** 3).toFixed(1);
  const cpu = s && typeof s.cpu_percent === 'number' ? clamp(s.cpu_percent) : null;
  const used = s ? s.mem_used_bytes : 0;
  const total = s ? s.mem_total_bytes : 0;
  const hasMem = total > 0;
  return {
    cpuPercent: cpu,
    cpuText: cpu === null ? '—' : `${cpu.toFixed(1)}%`,
    memPercent: hasMem ? clamp((used / total) * 100) : 0,
    memText: hasMem ? `${gb(Math.min(used, total))} / ${gb(total)} GB` : '—',
  };
}
