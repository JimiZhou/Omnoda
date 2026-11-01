// omnoda-fake-harness is an explicit test double. It is not Codex, Claude Code, or OpenCode.
package main

import (
	"encoding/json"
	"fmt"
	"os"
)

func main() {
	prompt := ""
	if len(os.Args) > 1 {
		prompt = os.Args[1]
	}
	emit := func(v any) {
		b, _ := json.Marshal(v)
		fmt.Println(string(b))
	}
	emit(map[string]any{"type": "started", "session_id": "fake-session"})
	emit(map[string]any{"type": "delta", "text": "fake-harness received prompt bytes=" + fmt.Sprint(len(prompt))})
	emit(map[string]any{"type": "completed", "session_id": "fake-session"})
}
