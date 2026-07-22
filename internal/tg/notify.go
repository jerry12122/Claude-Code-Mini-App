package tg

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
)

// Notify 透過 Bot API 傳送 Markdown 訊息給指定使用者。
func Notify(botToken string, chatID int64, text string) error {
	if botToken == "" || chatID == 0 {
		return nil
	}
	if len(text) > 4096 {
		text = text[:4090] + "…"
	}
	apiURL := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", botToken)
	resp, err := http.PostForm(apiURL, url.Values{
		"chat_id":    {strconv.FormatInt(chatID, 10)},
		"text":       {text},
		"parse_mode": {"Markdown"},
	})
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("telegram sendMessage HTTP %d: %s", resp.StatusCode, string(body))
	}
	return nil
}
