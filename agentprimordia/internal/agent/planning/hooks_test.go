// hooks_test.go — 增强规划器统一入口测试
package planning

import (
	"context"
	"testing"

	"github.com/Gleamseekers/AgentPrimordia/internal/llm"
)

// hooksStubProvider 最小化 LLM Provider 桩（仅用于构造，不实际调用）
type hooksStubProvider struct{}

func (s *hooksStubProvider) Complete(_ context.Context, _ *llm.CompletionRequest) (*llm.CompletionResponse, error) {
	return &llm.CompletionResponse{Content: "[]"}, nil
}

func (s *hooksStubProvider) Stream(_ context.Context, _ *llm.CompletionRequest) (<-chan llm.Chunk, error) {
	ch := make(chan llm.Chunk)
	close(ch)
	return ch, nil
}

func (s *hooksStubProvider) CallTools(_ context.Context, _ *llm.ToolCallRequest) (*llm.ToolCallResponse, error) {
	return &llm.ToolCallResponse{}, nil
}

func (s *hooksStubProvider) Info() llm.ModelInfo {
	return llm.ModelInfo{Name: "stub"}
}

var _ llm.Provider = (*hooksStubProvider)(nil)

// TestEnhancedPlannerImplementsPlanner 验证 EnhancedPlanner 实现 Planner 接口
func TestEnhancedPlannerImplementsPlanner(t *testing.T) {
	var _ Planner = (*EnhancedPlanner)(nil)
}

// TestEnhancedPlannerInitialization 验证所有子组件正确初始化
func TestEnhancedPlannerInitialization(t *testing.T) {
	provider := &hooksStubProvider{}
	actions := []string{"delete_file", "run_shell"}

	ep := NewEnhancedPlanner(provider, actions)

	if ep.Base == nil {
		t.Fatal("Base planner should not be nil")
	}
	if ep.Replanner == nil {
		t.Fatal("Replanner should not be nil")
	}
	if ep.Recovery == nil {
		t.Fatal("Recovery strategy should not be nil")
	}
	if ep.Deadlock == nil {
		t.Fatal("Deadlock detector should not be nil")
	}
	if ep.Approval == nil {
		t.Fatal("Approval gate should not be nil")
	}
}

// TestEnhancedPlannerApprovalWiring 验证审批门正确传递高风险动作
func TestEnhancedPlannerApprovalWiring(t *testing.T) {
	provider := &hooksStubProvider{}
	actions := []string{"delete_file", "run_shell"}

	ep := NewEnhancedPlanner(provider, actions)

	ctx := context.Background()
	if !ep.Approval.RequiresApproval(ctx, "delete_file") {
		t.Error("delete_file should require approval")
	}
	if !ep.Approval.RequiresApproval(ctx, "run_shell") {
		t.Error("run_shell should require approval")
	}
	if ep.Approval.RequiresApproval(ctx, "read_file") {
		t.Error("read_file should not require approval")
	}
}

// TestEnhancedPlannerDeadlockThreshold 验证死路检测器阈值设置
func TestEnhancedPlannerDeadlockThreshold(t *testing.T) {
	provider := &hooksStubProvider{}
	ep := NewEnhancedPlanner(provider, nil)

	ctx := context.Background()

	// 默认阈值为 3
	// 连续失败 2 次不应触发
	ep.Deadlock.RecordFailure("task-1")
	ep.Deadlock.RecordFailure("task-1")
	if ep.Deadlock.DetectDeadlock(ctx, nil, "task-1") {
		t.Error("should not detect deadlock after 2 failures (threshold=3)")
	}

	// 第 3 次应触发
	ep.Deadlock.RecordFailure("task-1")
	if !ep.Deadlock.DetectDeadlock(ctx, nil, "task-1") {
		t.Error("should detect deadlock after 3 failures (threshold=3)")
	}
}

// TestEnhancedPlannerDeadlockSuccessReset 验证成功时重置计数器
func TestEnhancedPlannerDeadlockSuccessReset(t *testing.T) {
	provider := &hooksStubProvider{}
	ep := NewEnhancedPlanner(provider, nil)

	ctx := context.Background()

	// 失败 2 次后成功，应重置计数
	ep.Deadlock.RecordFailure("task-2")
	ep.Deadlock.RecordFailure("task-2")
	ep.Deadlock.RecordSuccess("task-2")

	// 再失败 2 次，总共只有 2 次连续失败，不应触发
	ep.Deadlock.RecordFailure("task-2")
	ep.Deadlock.RecordFailure("task-2")
	if ep.Deadlock.DetectDeadlock(ctx, nil, "task-2") {
		t.Error("should not detect deadlock: success should reset counter")
	}
}

// TestEnhancedPlannerNilApprovalActions 验证空审批列表不 panic
func TestEnhancedPlannerNilApprovalActions(t *testing.T) {
	provider := &hooksStubProvider{}
	ep := NewEnhancedPlanner(provider, nil)

	if ep.Approval == nil {
		t.Fatal("Approval gate should not be nil even with nil actions")
	}

	ctx := context.Background()
	if ep.Approval.RequiresApproval(ctx, "anything") {
		t.Error("no action should require approval with empty actions list")
	}
}
