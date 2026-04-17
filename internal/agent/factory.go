package agent

import "fmt"

// AgentType 常數對應 sessions.agent_type 欄位允許的值。
const (
	TypeClaude = "claude"
	TypeCursor = "cursor"
	TypeCodex  = "codex"
	TypeGemini = "gemini"
)

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
	if agentType == "" {
		agentType = TypeClaude
	}
	b, ok := registry[agentType]
	if !ok {
		return nil, fmt.Errorf("unknown agent type: %q", agentType)
	}
	return b(), nil
}

// IsRegistered 檢查某個 agent type 是否已註冊（有對應實作）。
func IsRegistered(agentType string) bool {
	_, ok := registry[agentType]
	return ok
}
