package controlplane

import (
	"strings"
	"unicode"
)

// SeedIntent is a deterministic classification of a Seed goal.
// It is not a planner and it does not call a model.
type SeedIntent struct {
	Kind       string // builtin | missing | model | approval | unsupported
	Capability string
	Reason     string
	SideEffect bool
}

func classifySeed(goal string) SeedIntent {
	text := strings.TrimSpace(goal)
	folded := fold(text)
	switch {
	case folded == "":
		return SeedIntent{Kind: "unsupported", Reason: "empty goal"}
	case wantsSystemInfo(folded):
		return SeedIntent{Kind: "builtin", Capability: "omnoda.system-info"}
	case mentionsCapability(folded, "omnoda.weather") || mentionsCapability(folded, "weather"):
		return SeedIntent{Kind: "missing", Capability: "omnoda.weather", Reason: "capability omnoda.weather is not installed"}
	case needsModel(folded):
		return SeedIntent{Kind: "model", Reason: "this goal needs a configured model provider"}
	case needsApproval(folded):
		return SeedIntent{Kind: "approval", Reason: "this goal changes the machine and needs approval", SideEffect: true}
	default:
		return SeedIntent{Kind: "unsupported", Reason: "seed cannot map this goal to an installed capability"}
	}
}

func wantsSystemInfo(folded string) bool {
	if strings.Contains(folded, "system info") || strings.Contains(folded, "system information") {
		return true
	}
	if strings.Contains(folded, "系统信息") || strings.Contains(folded, "本机信息") {
		return true
	}
	if (strings.Contains(folded, "os") || strings.Contains(folded, "arch") || strings.Contains(folded, "architecture") || strings.Contains(folded, "架构") || strings.Contains(folded, "操作系统")) &&
		(strings.Contains(folded, "this computer") || strings.Contains(folded, "this machine") || strings.Contains(folded, "local") || strings.Contains(folded, "本机") || strings.Contains(folded, "what") || strings.Contains(folded, "report") || strings.Contains(folded, "查询") || strings.Contains(folded, "查看")) {
		return true
	}
	return false
}

func mentionsCapability(folded, name string) bool {
	return strings.Contains(folded, name)
}

func needsModel(folded string) bool {
	for _, word := range []string{"plan", "规划", "explain", "解释", "summarize", "总结", "write code", "写代码", "refactor", "重构"} {
		if strings.Contains(folded, word) {
			return true
		}
	}
	return false
}

func needsApproval(folded string) bool {
	for _, word := range []string{"delete", "删除", "install", "安装", "reboot", "重启", "format", "格式化", "shutdown", "关机"} {
		if strings.Contains(folded, word) {
			return true
		}
	}
	return false
}

func fold(s string) string {
	return strings.ToLower(strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return ' '
		}
		return r
	}, s))
}
