// 帳戶用量（quota）的版型表與數值計算。純函式；顏色與元件留在 ui 層。

function formatQuotaAge(iso) {
  if (!iso) return '';
  const t = Date.parse(iso);
  if (!Number.isFinite(t)) return '';
  const sec = Math.max(0, Math.floor((Date.now() - t) / 1000));
  if (sec < 60) return '剛剛更新';
  const min = Math.floor(sec / 60);
  if (min < 60) return min + ' 分鐘前';
  return Math.floor(min / 60) + ' 小時前';
}

/** display_text 內取最大的 N% 數字（無則回 null） */
function extractMaxQuotaPercent(text) {
  const matches = String(text || '').match(/(\d+(?:\.\d+)?)%/g);
  if (!matches) return null;
  return Math.max(...matches.map((m) => parseFloat(m)));
}

/**
 * 結構化用量（quota.windows）：各 runner 的版型設定表。bars 依 window key 對應，對不到的略過；
 * tip 是 tooltip 要列的窗口（含不畫條的，例如 Cursor 的 Auto/API）；showPct 控制條旁是否帶標籤與百分比。
 */
const QUOTA_LAYOUTS = (() => {
  const fiveWeek = { groups: [{ bars: [{ key: 'session', label: '5h' }, { key: 'weekly', label: 'Week' }] }], tip: [['session', '5h'], ['weekly', 'Week']], showPct: true };
  const kiro = { groups: [{ bars: [{ key: 'credits', label: 'Credits' }] }], tip: [['credits', 'Credits']], showPct: true, showUsed: true };
  return {
    claude: fiveWeek,
    codex: fiveWeek,
    cursor: { groups: [{ bars: [{ key: 'billing_total', label: 'Total' }] }], tip: [['billing_total', 'Total'], ['billing_auto', 'Auto'], ['billing_api', 'API']], showPct: true },
    kiro,
    kiroacp: kiro,
    antigravity: {
      groups: [
        { label: 'Gemini', bars: [{ key: 'session', label: '5h' }, { key: 'weekly', label: 'Week' }] },
        { label: 'Claude/GPT', bars: [{ key: 'session_claude', label: '5h' }, { key: 'weekly_claude', label: 'Week' }] },
      ],
      tip: [['session', 'Gemini 5h'], ['weekly', 'Gemini Week'], ['session_claude', 'Claude/GPT 5h'], ['weekly_claude', 'Claude/GPT Week']],
      showPct: false,
    },
  };
})();

function quotaLayoutFor(agentType) {
  const t = String(agentType || '').toLowerCase();
  return QUOTA_LAYOUTS[t === 'gemini' ? 'antigravity' : t] || null;
}

function quotaWinPercent(w) {
  if (!w) return null;
  if (typeof w.percent === 'number' && Number.isFinite(w.percent)) return w.percent;
  if (typeof w.used === 'number' && typeof w.limit === 'number' && w.limit > 0) return (w.used / w.limit) * 100;
  return null;
}

function quotaFmtNum(n) {
  return Math.abs(n - Math.round(n)) < 0.05 ? String(Math.round(n)) : n.toFixed(1);
}

/** 對應 layout 的窗口；windows 缺失／空／全是未知 key 時回 null（呼叫端退回文字顯示） */
function resolveQuotaBars(quota, agentType) {
  const layout = quotaLayoutFor(agentType);
  if (!layout || !quota || !Array.isArray(quota.windows)) return null;
  const byKey = {};
  quota.windows.forEach((w) => { if (w && w.key && !(w.key in byKey)) byKey[w.key] = w; });
  const groups = layout.groups
    .map((g) => ({
      label: g.label,
      bars: g.bars
        .map((b) => ({ ...b, win: byKey[b.key], pct: quotaWinPercent(byKey[b.key]) }))
        .filter((b) => b.pct !== null),
    }))
    .filter((g) => g.bars.length > 0);
  return groups.length ? { layout, groups, byKey } : null;
}

/** resets_at（ISO）→ "Resets in 3 hr 25 min"；解析失敗或已過期回空字串 */
function quotaResetCountdown(iso) {
  const t = Date.parse(iso || '');
  if (!Number.isFinite(t)) return '';
  const mins = Math.round((t - Date.now()) / 60000);
  if (mins <= 0) return '';
  if (mins < 60) return 'Resets in ' + mins + ' min';
  const h = Math.floor(mins / 60);
  const m = mins % 60;
  return 'Resets in ' + h + ' hr' + (m ? ' ' + m + ' min' : '');
}

/** tooltip：各窗口名稱／百分比／倒數，最後附完整 display_text */
function quotaTooltip(quota, agentType) {
  const layout = quotaLayoutFor(agentType);
  const lines = [];
  if (layout && Array.isArray(quota.windows)) {
    layout.tip.forEach(([key, name]) => {
      const w = quota.windows.find((x) => x && x.key === key);
      const pct = quotaWinPercent(w);
      if (pct === null) return;
      let line = name + ' ' + Math.round(pct * 10) / 10 + '%';
      if (typeof w.used === 'number' && typeof w.limit === 'number') line += ' (' + quotaFmtNum(w.used) + '/' + quotaFmtNum(w.limit) + ')';
      const left = quotaResetCountdown(w.resets_at);
      if (left) line += ' · ' + left;
      lines.push(line);
    });
  }
  if (quota.plan) lines.push('方案 ' + quota.plan);
  const age = formatQuotaAge(quota.updated_at);
  if (age) lines.push(age);
  if (quota.error) lines.push(quota.error);
  if (quota.display_text && quota.display_text !== '—') lines.push(quota.display_text);
  return lines.join('\n');
}

/** 手機晶片用：版型內所有已知窗口中 percent 最高者；對不到回 null */
function quotaPeakPercent(quota, agentType) {
  const layout = quotaLayoutFor(agentType);
  if (!layout || !quota || !Array.isArray(quota.windows)) return null;
  const pcts = layout.tip.map(([key]) => quotaWinPercent(quota.windows.find((x) => x && x.key === key))).filter((p) => p !== null);
  return pcts.length ? Math.max(...pcts) : null;
}
