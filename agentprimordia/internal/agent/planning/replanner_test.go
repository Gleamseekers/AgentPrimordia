package planning

import (
	"context"
	"fmt"
	"testing"

	"github.com/Gleamseekers/AgentPrimordia/internal/llm"
)

// mockProvider 模拟 LLM Provider，用于 replanner 测试
type mockProvider struct {
	completeFn func(ctx context.Context, req *llm.CompletionRequest) (*llm.CompletionResponse, error)
}

func (m *mockProvider) Complete(ctx context.Context, req *llm.CompletionRequest) (*llm.CompletionResponse, error) {
	if m.completeFn != nil {
		return m.completeFn(ctx, req)
	}
	return &llm.CompletionResponse{Content: "否，计划正常"}, nil
}

func (m *mockProvider) Stream(ctx context.Context, req *llm.CompletionRequest) (<-chan llm.Chunk, error) {
	return nil, fmt.Errorf("not implemented")
}

func (m *mockProvider) CallTools(ctx context.Context, req *llm.ToolCallRequest) (*llm.ToolCallResponse, error) {
	return nil, fmt.Errorf("not implemented")
}

func (m *mockProvider) Info() llm.ModelInfo {
	return llm.ModelInfo{Name: "mock", Provider: "test"}
}

// TestShouldReplan_Yes 测试 LLM 返回"是"时需要重规划
func TestShouldReplan_Yes(t *testing.T) {
	prov := &mockProvider{
		completeFn: func(_ context.Context, _ *llm.CompletionRequest) (*llm.CompletionResponse, error) {
			return &llm.CompletionResponse{Content: "是，子任务依赖关系冲突"}, nil
		},
	}
	replanner := NewLLMReplanner(prov)
	plan := &Plan{
		Goal: "测试目标",
		SubTasks: []SubTask{
			{ID: "s1", Description: "第一步", Status: TaskFailed},
		},
	}

	needReplan, reason := replanner.ShouldReplan(context.Background(), plan, "执行失败")
	if !needReplan {
		t.Fatal("应返回需要重规划")
	}
	if reason == "" {
		t.Fatal("应返回原因")
	}
}

// TestShouldReplan_No 测试 LLM 返回"否"时不需要重规划
func TestShouldReplan_No(t *testing.T) {
	prov := &mockProvider{
		completeFn: func(_ context.Context, _ *llm.CompletionRequest) (*llm.CompletionResponse, error) {
			return &llm.CompletionResponse{Content: "否，计划进展正常"}, nil
		},
	}
	replanner := NewLLMReplanner(prov)
	plan := &Plan{
		Goal: "测试目标",
		SubTasks: []SubTask{
			{ID: "s1", Description: "第一步", Status: TaskCompleted},
		},
	}

	needReplan, _ := replanner.ShouldReplan(context.Background(), plan, "子任务完成")
	if needReplan {
		t.Fatal("不应需要重规划")
	}
}

// TestShouldReplan_Error 测试 LLM 调用失败时的处理
func TestShouldReplan_Error(t *testing.T) {
	prov := &mockProvider{
		completeFn: func(_ context.Context, _ *llm.CompletionRequest) (*llm.CompletionResponse, error) {
			return nil, fmt.Errorf("connection timeout")
		},
	}
	replanner := NewLLMReplanner(prov)
	plan := &Plan{Goal: "test", SubTasks: []SubTask{{ID: "s1", Status: TaskPending}}}

	needReplan, reason := replanner.ShouldReplan(context.Background(), plan, "obs")
	if needReplan {
		t.Fatal("LLM 错误时不应返回需要重规划")
	}
	if reason == "" {
		t.Fatal("应返回错误信息")
	}
}

// TestReplan_Success 测试重新规划成功
func TestReplan_Success(t *testing.T) {
	prov := &mockProvider{
		completeFn: func(_ context.Context, _ *llm.CompletionRequest) (*llm.CompletionResponse, error) {
			return &llm.CompletionResponse{
				Content: `[{"id":"s1","description":"替代方案第一步","depends_on":[]},{"id":"s2","description":"替代方案第二步","depends_on":["s1"]}]`,
			}, nil
		},
	}
	replanner := NewLLMReplanner(prov)
	plan := &Plan{
		Goal: "原始目标",
		SubTasks: []SubTask{
			{ID: "old1", Description: "原方案", Status: TaskFailed, Result: "失败原因"},
		},
	}

	newPlan, err := replanner.Replan(context.Background(), plan, "原方案不可行")
	if err != nil {
		t.Fatalf("重新规划应成功: %v", err)
	}
	if newPlan.Goal != "原始目标" {
		t.Fatalf("目标应保持不变，实际 %s", newPlan.Goal)
	}
	if len(newPlan.SubTasks) != 2 {
		t.Fatalf("应有 2 个子任务，实际 %d", len(newPlan.SubTasks))
	}
	if newPlan.SubTasks[0].ID != "s1" {
		t.Fatalf("第一个子任务 ID 应为 s1，实际 %s", newPlan.SubTasks[0].ID)
	}
}

// TestReplan_LLMError 测试 LLM 调用失败
func TestReplan_LLMError(t *testing.T) {
	prov := &mockProvider{
		completeFn: func(_ context.Context, _ *llm.CompletionRequest) (*llm.CompletionResponse, error) {
			return nil, fmt.Errorf("timeout")
		},
	}
	replanner := NewLLMReplanner(prov)
	plan := &Plan{Goal: "test", SubTasks: []SubTask{{ID: "s1", Status: TaskFailed}}}

	_, err := replanner.Replan(context.Background(), plan, "失败")
	if err == nil {
		t.Fatal("LLM 错误应导致 replan 失败")
	}
}

// TestReplan_ParseError 测试 JSON 解析失败
func TestReplan_ParseError(t *testing.T) {
	prov := &mockProvider{
		completeFn: func(_ context.Context, _ *llm.CompletionRequest) (*llm.CompletionResponse, error) {
			return &llm.CompletionResponse{Content: "这不是有效的 JSON"}, nil
		},
	}
	replanner := NewLLMReplanner(prov)
	plan := &Plan{Goal: "test", SubTasks: []SubTask{{ID: "s1", Status: TaskFailed}}}

	_, err := replanner.Replan(context.Background(), plan, "失败")
	if err == nil {
		t.Fatal("无效 JSON 应导致解析失败")
	}
}
