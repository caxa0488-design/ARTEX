package agent

import (
	"context"
	"errors"
	"testing"

	actool "github.com/Autumn-27/norma/tool"
)

func TestAssemblyIntegrityStopsBeforeResolutionAndProvider(t *testing.T) {
	old, resolve := ToolAugment(), ToolResolve()
	t.Cleanup(func() { SetToolAugment(old); SetToolResolve(resolve) })
	sentinel := errors.New("synthetic managed integrity failure")
	SetToolAugment(func(context.Context, string) ([]actool.CoreTool, DeferredInfo, func()) {
		return nil, DeferredInfo{AssemblyError: sentinel}, func() {}
	})
	SetToolResolve(func(context.Context, string, []actool.CoreTool) []actool.CoreTool {
		t.Fatal("tool resolution ran after mandatory integrity failure")
		return nil
	})
	tools, def, cleanup := AugmentTools(t.Context(), "planner", nil)
	defer cleanup()
	if !errors.Is(def.AssemblyError, sentinel) || len(tools) != 0 {
		t.Fatal("integrity failure ignored")
	}
	// Nil provider would fail if reached. No model or network call is possible.
	chat := NewChatAgent(nil, "", t.TempDir(), nil, 0)
	if _, err := chat.Chat(t.Context(), "planner", "synthetic-session", "Only a local guard test", 1, 0, false, nil); !errors.Is(err, sentinel) {
		t.Fatalf("chat reached provider or swallowed integrity failure: %v", err)
	}
}
