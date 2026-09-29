// planning-enhanced 示例 —— 演示 v7.1 增强规划能力的完整工作流
//
// 展示内容：
//   - EnhancedPlanner：组合 Base+Replanner+Recovery+Deadlock+Approval 的统一规划器
//   - ManagedPlan：计划状态机（pending→active→blocked→completed/failed）
//   - LLMReplanner：执行偏离时动态重规划
//   - DeadlockDetector：连续失败死路检测
//   - PolicyApprovalGate：高风险动作审批门
//
// 本示例使用 MockProvider，不需要真实 API 调用。
package main

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	ap "github.com/Gleamseekers/AgentPrimordia/agentprimordia/pkg"
	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/testutil"
)

func main() {
	fmt.Println("=== AgentPrimordia: 增强规划示例 ===")
	fmt.Println()

	// ─── 1. 构造 MockProvider，预设响应序列 ───
	// 每次 LLM 调用按序消耗一个响应：
	//   [0] Decompose  → 子任务 JSON
	//   [1] ShouldReplan → "否，计划正常"
	//   [2] ShouldReplan → "是，依赖服务不可用，需要重规划"
	//   [3] Replan → 替代子任务 JSON
	//   [4] Recover → 恢复方案子任务 JSON
	mockLLM := testutil.NewMockProvider(
		// [0] Decompose 响应：将任务分解为 3 个子任务
		`[
			{"id":"s1","description":"获取需求文档","depends_on":[]},
			{"id":"s2","description":"编写代码实现","depends_on":["s1"]},
			{"id":"s3","description":"部署到生产环境","depends_on":["s2"]}
		]`,
		// [1] ShouldReplan 响应：计划正常，无需重规划
		"否，当前计划执行正常",
		// [2] ShouldReplan 响应：需要重规划
		"是，依赖的外部服务不可用，需要切换方案",
		// [3] Replan 响应：替代方案
		`[
			{"id":"r1","description":"使用本地缓存数据替代远程调用","depends_on":[]},
			{"id":"r2","description":"基于缓存数据编写实现","depends_on":["r1"]},
			{"id":"r3","description":"本地验证后部署","depends_on":["r2"]}
		]`,
		// [4] Recover 响应：死路恢复方案
		`[
			{"id":"f1","description":"跳过不可用步骤，用桩数据继续","depends_on":[]},
			{"id":"f2","description":"完成核心功能开发","depends_on":["f1"]},
			{"id":"f3","description":"标记受限功能为待恢复","depends_on":["f2"]}
		]`,
	)

	// ─── 2. 创建增强规划器 ───
	// 定义高风险动作列表——这些动作执行前需要通过审批门
	highRiskActions := []string{"deploy", "delete", "rm", "drop"}
	enhanced := ap.NewEnhancedPlanner(mockLLM, highRiskActions)

	ctx := context.Background()

	// ─── 3. 生成计划并包装为 ManagedPlan ───
	fmt.Println("【第 1 步】生成执行计划")
	task := "完成用户模块开发并部署上线"
	plan, err := enhanced.GeneratePlan(ctx, task)
	if err != nil {
		log.Fatalf("生成计划失败: %v", err)
	}
	printPlan(plan)

	// 用 ManagedPlan 包装，启用状态机管理
	managed := ap.NewManagedPlan(plan)
	fmt.Printf("计划状态: %s\n\n", managed.State)

	// ─── 4. 状态机演示：pending → active ───
	fmt.Println("【第 2 步】激活计划")
	if err := managed.Transition(ap.PlanStateActive, "开始执行子任务"); err != nil {
		log.Fatalf("状态转换失败: %v", err)
	}
	fmt.Printf("计划状态: %s\n\n", managed.State)

	// ─── 5. 模拟子任务执行 + ShouldReplan 检查 ───
	fmt.Println("【第 3 步】执行子任务（模拟）")

	// 模拟 s1 成功
	plan.SubTasks[0].Status = ap.TaskCompleted
	plan.SubTasks[0].Result = "需求文档已获取"
	enhanced.Deadlock.RecordSuccess("s1")
	fmt.Printf("  s1 完成: %s\n", plan.SubTasks[0].Result)

	// 第一次 ShouldReplan 检查（Mock 返回"否"）
	needsReplan, reason := enhanced.Replanner.ShouldReplan(ctx, plan, "s1 执行成功")
	fmt.Printf("  重规划检查: %s\n", reason)
	fmt.Printf("  是否需要重规划: %v\n\n", needsReplan)

	// 模拟 s2 失败
	plan.SubTasks[1].Status = ap.TaskFailed
	plan.SubTasks[1].Result = "外部服务超时"
	enhanced.Deadlock.RecordFailure("s2")
	fmt.Printf("  s2 失败: %s\n", plan.SubTasks[1].Result)

	// 第二次 ShouldReplan 检查（Mock 返回"是"）
	needsReplan, reason = enhanced.Replanner.ShouldReplan(ctx, plan, "外部服务不可用")
	fmt.Printf("  重规划检查: %s\n", reason)
	fmt.Printf("  是否需要重规划: %v\n\n", needsReplan)

	// ─── 6. 动态重规划 ───
	if needsReplan {
		fmt.Println("【第 4 步】执行动态重规划")
		newPlan, err := enhanced.Replanner.Replan(ctx, plan, "外部服务不可用，需切换方案")
		if err != nil {
			log.Fatalf("重规划失败: %v", err)
		}
		plan = newPlan
		printPlan(plan)
		fmt.Println()
	}

	// ─── 7. 死路检测与恢复 ───
	fmt.Println("【第 5 步】死路检测与恢复")

	// 模拟连续失败达到阈值（DeadlockDetector 阈值为 3）
	for i := 0; i < 3; i++ {
		enhanced.Deadlock.RecordFailure("r1")
	}
	isDeadlock := enhanced.Recovery.DetectDeadlock(ctx, plan, "r1")
	fmt.Printf("  连续失败 3 次后死路检测: %v\n", isDeadlock)

	if isDeadlock {
		fmt.Println("  检测到死路，触发恢复策略...")
		recoveredPlan, err := enhanced.Recovery.Recover(ctx, plan, "r1")
		if err != nil {
			log.Fatalf("恢复失败: %v", err)
		}
		plan = recoveredPlan
		fmt.Println("  恢复方案:")
		printPlan(plan)
	}
	fmt.Println()

	// ─── 8. 审批门演示 ───
	fmt.Println("【第 6 步】高风险动作审批门")

	// 检查普通动作——无需审批
	needsApproval := enhanced.Approval.RequiresApproval(ctx, "read_file")
	fmt.Printf("  'read_file' 需要审批: %v\n", needsApproval)

	// 检查高风险动作——需要审批
	needsApproval = enhanced.Approval.RequiresApproval(ctx, "deploy")
	fmt.Printf("  'deploy' 需要审批: %v\n", needsApproval)

	// 提交审批请求并在后台 goroutine 等待
	go func() {
		// 模拟审批延迟
		time.Sleep(100 * time.Millisecond)
		fmt.Println("  [审批者] 已批准 'deploy' 操作")
		enhanced.Approval.Approve("deploy")
	}()

	if err := enhanced.Approval.RequestApproval(ctx, "deploy", "生产环境部署"); err != nil {
		log.Fatalf("提交审批失败: %v", err)
	}
	fmt.Println("  等待 'deploy' 审批...")
	if err := enhanced.Approval.WaitApproval(ctx, "deploy"); err != nil {
		log.Fatalf("等待审批失败: %v", err)
	}
	fmt.Println("  'deploy' 审批通过，继续执行")
	fmt.Println()

	// ─── 9. 完成计划，展示状态转换历史 ───
	fmt.Println("【第 7 步】完成计划")
	// 标记所有子任务完成
	for i := range plan.SubTasks {
		plan.SubTasks[i].Status = ap.TaskCompleted
	}

	// blocked → active（模拟从阻塞恢复）
	_ = managed.Transition(ap.PlanStateBlocked, "等待外部服务恢复")
	fmt.Printf("  当前状态: %s\n", managed.State)
	_ = managed.Transition(ap.PlanStateActive, "外部服务恢复，继续执行")
	fmt.Printf("  当前状态: %s\n", managed.State)

	// active → completed
	if err := managed.Transition(ap.PlanStateCompleted, "所有子任务执行完成"); err != nil {
		log.Fatalf("状态转换失败: %v", err)
	}
	fmt.Printf("  最终状态: %s\n", managed.State)
	fmt.Printf("  是否终态: %v\n", managed.IsTerminal())

	// 打印完整状态转换历史
	fmt.Println("\n状态转换历史:")
	for _, t := range managed.History {
		fmt.Printf("  %s → %s  (%s) [%s]\n",
			t.From, t.To, t.Why, t.At.Format("15:04:05"))
	}

	// 演示非法状态转换（终态不可再转换）
	fmt.Println("\n尝试从终态转换（预期失败）:")
	err = managed.Transition(ap.PlanStateActive, "try restart")
	if err != nil {
		fmt.Printf("  预期错误: %v\n", err)
	}

	fmt.Println("\n=== 示例完成 ===")
}

// printPlan 格式化打印计划
func printPlan(plan *ap.Plan) {
	fmt.Printf("  目标: %s\n", plan.Goal)
	for _, st := range plan.SubTasks {
		deps := "无"
		if len(st.DependsOn) > 0 {
			deps = strings.Join(st.DependsOn, ", ")
		}
		fmt.Printf("  [%s] %s (状态: %s, 依赖: %s)\n",
			st.ID, st.Description, st.Status, deps)
	}
}

// 确保 mockLLM 实现 ap.Provider 接口（编译期检查）
var _ ap.Provider = (*testutil.MockProvider)(nil)
