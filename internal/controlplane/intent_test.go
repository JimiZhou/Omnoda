package controlplane

import "testing"

func TestClassifySeedDoesNotDefaultToSystemInfo(t *testing.T) {
	cases := []struct {
		goal string
		kind string
		cap  string
	}{
		{"report this computer's system info", "builtin", "omnoda.system-info"},
		{"查询本机系统信息", "builtin", "omnoda.system-info"},
		{"run omnoda.weather for tomorrow", "missing", "omnoda.weather"},
		{"please plan a refactor", "model", ""},
		{"delete the workspace logs", "approval", ""},
		{"sing a song about routers", "unsupported", ""},
	}
	for _, tc := range cases {
		got := classifySeed(tc.goal)
		if got.Kind != tc.kind || got.Capability != tc.cap {
			t.Fatalf("%q -> %+v", tc.goal, got)
		}
	}
}
