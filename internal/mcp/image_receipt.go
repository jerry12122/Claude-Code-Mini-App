package mcp

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
	gomcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/jerry12122/Claude-Code-Mini-App/internal/db"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/media"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/tg"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/ws"
)

const maxPostImages = 8

var (
	afterImageCommit    func()
	rollbackPostedImage = func(database *db.DB, sessionID string, messageID int64, freshIDs []string) error {
		return database.DeleteImageMessage(sessionID, messageID, freshIDs)
	}
)

func digestHex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func hasPostImages(in postMessageIn) bool {
	return len(in.ImagePaths) > 0 || len(in.AttachmentIDs) > 0
}

func receiptsFrom(atts []db.Attachment) []imageReceipt {
	out := make([]imageReceipt, 0, len(atts))
	for _, a := range atts {
		out = append(out, imageReceipt{ID: a.ID, URL: a.URL, Mime: a.MimeType, Bytes: a.Size})
	}
	return out
}

func (d *deps) deliverPostImages(sess *db.Session, role, text, author string, in postMessageIn) (*gomcp.CallToolResult, postMessageOut, error) {
	if role != "claude" && role != "system" {
		return nil, postMessageOut{}, fmt.Errorf("圖片只寫入 assistant 或 system")
	}
	paths, ids := in.ImagePaths, in.AttachmentIDs
	if len(paths)+len(ids) > maxPostImages {
		return nil, postMessageOut{}, fmt.Errorf("單次最多 %d 張圖片", maxPostImages)
	}
	expected := map[string]string{}
	if err := fillImageSums(d.db, sess.ID, ids, expected); err != nil {
		return nil, postMessageOut{}, err
	}
	fresh, files, err := stageChatImages(sess, paths)
	if err != nil {
		removeManaged(files)
		return nil, postMessageOut{}, err
	}
	for _, a := range fresh {
		if a.AcceptedSHA256 == "" {
			removeManaged(files)
			return nil, postMessageOut{}, fmt.Errorf("圖片雜湊沒有建立")
		}
		expected[a.ID] = a.AcceptedSHA256
	}
	all := make([]string, 0, len(fresh)+len(ids))
	for _, a := range fresh {
		all = append(all, a.ID)
	}
	all = append(all, ids...)
	msg, err := d.db.BindImageMessage(sess.ID, role, text, author, fresh, all)
	if err != nil {
		removeManaged(files)
		return nil, postMessageOut{}, err
	}
	if afterImageCommit != nil {
		afterImageCommit()
	}
	stored, receipts, err := confirmStoredImages(d.db, sess.ID, msg, expected)
	if err != nil {
		freshIDs := make([]string, len(fresh))
		for i, a := range fresh {
			freshIDs[i] = a.ID
		}
		if delErr := rollbackPostedImage(d.db, sess.ID, msg.ID, freshIDs); delErr != nil {
			return &gomcp.CallToolResult{IsError: true}, postMessageOut{
				MessageID: msg.ID, SessionID: sess.ID, Delivery: "unknown", Retry: "do_not_resend",
			}, nil
		}
		removeUnreferenced(d.db, sess.ID, fresh, files)
		return nil, postMessageOut{}, fmt.Errorf("回讀失敗，已撤回：%w", err)
	}
	_ = d.db.TouchSessionLastActive(sess.ID)
	ws.BroadcastSessionMessageAttachments(sess.ID, "message", text, role, author, stored.ID, stored.CreatedAt, stored.Attachments)
	ws.NotifySessionsChanged()
	d.notifyPost(in.Notify, text)
	return nil, postMessageOut{
		OK: true, MessageID: stored.ID, CreatedAt: stored.CreatedAt, SessionID: sess.ID,
		Delivery: "stored_verified", Attachments: receipts, RoomIDs: roomsShowingMessage(d.db, sess.ID, stored.ID),
	}, nil
}

func (d *deps) notifyPost(notify bool, text string) {
	if !notify || text == "" || d.botToken == "" {
		return
	}
	notifyFn := d.notifier
	if notifyFn == nil {
		notifyFn = tg.Notify
	}
	users, err := d.db.ListUsers()
	if err != nil {
		return
	}
	for _, uid := range users {
		_ = notifyFn(d.botToken, uid, text)
	}
}

func stageChatImages(sess *db.Session, paths []string) ([]db.Attachment, []string, error) {
	var fresh []db.Attachment
	var files []string
	for _, raw := range paths {
		data, name, err := readAllowedChatImage(sess, raw)
		if err != nil {
			return fresh, files, err
		}
		mime, ext, err := media.SniffChatImage(data)
		if err != nil {
			return fresh, files, err
		}
		saved, err := media.SaveUpload(sess.ID, "image"+ext, bytes.NewReader(data))
		if err != nil {
			return fresh, files, err
		}
		files = append(files, saved)
		id := uuid.NewString()
		fresh = append(fresh, db.Attachment{
			ID: id, SessionID: sess.ID, Name: name, StorageName: filepath.Base(saved),
			MimeType: mime, Size: int64(len(data)), AcceptedSHA256: digestHex(data),
		})
	}
	return fresh, files, nil
}

func readAllowedChatImage(sess *db.Session, raw string) ([]byte, string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.Contains(raw, "://") || strings.HasPrefix(strings.ToLower(raw), "data:") {
		return nil, "", fmt.Errorf("只接受目標 session 工作目錄或受控上傳區內的本機圖片")
	}
	real, err := canonicalRegular(raw)
	if err != nil || (!pathInside(sess.WorkDir, real) && !pathInsideUpload(sess.ID, real)) {
		return nil, "", fmt.Errorf("只接受目標 session 工作目錄或受控上傳區內的本機圖片")
	}
	f, err := os.Open(real)
	if err != nil {
		return nil, "", fmt.Errorf("只接受目標 session 工作目錄或受控上傳區內的本機圖片")
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, int64(media.ChatImageLimit())+1))
	if err != nil {
		return nil, "", fmt.Errorf("讀取圖片失敗")
	}
	if _, _, err := media.SniffChatImage(data); err != nil {
		return nil, "", err
	}
	return data, imageDisplayName(filepath.Base(real)), nil
}

func verifyExistingImages(database *db.DB, sessionID string, ids []string) error {
	return fillImageSums(database, sessionID, ids, map[string]string{})
}

func fillImageSums(database *db.DB, sessionID string, ids []string, expected map[string]string) error {
	seen := map[string]bool{}
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			return fmt.Errorf("附件 id 無效")
		}
		seen[id] = true
		a, err := database.GetAttachment(sessionID, id)
		if err != nil {
			return fmt.Errorf("附件不存在或不屬於此 session")
		}
		data, err := readStoredImage(a)
		if err != nil {
			return fmt.Errorf("附件不是可傳送的圖片")
		}
		expected[a.ID] = digestHex(data)
	}
	return nil
}

func confirmStoredImages(database *db.DB, sessionID string, msg *db.Message, expected map[string]string) (*db.Message, []imageReceipt, error) {
	got, err := database.MessageByID(sessionID, msg.ID)
	if err != nil || got.Content != msg.Content || got.Role != msg.Role || len(got.Attachments) != len(msg.AttachmentIDs) {
		return nil, nil, fmt.Errorf("回讀訊息失敗")
	}
	receipts := make([]imageReceipt, 0, len(got.Attachments))
	for i, a := range got.Attachments {
		if a.ID != msg.AttachmentIDs[i] {
			return nil, nil, fmt.Errorf("回讀附件順序不一致")
		}
		data, err := readStoredImage(&a)
		if err != nil || int64(len(data)) != a.Size {
			return nil, nil, fmt.Errorf("回讀附件與儲存內容不一致")
		}
		want := expected[a.ID]
		if want == "" || want != digestHex(data) {
			return nil, nil, fmt.Errorf("回讀附件與原本位元組不一致")
		}
		mime, _, err := media.SniffChatImage(data)
		if err != nil {
			return nil, nil, err
		}
		receipts = append(receipts, imageReceipt{ID: a.ID, URL: a.URL, Mime: mime, Bytes: int64(len(data)), SHA256: want})
	}
	return got, receipts, nil
}

func readStoredImage(a *db.Attachment) ([]byte, error) {
	p, err := media.ExistingUploadPath(a.SessionID, a.StorageName)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(p)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, int64(media.ChatImageLimit())+1))
	if err != nil {
		return nil, err
	}
	if _, _, err := media.SniffChatImage(data); err != nil {
		return nil, err
	}
	return data, nil
}

func roomsShowingMessage(database *db.DB, sessionID string, messageID int64) []string {
	rooms, err := database.SessionRoomIDs(sessionID)
	if err != nil {
		return nil
	}
	var hit []string
	for _, roomID := range rooms {
		page, err := database.RoomTimeline(roomID, nil, 50)
		if err != nil {
			continue
		}
		for _, item := range page.Items {
			if item.MessageID == messageID && item.SessionID == sessionID {
				hit = append(hit, roomID)
				break
			}
		}
	}
	return hit
}

func canonicalRegular(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	if _, err := os.Lstat(abs); err != nil {
		return "", err
	}
	real, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(real)
	if err != nil || !info.Mode().IsRegular() {
		return "", fmt.Errorf("不是一般檔案")
	}
	return real, nil
}

func pathInside(root, file string) bool {
	if strings.TrimSpace(root) == "" {
		return false
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return false
	}
	real, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return false
	}
	return hasPathPrefix(real, file)
}

func pathInsideUpload(sessionID, file string) bool {
	return pathInside(filepath.Join(media.WorkspaceDir, "uploads", sessionID), file)
}

func hasPathPrefix(root, file string) bool {
	root = strings.TrimRight(filepath.Clean(root), `\/`)
	file = filepath.Clean(file)
	if len(file) <= len(root) || !strings.EqualFold(file[:len(root)], root) {
		return false
	}
	sep := file[len(root)]
	return sep == os.PathSeparator || sep == '/' || sep == '\\'
}

func imageDisplayName(base string) string {
	base = filepath.Base(strings.ReplaceAll(base, `\`, "/"))
	if base == "." || base == ".." || base == "" || strings.ContainsAny(base, `/\:`) {
		return "image"
	}
	return base
}

func removeManaged(paths []string) {
	for _, p := range paths {
		if p != "" {
			_ = os.Remove(p)
		}
	}
}

func removeUnreferenced(database *db.DB, sessionID string, fresh []db.Attachment, files []string) {
	for i, a := range fresh {
		_, err := database.GetAttachment(sessionID, a.ID)
		if err == nil || !attachmentRowGone(err) {
			continue
		}
		if i < len(files) {
			_ = os.Remove(files[i])
		}
	}
}

func attachmentRowGone(err error) bool {
	return err != nil && err.Error() == "附件不存在或不屬於此會話"
}
