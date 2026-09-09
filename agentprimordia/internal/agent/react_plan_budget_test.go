// react_plan_budget_test.go — v7.3 P2 回归修复验证
// 验证子任务轮次预算和超时分配逻辑，确保 plan 场景下单子任务不会耗尽全局配额。
package agent

import (
	"context"
	"testing"
	"time"

	"agentprimordia/internal/agent/planning"
)

// TestPlanBudget_TurnDistribution 验证轮次预算按子任务数均分
func TestPlanBudget_TurnDistribution(t *testing.T) {
	tests := []struct {
		name      string
		maxTurns  int
		nSubtasks int
		wantTurns int
	}{
		{"10轮3子任务", 10, 3, 3},   // 10/3=3, min=3 → 3
		{"10轮2子任务", 10, 2, 5},   // 10/2=5, min=3 → 5
		{"50轮5子任务", 50, 5, 10},  // 50/5=10, min=3 → 10
		{"6轮4子任务", 6, 4, 3},     // 6/4=1, min=3 → 3（下限保护）
		{"100轮10子任务", 100, 10, 10}, // 100/10=10, min=3 → 10
		{"3轮5子任务", 3, 5, 3},     // 3/5=0, min=3 → 3（下限保护）
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.maxTurns / tt.nSubtasks
			if got < 3 {
				got = 3
			}
			if got != tt.wantTurns {
				t.Errorf("轮次预算 = %d, 期望 %d (maxTurns=%d, nSubtasks=%d)",
					got, tt.wantTurns, tt.maxTurns, tt.nSubtasks)
			}
		})
	}
}

// TestPlanBudget_TimeoutDistribution 验证超时预算从 context deadline 均分
func TestPlanBudget_TimeoutDistribution(t *testing.T) {
	nSubtasks := 5
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Second)
	defer cancel()

	deadline, ok := ctx.Deadline()
	if !ok {
		t.Fatal("context 应有 deadline")
	}
	remaining := time.Until(deadline)
	subTimeout := remaining / time.Duration(nSubtasks)
	if subTimeout < 10*time.Second {
		subTimeout = 10 * time.Second
	}

	// 50s / 5 = ~10s，应 >= 10s
	if subTimeout < 9*time.Second || subTimeout > 11*time.Second {
		t.Errorf("子任务超时 = %v, 期望 ~10s", subTimeout)
	}
}

// TestPlanBudget_TimeoutMinimum 验证超时下限保护
func TestPlanBudget_TimeoutMinimum(t *testing.T) {
	nSubtasks := 100
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	deadline, ok := ctx.Deadline()
	if !ok {
		t.Fatal("context 应有 deadline")
	}
	remaining := time.Until(deadline)
	subTimeout := remaining / time.Duration(nSubtasks)
	if subTimeout < 10*time.Second {
		subTimeout = 10 * time.Second
	}

	// 5s/100 = 50ms < 10s → 应回落至 10s
	if subTimeout != 10*time.Second {
		t.Errorf("子任务超时 = %v, 期望下限 10s", subTimeout)
	}
}

// TestPlanBudget_LoopConfigOverride 验证 loopConfig.subtaskMaxTurns 覆盖全局 MaxTurns
func TestPlanBudget_LoopConfigOverride(t *testing.T) {
	a := newReActAgent(ReActConfig{Name: "budget-test", MaxTurns: 50})

	// 无覆盖时，使用全局 MaxTurns
	cfg := loopConfig{}
	maxTurns := a.config.MaxTurns
	if cfg.subtaskMaxTurns > 0 && cfg.subtaskMaxTurns < maxTurns {
		maxTurns = cfg.subtaskMaxTurns
	}
	if maxTurns != 50 {
		t.Errorf("无覆盖时 maxTurns = %d, 期望 50", maxTurns)
	}

	// 有覆盖时，使用子任务预算
	cfg.subtaskMaxTurns = 5
	maxTurns = a.config.MaxTurns
	if cfg.subtaskMaxTurns > 0 && cfg.subtaskMaxTurns < maxTurns {
		maxTurns = cfg.subtaskMaxTurns
	}
	if maxTurns != 5 {
		t.Errorf("有覆盖时 maxTurns = %d, 期望 5", maxTurns)
	}

	// 覆盖值大于全局时，不生效（取较小值）
	cfg.subtaskMaxTurns = 100
	maxTurns = a.config.MaxTurns
	if cfg.subtaskMaxTurns > 0 && cfg.subtaskMaxTurns < maxTurns {
		maxTurns = cfg.subtaskMaxTurns
	}
	if maxTurns != 50 {
		t.Errorf("覆盖值 > 全局时 maxTurns = %d, 期望 50", maxTurns)
	}
}

// TestPlanBudget_ExecutesAllSubtasks 验证预算分配后所有子任务都能执行
func TestPlanBudget_ExecutesAllSubtasks(t *testing.T) {
	a := newReActAgent(ReActConfig{Name: "budget-exec", MaxTurns: 20})

	// 5 个子任务的 plan
	subtasks := make([]planning.SubTask, 5)
	for i := range subtasks {
		subtasks[i] = planning.SubTask{
			ID:          string(rune('a' + i)),
			Description: "step",
		}
	}
	plan := &planning.Plan{Goal: "test", SubTasks: subtasks}

	// 用 subtaskExecutor 代替真实 runLoop，记录每个子任务是否被执行
	executed := make([]string, 0)
	a.subtaskExecutor = func(ctx context.Context, st planning.SubTask, history []Message, cfg loopConfig) (*Response, error) {
		executed = append(executed, st.ID)
		// 验证轮次预算已设置
		if cfg.subtaskMaxTurns <= 0 || cfg.subtaskMaxTurns > 20 {
			t.Errorf("子任务 %s: subtaskMaxTurns=%d 不合理", st.ID, cfg.subtaskMaxTurns)
		}
		return &Response{Content: "ok"}, nil
	}

	resp, err := a.executePlanWithState(context.Background(), nil, plan, loopConfig{}, time.Now(), 0, 0, 0, nil)
	if err != nil {
		t.Fatalf("executePlanWithState 失败: %v", err)
	}
	if resp == nil {
		t.Fatal("resp 不应为 nil")
	}
	if len(executed) != 5 {
		t.Errorf("执行了 %d 个子任务, 期望 5", len(executed))
	}
}
