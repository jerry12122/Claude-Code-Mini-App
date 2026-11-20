package ws

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/jerry12122/Claude-Code-Mini-App/internal/agent"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/db"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/media"
)

type pmPromptRunner struct {
	runs chan agent.RunOptions
}

func (r pmPromptRunner) Name() string { return "pm-prompt" }
func (r pmPromptRunner) Run(ctx context.Context, opts agent.RunOptions, cb agent.EventCallback) error {
	r.runs <- opts
	sid := opts.SessionID
	cb(agent.Event{Type: agent.EventSessionInit, SessionID: sid})
	cb(agent.Event{Type: agent.EventDelta, Text: "ok"})
	cb(agent.Event{Type: agent.EventDone})
	return nil
}

func installPMPromptRunner() chan agent.RunOptions {
	runs := make(chan agent.RunOptions, 4)
	agent.Register("pm-prompt", func() agent.Runner { return pmPromptRunner{runs: runs} })
	return runs
}

func usePMSkill(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "references"), 0o755); err != nil {
		t.Fatal(err)
	}
	if body != "" {
		if err := os.WriteFile(filepath.Join(dir, "references", "pm-core.md"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		rewritePMFull(t, dir, pmFullSkill)
	}
	t.Setenv("MINIAPP_PM_SKILL", dir)
	return dir
}

func rewritePMSkill(t *testing.T, dir, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "references", "pm-core.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// pmFullSkill 是 usePMSkill 預設放的完整 SKILL.md，內容刻意與短檔不同，用來確認全文不會被注入。
const pmFullSkill = "FULL-SKILL-WORKFLOW-BODY\n"

func rewritePMFull(t *testing.T, dir, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// pmCoreHeader 是版本標記的契約：短檔加完整 SKILL.md（中間隔一個 NUL）的 SHA-256 前 6 bytes。
func pmCoreHeader(body string) string {
	sum := sha256.Sum256([]byte(body + "\x00" + pmFullSkill))
	return "PM短規則 " + hex.EncodeToString(sum[:6])
}

var pmVersionRe = regexp.MustCompile(`PM短規則 ([0-9a-f]{12})`)

func pmVersion(t *testing.T, prompt string) string {
	t.Helper()
	m := pmVersionRe.FindStringSubmatch(prompt)
	if m == nil {
		t.Fatalf("沒有版本標記：%q", prompt)
	}
	return m[1]
}

func newPMPromptSession(t *testing.T, database *db.DB, name string) *db.Session {
	t.Helper()
	s, err := database.CreateSession(name, "", t.TempDir(), "default", "pm-prompt", nil, "agent")
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func takePMRun(t *testing.T, runs <-chan agent.RunOptions) agent.RunOptions {
	t.Helper()
	select {
	case got := <-runs:
		return got
	case <-time.After(5 * time.Second):
		t.Fatal("runner did not start")
		return agent.RunOptions{}
	}
}

func sendPMInput(t *testing.T, c *wsClient, database *db.DB, id string, runs <-chan agent.RunOptions, text string) agent.RunOptions {
	t.Helper()
	c.send(t, map[string]any{"type": "input", "data": text})
	got := takePMRun(t, runs)
	waitFor(t, "idle "+text, func() bool { return !taskIsActive(id) })
	return got
}

func assertHistory(t *testing.T, database *db.DB, id string, want []string) {
	t.Helper()
	if got := userMessages(database, id); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("history = %q, want %q", got, want)
	}
}

func TestPMPromptUserResumeAndReport(t *testing.T) {
	const body = "MARKER-CORE\n只派工\n"
	dir := usePMSkill(t, body)
	database, url := startTestServer(t)
	runs := installPMPromptRunner()
	pm := newPMPromptSession(t, database, "pm")
	room, err := database.CreateRoom("工作室", []db.RoomMember{{SessionID: pm.ID, Role: db.RoomRolePM}})
	if err != nil {
		t.Fatal(err)
	}
	c := dialClient(t, url(pm.ID))

	first := sendPMInput(t, c, database, pm.ID, runs, "使用者原文")
	if !strings.Contains(first.Prompt, pmCoreHeader(body)) || !strings.Contains(first.Prompt, "MARKER-CORE") || !strings.Contains(first.Prompt, "使用者原文") {
		t.Fatalf("first prompt = %q", first.Prompt)
	}
	// 新建工作室的 PM 第一輪沒有接任標記，但必須知道完整 skill 在哪、何時要讀；全文本身不注入。
	if want := filepath.Join(dir, "SKILL.md"); !strings.Contains(first.Prompt, want) || !strings.Contains(first.Prompt, "首次") || strings.Contains(first.Prompt, "PM接任") || strings.Contains(first.Prompt, "FULL-SKILL-WORKFLOW-BODY") {
		t.Fatalf("first turn guidance = %q", first.Prompt)
	}
	if first.SystemInstruction != "" {
		t.Fatalf("system instruction changed: %q", first.SystemInstruction)
	}
	assertHistory(t, database, pm.ID, []string{"使用者原文"})

	if err := database.SetQueuePaused(pm.ID, true); err != nil {
		t.Fatal(err)
	}
	if _, err := database.EnqueueMessage(pm.ID, "恢復原文", ""); err != nil {
		t.Fatal(err)
	}
	c.send(t, map[string]any{"type": "queue_resume"})
	resumed := takePMRun(t, runs)
	waitFor(t, "resume idle", func() bool { return !taskIsActive(pm.ID) })
	if !strings.Contains(resumed.Prompt, "MARKER-CORE") || !strings.Contains(resumed.Prompt, "恢復原文") {
		t.Fatalf("resume prompt = %q", resumed.Prompt)
	}
	assertHistory(t, database, pm.ID, []string{"使用者原文", "恢復原文"})

	if err := database.SetQueuePaused(pm.ID, true); err != nil {
		t.Fatal(err)
	}
	if _, err := database.EnqueueMessage(pm.ID, "回報原文", "room-report:"+room.ID); err != nil {
		t.Fatal(err)
	}
	c.send(t, map[string]any{"type": "queue_resume"})
	report := takePMRun(t, runs)
	waitFor(t, "report idle", func() bool { return !taskIsActive(pm.ID) })
	if !strings.Contains(report.Prompt, "MARKER-CORE") || !strings.Contains(report.Prompt, "[執行者回報] 回報原文") {
		t.Fatalf("report prompt = %q", report.Prompt)
	}
	assertHistory(t, database, pm.ID, []string{"使用者原文", "恢復原文", "回報原文"})
}

func TestPMPromptFollowsCurrentRole(t *testing.T) {
	const body = "MARKER-ROLE\n"
	usePMSkill(t, body)
	database, url := startTestServer(t)
	runs := installPMPromptRunner()
	pm := newPMPromptSession(t, database, "pm")
	worker := newPMPromptSession(t, database, "worker")
	outsider := newPMPromptSession(t, database, "outsider")
	room, err := database.CreateRoom("工作室", []db.RoomMember{
		{SessionID: pm.ID, Role: db.RoomRolePM},
		{SessionID: worker.ID, Role: db.RoomRoleWorker},
	})
	if err != nil {
		t.Fatal(err)
	}
	pmConn := dialClient(t, url(pm.ID))
	workerConn := dialClient(t, url(worker.ID))
	outConn := dialClient(t, url(outsider.ID))

	if got := sendPMInput(t, workerConn, database, worker.ID, runs, "執行者原文"); strings.Contains(got.Prompt, "MARKER-ROLE") || got.Prompt != "執行者原文" {
		t.Fatalf("worker prompt = %q", got.Prompt)
	}
	if got := sendPMInput(t, outConn, database, outsider.ID, runs, "外部原文"); strings.Contains(got.Prompt, "MARKER-ROLE") || got.Prompt != "外部原文" {
		t.Fatalf("outsider prompt = %q", got.Prompt)
	}
	if got := sendPMInput(t, pmConn, database, pm.ID, runs, "PM原文"); !strings.Contains(got.Prompt, "MARKER-ROLE") || !strings.Contains(got.Prompt, "PM原文") {
		t.Fatalf("pm prompt = %q", got.Prompt)
	}

	members := []db.RoomMember{
		{SessionID: worker.ID, Role: db.RoomRolePM},
		{SessionID: pm.ID, Role: db.RoomRoleWorker},
	}
	if err := database.UpdateRoom(room.ID, nil, &members); err != nil {
		t.Fatal(err)
	}
	if got := sendPMInput(t, pmConn, database, pm.ID, runs, "卸任後"); strings.Contains(got.Prompt, "MARKER-ROLE") || got.Prompt != "卸任後" {
		t.Fatalf("former pm prompt = %q", got.Prompt)
	}
	if got := sendPMInput(t, workerConn, database, worker.ID, runs, "升任後"); !strings.Contains(got.Prompt, "MARKER-ROLE") || !strings.Contains(got.Prompt, "升任後") {
		t.Fatalf("promoted prompt = %q", got.Prompt)
	}
}

func TestPMPromptKeepsInstructionAttachmentHistory(t *testing.T) {
	const body = "MARKER-KEEP\n"
	usePMSkill(t, body)
	database, url := startTestServer(t)
	runs := installPMPromptRunner()
	old := media.WorkspaceDir
	media.WorkspaceDir = t.TempDir()
	t.Cleanup(func() { media.WorkspaceDir = old })

	s, err := database.CreateSessionWithInstructions("pm", "", t.TempDir(), "default", "pm-prompt", nil, "agent", "", "專屬規則", db.GlobalInstruction{Text: "全域規則", Mode: db.GlobalInstructionEvery})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.CreateRoom("工作室", []db.RoomMember{{SessionID: s.ID, Role: db.RoomRolePM}}); err != nil {
		t.Fatal(err)
	}
	path, err := media.SaveUpload(s.ID, "note.txt", strings.NewReader("note"))
	if err != nil {
		t.Fatal(err)
	}
	if err := database.AddAttachment(&db.Attachment{ID: "file", SessionID: s.ID, Name: "note.txt", StorageName: filepath.Base(path), MimeType: "text/plain", Size: 4}); err != nil {
		t.Fatal(err)
	}
	c := dialClient(t, url(s.ID))
	c.send(t, map[string]any{"type": "input", "data": "含附件原文", "attachment_ids": []string{"file"}})
	got := takePMRun(t, runs)
	waitFor(t, "attachment idle", func() bool { return !taskIsActive(s.ID) })
	for _, part := range []string{"MARKER-KEEP", "全域指令", "全域規則", "使用者訊息", "[附件]", path, "含附件原文"} {
		if !strings.Contains(got.Prompt, part) {
			t.Fatalf("prompt missing %q: %q", part, got.Prompt)
		}
	}
	if got.SystemInstruction != "專屬規則" {
		t.Fatalf("system instruction = %q", got.SystemInstruction)
	}
	assertHistory(t, database, s.ID, []string{"含附件原文"})
	msgs, err := database.ListMessages(s.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range msgs {
		if strings.Contains(m.Content, "MARKER-KEEP") || strings.Contains(m.Content, path) || strings.Contains(m.Content, "全域規則") {
			t.Fatalf("history rewritten: %+v", m)
		}
	}
}

func TestPMPromptMissingFileAndVersion(t *testing.T) {
	dir := usePMSkill(t, "")
	database, url := startTestServer(t)
	runs := installPMPromptRunner()
	pm := newPMPromptSession(t, database, "pm")
	if _, err := database.CreateRoom("工作室", []db.RoomMember{{SessionID: pm.ID, Role: db.RoomRolePM}}); err != nil {
		t.Fatal(err)
	}
	c := dialClient(t, url(pm.ID))
	missing := sendPMInput(t, c, database, pm.ID, runs, "缺檔原文")
	prefix, ok := strings.CutSuffix(missing.Prompt, "缺檔原文")
	if !ok || !strings.Contains(prefix, "PM短規則未載入") || strings.Contains(prefix, "派工") || len([]rune(prefix)) > 80 {
		t.Fatalf("missing diagnostic = %q", missing.Prompt)
	}
	assertHistory(t, database, pm.ID, []string{"缺檔原文"})

	// 短檔已補上但全文仍缺失，必須明確診斷 SKILL.md，且保留原歷史。
	rewritePMSkill(t, dir, "MARKER-CORE\n")
	missingFull := sendPMInput(t, c, database, pm.ID, runs, "缺全文原文")
	prefix, ok = strings.CutSuffix(missingFull.Prompt, "缺全文原文")
	if !ok || !strings.Contains(prefix, "PM短規則未載入") || !strings.Contains(prefix, "SKILL.md") || len([]rune(prefix)) > 80 {
		t.Fatalf("missing SKILL.md diagnostic = %q", missingFull.Prompt)
	}
	assertHistory(t, database, pm.ID, []string{"缺檔原文", "缺全文原文"})

	const alpha = "MARKER-ALPHA\n"
	const beta = "MARKER-BETA\n"
	rewritePMFull(t, dir, pmFullSkill)
	rewritePMSkill(t, dir, alpha)
	gotAlpha := sendPMInput(t, c, database, pm.ID, runs, "版本甲")
	if !strings.Contains(gotAlpha.Prompt, pmCoreHeader(alpha)) || !strings.Contains(gotAlpha.Prompt, "MARKER-ALPHA") || strings.Contains(gotAlpha.Prompt, "MARKER-BETA") {
		t.Fatalf("alpha prompt = %q", gotAlpha.Prompt)
	}
	rewritePMSkill(t, dir, beta)
	gotBeta := sendPMInput(t, c, database, pm.ID, runs, "版本乙")
	if !strings.Contains(gotBeta.Prompt, pmCoreHeader(beta)) || !strings.Contains(gotBeta.Prompt, "MARKER-BETA") || strings.Contains(gotBeta.Prompt, "MARKER-ALPHA") {
		t.Fatalf("beta prompt = %q", gotBeta.Prompt)
	}
	if pmCoreHeader(alpha) == pmCoreHeader(beta) {
		t.Fatal("version hash did not change")
	}
}

func TestPMPromptVersionFollowsFullSkillAndShortFile(t *testing.T) {
	const core = "MARKER-CORE\n"
	dir := usePMSkill(t, core)
	database, url := startTestServer(t)
	runs := installPMPromptRunner()
	pm := newPMPromptSession(t, database, "pm")
	if _, err := database.CreateRoom("工作室", []db.RoomMember{{SessionID: pm.ID, Role: db.RoomRolePM}}); err != nil {
		t.Fatal(err)
	}
	c := dialClient(t, url(pm.ID))
	turn := func(text string) string {
		got := sendPMInput(t, c, database, pm.ID, runs, text)
		if strings.Contains(got.Prompt, "FULL-SKILL-WORKFLOW-BODY") || strings.Contains(got.Prompt, "FULL-SKILL-CHANGED") {
			t.Fatalf("完整 skill 全文被注入：%q", got.Prompt)
		}
		return pmVersion(t, got.Prompt)
	}
	v1 := turn("一")
	if again := turn("二"); again != v1 {
		t.Fatalf("內容沒變版本卻變了 %s → %s", v1, again)
	}
	rewritePMFull(t, dir, "FULL-SKILL-CHANGED\n")
	v2 := turn("三")
	if v2 == v1 {
		t.Fatal("只改 SKILL.md，版本沒有變")
	}
	rewritePMSkill(t, dir, "MARKER-CORE-2\n")
	if v3 := turn("四"); v3 == v2 || v3 == v1 {
		t.Fatalf("只改 pm-core.md，版本沒有變 %s", v3)
	}
	assertHistory(t, database, pm.ID, []string{"一", "二", "三", "四"})
}

func handoffCount(t *testing.T, database *db.DB, sessionID, roomID string) int {
	t.Helper()
	msgs, err := database.ListMessages(sessionID)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, m := range msgs {
		if m.Author == "room-handoff:"+roomID {
			n++
		}
	}
	return n
}

func assertNoPMRun(t *testing.T, runs <-chan agent.RunOptions) {
	t.Helper()
	select {
	case got := <-runs:
		t.Fatalf("handoff started a run: %q", got.Prompt)
	default:
	}
}

func TestPMPromptHandoffReinjection(t *testing.T) {
	const body = "MARKER-HANDOFF\n"
	const memo = "MEMO-DONT-DUMP"
	dir := usePMSkill(t, body)
	database, url := startTestServer(t)
	runs := installPMPromptRunner()
	makeSession := func(name, instruction, agentID string) *db.Session {
		t.Helper()
		s, err := database.CreateSessionWithInstruction(name, "", t.TempDir(), "default", "pm-prompt", nil, "agent", "", instruction)
		if err != nil {
			t.Fatal(err)
		}
		if err := database.UpdateAgentSessionID(s.ID, agentID); err != nil {
			t.Fatal(err)
		}
		if err := database.AddMessage(s.ID, "user", name+"舊上下文", ""); err != nil {
			t.Fatal(err)
		}
		return s
	}
	a := makeSession("A", "專屬A", "agent-A")
	b := makeSession("B", "專屬B", "agent-B")
	room, err := database.CreateRoom("工作室", []db.RoomMember{
		{SessionID: a.ID, Role: db.RoomRolePM},
		{SessionID: b.ID, Role: db.RoomRoleWorker},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.SaveRoomMemo(room.ID, a.ID, memo); err != nil {
		t.Fatal(err)
	}
	aConn := dialClient(t, url(a.ID))
	bConn := dialClient(t, url(b.ID))
	if err := database.SetQueuePaused(b.ID, true); err != nil {
		t.Fatal(err)
	}
	if _, err := database.EnqueueMessage(b.ID, "待接任工作", ""); err != nil {
		t.Fatal(err)
	}
	toB := []db.RoomMember{{SessionID: b.ID, Role: db.RoomRolePM}, {SessionID: a.ID, Role: db.RoomRoleWorker}}
	if err := database.UpdateRoom(room.ID, nil, &toB, a.ID); err != nil {
		t.Fatal(err)
	}
	assertNoPMRun(t, runs)
	if handoffCount(t, database, b.ID, room.ID) != 1 || handoffCount(t, database, a.ID, room.ID) != 0 {
		t.Fatal("handoff notice went to the wrong session")
	}

	bConn.send(t, map[string]any{"type": "queue_resume"})
	first := takePMRun(t, runs)
	waitFor(t, "B takeover idle", func() bool { return !taskIsActive(b.ID) })
	if first.SessionID != "agent-B" || first.SystemInstruction != "專屬B" {
		t.Fatalf("resume id=%q instruction=%q", first.SessionID, first.SystemInstruction)
	}
	for _, part := range []string{"PM接任 " + room.ID, filepath.Join(dir, "SKILL.md"), pmCoreHeader(body), "MARKER-HANDOFF", "待接任工作"} {
		if !strings.Contains(first.Prompt, part) {
			t.Fatalf("takeover missing %q: %q", part, first.Prompt)
		}
	}
	if strings.Contains(first.Prompt, memo) {
		t.Fatalf("takeover dumped the memo: %q", first.Prompt)
	}
	storedB, err := database.GetSession(b.ID)
	if err != nil || storedB.SystemInstruction != "專屬B" || storedB.AgentSessionID != "agent-B" {
		t.Fatalf("B changed: %+v %v", storedB, err)
	}

	second := sendPMInput(t, bConn, database, b.ID, runs, "後續工作")
	if strings.Contains(second.Prompt, "PM接任") || !strings.Contains(second.Prompt, "MARKER-HANDOFF") || !strings.Contains(second.Prompt, "後續工作") || second.SessionID != "agent-B" {
		t.Fatalf("later turn = %q id=%q", second.Prompt, second.SessionID)
	}

	former := sendPMInput(t, aConn, database, a.ID, runs, "卸任後原文")
	if former.Prompt != "卸任後原文" || former.SystemInstruction != "專屬A" || former.SessionID != "agent-A" {
		t.Fatalf("former pm = %q id=%q instruction=%q", former.Prompt, former.SessionID, former.SystemInstruction)
	}

	if err := database.UpdateSessionStatus(b.ID, db.SessionStatusRunning); err != nil {
		t.Fatal(err)
	}
	toA := []db.RoomMember{{SessionID: a.ID, Role: db.RoomRolePM}, {SessionID: b.ID, Role: db.RoomRoleWorker}}
	if err := database.UpdateRoom(room.ID, nil, &toA, b.ID); !errors.Is(err, db.ErrRoomHandoffBusy) {
		t.Fatalf("busy handoff err = %v", err)
	}
	assertNoPMRun(t, runs)
	if handoffCount(t, database, a.ID, room.ID) != 0 {
		t.Fatal("failed handoff wrote a notice")
	}
	if got := sendPMInput(t, aConn, database, a.ID, runs, "失敗後仍是執行者"); got.Prompt != "失敗後仍是執行者" {
		t.Fatalf("failed handoff injected: %q", got.Prompt)
	}
	if err := database.UpdateSessionStatus(b.ID, db.SessionStatusIdle); err != nil {
		t.Fatal(err)
	}
	if err := database.UpdateRoom(room.ID, nil, &toA, b.ID); err != nil {
		t.Fatal(err)
	}
	assertNoPMRun(t, runs)
	if handoffCount(t, database, a.ID, room.ID) != 1 {
		t.Fatal("return handoff missing")
	}
	returned := sendPMInput(t, aConn, database, a.ID, runs, "A接回")
	if returned.SessionID != "agent-A" || returned.SystemInstruction != "專屬A" || strings.Contains(returned.Prompt, memo) {
		t.Fatalf("return = %q id=%q instruction=%q", returned.Prompt, returned.SessionID, returned.SystemInstruction)
	}
	for _, part := range []string{"PM接任 " + room.ID, filepath.Join(dir, "SKILL.md"), pmCoreHeader(body), "A接回"} {
		if !strings.Contains(returned.Prompt, part) {
			t.Fatalf("return missing %q: %q", part, returned.Prompt)
		}
	}
	again := sendPMInput(t, aConn, database, a.ID, runs, "A的下一輪")
	if strings.Contains(again.Prompt, "PM接任") || !strings.Contains(again.Prompt, "MARKER-HANDOFF") || again.SessionID != "agent-A" {
		t.Fatalf("A later = %q id=%q", again.Prompt, again.SessionID)
	}
	storedA, err := database.GetSession(a.ID)
	if err != nil || storedA.SystemInstruction != "專屬A" || storedA.AgentSessionID != "agent-A" {
		t.Fatalf("A changed: %+v %v", storedA, err)
	}
}
