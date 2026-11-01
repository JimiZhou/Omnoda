package planner

import "testing"

func TestTemplateCrossDemo(t *testing.T) {
	p := Template("检查 ARM 服务器。如果异常，让 Mac 修复，然后 Windows 测试。", nil)
	if p.Planner != "template" {
		t.Fatal(p.Planner)
	}
	if len(p.Steps) != 4 {
		t.Fatalf("steps %d", len(p.Steps))
	}
	if err := Validate(p); err != nil {
		t.Fatal(err)
	}
}

func TestRejectPrivilegedPlan(t *testing.T) {
	p := Template("hello", nil)
	p.Steps[0].Level = "PRIVILEGED"
	if err := Validate(p); err == nil {
		t.Fatal("privileged plan accepted")
	}
}
