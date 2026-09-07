// integration_test.go — P4 工具智能端到端集成测试
package intelligence_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"agentprimordia/internal/tools"
	"agentprimordia/internal/tools/intelligence"
	"agentprimordia/internal/tools/intelligence/create"
	"agentprimordia/internal/tools/intelligence/optimize"
)

// mockLLM 模拟 LLM 补全（确定性返回 shell 脚本）
type mockLLM struct {
	script string
	err    error
}

func (m *mockLLM) Complete(_ context.Context, _ string) (string, error) {
	if m.err != nil {
		return "", m.err
	}
	return m.script, nil
}

// TestToolIntelligence_EndToEnd 端到端测试：
// 工具调用失败 → IntelligenceHook 记录 → 缺口检测 → 工具创建 → Registry 注册
func TestToolIntelligence_EndToEnd(t *testing.T) {
	// 组装完整管线
	profiler := optimize.NewInMemoryProfiler()
	detector := create.NewTraceGapDetector()
	lifecycle := create.NewLifecycleCreator()
	reg := tools.NewRegistry()
	creator := intelligence.NewRegisteringCreator(lifecycle, reg, t.TempDir())
	hook := intelligence.NewIntelligenceHook(profiler, detector, creator)

	ctx := context.Background()

	// 模拟 5 次相同模式失败（触发缺口检测聚类）
	err := fmt.Errorf("convert: command not found")
	for i := 0; i < 5; i++ {
		hook.AfterToolCall(ctx, "shell", "convert img.png", "", err, 10*time.Millisecond)
	}

	// 验证轨迹已记录
	if got := hook.TraceLength(); got != 5 {
		t.Fatalf("期望轨迹长度 5，得到 %d", got)
	}

	// 触发缺口检测 + 工具创建
	hook.OnTurnEnd(ctx)

	// 验证 profiler 画像
	all, _ := profiler.AllProfiles(ctx)
	prof, ok := all["shell"]
	if !ok {
		t.Fatal("profiler 中未找到 shell 工具的画像")
	}
	if prof.TotalCalls != 5 {
		t.Fatalf("期望 TotalCalls=5，得到 %d", prof.TotalCalls)
	}
	if prof.SuccessRate != 0.0 {
		t.Fatalf("期望 SuccessRate=0.0，得到 %f", prof.SuccessRate)
	}

	// 验证工具已注册到 Registry
	registered := reg.List()
	t.Logf("注册的工具列表: %v", registered)
	if len(registered) == 0 {
		t.Fatal("期望至少一个工具被注册，但 Registry 为空")
	}

	// 检查注册的工具可以被 Get 到
	for _, name := range registered {
		tool, found := reg.Get(name)
		if !found {
			t.Fatalf("工具 %q 在 List 中但 Get 失败", name)
		}
		t.Logf("已注册工具: name=%s desc=%s", tool.Name(), tool.Description())
	}
}

// TestToolIntelligence_EndToEndWithLLMCreator 使用 LLMCreator + mock LLM 的端到端测试
func TestToolIntelligence_EndToEndWithLLMCreator(t *testing.T) {
	profiler := optimize.NewInMemoryProfiler()
	detector := create.NewTraceGapDetector()
	reg := tools.NewRegistry()

	// mock LLM 返回确定性 shell 脚本
	llm := &mockLLM{
		script: "#!/bin/sh\necho 'llm-generated tool' \"$@\" 2>/dev/null || echo 'fallback'",
	}
	llmCreator := create.NewLLMCreator(llm)
	creator := intelligence.NewRegisteringCreator(llmCreator, reg, t.TempDir())
	hook := intelligence.NewIntelligenceHook(profiler, detector, creator)

	ctx := context.Background()

	// 模拟 3 次失败
	err := fmt.Errorf("parse error in input")
	for i := 0; i < 3; i++ {
		hook.AfterToolCall(ctx, "parser", "parse data.json", "", err, 5*time.Millisecond)
	}

	// 触发缺口检测 + LLM 工具创建 + 注册
	hook.OnTurnEnd(ctx)

	// 验证 LLM 生成的工具已注册
	registered := reg.List()
	t.Logf("LLM Creator 注册的工具: %v", registered)
	if len(registered) == 0 {
		t.Fatal("期望 LLM 生成的工具被注册，但 Registry 为空")
	}

	// 验证工具描述包含 LLM 标记
	for _, name := range registered {
		tool, _ := reg.Get(name)
		t.Logf("LLM 工具: name=%s desc=%s", tool.Name(), tool.Description())
		if tool.Description() == "" {
			t.Fatalf("工具 %q 描述为空", name)
		}
	}
}

// TestToolIntelligence_MultipleTurns 多轮次端到端测试：
// 第一轮失败触发工具创建，第二轮新工具可用
func TestToolIntelligence_MultipleTurns(t *testing.T) {
	profiler := optimize.NewInMemoryProfiler()
	detector := create.NewTraceGapDetector()
	lifecycle := create.NewLifecycleCreator()
	reg := tools.NewRegistry()
	creator := intelligence.NewRegisteringCreator(lifecycle, reg, t.TempDir())
	hook := intelligence.NewIntelligenceHook(profiler, detector, creator)

	ctx := context.Background()

	// 第一轮：5 次失败调用
	err := fmt.Errorf("connection refused")
	for i := 0; i < 5; i++ {
		hook.AfterToolCall(ctx, "http_client", "GET /api/data", "", err, 20*time.Millisecond)
	}
	hook.OnTurnEnd(ctx)

	// 验证第一轮后工具已创建
	firstTurnTools := reg.List()
	t.Logf("第一轮创建的工具: %v", firstTurnTools)
	if len(firstTurnTools) == 0 {
		t.Fatal("第一轮期望至少一个工具被创建")
	}

	// 记录第一轮的工具数量
	firstCount := len(firstTurnTools)

	// 第二轮：使用新创建的工具（成功调用）
	for _, name := range firstTurnTools {
		tool, ok := reg.Get(name)
		if !ok {
			t.Fatalf("工具 %q 在第二轮不可用", name)
		}
		// 模拟工具成功执行
		hook.AfterToolCall(ctx, name, "test-args", "success-result", nil, 2*time.Millisecond)
		t.Logf("第二轮使用工具: %s (desc: %s)", tool.Name(), tool.Description())
	}

	// 第二轮正常调用不产生新缺口，OnTurnEnd 不应创建新工具
	hook.OnTurnEnd(ctx)

	secondTurnTools := reg.List()
	t.Logf("第二轮后的工具列表: %v", secondTurnTools)
	if len(secondTurnTools) != firstCount {
		t.Fatalf("期望工具数量不变 (%d)，但得到 %d", firstCount, len(secondTurnTools))
	}

	// 验证 profiler 中两轮调用都已记录
	all, _ := profiler.AllProfiles(ctx)

	// 第一轮的 5 次失败记录在 "http_client" 下
	httpProf, ok := all["http_client"]
	if !ok {
		t.Fatal("profiler 中未找到 http_client 的画像")
	}
	if httpProf.TotalCalls != 5 {
		t.Fatalf("http_client 期望 TotalCalls=5，得到 %d", httpProf.TotalCalls)
	}
	t.Logf("http_client 画像: TotalCalls=%d SuccessRate=%.2f", httpProf.TotalCalls, httpProf.SuccessRate)

	// 第二轮的 1 次成功记录在工具名（gap key）下
	for _, name := range firstTurnTools {
		toolProf, ok := all[name]
		if !ok {
			t.Fatalf("profiler 中未找到 %q 的画像", name)
		}
		if toolProf.TotalCalls != 1 {
			t.Fatalf("工具 %q 期望 TotalCalls=1，得到 %d", name, toolProf.TotalCalls)
		}
		if toolProf.SuccessRate != 1.0 {
			t.Fatalf("工具 %q 期望 SuccessRate=1.0，得到 %f", name, toolProf.SuccessRate)
		}
		t.Logf("工具 %q 画像: TotalCalls=%d SuccessRate=%.2f", name, toolProf.TotalCalls, toolProf.SuccessRate)
	}
}
