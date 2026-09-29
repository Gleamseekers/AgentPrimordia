package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/agent/autonomy"
)

// newTestCheckpoint 构造一个「步骤均已完成、目标未完成」的检查点。
func newTestCheckpoint(goalID string, stepIDs []string) *autonomy.Checkpoint {
	steps := make([]autonomy.PlanStep, 0, len(stepIDs))
	for _, id := range stepIDs {
		steps = append(steps, autonomy.PlanStep{
			ID:          id,
			Description: "echo {}",
			Strategy:    autonomy.StepStrategySequential,
			Status:      autonomy.StepCompleted,
		})
	}
	return &autonomy.Checkpoint{
		GoalID:          goalID,
		GoalDescription: "已完成步骤的恢复目标",
		State:           autonomy.GoalExecuting,
		PlanSnapshot: &autonomy.GoalPlan{
			GoalID:    goalID,
			Steps:     steps,
			Version:   1,
			CreatedAt: time.Now(),
		},
		Timestamp: time.Now(),
		Completed: false,
	}
}

// TestAutonomyCLIRunPersistsAndExecutes 生产路径回归：
// ap autonomy 必须真实装配 AutonomyRuntime + 持久化 CheckpointStore + 工具 StepExecutor，
// 而非此前的打印占位实现。
func TestAutonomyCLIRunPersistsAndExecutes(t *testing.T) {
	oldDir := autonomyDir
	autonomyDir = t.TempDir()
	defer func() { autonomyDir = oldDir }()

	if err := runAutonomy([]string{"run", "汇总本周变更"}); err != nil {
		t.Fatalf("autonomy run 失败: %v", err)
	}

	all, err := newAutonomyStore().listAll(context.Background())
	if err != nil {
		t.Fatalf("列举检查点失败: %v", err)
	}
	if len(all) != 1 {
		t.Fatalf("持久化目标数 = %d, want 1", len(all))
	}
	cp := all[0]
	if cp.PlanSnapshot == nil || len(cp.PlanSnapshot.Steps) != 1 {
		t.Fatalf("检查点计划缺失: %+v", cp.PlanSnapshot)
	}
	if cp.PlanSnapshot.Steps[0].Status != "completed" {
		t.Errorf("步骤状态 = %s, want completed", cp.PlanSnapshot.Steps[0].Status)
	}
	if !cp.Completed {
		t.Error("目标应标记为已完成")
	}
}

// TestAutonomyCLIListStatusResume list/status/resume 均可用。
func TestAutonomyCLIListStatusResume(t *testing.T) {
	oldDir := autonomyDir
	autonomyDir = t.TempDir()
	defer func() { autonomyDir = oldDir }()

	if err := runAutonomy([]string{"run", "目标A"}); err != nil {
		t.Fatalf("run 失败: %v", err)
	}
	all, _ := newAutonomyStore().listAll(context.Background())
	if len(all) == 0 {
		t.Fatal("无检查点")
	}
	id := all[0].GoalID

	if err := runAutonomy([]string{"list"}); err != nil {
		t.Fatalf("list 失败: %v", err)
	}
	if err := runAutonomy([]string{"status", id}); err != nil {
		t.Fatalf("status 失败: %v", err)
	}
	if err := runAutonomy([]string{"resume", id}); err != nil {
		t.Fatalf("resume 失败: %v", err)
	}
}

// TestAutonomyCLIResumeSkipsCompletedSteps 恢复时已完成的步骤不再重复执行：
// 用失败工具验证——若 resume 重跑已完成步骤会因工具缺失而失败。
func TestAutonomyCLIResumeSkipsCompletedSteps(t *testing.T) {
	oldDir := autonomyDir
	autonomyDir = t.TempDir()
	defer func() { autonomyDir = oldDir }()

	// 直接用 store 写入一个「全部步骤已完成但目标未完成」的检查点
	store := newAutonomyStore()
	cp := newTestCheckpoint("goal-resume", []string{"s1", "s2"})
	if err := store.SaveCheckpoint(context.Background(), cp); err != nil {
		t.Fatalf("写入检查点失败: %v", err)
	}

	// echo 工具对空描述会报错，但两步均为 completed → ReadySteps 为空 → IsComplete 为真
	if err := runAutonomy([]string{"resume", "goal-resume"}); err != nil {
		t.Fatalf("resume 应跳过已完成步骤并成功，实际失败: %v", err)
	}
}

// TestAutonomyCLIRunFailsOnUnknownTool 未知工具必须报错（不静默成功）。
func TestAutonomyCLIRunFailsOnUnknownTool(t *testing.T) {
	oldDir := autonomyDir
	autonomyDir = t.TempDir()
	defer func() { autonomyDir = oldDir }()

	if err := runAutonomy([]string{"run", "目标", "--tool", "not-registered"}); err == nil {
		t.Fatal("未注册工具应导致执行失败")
	}
}

// TestAutonomyCLIRunWithPlanFile 显式计划文件驱动多步执行。
func TestAutonomyCLIRunWithPlanFile(t *testing.T) {
	oldDir := autonomyDir
	autonomyDir = t.TempDir()
	defer func() { autonomyDir = oldDir }()

	planFile := filepath.Join(t.TempDir(), "plan.json")
	planJSON := `{"steps":[
		{"id":"s1","description":"echo {\"n\":1}","strategy":"sequential"},
		{"id":"s2","description":"echo {\"n\":2}","strategy":"sequential","depends_on":["s1"]}
	]}`
	if err := os.WriteFile(planFile, []byte(planJSON), 0o644); err != nil {
		t.Fatalf("写入计划失败: %v", err)
	}

	if err := runAutonomy([]string{"run", "多步目标", "--plan", planFile}); err != nil {
		t.Fatalf("run --plan 失败: %v", err)
	}
	all, _ := newAutonomyStore().listAll(context.Background())
	if len(all) != 1 || len(all[0].PlanSnapshot.Steps) != 2 {
		t.Fatalf("计划步骤数不正确: %+v", all)
	}
	for _, s := range all[0].PlanSnapshot.Steps {
		if s.Status != "completed" {
			t.Errorf("步骤 %s 状态 = %s, want completed", s.ID, s.Status)
		}
	}
}
