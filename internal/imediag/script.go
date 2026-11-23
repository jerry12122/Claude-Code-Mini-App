package imediag

import (
	_ "embed"
	"encoding/json"
)

//go:embed recorder.js
var recorderJS string

// Script 只在桌面導航完成後注入。owner 經 JSON 逸出，空 origin 不安裝。
func Script(owner string) string {
	if owner == "" {
		return ""
	}
	encoded, err := json.Marshal(owner)
	if err != nil {
		return ""
	}
	return "window.__imeDiagOwner=" + string(encoded) + ";\n" + recorderJS + "\nimeDiagAuto(window);\n"
}

// FreezeCall 請頁面立刻複製緩衝。呼叫端要在開啟儲存對話框之前送出。
func FreezeCall() string {
	return "if(window.__imeDiagFreeze)window.__imeDiagFreeze();"
}
