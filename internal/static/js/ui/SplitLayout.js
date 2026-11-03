// ── 雙欄分割佈局元件 (Split View) ──────────────────────────────────────────────

const SPLIT_MIN_PANE_WIDTH_PX = 320;
const SPLIT_MIN_PANE_HEIGHT_PX = 200;
const SPLIT_RATIO_MIN = 0.2;
const SPLIT_RATIO_MAX = 0.8;
const SPLIT_RATIO_DEFAULT = 0.5;

/** 可拖曳之分割分隔條（支援垂直左右與水平上下雙向） */
function SplitDivider({ containerRef, ratio, onRatioChange, onReset, orientation = 'horizontal' }) {
  const isVertical = orientation === 'vertical';

  const onMouseDown = useCallback((e) => {
    if (e.button !== undefined && e.button !== 0) return;
    e.preventDefault();

    const container = containerRef.current;
    if (!container) return;

    const rect = container.getBoundingClientRect();

    let minRatio, maxRatio;
    if (isVertical) {
      const containerHeight = rect.height;
      if (containerHeight <= SPLIT_MIN_PANE_HEIGHT_PX * 2) return;
      minRatio = SPLIT_MIN_PANE_HEIGHT_PX / containerHeight;
      maxRatio = (containerHeight - SPLIT_MIN_PANE_HEIGHT_PX) / containerHeight;
    } else {
      const containerWidth = rect.width;
      if (containerWidth <= SPLIT_MIN_PANE_WIDTH_PX * 2) return;
      minRatio = SPLIT_MIN_PANE_WIDTH_PX / containerWidth;
      maxRatio = (containerWidth - SPLIT_MIN_PANE_WIDTH_PX) / containerWidth;
    }

    const onMove = (ev) => {
      ev.preventDefault();
      let newRatio;
      if (isVertical) {
        const clientY = ev.touches && ev.touches.length ? ev.touches[0].clientY : ev.clientY;
        const offset = clientY - rect.top;
        newRatio = offset / rect.height;
      } else {
        const clientX = ev.touches && ev.touches.length ? ev.touches[0].clientX : ev.clientX;
        const offset = clientX - rect.left;
        newRatio = offset / rect.width;
      }
      newRatio = Math.max(minRatio, Math.min(maxRatio, newRatio));
      newRatio = Math.max(SPLIT_RATIO_MIN, Math.min(SPLIT_RATIO_MAX, newRatio));
      onRatioChange(newRatio);
    };

    const onUp = () => {
      document.removeEventListener('mousemove', onMove);
      document.removeEventListener('mouseup', onUp);
      document.removeEventListener('touchmove', onMove);
      document.removeEventListener('touchend', onUp);
      document.removeEventListener('touchcancel', onUp);
      document.body.style.cursor = '';
      document.body.style.userSelect = '';
    };

    if (e.type === 'touchstart') {
      document.addEventListener('touchmove', onMove, { passive: false });
      document.addEventListener('touchend', onUp);
      document.addEventListener('touchcancel', onUp);
    } else {
      document.addEventListener('mousemove', onMove);
      document.addEventListener('mouseup', onUp);
      document.body.style.cursor = isVertical ? 'row-resize' : 'col-resize';
      document.body.style.userSelect = 'none';
    }
  }, [containerRef, onRatioChange, isVertical]);

  return (
    <div
      role="separator"
      aria-orientation={isVertical ? 'horizontal' : 'vertical'}
      aria-valuenow={Math.round(ratio * 100)}
      aria-valuemin={Math.round(SPLIT_RATIO_MIN * 100)}
      aria-valuemax={Math.round(SPLIT_RATIO_MAX * 100)}
      title={isVertical ? '拖曳調整上下高度（雙擊重設為 50/50）' : '拖曳調整兩欄寬度（雙擊重設為 50/50）'}
      className={`group relative shrink-0 select-none touch-none bg-ra-raised hover:bg-[oklch(0.62_0.19_275_/30%)] transition-colors z-10 ${
        isVertical ? 'h-1.5 w-full cursor-row-resize' : 'w-1.5 h-full cursor-col-resize'
      }`}
      onMouseDown={onMouseDown}
      onTouchStart={onMouseDown}
      onDoubleClick={onReset}
    >
      <span
        className={`pointer-events-none absolute bg-ra-active group-hover:bg-violet-400 transition-all ${
          isVertical
            ? 'left-0 right-0 top-1/2 h-px -translate-y-1/2 group-hover:h-0.5'
            : 'top-0 bottom-0 left-1/2 w-px -translate-x-1/2 group-hover:w-0.5'
        }`}
        aria-hidden
      />
    </div>
  );
}

/** 雙欄聊天容器 */
function SplitChatContainer({
  isSplit,
  splitRatio = SPLIT_RATIO_DEFAULT,
  onSplitRatioChange,
  orientation = 'horizontal',
  onToggleOrientation,
  activePane = 'primary',
  onFocusPane,
  primaryContent,
  secondaryContent,
  isNarrow = false,
  mobileActivePane: mobileActivePaneProp,
  onMobileActivePaneChange,
  primarySessionName = '主聊天室',
  secondarySessionName = '副聊天室',
  onCloseSecondary,
}) {
  const containerRef = useRef(null);
  const [internalActivePane, setInternalActivePane] = useState('primary');
  const effectiveMobilePane = mobileActivePaneProp !== undefined ? mobileActivePaneProp : internalActivePane;

  const [windowDims, setWindowDims] = useState(() => ({
    w: typeof window !== 'undefined' ? window.innerWidth : 1200,
    h: typeof window !== 'undefined' ? window.innerHeight : 900,
  }));

  useEffect(() => {
    const handleResize = () => {
      setWindowDims({ w: window.innerWidth, h: window.innerHeight });
    };
    window.addEventListener('resize', handleResize);
    return () => window.removeEventListener('resize', handleResize);
  }, []);

  // 垂直分割時只在高度小於 600px 才退回分頁模式；水平分割時在寬度小於 900px 時退回分頁模式
  const isTooSmall = orientation === 'vertical' ? windowDims.h < 600 : windowDims.w < 900;
  const effectiveNarrow = isNarrow || isTooSmall;

  const handleResetRatio = useCallback(() => {
    onSplitRatioChange?.(SPLIT_RATIO_DEFAULT);
  }, [onSplitRatioChange]);

  const handleTabSwitch = useCallback((pane) => {
    setInternalActivePane(pane);
    onMobileActivePaneChange?.(pane);
    onFocusPane?.(pane);
  }, [onMobileActivePaneChange, onFocusPane]);

  // 單欄模式：直接渲染主內容
  if (!isSplit) {
    return <div className="h-full w-full min-w-0 min-h-0 flex-1">{primaryContent}</div>;
  }

  // 窄螢幕模式：顯示分頁標籤（Tab Switcher），並渲染當前選取的面板
  if (effectiveNarrow) {
    return (
      <div className="h-full w-full min-w-0 min-h-0 flex flex-col bg-ra-bg">
        {/* 窄螢幕分頁切換列 */}
        <div className="shrink-0 flex items-center justify-between border-b border-ra-line-soft bg-ra-sunken px-3 py-1.5 gap-2">
          <div className="flex items-center gap-1 min-w-0 flex-1">
            <button
              type="button"
              onClick={() => handleTabSwitch('primary')}
              className={`px-3 py-1 rounded-md text-xs font-medium truncate max-w-[140px] transition-colors ${
                effectiveMobilePane === 'primary'
                  ? 'bg-violet-600 text-white shadow-sm'
                  : 'text-gray-400 hover:text-gray-200 hover:bg-gray-800'
              }`}
            >
              {primarySessionName}
            </button>
            <button
              type="button"
              onClick={() => handleTabSwitch('secondary')}
              className={`px-3 py-1 rounded-md text-xs font-medium truncate max-w-[140px] transition-colors ${
                effectiveMobilePane === 'secondary'
                  ? 'bg-violet-600 text-white shadow-sm'
                  : 'text-gray-400 hover:text-gray-200 hover:bg-gray-800'
              }`}
            >
              {secondarySessionName}
            </button>
          </div>
          <div className="flex items-center gap-1 shrink-0">
            {onToggleOrientation && (
              <SplitOrientationButton
                orientation={orientation}
                onToggle={onToggleOrientation}
              />
            )}
            <button
              type="button"
              onClick={onCloseSecondary}
              title="關閉副聊天室"
              className="p-1 rounded-md text-gray-400 hover:text-red-400 hover:bg-gray-800 text-xs"
            >
              ✕ 關閉分割
            </button>
          </div>
        </div>
        <div className="flex-1 min-w-0 min-h-0">
          {effectiveMobilePane === 'secondary' ? secondaryContent : primaryContent}
        </div>
      </div>
    );
  }

  const isVertical = orientation === 'vertical';
  const firstDim = `calc(${splitRatio * 100}% - 3px)`;
  const secondDim = `calc(${(1 - splitRatio) * 100}% - 3px)`;

  const primaryStyle = isVertical
    ? { height: firstDim, minHeight: `${SPLIT_MIN_PANE_HEIGHT_PX}px`, width: '100%' }
    : { width: firstDim, minWidth: `${SPLIT_MIN_PANE_WIDTH_PX}px`, height: '100%' };

  const secondaryStyle = isVertical
    ? { height: secondDim, minHeight: `${SPLIT_MIN_PANE_HEIGHT_PX}px`, width: '100%' }
    : { width: secondDim, minWidth: `${SPLIT_MIN_PANE_WIDTH_PX}px`, height: '100%' };

  return (
    <div
      ref={containerRef}
      className={`h-full w-full min-w-0 min-h-0 flex ${
        isVertical ? 'flex-col' : 'flex-row'
      } overflow-hidden relative bg-ra-bg`}
    >
      {/* 第一面板：主面板（左右時在左，上下時在上） */}
      <div
        style={primaryStyle}
        onClickCapture={() => onFocusPane?.('primary')}
        className={`flex flex-col min-w-0 min-h-0 relative transition-all duration-75 ${
          activePane === 'primary' ? '' : 'opacity-95'
        }`}
      >
        {primaryContent}
        {/* 外框用覆蓋層畫在內容之上，inset ring 會被子元素背景蓋住 */}
        {activePane === 'primary' && <div aria-hidden="true" className="absolute inset-0 z-20 pointer-events-none ring-2 ring-inset ring-violet-400/70" />}
      </div>

      {/* 可拖曳分隔線 */}
      <SplitDivider
        containerRef={containerRef}
        ratio={splitRatio}
        onRatioChange={onSplitRatioChange}
        onReset={handleResetRatio}
        orientation={orientation}
      />

      {/* 第二面板：副面板（左右時在右，上下時在下） */}
      <div
        style={secondaryStyle}
        onClickCapture={() => onFocusPane?.('secondary')}
        className={`flex flex-col min-w-0 min-h-0 relative transition-all duration-75 ${
          activePane === 'secondary' ? '' : 'opacity-95'
        }`}
      >
        {secondaryContent}
        {/* 外框用覆蓋層畫在內容之上，inset ring 會被子元素背景蓋住 */}
        {activePane === 'secondary' && <div aria-hidden="true" className="absolute inset-0 z-20 pointer-events-none ring-2 ring-inset ring-violet-400/70" />}
      </div>
    </div>
  );
}

/** 分割按鈕（開啟／關閉分割視窗） */
function SplitViewButton({ isSplit, onClick, disabled = false }) {
  return (
    <button
      type="button"
      onClick={onClick}
      disabled={disabled}
      title={isSplit ? '關閉分割視窗 (Ctrl+\\)' : '開啟分割視窗 (Ctrl+\\)'}
      aria-label={isSplit ? '關閉分割視窗' : '開啟分割視窗'}
      className={`shrink-0 inline-flex items-center justify-center w-8 h-8 rounded-md transition-colors disabled:opacity-40 ${
        isSplit
          ? 'bg-violet-600/25 text-violet-300 hover:bg-violet-600/35'
          : 'text-ra-t3 hover:text-violet-300 hover:bg-ra-card'
      }`}
    >
      <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden>
        <rect x="3" y="3" width="18" height="18" rx="2" ry="2" />
        <line x1="12" y1="3" x2="12" y2="21" />
      </svg>
    </button>
  );
}

/** 分割方向切換按鈕（切換左右並排／上下分割） - 實心雙板塊造型 */
function SplitOrientationButton({ orientation = 'horizontal', onClick, onToggle, disabled = false }) {
  const isVertical = orientation === 'vertical';
  const handleClick = onClick || onToggle;
  return (
    <button
      type="button"
      onClick={handleClick}
      disabled={disabled}
      title={isVertical ? '切換為左右並排 (Ctrl+Alt+O)' : '切換為上下分割 (Ctrl+Alt+O)'}
      aria-label={isVertical ? '切換為左右並排' : '切換為上下分割'}
      className="shrink-0 inline-flex items-center justify-center w-8 h-8 rounded-md transition-colors text-ra-t3 hover:text-violet-300 hover:bg-ra-card disabled:opacity-40"
    >
      {isVertical ? (
        // 當前是上下分割，顯示目標「切換到左右並排」：兩條獨立直向膠囊 [||]
        <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden>
          <rect x="4" y="3.5" width="6.5" height="17" rx="1.5" />
          <rect x="13.5" y="3.5" width="6.5" height="17" rx="1.5" />
        </svg>
      ) : (
        // 當前是左右並排，顯示目標「切換到上下分割」：兩條獨立橫向膠囊 [=]
        <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden>
          <rect x="3.5" y="4" width="17" height="6.5" rx="1.5" />
          <rect x="3.5" y="13.5" width="17" height="6.5" rx="1.5" />
        </svg>
      )}
    </button>
  );
}
