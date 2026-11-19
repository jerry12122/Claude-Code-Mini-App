// 額度自動繼續橫幅的純狀態。只顯示 sync／queue_update 已帶來的狀態，不倒數、不改佇列。

function emptyQuotaResume() {
  return { state: 'none', at: '' };
}

function quotaResumeFromMessage(msg) {
  const hasState = Object.prototype.hasOwnProperty.call(msg, 'quota_resume_state');
  const hasAt = Object.prototype.hasOwnProperty.call(msg, 'quota_resume_at');
  if (!hasState && !hasAt) return emptyQuotaResume();
  const state = hasState && typeof msg.quota_resume_state === 'string' ? msg.quota_resume_state : 'none';
  const at = hasAt && typeof msg.quota_resume_at === 'string' ? msg.quota_resume_at : '';
  if (state !== 'waiting' && state !== 'resuming' && state !== 'exhausted' && state !== 'none') {
    return emptyQuotaResume();
  }
  return { state, at };
}

function reduceQuotaResume(prev, msg) {
  if (!msg || (msg.type !== 'sync' && msg.type !== 'queue_update')) return prev || emptyQuotaResume();
  return quotaResumeFromMessage(msg);
}

// 以瀏覽器本地時區把既有 UTC 時刻顯示成 24 小時 HH:MM。無效時刻留空，不另造時間。
function quotaResumeLocalHM(at, timeZone) {
  if (typeof at !== 'string' || at === '') return '';
  const date = new Date(at);
  if (Number.isNaN(date.getTime())) return '';
  const opts = { hour: '2-digit', minute: '2-digit', hourCycle: 'h23' };
  if (timeZone) opts.timeZone = timeZone;
  try {
    return new Intl.DateTimeFormat('en-GB', opts).format(date);
  } catch (err) {
    return '';
  }
}

function quotaResumeBannerDescriptor(input) {
  const resume = input && input.resume ? input.resume : emptyQuotaResume();
  let text = '';
  let cancel = false;
  if (resume.state === 'exhausted') {
    text = '已達自動繼續上限，請手動繼續';
  } else if (resume.state === 'waiting') {
    const hm = quotaResumeLocalHM(resume.at);
    text = hm ? `額度用盡，預計 ${hm} 自動繼續` : '額度用盡，預計自動繼續';
    cancel = !!(input && input.isOwner);
  } else {
    return null;
  }
  return {
    role: 'status',
    live: 'polite',
    text,
    button: cancel ? { type: 'button', name: '取消自動繼續', disabled: !(input && input.connected) } : null,
  };
}
