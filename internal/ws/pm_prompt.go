package ws

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/jerry12122/Claude-Code-Mini-App/internal/db"
)

type pmCoreNote struct {
	dir, hash, text string
}

// short 每輪都帶完整 skill 的確切路徑與版本，首次擔任 PM、未載入、版本變更時才知道去哪讀。
func (n pmCoreNote) short() string {
	return "PM短規則 " + n.hash + "（完整 skill " + filepath.Join(n.dir, "SKILL.md") +
		"；首次、接任、版本不同或上下文沒有此版本時先讀全文，否則沿用）\n" + n.text
}

func (n pmCoreNote) takeover(roomID string) string {
	return "PM接任 " + roomID + "\n本輪讀完整 skill，並讀本工作室備忘與未完成工作。原 PM 已讀不等於你已讀。\n\n" + n.short()
}

// withPMCore 只在現任 PM 的使用者訊息、佇列恢復與執行者回報前附上短規則。
// 交接已提交時，接任首輪另加身份、room、skill 位置與版本；其後只留短規則。
// 規則正文來自 skill 檔，不寫進歷史，也不取代全域或專屬指令。
func withPMCore(database *db.DB, sessionID string, userMsgID int64, prompt string) string {
	if userMsgID <= 0 || database == nil {
		return prompt
	}
	roomID, err := database.RoomForPM(sessionID)
	if err != nil {
		return prompt
	}
	pending, err := database.FirstTurnAfterHandoff(sessionID, roomID, userMsgID)
	if err != nil {
		pending = false
	}
	note, err := loadPMCore()
	if err != nil {
		msg := "PM短規則未載入：" + err.Error()
		if pending {
			msg = "PM接任 " + roomID + "\n" + msg
		}
		return msg + "\n\n" + prompt
	}
	if pending {
		return note.takeover(roomID) + "\n\n" + prompt
	}
	return note.short() + "\n\n" + prompt
}

// loadPMCore 版本是短檔加完整 SKILL.md 的雜湊：任一份改動，下一輪的版本就不同。
func loadPMCore() (pmCoreNote, error) {
	dir, err := pmSkillDir()
	if err != nil {
		return pmCoreNote{}, err
	}
	core, err := os.ReadFile(filepath.Join(dir, "references", "pm-core.md"))
	if err != nil {
		return pmCoreNote{}, errors.New("找不到 references/pm-core.md")
	}
	text := strings.TrimSpace(string(core))
	if text == "" {
		return pmCoreNote{}, errors.New("references/pm-core.md 是空的")
	}
	full, err := os.ReadFile(filepath.Join(dir, "SKILL.md"))
	if err != nil {
		return pmCoreNote{}, errors.New("找不到 SKILL.md")
	}
	if strings.TrimSpace(string(full)) == "" {
		return pmCoreNote{}, errors.New("SKILL.md 是空的")
	}
	h := sha256.New()
	h.Write(core)
	h.Write([]byte{0})
	h.Write(full)
	return pmCoreNote{dir: dir, hash: hex.EncodeToString(h.Sum(nil)[:6]), text: text}, nil
}

// pmSkillDir 先看 MINIAPP_PM_SKILL，否則用使用者目錄下的 supervisor-workflow。
func pmSkillDir() (string, error) {
	if p := strings.TrimSpace(os.Getenv("MINIAPP_PM_SKILL")); p != "" {
		return p, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	claude := filepath.Join(home, ".claude", "skills", "supervisor-workflow")
	agents := filepath.Join(home, ".agents", "skills", "supervisor-workflow")
	if _, err := os.Stat(filepath.Join(claude, "references", "pm-core.md")); err != nil {
		if _, altErr := os.Stat(filepath.Join(agents, "references", "pm-core.md")); altErr == nil {
			return agents, nil
		}
	}
	return claude, nil
}
