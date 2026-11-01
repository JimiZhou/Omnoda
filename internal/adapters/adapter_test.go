package adapters

import "testing"

func TestClaudeNormalize(t *testing.T) {
	et, payload, sid := Normalize("claude-code", `{"type":"assistant","session_id":"s1","message":{"content":[{"type":"text","text":"hello"}]}}`)
	if et != "message.delta" || payload["text"] != "hello" || sid != "s1" {
		t.Fatal(et, payload, sid)
	}
}

func TestRedact(t *testing.T) {
	if redactLine(`token=abc`) == "token=abc" {
		t.Fatal("not redacted")
	}
}
