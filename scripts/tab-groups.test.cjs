const assert = require('node:assert');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');

const targetFile = path.join(__dirname, '../internal/static/js/hooks/tabGroups.js');
const source = fs.readFileSync(targetFile, 'utf8');
const context = vm.createContext({
  window: {},
  console: console,
});
vm.runInContext(source, context);

const {
  tabGroupsReducer,
  createDefaultTabGroupsState,
  DEFAULT_TAB_GROUPS_STATE,
  clampRatio,
  pickNextActiveTab,
  serializeTabGroupsState,
  deserializeTabGroupsState,
  migrateLegacySplitState,
  pruneTabGroupsState,
  MINIAPP_SESSION_MIME,
  calculateDropRegion,
  calculateTabInsertIndex,
  parseSessionDragData,
} = context;

function deepFreeze(obj) {
  if (obj === null || typeof obj !== 'object') return obj;
  Object.freeze(obj);
  for (const key of Object.keys(obj)) {
    deepFreeze(obj[key]);
  }
  return obj;
}

// ─────────────────────────────────────────────────────────────
// 1. 純函式與不可變性（Immutable State）
// ─────────────────────────────────────────────────────────────
{
  const state = deepFreeze({
    groups: [{ tabs: ['s1', 's2'], active: 's1' }],
    focused: 0,
    orientation: 'horizontal',
    ratio: 0.5,
  });

  const next1 = tabGroupsReducer(state, { type: 'open', sessionId: 's3' });
  assert.notEqual(next1, state);
  assert.deepEqual(state.groups[0].tabs, ['s1', 's2'], '原狀態不可被修改');
  assert.deepEqual(next1.groups[0].tabs, ['s1', 's2', 's3']);
  assert.equal(next1.groups[0].active, 's3');
}

// ─────────────────────────────────────────────────────────────
// 2. 開啟與去重（Open Deduplication）
// ─────────────────────────────────────────────────────────────
{
  const initial = createDefaultTabGroupsState();
  const s1 = tabGroupsReducer(initial, { type: 'open', sessionId: 'A' });
  assert.deepEqual(s1.groups[0].tabs, ['A']);
  assert.equal(s1.groups[0].active, 'A');
  assert.equal(s1.focused, 0);

  const s2 = tabGroupsReducer(s1, { type: 'open', sessionId: 'B' });
  assert.deepEqual(s2.groups[0].tabs, ['A', 'B']);
  assert.equal(s2.groups[0].active, 'B');

  // 重複開啟 A：聚焦 A，不重複新增分頁
  const s3 = tabGroupsReducer(s2, { type: 'open', sessionId: 'A' });
  assert.deepEqual(s3.groups[0].tabs, ['A', 'B']);
  assert.equal(s3.groups[0].active, 'A');
  assert.equal(s3.focused, 0);

  // 兩群組時重複開啟另一群組的分頁：聚焦該群組與該分頁
  const twoGroupsState = deepFreeze({
    groups: [
      { tabs: ['A', 'B'], active: 'B' },
      { tabs: ['C', 'D'], active: 'D' },
    ],
    focused: 1,
    orientation: 'horizontal',
    ratio: 0.5,
  });
  const switched = tabGroupsReducer(twoGroupsState, { type: 'open', sessionId: 'A' });
  assert.equal(switched.focused, 0, '應切換焦點到群組 0');
  assert.equal(switched.groups[0].active, 'A', '群組 0 active 應切換到 A');
  assert.deepEqual(switched.groups[0].tabs, ['A', 'B']);
  assert.deepEqual(switched.groups[1].tabs, ['C', 'D'], '群組 1 不應被插入重複分頁');

  // 指定 insertIndex
  const inserted = tabGroupsReducer(s2, { type: 'open', sessionId: 'C', insertIndex: 1 });
  assert.deepEqual(inserted.groups[0].tabs, ['A', 'C', 'B']);
  assert.equal(inserted.groups[0].active, 'C');
}

// ─────────────────────────────────────────────────────────────
// 3. 關閉分頁與鄰近選取（Closing Tabs & Adjacent Selection）
// ─────────────────────────────────────────────────────────────
{
  // 關閉 active 分頁，優先右側
  const state3 = deepFreeze({
    groups: [{ tabs: ['A', 'B', 'C'], active: 'B' }],
    focused: 0,
    orientation: 'horizontal',
    ratio: 0.5,
  });
  const closedB = tabGroupsReducer(state3, { type: 'close', sessionId: 'B' });
  assert.deepEqual(closedB.groups[0].tabs, ['A', 'C']);
  assert.equal(closedB.groups[0].active, 'C', '關閉 B 後應選取右側 C');

  // 關閉末尾 active 分頁，無右側時選取左側
  const stateEnd = deepFreeze({
    groups: [{ tabs: ['A', 'C'], active: 'C' }],
    focused: 0,
    orientation: 'horizontal',
    ratio: 0.5,
  });
  const closedC = tabGroupsReducer(stateEnd, { type: 'close', sessionId: 'C' });
  assert.deepEqual(closedC.groups[0].tabs, ['A']);
  assert.equal(closedC.groups[0].active, 'A', '關閉 C 後應選取左側 A');

  // 關閉非 active 分頁：active 不變
  const stateNonActive = deepFreeze({
    groups: [{ tabs: ['A', 'B', 'C'], active: 'C' }],
    focused: 0,
    orientation: 'horizontal',
    ratio: 0.5,
  });
  const closedA = tabGroupsReducer(stateNonActive, { type: 'close', sessionId: 'A' });
  assert.deepEqual(closedA.groups[0].tabs, ['B', 'C']);
  assert.equal(closedA.groups[0].active, 'C', '關閉非 active 分頁，active 應保持');

  // 兩群組時關閉第二群組最後一個分頁：第二群組消失，回退為單群組
  const twoGroups = deepFreeze({
    groups: [
      { tabs: ['A'], active: 'A' },
      { tabs: ['B'], active: 'B' },
    ],
    focused: 1,
    orientation: 'horizontal',
    ratio: 0.5,
  });
  const closedLastB = tabGroupsReducer(twoGroups, { type: 'close', sessionId: 'B' });
  assert.equal(closedLastB.groups.length, 1);
  assert.deepEqual(closedLastB.groups[0].tabs, ['A']);
  assert.equal(closedLastB.groups[0].active, 'A');
  assert.equal(closedLastB.focused, 0);

  // 關閉第一群組最後一個分頁：原本的第二群組變成群組 0
  const closedLastA = tabGroupsReducer(twoGroups, { type: 'close', sessionId: 'A' });
  assert.equal(closedLastA.groups.length, 1);
  assert.deepEqual(closedLastA.groups[0].tabs, ['B']);
  assert.equal(closedLastA.groups[0].active, 'B');
  assert.equal(closedLastA.focused, 0);

  // 單一群組關閉唯一分頁：保持單群組，tabs 為空，active 為 null
  const singleOne = deepFreeze({
    groups: [{ tabs: ['A'], active: 'A' }],
    focused: 0,
    orientation: 'horizontal',
    ratio: 0.5,
  });
  const closedOnly = tabGroupsReducer(singleOne, { type: 'close', sessionId: 'A' });
  assert.equal(closedOnly.groups.length, 1);
  assert.deepEqual(closedOnly.groups[0].tabs, []);
  assert.equal(closedOnly.groups[0].active, null);
  assert.equal(closedOnly.focused, 0);
}

// ─────────────────────────────────────────────────────────────
// 4. 跨群組與同群組 move
// ─────────────────────────────────────────────────────────────
{
  // 同群組 reorder
  const stateReorder = deepFreeze({
    groups: [{ tabs: ['A', 'B', 'C'], active: 'B' }],
    focused: 0,
    orientation: 'horizontal',
    ratio: 0.5,
  });
  const reordered = tabGroupsReducer(stateReorder, { type: 'move', sessionId: 'C', toGroup: 0, toIndex: 0 });
  assert.deepEqual(reordered.groups[0].tabs, ['C', 'A', 'B']);
  assert.equal(reordered.groups[0].active, 'C');

  // 跨群組 move
  const stateMove = deepFreeze({
    groups: [
      { tabs: ['A', 'B'], active: 'B' },
      { tabs: ['C'], active: 'C' },
    ],
    focused: 0,
    orientation: 'horizontal',
    ratio: 0.5,
  });
  const moved = tabGroupsReducer(stateMove, { type: 'move', sessionId: 'B', toGroup: 1, toIndex: 0 });
  assert.deepEqual(moved.groups[0].tabs, ['A']);
  assert.equal(moved.groups[0].active, 'A', '移走 active 後群組 0 應選相鄰');
  assert.deepEqual(moved.groups[1].tabs, ['B', 'C']);
  assert.equal(moved.groups[1].active, 'B', '移入後群組 1 active 應設為該分頁');
  assert.equal(moved.focused, 1, '焦點移至目標群組');

  // 把第二群組唯一的 tab 移到第一群組：第二群組消失
  const moveLastToGroup0 = tabGroupsReducer(moved, { type: 'move', sessionId: 'C', toGroup: 0, toIndex: 1 });
  assert.equal(moveLastToGroup0.groups.length, 2); // 還有 'B' 在群組 1
  const moveFinal = tabGroupsReducer(moveLastToGroup0, { type: 'move', sessionId: 'B', toGroup: 0, toIndex: 2 });
  assert.equal(moveFinal.groups.length, 1, '第二群組變空後應消失');
  assert.deepEqual(moveFinal.groups[0].tabs, ['A', 'C', 'B']);
  assert.equal(moveFinal.focused, 0);
}

// ─────────────────────────────────────────────────────────────
// 5. 邊緣放置 splitDrop
// ─────────────────────────────────────────────────────────────
{
  const oneGroup = deepFreeze({
    groups: [{ tabs: ['A'], active: 'A' }],
    focused: 0,
    orientation: 'horizontal',
    ratio: 0.5,
  });

  // right: orientation horizontal, new group at index 1
  const dropRight = tabGroupsReducer(oneGroup, { type: 'splitDrop', sessionId: 'B', edge: 'right' });
  assert.equal(dropRight.groups.length, 2);
  assert.deepEqual(dropRight.groups[0].tabs, ['A']);
  assert.deepEqual(dropRight.groups[1].tabs, ['B']);
  assert.equal(dropRight.orientation, 'horizontal');
  assert.equal(dropRight.focused, 1);

  // left: orientation horizontal, new group at index 0
  const dropLeft = tabGroupsReducer(oneGroup, { type: 'splitDrop', sessionId: 'B', edge: 'left' });
  assert.equal(dropLeft.groups.length, 2);
  assert.deepEqual(dropLeft.groups[0].tabs, ['B']);
  assert.deepEqual(dropLeft.groups[1].tabs, ['A']);
  assert.equal(dropLeft.orientation, 'horizontal');
  assert.equal(dropLeft.focused, 0);

  // bottom: orientation vertical, new group at index 1
  const dropBottom = tabGroupsReducer(oneGroup, { type: 'splitDrop', sessionId: 'B', edge: 'bottom' });
  assert.equal(dropBottom.groups.length, 2);
  assert.deepEqual(dropBottom.groups[0].tabs, ['A']);
  assert.deepEqual(dropBottom.groups[1].tabs, ['B']);
  assert.equal(dropBottom.orientation, 'vertical');
  assert.equal(dropBottom.focused, 1);

  // top: orientation vertical, new group at index 0
  const dropTop = tabGroupsReducer(oneGroup, { type: 'splitDrop', sessionId: 'B', edge: 'top' });
  assert.equal(dropTop.groups.length, 2);
  assert.deepEqual(dropTop.groups[0].tabs, ['B']);
  assert.deepEqual(dropTop.groups[1].tabs, ['A']);
  assert.equal(dropTop.orientation, 'vertical');
  assert.equal(dropTop.focused, 0);

  // 已有兩個群組時 edge drop：退化為加入該群組，不產生第三群組
  const dropOnTwo = tabGroupsReducer(dropRight, { type: 'splitDrop', sessionId: 'C', edge: 'right' });
  assert.equal(dropOnTwo.groups.length, 2, '已有兩群組時不可建立第三群組');
}

// ─────────────────────────────────────────────────────────────
// 6. toggleSplit 兩種方向
// ─────────────────────────────────────────────────────────────
{
  // 1 -> 2: 有 >= 2 分頁時分割，將 active 移入第二群組
  const singleTwoTabs = deepFreeze({
    groups: [{ tabs: ['A', 'B'], active: 'B' }],
    focused: 0,
    orientation: 'horizontal',
    ratio: 0.5,
  });
  const splitState = tabGroupsReducer(singleTwoTabs, { type: 'toggleSplit' });
  assert.equal(splitState.groups.length, 2);
  assert.deepEqual(splitState.groups[0].tabs, ['A']);
  assert.equal(splitState.groups[0].active, 'A');
  assert.deepEqual(splitState.groups[1].tabs, ['B']);
  assert.equal(splitState.groups[1].active, 'B');
  assert.equal(splitState.focused, 1);

  // 2 -> 1: 合併第二群組回第一群組
  const mergedState = tabGroupsReducer(splitState, { type: 'toggleSplit' });
  assert.equal(mergedState.groups.length, 1);
  assert.deepEqual(mergedState.groups[0].tabs, ['A', 'B']);
  assert.equal(mergedState.focused, 0);

  // 1 -> 2: 只有 1 個分頁時不動作
  const singleOneTab = deepFreeze({
    groups: [{ tabs: ['A'], active: 'A' }],
    focused: 0,
    orientation: 'horizontal',
    ratio: 0.5,
  });
  const unchange = tabGroupsReducer(singleOneTab, { type: 'toggleSplit' });
  assert.equal(unchange.groups.length, 1);
  assert.deepEqual(unchange.groups[0].tabs, ['A']);
}

// ─────────────────────────────────────────────────────────────
// 7. focus, setRatio, setOrientation
// ─────────────────────────────────────────────────────────────
{
  const state = deepFreeze({
    groups: [{ tabs: ['A'], active: 'A' }, { tabs: ['B'], active: 'B' }],
    focused: 0,
    orientation: 'horizontal',
    ratio: 0.5,
  });
  const f1 = tabGroupsReducer(state, { type: 'focus', groupIndex: 1 });
  assert.equal(f1.focused, 1);

  const rClampedLow = tabGroupsReducer(state, { type: 'setRatio', ratio: 0.05 });
  assert.equal(rClampedLow.ratio, 0.2);

  const rClampedHigh = tabGroupsReducer(state, { type: 'setRatio', ratio: 0.95 });
  assert.equal(rClampedHigh.ratio, 0.8);

  const oVert = tabGroupsReducer(state, { type: 'setOrientation', orientation: 'vertical' });
  assert.equal(oVert.orientation, 'vertical');
}

// ─────────────────────────────────────────────────────────────
// 8. 序列化、損毀字串容錯、舊狀態遷移（Task 1.2）
// ─────────────────────────────────────────────────────────────
{
  // 正常序列化與反序列化
  const state = {
    groups: [{ tabs: ['A', 'B'], active: 'A' }, { tabs: ['C'], active: 'C' }],
    focused: 1,
    orientation: 'vertical',
    ratio: 0.45,
  };
  const jsonStr = serializeTabGroupsState(state);
  const deserialized = deserializeTabGroupsState(jsonStr);
  assert.deepEqual(deserialized, state);

  // 損毀字串退回單一空群組，不報錯
  const corrupted = deserializeTabGroupsState('not a valid json{');
  assert.equal(corrupted.groups.length, 1);
  assert.deepEqual(corrupted.groups[0].tabs, []);
  assert.equal(corrupted.groups[0].active, null);

  const nullVal = deserializeTabGroupsState(null);
  assert.equal(nullVal.groups.length, 1);
  assert.deepEqual(nullVal.groups[0].tabs, []);

  // 舊狀態遷移：主 A、副 B、enabled、orientation vertical、ratio 0.4
  const legacyEnabled = {
    enabled: true,
    secondarySessionId: 'B',
    orientation: 'vertical',
    ratio: 0.4,
  };
  const migrated = migrateLegacySplitState(legacyEnabled, 'A');
  assert.equal(migrated.groups.length, 2);
  assert.deepEqual(migrated.groups[0].tabs, ['A']);
  assert.equal(migrated.groups[0].active, 'A');
  assert.deepEqual(migrated.groups[1].tabs, ['B']);
  assert.equal(migrated.groups[1].active, 'B');
  assert.equal(migrated.orientation, 'vertical');
  assert.equal(migrated.ratio, 0.4);
  assert.equal(migrated.focused, 0);

  // 舊狀態自帶 primarySessionId
  const legacyWithPrimary = {
    enabled: true,
    primarySessionId: 'P',
    secondarySessionId: 'S',
    orientation: 'horizontal',
    ratio: 0.6,
  };
  const migratedWithPrimary = migrateLegacySplitState(legacyWithPrimary);
  assert.equal(migratedWithPrimary.groups.length, 2);
  assert.deepEqual(migratedWithPrimary.groups[0].tabs, ['P']);
  assert.deepEqual(migratedWithPrimary.groups[1].tabs, ['S']);

  // clear action: 清空回單一空群組
  const cleared = tabGroupsReducer(migratedWithPrimary, { type: 'clear' });
  assert.equal(cleared.groups.length, 1);
  assert.deepEqual(cleared.groups[0].tabs, []);
  assert.equal(cleared.groups[0].active, null);
  assert.equal(cleared.focused, 0);
}

// ─────────────────────────────────────────────────────────────
// 9. Prune 移除不存在的會話與空群組
// ─────────────────────────────────────────────────────────────
{
  const state = deepFreeze({
    groups: [
      { tabs: ['A', 'B'], active: 'B' },
      { tabs: ['C'], active: 'C' },
    ],
    focused: 1,
    orientation: 'horizontal',
    ratio: 0.5,
  });

  // 'B' 被刪除：群組 0 active 改選相鄰 A
  const pruned1 = pruneTabGroupsState(state, ['A', 'C']);
  assert.equal(pruned1.groups.length, 2);
  assert.deepEqual(pruned1.groups[0].tabs, ['A']);
  assert.equal(pruned1.groups[0].active, 'A');

  // 'C' 被刪除：群組 1 變空而消失，回到單一群組
  const pruned2 = pruneTabGroupsState(state, ['A', 'B']);
  assert.equal(pruned2.groups.length, 1);
  assert.deepEqual(pruned2.groups[0].tabs, ['A', 'B']);
  assert.equal(pruned2.groups[0].active, 'B');
  assert.equal(pruned2.focused, 0);

  // 全都被刪除：回到單一空群組
  const prunedAll = pruneTabGroupsState(state, ['X', 'Y']);
  assert.equal(prunedAll.groups.length, 1);
  assert.deepEqual(prunedAll.groups[0].tabs, []);
  assert.equal(prunedAll.groups[0].active, null);
  assert.equal(prunedAll.focused, 0);
}

// ─────────────────────────────────────────────────────────────
// 10. 拖放輔助純函式（區域判定、插入索引、資料解析）
// ─────────────────────────────────────────────────────────────
{
  const rect = { left: 100, top: 100, width: 400, height: 400 };

  // (1) calculateDropRegion: 單一群組剛好 25% 線與內部中心
  // left: relX = (200 - 100) / 400 = 0.25 (剛好 25% 線)
  assert.equal(calculateDropRegion(rect, 200, 300, 1), 'left', '剛好 25% 線應判定為 left');
  // right: relX = (400 - 100) / 400 = 0.75 (剛好 75% 線，距離右側 25%)
  assert.equal(calculateDropRegion(rect, 400, 300, 1), 'right', '剛好 75% 線應判定為 right');
  // top: relY = (200 - 100) / 400 = 0.25 (剛好 25% 線)
  assert.equal(calculateDropRegion(rect, 300, 200, 1), 'top', '剛好 25% 線應判定為 top');
  // bottom: relY = (400 - 100) / 400 = 0.75 (剛好 75% 線，距離底側 25%)
  assert.equal(calculateDropRegion(rect, 300, 400, 1), 'bottom', '剛好 75% 線應判定為 bottom');

  // 超出 25% 線進入中央區域
  assert.equal(calculateDropRegion(rect, 201, 300, 1), 'center', '超過 25% 線進入中央');
  assert.equal(calculateDropRegion(rect, 399, 300, 1), 'center', '未達 75% 線為中央');
  assert.equal(calculateDropRegion(rect, 300, 300, 1), 'center', '正中央位置為 center');

  // 兩群組時邊緣一律為 center（不建第三群組）
  assert.equal(calculateDropRegion(rect, 110, 300, 2), 'center', '兩群組時左邊緣一律為 center');
  assert.equal(calculateDropRegion(rect, 490, 300, 2), 'center', '兩群組時右邊緣一律為 center');
  assert.equal(calculateDropRegion(rect, 300, 110, 2), 'center', '兩群組時頂邊緣一律為 center');
  assert.equal(calculateDropRegion(rect, 300, 490, 2), 'center', '兩群組時底邊緣一律為 center');

  // 異常 rect 防護
  assert.equal(calculateDropRegion(null, 100, 100, 1), 'center');
  assert.equal(calculateDropRegion({ left: 0, top: 0, width: 0, height: 0 }, 10, 10, 1), 'center');

  // (2) calculateTabInsertIndex: 空分頁列與多分頁中線判定
  assert.equal(calculateTabInsertIndex([], 100), 0, '空分頁列插入索引為 0');

  const tabRects = [
    { left: 0, width: 100 },   // mid = 50
    { left: 100, width: 100 }, // mid = 150
  ];
  assert.equal(calculateTabInsertIndex(tabRects, 30), 0, '滑鼠在 tab 0 中線左側，插入索引 0');
  assert.equal(calculateTabInsertIndex(tabRects, 100), 1, '滑鼠在 tab 0 與 1 之間，插入索引 1');
  assert.equal(calculateTabInsertIndex(tabRects, 180), 2, '滑鼠在 tab 1 中線右側，插入索引 2');

  // 同列拖曳排除自身（例如拖曳 tab 0，排除後僅剩原 tab 1 在 mid = 150）
  assert.equal(calculateTabInsertIndex(tabRects, 30, 0), 0, '排除自身後在 tab 1 前，索引為 0');
  assert.equal(calculateTabInsertIndex(tabRects, 180, 0), 1, '排除自身後在 tab 1 後，索引為 1');

  // (3) parseSessionDragData: 解析有效與無效資料
  const validSidebar = JSON.stringify({ sessionId: 's-1', fromGroup: null });
  assert.deepEqual(parseSessionDragData(validSidebar), { sessionId: 's-1', fromGroup: null });

  const validGroup = JSON.stringify({ sessionId: 's-2', fromGroup: 1 });
  assert.deepEqual(parseSessionDragData(validGroup), { sessionId: 's-2', fromGroup: 1 });

  const fakeDataTransfer = {
    getData: (mime) => (mime === MINIAPP_SESSION_MIME ? validGroup : ''),
  };
  assert.deepEqual(parseSessionDragData(fakeDataTransfer), { sessionId: 's-2', fromGroup: 1 });

  assert.equal(parseSessionDragData(null), null);
  assert.equal(parseSessionDragData('not json'), null);
  assert.equal(parseSessionDragData(JSON.stringify({ fromGroup: 0 })), null);
}
// ─────────────────────────────────────────────────────────────
// 11. 放置動作解析 resolveDropAction 與 isSessionDragTypes（4.1 / 4.2）
// ─────────────────────────────────────────────────────────────
{
  const { resolveDropAction, isSessionDragTypes } = context;
  const one = { groups: [{ tabs: ['A', 'B'], active: 'A' }], focused: 0, orientation: 'horizontal', ratio: 0.5 };
  const two = {
    groups: [{ tabs: ['A', 'B'], active: 'A' }, { tabs: ['C'], active: 'C' }],
    focused: 0, orientation: 'horizontal', ratio: 0.5,
  };
  const apply = (st, a) => (a ? tabGroupsReducer(st, a) : st);

  // 側欄新會話 → 分頁列指定索引插入並成為作用中
  let s = apply(one, resolveDropAction(one, 'D', { kind: 'tabbar', group: 0, index: 1 }));
  assert.deepEqual(s.groups[0].tabs, ['A', 'D', 'B']);
  assert.equal(s.groups[0].active, 'D');

  // 內容區中央 → 加入該群組
  s = apply(one, resolveDropAction(one, 'D', { kind: 'content', group: 0, region: 'center' }));
  assert.deepEqual(s.groups[0].tabs, ['A', 'B', 'D']);

  // 單一群組：右邊緣=horizontal、被拖會話在右側；下邊緣=vertical、在下方
  s = apply(one, resolveDropAction(one, 'D', { kind: 'content', group: 0, region: 'right' }));
  assert.equal(s.groups.length, 2);
  assert.equal(s.orientation, 'horizontal');
  assert.deepEqual(s.groups[1].tabs, ['D']);
  s = apply(one, resolveDropAction(one, 'D', { kind: 'content', group: 0, region: 'bottom' }));
  assert.equal(s.orientation, 'vertical');
  assert.deepEqual(s.groups[1].tabs, ['D']);
  s = apply(one, resolveDropAction(one, 'D', { kind: 'content', group: 0, region: 'left' }));
  assert.deepEqual(s.groups[0].tabs, ['D']);

  // 兩群組：邊緣一律加入該群組，不產生第三群組
  s = apply(two, resolveDropAction(two, 'D', { kind: 'content', group: 1, region: 'right' }));
  assert.equal(s.groups.length, 2);
  assert.deepEqual(s.groups[1].tabs, ['C', 'D']);

  // 同列重排（索引相對於「移除自身後」的分頁）：B 拖到 A 之前
  s = apply(one, resolveDropAction(one, 'B', { kind: 'tabbar', group: 0, index: 0 }));
  assert.deepEqual(s.groups[0].tabs, ['B', 'A']);
  assert.equal(s.groups[0].active, 'B', '拖入已開啟的非作用中會話應切換到該分頁');

  // 目標群組未聚焦：同列放置仍須切換群組與作用中分頁
  const unfocused = deepFreeze({ ...two, focused: 1 });
  s = apply(unfocused, resolveDropAction(unfocused, 'B', { kind: 'tabbar', group: 0, index: 1 }));
  assert.deepEqual(s.groups[0].tabs, ['A', 'B']);
  assert.equal(s.groups[0].active, 'B');
  assert.equal(s.focused, 0);

  // 內容中央放置已開啟的會話：保留順序並切換分頁與群組焦點
  s = apply(unfocused, resolveDropAction(unfocused, 'B', { kind: 'content', group: 0, region: 'center' }));
  assert.deepEqual(s.groups[0].tabs, ['A', 'B']);
  assert.equal(s.groups[0].active, 'B');
  assert.equal(s.focused, 0);

  // 側欄拖已開啟在別群組的會話 → 視為移動（不是只聚焦）
  s = apply(two, resolveDropAction(two, 'C', { kind: 'content', group: 0, region: 'center' }));
  assert.equal(s.groups.length, 1, '唯一分頁被移走後第二群組消失');
  assert.deepEqual(s.groups[0].tabs, ['A', 'B', 'C']);
  assert.equal(s.groups[0].active, 'C');

  // 跨群組移動到另一群組分頁列
  s = apply(two, resolveDropAction(two, 'B', { kind: 'tabbar', group: 1, index: 0 }));
  assert.deepEqual(s.groups[0].tabs, ['A']);
  assert.deepEqual(s.groups[1].tabs, ['B', 'C']);

  // 放回已聚焦且作用中的會話 → 狀態不變
  assert.equal(apply(two, resolveDropAction(two, 'A', { kind: 'content', group: 0, region: 'center' })), two);
  // 單一群組、唯一分頁拖到邊緣 → 不會留下空群組
  const lone = { groups: [{ tabs: ['A'], active: 'A' }], focused: 0, orientation: 'horizontal', ratio: 0.5 };
  s = apply(lone, resolveDropAction(lone, 'A', { kind: 'content', group: 0, region: 'right' }));
  assert.equal(s.groups.length, 1);
  // 目標無效 → null
  assert.equal(resolveDropAction(one, '', { kind: 'content', group: 0, region: 'center' }), null);
  assert.equal(resolveDropAction(one, 'D', null), null);

  // isSessionDragTypes：只接受含自訂 MIME 的拖曳（檔案拖曳不可被劫持）
  assert.equal(isSessionDragTypes(['Files']), false);
  assert.equal(isSessionDragTypes(['text/plain', MINIAPP_SESSION_MIME]), true);
  assert.equal(isSessionDragTypes({ contains: (t) => t === MINIAPP_SESSION_MIME }), true);
  assert.equal(isSessionDragTypes(null), false);
}


// ─────────────────────────────────────────────────────────────
// 12. 快捷鍵（5.1）：toggleSplit、focus 與按鍵對應 resolveTabShortcut
// ─────────────────────────────────────────────────────────────
{
  const { resolveTabShortcut } = context;
  const mk = (groups, focused = 0) => deepFreeze({ groups, focused, orientation: 'horizontal', ratio: 0.5 });

  // 只有一個分頁：Ctrl+\ 不動作（回傳同一個 state）
  const solo = mk([{ tabs: ['A'], active: 'A' }]);
  assert.equal(tabGroupsReducer(solo, { type: 'toggleSplit' }), solo);
  const empty = mk([{ tabs: [], active: null }]);
  assert.equal(tabGroupsReducer(empty, { type: 'toggleSplit' }), empty);

  // 單一群組、兩個以上分頁：作用中分頁移到新的第二群組並聚焦
  const two = mk([{ tabs: ['A', 'B'], active: 'B' }]);
  let s = tabGroupsReducer(two, { type: 'toggleSplit' });
  assert.equal(s.groups.length, 2);
  assert.deepEqual(s.groups[0].tabs, ['A']);
  assert.equal(s.groups[0].active, 'A');
  assert.deepEqual(s.groups[1].tabs, ['B']);
  assert.equal(s.focused, 1);

  // 作用中是中間分頁：另一群組保留順序
  s = tabGroupsReducer(mk([{ tabs: ['A', 'B', 'C'], active: 'B' }]), { type: 'toggleSplit' });
  assert.deepEqual(s.groups[0].tabs, ['A', 'C']);
  assert.deepEqual(s.groups[1].tabs, ['B']);

  // 兩群組：合併，第二群組分頁接在第一群組後面，作用中沿用第一群組
  const merged = tabGroupsReducer(
    mk([{ tabs: ['A', 'B'], active: 'A' }, { tabs: ['C', 'D'], active: 'D' }], 1),
    { type: 'toggleSplit' }
  );
  assert.equal(merged.groups.length, 1);
  assert.deepEqual(merged.groups[0].tabs, ['A', 'B', 'C', 'D']);
  assert.equal(merged.groups[0].active, 'A');
  assert.equal(merged.focused, 0);

  // 分割後再合併，分頁集合不變
  const roundTrip = tabGroupsReducer(tabGroupsReducer(two, { type: 'toggleSplit' }), { type: 'toggleSplit' });
  assert.deepEqual([...roundTrip.groups[0].tabs].sort(), ['A', 'B']);

  // focus：邊界與單一群組
  const dbl = mk([{ tabs: ['A'], active: 'A' }, { tabs: ['B'], active: 'B' }], 0);
  assert.equal(tabGroupsReducer(dbl, { type: 'focus', groupIndex: 1 }).focused, 1);
  assert.equal(tabGroupsReducer(dbl, { type: 'focus', groupIndex: 0 }), dbl, '已聚焦同群組回傳原 state');
  assert.equal(tabGroupsReducer(dbl, { type: 'focus', groupIndex: 2 }), dbl, '超出範圍忽略');
  assert.equal(tabGroupsReducer(dbl, { type: 'focus', groupIndex: -1 }), dbl, '負數忽略');
  assert.equal(tabGroupsReducer(solo, { type: 'focus', groupIndex: 1 }), solo, '單一群組無第二群組可聚焦');

  // 按鍵對應
  const key = (o) => ({ key: '', code: '', ctrlKey: false, metaKey: false, altKey: false, shiftKey: false, isComposing: false, ...o });
  assert.deepEqual(resolveTabShortcut(key({ key: '\\', ctrlKey: true })), { type: 'toggleSplit' });
  assert.deepEqual(resolveTabShortcut(key({ key: '\\', metaKey: true })), { type: 'toggleSplit' });
  assert.deepEqual(resolveTabShortcut(key({ key: 'ArrowLeft', ctrlKey: true, altKey: true })), { type: 'focus', groupIndex: 0 });
  assert.deepEqual(resolveTabShortcut(key({ key: 'ArrowRight', ctrlKey: true, altKey: true })), { type: 'focus', groupIndex: 1 });
  // 不衝突：沒有修飾鍵、輸入框一般打字、Ctrl+Shift+\、AltGr(ctrl+alt)+\ 、單獨方向鍵
  assert.equal(resolveTabShortcut(key({ key: '\\' })), null);
  assert.equal(resolveTabShortcut(key({ key: 'a', ctrlKey: true })), null);
  assert.equal(resolveTabShortcut(key({ key: '\\', ctrlKey: true, shiftKey: true })), null);
  assert.equal(resolveTabShortcut(key({ key: '\\', ctrlKey: true, altKey: true })), null, 'AltGr+\\ 是打字');
  assert.equal(resolveTabShortcut(key({ key: 'ArrowLeft' })), null);
  assert.equal(resolveTabShortcut(key({ key: 'ArrowLeft', ctrlKey: true })), null, '只有 Ctrl 的 Left 是移動游標');
  // IME 組字中不觸發
  assert.equal(resolveTabShortcut(key({ key: '\\', ctrlKey: true, isComposing: true })), null);
  assert.equal(resolveTabShortcut(key({ key: 'ArrowLeft', ctrlKey: true, altKey: true, isComposing: true })), null);
  assert.equal(resolveTabShortcut(null), null);
}



console.log('tab-groups checks passed');

