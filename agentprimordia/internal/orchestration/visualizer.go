package orchestration

import (
	_ "embed"
	"encoding/json"
	"net/http"

	"github.com/Gleamseekers/AgentPrimordia/internal/agent"
)

//go:embed static/editor.html
var editorHTML string

// DAGExportNode DAG 导出节点
type DAGExportNode struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// DAGExportEdge DAG 导出边
type DAGExportEdge struct {
	From      string `json:"from"`
	To        string `json:"to"`
	Label     string `json:"label,omitempty"`
	Condition bool   `json:"condition,omitempty"`
}

// DAGExport DAG 的 JSON 导出格式
type DAGExport struct {
	Name  string          `json:"name"`
	Nodes []DAGExportNode `json:"nodes"`
	Edges []DAGExportEdge `json:"edges"`
}

// Visualizer DAG 可视化器
type Visualizer struct {
	dag *agent.DAGWorkflow
}

// NewVisualizer 创建可视化器
func NewVisualizer(dag *agent.DAGWorkflow) *Visualizer {
	return &Visualizer{dag: dag}
}

// ExportJSON 导出 DAG 为 JSON
func (v *Visualizer) ExportJSON() string {
	jsonData := v.dag.ToJSON()

	export := DAGExport{
		Name:  "",
		Nodes: make([]DAGExportNode, 0),
		Edges: make([]DAGExportEdge, 0),
	}

	if name, ok := jsonData["name"].(string); ok {
		export.Name = name
	}

	if nodes, ok := jsonData["nodes"].([]map[string]string); ok {
		for _, n := range nodes {
			label := n["id"]
			if l, ok := n["label"]; ok && l != "" {
				label = l
			}
			export.Nodes = append(export.Nodes, DAGExportNode{
				ID:   n["id"],
				Name: label,
			})
		}
	}

	if edges, ok := jsonData["edges"].([]map[string]string); ok {
		for _, e := range edges {
			export.Edges = append(export.Edges, DAGExportEdge{
				From:  e["from"],
				To:    e["to"],
				Label: e["label"],
			})
		}
	}

	data, _ := json.MarshalIndent(export, "", "  ")
	return string(data)
}

// EditorHandler 返回可视化编辑器 HTTP handler
func (v *Visualizer) EditorHandler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("/editor", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(editorHTML))
	})

	mux.HandleFunc("/api/dag/export", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(v.ExportJSON()))
	})

	return mux
}
