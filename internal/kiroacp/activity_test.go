package kiroacp

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/jerry12122/Claude-Code-Mini-App/internal/agent"
)

func TestSessionActivity(t *testing.T) {
	var st sessionActivity
	for _, tc := range []struct {
		json string
		kind agent.EventType
		text string
	}{
		{`{"sessionUpdate":"agent_thought_chunk","content":{"type":"text","text":"先讀"}}`, agent.EventThinking, "先讀"},
		{`{"sessionUpdate":"agent_thought_chunk","content":"檔案"}`, agent.EventThinking, "先讀檔案"},
		{`{"sessionUpdate":"tool_call","toolCallId":"a","title":"Bash","status":"pending","rawInput":{"command":"git status"}}`, agent.EventActivity, "準備工具：Bash：git status"},
		{`{"sessionUpdate":"tool_call_update","toolCallId":"a","status":"in_progress","rawInput":null}`, agent.EventActivity, "執行工具：Bash：git status"},
		{`{"sessionUpdate":"tool_call","toolCallId":"b","kind":"read","locations":[{"path":"a.go"}],"status":"in_progress"}`, agent.EventActivity, "執行工具：read：a.go"},
		{`{"sessionUpdate":"tool_call_update","toolCallId":"a","status":"completed"}`, agent.EventActivity, "工具已完成：Bash：git status"},
		{`{"sessionUpdate":"tool_call_update","toolCallId":"b","status":"failed"}`, agent.EventActivity, "工具失敗：read：a.go"},
		{`{"sessionUpdate":"agent_thought_chunk","text":"確認結果"}`, agent.EventThinking, "確認結果"},
		{`{"sessionUpdate":"agent_message_chunk","text":"回覆"}`, "", ""},
		{`{"sessionUpdate":"agent_thought_chunk","text":"下一段思考"}`, agent.EventThinking, "下一段思考"},
		{`{"sessionUpdate":"plan","entries":[{"content":"讀取檔案","status":"completed"},{"content":"跑測試","status":"in_progress"}]}`, agent.EventActivity, "進行中：跑測試"},
		{`{"sessionUpdate":"tool_call_update","name":"search","rawInput":{"query":"ACP"}}`, agent.EventActivity, "執行工具：search：ACP"},
	} {
		var body sessionUpdateBody
		if err := json.Unmarshal([]byte(tc.json), &body); err != nil {
			t.Fatal(err)
		}
		e := st.update(body)
		if e.Type != tc.kind || e.Text != tc.text {
			t.Fatalf("%s: got %s %q, want %s %q", tc.json, e.Type, e.Text, tc.kind, tc.text)
		}
	}
	if len(st.tools) != 0 {
		t.Fatalf("已完成的工具不得殘留：%v", st.tools)
	}
	st.update(sessionUpdateBody{SessionUpdate: "agent_thought_chunk", Text: strings.Repeat("x", 512*1024)})
	if st.thoughts.Len() > 512*1024 {
		t.Fatal("思考快照超出上限")
	}
}

func TestActivityStreamThroughRunner(t *testing.T) {
	r := runFake(t, "activity")
	if r.err != nil {
		t.Fatal(r.err)
	}
	var thoughts, activities, reply []string
	for _, event := range r.events {
		switch event.Type {
		case agent.EventThinking:
			thoughts = append(thoughts, event.Text)
		case agent.EventActivity:
			activities = append(activities, event.Text)
		case agent.EventDelta:
			reply = append(reply, event.Text)
		}
	}
	if strings.Join(thoughts, "|") != "先讀|先讀檔案" || strings.Join(activities, "|") != "執行工具：Bash：git status|工具已完成：Bash：git status" || strings.Join(reply, "") != "hi" {
		t.Fatalf("thoughts=%v activities=%v reply=%v", thoughts, activities, reply)
	}
	if r.attempts != 1 || r.count(agent.EventDone) != 1 {
		t.Fatalf("attempts=%d done=%d", r.attempts, r.count(agent.EventDone))
	}
}
