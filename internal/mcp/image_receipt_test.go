package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	gomcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/jerry12122/Claude-Code-Mini-App/internal/api"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/db"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/media"
)

// 1x1 可解碼 PNG。截成前 8 byte 只剩簽章，翻第 40 byte 仍維持原長度。
var receiptPNG []byte

func init() {
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		panic(err)
	}
	receiptPNG = buf.Bytes()
}

func receiptDB(t *testing.T) (*deps, *db.Session, string) {
	t.Helper()
	database := setupTestDB(t)
	work := t.TempDir()
	sess, err := database.CreateSession("圖片", "d", work, "default", "claude", nil, "agent")
	if err != nil {
		t.Fatal(err)
	}
	uploads := t.TempDir()
	prev := media.WorkspaceDir
	media.WorkspaceDir = uploads
	t.Cleanup(func() { media.WorkspaceDir = prev })
	return &deps{db: database}, sess, work
}

func postImage(t *testing.T, d *deps, sessionID, text string, paths, ids []string) postMessageOut {
	t.Helper()
	_, out, err := d.postMessage(context.Background(), nil, postMessageIn{
		SessionID: sessionID, Text: text, ImagePaths: paths, AttachmentIDs: ids,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !out.OK || out.Delivery != "stored_verified" || out.SessionID != sessionID || out.MessageID == 0 {
		t.Fatalf("回執不完整：%+v", out)
	}
	return out
}

func TestPostMessageImageStoredAndReadable(t *testing.T) {
	d, sess, work := receiptDB(t)
	src := filepath.Join(work, "shot.png")
	if err := os.WriteFile(src, receiptPNG, 0o644); err != nil {
		t.Fatal(err)
	}
	out := postImage(t, d, sess.ID, "畫面", []string{src}, nil)
	if len(out.Attachments) != 1 || out.Attachments[0].Bytes != int64(len(receiptPNG)) || out.Attachments[0].Mime != "image/png" {
		t.Fatalf("附件回執錯誤：%+v", out.Attachments)
	}
	if _, err := os.Stat(src); err != nil {
		t.Fatal("來源被刪了")
	}
	body := readAttachmentHTTP(t, d, sess.ID, out.Attachments[0].ID)
	if !bytes.Equal(body, receiptPNG) {
		t.Fatalf("HTTP 位元組不一致：%d", len(body))
	}
	_, listed, err := d.getMessages(context.Background(), nil, getMessagesIn{SessionID: sess.ID, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(listed.Messages) != 1 || listed.Messages[0].ID != out.MessageID || len(listed.Messages[0].Attachments) != 1 {
		t.Fatalf("get_messages 讀不回附件：%+v", listed.Messages)
	}
	if listed.Messages[0].Attachments[0].ID != out.Attachments[0].ID {
		t.Fatal("回讀 id 不同")
	}
	other, err := d.db.CreateSession("別人", "d", t.TempDir(), "default", "claude", nil, "agent")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.db.GetAttachment(other.ID, out.Attachments[0].ID); err == nil {
		t.Fatal("別的 session 讀到這張圖")
	}
	only := postImage(t, d, sess.ID, "", []string{src}, nil)
	if only.Attachments[0].Bytes != int64(len(receiptPNG)) {
		t.Fatal("只有圖片的訊息沒有回讀到位元組")
	}
}

func TestPostMessageImageRejectsUnsafeSources(t *testing.T) {
	d, sess, work := receiptDB(t)
	before, err := d.db.ListMessages(sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "outside.png")
	if err := os.WriteFile(outside, receiptPNG, 0o644); err != nil {
		t.Fatal(err)
	}
	cases := []string{
		outside,
		"https://example.invalid/shot.png",
		filepath.Join(work, "missing.png"),
	}
	fake := filepath.Join(work, "fake.png")
	if err := os.WriteFile(fake, []byte("<svg onload='alert(1)'></svg>"), 0o644); err != nil {
		t.Fatal(err)
	}
	cases = append(cases, fake)
	empty := filepath.Join(work, "empty.png")
	if err := os.WriteFile(empty, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	cases = append(cases, empty)
	link := filepath.Join(work, "link.png")
	if err := os.Symlink(outside, link); err == nil {
		cases = append(cases, link)
	} else if runtime.GOOS != "windows" {
		t.Logf("略過檔案 symlink 子例：%v", err)
	}
	webp := filepath.Join(work, "pic.webp")
	if err := os.WriteFile(webp, []byte("RIFF\x00\x00\x00\x00WEBP"), 0o644); err != nil {
		t.Fatal(err)
	}
	cases = append(cases, webp)
	cases = addEscapeCase(t, work, cases)
	for _, path := range cases {
		_, _, err := d.postMessage(context.Background(), nil, postMessageIn{SessionID: sess.ID, Text: "不該成功", ImagePaths: []string{path}})
		if err == nil {
			t.Fatalf("危險來源不該成功：%s", path)
		}
	}
	after, err := d.db.ListMessages(sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != len(before) {
		t.Fatalf("失敗仍留下訊息：%d", len(after))
	}
	if _, err := os.Stat(outside); err != nil {
		t.Fatal("失敗時刪了來源")
	}
}

func TestPostMessageImageRoomIsSeparateFromPrivate(t *testing.T) {
	d, worker, work := receiptDB(t)
	pm, err := d.db.CreateSession("PM", "d", t.TempDir(), "default", "claude", nil, "agent")
	if err != nil {
		t.Fatal(err)
	}
	room, err := d.db.CreateRoom("群", []db.RoomMember{{SessionID: pm.ID, Role: "pm"}, {SessionID: worker.ID, Role: "worker"}})
	if err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(work, "room.png")
	if err := os.WriteFile(src, receiptPNG, 0o644); err != nil {
		t.Fatal(err)
	}
	workerOut := postImage(t, d, worker.ID, "給群", []string{src}, nil)
	if !contains(workerOut.RoomIDs, room.ID) {
		t.Fatalf("成員訊息應出現在工作室時間軸：%v", workerOut.RoomIDs)
	}
	private, err := d.db.CreateSession("私人", "d", t.TempDir(), "default", "claude", nil, "agent")
	if err != nil {
		t.Fatal(err)
	}
	privateSrc := filepath.Join(private.WorkDir, "private.png")
	if err := os.WriteFile(privateSrc, receiptPNG, 0o644); err != nil {
		t.Fatal(err)
	}
	privateOut := postImage(t, d, private.ID, "只在私人", []string{privateSrc}, nil)
	if len(privateOut.RoomIDs) != 0 {
		t.Fatalf("私人 session 不該冒充工作室有圖：%v", privateOut.RoomIDs)
	}
	page, err := d.db.RoomTimeline(room.ID, nil, 20)
	if err != nil {
		t.Fatal(err)
	}
	if !timelineHas(page.Items, workerOut.MessageID, worker.ID) {
		t.Fatal("時間軸沒有 worker 那則")
	}
	if timelineHas(page.Items, privateOut.MessageID, private.ID) {
		t.Fatal("時間軸出現非成員私人訊息")
	}
}

func readAttachmentHTTP(t *testing.T, d *deps, sessionID, attachmentID string) []byte {
	t.Helper()
	app := fiber.New()
	h := api.NewUploadHandler(d.db)
	app.Get("/sessions/:id/uploads/:attachmentId", h.Content)
	req := httptest.NewRequest(http.MethodGet, "/sessions/"+sessionID+"/uploads/"+attachmentID, nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 || !strings.HasPrefix(resp.Header.Get("Content-Type"), "image/") {
		t.Fatalf("附件 HTTP %d %s", resp.StatusCode, body)
	}
	return body
}

func addEscapeCase(t *testing.T, work string, cases []string) []string {
	t.Helper()
	jumpDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(jumpDir, "secret.png"), receiptPNG, 0o644); err != nil {
		t.Fatal(err)
	}
	junction := filepath.Join(work, "jump")
	if runtime.GOOS == "windows" {
		if err := exec.Command("cmd", "/c", "mklink", "/J", junction, jumpDir).Run(); err != nil {
			t.Fatal(err)
		}
		return append(cases, filepath.Join(junction, "secret.png"))
	}
	if err := os.Symlink(jumpDir, junction); err != nil {
		t.Logf("略過目錄 symlink 子例：%v", err)
		return cases
	}
	return append(cases, filepath.Join(junction, "secret.png"))
}

func contains(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}

func timelineHas(items []db.RoomTimelineItem, messageID int64, sessionID string) bool {
	for _, item := range items {
		if item.MessageID == messageID && item.SessionID == sessionID {
			return true
		}
	}
	return false
}

func TestPostMessageImageReusesManagedAttachment(t *testing.T) {
	d, sess, _ := receiptDB(t)
	path, err := media.SaveUpload(sess.ID, "kept.png", bytes.NewReader(receiptPNG))
	if err != nil {
		t.Fatal(err)
	}
	att := &db.Attachment{ID: "kept-png", SessionID: sess.ID, Name: "kept.png", StorageName: filepath.Base(path), MimeType: "image/png", Size: int64(len(receiptPNG))}
	if err := d.db.AddAttachment(att); err != nil {
		t.Fatal(err)
	}
	out := postImage(t, d, sess.ID, "沿用", nil, []string{att.ID})
	if out.Attachments[0].ID != att.ID {
		t.Fatalf("沒有沿用既有附件：%+v", out.Attachments)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal("既有附件被刪了")
	}
	other, err2 := d.db.CreateSession("別人", "d", t.TempDir(), "default", "claude", nil, "agent")
	if err2 != nil {
		t.Fatal(err2)
	}
	_, _, err = d.postMessage(context.Background(), nil, postMessageIn{SessionID: other.ID, Text: "跨會話", AttachmentIDs: []string{att.ID}})
	if err == nil {
		t.Fatal("別的 session 不該引用這張圖")
	}
}

func TestPostMessageImageFailureLeavesNoSuccess(t *testing.T) {
	d, sess, work := receiptDB(t)
	src := filepath.Join(work, "shot.png")
	if err := os.WriteFile(src, receiptPNG, 0o644); err != nil {
		t.Fatal(err)
	}
	_, _, err := d.postMessage(context.Background(), nil, postMessageIn{
		SessionID: sess.ID, Text: "半套", ImagePaths: []string{src}, AttachmentIDs: []string{"missing-id"},
	})
	if err == nil {
		t.Fatal("壞附件 id 不該成功")
	}
	msgs, err := d.db.ListMessages(sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 0 {
		t.Fatalf("失敗留下訊息：%d", len(msgs))
	}
	if _, err := os.Stat(src); err != nil {
		t.Fatal("失敗刪了來源")
	}
	dir := filepath.Join(media.WorkspaceDir, "uploads", sess.ID)
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Fatalf("失敗留下受控檔：%d", len(entries))
	}
	huge := filepath.Join(work, "huge.png")
	f, err := os.Create(huge)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write(receiptPNG[:8]); err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(8*1024*1024 + 1); err != nil {
		t.Fatal(err)
	}
	f.Close()
	_, _, err = d.postMessage(context.Background(), nil, postMessageIn{SessionID: sess.ID, Text: "太大", ImagePaths: []string{huge}})
	if err == nil {
		t.Fatal("超過大小不該成功")
	}
	if _, err := d.db.Exec(`UPDATE sessions SET agent_type = 'kiro' WHERE id = ?`, sess.ID); err != nil {
		t.Fatal(err)
	}
	_, _, err = d.postMessage(context.Background(), nil, postMessageIn{SessionID: sess.ID, Text: "唯讀", ImagePaths: []string{src}})
	if err == nil {
		t.Fatal("停用的 session 不該寫入")
	}
	msgs, err = d.db.ListMessages(sess.ID)
	if err != nil || len(msgs) != 0 {
		t.Fatalf("停用後仍有訊息：%v %d", err, len(msgs))
	}
}

func TestPostMessageImageRejectsSameSizeSwap(t *testing.T) {
	d, sess, work := receiptDB(t)
	red := solidPNG(t, color.RGBA{R: 255, A: 255})
	blue := solidPNG(t, color.RGBA{B: 255, A: 255})
	if len(red) != len(blue) || bytes.Equal(red, blue) {
		t.Fatalf("需要兩張同大小、可解碼、內容不同的圖：%d %d", len(red), len(blue))
	}
	src := filepath.Join(work, "red.png")
	if err := os.WriteFile(src, red, 0o644); err != nil {
		t.Fatal(err)
	}
	out := postImage(t, d, sess.ID, "紅", []string{src}, nil)
	if out.Attachments[0].SHA256 == "" {
		t.Fatal("回執沒有 sha256")
	}
	msg, err := d.db.MessageByID(sess.ID, out.MessageID)
	if err != nil {
		t.Fatal(err)
	}
	saved, err := media.ExistingUploadPath(msg.Attachments[0].SessionID, msg.Attachments[0].StorageName)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(saved, blue, 0o644); err != nil {
		t.Fatal(err)
	}
	expected := map[string]string{msg.Attachments[0].ID: digestHex(red)}
	if _, _, err := confirmStoredImages(d.db, sess.ID, msg, expected); err == nil {
		t.Fatal("同大小的另一張可解碼圖不該通過回讀")
	}
}

func TestPostMessageRollbackFailureReachesMCPClient(t *testing.T) {
	d, sess, work := receiptDB(t)
	src := filepath.Join(work, "shot.png")
	if err := os.WriteFile(src, receiptPNG, 0o644); err != nil {
		t.Fatal(err)
	}
	prevAfter, prevRollback := afterImageCommit, rollbackPostedImage
	t.Cleanup(func() { afterImageCommit, rollbackPostedImage = prevAfter, prevRollback })
	afterImageCommit = func() {
		dir := filepath.Join(media.WorkspaceDir, "uploads", sess.ID)
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Error(err)
			return
		}
		for _, entry := range entries {
			p := filepath.Join(dir, entry.Name())
			buf, err := os.ReadFile(p)
			if err != nil || len(buf) < 41 {
				continue
			}
			buf[40] ^= 1
			if err := os.WriteFile(p, buf, 0o644); err != nil {
				t.Error(err)
			}
		}
	}
	rollbackPostedImage = func(*db.DB, string, int64, []string) error { return os.ErrPermission }
	server := gomcp.NewServer(&gomcp.Implementation{Name: "post", Version: "1"}, nil)
	registerTools(server, d)
	ct, st := gomcp.NewInMemoryTransports()
	ss, err := server.Connect(t.Context(), st, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ss.Close() })
	client, err := gomcp.NewClient(&gomcp.Implementation{Name: "c", Version: "1"}, nil).Connect(t.Context(), ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	res, err := client.CallTool(t.Context(), &gomcp.CallToolParams{
		Name:      "post_message",
		Arguments: map[string]any{"session_id": sess.ID, "text": "半套", "image_paths": []string{src}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError {
		t.Fatalf("撤回失敗仍像成功：%+v", res)
	}
	raw, _ := json.Marshal(res.StructuredContent)
	text := string(raw)
	for _, block := range res.Content {
		if item, ok := block.(*gomcp.TextContent); ok {
			text += item.Text
		}
	}
	if !strings.Contains(text, `"message_id":1`) || !strings.Contains(text, `"retry":"do_not_resend"`) {
		t.Fatalf("客戶沒拿到 message_id 與 do_not_resend：%s", text)
	}
	if _, err := d.db.MessageByID(sess.ID, 1); err != nil {
		t.Fatal("撤回失敗後訊息應仍可定位", err)
	}
}

func solidPNG(t *testing.T, c color.Color) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	img.Set(0, 0, c)
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestPostMessageTextContractUnchanged(t *testing.T) {
	d, sess, _ := receiptDB(t)
	_, out, err := d.postMessage(context.Background(), nil, postMessageIn{SessionID: sess.ID, Text: "只有文字"})
	if err != nil || !out.OK || out.MessageID == 0 || out.Delivery != "" || len(out.Attachments) != 0 || len(out.RoomIDs) != 0 {
		t.Fatalf("純文字契約被改了：%+v %v", out, err)
	}
	_, listed, err := d.getMessages(context.Background(), nil, getMessagesIn{SessionID: sess.ID, Limit: 5})
	if err != nil || len(listed.Messages) != 1 || len(listed.Messages[0].Attachments) != 0 {
		t.Fatalf("純文字回讀帶了附件：%+v %v", listed.Messages, err)
	}
}
