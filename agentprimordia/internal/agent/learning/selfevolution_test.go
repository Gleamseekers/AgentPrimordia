// selfevolution_test.go — v5.4 验收场景：受控自进化实验。
//
// V6-ROADMAP §六 验收：同一任务集经 ≥5 轮进化迭代（每轮 ≥20 任务），
// 成功率曲线上升且无回归（任一轮较基线退化 >2% 即判失败），全程审计可查。
//
// 实验设计（确定性、无外部依赖；真实 LLM 版由 nightly 刷新）：
//   - 任务集：20 个任务，各带能力域与需求强度（所需轮数、是否需规避提示）
//   - 被测执行器：按「思考预算 + 提示增强」判定成败——正是 FeedbackLoop
//     建议所调控的两个旋钮（config 层 MaxTurns / prompt 层规避指引）
//   - 进化机制：每轮结束 RecordOutcome → Suggest → Approve → ApplyApproved，
//     Applier 真实修改执行器配置（闭环落地，非模拟记账）
package learning

import (
	"context"
	"fmt"
	"testing"

	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/memory"
)

// evoTask 进化实验任务
type evoTask struct {
	domain      string
	demandTurns int    // 成功所需最小预算
	needHint    string // 非空则必须注入该规避提示才成功
}

func evoTaskSet() []evoTask {
	tasks := make([]evoTask, 0, 20)
	for i := 0; i < 20; i++ {
		switch i % 4 {
		case 0: // 简单：浅预算即可
			tasks = append(tasks, evoTask{domain: "simple", demandTurns: 4})
		case 1: // 中等：需要中预算
			tasks = append(tasks, evoTask{domain: "medium", demandTurns: 8})
		case 2: // 困难：需要深预算
			tasks = append(tasks, evoTask{domain: "hard", demandTurns: 16})
		case 3: // 易错：需要规避提示
			tasks = append(tasks, evoTask{domain: "tricky", demandTurns: 4, needHint: "注意规避：预先拉取镜像"})
		}
	}
	return tasks
}

// evoExecutor 被测执行器：配置由反馈回路建议驱动演进
type evoExecutor struct {
	maxTurnsPerDomain map[string]int
	hints             map[string]bool
}

func newEvoExecutor() *evoExecutor {
	return &evoExecutor{
		maxTurnsPerDomain: map[string]int{"simple": 4, "medium": 4, "hard": 4, "tricky": 4},
		hints:             map[string]bool{},
	}
}

func (e *evoExecutor) run(t evoTask) bool {
	if t.needHint != "" && !e.hints[t.needHint] {
		return false
	}
	return e.maxTurnsPerDomain[t.domain] >= t.demandTurns
}

func TestControlledSelfEvolutionExperiment(t *testing.T) {
	ctx := context.Background()
	model := memory.NewSelfModel()
	loop := NewFeedbackLoop(model, nil, nil)
	exec := newEvoExecutor()

	// Applier：建议真实作用于执行器（config 层调预算 / prompt 层注提示）
	loop.SetApplier(func(s Suggestion) error {
		switch s.Scope {
		case ScopeConfig:
			if v, ok := s.Payload["max_turns"]; ok {
				var n int
				if _, err := fmt.Sscanf(v, "%d", &n); err == nil {
					exec.maxTurnsPerDomain[s.Domain] = n
				}
			}
		case ScopePrompt:
			if h, ok := s.Payload["append"]; ok {
				exec.hints[h] = true
			}
		}
		return nil
	})

	// 预置已知缓解手段（模拟 FailureStore 历史沉淀）
	model.SetMitigation("image_pull_backoff", "预先拉取镜像")

	const rounds = 6
	successCurve := make([]float64, rounds)

	for round := 0; round < rounds; round++ {
		tasks := evoTaskSet()
		successes := 0
		for _, tk := range tasks {
			ok := exec.run(tk)
			if ok {
				successes++
			}
			o := Outcome{Domain: tk.domain, Success: ok, Turns: exec.maxTurnsPerDomain[tk.domain]}
			if !ok && tk.needHint != "" {
				o.FailureSignature = "image_pull_backoff"
				o.ErrorText = "ImagePullBackOff"
			}
			loop.RecordOutcome(ctx, o)
		}
		successCurve[round] = float64(successes) / float64(len(tasks))

		// 进化步：为每个域生成建议→全部批准→应用（人工确认在实验中自动化）
		for _, domain := range []string{"simple", "medium", "hard", "tricky"} {
			for _, s := range loop.Suggest(domain, "") {
				if err := loop.Propose(s); err != nil {
					t.Fatalf("round %d: 建议被沙箱拒绝: %v", round+1, err)
				}
				loop.Approve(s.ID)
			}
		}
		if _, err := loop.ApplyApproved(ctx); err != nil {
			t.Fatalf("round %d: 应用失败: %v", round+1, err)
		}
	}

	// 验收断言
	t.Logf("成功率曲线: %.2f → %.2f → %.2f → %.2f → %.2f → %.2f",
		successCurve[0], successCurve[1], successCurve[2], successCurve[3], successCurve[4], successCurve[5])

	if successCurve[0] >= 1.0 {
		t.Fatal("实验设计错误：基线轮不应满分")
	}
	if successCurve[rounds-1] != 1.0 {
		t.Errorf("终轮应达 100%%，得到 %.2f", successCurve[rounds-1])
	}
	for r := 1; r < rounds; r++ {
		if successCurve[r] < successCurve[r-1]-0.02 {
			t.Errorf("第 %d 轮回归：%.2f < %.2f-0.02", r+1, successCurve[r], successCurve[r-1])
		}
	}
	if !(successCurve[rounds-1] > successCurve[0]) {
		t.Error("曲线必须上升")
	}
}
