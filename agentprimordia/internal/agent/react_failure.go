// react_failure.go — 失败捕获与一键重放（v3.4-6）
// Run 失败时自动记录失败记录（含失败时检查点），
// ReplayFailure 可从内嵌检查点恢复执行，实现「任意失败可一键重放」。
package agent

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/persist"
)

// subtaskFailPattern 匹配 plan 子任务失败的错误格式（见 react_plan_executor.go）
var subtaskFailPattern = regexp.MustCompile(`subtask (\S+) failed:`)

// recordFailure 运行以失败结束时自动记录失败记录（v3.4-6）。
// 护栏拒绝（ErrInputBlocked）属业务拒绝，不计为失败。
// 由 reactLoopEngine 入口 defer 调用；仅依赖 self 能力发现，不依赖 capCache。
func (a *ReActAgent) recordFailure(ctx context.Context, input Message, err error) {
	if err == nil || errors.Is(err, ErrInputBlocked) {
		return
	}
	store := a.getFailureStore()
	if store == nil {
		return
	}

	rec := &persist.FailureRecord{
		ID:        fmt.Sprintf("fail-%d", time.Now().UnixNano()),
		AgentID:   a.config.Name,
		SessionID: a.config.SessionID,
		Error:     err.Error(),
		Turn:      int(a.atomicTurn.Load()),
		Input:     input.Content,
		CreatedAt: time.Now(),
	}

	// 按错误形态判定失败阶段：plan 子任务失败错误含 "subtask <id> failed"
	if m := subtaskFailPattern.FindStringSubmatch(rec.Error); m != nil {
		rec.Phase = persist.PhasePlan
		rec.SubTaskID = m[1]
	} else {
		rec.Phase = persist.PhaseRun
	}

	// 嵌入失败前最新检查点（若有），使记录可一键重放
	if cs := a.getCheckpointStore(); cs != nil {
		if state, lerr := cs.Load(ctx, a.config.Name); lerr == nil && state != nil {
			state.Status = "failed"
			rec.State = state
		}
	}

	if rerr := store.Record(ctx, rec); rerr != nil {
		a.logger.Warn("写入失败记录失败", "error", rerr)
		return
	}
	a.logger.Info("失败记录已创建", "agent", a.config.Name, "phase", rec.Phase, "id", rec.ID)
}

// ReplayFailure 一键重放：从失败记录内嵌的检查点恢复执行（v3.4-6）。
// 典型场景：修复故障根因（更换 LLM/工具/网络）后，从失败处断点续跑。
// plan 阶段失败会从失败子任务起重跑（已完成子任务沿用结果）。
func (a *ReActAgent) ReplayFailure(ctx context.Context, failureID string) (*Response, error) {
	a.runMu.Lock()
	defer a.runMu.Unlock()

	store := a.getFailureStore()
	if store == nil {
		return nil, fmt.Errorf("failure store not configured")
	}
	rec, err := store.Get(ctx, failureID)
	if err != nil {
		return nil, fmt.Errorf("failed to load failure record %s: %w", failureID, err)
	}
	if rec.State == nil {
		return nil, fmt.Errorf("failure record %s has no embedded checkpoint, cannot replay", failureID)
	}

	a.logger.Info("重放失败任务", "agent", a.config.Name, "failure", failureID, "phase", rec.Phase, "turn", rec.Turn)
	return a.resumeFromState(ctx, rec.State)
}
