// ── 分頁群組狀態管理（VSCode-style Session Tabs）──────────────────────────────
// 純函式實作，無 DOM、無 React 相依性，無 import/export（依賴全域命名空間）。

const TAB_GROUPS_STORAGE_KEY = 'miniapp_tab_groups_v1';
const LEGACY_SPLIT_STORAGE_KEY = 'miniapp_split_chat_state';

const DEFAULT_TAB_GROUPS_STATE = Object.freeze({
  groups: Object.freeze([Object.freeze({ tabs: Object.freeze([]), active: null })]),
  focused: 0,
  orientation: 'horizontal',
  ratio: 0.5,
});

function createDefaultTabGroupsState() {
  return {
    groups: [{ tabs: [], active: null }],
    focused: 0,
    orientation: 'horizontal',
    ratio: 0.5,
  };
}

function clampRatio(r) {
  if (typeof r !== 'number' || isNaN(r)) return 0.5;
  return Math.max(0.2, Math.min(0.8, Math.round(r * 1000) / 1000));
}

function pickNextActiveTab(tabs, removedIndex) {
  if (!Array.isArray(tabs) || tabs.length === 0) return null;
  if (removedIndex < tabs.length && removedIndex >= 0) return tabs[removedIndex];
  if (removedIndex - 1 >= 0 && removedIndex - 1 < tabs.length) return tabs[removedIndex - 1];
  return tabs[0] || null;
}

function tabGroupsReducer(state, action) {
  if (!state || !Array.isArray(state.groups)) {
    state = createDefaultTabGroupsState();
  }
  if (!action || !action.type) return state;

  switch (action.type) {
    case 'open': {
      const { sessionId, groupIndex, insertIndex } = action;
      if (!sessionId) return state;

      // 1. 去重檢查：若已存在於任一群組中，直接切換到該群組與該分頁
      for (let g = 0; g < state.groups.length; g++) {
        const grp = state.groups[g];
        const idx = grp.tabs.indexOf(sessionId);
        if (idx !== -1) {
          if (state.focused === g && grp.active === sessionId) {
            return state;
          }
          return {
            ...state,
            focused: g,
            groups: state.groups.map((group, i) =>
              i === g ? { ...group, active: sessionId } : group
            ),
          };
        }
      }

      // 2. 不存在：插入至指定或聚焦的目標群組
      let targetGroupIdx =
        typeof groupIndex === 'number' && groupIndex >= 0 && groupIndex < state.groups.length
          ? groupIndex
          : (state.focused >= 0 && state.focused < state.groups.length ? state.focused : 0);

      const targetGroup = state.groups[targetGroupIdx];
      let nextTabs;
      if (typeof insertIndex === 'number' && insertIndex >= 0 && insertIndex <= targetGroup.tabs.length) {
        nextTabs = [
          ...targetGroup.tabs.slice(0, insertIndex),
          sessionId,
          ...targetGroup.tabs.slice(insertIndex),
        ];
      } else {
        nextTabs = [...targetGroup.tabs, sessionId];
      }

      return {
        ...state,
        focused: targetGroupIdx,
        groups: state.groups.map((group, i) =>
          i === targetGroupIdx ? { ...group, tabs: nextTabs, active: sessionId } : group
        ),
      };
    }

    case 'close': {
      const { sessionId, groupIndex } = action;
      if (!sessionId) return state;

      let targetGroupIdx = -1;
      let tabIdx = -1;

      if (typeof groupIndex === 'number' && groupIndex >= 0 && groupIndex < state.groups.length) {
        const idx = state.groups[groupIndex].tabs.indexOf(sessionId);
        if (idx !== -1) {
          targetGroupIdx = groupIndex;
          tabIdx = idx;
        }
      }
      if (targetGroupIdx === -1) {
        for (let g = 0; g < state.groups.length; g++) {
          const idx = state.groups[g].tabs.indexOf(sessionId);
          if (idx !== -1) {
            targetGroupIdx = g;
            tabIdx = idx;
            break;
          }
        }
      }

      if (targetGroupIdx === -1) return state;

      const targetGroup = state.groups[targetGroupIdx];
      const newTabs = targetGroup.tabs.filter((_, i) => i !== tabIdx);

      let newActive = targetGroup.active;
      if (targetGroup.active === sessionId) {
        newActive = pickNextActiveTab(newTabs, tabIdx);
      }

      // 群組若變空
      if (newTabs.length === 0) {
        if (state.groups.length === 2) {
          const remainingGroup = state.groups[targetGroupIdx === 0 ? 1 : 0];
          return {
            ...state,
            groups: [{ ...remainingGroup }],
            focused: 0,
          };
        }
        return {
          ...state,
          groups: [{ tabs: [], active: null }],
          focused: 0,
        };
      }

      return {
        ...state,
        groups: state.groups.map((group, i) =>
          i === targetGroupIdx ? { ...group, tabs: newTabs, active: newActive } : group
        ),
      };
    }

    case 'move': {
      const { sessionId, toGroup, toIndex } = action;
      if (!sessionId) return state;

      let fromGroup = -1;
      let fromIndex = -1;
      for (let g = 0; g < state.groups.length; g++) {
        const idx = state.groups[g].tabs.indexOf(sessionId);
        if (idx !== -1) {
          fromGroup = g;
          fromIndex = idx;
          break;
        }
      }
      if (fromGroup === -1) return state;

      const targetGroupIdx = Math.max(
        0,
        Math.min(state.groups.length - 1, typeof toGroup === 'number' ? toGroup : 0)
      );

      if (fromGroup === targetGroupIdx) {
        const grp = state.groups[fromGroup];
        const remainingTabs = grp.tabs.filter((_, i) => i !== fromIndex);
        const targetIndex = Math.max(
          0,
          Math.min(remainingTabs.length, typeof toIndex === 'number' ? toIndex : remainingTabs.length)
        );
        const nextTabs = [
          ...remainingTabs.slice(0, targetIndex),
          sessionId,
          ...remainingTabs.slice(targetIndex),
        ];
        return {
          ...state,
          focused: fromGroup,
          groups: state.groups.map((group, i) =>
            i === fromGroup ? { ...group, tabs: nextTabs, active: sessionId } : group
          ),
        };
      }

      // 跨群組移動
      const srcGroup = state.groups[fromGroup];
      const destGroup = state.groups[targetGroupIdx];

      const srcRemainingTabs = srcGroup.tabs.filter((_, i) => i !== fromIndex);
      let srcNewActive = srcGroup.active;
      if (srcGroup.active === sessionId) {
        srcNewActive = pickNextActiveTab(srcRemainingTabs, fromIndex);
      }

      const destRemainingTabs = destGroup.tabs.filter((id) => id !== sessionId);
      const targetIndex = Math.max(
        0,
        Math.min(destRemainingTabs.length, typeof toIndex === 'number' ? toIndex : destRemainingTabs.length)
      );
      const destNextTabs = [
        ...destRemainingTabs.slice(0, targetIndex),
        sessionId,
        ...destRemainingTabs.slice(targetIndex),
      ];

      // 若來源群組最後一個分頁離開，第二群組消失
      if (srcRemainingTabs.length === 0 && state.groups.length === 2) {
        return {
          ...state,
          groups: [{ tabs: destNextTabs, active: sessionId }],
          focused: 0,
        };
      }

      return {
        ...state,
        focused: targetGroupIdx,
        groups: state.groups.map((group, i) => {
          if (i === fromGroup) {
            return { ...group, tabs: srcRemainingTabs, active: srcNewActive };
          }
          if (i === targetGroupIdx) {
            return { ...group, tabs: destNextTabs, active: sessionId };
          }
          return group;
        }),
      };
    }

    case 'splitDrop': {
      const { sessionId, edge } = action;
      if (!sessionId) return state;

      if (state.groups.length > 1) {
        return tabGroupsReducer(state, { type: 'open', sessionId, groupIndex: state.focused });
      }

      const orientation = edge === 'top' || edge === 'bottom' ? 'vertical' : 'horizontal';
      const currentGroup = state.groups[0];

      const existingIdx = currentGroup.tabs.indexOf(sessionId);
      let updatedCurrentGroup = currentGroup;

      if (existingIdx !== -1) {
        if (currentGroup.tabs.length <= 1) {
          return state;
        }
        const newTabs = currentGroup.tabs.filter((_, i) => i !== existingIdx);
        let newActive = currentGroup.active;
        if (currentGroup.active === sessionId) {
          newActive = pickNextActiveTab(newTabs, existingIdx);
        }
        updatedCurrentGroup = { ...currentGroup, tabs: newTabs, active: newActive };
      }

      const newGroup = { tabs: [sessionId], active: sessionId };

      if (edge === 'left' || edge === 'top') {
        return {
          ...state,
          orientation,
          focused: 0,
          groups: [newGroup, updatedCurrentGroup],
        };
      }

      return {
        ...state,
        orientation,
        focused: 1,
        groups: [updatedCurrentGroup, newGroup],
      };
    }

    case 'toggleSplit': {
      if (state.groups.length > 1) {
        const group0 = state.groups[0];
        const group1 = state.groups[1];
        const mergedTabs = [...group0.tabs];
        for (const t of group1.tabs) {
          if (!mergedTabs.includes(t)) {
            mergedTabs.push(t);
          }
        }
        const mergedActive = group0.active || group1.active || (mergedTabs[0] || null);
        return {
          ...state,
          groups: [{ tabs: mergedTabs, active: mergedActive }],
          focused: 0,
        };
      }

      const group0 = state.groups[0];
      if (group0.tabs.length >= 2) {
        const activeTab = group0.active || group0.tabs[group0.tabs.length - 1];
        const activeIdx = group0.tabs.indexOf(activeTab);
        const newTabs0 = group0.tabs.filter((_, i) => i !== activeIdx);
        const newActive0 = pickNextActiveTab(newTabs0, activeIdx);

        return {
          ...state,
          focused: 1,
          groups: [
            { ...group0, tabs: newTabs0, active: newActive0 },
            { tabs: [activeTab], active: activeTab },
          ],
        };
      }

      if (action && action.secondSessionId && !group0.tabs.includes(action.secondSessionId)) {
        return {
          ...state,
          focused: 1,
          groups: [
            { ...group0 },
            { tabs: [action.secondSessionId], active: action.secondSessionId },
          ],
        };
      }

      return state;
    }

    case 'focus': {
      const idx = typeof action.groupIndex === 'number' ? action.groupIndex : 0;
      if (idx >= 0 && idx < state.groups.length && idx !== state.focused) {
        return { ...state, focused: idx };
      }
      return state;
    }

    case 'setRatio': {
      const nextRatio = clampRatio(action.ratio);
      if (nextRatio !== state.ratio) {
        return { ...state, ratio: nextRatio };
      }
      return state;
    }

    case 'setOrientation': {
      const nextOrientation = action.orientation === 'vertical' ? 'vertical' : 'horizontal';
      if (nextOrientation !== state.orientation) {
        return { ...state, orientation: nextOrientation };
      }
      return state;
    }

    case 'prune': {
      return pruneTabGroupsState(state, action.validSessionIds);
    }

    case 'clear': {
      return {
        ...state,
        groups: [{ tabs: [], active: null }],
        focused: 0,
      };
    }

    default:
      return state;
  }
}

function pruneTabGroupsState(state, validSessionIds) {
  if (!state || !Array.isArray(state.groups)) return createDefaultTabGroupsState();
  const validSet = new Set(Array.isArray(validSessionIds) ? validSessionIds : []);

  const prunedGroups = [];
  for (const group of state.groups) {
    if (!group || !Array.isArray(group.tabs)) continue;
    const newTabs = group.tabs.filter((id) => validSet.has(id));
    if (newTabs.length > 0) {
      let newActive = group.active;
      if (!newTabs.includes(newActive)) {
        const origIdx = group.tabs.indexOf(group.active);
        newActive = pickNextActiveTab(newTabs, origIdx !== -1 ? origIdx : 0) || newTabs[0];
      }
      prunedGroups.push({ tabs: newTabs, active: newActive });
    }
  }

  if (prunedGroups.length === 0) {
    return {
      ...state,
      groups: [{ tabs: [], active: null }],
      focused: 0,
    };
  }

  const nextFocused = Math.max(0, Math.min(prunedGroups.length - 1, state.focused || 0));
  return {
    ...state,
    groups: prunedGroups,
    focused: nextFocused,
  };
}

function migrateLegacySplitState(legacyState, primarySessionId) {
  const isPortrait = typeof window !== 'undefined' && window.innerHeight > window.innerWidth;
  const defaultOrientation = isPortrait ? 'vertical' : 'horizontal';

  if (!legacyState || typeof legacyState !== 'object') {
    return {
      groups: [{ tabs: primarySessionId ? [primarySessionId] : [], active: primarySessionId || null }],
      focused: 0,
      orientation: defaultOrientation,
      ratio: 0.5,
    };
  }

  const orientation =
    legacyState.orientation === 'vertical'
      ? 'vertical'
      : legacyState.orientation === 'horizontal'
      ? 'horizontal'
      : defaultOrientation;
  const ratio = clampRatio(legacyState.ratio);

  const primary = primarySessionId || legacyState.primarySessionId || null;
  const group0 = {
    tabs: primary ? [primary] : [],
    active: primary || null,
  };

  if (legacyState.enabled && legacyState.secondarySessionId && legacyState.secondarySessionId !== primary) {
    const group1 = {
      tabs: [legacyState.secondarySessionId],
      active: legacyState.secondarySessionId,
    };
    return {
      groups: [group0, group1],
      focused: 0,
      orientation,
      ratio,
    };
  }

  return {
    groups: [group0],
    focused: 0,
    orientation,
    ratio,
  };
}

function serializeTabGroupsState(state) {
  if (!state) return '';
  return JSON.stringify({
    groups: (state.groups || []).map((g) => ({
      tabs: Array.isArray(g.tabs) ? [...g.tabs] : [],
      active: g.active || null,
    })),
    focused: state.focused === 1 ? 1 : 0,
    orientation: state.orientation === 'vertical' ? 'vertical' : 'horizontal',
    ratio: clampRatio(state.ratio),
  });
}

function deserializeTabGroupsState(raw) {
  if (!raw || typeof raw !== 'string') return createDefaultTabGroupsState();
  try {
    const parsed = JSON.parse(raw);
    if (!parsed || typeof parsed !== 'object' || !Array.isArray(parsed.groups) || parsed.groups.length === 0) {
      return createDefaultTabGroupsState();
    }

    const seenTabs = new Set();
    const groups = [];

    for (const g of parsed.groups.slice(0, 2)) {
      if (!g || !Array.isArray(g.tabs)) continue;
      const validTabs = [];
      for (const t of g.tabs) {
        if (typeof t === 'string' && t && !seenTabs.has(t)) {
          seenTabs.add(t);
          validTabs.push(t);
        }
      }
      let active =
        typeof g.active === 'string' && validTabs.includes(g.active) ? g.active : validTabs[0] || null;
      groups.push({ tabs: validTabs, active });
    }

    if (groups.length === 0) {
      return createDefaultTabGroupsState();
    }

    const focused = parsed.focused === 1 && groups.length > 1 ? 1 : 0;
    const orientation = parsed.orientation === 'vertical' ? 'vertical' : 'horizontal';
    const ratio = clampRatio(parsed.ratio);

    return {
      groups,
      focused,
      orientation,
      ratio,
    };
  } catch (_) {
    return createDefaultTabGroupsState();
  }
}

function readStoredTabGroupsState(storage, primarySessionId = null) {
  const store = storage || (typeof localStorage !== 'undefined' ? localStorage : null);
  if (!store) return createDefaultTabGroupsState();

  try {
    const rawNew = store.getItem(TAB_GROUPS_STORAGE_KEY);
    if (rawNew) {
      return deserializeTabGroupsState(rawNew);
    }
    const rawLegacy = store.getItem(LEGACY_SPLIT_STORAGE_KEY);
    if (rawLegacy) {
      const parsedLegacy = JSON.parse(rawLegacy);
      return migrateLegacySplitState(parsedLegacy, primarySessionId);
    }
  } catch (_) {}

  return createDefaultTabGroupsState();
}

// ── 拖放輔助純函式（Drag and Drop Helpers）─────────────────────────

var MINIAPP_SESSION_MIME = 'application/x-miniapp-session';

/**
 * 計算拖曳至群組內容區時的落入區域（'center' | 'left' | 'right' | 'top' | 'bottom'）
 * - groupCount > 1 時一律回傳 'center'（不建立第三群組）
 * - groupCount <= 1 時，四邊緣各佔 25% 尺寸，超過 25% 或中心為 'center'
 */
function calculateDropRegion(rect, clientX, clientY, groupCount = 1) {
  if (groupCount > 1) {
    return 'center';
  }
  if (!rect || typeof rect.width !== 'number' || typeof rect.height !== 'number') {
    return 'center';
  }
  const { left = 0, top = 0, width = 0, height = 0 } = rect;
  if (width <= 0 || height <= 0) {
    return 'center';
  }

  const relX = (clientX - left) / width;
  const relY = (clientY - top) / height;

  const distLeft = relX;
  const distRight = 1 - relX;
  const distTop = relY;
  const distBottom = 1 - relY;

  const minDist = Math.min(distLeft, distRight, distTop, distBottom);

  if (minDist <= 0.25) {
    if (minDist === distLeft) return 'left';
    if (minDist === distRight) return 'right';
    if (minDist === distTop) return 'top';
    if (minDist === distBottom) return 'bottom';
  }

  return 'center';
}

/**
 * 計算拖曳至分頁列時的插入索引（依據各分頁中線判斷）
 * - tabRects: 每個 tab 的 DOMRect（需包含 left 與 width）
 * - clientX: 滑鼠 X 座標
 * - draggedIndex: 若在同群組內拖曳，排除自身後計算目標索引
 */
function calculateTabInsertIndex(tabRects, clientX, draggedIndex = -1) {
  if (!Array.isArray(tabRects) || tabRects.length === 0) {
    return 0;
  }
  const rects = (typeof draggedIndex === 'number' && draggedIndex >= 0 && draggedIndex < tabRects.length)
    ? tabRects.filter((_, idx) => idx !== draggedIndex)
    : tabRects;

  if (rects.length === 0) {
    return 0;
  }

  for (let i = 0; i < rects.length; i++) {
    const rect = rects[i];
    if (!rect) continue;
    const midX = (rect.left || 0) + (rect.width || 0) / 2;
    if (clientX < midX) {
      return i;
    }
  }
  return rects.length;
}

/**
 * 解析拖放資料中的會話資料
 */
function parseSessionDragData(dataTransfer) {
  if (!dataTransfer) return null;
  try {
    let raw = '';
    if (typeof dataTransfer.getData === 'function') {
      raw = dataTransfer.getData(MINIAPP_SESSION_MIME);
    } else if (typeof dataTransfer === 'string') {
      raw = dataTransfer;
    }
    if (!raw) return null;
    const parsed = JSON.parse(raw);
    if (!parsed || typeof parsed.sessionId !== 'string' || !parsed.sessionId) {
      return null;
    }
    return {
      sessionId: parsed.sessionId,
      fromGroup: typeof parsed.fromGroup === 'number' ? parsed.fromGroup : null,
    };
  } catch (_) {
    return null;
  }
}

/**
 * 判斷拖曳是否為會話拖曳（dragover 階段只能讀 dataTransfer.types，不能讀內容）。
 * 避免劫持檔案附件等其他拖曳。
 */
function isSessionDragTypes(types) {
  if (!types) return false;
  if (typeof types.includes === 'function') return types.includes(MINIAPP_SESSION_MIME);
  if (typeof types.contains === 'function') return types.contains(MINIAPP_SESSION_MIME);
  return false;
}

/**
 * 將放置事件轉成 reducer 動作（回傳 null 表示不動作）。
 * - 以 state 為準判斷會話現在位於哪個群組，不信任拖曳資料的 fromGroup
 * - target: { kind: 'tabbar', group, index } | { kind: 'content', group, region }
 * - tabbar 的 index 以「移除被拖曳分頁後」的分頁列計算（見 calculateTabInsertIndex）
 */
function resolveDropAction(state, sessionId, target) {
  if (!sessionId || !target || !state || !Array.isArray(state.groups)) return null;
  const loc = state.groups.findIndex((g) => g.tabs.includes(sessionId));
  const group = target.group;

  if (target.kind === 'tabbar') {
    return loc === -1
      ? { type: 'open', sessionId, groupIndex: group, insertIndex: target.index }
      : { type: 'move', sessionId, toGroup: group, toIndex: target.index };
  }
  if (target.kind !== 'content') return null;

  if (state.groups.length === 1 && target.region && target.region !== 'center') {
    return { type: 'splitDrop', sessionId, edge: target.region };
  }
  if (loc === group) return { type: 'open', sessionId, groupIndex: group };
  return loc === -1
    ? { type: 'open', sessionId, groupIndex: group }
    : { type: 'move', sessionId, toGroup: group };
}

/**
 * 鍵盤事件 → 分頁群組動作（純函式，回傳 null 表示不處理）。
 * - Ctrl/Cmd+\（不含 Alt/Shift）→ toggleSplit；Ctrl+Alt+←/→ → focus 群組 0/1
 * - 排除 AltGr（部分鍵盤的 \ 需要 Ctrl+Alt）與 IME 組字中（isComposing / keyCode 229）
 */
function resolveTabShortcut(e) {
  if (!e || e.isComposing || e.keyCode === 229) return null;
  const mod = e.ctrlKey || e.metaKey;
  if (!mod) return null;
  if (e.key === '\\' && !e.altKey && !e.shiftKey) return { type: 'toggleSplit' };
  if (e.altKey && !e.shiftKey && e.key === 'ArrowLeft') return { type: 'focus', groupIndex: 0 };
  if (e.altKey && !e.shiftKey && e.key === 'ArrowRight') return { type: 'focus', groupIndex: 1 };
  return null;
}
