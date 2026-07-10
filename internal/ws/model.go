package ws

import (
	"time"

	"github.com/jerry12122/Claude-Code-Mini-App/internal/agent"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/db"
	"github.com/jerry12122/Claude-Code-Mini-App/internal/model"
)

func sessionModelPayload(sess *db.Session) *model.Payload {
	if sess == nil {
		return nil
	}
	agentType := sess.AgentType
	if agentType == "" {
		agentType = agent.TypeClaude
	}
	if agentType == agent.TypeAntigravity || agentType == agent.TypeGemini {
		return nil
	}
	info := model.ResolveForSession(agentType, sess.CliExtraArgs, sess.ActiveModel, sess.ActiveModelSource)
	if !info.Ok && info.DisplayText == "—" {
		return nil
	}
	p := info.ToPayload()
	if sess.ActiveModelAt != "" {
		p.UpdatedAt = sess.ActiveModelAt
	}
	return &p
}

func persistModelUpdate(database *db.DB, sessionID string, snap *agent.ModelSnapshot) model.Payload {
	if snap == nil || snap.DisplayText == "" || snap.DisplayText == "—" {
		return model.Payload{}
	}
	modelName := snap.Model
	if modelName == "" {
		modelName = snap.DisplayText
	}
	_ = database.UpdateSessionActiveModel(sessionID, modelName, snap.Source)
	return model.Payload{
		DisplayText: snap.DisplayText,
		Source:      snap.Source,
		UpdatedAt:   time.Now().UTC().Format(time.RFC3339),
	}
}

func persistInfoUpdate(database *db.DB, sessionID string, info model.Info) *model.Payload {
	if !info.Ok || info.DisplayText == "" || info.DisplayText == "—" {
		return nil
	}
	modelName := info.Model
	if modelName == "" {
		modelName = info.DisplayText
	}
	_ = database.UpdateSessionActiveModel(sessionID, modelName, string(info.Source))
	p := info.ToPayloadAt(time.Now())
	return &p
}
