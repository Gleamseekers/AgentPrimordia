//go:build integration

// integration_mock_test.go — 无 API Key 可执行的集成测试（MockLLM 驱动）
//
// 与 integration_test.go（真实 OpenAI 调用，缺 key 时 Skip）互补：
// 本文件不依赖任何外部服务，始终执行完整链路（ReAct 循环 + 工具执行
// + 流式 + 记忆），作为 CI 无 key 环境下的集成保障。
//
// 运行：go test -tags=integration -run MockIntegration ./internal/agent/
package agent

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/llm"
	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/memory"
	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/tools"
	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/tools/builtin"
)

// TestMockIntegration_ReActLoopWithTool 完整 ReAct 链路：MockLLM 先要求
// 调用 filesystem 工具（文件不存在 → 错误回传），再输出最终回答。
// 断言：工具真实执行（TotalTools>=1）+ 最终回答正确。
func TestMockIntegration_ReActLoopWithTool(t *testing.T) {
	mock := llm.NewMockLLM(t)
	mock.WithToolResponse([]llm.FunctionCall{{
		ID: "call-1", Name: "filesystem",
		Arguments: `{"action":"read","path":"nonexistent.txt"}`,
	}})
	mock.WithResponse("文件读取失败，已如实报告")

	reg := tools.NewRegistry()
	fs, err := builtin.NewFileSystem(t.TempDir())
	if err != nil {
		t.Fatalf("NewFileSystem: %v", err)
	}
	_ = reg.Register(fs)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	agt, err := NewAgent("mock-integration", "你是测试助手", mock,
		WithMaxTurns(5), WithToolkit(reg))
	if err != nil {
		t.Fatalf("NewAgent: %v", err)
	}

	resp, err := agt.Run(ctx, UserMessage("请读取 nonexistent.txt"))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if resp.Content != "文件读取失败，已如实报告" {
		t.Errorf("最终回答 = %q", resp.Content)
	}
	if resp.Metrics.TotalTools < 1 {
		t.Errorf("工具应被执行，TotalTools = %d", resp.Metrics.TotalTools)
	}
	t.Logf("✅ ReAct+工具链路：%d 轮 / %d 次工具 / %s", resp.Metrics.TotalTurns, resp.Metrics.TotalTools, resp.Content)
}

// TestMockIntegration_StreamRun 流式模式：验证事件流完整（含完成事件）。
func TestMockIntegration_StreamRun(t *testing.T) {
	mock := llm.NewMockLLM(t)
	mock.WithResponse("流式输出内容")

	agt, err := NewAgent("mock-stream", "你是测试助手", mock, WithMaxTurns(3))
	if err != nil {
		t.Fatalf("NewAgent: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	ch, err := agt.StreamRun(ctx, UserMessage("请回复"))
	if err != nil {
		t.Fatalf("StreamRun: %v", err)
	}

	gotComplete := false
	var gotContent string
	for ev := range ch {
		switch ev.Type {
		case StreamEventComplete:
			gotComplete = true
			gotContent = ev.Content
		case StreamEventError:
			t.Fatalf("流式错误事件: %s", ev.Content)
		}
	}
	if !gotComplete {
		t.Fatal("未收到完成事件")
	}
	if !strings.Contains(gotContent, "流式输出内容") {
		t.Errorf("完成事件内容 = %q", gotContent)
	}
	t.Logf("✅ 流式链路：完成事件内容=%q", gotContent)
}

// TestMockIntegration_MemoryPersistence 记忆链路：Run 后 Episode 应写入存储。
func TestMockIntegration_MemoryPersistence(t *testing.T) {
	mock := llm.NewMockLLM(t)
	mock.WithResponse("记住了")

	mem := memory.NewInMemoryStore()
	agt, err := NewAgent("mock-memory", "你是测试助手", mock,
		WithMaxTurns(2), WithMemory(mem))
	if err != nil {
		t.Fatalf("NewAgent: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if _, err := agt.Run(ctx, UserMessage("请记住这条消息")); err != nil {
		t.Fatalf("Run: %v", err)
	}

	// 记忆应已写入（用户消息 + 助手回复）。
	// 注：倒排索引按 Unicode 字母整段分词（中文不按词切分），
	// 子串查询不命中——用空 query 获取全部 Episode 验证写入。
	search, err := mem.Search(ctx, "", nil)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(search) == 0 {
		t.Fatal("记忆存储中应存在 Episode")
	}
	t.Logf("✅ 记忆链路：命中 %d 条 Episode", len(search))
}
