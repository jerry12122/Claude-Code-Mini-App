// ── 工作室時間軸的輪詢排程（無 JSX、無 React）──────────────────────────────
// 規則：只在頁籤可見時輪詢；一次做完才排下一次（不重疊、出錯也繼續）；變成不可見就取消待處理的計時器，
// 回到可見立刻補一次；stop() 之後不再有任何動作。注入 doc／計時器只為了能在 node 裡測試。

function startRoomPoller({ tick, intervalMs = ROOM_POLL_MS, doc = document, setTimer = setTimeout, clearTimer = clearTimeout }) {
  let timer = null;
  let stopped = false;
  let inFlight = false;

  const schedule = () => {
    if (stopped || doc.hidden || inFlight || timer !== null) return;
    timer = setTimer(run, intervalMs);
  };
  async function run() {
    timer = null;
    if (stopped || doc.hidden || inFlight) return;
    inFlight = true;
    try { await tick(); } catch (_) { /* 失敗由呼叫端的 tick 自己呈現；排程不能因此停掉 */ }
    inFlight = false;
    schedule();
  }
  const onVisibility = () => {
    if (stopped) return;
    if (doc.hidden) {
      if (timer !== null) { clearTimer(timer); timer = null; }
    } else if (timer === null && !inFlight) {
      run();
    }
  };

  doc.addEventListener('visibilitychange', onVisibility);
  schedule();
  return function stop() {
    stopped = true;
    if (timer !== null) { clearTimer(timer); timer = null; }
    doc.removeEventListener('visibilitychange', onVisibility);
  };
}
