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

// finishQuotaResume 在失敗收尾判斷是否為 5 小時額度用盡。符合時把原使用者訊息放回佇列最前面並暫停。
// 這條沒有持有世代，只允許從 none 武裝。
func finishQuotaResume(database *db.DB, sessionID, agentType string, userMsgID int64, runFailed, cancelled, permDenied bool, refresh func(context.Context, string) (quota.Snapshot, error), now time.Time) error {
	return finishHeldQuotaResume(database, sessionID, agentType, userMsgID, runFailed, cancelled, permDenied, refresh, now, 0, 0)
}

// finishHeldQuotaResume 與 finishQuotaResume 相同。
// held 大於 0 時是這次持有的 resuming，武裝必須對上該世代。
// held 為 0 時是一般執行，observed 是執行開始時的世代；取消或新訊息會推進世代，對不上就不武裝。
// 不符合候選、快照不能用、或轉移沒搶到時不武裝。資料寫入失敗回傳錯誤，呼叫端不得把它當成已武裝。
func finishHeldQuotaResume(database *db.DB, sessionID, agentType string, userMsgID int64, runFailed, cancelled, permDenied bool, refresh func(context.Context, string) (quota.Snapshot, error), now time.Time, held, observed int64) error {
	if userMsgID <= 0 || !runFailed || cancelled || permDenied || agentType == agent.TypeTelegram {
		return nil
	}
	settings, err := database.GetGeneralSettings()
	if err != nil {
		return err
	}
	if !settings.AutoResumeOnQuota || refresh == nil {
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
	at := reset.UTC().Add(time.Minute).Truncate(time.Second).Format(time.RFC3339)
	if held > 0 {
		_, _, attempts, err := database.GetQuotaResume(sessionID)
		if err != nil {
			return err
		}
		to := db.QuotaResumeWaiting
		if attempts >= 3 {
			to = db.QuotaResumeExhausted
		}
		_, err = database.ArmQuotaResumeGeneration(sessionID, userMsgID, db.QuotaResumeResuming, to, at, held)
		return err
	}
	_, err = database.ArmQuotaResumeExact(sessionID, userMsgID, db.QuotaResumeNone, db.QuotaResumeWaiting, at, observed)
	return err
}

// claimQuotaResume 在執行開始時一次讀出這是不是自動重跑，以及當時的世代。
// 世代在 waiting→resuming、取消與關閉開關時增加，不歸零、不重用。
func claimQuotaResume(database *db.DB, sessionID string) (owned bool, generation int64) {
	owned, generation, err := database.ClaimQuotaResume(sessionID)
	if err != nil {
		return false, 0
	}
	return owned, generation
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
	settings, err := database.GetGeneralSettings()
	if err != nil || !settings.AutoResumeOnQuota {
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
