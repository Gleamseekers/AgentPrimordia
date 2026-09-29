// react_failure_test.go — v3.4-6 失败记录与一键重放测试
package agent

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Gleamseekers/AgentPrimordia/internal/agent/planning"
	"github.com/Gleamseekers/AgentPrimordia/internal/llm"
	"github.com/Gleamseekers/AgentPrimordia/internal/persist"
	"github.com/Gleamseekers/AgentPrimordia/internal/tools"
)

// newEchoRegistry 创建注册了 echo 工具（见 metrics_labels_test.go）的注册表，
// 使 Agent 持有非空 toolDefs、走 CallTools 路径。
func newEchoRegistry(t *testing.T) *tools.Registry {
	t.Helper()
	reg := tools.NewRegistry()
	if err := reg.Register(echoTool{}); err != nil {
		t.Fatalf("注册 echo 工具失败: %v", err)
	}
	return reg
}

// seqProvider 第一轮返回一次工具调用（使循环继续并落检查点）、之后永久失败的 LLM。
// 注意：CallTools 的重试会多次调用，首次成功、其余均失败即可。
type seqProvider struct {
	calls int
}

func (p *seqProvider) Complete(_ context.Context, _ *llm.CompletionRequest) (*llm.CompletionResponse, error) {
	return nil, errors.New("llm boom")
}

func (p *seqProvider) Stream(_ context.Context, _ *llm.CompletionRequest) (<-chan llm.Chunk, error) {
	return nil, errors.New("not implemented")
}

func (p *seqProvider) CallTools(_ context.Context, _ *llm.ToolCallRequest) (*llm.ToolCallResponse, error) {
	p.calls++
	if p.calls == 1 {
		return &llm.ToolCallResponse{
			ToolCalls: []llm.FunctionCall{{ID: "call-1", Name: "echo", Arguments: "{}"}},
		}, nil
	}
	return nil, errors.New("llm boom")
}

func (p *seqProvider) Embeddings(_ context.Context, _ []string) ([][]float32, error) {
	return nil, errors.New("not implemented")
}

func (p *seqProvider) Info() llm.ModelInfo {
	return llm.ModelInfo{Provider: "seq", Name: "seq-provider"}
}

// TestFailureRecord_RunError 运行失败时自动记录失败记录：
//  1. Phase=run、Error/Input/Turn 填充
//  2. 嵌入失败前最新检查点（State 非空且 Status=failed），支持一键重放
func TestFailureRecord_RunError(t *testing.T) {
	fstore := persist.NewMemoryFailureStore()
	cstore, err := persist.InMemoryCheckpointStore()
	if err != nil {
		t.Fatalf("InMemoryCheckpointStore 失败: %v", err)
	}

	ag, err := NewAgent("fail-agent", "助手", &seqProvider{},
		WithMaxTurns(5),
		WithToolkit(newEchoRegistry(t)),
		WithFailureStore(fstore),
		WithCheckpointStore(cstore),
	)
	if err != nil {
		t.Fatalf("NewAgent 失败: %v", err)
	}

	_, runErr := ag.Run(context.Background(), UserMessage("do something risky"))
	if runErr == nil {
		t.Fatal("期望运行失败")
	}

	recs, err := fstore.List(context.Background(), "fail-agent")
	if err != nil || len(recs) != 1 {
		t.Fatalf("期望 1 条失败记录，实际 %d（err=%v）", len(recs), err)
	}
	rec := recs[0]
	if rec.Phase != persist.PhaseRun {
		t.Errorf("Phase = %q, want run", rec.Phase)
	}
	if !strings.Contains(rec.Error, "llm boom") {
		t.Errorf("Error 应包含原始错误，实际 %q", rec.Error)
	}
	if rec.Input != "do something risky" {
		t.Errorf("Input = %q", rec.Input)
	}
	if rec.State == nil {
		t.Fatal("应嵌入失败时的检查点快照")
	}
	if rec.State.Status != "failed" {
		t.Errorf("嵌入检查点 Status = %q, want failed", rec.State.Status)
	}
	if rec.State.TurnCount < 1 {
		t.Errorf("嵌入检查点 TurnCount = %d, want >= 1", rec.State.TurnCount)
	}
	// 诊断摘要可用
	if diag := rec.Diagnose(); !strings.Contains(diag, "llm boom") {
		t.Errorf("Diagnose 缺少错误信息:\n%s", diag)
	}
}

// TestFailureRecord_PlanError_ExtractsSubtask plan 阶段失败时提取子任务 ID
func TestFailureRecord_PlanError_ExtractsSubtask(t *testing.T) {
	fstore := persist.NewMemoryFailureStore()
	cstore, err := persist.InMemoryCheckpointStore()
	if err != nil {
		t.Fatalf("InMemoryCheckpointStore 失败: %v", err)
	}

	mock := llm.NewMockLLM(t).WithResponse("unused")
	ag, err := NewAgent("plan-fail-agent", "助手", mock,
		WithMaxTurns(5),
		WithFailureStore(fstore),
		WithCheckpointStore(cstore),
		WithPlanner(&fixedPlanner{plan: &planning.Plan{
			Goal: "test",
			SubTasks: []planning.SubTask{
				{ID: "1", Description: "编写", DependsOn: []string{}},
				{ID: "2", Description: "测试", DependsOn: []string{"1"}},
			},
		}}),
	)
	if err != nil {
		t.Fatalf("NewAgent 失败: %v", err)
	}

	// 子任务 2 永久失败（含默认 1 次重试）
	ag.Inner().subtaskExecutor = func(_ context.Context, task planning.SubTask, _ []Message, _ loopConfig) (*Response, error) {
		if task.ID == "2" {
			return nil, errors.New("tool broke")
		}
		return &Response{Content: "ok-" + task.ID}, nil
	}
	// v3.6-1：本测试聚焦失败记录，显式关闭自愈（否则计划失败会被降级恢复而不再报错）
	ag.Inner().config.PlanRecoveryMode = "off"

	_, runErr := ag.Run(context.Background(), UserMessage("run the plan"))
	if runErr == nil {
		t.Fatal("期望计划执行失败")
	}

	recs, err := fstore.List(context.Background(), "plan-fail-agent")
	if err != nil || len(recs) != 1 {
		t.Fatalf("期望 1 条失败记录，实际 %d（err=%v）", len(recs), err)
	}
	rec := recs[0]
	if rec.Phase != persist.PhasePlan {
		t.Errorf("Phase = %q, want plan", rec.Phase)
	}
	if rec.SubTaskID != "2" {
		t.Errorf("SubTaskID = %q, want 2", rec.SubTaskID)
	}
	if !strings.Contains(rec.Error, "tool broke") {
		t.Errorf("Error 应包含根因，实际 %q", rec.Error)
	}
	if rec.State == nil || rec.State.Plan == nil {
		t.Fatal("plan 失败应嵌入带 Plan 进度的检查点")
	}
}

// TestFailureRecord_InputBlocked_NotRecorded 护栏拒绝不算失败，不记录
func TestFailureRecord_InputBlocked_NotRecorded(t *testing.T) {
	fstore := persist.NewMemoryFailureStore()
	mock := llm.NewMockLLM(t).WithResponse("ok")

	blockAll := func(_ string) (string, bool, error) {
		return "", true, nil
	}
	ag, err := NewAgent("blocked-agent", "助手", mock,
		WithMaxTurns(3),
		WithFailureStore(fstore),
		WithInputGuard(InputGuard(blockAll)),
	)
	if err != nil {
		t.Fatalf("NewAgent 失败: %v", err)
	}

	if _, runErr := ag.Run(context.Background(), UserMessage("evil input")); runErr == nil {
		t.Fatal("期望输入被拒绝")
	}

	recs, _ := fstore.List(context.Background(), "blocked-agent")
	if len(recs) != 0 {
		t.Fatalf("护栏拒绝不应记录失败，实际 %d 条", len(recs))
	}
}

// TestFailureRecord_NoStore_Noop 未配置 FailureStore 时不 panic、正常运行
func TestFailureRecord_NoStore_Noop(t *testing.T) {
	ag, err := NewAgent("nostore-agent", "助手", &seqProvider{},
		WithMaxTurns(5),
		WithToolkit(newEchoRegistry(t)),
	)
	if err != nil {
		t.Fatalf("NewAgent 失败: %v", err)
	}
	if _, runErr := ag.Run(context.Background(), UserMessage("boom")); runErr == nil {
		t.Fatal("期望运行失败")
	}
}

// TestReplayFailure_ResumesFromEmbeddedState 一键重放：
// 从失败记录内嵌检查点恢复执行，换掉故障 LLM 后跑通。
func TestReplayFailure_ResumesFromEmbeddedState(t *testing.T) {
	fstore := persist.NewMemoryFailureStore()
	cstore, err := persist.InMemoryCheckpointStore()
	if err != nil {
		t.Fatalf("InMemoryCheckpointStore 失败: %v", err)
	}

	// 第一次运行：turn1 发起工具调用后失败，留下失败记录
	ag1, err := NewAgent("replay-agent", "助手", &seqProvider{},
		WithMaxTurns(5),
		WithToolkit(newEchoRegistry(t)),
		WithFailureStore(fstore),
		WithCheckpointStore(cstore),
	)
	if err != nil {
		t.Fatalf("NewAgent 失败: %v", err)
	}
	if _, runErr := ag1.Run(context.Background(), UserMessage("risky task")); runErr == nil {
		t.Fatal("期望首次运行失败")
	}

	recs, _ := fstore.List(context.Background(), "replay-agent")
	if len(recs) != 1 {
		t.Fatalf("期望 1 条失败记录，实际 %d", len(recs))
	}

	// 第二个 Agent（同名、正常 LLM）一键重放失败
	ag2, err := NewAgent("replay-agent", "助手", llm.NewMockLLM(t).WithResponse("recovered"),
		WithMaxTurns(5),
		WithToolkit(newEchoRegistry(t)),
		WithFailureStore(fstore),
		WithCheckpointStore(cstore),
	)
	if err != nil {
		t.Fatalf("NewAgent 失败: %v", err)
	}

	resp, err := ag2.ReplayFailure(context.Background(), recs[0].ID)
	if err != nil {
		t.Fatalf("ReplayFailure 失败: %v", err)
	}
	if resp == nil || resp.Content != "recovered" {
		t.Fatalf("重放响应 Content = %+v, want recovered", resp)
	}
}

// TestReplayFailure_ErrorPaths 重放的错误路径
func TestReplayFailure_ErrorPaths(t *testing.T) {
	mock := llm.NewMockLLM(t).WithResponse("ok")

	// 1. 未配置 FailureStore
	ag, _ := NewAgent("rp-1", "助手", mock, WithMaxTurns(3))
	if _, err := ag.ReplayFailure(context.Background(), "any"); err == nil {
		t.Error("未配置 FailureStore 应返回错误")
	}

	// 2. 记录不存在
	fstore := persist.NewMemoryFailureStore()
	ag2, _ := NewAgent("rp-2", "助手", mock, WithMaxTurns(3), WithFailureStore(fstore))
	if _, err := ag2.ReplayFailure(context.Background(), "missing"); err == nil {
		t.Error("记录不存在应返回错误")
	}

	// 3. 记录无内嵌检查点（不可重放）
	_ = fstore.Record(context.Background(), &persist.FailureRecord{
		ID: "no-state", AgentID: "rp-2", Phase: persist.PhaseRun, Error: "x", CreatedAt: time.Now(),
	})
	if _, err := ag2.ReplayFailure(context.Background(), "no-state"); err == nil {
		t.Error("无内嵌检查点的失败记录应返回错误")
	}
}

// TestCapabilityAgent_FailureStoreWiring CapabilityAgent 的注入与委托
func TestCapabilityAgent_FailureStoreWiring(t *testing.T) {
	fstore := persist.NewMemoryFailureStore()
	mock := llm.NewMockLLM(t).WithResponse("ok")

	ag, err := NewAgent("wire-agent", "助手", mock, WithFailureStore(fstore))
	if err != nil {
		t.Fatalf("NewAgent 失败: %v", err)
	}
	if ag.GetFailureStore() == nil {
		t.Fatal("GetFailureStore 应返回已注入的存储")
	}

	// 链式 API
	a2 := newReActAgent(ReActConfig{Name: "chain-wire", MaxTurns: 3, Model: mock})
	cap := a2.WithFailureStore(fstore)
	if cap.GetFailureStore() == nil {
		t.Fatal("链式 WithFailureStore 应生效")
	}
}

// fixedPlanner 返回固定计划的 Planner
type fixedPlanner struct {
	plan *planning.Plan
}

func (p *fixedPlanner) Decompose(_ context.Context, _ string) ([]planning.SubTask, error) {
	return p.plan.SubTasks, nil
}

func (p *fixedPlanner) GeneratePlan(_ context.Context, goal string) (*planning.Plan, error) {
	plan := *p.plan
	plan.Goal = goal
	return &plan, nil
}
