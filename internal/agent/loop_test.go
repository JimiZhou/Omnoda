package agent

import (
	"context"
	"testing"

	"github.com/JimiZhou/Omnoda/internal/protocol"
)

func TestUnconfiguredErrors(t *testing.T) {
	_, err := Run(context.Background(), Config{}, "hi", ToolContext{}, func(protocol.Event) {})
	if err == nil {
		t.Fatal("expected error")
	}
}
