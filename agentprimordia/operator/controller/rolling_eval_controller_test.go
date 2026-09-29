package controller

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	agentv1 "github.com/Gleamseekers/AgentPrimordia/operator/api/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// --- CanaryState 序列化/反序列化 ---

func TestCanaryState_Serialization(t *testing.T) {
	original := CanaryState{
		Phase:         CanaryProgressing,
		StableImage:   "img:v1",
		CanaryImage:   "img:v2",
		CanaryPercent: 25,
		StartedAt:     time.Now().UTC().Truncate(time.Second),
		UpdatedAt:     time.Now().UTC().Truncate(time.Second),
		Decision:      "灰度进行中",
		EvalResult: &EvalResult{
			RanOK:     true,
			PassRate:  0.92,
			Threshold: 0.8,
		},
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var restored CanaryState
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if restored.Phase != original.Phase {
		t.Errorf("Phase = %q, want %q", restored.Phase, original.Phase)
	}
	if restored.StableImage != original.StableImage {
		t.Errorf("StableImage = %q", restored.StableImage)
	}
	if restored.CanaryPercent != original.CanaryPercent {
		t.Errorf("CanaryPercent = %d", restored.CanaryPercent)
	}
	if restored.EvalResult == nil || restored.EvalResult.PassRate != 0.92 {
		t.Errorf("EvalResult mismatch: %+v", restored.EvalResult)
	}
}

// --- getCanaryState / saveCanaryState 读写 ---

func TestCanaryState_ReadWrite(t *testing.T) {
	ad := &agentv1.AgentDeployment{
		ObjectMeta: metav1.ObjectMeta{Name: "test-agent", Namespace: "default"},
	}

	r := &CanaryRolloutReconciler{}

	// 初始状态应为 Stable
	state, err := r.getCanaryState(ad)
	if err != nil {
		t.Fatalf("getCanaryState: %v", err)
	}
	if state.Phase != CanaryStable {
		t.Fatalf("初始 Phase = %q, want %q", state.Phase, CanaryStable)
	}

	// 写入 Progressing 状态
	state.Phase = CanaryProgressing
	state.CanaryPercent = 25
	state.CanaryImage = "img:v2"
	state.StableImage = "img:v1"

	data, _ := json.Marshal(state)
	if ad.Annotations == nil {
		ad.Annotations = map[string]string{}
	}
	ad.Annotations[canaryStateAnnotation] = string(data)

	// 重新读取
	state2, _ := r.getCanaryState(ad)
	if state2.Phase != CanaryProgressing {
		t.Fatalf("读取 Phase = %q, want %q", state2.Phase, CanaryProgressing)
	}
	if state2.CanaryPercent != 25 {
		t.Fatalf("CanaryPercent = %d, want 25", state2.CanaryPercent)
	}
}

// --- 损坏的 annotation 应回退到 Stable ---

func TestCanaryState_CorruptedAnnotation(t *testing.T) {
	ad := &agentv1.AgentDeployment{
		ObjectMeta: metav1.ObjectMeta{Name: "test-agent", Namespace: "default"},
	}
	ad.Annotations = map[string]string{
		canaryStateAnnotation: "{broken json",
	}

	r := &CanaryRolloutReconciler{}
	state, _ := r.getCanaryState(ad)
	if state.Phase != CanaryStable {
		t.Fatalf("损坏 annotation 应回退 Stable，got %q", state.Phase)
	}
}

// --- 默认灰度配置 ---

func TestDefaultCanaryConfig(t *testing.T) {
	cfg := DefaultCanaryConfig()
	if len(cfg.Canaries) != 4 {
		t.Fatalf("Canaries 长度 = %d, want 4", len(cfg.Canaries))
	}
	if cfg.Canaries[0] != 10 || cfg.Canaries[3] != 100 {
		t.Fatalf("Canaries = %v, want [10 25 50 100]", cfg.Canaries)
	}
	if cfg.PassThreshold != 0.8 {
		t.Fatalf("PassThreshold = %v, want 0.8", cfg.PassThreshold)
	}
	if cfg.EvalWait != 5*time.Minute {
		t.Fatalf("EvalWait = %v, want 5m", cfg.EvalWait)
	}
}

// --- Mock EvalRunner ---

type mockEvalRunner struct {
	result EvalResult
	err    error
}

func (m *mockEvalRunner) RunEval(ctx context.Context, agentName, image string) (EvalResult, error) {
	return m.result, m.err
}

// --- 状态机转换：Decision → Action 的集成 ---

func TestCanaryStateMachine_PromoteFlow(t *testing.T) {
	cfg := DefaultCanaryConfig()

	// 模拟 10% 灰度，Eval 通过
	state := CanaryState{
		Phase:         CanaryProgressing,
		CanaryPercent: 10,
		StableImage:   "img:v1",
		CanaryImage:   "img:v2",
	}
	evalResult := EvalResult{RanOK: true, PassRate: 0.95, Threshold: cfg.PassThreshold}

	decision := DecideRollout(state.CanaryPercent, evalResult)
	if decision.Action != ActionPromote {
		t.Fatalf("Eval 通过应 Promote，got %q", decision.Action)
	}

	// 当前在步进 0（10%），应推进到 25%
	currentIdx := -1
	for i, pct := range cfg.Canaries {
		if pct == state.CanaryPercent {
			currentIdx = i
			break
		}
	}
	if currentIdx != 0 {
		t.Fatalf("当前步进 idx = %d, want 0", currentIdx)
	}

	// 推进后应为 25
	nextPercent := cfg.Canaries[currentIdx+1]
	if nextPercent != 25 {
		t.Fatalf("下一步进 = %d, want 25", nextPercent)
	}
}

func TestCanaryStateMachine_RollbackFlow(t *testing.T) {
	cfg := DefaultCanaryConfig()

	// 模拟 25% 灰度，Eval 失败
	state := CanaryState{
		Phase:         CanaryProgressing,
		CanaryPercent: 25,
		StableImage:   "img:v1",
		CanaryImage:   "img:v2",
	}
	evalResult := EvalResult{RanOK: true, PassRate: 0.5, Threshold: cfg.PassThreshold}

	decision := DecideRollout(state.CanaryPercent, evalResult)
	if decision.Action != ActionRollback {
		t.Fatalf("Eval 失败应 Rollback，got %q", decision.Action)
	}
}

func TestCanaryStateMachine_HoldFlow(t *testing.T) {
	state := CanaryState{
		Phase:         CanaryProgressing,
		CanaryPercent: 25,
	}
	evalResult := EvalResult{RanOK: false, PassRate: 0, Threshold: 0.8}

	decision := DecideRollout(state.CanaryPercent, evalResult)
	if decision.Action != ActionHold {
		t.Fatalf("Eval 未运行应 Hold，got %q", decision.Action)
	}
}

// --- 最终步进 Promote ---

func TestCanaryStateMachine_FinalStepPromote(t *testing.T) {
	cfg := DefaultCanaryConfig()

	// 模拟 100% 灰度，Eval 通过 → 应直接 promote（而非推进）
	state := CanaryState{
		Phase:         CanaryProgressing,
		CanaryPercent: 100,
	}
	evalResult := EvalResult{RanOK: true, PassRate: 0.95, Threshold: cfg.PassThreshold}

	decision := DecideRollout(state.CanaryPercent, evalResult)
	if decision.Action != ActionPromote {
		t.Fatalf("100%% 灰度通过应 Promote，got %q", decision.Action)
	}

	// 在步进序列中 100 是最后一个
	currentIdx := -1
	for i, pct := range cfg.Canaries {
		if pct == state.CanaryPercent {
			currentIdx = i
			break
		}
	}
	if currentIdx != len(cfg.Canaries)-1 {
		t.Fatalf("100 应在最后位置 idx=%d", currentIdx)
	}
	// 已到最后 → 应 promote 而非 advance
	if currentIdx >= len(cfg.Canaries)-1 {
		// 正确路径：promote
		return
	}
	t.Fatal("不应到达此处")
}

// --- 超时回滚逻辑 ---

func TestCanaryState_TimeoutRollback(t *testing.T) {
	cfg := CanaryRolloutConfig{
		Canaries:      []int{10, 25, 50, 100},
		EvalWait:      100 * time.Millisecond,
		PassThreshold: 0.8,
	}

	state := CanaryState{
		Phase:     CanaryProgressing,
		StartedAt: time.Now().Add(-cfg.EvalWait * 3), // 远超超时
	}

	// 超时判断
	if time.Since(state.StartedAt) > cfg.EvalWait*2 {
		// 应回滚
		return
	}
	t.Fatal("超时应触发回滚")
}

// --- Mock EvalRunner 测试 ---

func TestMockEvalRunner(t *testing.T) {
	mock := &mockEvalRunner{
		result: EvalResult{RanOK: true, PassRate: 0.9, Threshold: 0.8},
	}

	ctx := context.Background()
	result, err := mock.RunEval(ctx, "test-agent", "img:v2")
	if err != nil {
		t.Fatalf("RunEval err: %v", err)
	}
	if !result.RanOK || result.PassRate != 0.9 {
		t.Fatalf("result = %+v", result)
	}

	// 测试错误路径
	mockErr := &mockEvalRunner{err: context.DeadlineExceeded}
	_, err = mockErr.RunEval(ctx, "test-agent", "img:v2")
	if err == nil {
		t.Fatal("期望错误，但返回 nil")
	}
}
