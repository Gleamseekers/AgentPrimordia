package planning

import (
	"context"
	"fmt"
	"testing"

	"github.com/Gleamseekers/AgentPrimordia/internal/llm"
)

// TestDeadlockDetector_BelowThreshold 测试低于阈值不触发
func TestDeadlockDetector_BelowThreshold(t *testing.T) {
	d := NewDeadlockDetector(3)

	if d.RecordFailure("s1") {
		t.Fatal("第 1 次失败不应触发死路")
	}
	if d.RecordFailure("s1") {
		t.Fatal("第 2 次失败不应触发死路")
	}
}

// TestDeadlockDetector_AtThreshold 测试达到阈值触发
func TestDeadlockDetector_AtThreshold(t *testing.T) {
	d := NewDeadlockDetector(3)

	d.RecordFailure("s1")
	d.RecordFailure("s1")
	if !d.RecordFailure("s1") {
		t.Fatal("第 3 次连续失败应触发死路")
	}
}

// TestDeadlockDetector_SuccessResets 测试成功重置计数
func TestDeadlockDetector_SuccessResets(t *testing.T) {
	d := NewDeadlockDetector(3)

	d.RecordFailure("s1")
	d.RecordFailure("s1")
	d.RecordSuccess("s1") // 重置

	if d.RecordFailure("s1") {
		t.Fatal("成功后的第 1 次失败不应触发死路")
	}
}

// TestDeadlockDetector_DifferentSubTasks 测试不同子任务独立计数
func TestDeadlockDetector_DifferentSubTasks(t *testing.T) {
	d := NewDeadlockDetector(2)

	d.RecordFailure("s1")
	d.RecordFailure("s2")
	// s1 失败 1 次, s2 失败 1 次，都不应触发
	if d.RecordFailure("s3") {
		t.Fatal("s3 第 1 次失败不应触发")
	}
	// s1 再失败一次达到阈值
	if !d.RecordFailure("s1") {
		t.Fatal("s1 连续 2 次失败应触发死路")
	}
}

// TestDeadlockDetector_DetectDeadlock 测试 DetectDeadlock 方法
func TestDeadlockDetector_DetectDeadlock(t *testing.T) {
	d := NewDeadlockDetector(2)
	ctx := context.Background()

	if d.DetectDeadlock(ctx, nil, "s1") {
		t.Fatal("无失败记录时不应检测到死路")
	}
	d.RecordFailure("s1")
	if d.DetectDeadlock(ctx, nil, "s1") {
		t.Fatal("仅 1 次失败（阈值 2）不应检测到死路")
	}
	d.RecordFailure("s1")
	if !d.DetectDeadlock(ctx, nil, "s1") {
		t.Fatal("连续失败达到阈值应检测到死路")
	}
}

// TestDeadlockDetector_Reset 测试全量重置
func TestDeadlockDetector_Reset(t *testing.T) {
	d := NewDeadlockDetector(2)
	d.RecordFailure("s1")
	d.RecordFailure("s2")
	d.Reset()

	ctx := context.Background()
	if d.DetectDeadlock(ctx, nil, "s1") {
		t.Fatal("重置后不应检测到死路")
	}
}

// recoveryMockProvider 用于恢复策略测试的 mock
type recoveryMockProvider struct {
	completeFn func(ctx context.Context, req *llm.CompletionRequest) (*llm.CompletionResponse, error)
}

func (m *recoveryMockProvider) Complete(ctx context.Context, req *llm.CompletionRequest) (*llm.CompletionResponse, error) {
	if m.completeFn != nil {
		return m.completeFn(ctx, req)
	}
	return &llm.CompletionResponse{Content: "[]"}, nil
}

func (m *recoveryMockProvider) Stream(_ context.Context, _ *llm.CompletionRequest) (<-chan llm.Chunk, error) {
	return nil, fmt.Errorf("not implemented")
}

func (m *recoveryMockProvider) CallTools(_ context.Context, _ *llm.ToolCallRequest) (*llm.ToolCallResponse, error) {
	return nil, fmt.Errorf("not implemented")
}

func (m *recoveryMockProvider) Info() llm.ModelInfo {
	return llm.ModelInfo{Name: "mock", Provider: "test"}
}

// TestLLMRecoveryStrategy_Recover 测试恢复计划生成
func TestLLMRecoveryStrategy_Recover(t *testing.T) {
	prov := &recoveryMockProvider{
		completeFn: func(_ context.Context, _ *llm.CompletionRequest) (*llm.CompletionResponse, error) {
			return &llm.CompletionResponse{
				Content: `[{"id":"alt1","description":"替代方案","depends_on":[]}]`,
			}, nil
		},
	}
	detector := NewDeadlockDetector(2)
	strategy := NewLLMRecoveryStrategy(prov, detector)

	plan := &Plan{
		Goal: "原始目标",
		SubTasks: []SubTask{
			{ID: "s1", Description: "失败步骤", Status: TaskFailed, Result: "错误"},
		},
	}

	newPlan, err := strategy.Recover(context.Background(), plan, "s1")
	if err != nil {
		t.Fatalf("恢复应成功: %v", err)
	}
	if newPlan.Goal != "原始目标" {
		t.Fatalf("目标应保持不变")
	}
	if len(newPlan.SubTasks) != 1 {
		t.Fatalf("应有 1 个替代子任务，实际 %d", len(newPlan.SubTasks))
	}
}

// TestLLMRecoveryStrategy_LLMError 测试 LLM 失败时恢复失败
func TestLLMRecoveryStrategy_LLMError(t *testing.T) {
	prov := &recoveryMockProvider{
		completeFn: func(_ context.Context, _ *llm.CompletionRequest) (*llm.CompletionResponse, error) {
			return nil, fmt.Errorf("connection refused")
		},
	}
	detector := NewDeadlockDetector(1)
	strategy := NewLLMRecoveryStrategy(prov, detector)

	plan := &Plan{Goal: "test", SubTasks: []SubTask{{ID: "s1", Status: TaskFailed}}}
	_, err := strategy.Recover(context.Background(), plan, "s1")
	if err == nil {
		t.Fatal("LLM 错误应导致恢复失败")
	}
}
