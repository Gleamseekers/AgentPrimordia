package agent

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/agent/tool_learning"
	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/llm"
	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/memory"
	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/tools"
)

// wiringEchoTool 端到端测试用的最小工具。
type wiringEchoTool struct{}

func (wiringEchoTool) Name() string        { return "echo" }
func (wiringEchoTool) Description() string { return "echo" }
func (wiringEchoTool) Parameters() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{}}`)
}
func (wiringEchoTool) Execute(_ context.Context, _ json.RawMessage) (*tools.Result, error) {
	return tools.NewResult("echo ok"), nil
}

// TestToolLearningRecordedDuringRun 端到端：Agent 真实执行工具后，
// 工具使用经验必须落入记忆存储——这是"回注闭环真的生效"的判据，
// 而非仅仅 GetToolLearner() 非 nil。
func TestToolLearningRecordedDuringRun(t *testing.T) {
	store := memory.NewInMemoryStore()
	mock := llm.NewMockLLM(t)
	mock.WithToolResponse([]llm.FunctionCall{{ID: "c1", Name: "echo", Arguments: "{}"}})
	mock.WithResponse("done")

	reg := tools.NewRegistry()
	if err := reg.Register(wiringEchoTool{}); err != nil {
		t.Fatalf("注册工具失败: %v", err)
	}

	a, err := NewAgent("tl-run", "", mock,
		WithMaxTurns(5), WithMemory(store), WithToolkit(reg))
	if err != nil {
		t.Fatalf("NewAgent 失败: %v", err)
	}
	if _, err := a.Run(context.Background(), UserMessage("hi")); err != nil {
		t.Fatalf("Run 失败: %v", err)
	}

	eps, err := store.List(context.Background(), &memory.ListOptions{
		SessionID: "tool_learning", Limit: 100,
	})
	if err != nil {
		t.Fatalf("列举工具学习记录失败: %v", err)
	}
	if len(eps) == 0 {
		t.Fatal("工具执行后未落库任何 tool_learning 记录——回注闭环未生效")
	}
	if eps[0].Metadata["tool_name"] != "echo" {
		t.Errorf("落库记录的 tool_name = %q, want echo", eps[0].Metadata["tool_name"])
	}
}

// addOnlyMemoryStore 只实现 agent.MemoryStore（Add/UpdateSummary），
// 不具备会话列举能力——用于验证"能力不足时不装配工具学习器"。
type addOnlyMemoryStore struct{}

func (s *addOnlyMemoryStore) Add(ctx context.Context, episode *memory.Episode) error { return nil }
func (s *addOnlyMemoryStore) UpdateSummary(ctx context.Context, id, summary, topics string) error {
	return nil
}

// TestToolLearnerAutoWiredFromMemoryStore 回归：记忆存储具备会话列举能力时，
// 工具学习器必须被自动装配——此前 NewMemoryToolLearner 无任何生产构造点，
// 导致 react_loop_tools.go 的回注钩子恒为 nil、ToolLearning 闭环不生效。
func TestToolLearnerAutoWiredFromMemoryStore(t *testing.T) {
	store := memory.NewInMemoryStore()
	a, err := NewAgent("tl-agent", "", llm.NewMockLLM(t), WithMemory(store))
	if err != nil {
		t.Fatalf("NewAgent 失败: %v", err)
	}
	learner := a.GetToolLearner()
	if learner == nil {
		t.Fatal("具备会话列举能力的记忆存储应自动装配 ToolLearner（实际为 nil）")
	}

	ctx := context.Background()
	for _, args := range []string{`{"path":"/a"}`, `{"path":"/b"}`} {
		if err := learner.RecordSuccess(ctx, "read_file", args, "ok"); err != nil {
			t.Fatalf("RecordSuccess 失败: %v", err)
		}
	}
	if err := learner.RecordFailure(ctx, "read_file", `{"path":"/bad"}`, "boom"); err != nil {
		t.Fatalf("RecordFailure 失败: %v", err)
	}
	// 另一工具的记录不得混入
	if err := learner.RecordSuccess(ctx, "write_file", `{"path":"/c"}`, "ok"); err != nil {
		t.Fatalf("RecordSuccess 失败: %v", err)
	}

	practices, err := learner.GetBestPractices(ctx, "read_file")
	if err != nil {
		t.Fatalf("GetBestPractices 失败: %v", err)
	}
	if len(practices) != 1 {
		t.Fatalf("read_file 应有 1 条最佳实践，实际 %d", len(practices))
	}
	if got, want := practices[0].SuccessRate, 2.0/3.0; got != want {
		t.Errorf("成功率 = %v, want %v", got, want)
	}

	others, err := learner.GetBestPractices(ctx, "write_file")
	if err != nil {
		t.Fatalf("GetBestPractices 失败: %v", err)
	}
	if len(others) != 1 || others[0].SuccessRate != 1.0 {
		t.Errorf("write_file 应独立聚合（1 条、成功率 1.0），实际 %+v", others)
	}
}

// TestToolLearnerNotWiredWithoutSessionListing 能力不足时不得装配（避免运行期静默失败）。
func TestToolLearnerNotWiredWithoutSessionListing(t *testing.T) {
	a, err := NewAgent("tl-agent", "", llm.NewMockLLM(t), WithMemory(&addOnlyMemoryStore{}))
	if err != nil {
		t.Fatalf("NewAgent 失败: %v", err)
	}
	if a.GetToolLearner() != nil {
		t.Error("仅具备 Add/UpdateSummary 的记忆存储不应自动装配 ToolLearner")
	}
}

// TestToolLearnerExplicitOverridesAuto 显式注入优先于自动装配。
func TestToolLearnerExplicitOverridesAuto(t *testing.T) {
	custom := tool_learning.NewMemoryToolLearner(newToolLearningMemoryAdapter(memory.NewInMemoryStore()))
	a, err := NewAgent("tl-agent", "", llm.NewMockLLM(t),
		WithMemory(memory.NewInMemoryStore()),
		WithToolLearner(custom),
	)
	if err != nil {
		t.Fatalf("NewAgent 失败: %v", err)
	}
	if got := a.GetToolLearner(); got != custom {
		t.Errorf("显式注入的 ToolLearner 应优先生效，got %T", got)
	}
}

// TestToolLearningAdapterMetadataFilter 校验适配器的 Episode 转换与 metadata 过滤语义。
func TestToolLearningAdapterMetadataFilter(t *testing.T) {
	store := memory.NewInMemoryStore()
	ad := newToolLearningMemoryAdapter(store)
	ctx := context.Background()

	add := func(id, session, tool string) {
		t.Helper()
		if err := ad.Add(ctx, &tool_learning.Episode{
			ID:        id,
			SessionID: session,
			Role:      "tool_usage",
			Content:   `{"tool_name":"` + tool + `"}`,
			CreatedAt: "2026-09-27T00:00:00Z",
			Metadata:  map[string]string{"tool_name": tool},
		}); err != nil {
			t.Fatalf("Add 失败: %v", err)
		}
	}
	add("1", "tool_learning", "a")
	add("2", "tool_learning", "b")
	add("3", "other", "a")

	got, err := ad.Query(ctx, "tool_learning", map[string]string{"tool_name": "a"})
	if err != nil {
		t.Fatalf("Query 失败: %v", err)
	}
	if len(got) != 1 || got[0].ID != "1" {
		t.Errorf("metadata+session 过滤结果 = %+v, want 仅 id=1", got)
	}

	all, err := ad.Query(ctx, "tool_learning", nil)
	if err != nil {
		t.Fatalf("Query 失败: %v", err)
	}
	if len(all) != 2 {
		t.Errorf("session 过滤结果数 = %d, want 2", len(all))
	}

	// 往返转换保真
	if all[0].SessionID != "tool_learning" || all[0].Role != "tool_usage" {
		t.Errorf("Episode 往返字段丢失: %+v", all[0])
	}
}
