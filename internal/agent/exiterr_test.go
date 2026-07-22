package agent

import "testing"

func TestNewExitError_prefersDetail(t *testing.T) {
	err := NewExitError("kiro-cli", "Not logged in. Run kiro login", nil)
	got := err.Error()
	if got == "exit status 1" || got == "kiro-cli: exit status 1" {
		t.Fatalf("expected detail in error, got %q", got)
	}
	if want := "kiro-cli: Not logged in. Run kiro login"; got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestNewExitError_fallbackWait(t *testing.T) {
	err := NewExitError("claude", "", fmtWait("exit status 1"))
	got := err.Error()
	if got != "claude: exit status 1" {
		t.Fatalf("got %q", got)
	}
}

type waitErr string

func (e waitErr) Error() string { return string(e) }
func fmtWait(s string) error     { return waitErr(s) }

func TestPreferErrorText(t *testing.T) {
	cases := []struct {
		a, b, want string
	}{
		{"exit status 1", "kiro-cli: Not logged in", "kiro-cli: Not logged in"},
		{"auth failed", "exit status 1", "auth failed"},
		{"", "something", "something"},
	}
	for _, c := range cases {
		if got := PreferErrorText(c.a, c.b); got != c.want {
			t.Errorf("PreferErrorText(%q, %q) = %q, want %q", c.a, c.b, got, c.want)
		}
	}
}
