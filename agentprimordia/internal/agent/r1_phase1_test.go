// r1_phase1_test.go — Phase 1 G1 闭环相关单元测试
// 覆盖 R1.3 (Planning DAG) / R1.4 (Reflection severity gating)
// 这些测试不依赖 LLM/工具注册表，可作为快速回归套件。
package agent

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/Gleamseekers/AgentPrimordia/internal/agent/planning"
	"github.com/Gleamseekers/AgentPrimordia/internal/agent/reflection"
)

// discardLogger 返回丢弃所有输出的 slog.Logger，避免测试时日志噪音
func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// ===== R1.3 G1-1：Planning DAG =====

// TestBuildDependencyGraph_Simple 测试基本依赖图构建
func TestBuildDependencyGraph_Simple(t *testing.T) {
	tasks := []planning.SubTask{
		{ID: "1", Description: "step 1"},
		{ID: "2", Description: "step 2", DependsOn: []string{"1"}},
		{ID: "3", Description: "step 3", DependsOn: []string{"1"}},
	}
	g := buildDependencyGraph(tasks)
	if len(g.allIDs) != 3 {
		t.Errorf("expected 3 nodes, got %d", len(g.allIDs))
	}
	if g.inEdges["1"] != 0 || g.inEdges["2"] != 1 || g.inEdges["3"] != 1 {
		t.Errorf("unexpected inEdges: %v", g.inEdges)
	}
}

// TestBuildDependencyGraph_DanglingDep 测试悬空依赖（指向不存在的 ID）应被跳过
func TestBuildDependencyGraph_DanglingDep(t *testing.T) {
	tasks := []planning.SubTask{
		{ID: "1", Description: "step 1", DependsOn: []string{"ghost"}},
	}
	g := buildDependencyGraph(tasks)
	// 悬空依赖被跳过，所以 1 的入度应为 0
	if g.inEdges["1"] != 0 {
		t.Errorf("dangling dep should be ignored, got inEdges=%d", g.inEdges["1"])
	}
}

// TestTopologicalLayers_Linear 测试线性 DAG（1→2→3）的分层
func TestTopologicalLayers_Linear(t *testing.T) {
	tasks := []planning.SubTask{
		{ID: "1", Description: "a"},
		{ID: "2", Description: "b", DependsOn: []string{"1"}},
		{ID: "3", Description: "c", DependsOn: []string{"2"}},
	}
	g := buildDependencyGraph(tasks)
	layers, err := g.topologicalLayers()
	if err != nil {
		t.Fatalf("topologicalLayers failed: %v", err)
	}
	if len(layers) != 3 {
		t.Errorf("expected 3 layers, got %d", len(layers))
	}
	if layers[0][0].ID != "1" || layers[1][0].ID != "2" || layers[2][0].ID != "3" {
		t.Errorf("layer order wrong: %v", layers)
	}
}

// TestTopologicalLayers_Diamond 测试菱形 DAG（1→{2,3}→4）的分层
func TestTopologicalLayers_Diamond(t *testing.T) {
	tasks := []planning.SubTask{
		{ID: "1", Description: "root"},
		{ID: "2", Description: "left", DependsOn: []string{"1"}},
		{ID: "3", Description: "right", DependsOn: []string{"1"}},
		{ID: "4", Description: "join", DependsOn: []string{"2", "3"}},
	}
	g := buildDependencyGraph(tasks)
	layers, err := g.topologicalLayers()
	if err != nil {
		t.Fatalf("topologicalLayers failed: %v", err)
	}
	if len(layers) != 3 {
		t.Errorf("expected 3 layers (root, [left,right], join), got %d", len(layers))
	}
	if len(layers[1]) != 2 {
		t.Errorf("layer 1 should have 2 parallel-able tasks, got %d", len(layers[1]))
	}
	// 第 0 层只有 root
	if layers[0][0].ID != "1" {
		t.Errorf("layer 0 should contain root (1), got %s", layers[0][0].ID)
	}
	// 第 2 层只有 join
	if layers[2][0].ID != "4" {
		t.Errorf("layer 2 should contain join (4), got %s", layers[2][0].ID)
	}
}

// TestTopologicalLayers_Cycle 测试循环依赖应返回错误
func TestTopologicalLayers_Cycle(t *testing.T) {
	tasks := []planning.SubTask{
		{ID: "1", Description: "a", DependsOn: []string{"2"}},
		{ID: "2", Description: "b", DependsOn: []string{"1"}},
	}
	g := buildDependencyGraph(tasks)
	_, err := g.topologicalLayers()
	if err == nil {
		t.Error("expected cycle detection error, got nil")
	}
}

// TestTopologicalLayers_Independent 多个无依赖任务应在第一层
func TestTopologicalLayers_Independent(t *testing.T) {
	tasks := []planning.SubTask{
		{ID: "1", Description: "a"},
		{ID: "2", Description: "b"},
		{ID: "3", Description: "c"},
	}
	g := buildDependencyGraph(tasks)
	layers, err := g.topologicalLayers()
	if err != nil {
		t.Fatalf("topologicalLayers failed: %v", err)
	}
	if len(layers) != 1 {
		t.Errorf("independent tasks should be 1 layer, got %d", len(layers))
	}
	if len(layers[0]) != 3 {
		t.Errorf("layer 0 should have 3 tasks, got %d", len(layers[0]))
	}
}

// ===== R1.4 G1-2：Reflection severity gating =====

// TestShouldImprove_HighSeverity 测试严重度比较
func TestShouldImprove_HighSeverity(t *testing.T) {
	cases := []struct {
		name     string
		actual   reflection.Severity
		thresh   reflection.Severity
		expected bool
	}{
		{"critical >= high", reflection.SeverityCritical, reflection.SeverityHigh, true},
		{"high >= high", reflection.SeverityHigh, reflection.SeverityHigh, true},
		{"medium < high", reflection.SeverityMedium, reflection.SeverityHigh, false},
		{"low < medium", reflection.SeverityLow, reflection.SeverityMedium, false},
		{"high >= medium", reflection.SeverityHigh, reflection.SeverityMedium, true},
		// 未知 actual 严重度：仅 critical 触发（critical 走 ok1=true 分支）
		{"unknown actual only critical triggers", reflection.Severity("unknown"), reflection.SeverityLow, false},
		// 未知 threshold：降到默认 high，critical >= high = true
		{"unknown threshold defaults to high", reflection.SeverityCritical, reflection.Severity("unknown"), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := shouldImprove(tc.actual, tc.thresh)
			if got != tc.expected {
				t.Errorf("shouldImprove(%s, %s) = %v, want %v", tc.actual, tc.thresh, got, tc.expected)
			}
		})
	}
}

// mockReflector 用于测试 reflectAndImprove
type mockReflector struct {
	critiqueReturn *reflection.Critique
	critiqueErr    error
	improvedReturn string
	improveErr     error
}

func (m *mockReflector) Reflect(ctx context.Context, input, output string) (*reflection.Reflection, error) {
	return &reflection.Reflection{}, nil
}

func (m *mockReflector) Critique(ctx context.Context, output string) (*reflection.Critique, error) {
	return m.critiqueReturn, m.critiqueErr
}

func (m *mockReflector) Improve(ctx context.Context, output string, feedback *reflection.Critique) (string, error) {
	return m.improvedReturn, m.improveErr
}

// TestReflectAndImprove_NilReflector 测试 reflector 为空时直接返回原文
func TestReflectAndImprove_NilReflector(t *testing.T) {
	a := &ReActAgent{config: ReActConfig{Name: "test"}, logger: discardLogger()}
	got, err := a.reflectAndImprove(context.Background(), "original content")
	if err != nil {
		t.Fatalf("reflectAndImprove failed: %v", err)
	}
	if got != "original content" {
		t.Errorf("expected unchanged content, got %q", got)
	}
}

// TestReflectAndImprove_EmptyContent 测试空内容时直接返回
func TestReflectAndImprove_EmptyContent(t *testing.T) {
	a := &ReActAgent{
		config: ReActConfig{Name: "test"},
		logger: discardLogger(),
		capCache: &capabilityCache{
			reflector: &mockReflector{
				critiqueReturn: &reflection.Critique{Severity: reflection.SeverityCritical},
			},
		},
	}
	got, err := a.reflectAndImprove(context.Background(), "")
	if err != nil {
		t.Fatalf("reflectAndImprove failed: %v", err)
	}
	if got != "" {
		t.Errorf("expected empty content, got %q", got)
	}
}

// TestReflectAndImprove_LowSeverityNoImprove 测试低严重度不触发 Improve
func TestReflectAndImprove_LowSeverityNoImprove(t *testing.T) {
	a := &ReActAgent{
		config: ReActConfig{
			Name:                        "test",
			ReflectionSeverityThreshold: string(reflection.SeverityHigh),
		},
		logger: discardLogger(),
		capCache: &capabilityCache{
			reflector: &mockReflector{
				critiqueReturn: &reflection.Critique{Severity: reflection.SeverityLow},
				improvedReturn: "improved",
			},
		},
	}
	got, err := a.reflectAndImprove(context.Background(), "original")
	if err != nil {
		t.Fatalf("reflectAndImprove failed: %v", err)
	}
	if got != "original" {
		t.Errorf("low severity should not trigger improve, got %q", got)
	}
}

// TestReflectAndImprove_HighSeverityTriggersImprove 测试高严重度触发 Improve
func TestReflectAndImprove_HighSeverityTriggersImprove(t *testing.T) {
	a := &ReActAgent{
		config: ReActConfig{
			Name:                        "test",
			ReflectionSeverityThreshold: string(reflection.SeverityHigh),
		},
		logger: discardLogger(),
		capCache: &capabilityCache{
			reflector: &mockReflector{
				critiqueReturn: &reflection.Critique{Severity: reflection.SeverityHigh},
				improvedReturn: "improved output",
			},
		},
	}
	got, err := a.reflectAndImprove(context.Background(), "original")
	if err != nil {
		t.Fatalf("reflectAndImprove failed: %v", err)
	}
	if got != "improved output" {
		t.Errorf("expected improved output, got %q", got)
	}
}

// TestReflectAndImprove_CritiqueError 测试 Critique 失败时降级
func TestReflectAndImprove_CritiqueError(t *testing.T) {
	a := &ReActAgent{
		config: ReActConfig{
			Name:                        "test",
			ReflectionSeverityThreshold: string(reflection.SeverityHigh),
		},
		logger: discardLogger(),
		capCache: &capabilityCache{
			reflector: &mockReflector{
				critiqueErr: errFake("LLM timeout"),
			},
		},
	}
	got, err := a.reflectAndImprove(context.Background(), "original")
	if err != nil {
		t.Fatalf("reflectAndImprove should not propagate error: %v", err)
	}
	if got != "original" {
		t.Errorf("expected original on error, got %q", got)
	}
}

// TestReflectAndImprove_EmptyImproveResult 测试 Improve 返回空字符串时保留原文
func TestReflectAndImprove_EmptyImproveResult(t *testing.T) {
	a := &ReActAgent{
		config: ReActConfig{
			Name:                        "test",
			ReflectionSeverityThreshold: string(reflection.SeverityHigh),
		},
		logger: discardLogger(),
		capCache: &capabilityCache{
			reflector: &mockReflector{
				critiqueReturn: &reflection.Critique{Severity: reflection.SeverityCritical},
				improvedReturn: "   ", // 空白也算空
			},
		},
	}
	got, err := a.reflectAndImprove(context.Background(), "original")
	if err != nil {
		t.Fatalf("reflectAndImprove failed: %v", err)
	}
	if got != "original" {
		t.Errorf("expected original when improve is blank, got %q", got)
	}
}

// ===== 辅助 =====

// errFake 简单的 error 实现，避免 import errors
type errFake string

func (e errFake) Error() string { return string(e) }
