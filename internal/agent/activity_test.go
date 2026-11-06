package agent

import (
	"encoding/json"
	"testing"
)

func TestToolActivity(t *testing.T) {
	for _, tc := range []struct{ args, want string }{
		{`{"command":"go test ./..."}`, "Bash：go test ./..."},
		{`"{\"path\":\"README.md\"}"`, "Bash：README.md"},
		{`{"AbsolutePath":"a.go","content":"private content"}`, "Bash：a.go"},
		{`{"command":5,"path":"a.go"}`, "Bash：a.go"},
		{`{"content":"private content"}`, "Bash"},
		{`[]`, "Bash"},
		{`broken`, "Bash"},
	} {
		if got := ToolActivity("Bash", json.RawMessage(tc.args)); got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.args, got, tc.want)
		}
	}
}
