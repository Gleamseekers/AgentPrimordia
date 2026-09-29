package orchestration

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/cmd/example/demo"
	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/agent"
)

func TestCollaboration_DebateMode(t *testing.T) {
	session := NewCollaborationSession(CollaborationConfig{
		Mode:           DebateMode,
		Name:           "TestDebate",
		MaxRounds:      2,
		EnableCritique: true,
	})

	proAgent, err := agent.NewAgent("ProAgent", "你是支持方，请提出支持论点", demo.NewDemoLLM("我强烈支持这个观点，因为..."), agent.WithMaxTurns(1))
	if err != nil {
		t.Fatal(err)
	}

	conAgent, err := agent.NewAgent("ConAgent", "你是反对方，请提出反对论点", demo.NewDemoLLM("我反对这个观点，理由是..."), agent.WithMaxTurns(1))
	if err != nil {
		t.Fatal(err)
	}

	_ = session.AddCollaborator(&Collaborator{
		ID:          "pro",
		Name:        "支持者",
		Role:        "debater",
		Agent:       proAgent,
		Perspective: "支持",
		Weight:      1.0,
	})

	_ = session.AddCollaborator(&Collaborator{
		ID:          "con",
		Name:        "反对者",
		Role:        "debater",
		Agent:       conAgent,
		Perspective: "反对",
		Weight:      1.0,
	})

	result, err := session.Execute(context.Background(), "是否应该实施远程办公政策？")
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	if result.Status != CollabStatusCompleted {
		t.Errorf("expected completed, got %s", result.Status)
	}
	if len(result.Rounds) != 2 {
		t.Errorf("expected 2 rounds, got %d", len(result.Rounds))
	}
	if result.FinalOutcome == nil {
		t.Fatal("expected final outcome")
	}

	totalStatements := 0
	for _, round := range result.Rounds {
		totalStatements += len(round.Statements)
	}
	if totalStatements < 2 {
		t.Errorf("expected at least 4 statements, got %d", totalStatements)
	}

	t.Logf("✅ Debate Mode: status=%s rounds=%d statements=%d", result.Status, len(result.Rounds), totalStatements)
	t.Logf("   Agreement Level: %.2f", result.FinalOutcome.AgreementLevel)
}

func TestCollaboration_ReviewMode(t *testing.T) {
	session := NewCollaborationSession(CollaborationConfig{
		Mode:      ReviewMode,
		Name:      "CodeReview",
		MaxRounds: 1,
	})

	reviewer1, err := agent.NewAgent("Reviewer1", "你是高级代码审查员", demo.NewDemoLLM("代码整体结构良好，但需要注意错误处理"), agent.WithMaxTurns(1))
	if err != nil {
		t.Fatal(err)
	}

	reviewer2, err := agent.NewAgent("Reviewer2", "你是安全专家", demo.NewDemoLLM("存在潜在的安全漏洞，建议加强输入验证"), agent.WithMaxTurns(1))
	if err != nil {
		t.Fatal(err)
	}

	_ = session.AddCollaborator(&Collaborator{
		ID:    "rev1",
		Name:  "代码审查员",
		Role:  "reviewer",
		Agent: reviewer1,
	})

	_ = session.AddCollaborator(&Collaborator{
		ID:    "rev2",
		Name:  "安全专家",
		Role:  "reviewer",
		Agent: reviewer2,
	})

	sampleContent := `func processData(input string) (string, error) {
	result := process(input)
	return result, nil
}`

	result, err := session.Execute(context.Background(), sampleContent)
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	if result.Status != CollabStatusCompleted {
		t.Errorf("expected completed, got %s", result.Status)
	}
	if result.FinalOutcome.Type != "review_summary" {
		t.Errorf("expected review_summary type, got %s", result.FinalOutcome.Type)
	}

	t.Logf("✅ Review Mode: status=%s type=%s", result.Status, result.FinalOutcome.Type)
	t.Logf("   Review Content length: %d", len(result.FinalOutcome.Content))
}

func TestCollaboration_ConsensusMode(t *testing.T) {
	session := NewCollaborationSession(CollaborationConfig{
		Mode:            ConsensusMode,
		Name:            "TechDecision",
		MaxRounds:       2,
		VotingThreshold: 0.6,
		SaveHistory:     true,
	})

	techLead, err := agent.NewAgent("TechLead", "你是技术负责人", demo.NewDemoLLM("我选择方案A，因为它性能更好"), agent.WithMaxTurns(1))
	if err != nil {
		t.Fatal(err)
	}

	architect, err := agent.NewAgent("Architect", "你是架构师", demo.NewDemoLLM("我倾向于方案B，因为可扩展性更强"), agent.WithMaxTurns(1))
	if err != nil {
		t.Fatal(err)
	}

	devOps, err := agent.NewAgent("DevOps", "你是运维工程师", demo.NewDemoLLM("方案A更容易部署和维护"), agent.WithMaxTurns(1))
	if err != nil {
		t.Fatal(err)
	}

	_ = session.AddCollaborator(&Collaborator{
		ID:     "lead",
		Name:   "技术负责人",
		Role:   "voter",
		Agent:  techLead,
		Weight: 1.5,
	})

	_ = session.AddCollaborator(&Collaborator{
		ID:     "arch",
		Name:   "架构师",
		Role:   "voter",
		Agent:  architect,
		Weight: 1.2,
	})

	_ = session.AddCollaborator(&Collaborator{
		ID:     "ops",
		Name:   "运维",
		Role:   "voter",
		Agent:  devOps,
		Weight: 1.0,
	})

	result, err := session.Execute(context.Background(), "选择数据库技术栈：PostgreSQL vs MongoDB")
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	if result.Status != CollabStatusCompleted {
		t.Errorf("expected completed, got %s", result.Status)
	}
	if result.FinalOutcome.Winner == nil {
		t.Fatal("expected consensus winner")
	}

	winnerDesc := result.FinalOutcome.Winner.Description
	if len(winnerDesc) > 50 {
		winnerDesc = winnerDesc[:50]
	}

	t.Logf("✅ Consensus Mode: status=%s winner=%s score=%.1f%%",
		result.Status, winnerDesc, result.FinalOutcome.Winner.Score)
	t.Logf("   Agreement Level: %.2f", result.FinalOutcome.AgreementLevel)
}

func TestCollaboration_BrainstormMode(t *testing.T) {
	session := NewCollaborationSession(CollaborationConfig{
		Mode:      BrainstormMode,
		Name:      "ProductIdeas",
		MaxRounds: 1,
	})

	ideaGen1, err := agent.NewAgent("Creative1", "你是创意总监", demo.NewDemoLLM("想法1: AI驱动的个性化推荐系统\n想法2: 增强现实购物体验\n想法3: 社交化产品评论"), agent.WithMaxTurns(1))
	if err != nil {
		t.Fatal(err)
	}

	ideaGen2, err := agent.NewAgent("Creative2", "你是用户体验设计师", demo.NewDemoLLM("想法A: 游戏化用户激励体系\n想法B: 智能客服聊天机器人\n想法C: 语音交互界面"), agent.WithMaxTurns(1))
	if err != nil {
		t.Fatal(err)
	}

	_ = session.AddCollaborator(&Collaborator{
		ID:          "creative1",
		Name:        "创意总监",
		Role:        "ideator",
		Agent:       ideaGen1,
		Perspective: "创新导向",
	})

	_ = session.AddCollaborator(&Collaborator{
		ID:          "creative2",
		Name:        "UX设计师",
		Role:        "ideator",
		Agent:       ideaGen2,
		Perspective: "用户体验",
	})

	result, err := session.Execute(context.Background(), "为电商平台生成新功能创意")
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	if result.Status != CollabStatusCompleted {
		t.Errorf("expected completed, got %s", result.Status)
	}
	if result.FinalOutcome.Type != "brainstorm_collection" {
		t.Errorf("expected brainstorm_collection, got %s", result.FinalOutcome.Type)
	}

	ideasCount := countStrings(result.FinalOutcome.Content, "\n")
	if ideasCount < 3 {
		t.Errorf("expected at least 3 ideas, found around %d", ideasCount)
	}

	t.Logf("✅ Brainstorm Mode: status=%s ideas≈%d", result.Status, ideasCount)
}

func TestCollaboration_Events(t *testing.T) {
	session := NewCollaborationSession(CollaborationConfig{
		Mode:      DebateMode,
		MaxRounds: 1,
	})

	testAgent, err := agent.NewAgent("EventTester", "", demo.NewDemoLLM("test response"), agent.WithMaxTurns(1))
	if err != nil {
		t.Fatal(err)
	}

	_ = session.AddCollaborator(&Collaborator{
		ID:    "tester",
		Name:  "测试者",
		Agent: testAgent,
	})

	// 修复（-race 实测发现）：events 被收集 goroutine 与主 goroutine 共享，
	// append 与 range 需互斥
	var eventsMu sync.Mutex
	events := make([]*CollaborationEvent, 0)
	go func() {
		for event := range session.Events() {
			eventsMu.Lock()
			events = append(events, event)
			eventsMu.Unlock()
			if event.Type == "session_completed" || len(events) >= 5 {
				break
			}
		}
	}()

	_, _ = session.Execute(context.Background(), "事件测试")

	time.Sleep(50 * time.Millisecond)

	hasStartEvent := false
	hasCompleteEvent := false

	eventsMu.Lock()
	for _, e := range events {
		switch e.Type {
		case "session_started":
			hasStartEvent = true
		case "session_completed":
			hasCompleteEvent = true
		}
	}
	eventsMu.Unlock()

	if !hasStartEvent || !hasCompleteEvent {
		t.Errorf("missing lifecycle events: start=%v complete=%v", hasStartEvent, hasCompleteEvent)
	}

	t.Logf("✅ Events: collected %d events", len(events))
	for _, e := range events {
		t.Logf("   - %s", e.Type)
	}
}

func TestCollaboration_Export(t *testing.T) {
	session := NewCollaborationSession(CollaborationConfig{
		Mode:        DebateMode,
		Name:        "ExportTest",
		Description: "导出测试会话",
		MaxRounds:   1,
	})

	testAgent, err := agent.NewAgent("ExportAgent", "", demo.NewDemoLLM("export content"), agent.WithMaxTurns(1))
	if err != nil {
		t.Fatal(err)
	}

	_ = session.AddCollaborator(&Collaborator{
		ID:    "exp",
		Name:  "Export",
		Agent: testAgent,
	})

	_, _ = session.Execute(context.Background(), "导出测试")

	data, err := session.Export()
	if err != nil {
		t.Fatalf("Export error: %v", err)
	}

	var exported map[string]any
	_ = json.Unmarshal(data, &exported)

	if exported["config"] == nil {
		t.Error("missing config in export")
	}
	if exported["result"] == nil {
		t.Error("missing result in export")
	}

	t.Logf("✅ Export: size=%d bytes", len(data))

	formattedJSON, _ := json.MarshalIndent(json.RawMessage(data), "", "  ")
	preview := string(formattedJSON)
	if len(preview) > 300 {
		preview = preview[:300]
	}
	t.Logf("   Preview:\n%s", preview)
}

func TestCollaboration_Metrics(t *testing.T) {
	session := NewCollaborationSession(CollaborationConfig{
		Mode:      DebateMode,
		MaxRounds: 2,
	})

	for i := 0; i < 3; i++ {
		idx := i
		a, err := agent.NewAgent(fmt.Sprintf("MetricsAgent-%d", idx), "", demo.NewDemoLLM(fmt.Sprintf("response-%d", idx)), agent.WithMaxTurns(1))
		if err != nil {
			t.Fatal(err)
		}
		_ = session.AddCollaborator(&Collaborator{
			ID:    fmt.Sprintf("m%d", idx),
			Name:  fmt.Sprintf("Agent-%d", idx),
			Agent: a,
		})
	}

	result, _ := session.Execute(context.Background(), "指标测试")

	metrics := result.Metrics
	if metrics.TotalRounds != 2 {
		t.Errorf("expected 2 rounds, got %d", metrics.TotalRounds)
	}
	if metrics.TotalStatements == 0 {
		t.Error("expected some statements")
	}

	metricsJSON, _ := json.MarshalIndent(metrics, "", "  ")
	t.Logf("✅ Metrics:\n%s", string(metricsJSON))
}

func TestCollaboration_Timeout(t *testing.T) {
	session := NewCollaborationSession(CollaborationConfig{
		Mode:    DebateMode,
		Timeout: 100 * time.Millisecond,
	})

	slowAgent, err := agent.NewAgent("SlowAgent", "", demo.NewDemoLLM("slow"), agent.WithMaxTurns(5))
	if err != nil {
		t.Fatal(err)
	}

	_ = session.AddCollaborator(&Collaborator{
		ID:    "slow",
		Name:  "慢速",
		Agent: slowAgent,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	result, err := session.Execute(ctx, "超时测试")

	if err != nil {
		t.Logf("⚠️ Timeout test: error (may be expected): %v", err)
	} else if result != nil {
		t.Logf("✅ Timeout test: completed in time, status=%s", result.Status)
	}
}

func BenchmarkCollaboration_Debate(b *testing.B) {
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			i++
			session := NewCollaborationSession(CollaborationConfig{
				Mode:      DebateMode,
				MaxRounds: 1,
			})

			a, err := agent.NewAgent(fmt.Sprintf("Bench-%d", i), "", demo.NewDemoLLM("bench"), agent.WithMaxTurns(1))
			if err != nil {
				b.Fatal(err)
			}

			_ = session.AddCollaborator(&Collaborator{
				ID:    fmt.Sprintf("b%d", i),
				Agent: a,
			})

			_, _ = session.Execute(context.Background(), "benchmark topic")
		}
	})
}

// 辅助函数
func countStrings(text, sep string) int {
	count := 0
	start := 0
	for {
		idx := findSubstring(text, sep, start)
		if idx == -1 {
			break
		}
		count++
		start = idx + len(sep)
	}
	return count + 1
}

func findSubstring(s, substr string, start int) int {
	if start >= len(s) {
		return -1
	}
	for i := start; i <= len(s)-len(substr); i++ {
		found := true
		for j := 0; j < len(substr); j++ {
			if s[i+j] != substr[j] {
				found = false
				break
			}
		}
		if found {
			return i
		}
	}
	return -1
}
