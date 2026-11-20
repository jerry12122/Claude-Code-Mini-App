package ws

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/google/uuid"
)

type userQuestion struct {
	Question string `json:"question"`
	Header   string `json:"header"`
	Options  []struct {
		Label       string `json:"label"`
		Description string `json:"description"`
		Preview     string `json:"preview,omitempty"`
	} `json:"options"`
	MultiSelect bool `json:"multiSelect"`
}

type questionAnswer struct {
	Selected []string `json:"selected"`
	Text     string   `json:"text"`
}

type pendingQuestion struct {
	operation bool
	ctx       context.Context
	RequestID string         `json:"request_id"`
	Questions []userQuestion `json:"questions"`
	input     map[string]json.RawMessage
	ch        chan json.RawMessage
	resolved  bool
}

var pendingQuestions = struct {
	sync.Mutex
	m map[string]*pendingQuestion
}{m: make(map[string]*pendingQuestion)}

func newQuestion(input json.RawMessage) (*pendingQuestion, error) {
	q := &pendingQuestion{RequestID: uuid.NewString(), ch: make(chan json.RawMessage, 1)}
	if err := json.Unmarshal(input, &q.input); err != nil {
		return nil, fmt.Errorf("無法解析問題")
	}
	if err := json.Unmarshal(q.input["questions"], &q.Questions); err != nil || len(q.Questions) == 0 || len(q.Questions) > 4 {
		return nil, fmt.Errorf("問題數量必須為 1–4 題")
	}
	seen := map[string]bool{}
	for _, item := range q.Questions {
		if strings.TrimSpace(item.Question) == "" || seen[item.Question] || len(item.Options) < 2 || len(item.Options) > 4 {
			return nil, fmt.Errorf("問題或選項格式無效")
		}
		seen[item.Question] = true
		labels := map[string]bool{}
		for _, option := range item.Options {
			if strings.TrimSpace(option.Label) == "" || labels[option.Label] {
				return nil, fmt.Errorf("選項不可空白或重複")
			}
			labels[option.Label] = true
		}
	}
	return q, nil
}

func questionPending(sessionID string) *pendingQuestion {
	pendingQuestions.Lock()
	defer pendingQuestions.Unlock()
	q := pendingQuestions.m[sessionID]
	if q != nil && q.resolved {
		return nil
	}
	return q
}

func questionResolve(sessionID, requestID string, answers []questionAnswer) error {
	pendingQuestions.Lock()
	defer pendingQuestions.Unlock()
	q := pendingQuestions.m[sessionID]
	if q == nil || q.RequestID != requestID || q.resolved || (q.ctx != nil && q.ctx.Err() != nil) {
		return fmt.Errorf("此問題已回答或已失效")
	}
	if len(answers) != len(q.Questions) {
		return fmt.Errorf("請回答每一題")
	}
	values := make(map[string]string, len(answers))
	for i, answer := range answers {
		if q.operation && (len(answer.Selected) != 1 || answer.Text != "") {
			return fmt.Errorf("請由使用者選擇明確的批准或取消按鈕")
		}
		item := q.Questions[i]
		if len(answer.Text) > 16000 {
			return fmt.Errorf("回答過長")
		}
		parts := []string{}
		seen := map[string]bool{}
		for _, label := range answer.Selected {
			found := false
			for _, option := range item.Options {
				if option.Label == label {
					found = true
					break
				}
			}
			if !found || seen[label] {
				return fmt.Errorf("回答含有無效或重複的選項")
			}
			seen[label] = true
			parts = append(parts, label)
		}
		if text := strings.TrimSpace(answer.Text); text != "" {
			parts = append(parts, text)
		}
		if len(parts) == 0 || (!item.MultiSelect && len(parts) != 1) {
			return fmt.Errorf("請依單選／多選規則回答每一題")
		}
		values[item.Question] = strings.Join(parts, ", ")
	}
	// Preserve the CLI's original questions and any extension fields; accept only answers from the browser.
	q.input["answers"], _ = json.Marshal(values)
	updated, err := json.Marshal(q.input)
	if err != nil {
		return err
	}
	q.resolved = true
	q.ch <- updated
	return nil
}

// AskClaudeQuestion shares serialization and cancellation with ordinary permissions, but never asks for authorization.
func AskClaudeQuestion(ctx context.Context, sessionID string, input json.RawMessage, toolUseID string) (json.RawMessage, error) {
	v, ok := claudeAskers.Load(sessionID)
	if !ok {
		return nil, fmt.Errorf("問答通道已結束，請用文字詢問使用者")
	}
	a := v.(*claudeAsker)
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	stop := context.AfterFunc(a.runCtx, cancel)
	defer stop()
	select {
	case a.sem <- struct{}{}:
		defer func() { <-a.sem }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	if a.askQuestion == nil {
		return nil, fmt.Errorf("問答通道未啟用")
	}
	out, err := a.askQuestion(ctx, input)
	a.mu.Lock()
	a.answered[toolUseID] = true
	a.mu.Unlock()
	return out, err
}
