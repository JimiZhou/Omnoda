package integration

import (
	"context"
	"os/exec"
	"testing"

	"github.com/JimiZhou/Omnoda/internal/adapters"
)

func TestDetectRealCLIs(t *testing.T) {
	ctx := context.Background()
	for _, id := range []string{"codex", "claude-code", "opencode"} {
		a, err := adapters.ByID(id)
		if err != nil {
			t.Fatal(err)
		}
		d := a.Detect(ctx)
		t.Logf("%s present=%v version=%q path=%q err=%q", id, d.Info.Present, d.Info.Version, d.Info.Path, d.Info.Error)
		switch id {
		case "codex":
			if _, err := exec.LookPath("codex"); err == nil && !d.Info.Present {
				t.Fatal("codex on PATH but not detected")
			}
		case "claude-code":
			if _, err := exec.LookPath("claude"); err == nil && !d.Info.Present {
				t.Fatal("claude on PATH but not detected")
			}
		case "opencode":
			if _, err := exec.LookPath("opencode"); err == nil && !d.Info.Present {
				t.Fatal("opencode on PATH but not detected")
			}
			if !d.Info.Present {
				t.Log("opencode is not installed in this environment; adapter is implemented, runtime E2E was not run")
			}
		}
	}
}
