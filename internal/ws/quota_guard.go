package ws

import (
	"fmt"

	"github.com/jerry12122/Claude-Code-Mini-App/internal/quota"
)

// HandlerOption 是 NewHandler 的選用設定，避免為單一旗標改動所有呼叫端的簽名。
type HandlerOption func(*handlerOptions)

type handlerOptions struct{ quotaGuardPercent float64 }

// WithQuotaGuardPercent 設定佇列自動啟動前的額度守門門檻（已用百分比）；0 或負數＝停用。
func WithQuotaGuardPercent(p float64) HandlerOption {
	return func(o *handlerOptions) { o.quotaGuardPercent = p }
}

// quotaGuardFor 以 quota 快取（不觸發刷新）建立指定 provider 的守門；沒有 quota 服務時回 nil。
func quotaGuardFor(svc *quota.Service, provider string, percent float64) func() string {
	if svc == nil {
		return nil
	}
	return newQuotaGuard(func() quota.Snapshot { return svc.Get(provider) }, percent)
}

// newQuotaGuard 建立佇列自動啟動前的額度守門；percent <= 0 代表停用（回 nil）。
// 回傳函式給出非空字串＝該 provider 已有視窗用量達門檻，原因文字供使用者閱讀。
// 快取 stale、沒資料、視窗沒有 percent 一律放行（fail-open）：伺服器無法詢問使用者，擋下比放行更糟。
func newQuotaGuard(get func() quota.Snapshot, percent float64) func() string {
	if percent <= 0 || get == nil {
		return nil
	}
	return func() string {
		snap := get()
		if snap.Stale {
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
}
