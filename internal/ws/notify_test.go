package ws

import (
	"testing"

	"github.com/jerry12122/Claude-Code-Mini-App/internal/tg"
)

func TestFinishShellNotify_nonZeroExit(t *testing.T) {
	cfg := tg.DefaultNotifyConfig()
	// 不實際發 HTTP；exit 0 不應觸發（函式內 early return，此處只驗證邏輯路徑不 panic）
	finishShellNotify("", 0, cfg, "s", "ls", "", 0, false, nil)
	finishShellNotify("", 0, cfg, "s", "ls", "", 1, false, nil)
}
