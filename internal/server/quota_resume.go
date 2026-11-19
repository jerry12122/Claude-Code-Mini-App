package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"strings"
	"time"

	"github.com/jerry12122/Claude-Code-Mini-App/internal/db"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/quota"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/usage"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/ws"
)

var quotaResumeEvery = 30 * time.Second // var：只有 e2e 測試會縮短，預設值由 TestQuotaResumeDefaultInterval 固定

// recoverSessionsAfterStartup 是正式啟動在開啟資料庫之後的復原步驟。
// 先把中斷的執行收回 idle，再處理自動復原狀態。任一失敗都要往上傳，不能當成啟動成功。
func recoverSessionsAfterStartup(database *db.DB, now time.Time) error {
	if err := database.ResetRunningSessions(); err != nil {
		return fmt.Errorf("重啟任務狀態恢復失敗: %w", err)
	}
	if err := database.RecoverQuotaResume(now); err != nil {
		return fmt.Errorf("自動復原狀態恢復失敗: %w", err)
	}
	return nil
}

// errQuotaResumeNotHeld 表示撥號前開關已關，或狀態已不再是 resuming。呼叫端不得把它當成投遞失敗而退回 waiting。
var errQuotaResumeNotHeld = errors.New("quota resume not held")

func (r roomRuntime) watchQuota(ctx context.Context) {
	ticker := time.NewTicker(quotaResumeEvery)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.resumeDue(ctx, time.Now().UTC(), r.quotaRefresh, r.wakeQuota)
		}
	}
}

// resumeDue 處理這一輪已到期的 waiting。開關關閉時不刷新、不啟動。
func (r roomRuntime) resumeDue(ctx context.Context, now time.Time, refresh func(context.Context, string) (quota.Snapshot, error), wake func(context.Context, string) (bool, error)) {
	if ctx.Err() != nil || refresh == nil {
		return
	}
	on, err := r.db.AutoResumeOnQuota()
	if err != nil {
		slog.Error("[quota-resume] settings", "err", err)
		return
	}
	if !on {
		return
	}
	due, err := r.db.ListDueQuotaResume(now)
	if err != nil {
		slog.Error("[quota-resume] list due", "err", err)
		return
	}
	for _, item := range due {
		if ctx.Err() != nil {
			return
		}
		r.resumeOne(ctx, now, item, refresh, wake)
	}
}

func (r roomRuntime) resumeOne(ctx context.Context, now time.Time, item db.DueQuotaResume, refresh func(context.Context, string) (quota.Snapshot, error), wake func(context.Context, string) (bool, error)) {
	sessionID := item.SessionID
	sess, err := r.db.GetSession(sessionID)
	if err != nil {
		slog.Warn("[quota-resume] session", "session", sessionID, "err", err)
		return
	}
	snap, err := refresh(ctx, sess.AgentType)
	if err != nil {
		slog.Warn("[quota-resume] refresh", "session", sessionID, "err", err)
		return
	}
	on, err := r.db.AutoResumeOnQuota()
	if err != nil || !on {
		return
	}
	switch kind, at := quotaResumePlan(snap, now); kind {
	case "delay":
		ok, err := r.db.DelayQuotaResume(sessionID, item.Generation, item.At, at)
		if err != nil {
			slog.Error("[quota-resume] delay", "session", sessionID, "err", err)
			return
		}
		if !ok {
			return
		}
		if err := ws.BroadcastSessionQueue(r.db, sessionID); err != nil {
			slog.Error("[quota-resume] delay broadcast", "session", sessionID, "err", err)
		}
	case "start":
		generation, ok, err := r.db.StartQuotaResume(sessionID, item.Generation, item.At)
		if err != nil {
			slog.Error("[quota-resume] start", "session", sessionID, "err", err)
			return
		}
		if !ok {
			return
		}
		r.deliverQuotaResume(ctx, sessionID, generation, wake)
	}
}

type quotaResumeGenerationKey struct{}

// deliverQuotaResume 在 waiting→resuming 搶到之後、真正撥號之前再確認開關與同一個世代。
// started 為 false 且佇列是空的才回到 none；投遞失敗退回 waiting 並還原 attempts。
// 回退與清空都帶著這次搶到的世代，取消後新的一輪不會被舊投遞改掉。
func (r roomRuntime) deliverQuotaResume(ctx context.Context, sessionID string, generation int64, wake func(context.Context, string) (bool, error)) {
	if !r.quotaResumeHeldGeneration(sessionID, generation) {
		r.releaseResuming(sessionID, generation)
		return
	}
	if wake == nil {
		r.restoreWaiting(sessionID, generation)
		return
	}
	started, err := wake(context.WithValue(ctx, quotaResumeGenerationKey{}, generation), sessionID)
	if errors.Is(err, errQuotaResumeNotHeld) {
		r.releaseResuming(sessionID, generation)
		return
	}
	if err != nil {
		slog.Warn("[quota-resume] wake", "session", sessionID, "err", err)
		on, onErr := r.db.AutoResumeOnQuota()
		if onErr == nil && !on {
			r.releaseResuming(sessionID, generation)
			return
		}
		r.restoreWaiting(sessionID, generation)
		return
	}
	if started {
		return
	}
	on, err := r.db.AutoResumeOnQuota()
	if err != nil {
		slog.Error("[quota-resume] settings", "session", sessionID, "err", err)
		return
	}
	if !on {
		r.releaseResuming(sessionID, generation)
		return
	}
	n, err := r.db.CountQueuedMessages(sessionID)
	if err != nil {
		slog.Error("[quota-resume] queue count", "session", sessionID, "err", err)
		return
	}
	if !r.quotaResumeHeldGeneration(sessionID, generation) {
		return
	}
	if n == 0 {
		r.releaseResuming(sessionID, generation)
		return
	}
	r.restoreWaiting(sessionID, generation)
}

func (r roomRuntime) quotaResumeHeldGeneration(sessionID string, generation int64) bool {
	if generation <= 0 {
		return false
	}
	on, err := r.db.AutoResumeOnQuota()
	if err != nil || !on {
		return false
	}
	owned, current, err := r.db.ClaimQuotaResume(sessionID)
	return err == nil && owned && current == generation
}

func (r roomRuntime) releaseResuming(sessionID string, generation int64) {
	if _, err := r.db.ReleaseQuotaResumeGeneration(sessionID, generation); err != nil {
		slog.Error("[quota-resume] release", "session", sessionID, "err", err)
	}
}

func (r roomRuntime) restoreWaiting(sessionID string, generation int64) {
	if _, err := r.db.RestoreQuotaResumeWaiting(sessionID, generation); err != nil {
		slog.Error("[quota-resume] restore", "session", sessionID, "err", err)
	}
}

// quotaResumePlan 只看 session 窗口。keep＝維持 waiting 與原時間；delay＝改到 reset 後 60 秒；start＝可以啟動。
func quotaResumePlan(snap quota.Snapshot, now time.Time) (kind, at string) {
	if snap.Stale || strings.TrimSpace(snap.Error) != "" {
		return "keep", ""
	}
	type sample struct {
		percent float64
		reset   time.Time
		has     bool
	}
	var samples []sample
	for _, w := range snap.Windows {
		if w.Key != "session" || w.Percent == nil {
			continue
		}
		p := *w.Percent
		if math.IsNaN(p) || math.IsInf(p, 0) {
			continue
		}
		item := sample{percent: p}
		if reset, ok := usage.ParseResetsAt(w.ResetsAt, now); ok {
			item.reset = reset
			item.has = true
		}
		samples = append(samples, item)
	}
	if len(samples) == 0 {
		return "keep", ""
	}
	for _, item := range samples {
		if item.percent >= 100 {
			if !item.has {
				return "keep", ""
			}
			return "delay", db.QuotaResumeAt(item.reset)
		}
	}
	return "start", ""
}

func (r roomRuntime) wakeQuota(ctx context.Context, sessionID string) (bool, error) {
	generation, _ := ctx.Value(quotaResumeGenerationKey{}).(int64)
	if !r.quotaResumeHeldGeneration(sessionID, generation) {
		return false, errQuotaResumeNotHeld
	}
	payload, err := r.wakeSession(ctx, sessionID, 5*time.Second, map[string]any{"type": "quota_resume", "generation": generation}, "quota_resumed")
	if err != nil {
		return false, err
	}
	var event struct {
		Started *bool `json:"started"`
	}
	if json.Unmarshal(payload, &event) != nil || event.Started == nil {
		return false, errors.New("quota_resumed 缺少 started")
	}
	return *event.Started, nil
}
