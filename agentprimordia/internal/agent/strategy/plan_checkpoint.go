// plan_checkpoint.go — v5.2 计划级 checkpoint：规划树持久化 + 断点续跑。
//
// 偿还 v3.4 缺口（checkpoint 只存轮次不存计划）：保存 Plan 的子任务状态，
// kill 后 Resume 从断点子任务继续，已完成子任务不重跑。
package strategy

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/agent/planning"
)

// PlanCheckpoint 计划级检查点
type PlanCheckpoint struct {
	Goal      string             `json:"goal"`
	SubTasks  []planning.SubTask `json:"subtasks"`
	UpdatedAt time.Time          `json:"updated_at"`
}

// PlanCheckpointStore 计划级检查点存储接口（复用既有 CheckpointStore 后端均可适配）
type PlanCheckpointStore interface {
	SavePlanCheckpoint(ctx context.Context, sessionID string, cp PlanCheckpoint) error
	LoadPlanCheckpoint(ctx context.Context, sessionID string) (*PlanCheckpoint, error)
}

// InMemoryPlanCheckpointStore 内存实现（测试与单机场景）
type InMemoryPlanCheckpointStore struct {
	mu    sync.Mutex
	items map[string]PlanCheckpoint
}

// NewInMemoryPlanCheckpointStore 创建内存存储
func NewInMemoryPlanCheckpointStore() *InMemoryPlanCheckpointStore {
	return &InMemoryPlanCheckpointStore{items: make(map[string]PlanCheckpoint)}
}

// SavePlanCheckpoint 实现 PlanCheckpointStore
func (s *InMemoryPlanCheckpointStore) SavePlanCheckpoint(_ context.Context, sessionID string, cp PlanCheckpoint) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.items[sessionID] = cp
	return nil
}

// LoadPlanCheckpoint 实现 PlanCheckpointStore；不存在返回 nil, nil
func (s *InMemoryPlanCheckpointStore) LoadPlanCheckpoint(_ context.Context, sessionID string) (*PlanCheckpoint, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if cp, ok := s.items[sessionID]; ok {
		return &cp, nil
	}
	return nil, nil
}

// SavePlanCheckpoint 序列化计划为检查点并落store：
// 仅保留 completed / pending 状态（running 视作 pending——中断的子任务重跑）。
func SavePlanCheckpoint(ctx context.Context, store PlanCheckpointStore, sessionID string, plan *planning.Plan) error {
	if store == nil || plan == nil {
		return fmt.Errorf("strategy: 计划级 checkpoint 参数缺失")
	}
	cp := PlanCheckpoint{Goal: plan.Goal, UpdatedAt: time.Now()}
	for _, st := range plan.SubTasks {
		status := st.Status
		if status == planning.TaskRunning {
			status = planning.TaskPending // 中断语义：running → 重跑
		}
		cp.SubTasks = append(cp.SubTasks, planning.SubTask{
			ID: st.ID, Description: st.Description,
			DependsOn: st.DependsOn, Status: status, Result: st.Result,
		})
	}
	return store.SavePlanCheckpoint(ctx, sessionID, cp)
}

// ResumePlan 从检查点恢复计划：已完成子任务保留结果，返回下一批可执行子任务
// （依赖全部完成且自身未完成）。无检查点时返回错误。
func ResumePlan(ctx context.Context, store PlanCheckpointStore, sessionID string) (*planning.Plan, []planning.SubTask, error) {
	cp, err := store.LoadPlanCheckpoint(ctx, sessionID)
	if err != nil {
		return nil, nil, err
	}
	if cp == nil {
		return nil, nil, fmt.Errorf("strategy: 会话 %s 无计划级 checkpoint", sessionID)
	}

	plan := &planning.Plan{Goal: cp.Goal, SubTasks: cp.SubTasks, CreatedAt: cp.UpdatedAt}
	done := make(map[string]bool)
	for _, st := range cp.SubTasks {
		if st.Status == planning.TaskCompleted {
			done[st.ID] = true
		}
	}
	var next []planning.SubTask
	for _, st := range cp.SubTasks {
		if st.Status == planning.TaskCompleted {
			continue
		}
		ready := true
		for _, dep := range st.DependsOn {
			if !done[dep] {
				ready = false
				break
			}
		}
		if ready {
			next = append(next, st)
		}
	}
	return plan, next, nil
}

// MarshalPlanCheckpoint 导出 JSON（供通用 checkpoint 存储复用）
func MarshalPlanCheckpoint(cp *PlanCheckpoint) ([]byte, error) {
	return json.Marshal(cp)
}
