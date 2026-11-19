package ws

import (
	"context"
	"fmt"

	"github.com/jerry12122/Claude-Code-Mini-App/internal/quota"
)

// HandlerOption 是 NewHandler 的選用設定，避免為單一旗標改動所有呼叫端的簽名。
type HandlerOption func(*handlerOptions)

type handlerOptions struct {
	quotaGuardPercent  float64
	quotaGuardSettings func() (enabled bool, percent float64)
	roomBridge         func(sessionID string) (url, token string)
	quotaResumeRefresh func(context.Context, string) (quota.Snapshot, error)
}

func WithRoomBridge(bridge func(string) (string, string)) HandlerOption {
	return func(o *handlerOptions) { o.roomBridge = bridge }
}

// WithQuotaGuardPercent 設定佇列自動啟動前的額度守門門檻（已用百分比）；0 或負數＝停用。
// 沒有提供 WithQuotaGuardSettings 時，連線使用這個固定值。
func WithQuotaGuardPercent(p float64) HandlerOption {
	return func(o *handlerOptions) { o.quotaGuardPercent = p }
}

// WithQuotaGuardSettings 讓守門在每次自動啟動前讀取當時的有效設定。
func WithQuotaGuardSettings(get func() (enabled bool, percent float64)) HandlerOption {
	return func(o *handlerOptions) { o.quotaGuardSettings = get }
}

// quotaGuardFor 以 quota 快取（不觸發刷新）建立指定 provider 的守門；沒有 quota 服務時回 nil。
// settings 非 nil 時，閉包每次呼叫都讀設定；停用時回空字串，不把連線建立當下的百分比固定下來。
func quotaGuardFor(svc *quota.Service, provider string, percent float64, settings func() (enabled bool, percent float64)) func() string {
	if svc == nil {
		return nil
	}
	get := func() quota.Snapshot { return svc.Get(provider) }
	if settings != nil {
		return quotaGuardFromSettings(get, settings)
	}
	return newQuotaGuard(get, percent)
}

// newQuotaGuard 建立佇列自動啟動前的額度守門；percent <= 0 代表停用（回 nil）。
// 回傳函式給出非空字串＝該 provider 已有視窗用量達門檻，原因文字供使用者閱讀。
// 快取 stale、沒資料、視窗沒有 percent 一律放行（fail-open）：伺服器無法詢問使用者，擋下比放行更糟。
func newQuotaGuard(get func() quota.Snapshot, percent float64) func() string {
	if percent <= 0 || get == nil {
		return nil
	}
	return func() string { return quotaGuardReason(get(), percent) }
}

// quotaGuardFromSettings 每次呼叫都讀 enabled／percent。有 quota 服務時閉包本身不為 nil，停用時回空字串。
func quotaGuardFromSettings(get func() quota.Snapshot, settings func() (enabled bool, percent float64)) func() string {
	if get == nil || settings == nil {
		return nil
	}
	return func() string {
		enabled, percent := settings()
		if !enabled {
			return ""
		}
		return quotaGuardReason(get(), percent)
	}
}

func quotaGuardReason(snap quota.Snapshot, percent float64) string {
	if snap.Stale || percent <= 0 {
		return ""
	}
	for _, w := range snap.Windows {
		if w.Percent == nil || *w.Percent < percent {
			continue
		}
		name := w.Label
		if name == "" {
			name = w.Key
		}
		return fmt.Sprintf("額度守門：%s 已用 %.0f%%，達到門檻 %.0f%%，佇列已暫停（按「繼續」可解除）", name, *w.Percent, percent)
	}
	return ""
}
