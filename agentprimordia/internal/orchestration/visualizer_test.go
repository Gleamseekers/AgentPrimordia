package orchestration

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Gleamseekers/AgentPrimordia/internal/agent"
)

func TestVisualizer_DAGExport(t *testing.T) {
	dag := agent.NewDAGWorkflow().WithName("test-wf")
	_ = dag.AddNode(&agent.DAGNode{ID: "step-1", Metadata: map[string]string{"label": "第一步"}})
	_ = dag.AddNode(&agent.DAGNode{ID: "step-2", Metadata: map[string]string{"label": "第二步"}})
	_ = dag.AddEdge(agent.DAGEdge{From: "step-1", To: "step-2"})

	v := NewVisualizer(dag)
	export := v.ExportJSON()

	var result map[string]any
	if err := json.Unmarshal([]byte(export), &result); err != nil {
		t.Fatalf("JSON 解析失败: %v", err)
	}

	nodes, ok := result["nodes"].([]any)
	if !ok || len(nodes) != 2 {
		t.Errorf("nodes = %v, 期望 2 个节点", result["nodes"])
	}

	edges, ok := result["edges"].([]any)
	if !ok || len(edges) != 1 {
		t.Errorf("edges = %v, 期望 1 条边", result["edges"])
	}
}

func TestVisualizer_EditorEndpoint(t *testing.T) {
	dag := agent.NewDAGWorkflow().WithName("test-wf")
	_ = dag.AddNode(&agent.DAGNode{ID: "step-1", Metadata: map[string]string{"label": "第一步"}})

	v := NewVisualizer(dag)
	handler := v.EditorHandler()

	req := httptest.NewRequest("GET", "/editor", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("状态码 = %d, 期望 %d", w.Code, http.StatusOK)
	}

	ct := w.Header().Get("Content-Type")
	if ct != "text/html; charset=utf-8" {
		t.Errorf("Content-Type = %q, 期望 text/html", ct)
	}

	body := w.Body.String()
	if len(body) < 100 {
		t.Error("HTML 内容过短")
	}
}
