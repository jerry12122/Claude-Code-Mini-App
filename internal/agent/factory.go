package agent

import "fmt"

// AgentType 常數對應 sessions.agent_type 欄位允許的值。
const (
	TypeClaude = "claude"
	TypeCursor = "cursor"
	TypeCodex       = "codex"
	TypeAntigravity = "antigravity"
	TypeKiro        = "kiro"
	// TypeKiroACP 為實驗 type：kiro-cli acp（JSON-RPC），與 TypeKiro（--no-interactive）並存以便對照。
	TypeKiroACP = "kiroacp"
	// TypeGemini 已棄用；與 TypeAntigravity 一併停用（headless 無法使用，見 issue #76）。
	TypeGemini = "gemini"
)

// disabledAgentTypes 為應用內停用的 agent_type（仍可讀取舊 Session，但不可新建或執行）。
var disabledAgentTypes = map[string]string{
	TypeGemini:      "Gemini / Antigravity CLI 已停用（headless 無法使用）",
	TypeAntigravity: "Gemini / Antigravity CLI 已停用（headless 無法使用）",
}

func normalizeAgentType(agentType string) string {
	if agentType == "" {
		return TypeClaude
	}
	if agentType == TypeGemini {
		return TypeAntigravity
	}
	return agentType
}

// IsEnabled 檢查 agent_type 是否可在應用內新建 Session 或執行 runner。
func IsEnabled(agentType string) bool {
	_, disabled := disabledAgentTypes[normalizeAgentType(agentType)]
	return !disabled
}

// DisabledReason 回傳停用原因；未停用時為空字串。
func DisabledReason(agentType string) string {
	return disabledAgentTypes[normalizeAgentType(agentType)]
}

// runnerBuilder 由各工具套件在 init() 時註冊，避免 agent 套件反向依賴 claude/codex 等套件。
type runnerBuilder func() Runner

var registry = map[string]runnerBuilder{}

// Register 讓各工具套件在 init() 註冊自己的 builder。
func Register(name string, build runnerBuilder) {
	registry[name] = build
}

// NewRunner 依 agentType 建立 Runner 實例。
// agentType 為空字串時預設為 "claude"。
func NewRunner(agentType string) (Runner, error) {
	agentType = normalizeAgentType(agentType)
	if reason := disabledAgentTypes[agentType]; reason != "" {
		return nil, fmt.Errorf("%s", reason)
	}
	b, ok := registry[agentType]
	if !ok {
		return nil, fmt.Errorf("unknown agent type: %q", agentType)
	}
	return b(), nil
}

// IsRegistered 檢查某個 agent type 是否已註冊（有對應實作）。
func IsRegistered(agentType string) bool {
	agentType = normalizeAgentType(agentType)
	if !IsEnabled(agentType) {
		return false
	}
	_, ok := registry[agentType]
	return ok
}
