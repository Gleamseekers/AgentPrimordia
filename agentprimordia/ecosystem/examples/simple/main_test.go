package main

import (
	"context"
	"testing"
	"time"

	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/cmd/example/demo"
	ap "github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/pkg"
)

// TestSimpleExample 验证最简示例的核心逻辑不会腐烂
func TestSimpleExample(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	demoLLM := demo.NewDemoLLM("你好!我是AI助手!")
	a, err := ap.NewAgent("TestBot", "你是一个友好的AI助手。", demoLLM, ap.WithMaxTurns(3))
	if err != nil {
		t.Fatalf("NewAgent error: %v", err)
	}

	resp, err := a.Run(ctx, ap.UserMessage("你好"))
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}
	if resp.Content == "" {
		t.Error("expected non-empty response")
	}

	stats := a.Stats()
	if stats.Status == "" {
		t.Error("expected non-empty status")
	}
}
