package ws

import (
	"errors"
	"strings"

	"github.com/jerry12122/Claude-Code-Mini-App/internal/agent"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/db"
)

var (
	ErrInstructionConflict = errors.New("原生指令與 CLI system prompt 參數請擇一使用")
	ErrInstructionTooLong  = errors.New("全域與專屬指令合併後最多 16384 UTF-8 bytes")
)

// CreateSession saves the current global instruction alongside the session's own instruction.
func CreateSession(database *db.DB, session db.Session) (*db.Session, error) {
	if session.AgentType == "" {
		session.AgentType = agent.TypeClaude
	}
	if session.AgentType != agent.TypeTelegram && session.InputMode != "shell" {
		global, err := database.GetGlobalInstruction()
		if err != nil {
			return nil, err
		}
		session.GlobalInstruction = global
	}
	session.SystemInstruction = strings.TrimSpace(session.SystemInstruction)
	if session.GlobalInstruction.Mode == db.GlobalInstructionNative && session.GlobalInstruction.Text != "" &&
		len(nativeSystemInstruction(&session)) > db.MaxSystemInstructionBytes {
		return nil, ErrInstructionTooLong
	}
	if err := ValidateInstructionArgs(&session, session.CliExtraArgs); err != nil {
		return nil, err
	}
	return database.CreateSessionWithInstructions(session.Name, session.Description, session.WorkDir,
		session.PermissionMode, session.AgentType, session.CliExtraArgs, session.InputMode,
		session.DisguiseAgent, session.SystemInstruction, session.GlobalInstruction)
}

func nativeSystemInstruction(s *db.Session) string {
	g := s.GlobalInstruction
	if g.Mode != db.GlobalInstructionNative || g.Text == "" || !agent.SupportsSystemInstruction(s.AgentType) {
		return s.SystemInstruction
	}
	if s.SystemInstruction == "" {
		return g.Text
	}
	return g.Text + "\n\n" + s.SystemInstruction
}

func ValidateInstructionArgs(s *db.Session, args []string) error {
	if s.AgentType != agent.TypeClaude || nativeSystemInstruction(s) == "" {
		return nil
	}
	for _, arg := range args {
		flag, _, _ := strings.Cut(arg, "=")
		switch flag {
		case "--system-prompt", "--system-prompt-file", "--append-system-prompt", "--append-system-prompt-file":
			return ErrInstructionConflict
		}
	}
	return nil
}
