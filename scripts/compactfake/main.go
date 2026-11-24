// 本機假 claude：只依 COMPACT_FAKE 吐 stream-json，不連線供應商。
package main

import (
	"encoding/json"
	"io"
	"os"
)

func main() {
	prompt, _ := io.ReadAll(os.Stdin)
	if path := os.Getenv("COMPACT_FAKE_LOG"); path != "" {
		_ = os.WriteFile(path, append(append([]byte{}, prompt...), '\n'), 0o644)
	}
	resume := ""
	args := os.Args[1:]
	for i := 0; i < len(args)-1; i++ {
		if args[i] == "--resume" {
			resume = args[i+1]
		}
	}
	write := func(v any) {
		b, _ := json.Marshal(v)
		_, _ = os.Stdout.Write(append(b, '\n'))
	}
	mode := os.Getenv("COMPACT_FAKE")
	if mode == "" {
		mode = "success"
	}
	if mode == "success" || mode == "exit" {
		write(map[string]string{"type": "system", "subtype": "compact_boundary", "session_id": resume})
	}
	write(map[string]any{
		"type": "result", "subtype": "success", "is_error": false,
		"session_id": resume, "result": "summary secret",
	})
	if mode == "exit" {
		os.Exit(2)
	}
}
