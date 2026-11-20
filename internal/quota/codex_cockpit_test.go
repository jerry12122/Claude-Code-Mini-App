package quota

import (
	"context"
	"database/sql"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func cockpitHistoryFixture(t *testing.T, dir, setup string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "codex-quota-history-v1.sqlite")
	uri := url.URL{Scheme: "file", Path: "/" + strings.TrimPrefix(filepath.ToSlash(path), "/")}
	db, err := sql.Open("sqlite", uri.String())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(setup); err != nil {
		t.Fatal(err)
	}
	return path
}

const cockpitSchema = `CREATE TABLE quota_samples(account_id TEXT,identity TEXT,minute INTEGER,observed_at INTEGER,short_remaining INTEGER,weekly_remaining INTEGER);`

func TestCodexFetcherCockpitFallback(t *testing.T) {
	fakeCodexOnPath(t)
	for _, tc := range []struct {
		mode           string
		missing, stale bool
	}{
		{"success", true, false}, {"auth-error", false, true}, {"auth-error", true, false},
		{"rpc-error", false, false}, {"auth-unrelated", false, false}, {"auth-initialize", false, false}, {"timeout", false, false}, {"cancelled", false, false},
	} {
		t.Run(tc.mode+map[bool]string{true: "/missing", false: "/history"}[tc.missing], func(t *testing.T) {
			dir := t.TempDir()
			if !tc.missing {
				cockpitHistoryFixture(t, dir, cockpitSchema+`INSERT INTO quota_samples VALUES('fixture','unused',1,100,72,96);`)
			}
			t.Setenv("CMA_COCKPIT_DIR", dir)
			t.Setenv("QUOTA_TEST_CODEX_PROCESS", tc.mode)
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			if tc.mode == "timeout" {
				ctx, cancel = context.WithTimeout(t.Context(), 150*time.Millisecond)
				defer cancel()
			}
			if tc.mode == "cancelled" {
				cancel()
			}
			snap, err := (&CodexFetcher{}).Fetch(ctx)
			if tc.mode == "success" {
				if err != nil || snap.Stale || snap.UpdatedAt.IsZero() || snap.DisplayText != "5h 2% · Week 13%" {
					t.Fatalf("app-server 應優先：%+v %v", snap, err)
				}
				return
			}
			if tc.stale {
				if err != nil || !snap.Stale || !snap.UpdatedAt.IsZero() || !strings.Contains(snap.DisplayText, "歷史估計（非即時）") {
					t.Fatalf("fallback 偽裝 fresh：%+v %v", snap, err)
				}
				checkWindows(t, snap.Windows, []wantWindow{{"session", 28}, {"weekly", 4}})
				for _, w := range snap.Windows {
					if w.ResetsAt != "" {
						t.Fatal("歷史不能捏造 reset")
					}
				}
				service := NewService()
				service.UseFetch(func(context.Context, string) (Snapshot, error) { return snap, nil })
				refreshed, err := service.RefreshManual(t.Context(), "codex")
				if err != nil {
					t.Fatal(err)
				}
				for _, cached := range []Snapshot{refreshed, service.Get("codex"), service.GetAll()["codex"]} {
					if !cached.Stale || !cached.UpdatedAt.IsZero() || cached.ToPayload().UpdatedAt != "" {
						t.Fatalf("service 丟失歷史狀態：%+v", cached)
					}
				}
				return
			}
			if err == nil || len(snap.Windows) != 0 {
				t.Fatalf("不該 fallback：%+v %v", snap, err)
			}
			if tc.mode == "timeout" && !errors.Is(err, context.DeadlineExceeded) {
				t.Fatal(err)
			}
			if tc.mode == "cancelled" && !errors.Is(err, context.Canceled) {
				t.Fatal(err)
			}
			if tc.mode == "auth-error" {
				var rpc *codexQuotaRPCError
				if !errors.As(err, &rpc) || !strings.Contains(err.Error(), "chatgpt authentication required") || !strings.Contains(err.Error(), "cockpit fallback") {
					t.Fatalf("fallback 失敗遺失原 RPC：%v", err)
				}
			}
			if tc.mode != "auth-error" && strings.Contains(err.Error(), "cockpit fallback") {
				t.Fatalf("非指定 auth 錯誤被 fallback 掩蓋：%v", err)
			}
		})
	}
}

func TestCockpitHistoryInvalidAndLatestSamples(t *testing.T) {
	for _, tc := range []struct {
		name, setup string
		want        string
	}{
		{"latest null does not revive old", cockpitSchema + `INSERT INTO quota_samples VALUES('a','',1,100,100,100),('a','',2,200,NULL,90),('b','',1,100,30,90);`, "5h 70% · Week 10%"},
		{"all latest null", cockpitSchema + `INSERT INTO quota_samples VALUES('a','',1,100,100,100),('a','',2,200,NULL,NULL);`, ""},
		{"both windows limit best", cockpitSchema + `INSERT INTO quota_samples VALUES('a','',1,100,99,5),('b','',1,100,50,60),('c','',1,100,60,40);`, "5h 50% · Week 40%"},
		{"zero is exhausted", cockpitSchema + `INSERT INTO quota_samples VALUES('a','',1,100,0,0);`, "5h 100% · Week 100%"},
		{"over 100", cockpitSchema + `INSERT INTO quota_samples VALUES('a','',1,100,101,90);`, ""},
		{"negative", cockpitSchema + `INSERT INTO quota_samples VALUES('a','',1,100,90,-1);`, ""},
		{"fractional", cockpitSchema + `INSERT INTO quota_samples VALUES('a','',1,100,50.5,90);`, ""},
		{"wrong type", cockpitSchema + `INSERT INTO quota_samples VALUES('a','',1,100,'unknown',90);`, ""},
		{"wrong timestamp", cockpitSchema + `INSERT INTO quota_samples VALUES('a','',1,'unknown',90,90);`, ""},
		{"empty", cockpitSchema, ""}, {"missing table", `CREATE TABLE other(value INTEGER);`, ""},
		{"bad schema", `CREATE TABLE quota_samples(account_id TEXT,observed_at INTEGER);`, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := cockpitHistoryFixture(t, dir, tc.setup)
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			info, err := cockpitCodexQuota(t.Context(), dir)
			if tc.want == "" {
				if err == nil || info != nil {
					t.Fatalf("無效資料冒充額度：%+v %v", info, err)
				}
			} else if err != nil || FormatDisplay("codex", info) != tc.want {
				t.Fatalf("info=%+v err=%v want=%s", info, err, tc.want)
			}
			after, err := os.ReadFile(path)
			if err != nil || string(before) != string(after) {
				t.Fatal("唯讀查詢修改 DB", err)
			}
		})
	}
}

func TestCockpitHistoryReadOnlySpecialPaths(t *testing.T) {
	name := "space # percent% 中文"
	if runtime.GOOS != "windows" {
		name += "?query"
	}
	dir := filepath.Join(t.TempDir(), name)
	path := cockpitHistoryFixture(t, dir, cockpitSchema+`INSERT INTO quota_samples VALUES('fixture','',1,100,72,96);`)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	info, err := cockpitCodexQuota(t.Context(), dir)
	if err != nil || FormatDisplay("codex", info) != "5h 28% · Week 4%" {
		t.Fatalf("特殊路徑開錯 DB：%v %v", info, err)
	}
	after, err := os.ReadFile(path)
	if err != nil || string(before) != string(after) {
		t.Fatal("特殊路徑 DB 被修改")
	}
	missing := filepath.Join(t.TempDir(), "missing #.sqlite")
	if _, err := cockpitCodexQuota(t.Context(), missing); err == nil {
		t.Fatal("缺 DB 可讀")
	}
	if _, err := os.Stat(missing); !os.IsNotExist(err) {
		t.Fatal("缺 DB 被建立", err)
	}
	if _, err := cockpitCodexQuota(t.Context(), ""); err == nil {
		t.Fatal("未知目錄不能讀 cwd")
	}
	if runtime.GOOS == "windows" {
		if _, err := cockpitCodexQuota(t.Context(), dir+"?mode=rw"); err == nil {
			t.Fatal("Windows 非法 ? 路徑不可誤讀原檔")
		}
	}
	badDir := t.TempDir()
	badPath := filepath.Join(badDir, "codex-quota-history-v1.sqlite")
	if err := os.WriteFile(badPath, []byte("not a sqlite database"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := cockpitCodexQuota(t.Context(), badDir); err == nil {
		t.Fatal("損壞 DB 不可冒充額度")
	}
	data, err := os.ReadFile(badPath)
	if err != nil || string(data) != "not a sqlite database" {
		t.Fatal("損壞 DB 被修改")
	}
	emptyDir := t.TempDir()
	if _, err := cockpitCodexQuota(t.Context(), emptyDir); err == nil {
		t.Fatal("缺檔不可讀")
	}
	if _, err := os.Stat(filepath.Join(emptyDir, "codex-quota-history-v1.sqlite")); !os.IsNotExist(err) {
		t.Fatal("唯讀查詢建立缺檔", err)
	}
}

func TestQuotaServicePreservesUpstreamFreshness(t *testing.T) {
	known := time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC)
	for _, tc := range []struct {
		name                                 string
		upstreamStale, oldCache, unknownTime bool
		wantStale                            bool
	}{
		{"fresh", false, false, false, false}, {"aged cache", false, true, false, true},
		{"upstream stale", true, false, false, true}, {"unknown historical time", true, false, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := NewService()
			updated := known
			if tc.unknownTime {
				updated = time.Time{}
			}
			s.Store("claude", Snapshot{DisplayText: "cached", Stale: tc.upstreamStale, UpdatedAt: updated})
			if tc.oldCache {
				s.cache["claude"].fetchedAt = time.Now().Add(-3 * s.ttl("claude"))
			}
			got := s.Get("claude")
			if got.Stale != tc.wantStale || !got.UpdatedAt.Equal(updated) {
				t.Fatalf("fresh/cache 語意改變：%+v", got)
			}
		})
	}
	s := NewService()
	s.Store("cursor", Snapshot{DisplayText: "legacy cache"})
	if got := s.Get("cursor"); got.Stale || got.UpdatedAt.IsZero() {
		t.Fatalf("一般 cache 的缺時間 fallback 被破壞：%+v", got)
	}
}

func TestCockpitHistoryStaleSurvivesService(t *testing.T) {
	s := NewService()
	s.UseFetch(func(context.Context, string) (Snapshot, error) {
		return Snapshot{Provider: "codex", DisplayText: "歷史估計 · 5h 28% · Week 4%", Stale: true}, nil
	})
	refreshed, err := s.RefreshManual(t.Context(), "codex")
	if err != nil {
		t.Fatal(err)
	}
	for _, snap := range []Snapshot{refreshed, s.Get("codex")} {
		if !snap.Stale || !snap.UpdatedAt.IsZero() {
			t.Fatalf("歷史資料被包裝為即時：%+v", snap)
		}
	}
}

func TestCockpitCodexQuotaPicksBestAccount(t *testing.T) {
	dir := t.TempDir()
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(filepath.Join(dir, "codex-quota-history-v1.sqlite")))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE quota_samples (account_id TEXT, identity TEXT, minute INTEGER, observed_at INTEGER, short_remaining INTEGER, weekly_remaining INTEGER)`); err != nil {
		t.Fatal(err)
	}
	for _, row := range []string{
		// a 舊樣本 5h 剩 100，最新是 0：只看最新。b 週剩 8。c 兩個都夠。
		`('a','i',1,100,100,50)`, `('a','i',2,200,0,53)`,
		`('b','i',1,100,100,8)`,
		`('c','i',1,100,72,96)`,
		`('d','i',1,100,NULL,NULL)`,
	} {
		if _, err := db.Exec(`INSERT INTO quota_samples VALUES ` + row); err != nil {
			t.Fatal(err)
		}
	}
	db.Close()
	info, err := cockpitCodexQuota(t.Context(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := FormatDisplay("codex", info); got != "5h 28% · Week 4%" {
		t.Fatalf("got %q", got)
	}
	if _, err := cockpitCodexQuota(t.Context(), t.TempDir()); err == nil {
		t.Fatal("missing history file must fail")
	}
}
