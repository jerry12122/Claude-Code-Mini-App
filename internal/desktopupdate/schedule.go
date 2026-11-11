package desktopupdate

import (
	"context"
	"time"
)

// CheckInterval 是背景檢查間隔（產品常數）。啟動時另外檢查一次。
const CheckInterval = time.Hour

// StartSchedule 啟動背景檢查：啟動時一次，之後每 interval 一次，且只呼叫 Check。
// 背景檢查不下載、不開啟設定頁；錯誤只寫入 Status 供查詢。
// after 注入計時器（正式環境傳入 time.After），讓測試以假時鐘驗證排程。
// ctx 取消後迴圈結束，回傳的 channel 會被關閉。
func StartSchedule(ctx context.Context, m *Manager, interval time.Duration, after func(time.Duration) <-chan time.Time) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		backgroundCheck(ctx, m)
		for {
			select {
			case <-ctx.Done():
				return
			case <-after(interval):
				backgroundCheck(ctx, m)
			}
		}
	}()
	return done
}

// backgroundCheck 在下載進行中、驗證中或已 ready 時跳過，避免覆寫使用者正在進行的操作。
// Check 本身也會以 ErrBusy 拒絕這些情況；這裡的狀態判斷只是避免不必要的網路請求。
func backgroundCheck(ctx context.Context, m *Manager) {
	if ctx.Err() != nil {
		return
	}
	switch m.Status().State {
	case StateDownloading, StateVerifying, StateReady:
		return
	}
	_ = m.Check(ctx) // 錯誤已寫入 Status（ErrorCode），背景不彈窗
}
