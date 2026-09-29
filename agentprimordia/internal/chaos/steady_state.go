package chaos

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Gleamseekers/AgentPrimordia/internal/health"
)

// ===== 稳态验证器 =====

// SLOSteadyState 基于 SLO 的稳态条件
type SLOSteadyState struct {
	name       string
	target     float64
	registry   *health.SLORegistry
	snapshotFn func() []health.SLIMetric // 获取当前 SLI 指标的函数
}

// NewSLOSteadyState 创建基于 SLO 的稳态条件
func NewSLOSteadyState(name string, target float64, snapshotFn func() []health.SLIMetric) *SLOSteadyState {
	return &SLOSteadyState{
		name:       name,
		target:     target,
		registry:   health.NewSLORegistry(target),
		snapshotFn: snapshotFn,
	}
}

func (s *SLOSteadyState) Name() string { return s.name }

func (s *SLOSteadyState) Check(ctx context.Context) (SteadyStateResult, error) {
	if s.snapshotFn == nil {
		return SteadyStateResult{
			Met:     true,
			Message: "无快照函数，默认通过",
		}, nil
	}

	metrics := s.snapshotFn()
	if len(metrics) == 0 {
		return SteadyStateResult{
			Met:     true,
			Message: "无 SLI 指标，默认通过",
			Details: map[string]any{
				"metrics_count": 0,
			},
		}, nil
	}

	statuses := health.CheckSLO(metrics, s.target)
	allMet := true
	details := make(map[string]any)
	var failedSLIs []string

	for _, st := range statuses {
		details[st.Name] = map[string]any{
			"current":      st.Current,
			"target":       st.Target,
			"burn_rate":    st.BurnRate,
			"error_budget": st.ErrorBudget,
			"violated":     st.Violated,
		}
		if st.Violated {
			allMet = false
			failedSLIs = append(failedSLIs, st.Name)
		}
	}

	msg := "所有 SLO 满足"
	if !allMet {
		msg = fmt.Sprintf("SLO 违反: %v", failedSLIs)
	}

	return SteadyStateResult{
		Met:     allMet,
		Message: msg,
		Details: details,
	}, nil
}

// AvailabilitySteadyState 可用性稳态条件
type AvailabilitySteadyState struct {
	name    string
	target  float64
	checkFn func() (total, failures int) // 返回总请求数和失败数
}

// NewAvailabilitySteadyState 创建可用性稳态条件
func NewAvailabilitySteadyState(name string, target float64, checkFn func() (total, failures int)) *AvailabilitySteadyState {
	return &AvailabilitySteadyState{
		name:    name,
		target:  target,
		checkFn: checkFn,
	}
}

func (s *AvailabilitySteadyState) Name() string { return s.name }

func (s *AvailabilitySteadyState) Check(ctx context.Context) (SteadyStateResult, error) {
	if s.checkFn == nil {
		return SteadyStateResult{Met: true, Message: "无检查函数"}, nil
	}

	total, failures := s.checkFn()
	avail := health.CalculateAvailability(total, failures)
	met := avail >= s.target

	return SteadyStateResult{
		Met:     met,
		Message: fmt.Sprintf("可用性 %.4f (目标 %.4f)", avail, s.target),
		Details: map[string]any{
			"total":        total,
			"failures":     failures,
			"availability": avail,
			"target":       s.target,
		},
	}, nil
}

// LatencySteadyState 延迟稳态条件
type LatencySteadyState struct {
	name      string
	p99Target time.Duration // P99 延迟目标
	samples   []time.Duration
	mu        sync.Mutex
}

// NewLatencySteadyState 创建延迟稳态条件
func NewLatencySteadyState(name string, p99Target time.Duration) *LatencySteadyState {
	return &LatencySteadyState{
		name:      name,
		p99Target: p99Target,
	}
}

func (s *LatencySteadyState) Name() string { return s.name }

// Record 记录一个延迟样本
func (s *LatencySteadyState) Record(latency time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.samples = append(s.samples, latency)
}

// Check 检查稳态
func (s *LatencySteadyState) Check(ctx context.Context) (SteadyStateResult, error) {
	s.mu.Lock()
	samples := make([]time.Duration, len(s.samples))
	copy(samples, s.samples)
	s.mu.Unlock()

	if len(samples) == 0 {
		return SteadyStateResult{
			Met:     true,
			Message: "无延迟样本",
		}, nil
	}

	p99 := health.CalculateLatencyP99(samples)
	met := p99 <= s.p99Target

	return SteadyStateResult{
		Met:     met,
		Message: fmt.Sprintf("P99 延迟 %v (目标 %v)", p99, s.p99Target),
		Details: map[string]any{
			"p99":     p99.String(),
			"target":  s.p99Target.String(),
			"samples": len(samples),
		},
	}, nil
}

// CompositeSteadyState 组合稳态条件（所有条件都必须满足）
type CompositeSteadyState struct {
	name   string
	states []SteadyState
}

// NewCompositeSteadyState 创建组合稳态条件
func NewCompositeSteadyState(name string, states ...SteadyState) *CompositeSteadyState {
	return &CompositeSteadyState{
		name:   name,
		states: states,
	}
}

func (s *CompositeSteadyState) Name() string { return s.name }

func (s *CompositeSteadyState) Check(ctx context.Context) (SteadyStateResult, error) {
	allMet := true
	details := make(map[string]any)

	for _, ss := range s.states {
		result, err := ss.Check(ctx)
		if err != nil {
			return SteadyStateResult{
				Met:     false,
				Message: fmt.Sprintf("稳态检查错误 (%s): %v", ss.Name(), err),
			}, err
		}
		if !result.Met {
			allMet = false
		}
		details[ss.Name()] = result
	}

	msg := "所有稳态条件满足"
	if !allMet {
		msg = "稳态条件不满足"
	}

	return SteadyStateResult{
		Met:     allMet,
		Message: msg,
		Details: details,
	}, nil
}

// AlwaysMetSteadyState 始终满足的稳态条件（用于测试框架）
type AlwaysMetSteadyState struct{}

func NewAlwaysMetSteadyState() *AlwaysMetSteadyState { return &AlwaysMetSteadyState{} }

func (s *AlwaysMetSteadyState) Name() string { return "always_met" }

func (s *AlwaysMetSteadyState) Check(ctx context.Context) (SteadyStateResult, error) {
	return SteadyStateResult{
		Met:     true,
		Message: "始终满足（测试用）",
	}, nil
}

// NeverMetSteadyState 始终不满足的稳态条件（用于测试框架）
type NeverMetSteadyState struct{}

func NewNeverMetSteadyState() *NeverMetSteadyState { return &NeverMetSteadyState{} }

func (s *NeverMetSteadyState) Name() string { return "never_met" }

func (s *NeverMetSteadyState) Check(ctx context.Context) (SteadyStateResult, error) {
	return SteadyStateResult{
		Met:     false,
		Message: "始终不满足（测试用）",
	}, nil
}

// ToggleSteadyState 可切换的稳态条件（用于测试）
// 初始为满足，调用 Toggle() 后变为不满足
type ToggleSteadyState struct {
	name string
	met  atomic.Bool
}

// NewToggleSteadyState 创建可切换稳态条件（初始满足）
func NewToggleSteadyState(name string) *ToggleSteadyState {
	s := &ToggleSteadyState{name: name}
	s.met.Store(true)
	return s
}

// Toggle 切换稳态状态
func (s *ToggleSteadyState) Toggle() {
	s.met.Store(!s.met.Load())
}

// SetMet 设置稳态是否满足
func (s *ToggleSteadyState) SetMet(met bool) {
	s.met.Store(met)
}

func (s *ToggleSteadyState) Name() string { return s.name }

func (s *ToggleSteadyState) Check(ctx context.Context) (SteadyStateResult, error) {
	met := s.met.Load()
	msg := "满足"
	if !met {
		msg = "不满足（已切换）"
	}
	return SteadyStateResult{
		Met:     met,
		Message: msg,
	}, nil
}
