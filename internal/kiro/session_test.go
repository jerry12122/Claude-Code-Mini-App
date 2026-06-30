package kiro

import "testing"

func TestParseListSessions(t *testing.T) {
	raw := `
Chat sessions for C:\mydir:

Chat SessionId: 02a96778-f93a-45f6-8d8d-53548d42f700
  7 minutes ago | what git branch am I on? only answer the branch name | 4 msgs | classic

Chat SessionId: 96447d99-3780-41bf-99e1-28c924166e0f
  9 minutes ago | say hello in one word | 2 msgs | classic

To delete a session, use: kiro-cli chat --delete-session <SESSION_ID>
`
	sessions := parseListSessions(raw)
	if len(sessions) != 2 {
		t.Fatalf("expected 2 sessions, got %d", len(sessions))
	}
	if sessions[0].ID != "02a96778-f93a-45f6-8d8d-53548d42f700" {
		t.Errorf("session[0].ID = %q", sessions[0].ID)
	}
	if sessions[0].Title != "what git branch am I on? only answer the branch name" {
		t.Errorf("session[0].Title = %q", sessions[0].Title)
	}
}

func TestResolveNewSessionID_SingleNew(t *testing.T) {
	before := []kiroSession{{ID: "aaa", Title: "old"}}
	after := []kiroSession{
		{ID: "bbb", Title: "new prompt"},
		{ID: "aaa", Title: "old"},
	}
	got := resolveNewSessionID(before, after, "new prompt")
	if got != "bbb" {
		t.Errorf("got %q, want bbb", got)
	}
}

func TestResolveNewSessionID_MultipleNewMatchPrompt(t *testing.T) {
	before := []kiroSession{}
	after := []kiroSession{
		{ID: "id1", Title: "say hello in one word"},
		{ID: "id2", Title: "what git branch am I on? only answer the branch name"},
	}
	got := resolveNewSessionID(before, after, "what git branch am I on? only answer the branch name")
	if got != "id2" {
		t.Errorf("got %q, want id2", got)
	}
}

func TestResolveNewSessionID_NoNewMatchByPrompt(t *testing.T) {
	before := []kiroSession{{ID: "same", Title: "hello world"}}
	after := []kiroSession{{ID: "same", Title: "hello world"}}
	got := resolveNewSessionID(before, after, "hello world")
	if got != "same" {
		t.Errorf("got %q, want same", got)
	}
}

func TestPromptMatchesTitle_Truncated(t *testing.T) {
	long := "what git branch am I on? only answer the branch name please"
	short := "what git branch am I on? only answer the branch name"
	if !promptMatchesTitle(long, short) {
		t.Error("expected truncated title to match")
	}
}

func TestResolveNewSessionID_AmbiguousReturnsEmpty(t *testing.T) {
	before := []kiroSession{}
	after := []kiroSession{
		{ID: "id1", Title: "unrelated one"},
		{ID: "id2", Title: "unrelated two"},
	}
	got := resolveNewSessionID(before, after, "completely different prompt")
	if got != "id1" {
		// fallback to first new when prompt doesn't match
		t.Errorf("got %q, want id1 (first new fallback)", got)
	}
}
