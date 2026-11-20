package quota

import (
	"context"
	"database/sql"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"

	"github.com/jerry12122/Claude-Code-Mini-App/internal/usage"
)

// Cockpit Tools 把 Codex CLI 改成 API key 指向本機反代後，auth.json 沒有 ChatGPT 登入，
// app-server 的 account/rateLimits/read 會失敗，反代也沒有額度端點。
// ponytail: 改讀 Cockpit 的額度歷史（它的私有檔案，格式變動時這裡會失敗並保留原錯誤）。
// 取「5h 與週剩餘較小者」最大的歷史樣本，只是估計，不保證帳號目前可用，也不替 Cockpit 切帳號。
// 歷史沒有重設時間，也只在數值變動時記錄，所以不填 ResetsAt。

func cockpitDir() string {
	if dir := os.Getenv("CMA_COCKPIT_DIR"); dir != "" {
		return dir
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".cockpit_tools")
}

func cockpitCodexQuota(ctx context.Context, dir string) (*usage.QuotaInfo, error) {
	if strings.TrimSpace(dir) == "" {
		return nil, errors.New("cockpit history directory is unknown")
	}
	path, err := filepath.Abs(filepath.Join(dir, "codex-quota-history-v1.sqlite"))
	if err != nil {
		return nil, err
	}
	if stat, err := os.Stat(path); err != nil {
		return nil, err
	} else if !stat.Mode().IsRegular() {
		return nil, errors.New("cockpit history is not a regular file")
	}
	uri := url.URL{Scheme: "file", Path: "/" + strings.TrimPrefix(filepath.ToSlash(path), "/"), RawQuery: "mode=ro&_pragma=busy_timeout(2000)"}
	db, err := sql.Open("sqlite", uri.String())
	if err != nil {
		return nil, err
	}
	defer db.Close()
	rows, err := db.QueryContext(ctx, `SELECT short_remaining, weekly_remaining,typeof(short_remaining),typeof(weekly_remaining),typeof(account_id),typeof(observed_at)
		FROM quota_samples s WHERE account_id IS NOT NULL AND trim(account_id)!=''
		AND observed_at = (SELECT MAX(observed_at) FROM quota_samples WHERE account_id = s.account_id)`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	bestShort, bestWeekly, best := 0, 0, -1
	for rows.Next() {
		var short, weekly sql.NullInt64
		var shortType, weeklyType, accountType, timeType string
		if err := rows.Scan(&short, &weekly, &shortType, &weeklyType, &accountType, &timeType); err != nil {
			return nil, err
		}
		if !short.Valid || !weekly.Valid {
			continue
		}
		if shortType != "integer" || weeklyType != "integer" || accountType != "text" || timeType != "integer" || short.Int64 < 0 || short.Int64 > 100 || weekly.Int64 < 0 || weekly.Int64 > 100 {
			return nil, errors.New("cockpit quota history has an invalid sample")
		}
		if score := min(int(short.Int64), int(weekly.Int64)); score > best {
			bestShort, bestWeekly, best = int(short.Int64), int(weekly.Int64), score
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if best < 0 {
		return nil, errors.New("cockpit quota history has no samples")
	}
	used := func(remaining int) *float64 {
		v := float64(100 - remaining)
		return &v
	}
	return &usage.QuotaInfo{
		Provider: "codex",
		Source:   "cockpit quota history (best historical estimate; freshness unknown)",
		Windows: []usage.QuotaWindow{
			{Kind: "session", Label: "300 min", Percent: used(bestShort)},
			{Kind: "weekly", Label: "10080 min", Percent: used(bestWeekly)},
		},
	}, nil
}
