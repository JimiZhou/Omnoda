package executor

import (
	"testing"

	"github.com/JimiZhou/Omnoda/internal/capabilities/builtins"
	"github.com/JimiZhou/Omnoda/internal/capabilities/registry"
)

func TestBuiltinAndDisabled(t *testing.T) {
	reg, err := registry.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := builtins.Ensure(reg); err != nil {
		t.Fatal(err)
	}
	ex := Executor{Reg: reg}
	res := ex.Invoke(t.Context(), Request{ID: "omnoda.system-info"})
	if !res.OK || res.Output["os"] == "" {
		t.Fatalf("%+v", res)
	}
	if _, err := reg.SetStatus("omnoda.system-info", "0.1.0", registry.StatusDisabled); err != nil {
		t.Fatal(err)
	}
	res = ex.Invoke(t.Context(), Request{ID: "omnoda.system-info"})
	if res.OK || res.Error == "" {
		t.Fatalf("disabled call succeeded: %+v", res)
	}
	res = ex.Invoke(t.Context(), Request{ID: "missing.thing"})
	if res.ExitCode != 127 {
		t.Fatal(res)
	}
}

func TestBadInputRejected(t *testing.T) {
	raw := []byte(`{"type":"object","properties":{"input_file":{"type":"string"}},"required":["input_file"]}`)
	if err := validateInput(raw, map[string]any{}); err == nil {
		t.Fatal("missing required")
	}
	if err := validateInput(raw, map[string]any{"input_file": 1}); err == nil {
		t.Fatal("wrong type")
	}
	if err := validateInput(raw, map[string]any{"input_file": "a.log"}); err != nil {
		t.Fatal(err)
	}
}
