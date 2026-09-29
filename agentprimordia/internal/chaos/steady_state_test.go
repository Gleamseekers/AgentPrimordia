// steady_state_test.go — 稳态验证器详细测试
package chaos

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/health"
)

// ===== SLOSteadyState 测试 =====

func TestSLOSteadyState_Name(t *testing.T) {
	s := NewSLOSteadyState("slo-test", 0.999, nil)
	if s.Name() != "slo-test" {
		t.Errorf("Name() = %s, 期望 slo-test", s.Name())
	}
}

func TestSLOSteadyState_NilSnapshotFn(t *testing.T) {
	s := NewSLOSteadyState("slo-nil", 0.999, nil)
	result, err := s.Check(context.Background())
	if err != nil {
		t.Fatalf("Check() 错误: %v", err)
	}
	if !result.Met {
		t.Error("nil snapshotFn 应默认通过")
	}
	if !strings.Contains(result.Message, "无快照函数") {
		t.Errorf("Message = %s", result.Message)
	}
}

func TestSLOSteadyState_EmptyMetrics(t *testing.T) {
	s := NewSLOSteadyState("slo-empty", 0.999, func() []health.SLIMetric {
		return []health.SLIMetric{}
	})
	result, err := s.Check(context.Background())
	if err != nil {
		t.Fatalf("Check() 错误: %v", err)
	}
	if !result.Met {
		t.Error("空指标应默认通过")
	}
	if !strings.Contains(result.Message, "无 SLI 指标") {
		t.Errorf("Message = %s", result.Message)
	}
	if result.Details["metrics_count"].(int) != 0 {
		t.Errorf("metrics_count 应为 0")
	}
}

func TestSLOSteadyState_MetricsMet(t *testing.T) {
	s := NewSLOSteadyState("slo-met", 0.99, func() []health.SLIMetric {
		return []health.SLIMetric{
			{Name: "availability", Value: 0.999, Target: 0.99},
		}
	})
	result, err := s.Check(context.Background())
	if err != nil {
		t.Fatalf("Check() 错误: %v", err)
	}
	if !result.Met {
		t.Error("指标满足 SLO 时应通过")
	}
	if !strings.Contains(result.Message, "所有 SLO 满足") {
		t.Errorf("Message = %s", result.Message)
	}
	// 验证 details 包含指标信息
	if _, ok := result.Details["availability"]; !ok {
		t.Error("Details 应包含 availability 键")
	}
}

func TestSLOSteadyState_MetricsViolated(t *testing.T) {
	s := NewSLOSteadyState("slo-violated", 0.999, func() []health.SLIMetric {
		return []health.SLIMetric{
			{Name: "availability", Value: 0.95, Target: 0.999},
		}
	})
	result, err := s.Check(context.Background())
	if err != nil {
		t.Fatalf("Check() 错误: %v", err)
	}
	if result.Met {
		t.Error("指标违反 SLO 时应不满足")
	}
	if !strings.Contains(result.Message, "SLO 违反") {
		t.Errorf("Message = %s", result.Message)
	}
}

func TestSLOSteadyState_MultipleMetrics(t *testing.T) {
	s := NewSLOSteadyState("slo-multi", 0.99, func() []health.SLIMetric {
		return []health.SLIMetric{
			{Name: "avail", Value: 0.995},
			{Name: "latency", Value: 0.998},
		}
	})
	result, err := s.Check(context.Background())
	if err != nil {
		t.Fatalf("Check() 错误: %v", err)
	}
	if !result.Met {
		t.Error("所有指标满足时应通过")
	}
	if len(result.Details) != 2 {
		t.Errorf("Details 应有 2 个条目, 得到 %d", len(result.Details))
	}
}

// ===== AvailabilitySteadyState 测试 =====

func TestAvailabilitySteadyState_Name(t *testing.T) {
	s := NewAvailabilitySteadyState("avail-test", 0.99, nil)
	if s.Name() != "avail-test" {
		t.Errorf("Name() = %s", s.Name())
	}
}

func TestAvailabilitySteadyState_NilCheckFn(t *testing.T) {
	s := NewAvailabilitySteadyState("avail-nil", 0.99, nil)
	result, err := s.Check(context.Background())
	if err != nil {
		t.Fatalf("Check() 错误: %v", err)
	}
	if !result.Met {
		t.Error("nil checkFn 应默认通过")
	}
}

func TestAvailabilitySteadyState_Met(t *testing.T) {
	s := NewAvailabilitySteadyState("avail-met", 0.99, func() (int, int) {
		return 1000, 5 // 99.5% 可用性
	})
	result, err := s.Check(context.Background())
	if err != nil {
		t.Fatalf("Check() 错误: %v", err)
	}
	if !result.Met {
		t.Error("可用性 99.5% 应满足目标 99%")
	}
	if result.Details["total"].(int) != 1000 {
		t.Errorf("total = %v", result.Details["total"])
	}
	if result.Details["failures"].(int) != 5 {
		t.Errorf("failures = %v", result.Details["failures"])
	}
}

func TestAvailabilitySteadyState_NotMet(t *testing.T) {
	s := NewAvailabilitySteadyState("avail-fail", 0.999, func() (int, int) {
		return 100, 10 // 90% 可用性
	})
	result, err := s.Check(context.Background())
	if err != nil {
		t.Fatalf("Check() 错误: %v", err)
	}
	if result.Met {
		t.Error("可用性 90% 不应满足目标 99.9%")
	}
	if !strings.Contains(result.Message, "可用性") {
		t.Errorf("Message 应包含可用性信息, 得到 %s", result.Message)
	}
}

func TestAvailabilitySteadyState_ZeroRequests(t *testing.T) {
	s := NewAvailabilitySteadyState("avail-zero", 0.99, func() (int, int) {
		return 0, 0
	})
	result, err := s.Check(context.Background())
	if err != nil {
		t.Fatalf("Check() 错误: %v", err)
	}
	// 0 请求时 CalculateAvailability 返回 1.0
	if !result.Met {
		t.Error("0 请求时应默认满足")
	}
}

// ===== LatencySteadyState 测试 =====

func TestLatencySteadyState_Name(t *testing.T) {
	s := NewLatencySteadyState("latency-test", 100*time.Millisecond)
	if s.Name() != "latency-test" {
		t.Errorf("Name() = %s", s.Name())
	}
}

func TestLatencySteadyState_NoSamples(t *testing.T) {
	s := NewLatencySteadyState("latency-empty", 100*time.Millisecond)
	result, err := s.Check(context.Background())
	if err != nil {
		t.Fatalf("Check() 错误: %v", err)
	}
	if !result.Met {
		t.Error("无样本应默认通过")
	}
	if !strings.Contains(result.Message, "无延迟样本") {
		t.Errorf("Message = %s", result.Message)
	}
}

func TestLatencySteadyState_Met(t *testing.T) {
	s := NewLatencySteadyState("latency-met", 200*time.Millisecond)
	// 所有样本都在目标内
	for i := 0; i < 100; i++ {
		s.Record(time.Duration(i) * time.Millisecond)
	}
	result, err := s.Check(context.Background())
	if err != nil {
		t.Fatalf("Check() 错误: %v", err)
	}
	if !result.Met {
		t.Error("P99 延迟应满足目标")
	}
	if result.Details["samples"].(int) != 100 {
		t.Errorf("samples = %v", result.Details["samples"])
	}
}

func TestLatencySteadyState_NotMet(t *testing.T) {
	s := NewLatencySteadyState("latency-fail", 50*time.Millisecond)
	// 大部分样本超出目标
	for i := 0; i < 100; i++ {
		s.Record(time.Duration(i*10) * time.Millisecond)
	}
	result, err := s.Check(context.Background())
	if err != nil {
		t.Fatalf("Check() 错误: %v", err)
	}
	if result.Met {
		t.Error("P99 延迟不应满足目标")
	}
	if !strings.Contains(result.Message, "P99 延迟") {
		t.Errorf("Message = %s", result.Message)
	}
}

func TestLatencySteadyState_RecordConcurrency(t *testing.T) {
	s := NewLatencySteadyState("latency-concurrent", 1*time.Second)
	done := make(chan struct{})
	// 并发写入
	for i := 0; i < 10; i++ {
		go func(n int) {
			for j := 0; j < 100; j++ {
				s.Record(time.Millisecond)
			}
			done <- struct{}{}
		}(i)
	}
	for i := 0; i < 10; i++ {
		<-done
	}
	result, err := s.Check(context.Background())
	if err != nil {
		t.Fatalf("Check() 错误: %v", err)
	}
	if !result.Met {
		t.Error("所有 1ms 样本应满足 1s 目标")
	}
	if result.Details["samples"].(int) != 1000 {
		t.Errorf("samples = %v, 期望 1000", result.Details["samples"])
	}
}

// ===== CompositeSteadyState 测试 =====

func TestCompositeSteadyState_Name(t *testing.T) {
	s := NewCompositeSteadyState("comp-test")
	if s.Name() != "comp-test" {
		t.Errorf("Name() = %s", s.Name())
	}
}

func TestCompositeSteadyState_Empty(t *testing.T) {
	s := NewCompositeSteadyState("comp-empty")
	result, err := s.Check(context.Background())
	if err != nil {
		t.Fatalf("Check() 错误: %v", err)
	}
	if !result.Met {
		t.Error("空组合应默认满足")
	}
}

func TestCompositeSteadyState_AllMet(t *testing.T) {
	s := NewCompositeSteadyState("comp-all",
		NewAlwaysMetSteadyState(),
		NewAlwaysMetSteadyState(),
	)
	result, err := s.Check(context.Background())
	if err != nil {
		t.Fatalf("Check() 错误: %v", err)
	}
	if !result.Met {
		t.Error("所有满足时组合应满足")
	}
	if !strings.Contains(result.Message, "所有稳态条件满足") {
		t.Errorf("Message = %s", result.Message)
	}
}

func TestCompositeSteadyState_PartialNotMet(t *testing.T) {
	s := NewCompositeSteadyState("comp-partial",
		NewAlwaysMetSteadyState(),
		NewNeverMetSteadyState(),
	)
	result, err := s.Check(context.Background())
	if err != nil {
		t.Fatalf("Check() 错误: %v", err)
	}
	if result.Met {
		t.Error("部分不满足时组合不应满足")
	}
	if !strings.Contains(result.Message, "稳态条件不满足") {
		t.Errorf("Message = %s", result.Message)
	}
}

func TestCompositeSteadyState_WithError(t *testing.T) {
	s := NewCompositeSteadyState("comp-err",
		NewAlwaysMetSteadyState(),
		&errorSteadyState{},
	)
	_, err := s.Check(context.Background())
	if err == nil {
		t.Error("子稳态返回错误时组合应返回错误")
	}
}

// errorSteadyState Check 返回错误的稳态
type errorSteadyState struct{}

func (s *errorSteadyState) Name() string { return "error-state" }
func (s *errorSteadyState) Check(ctx context.Context) (SteadyStateResult, error) {
	return SteadyStateResult{}, fmt.Errorf("模拟错误")
}

func TestCompositeSteadyState_DetailsContainSubStates(t *testing.T) {
	s := NewCompositeSteadyState("comp-detail",
		NewAlwaysMetSteadyState(),
		NewNeverMetSteadyState(),
	)
	result, _ := s.Check(context.Background())
	if _, ok := result.Details["always_met"]; !ok {
		t.Error("Details 应包含 always_met 键")
	}
	if _, ok := result.Details["never_met"]; !ok {
		t.Error("Details 应包含 never_met 键")
	}
}

// ===== AlwaysMetSteadyState 测试 =====

func TestAlwaysMetSteadyState(t *testing.T) {
	s := NewAlwaysMetSteadyState()
	if s.Name() != "always_met" {
		t.Errorf("Name() = %s", s.Name())
	}
	result, err := s.Check(context.Background())
	if err != nil {
		t.Fatalf("Check() 错误: %v", err)
	}
	if !result.Met {
		t.Error("应始终满足")
	}
}

// ===== NeverMetSteadyState 测试 =====

func TestNeverMetSteadyState(t *testing.T) {
	s := NewNeverMetSteadyState()
	if s.Name() != "never_met" {
		t.Errorf("Name() = %s", s.Name())
	}
	result, err := s.Check(context.Background())
	if err != nil {
		t.Fatalf("Check() 错误: %v", err)
	}
	if result.Met {
		t.Error("应始终不满足")
	}
}

// ===== ToggleSteadyState 测试 =====

func TestToggleSteadyState_Initial(t *testing.T) {
	s := NewToggleSteadyState("toggle-init")
	if s.Name() != "toggle-init" {
		t.Errorf("Name() = %s", s.Name())
	}
	result, _ := s.Check(context.Background())
	if !result.Met {
		t.Error("初始应满足")
	}
}

func TestToggleSteadyState_Toggle(t *testing.T) {
	s := NewToggleSteadyState("toggle-switch")
	// 初始满足
	result, _ := s.Check(context.Background())
	if !result.Met {
		t.Error("初始应满足")
	}
	// 切换后不满足
	s.Toggle()
	result, _ = s.Check(context.Background())
	if result.Met {
		t.Error("切换后应不满足")
	}
	if !strings.Contains(result.Message, "已切换") {
		t.Errorf("Message = %s", result.Message)
	}
	// 再切换回来
	s.Toggle()
	result, _ = s.Check(context.Background())
	if !result.Met {
		t.Error("再次切换后应满足")
	}
}

func TestToggleSteadyState_SetMet(t *testing.T) {
	s := NewToggleSteadyState("toggle-set")
	s.SetMet(false)
	result, _ := s.Check(context.Background())
	if result.Met {
		t.Error("SetMet(false) 后应不满足")
	}
	s.SetMet(true)
	result, _ = s.Check(context.Background())
	if !result.Met {
		t.Error("SetMet(true) 后应满足")
	}
}
