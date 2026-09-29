package planning

import (
	"context"
	"testing"

	"github.com/Gleamseekers/AgentPrimordia/internal/llm"
)

// TestPlannerInterface 验证 Planner 接口定义
func TestPlannerInterface(t *testing.T) {
	// 确保 LLMPlanner 实现了 Planner 接口
	var _ Planner = (*LLMPlanner)(nil)
}

// TestSubTaskCreation 测试子任务创建
func TestSubTaskCreation(t *testing.T) {
	task := SubTask{
		ID:          "task-1",
		Description: "分析需求",
		DependsOn:   []string{},
		Status:      TaskPending,
	}

	if task.ID != "task-1" {
		t.Errorf("Expected ID 'task-1', got '%s'", task.ID)
	}
	if task.Status != TaskPending {
		t.Errorf("Expected status TaskPending, got %v", task.Status)
	}
}

// TestPlanCreation 测试计划创建
func TestPlanCreation(t *testing.T) {
	plan := &Plan{
		Goal: "完成项目",
		SubTasks: []SubTask{
			{ID: "1", Description: "需求分析", Status: TaskPending},
			{ID: "2", Description: "设计", Status: TaskPending, DependsOn: []string{"1"}},
			{ID: "3", Description: "实现", Status: TaskPending, DependsOn: []string{"2"}},
		},
	}

	if plan.Goal != "完成项目" {
		t.Errorf("Expected goal '完成项目', got '%s'", plan.Goal)
	}
	if len(plan.SubTasks) != 3 {
		t.Errorf("Expected 3 subtasks, got %d", len(plan.SubTasks))
	}
}

// TestTaskStatus 测试任务状态
func TestTaskStatus(t *testing.T) {
	statuses := []TaskStatus{TaskPending, TaskRunning, TaskCompleted, TaskFailed}

	for _, status := range statuses {
		if status == "" {
			t.Errorf("Status should not be empty")
		}
	}
}

// TestLLMPlannerDecompose 测试 LLMPlanner 的任务分解功能
func TestLLMPlannerDecompose(t *testing.T) {
	// 创建模拟 LLM Provider
	mockProvider := &mockLLMProvider{
		completeFunc: func(ctx context.Context, req *llm.CompletionRequest) (*llm.CompletionResponse, error) {
			// 返回模拟的分解结果
			return &llm.CompletionResponse{
				Content: `[{"id":"1","description":"第一步","depends_on":[]},{"id":"2","description":"第二步","depends_on":["1"]}]`,
			}, nil
		},
	}

	planner := NewLLMPlanner(mockProvider)

	subtasks, err := planner.Decompose(context.Background(), "完成一个复杂任务")
	if err != nil {
		t.Fatalf("Decompose failed: %v", err)
	}

	if len(subtasks) == 0 {
		t.Error("Expected at least one subtask")
	}
}

// TestLLMPlannerGeneratePlan 测试 LLMPlanner 的计划生成功能
func TestLLMPlannerGeneratePlan(t *testing.T) {
	mockProvider := &mockLLMProvider{
		completeFunc: func(ctx context.Context, req *llm.CompletionRequest) (*llm.CompletionResponse, error) {
			return &llm.CompletionResponse{
				Content: `[{"id":"1","description":"任务1","depends_on":[]}]`,
			}, nil
		},
	}

	planner := NewLLMPlanner(mockProvider)

	plan, err := planner.GeneratePlan(context.Background(), "测试任务")
	if err != nil {
		t.Fatalf("GeneratePlan failed: %v", err)
	}

	if plan == nil {
		t.Fatal("Expected plan, got nil")
	}
	if plan.Goal == "" {
		t.Error("Expected non-empty goal")
	}
}

// mockLLMProvider 模拟 LLM Provider
type mockLLMProvider struct {
	completeFunc func(ctx context.Context, req *llm.CompletionRequest) (*llm.CompletionResponse, error)
}

func (m *mockLLMProvider) Complete(ctx context.Context, req *llm.CompletionRequest) (*llm.CompletionResponse, error) {
	if m.completeFunc != nil {
		return m.completeFunc(ctx, req)
	}
	return &llm.CompletionResponse{Content: "[]"}, nil
}

func (m *mockLLMProvider) Stream(ctx context.Context, req *llm.CompletionRequest) (<-chan llm.Chunk, error) {
	return nil, nil
}

func (m *mockLLMProvider) CallTools(ctx context.Context, req *llm.ToolCallRequest) (*llm.ToolCallResponse, error) {
	return &llm.ToolCallResponse{}, nil
}

func (m *mockLLMProvider) Info() llm.ModelInfo {
	return llm.ModelInfo{}
}
