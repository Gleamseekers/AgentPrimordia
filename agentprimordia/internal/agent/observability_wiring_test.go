package agent

import (
	"testing"

	"github.com/Gleamseekers/AgentPrimordia/internal/llm"
	"github.com/Gleamseekers/AgentPrimordia/internal/observability"
)

// TestCorrelationStoreAutoWiredWhenObservabilityEnabled 回归：任一可观测能力开启时，
// 全链路关联存储必须被自动装配——此前 NewCorrelationStore 零生产构造点，
// capCache.observability 恒 nil，RecordLLM/Tool/Turn 与 AddSpan/AddAuditEvent 全不执行。
func TestCorrelationStoreAutoWiredWhenObservabilityEnabled(t *testing.T) {
	a, err := NewAgent("obs-agent", "", llm.NewMockLLM(t), WithMetrics(&capTestMetricsRecorder{}))
	if err != nil {
		t.Fatalf("NewAgent 失败: %v", err)
	}
	if a.GetObservability() == nil {
		t.Fatal("开启可观测能力后应自动装配 CorrelationStore（实际为 nil）")
	}
}

// TestCorrelationStoreNotWiredByDefault 未开启可观测能力时保持 nil（默认路径零额外开销）。
func TestCorrelationStoreNotWiredByDefault(t *testing.T) {
	a, err := NewAgent("plain-agent", "", llm.NewMockLLM(t))
	if err != nil {
		t.Fatalf("NewAgent 失败: %v", err)
	}
	if a.GetObservability() != nil {
		t.Error("未配置可观测能力时不应装配 CorrelationStore")
	}
}

// TestCorrelationStoreExplicitOverridesAuto 显式注入优先，且保留自定义 retention。
func TestCorrelationStoreExplicitOverridesAuto(t *testing.T) {
	cs := observability.NewCorrelationStore(observability.WithMaxRecords(5))
	a, err := NewAgent("obs-agent", "", llm.NewMockLLM(t),
		WithMetrics(&capTestMetricsRecorder{}),
		WithCorrelationStore(cs),
	)
	if err != nil {
		t.Fatalf("NewAgent 失败: %v", err)
	}
	if got := a.GetObservability(); got != cs {
		t.Errorf("显式注入的 CorrelationStore 应优先生效，got %T", got)
	}
}

// TestCorrelationStoreAutoWiredIsBounded 自动装配的存储必须有界（避免长驻进程内存无界）。
func TestCorrelationStoreAutoWiredIsBounded(t *testing.T) {
	a, err := NewAgent("obs-agent", "", llm.NewMockLLM(t), WithMetrics(&capTestMetricsRecorder{}))
	if err != nil {
		t.Fatalf("NewAgent 失败: %v", err)
	}
	cs := a.GetObservability()
	if cs == nil {
		t.Fatal("应已装配")
	}
	if cs.MaxRecords() != observability.DefaultMaxRecords() {
		t.Errorf("自动装配的 retention = %d, want %d", cs.MaxRecords(), observability.DefaultMaxRecords())
	}
	if cs.MaxRecords() <= 0 {
		t.Fatal("retention 必须为正")
	}
}
