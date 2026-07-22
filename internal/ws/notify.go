package ws

import (
	"fmt"
	"log"

	"github.com/jerry12122/Claude-Code-Mini-App/internal/tg"
)

func notifyTaskAsync(botToken string, chatID int64, cfg tg.NotifyConfig, alert tg.TaskAlert) {
	if botToken == "" || chatID == 0 {
		return
	}
	go func() {
		if err := tg.NotifyTask(botToken, chatID, cfg, alert); err != nil {
			log.Printf("[tg] notify: %v", err)
		}
	}()
}

func notifyConfigFrom(cfg tg.NotifyConfig) tg.NotifyConfig {
	if cfg.ErrorPreviewLen <= 0 {
		cfg.ErrorPreviewLen = 800
	}
	if cfg.PromptPreviewLen <= 0 {
		cfg.PromptPreviewLen = 120
	}
	return cfg
}

// finishShellNotify 在 shell 任務結束後依結果推送 TG（成功不通知）。
func finishShellNotify(botToken string, chatID int64, cfg tg.NotifyConfig, sessName, command, shellErr string, exitCode int, interrupted bool, runErr error) {
	if botToken == "" || chatID == 0 {
		return
	}
	if interrupted && shellErr == "" && runErr == nil {
		notifyTaskAsync(botToken, chatID, cfg, tg.TaskAlert{
			SessionName: sessName,
			AgentType:   "shell",
			Outcome:     tg.OutcomeCancelled,
			Prompt:      command,
		})
		return
	}
	failed := shellErr != "" || exitCode != 0 || runErr != nil
	if !failed {
		return
	}
	errMsg := shellErr
	if errMsg == "" && runErr != nil {
		errMsg = runErr.Error()
	}
	if errMsg == "" && exitCode != 0 {
		errMsg = fmt.Sprintf("命令以 exit code %d 結束", exitCode)
	}
	var codePtr *int
	if exitCode != 0 {
		c := exitCode
		codePtr = &c
	}
	notifyTaskAsync(botToken, chatID, cfg, tg.TaskAlert{
		SessionName: sessName,
		AgentType:   "shell",
		Outcome:     tg.OutcomeError,
		Prompt:      command,
		Error:       errMsg,
		ExitCode:    codePtr,
	})
}
