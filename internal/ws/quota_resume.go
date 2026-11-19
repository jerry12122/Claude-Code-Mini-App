package ws

import (
	"context"
	"math"
	"time"

	"github.com/jerry12122/Claude-Code-Mini-App/internal/agent"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/db"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/quota"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/usage"
)

// WithQuotaResumeRefresh 注入失敗收尾用的額度刷新。沒有注入時改用 quota 服務的 RefreshAfterRun。
func WithQuotaResumeRefresh(refresh func(context.Context, string) (quota.Snapshot, error)) HandlerOption {
	return func(o *handlerOptions) { o.quotaResumeRefresh = refresh }
}

// finishHeldQuotaResume 與 finishQuotaResume 相同。
// held 大於 0 時是這次持有的 resuming，武裝必須對上該世代。
// held 為 0 時是一般執行，observed 是執行開始時的世代；取消或新訊息會推進世代，對不上就不武裝。
// 不符合候選、快照不能用、或轉移沒搶到時不武裝。資料寫入失敗回傳錯誤，呼叫端不得把它當成已武裝。
func finishHeldQuotaResume(database *db.DB, sessionID, agentType string, userMsgID int64, runFailed, cancelled, permDenied bool, refresh func(context.Context, string) (quota.Snapshot, error), now time.Time, held, observed int64) error {
	if userMsgID <= 0 || !runFailed || cancelled || permDenied || agentType == agent.TypeTelegram {
		return nil
	}
	on, err := database.AutoResumeOnQuota()
	if err != nil {
		return err
	}
	if !on || refresh == nil {
		return nil
	}
	snap, err := refresh(context.Background(), agentType)
	if err != nil {
		return nil
	}
	reset, ok := sessionQuotaReset(snap, now)
	if !ok {
		return nil
	}
	at := db.QuotaResumeAt(reset)
	if held > 0 {
		_, _, attempts, err := database.GetQuotaResume(sessionID)
		if err != nil {
			return err
		}
		to := db.QuotaResumeWaiting
		if attempts >= 3 {
			to = db.QuotaResumeExhausted
		}
		_, err = database.ArmQuotaResume(sessionID, userMsgID, db.QuotaResumeResuming, to, at, held)
		return err
	}
	_, err = database.ArmQuotaResume(sessionID, userMsgID, db.QuotaResumeNone, db.QuotaResumeWaiting, at, observed)
	return err
}

// releaseQuotaResumeRun 只把這一次仍持有的 resuming 轉回 none，次數歸零、時間清空。
// 世代比對與轉移是同一道 UPDATE。同一回合已改成 waiting／exhausted、或世代已換，都不會清掉新的一輪。
func releaseQuotaResumeRun(database *db.DB, sessionID string, owned bool, generation int64) error {
	if !owned {
		return nil
	}
	_, err := database.ReleaseQuotaResumeGeneration(sessionID, generation)
	return err
}

// quotaResumeGenerationHeld 是內部喚醒可以呼叫 Resume 的條件：開關仍開，且這次仍持有同一個世代的 resuming。
func quotaResumeGenerationHeld(database *db.DB, sessionID string, generation int64) bool {
	if generation <= 0 {
		return false
	}
	if on, err := database.AutoResumeOnQuota(); err != nil || !on {
		return false
	}
	owned, current, err := database.ClaimQuotaResume(sessionID)
	return err == nil && owned && current == generation
}

func sessionQuotaReset(snap quota.Snapshot, now time.Time) (time.Time, bool) {
	if snap.Stale {
		return time.Time{}, false
	}
	for _, w := range snap.Windows {
		if w.Key != "session" || w.Percent == nil {
			continue
		}
		percent := *w.Percent
		if math.IsNaN(percent) || math.IsInf(percent, 0) || percent < 100 {
			continue
		}
		reset, ok := usage.ParseResetsAt(w.ResetsAt, now)
		if !ok {
			continue
		}
		return reset, true
	}
	return time.Time{}, false
}
