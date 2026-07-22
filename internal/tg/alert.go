package tg

import (
	"fmt"
	"strings"
)

// Outcome 代表任務結束狀態（用於決定是否／如何通知）。
type Outcome string

const (
	OutcomeSuccess   Outcome = "success"
	OutcomeError     Outcome = "error"
	OutcomeConfirm   Outcome = "confirm"
	OutcomeCancelled Outcome = "cancelled"
)

// NotifyConfig 控制 TG 任務通知行為（來自 config.yaml notify 區塊）。
type NotifyConfig struct {
	OnError          bool
	OnCancel         bool
	OnShellError     bool
	ErrorPreviewLen  int
	IncludePrompt    bool
	PromptPreviewLen int
}

// DefaultNotifyConfig 回傳與 config 預設值一致的設定。
func DefaultNotifyConfig() NotifyConfig {
	return NotifyConfig{
		OnError:          true,
		OnCancel:         false,
		OnShellError:     true,
		ErrorPreviewLen:  800,
		IncludePrompt:    true,
		PromptPreviewLen: 120,
	}
}

// TaskAlert 是單次任務結束時的通知承載。
type TaskAlert struct {
	SessionName string
	AgentType   string // claude / cursor / shell …
	Outcome     Outcome
	Prompt      string
	Error       string
	ExitCode    *int
}

// FormatTaskAlert 將 TaskAlert 格式化為 Telegram Markdown 訊息。
func FormatTaskAlert(alert TaskAlert, cfg NotifyConfig) string {
	name := escapeMarkdown(strings.TrimSpace(alert.SessionName))
	if name == "" {
		name = "Session"
	}
	kind := strings.TrimSpace(alert.AgentType)
	if kind == "" {
		kind = "agent"
	}

	var b strings.Builder
	switch alert.Outcome {
	case OutcomeSuccess:
		fmt.Fprintf(&b, "✅ *%s* 任務完成", name)
	case OutcomeConfirm:
		fmt.Fprintf(&b, "⚠️ *%s* 需要授權確認，請開啟 App", name)
	case OutcomeCancelled:
		fmt.Fprintf(&b, "⏹ *%s* 任務已中斷", name)
	case OutcomeError:
		fmt.Fprintf(&b, "❌ *%s* 任務失敗\n\n", name)
		fmt.Fprintf(&b, "類型：%s\n", escapeMarkdown(kind))
		if cfg.IncludePrompt {
			if p := truncatePreview(alert.Prompt, cfg.PromptPreviewLen); p != "" {
				fmt.Fprintf(&b, "指令：%s\n", escapeMarkdown(p))
			}
		}
		if alert.ExitCode != nil {
			fmt.Fprintf(&b, "Exit code：%d\n", *alert.ExitCode)
		}
		if errText := truncatePreview(alert.Error, cfg.ErrorPreviewLen); errText != "" {
			fmt.Fprintf(&b, "錯誤：%s\n", escapeMarkdown(errText))
		}
		b.WriteString("\n請開啟 App 查看詳情")
	default:
		return ""
	}
	return b.String()
}

// NotifyTask 依 outcome 與設定決定是否推送 TG 訊息。
func NotifyTask(botToken string, chatID int64, cfg NotifyConfig, alert TaskAlert) error {
	if botToken == "" || chatID == 0 {
		return nil
	}
	switch alert.Outcome {
	case OutcomeSuccess, OutcomeConfirm:
		// 永遠通知
	case OutcomeError:
		if alert.AgentType == "shell" {
			if !cfg.OnShellError {
				return nil
			}
		} else if !cfg.OnError {
			return nil
		}
	case OutcomeCancelled:
		if !cfg.OnCancel {
			return nil
		}
	default:
		return nil
	}
	text := FormatTaskAlert(alert, cfg)
	if text == "" {
		return nil
	}
	return Notify(botToken, chatID, text)
}

func truncatePreview(s string, max int) string {
	s = strings.TrimSpace(s)
	if max <= 0 || len(s) <= max {
		return s
	}
	return s[:max] + "…"
}

func escapeMarkdown(s string) string {
	return strings.NewReplacer(
		"_", "\\_",
		"*", "\\*",
		"`", "\\`",
		"[", "\\[",
	).Replace(s)
}
