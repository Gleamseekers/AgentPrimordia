package main

import (
	"context"
	"fmt"
	"log"

	emailplugin "github.com/Gleamseekers/AgentPrimordia/agentprimordia/ecosystem/plugins/email"
	jsonplugin "github.com/Gleamseekers/AgentPrimordia/agentprimordia/ecosystem/plugins/json"
	kvplugin "github.com/Gleamseekers/AgentPrimordia/agentprimordia/ecosystem/plugins/kv"
	ap "github.com/Gleamseekers/AgentPrimordia/agentprimordia/pkg"
)

// MockLLM 是示例用的模拟 LLM
type MockLLM struct{}

func (m *MockLLM) Complete(ctx context.Context, req *ap.CompletionRequest) (*ap.CompletionResponse, error) {
	return &ap.CompletionResponse{
		ID: "mock-1", Content: "我已经处理了数据并发送了通知邮件。",
		Role: "assistant", Usage: ap.Usage{PromptTokens: 10, CompletionTokens: 20},
	}, nil
}
func (m *MockLLM) Stream(ctx context.Context, req *ap.CompletionRequest) (<-chan ap.Chunk, error) {
	ch := make(chan ap.Chunk, 1)
	go func() { defer close(ch); ch <- ap.Chunk{Content: "处理中...", Done: true} }()
	return ch, nil
}
func (m *MockLLM) CallTools(ctx context.Context, req *ap.ToolCallRequest) (*ap.ToolCallResponse, error) {
	return &ap.ToolCallResponse{Usage: ap.Usage{}}, nil
}
func (m *MockLLM) Info() ap.ModelInfo {
	return ap.ModelInfo{Name: "mock", Provider: "mock", MaxContext: 4096, SupportsTools: true}
}

func main() {
	fmt.Println("=== 链式 API：插件生态 ===")
	fmt.Println()

	// 创建工具注册表
	registry := ap.NewToolRegistry()

	// 加载官方插件
	loader := ap.NewPluginLoader(registry)

	// JSON 插件 — 无需配置
	jsonPlugin := jsonplugin.New()
	if err := loader.Load(jsonPlugin); err != nil {
		log.Fatalf("加载 JSON 插件失败: %v", err)
	}
	fmt.Println("✓ JSON 插件已加载")

	// KV 插件 — 需要配置数据库路径
	kvPlugin := kvplugin.New()
	if err := loader.LoadWithConfig(kvPlugin, map[string]any{
		"db_path": "test_kv.db",
	}); err != nil {
		log.Fatalf("加载 KV 插件失败: %v", err)
	}
	fmt.Println("✓ KV 插件已加载")

	// Email 插件 — 需要 SMTP 配置
	emailPlugin := emailplugin.New()
	if err := loader.LoadWithConfig(emailPlugin, map[string]any{
		"smtp_host":     "smtp.example.com",
		"smtp_port":     "587",
		"smtp_username": "agent@example.com",
		"smtp_password": "password",
		"from_address":  "agent@example.com",
	}); err != nil {
		log.Fatalf("加载 Email 插件失败: %v", err)
	}
	fmt.Println("✓ Email 插件已加载")

	// 查看已加载插件
	fmt.Println()
	for _, info := range loader.List() {
		fmt.Printf("  插件: %s v%s (%d 个工具)\n", info.Name, info.Version, info.Count)
	}

	// 创建带插件的 Agent
	agent, err := ap.NewAgent("plugin-agent", "你是一个数据处理助手，可以处理 JSON、存储键值对和发送邮件。", &MockLLM{},
		ap.WithMaxTurns(5),
		ap.WithToolkit(registry),
	)
	if err != nil {
		log.Fatalf("创建 Agent 失败: %v", err)
	}

	resp, err := agent.Run(context.Background(), ap.UserMessage("处理数据并发送通知"))
	if err != nil {
		log.Fatalf("运行失败: %v", err)
	}

	fmt.Printf("\n回复: %s\n", resp.Content)
	fmt.Printf("已注册工具: %v\n", registry.List())
}
