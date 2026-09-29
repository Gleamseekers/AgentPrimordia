//go:build integration
// +build integration

package ap_test

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	ap "github.com/Gleamseekers/AgentPrimordia/agentprimordia/pkg"
)

// ===== Phase 17-D: pkg/ 公共 API 端到端集成测试 =====
//
// 这些测试是用户视角的「Hello World 真实跑通」验证。
// 如果这里任何一个失败，意味着 pkg/example_test.go 里的 Example 与真实 API 行为不一致。
// 必须设置真实 API Key 才能运行（缺 Key 自动 Skip）。

// getOpenAIKey 返回真实 LLM API Key；未设置时跳过测试。
func getOpenAIKey(t *testing.T) string {
	t.Helper()
	key := os.Getenv("OPENAI_API_KEY")
	if key == "" {
		t.Skip("OPENAI_API_KEY not set, skipping pkg public API integration test")
	}
	return key
}

// openAIConfigFromEnv 构建 Provider 配置，支持第三方 OpenAI 兼容网关：
//   - OPENAI_BASE_URL：自定义 Base URL（默认 https://api.openai.com/v1）
//   - OPENAI_MODEL：自定义模型名（默认 gpt-4o-mini）
//
// v6.0.1：真实 LLM 复测发现硬编码官方端点/模型导致兼容网关无法运行集成测试。
func openAIConfigFromEnv(t *testing.T) ap.Config {
	t.Helper()
	cfg := ap.Config{
		APIKey:  getOpenAIKey(t),
		Model:   "gpt-4o-mini",
		BaseURL: os.Getenv("OPENAI_BASE_URL"),
	}
	if m := os.Getenv("OPENAI_MODEL"); m != "" {
		cfg.Model = m
	}
	return cfg
}

// TestIntegration_NewAgent_Run 验证 ap.NewAgent + ap.NewOpenAIProvider 端到端跑通
func TestIntegration_NewAgent_Run(t *testing.T) {
	t.Parallel()

	provider, err := ap.NewOpenAIProvider(openAIConfigFromEnv(t))
	if err != nil {
		t.Fatalf("NewOpenAIProvider() error = %v", err)
	}

	agent, err := ap.NewAgent("integration-test", "You are a helpful assistant. Follow instructions exactly.",
		provider,
		ap.WithMaxTurns(3),
		ap.WithTemperature(0),
	)
	if err != nil {
		t.Fatalf("NewAgent error: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	resp, err := agent.Run(ctx, ap.UserMessage("Say 'hello from ap' and nothing else."))
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if resp.Content == "" {
		t.Error("expected non-empty response content")
	}

	t.Logf("Response: %s", resp.Content)
	t.Logf("Metrics: turns=%d tools=%d duration=%v",
		resp.Metrics.TotalTurns, resp.Metrics.TotalTools, resp.Metrics.Duration)
}

// TestIntegration_NewAgent_Stream 验证 agent.StreamRun 流式输出
func TestIntegration_NewAgent_Stream(t *testing.T) {
	t.Parallel()

	provider, err := ap.NewOpenAIProvider(openAIConfigFromEnv(t))
	if err != nil {
		t.Fatalf("NewOpenAIProvider() error = %v", err)
	}

	agent, err := ap.NewAgent("stream-test", "You are a helpful assistant.",
		provider,
		ap.WithMaxTurns(2),
		ap.WithTemperature(0),
	)
	if err != nil {
		t.Fatalf("NewAgent error: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	ch, err := agent.StreamRun(ctx, ap.UserMessage("Count from 1 to 3."))
	if err != nil {
		t.Fatalf("StreamRun() error = %v", err)
	}

	var fullContent string
	eventCount := 0
	for event := range ch {
		eventCount++
		if event.Type == ap.StreamEventToken {
			fullContent += event.Content
		}
		if event.Type == ap.StreamEventComplete {
			break
		}
	}

	if fullContent == "" {
		t.Error("expected non-empty streamed content")
	}
	if eventCount == 0 {
		t.Error("expected at least 1 event")
	}

	t.Logf("Streamed (%d events): %s", eventCount, fullContent)
}

// TestIntegration_NewAgent_WithMemory 验证多轮对话 + 记忆功能。
// v6.0.1：此前对「第二轮未记住名字」仅 t.Logf 记录，永远绿灯、掩盖了
// 会话历史只写不读的真实缺陷；现改为硬断言。
func TestIntegration_NewAgent_WithMemory(t *testing.T) {
	provider, err := ap.NewOpenAIProvider(openAIConfigFromEnv(t))
	if err != nil {
		t.Fatalf("NewOpenAIProvider() error = %v", err)
	}

	memStore, err := ap.WithInMemory()
	if err != nil {
		t.Fatalf("WithInMemory() error = %v", err)
	}
	defer memStore.Close()

	agent, err := ap.NewAgent("memory-test", "You are a helpful assistant. Remember the user's name when told.",
		provider,
		ap.WithMaxTurns(3),
		ap.WithTemperature(0),
	)
	if err != nil {
		t.Fatalf("NewAgent error: %v", err)
	}
	agent = agent.WithMemory(memStore)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// 第一轮：告诉 Agent 用户名
	resp1, err := agent.Run(ctx, ap.UserMessage("My name is Alice."))
	if err != nil {
		t.Fatalf("first Run() error = %v", err)
	}
	if resp1.Content == "" {
		t.Error("expected non-empty response for first turn")
	}
	t.Logf("Turn 1: %s", resp1.Content)

	// 第二轮：问用户名，验证记忆是否生效（硬断言）
	resp2, err := agent.Run(ctx, ap.UserMessage("What is my name?"))
	if err != nil {
		t.Fatalf("second Run() error = %v", err)
	}
	if resp2.Content == "" {
		t.Error("expected non-empty response for second turn")
	}
	if !contains(resp2.Content, "Alice") {
		t.Errorf("多轮记忆失效：第二轮应答未包含 Alice, got: %q", resp2.Content)
	}
	t.Logf("Turn 2: %s", resp2.Content)
}

// TestIntegration_NewSession_Ask 验证 ap.NewSession 多轮对话便利 API
func TestIntegration_NewSession_Ask(t *testing.T) {
	t.Parallel()

	provider, err := ap.NewOpenAIProvider(openAIConfigFromEnv(t))
	if err != nil {
		t.Fatalf("NewOpenAIProvider() error = %v", err)
	}

	agent, err := ap.NewAgent("session-test", "You are a helpful assistant. Always answer in one short sentence.",
		provider,
		ap.WithMaxTurns(2),
		ap.WithTemperature(0),
	)
	if err != nil {
		t.Fatalf("NewAgent error: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// 不传 mem，使用 agent 内部默认
	sess := ap.NewSession(agent, nil)

	resp1, err := sess.Ask(ctx, "Say hello.")
	if err != nil {
		t.Fatalf("first Ask() error = %v", err)
	}
	if resp1.Content == "" {
		t.Error("expected non-empty response for first Ask")
	}
	t.Logf("Ask 1: %s", resp1.Content)

	// 验证 TurnCount 递增
	if got := sess.TurnCount(); got < 1 {
		t.Errorf("TurnCount = %d, want >= 1", got)
	}
}

// ===== helpers =====

// contains 是 strings.Contains 的便利封装，避免引入 strings 包增加测试体积
func contains(haystack, needle string) bool {
	if needle == "" {
		return true
	}
	if len(needle) > len(haystack) {
		return false
	}
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}

// 确保 encoding/json 被引用（部分工具链会检查）
var _ = json.Marshal
