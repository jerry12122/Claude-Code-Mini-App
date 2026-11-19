package db

import "testing"

func TestGeneralSettings_DefaultAndRoundtrip(t *testing.T) {
	database, err := Open(t.TempDir() + "/general_settings.db")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	got, err := database.GetGeneralSettings()
	if err != nil {
		t.Fatal(err)
	}
	if got.VscodeNoAdmin {
		t.Fatal("預設應為 false")
	}

	if _, err := database.PutGeneralSettings([]byte(`{"vscodeNoAdmin":true}`)); err != nil {
		t.Fatal(err)
	}
	got, err = database.GetGeneralSettings()
	if err != nil {
		t.Fatal(err)
	}
	if !got.VscodeNoAdmin {
		t.Fatal("寫入後應為 true")
	}

	if _, err := database.PutGeneralSettings([]byte(`not json`)); err == nil {
		t.Fatal("非 JSON 應拒絕")
	}
}

func TestGeneralSettings_ClaudeAutoAllowAskMissingIsFalse(t *testing.T) {
	database, err := Open(t.TempDir() + "/general_settings_auto.db")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	got, err := database.GetGeneralSettings()
	if err != nil {
		t.Fatal(err)
	}
	if got.ClaudeAutoAllowAsk {
		t.Fatal("無列時 claudeAutoAllowAsk 應為 false")
	}

	if _, err := database.Exec(
		`INSERT INTO settings (key, value) VALUES (?, ?)`,
		SettingKeyGeneral, `{"vscodeNoAdmin":true}`,
	); err != nil {
		t.Fatal(err)
	}
	got, err = database.GetGeneralSettings()
	if err != nil {
		t.Fatal(err)
	}
	if got.ClaudeAutoAllowAsk {
		t.Fatal("舊 JSON 缺欄位時 claudeAutoAllowAsk 應為 false")
	}
	if !got.VscodeNoAdmin {
		t.Fatal("缺欄位不應清掉既有 vscodeNoAdmin")
	}

	saved, err := database.PutGeneralSettings([]byte(`{"vscodeNoAdmin":true,"claudeAutoAllowAsk":true}`))
	if err != nil {
		t.Fatal(err)
	}
	if !saved.ClaudeAutoAllowAsk || !saved.VscodeNoAdmin {
		t.Fatalf("寫入回傳應保留兩欄: %+v", saved)
	}
	got, err = database.GetGeneralSettings()
	if err != nil {
		t.Fatal(err)
	}
	if !got.ClaudeAutoAllowAsk || !got.VscodeNoAdmin {
		t.Fatalf("寫入後讀回應為 true: %+v", got)
	}
}

func TestGeneralSettings_AutoResumeAndEffectiveGuard(t *testing.T) {
	database, err := Open(t.TempDir() + "/general_effective.db")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	got, err := database.GetGeneralSettings()
	if err != nil {
		t.Fatal(err)
	}
	if got.AutoResumeOnQuota {
		t.Fatal("無列時 autoResumeOnQuota 應為 false")
	}
	eff, err := database.EffectiveGeneralSettings(90)
	if err != nil {
		t.Fatal(err)
	}
	if eff.AutoResumeOnQuota || !eff.QuotaGuardEnabled || eff.QuotaGuardPercent != 90 {
		t.Fatalf("從未儲存且 config 90：got %+v", eff)
	}
	eff, err = database.EffectiveGeneralSettings(0)
	if err != nil {
		t.Fatal(err)
	}
	if eff.AutoResumeOnQuota || eff.QuotaGuardEnabled || eff.QuotaGuardPercent != 90 {
		t.Fatalf("從未儲存且 config 0：got %+v", eff)
	}

	if _, err := database.Exec(
		`INSERT INTO settings (key, value) VALUES (?, ?)`,
		SettingKeyGeneral, `{"vscodeNoAdmin":true}`,
	); err != nil {
		t.Fatal(err)
	}
	got, err = database.GetGeneralSettings()
	if err != nil {
		t.Fatal(err)
	}
	if got.AutoResumeOnQuota || !got.VscodeNoAdmin {
		t.Fatalf("舊 JSON 缺 autoResumeOnQuota 應為 false：got %+v", got)
	}
	eff, err = database.EffectiveGeneralSettings(90)
	if err != nil {
		t.Fatal(err)
	}
	if !eff.VscodeNoAdmin || eff.AutoResumeOnQuota || !eff.QuotaGuardEnabled || eff.QuotaGuardPercent != 90 {
		t.Fatalf("舊 JSON 沒有守門鍵應視為尚未儲存：got %+v", eff)
	}

	if _, err := database.PutGeneralSettings([]byte(`{"vscodeNoAdmin":true,"quotaGuardEnabled":true,"quotaGuardPercent":75}`)); err != nil {
		t.Fatal(err)
	}
	eff, err = database.EffectiveGeneralSettings(0)
	if err != nil {
		t.Fatal(err)
	}
	if !eff.QuotaGuardEnabled || eff.QuotaGuardPercent != 75 || !eff.VscodeNoAdmin || eff.AutoResumeOnQuota {
		t.Fatalf("已儲存啟用 75 應覆蓋 config 0：got %+v", eff)
	}
}

func TestPutGeneralOffInvalidatesNoneAndKeepsQueue(t *testing.T) {
	database := openQuotaDB(t)
	if _, err := database.PutGeneralSettings([]byte(`{"autoResumeOnQuota":true,"quotaGuardEnabled":true,"quotaGuardPercent":90}`)); err != nil {
		t.Fatal(err)
	}
	s := newQuotaSession(t, database, "none-off")
	if _, err := database.EnqueueMessage(s.ID, "A", ""); err != nil {
		t.Fatal(err)
	}
	if err := database.SetQueuePaused(s.ID, true); err != nil {
		t.Fatal(err)
	}
	before, err := database.GetQuotaResumeGeneration(s.ID)
	if err != nil || before != 0 {
		t.Fatalf("generation=%d err=%v", before, err)
	}
	if _, err := database.PutGeneralSettings([]byte(`{"autoResumeOnQuota":false,"quotaGuardEnabled":true,"quotaGuardPercent":90}`)); err != nil {
		t.Fatal(err)
	}
	state, at, attempts, err := database.GetQuotaResume(s.ID)
	gen, gerr := database.GetQuotaResumeGeneration(s.ID)
	paused, perr := database.QueuePaused(s.ID)
	list, lerr := database.ListQueuedMessages(s.ID)
	if err != nil || gerr != nil || perr != nil || lerr != nil || state != QuotaResumeNone || at != "" || attempts != 0 || gen != before+1 || !paused || len(list) != 1 || list[0].Content != "A" {
		t.Fatalf("state=%s at=%s attempts=%d gen=%d paused=%v queue=%v err=%v %v %v %v", state, at, attempts, gen, paused, list, err, gerr, perr, lerr)
	}
	if _, err := database.PutGeneralSettings([]byte(`{"autoResumeOnQuota":false,"quotaGuardEnabled":true,"quotaGuardPercent":80}`)); err != nil {
		t.Fatal(err)
	}
	gen2, err := database.GetQuotaResumeGeneration(s.ID)
	got, gerr := database.GetGeneralSettings()
	if err != nil || gerr != nil || gen2 != gen || got.AutoResumeOnQuota || got.QuotaGuardPercent != 80 {
		t.Fatalf("false→false gen=%d want %d settings=%+v err=%v %v", gen2, gen, got, err, gerr)
	}
}

func TestPutGeneralOffFailureRollsBackGeneration(t *testing.T) {
	database := openQuotaDB(t)
	if _, err := database.PutGeneralSettings([]byte(`{"autoResumeOnQuota":true,"quotaGuardEnabled":true,"quotaGuardPercent":90}`)); err != nil {
		t.Fatal(err)
	}
	s := newQuotaSession(t, database, "off-rollback")
	if _, err := database.TransitionQuotaResume(s.ID, QuotaResumeNone, QuotaResumeWaiting, "2026-11-18T10:01:00Z", false); err != nil {
		t.Fatal(err)
	}
	before, err := database.GetQuotaResumeGeneration(s.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`CREATE TRIGGER fail_settings_write BEFORE UPDATE ON settings BEGIN SELECT RAISE(ABORT,'settings failed'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.PutGeneralSettings([]byte(`{"autoResumeOnQuota":false,"quotaGuardEnabled":true,"quotaGuardPercent":90}`)); err == nil {
		t.Fatal("設定寫入應失敗")
	}
	got, err := database.GetGeneralSettings()
	state, at, attempts, serr := database.GetQuotaResume(s.ID)
	gen, gerr := database.GetQuotaResumeGeneration(s.ID)
	if err != nil || serr != nil || gerr != nil || !got.AutoResumeOnQuota || got.QuotaGuardPercent != 90 || state != QuotaResumeWaiting || at != "2026-11-18T10:01:00Z" || attempts != 0 || gen != before {
		t.Fatalf("settings=%+v state=%s at=%s attempts=%d gen=%d before=%d err=%v %v %v", got, state, at, attempts, gen, before, err, serr, gerr)
	}
}
