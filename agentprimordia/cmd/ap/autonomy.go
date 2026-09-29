// autonomy.go — ap autonomy 子命令（v7.4 起为真实实现）
//
// 装配真实的 AutonomyRuntime：
//   - StepExecutor：把步骤描述按 "<tool> <json-args>" 约定映射到已注册工具执行；
//   - CheckpointStore：JSON 文件持久化（.ap-autonomy/checkpoints/），支持 list/status/resume；
//   - 计划来源：--plan <file> 显式计划，或默认单步计划。
//
// 安全边界：默认仅注册无副作用的 echo 工具；执行其他工具需显式 --tool 或 plan
// 中指定且该工具已在默认注册表内。不提供 shell 直通，避免自治目标变成任意命令执行。
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/agent/autonomy"
	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/tools"
)

const autonomyUsage = `Usage: ap autonomy <subcommand> [arguments]

Subcommands:
  run <goal> [--plan file] [--tool NAME]   提交并执行自治目标
  list                                     列出已持久化的目标
  status <id>                              查看目标与步骤详情
  resume <id>                              从检查点恢复并继续执行

说明：
  - 步骤描述约定为 "<tool> <json-args>"（json-args 省略时为空对象）；
  - 默认仅注册无副作用的 echo 工具（不提供 shell 直通）；
  - 目标与步骤状态持久化于 .ap-autonomy/checkpoints/<id>.json。

Examples:
  ap autonomy run "汇总本周变更"
  ap autonomy run "执行计划" --plan plan.json
  ap autonomy list
  ap autonomy status goal-xxxx
  ap autonomy resume goal-xxxx
`

// autonomyDir 自治目标检查点目录。
var autonomyDir = ".ap-autonomy"

func autonomyCheckpointDir() string { return filepath.Join(autonomyDir, "checkpoints") }

// newAutonomyStore 生产构造点：JSON 文件检查点存储。
func newAutonomyStore() *jsonCheckpointStore {
	return newJSONCheckpointStore(autonomyCheckpointDir())
}

// ===== 步骤执行器 =====

// echoTool 无副作用的默认工具：回显参数，便于验证自治链路。
type echoTool struct{}

func (echoTool) Name() string        { return "echo" }
func (echoTool) Description() string { return "回显参数（无副作用）" }
func (echoTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{}}`)
}
func (echoTool) Execute(_ context.Context, args json.RawMessage) (*tools.Result, error) {
	return tools.NewResult(string(args)), nil
}

// defaultAutonomyRegistry 自治 CLI 默认工具注册表（仅无副作用工具）。
func defaultAutonomyRegistry() *tools.Registry {
	reg := tools.NewRegistry()
	_ = reg.Register(echoTool{})
	return reg
}

// toolStepExecutor 按 "<tool> <json-args>" 约定把步骤映射到注册工具。
type toolStepExecutor struct {
	registry *tools.Registry
}

// parseStepCommand 解析步骤描述为工具名与参数。
func parseStepCommand(desc string) (string, json.RawMessage) {
	desc = strings.TrimSpace(desc)
	if desc == "" {
		return "", json.RawMessage(`{}`)
	}
	name := desc
	args := "{}"
	if idx := strings.IndexAny(desc, " \t"); idx >= 0 {
		name = desc[:idx]
		if rest := strings.TrimSpace(desc[idx+1:]); rest != "" {
			args = rest
		}
	}
	return name, json.RawMessage(args)
}

func (e *toolStepExecutor) ExecuteStep(ctx context.Context, step autonomy.PlanStep) (string, error) {
	if e.registry == nil {
		return "", fmt.Errorf("autonomy: 未配置工具注册表")
	}
	name, args := parseStepCommand(step.Description)
	if name == "" {
		return "", fmt.Errorf("autonomy: 步骤 %s 描述为空，无法映射工具", step.ID)
	}
	tool, ok := e.registry.Get(name)
	if !ok {
		return "", fmt.Errorf("autonomy: 工具 %q 未注册（步骤 %s）", name, step.ID)
	}
	res, err := tool.Execute(ctx, args)
	if err != nil {
		return "", fmt.Errorf("autonomy: 步骤 %s 执行工具 %q 失败: %w", step.ID, name, err)
	}
	if res == nil {
		return "", nil
	}
	if res.IsError {
		return res.Content, fmt.Errorf("autonomy: 步骤 %s 工具 %q 返回错误: %s", step.ID, name, res.Content)
	}
	return res.Content, nil
}

// ===== 子命令分发 =====

func runAutonomy(args []string) error {
	if len(args) == 0 {
		fmt.Print(autonomyUsage)
		return nil
	}

	sub := args[0]
	subArgs := args[1:]

	switch sub {
	case "run":
		return runAutonomyRun(subArgs)
	case "list":
		return runAutonomyList(subArgs)
	case "status":
		return runAutonomyStatus(subArgs)
	case "resume":
		return runAutonomyResume(subArgs)
	case "--help", "-h", "help":
		fmt.Print(autonomyUsage)
		return nil
	default:
		return fmt.Errorf("unknown autonomy subcommand %q, run \"ap autonomy --help\"", sub)
	}
}

// buildAutonomyPlan 构造计划：优先 --plan 文件，否则默认单步计划。
func buildAutonomyPlan(goalID, goal, planFile, toolName string) (*autonomy.GoalPlan, error) {
	if planFile != "" {
		data, err := os.ReadFile(planFile)
		if err != nil {
			return nil, fmt.Errorf("读取计划文件失败: %w", err)
		}
		var filePlan struct {
			Steps []autonomy.PlanStep `json:"steps"`
		}
		if err := json.Unmarshal(data, &filePlan); err != nil {
			return nil, fmt.Errorf("解析计划文件失败: %w", err)
		}
		if len(filePlan.Steps) == 0 {
			return nil, fmt.Errorf("计划文件未包含任何步骤")
		}
		return autonomy.NewGoalPlan(goalID, filePlan.Steps), nil
	}

	if toolName == "" {
		toolName = "echo"
	}
	args, _ := json.Marshal(map[string]string{"goal": goal})
	return autonomy.NewGoalPlan(goalID, []autonomy.PlanStep{{
		ID:          "step-1",
		Description: toolName + " " + string(args),
		Strategy:    autonomy.StepStrategySequential,
	}}), nil
}

func runAutonomyRun(args []string) error {
	var goal, planFile, toolName string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--plan":
			i++
			if i >= len(args) {
				return fmt.Errorf("--plan 需要指定文件路径")
			}
			planFile = args[i]
		case "--tool":
			i++
			if i >= len(args) {
				return fmt.Errorf("--tool 需要指定工具名")
			}
			toolName = args[i]
		case "--help", "-h":
			fmt.Print(autonomyUsage)
			return nil
		default:
			if goal == "" {
				goal = args[i]
			}
		}
	}
	if goal == "" {
		return fmt.Errorf("用法: ap autonomy run <goal> [--plan file] [--tool NAME]")
	}

	ctx := context.Background()
	rt := autonomy.NewAutonomyRuntime(autonomy.RuntimeConfig{
		StepExecutor:    &toolStepExecutor{registry: defaultAutonomyRegistry()},
		CheckpointStore: newAutonomyStore(),
	})
	g := rt.SubmitGoal(goal, autonomy.GoalConfig{})

	plan, err := buildAutonomyPlan(g.ID, goal, planFile, toolName)
	if err != nil {
		return err
	}
	if err := rt.SetPlan(g.ID, plan); err != nil {
		return fmt.Errorf("设置计划失败: %w", err)
	}

	fmt.Printf("目标 %s 已提交，计划 %d 步，开始执行...\n", g.ID, len(plan.Steps))
	if err := rt.ExecuteGoal(ctx, g.ID); err != nil {
		_ = printAutonomyGoal(rt, g.ID)
		return fmt.Errorf("目标执行失败: %w", err)
	}
	if err := rt.CompleteGoal(g.ID); err != nil {
		return fmt.Errorf("目标完成标记失败: %w", err)
	}
	// runtime 仅在 ExecuteGoal 内落盘（状态 validated）；完成态需 CLI 补一次终态检查点，
	// 否则 list/status 看不到"已完成"。
	if err := saveFinalAutonomyCheckpoint(g.ID, goal, plan); err != nil {
		return err
	}
	successf("目标 %s 执行完成（检查点: %s）", g.ID, filepath.Join(autonomyCheckpointDir(), g.ID+".json"))
	return printAutonomyGoal(rt, g.ID)
}

// saveFinalAutonomyCheckpoint 写入终态（Done）检查点。
func saveFinalAutonomyCheckpoint(goalID, description string, plan *autonomy.GoalPlan) error {
	return newAutonomyStore().SaveCheckpoint(context.Background(), &autonomy.Checkpoint{
		GoalID:          goalID,
		GoalDescription: description,
		State:           autonomy.GoalDone,
		PlanSnapshot:    plan,
		Timestamp:       time.Now(),
		Completed:       true,
	})
}

func runAutonomyList(args []string) error {
	_ = args
	all, err := newAutonomyStore().listAll(context.Background())
	if err != nil {
		return err
	}
	if len(all) == 0 {
		infof("暂无自治目标（用 ap autonomy run <goal> 提交）")
		return nil
	}
	fmt.Println("自治目标:")
	for _, cp := range all {
		state := cp.State.String()
		if cp.Completed {
			state += " (已完成)"
		}
		fmt.Printf("  %-32s %-22s %s\n", cp.GoalID, state, cp.GoalDescription)
	}
	return nil
}

func runAutonomyStatus(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("用法: ap autonomy status <goal-id>")
	}
	cp, err := newAutonomyStore().LoadCheckpoint(context.Background(), args[0])
	if err != nil {
		return err
	}
	fmt.Printf("目标: %s\n", cp.GoalID)
	fmt.Printf("描述: %s\n", cp.GoalDescription)
	fmt.Printf("状态: %s\n", cp.State.String())
	fmt.Printf("更新时间: %s\n", cp.Timestamp.Format(time.RFC3339))
	if cp.PlanSnapshot != nil {
		fmt.Println("步骤:")
		for _, s := range cp.PlanSnapshot.Steps {
			result := s.Result
			if len(result) > 60 {
				result = result[:60] + "..."
			}
			fmt.Printf("  %-16s %-12s %s %s\n", s.ID, s.Status, s.Description, result)
		}
	}
	return nil
}

func runAutonomyResume(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("用法: ap autonomy resume <goal-id>")
	}
	goalID := args[0]
	ctx := context.Background()
	cp, err := newAutonomyStore().LoadCheckpoint(ctx, goalID)
	if err != nil {
		return err
	}
	if cp.PlanSnapshot == nil {
		return fmt.Errorf("目标 %s 的检查点缺少计划快照，无法恢复", goalID)
	}

	rt := autonomy.NewAutonomyRuntime(autonomy.RuntimeConfig{
		StepExecutor:    &toolStepExecutor{registry: defaultAutonomyRegistry()},
		CheckpointStore: newAutonomyStore(),
	})
	g := rt.SubmitGoal(cp.GoalDescription, autonomy.GoalConfig{})
	// 复用原计划快照（含各步骤已完成状态，ReadySteps 会自动跳过已完成步骤）。
	// GoalPlan 含 RWMutex，必须走 Clone 深拷贝（按值拷贝会 copylocks）。
	plan := cp.PlanSnapshot.Clone()
	if err := rt.SetPlan(g.ID, plan); err != nil {
		return fmt.Errorf("恢复计划失败: %w", err)
	}
	fmt.Printf("从检查点恢复目标 %s（原目标 %s），继续执行...\n", g.ID, goalID)

	if err := rt.ExecuteGoal(ctx, g.ID); err != nil {
		_ = printAutonomyGoal(rt, g.ID)
		return fmt.Errorf("恢复执行失败: %w", err)
	}
	if err := rt.CompleteGoal(g.ID); err != nil {
		return fmt.Errorf("目标完成标记失败: %w", err)
	}
	if err := saveFinalAutonomyCheckpoint(g.ID, cp.GoalDescription, plan); err != nil {
		return err
	}
	successf("目标 %s 恢复执行完成", g.ID)
	return printAutonomyGoal(rt, g.ID)
}

// printAutonomyGoal 打印运行时内目标的当前状态与步骤结果。
func printAutonomyGoal(rt *autonomy.AutonomyRuntime, goalID string) error {
	g, ok := rt.GetGoal(goalID)
	if !ok {
		return fmt.Errorf("目标 %s 不存在", goalID)
	}
	fmt.Printf("状态: %s\n", g.State.String())
	if plan, ok := rt.GetPlan(goalID); ok {
		for _, s := range plan.Steps {
			fmt.Printf("  步骤 %-12s %-12s %s\n", s.ID, s.Status, s.Result)
		}
	}
	return nil
}
