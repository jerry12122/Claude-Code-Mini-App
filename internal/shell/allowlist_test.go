package shell

import "testing"

func TestCommandAllowed_EmptyAllowlist(t *testing.T) {
	if !CommandAllowed("rm -rf /", nil) {
		t.Fatal("empty allowlist should allow")
	}
	if !CommandAllowed("rm -rf /", []string{}) {
		t.Fatal("empty slice should allow")
	}
}

func TestCommandAllowed_FirstToken(t *testing.T) {
	list := []string{"git", "ls"}
	if !CommandAllowed("git status", list) {
		t.Fatal("git should match")
	}
	if !CommandAllowed("GIT status", list) {
		t.Fatal("case insensitive")
	}
	if !CommandAllowed("ls -la", list) {
		t.Fatal("ls should match")
	}
	if CommandAllowed("rm -f", list) {
		t.Fatal("rm should not match")
	}
}

func TestCommandAllowed_ChainingBlocked(t *testing.T) {
	list := []string{"git"}
	if CommandAllowed("git status && rm -rf /", list) {
		t.Fatal("&& should be blocked")
	}
	if CommandAllowed("git status | xargs rm", list) {
		t.Fatal("pipe should be blocked")
	}
}

func TestEffectiveAllowlist_Dedupe(t *testing.T) {
	got := EffectiveAllowlist([]string{"git", "GIT"}, []string{"git", "ls"})
	if len(got) != 2 {
		t.Fatalf("want 2 unique, got %v", got)
	}
}

func TestClassifyShellLine(t *testing.T) {
	run, need, err := ClassifyShellLine("git status", []string{"git"})
	if err != nil || !run || need {
		t.Fatalf("git in list: run=%v need=%v err=%v", run, need, err)
	}
	run, need, err = ClassifyShellLine("docker ps", []string{"git"})
	if err != nil || run || !need {
		t.Fatalf("docker not in list: run=%v need=%v err=%v", run, need, err)
	}
	run, need, err = ClassifyShellLine("docker ps", nil)
	if err != nil || run || !need {
		t.Fatalf("empty effective should require confirm: run=%v need=%v err=%v", run, need, err)
	}
	_, _, err = ClassifyShellLine("git status && rm -rf /", nil)
	if err == nil {
		t.Fatal("chaining should error even when effective empty")
	}
}
