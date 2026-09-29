package debugger

// 本文件从 visual_editor.go 拆分而来，包含 VisualEditorServer 的 HTTP 处理逻辑。

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/Gleamseekers/AgentPrimordia/internal/orchestration"
)

type VisualEditorServer struct {
	editor *VisualEditor
	mux    *http.ServeMux
}

// NewVisualEditorServer 创建可视化编辑器服务器
func NewVisualEditorServer(editor *VisualEditor) *VisualEditorServer {
	s := &VisualEditorServer{
		editor: editor,
		mux:    http.NewServeMux(),
	}

	// API路由
	s.mux.HandleFunc("/api/editor/configs", s.handleConfigs)
	s.mux.HandleFunc("/api/editor/config/", s.handleConfig)
	s.mux.HandleFunc("/api/editor/execute/", s.handleExecute)
	s.mux.HandleFunc("/api/editor/executions", s.handleExecutions)
	s.mux.HandleFunc("/api/editor/execution/", s.handleExecution)

	// Web UI
	s.mux.HandleFunc("/editor", s.handleEditorUI)

	return s
}

// Handler 返回HTTP处理器
func (s *VisualEditorServer) Handler() http.Handler {
	return s.mux
}

// handleConfigs 处理配置列表
func (s *VisualEditorServer) handleConfigs(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.listConfigs(w, r)
	case http.MethodPost:
		s.createConfig(w, r)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// listConfigs 列出所有配置
func (s *VisualEditorServer) listConfigs(w http.ResponseWriter, r *http.Request) {
	s.editor.mu.RLock()
	defer s.editor.mu.RUnlock()

	configs := make([]*EditorConfig, 0, len(s.editor.configs))
	for _, cfg := range s.editor.configs {
		configs = append(configs, cfg)
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(configs)
}

// createConfig 创建新配置
func (s *VisualEditorServer) createConfig(w http.ResponseWriter, r *http.Request) {
	var cfg EditorConfig
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		http.Error(w, "Invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}

	if cfg.ID == "" {
		cfg.ID = generateID()
	}
	cfg.CreatedAt = time.Now()
	cfg.UpdatedAt = time.Now()

	s.editor.mu.Lock()
	s.editor.configs[cfg.ID] = &cfg
	s.editor.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(cfg)
}

// handleConfig 处理单个配置
func (s *VisualEditorServer) handleConfig(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Path[len("/api/editor/config/"):]
	if id == "" {
		http.Error(w, "Config ID required", http.StatusBadRequest)
		return
	}

	switch r.Method {
	case http.MethodGet:
		s.getConfig(w, r, id)
	case http.MethodPut:
		s.updateConfig(w, r, id)
	case http.MethodDelete:
		s.deleteConfig(w, r, id)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// getConfig 获取配置
func (s *VisualEditorServer) getConfig(w http.ResponseWriter, r *http.Request, id string) {
	s.editor.mu.RLock()
	cfg, exists := s.editor.configs[id]
	s.editor.mu.RUnlock()

	if !exists {
		http.Error(w, "Config not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(cfg)
}

// updateConfig 更新配置
func (s *VisualEditorServer) updateConfig(w http.ResponseWriter, r *http.Request, id string) {
	s.editor.mu.Lock()
	defer s.editor.mu.Unlock()

	cfg, exists := s.editor.configs[id]
	if !exists {
		http.Error(w, "Config not found", http.StatusNotFound)
		return
	}

	var updated EditorConfig
	if err := json.NewDecoder(r.Body).Decode(&updated); err != nil {
		http.Error(w, "Invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}

	updated.ID = id
	updated.CreatedAt = cfg.CreatedAt
	updated.UpdatedAt = time.Now()

	s.editor.configs[id] = &updated

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(updated)
}

// deleteConfig 删除配置
func (s *VisualEditorServer) deleteConfig(w http.ResponseWriter, r *http.Request, id string) {
	s.editor.mu.Lock()
	defer s.editor.mu.Unlock()

	if _, exists := s.editor.configs[id]; !exists {
		http.Error(w, "Config not found", http.StatusNotFound)
		return
	}

	delete(s.editor.configs, id)
	delete(s.editor.orchestrators, id)

	w.WriteHeader(http.StatusNoContent)
}

// handleExecute 执行编排
func (s *VisualEditorServer) handleExecute(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	id := r.URL.Path[len("/api/editor/execute/"):]
	if id == "" {
		http.Error(w, "Config ID required", http.StatusBadRequest)
		return
	}

	s.editor.mu.RLock()
	_, exists := s.editor.configs[id]
	s.editor.mu.RUnlock()

	if !exists {
		http.Error(w, "Config not found", http.StatusNotFound)
		return
	}

	// 创建执行记录
	execID := generateID()
	execRecord := &ExecutionRecord{
		ID:          execID,
		ConfigID:    id,
		Status:      orchestration.StatusRunning,
		StartTime:   time.Now(),
		StepResults: make(map[string]*orchestration.StepResult),
		FinalOutput: make(map[string]interface{}),
	}

	s.editor.mu.Lock()
	s.editor.executions[execID] = execRecord
	cfg := s.editor.configs[id]
	s.editor.mu.Unlock()

	// 异步执行编排
	go s.editor.executeAsync(execRecord, cfg)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"execution_id": execID,
		"status":       "started",
	})
}

// handleExecutions 处理执行列表
func (s *VisualEditorServer) handleExecutions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	s.editor.mu.RLock()
	defer s.editor.mu.RUnlock()

	executions := make([]*ExecutionRecord, 0, len(s.editor.executions))
	for _, exec := range s.editor.executions {
		executions = append(executions, exec)
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(executions)
}

// handleExecution 处理单个执行
func (s *VisualEditorServer) handleExecution(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	id := r.URL.Path[len("/api/editor/execution/"):]
	if id == "" {
		http.Error(w, "Execution ID required", http.StatusBadRequest)
		return
	}

	// 修复（-race 实测发现）：exec 是指向内部存储的指针，executeAsync 会持锁
	// 就地修改其字段。必须在读锁内完成序列化，不能在锁外 Encode，
	// 否则与写者并发构成数据竞争。
	s.editor.mu.RLock()
	exec, exists := s.editor.executions[id]
	var data []byte
	if exists {
		data, _ = json.Marshal(exec)
	}
	s.editor.mu.RUnlock()

	if !exists {
		http.Error(w, "Execution not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(data)
}

// handleEditorUI 提供编辑器Web UI
func (s *VisualEditorServer) handleEditorUI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(editorHTML))
}

// editorHTML 编辑器HTML（使用React Flow）
const editorHTML = `<!DOCTYPE html>
<html lang="zh-CN">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>AP Visual Editor - 可视化编排tool</title>
    <style>
        * {
            margin: 0;
            padding: 0;
            box-sizing: border-box;
        }
        
        body {
            font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif;
            background: #f5f7fa;
        }
        
        .header {
            background: linear-gradient(135deg, #667eea 0%, #764ba2 100%);
            color: white;
            padding: 20px 40px;
            box-shadow: 0 2px 10px rgba(0,0,0,0.1);
        }
        
        .header h1 {
            font-size: 28px;
            font-weight: 600;
        }
        
        .header p {
            margin-top: 5px;
            opacity: 0.9;
            font-size: 14px;
        }
        
        .container {
            display: flex;
            height: calc(100vh - 100px);
        }
        
        .sidebar {
            width: 300px;
            background: white;
            border-right: 1px solid #e0e0e0;
            padding: 20px;
            overflow-y: auto;
        }
        
        .sidebar h2 {
            font-size: 18px;
            margin-bottom: 15px;
            color: #2c3e50;
        }
        
        .node-palette {
            display: grid;
            grid-template-columns: 1fr 1fr;
            gap: 10px;
            margin-bottom: 30px;
        }
        
        .node-item {
            padding: 15px;
            border: 2px solid #e0e0e0;
            border-radius: 8px;
            text-align: center;
            cursor: pointer;
            transition: all 0.2s;
        }
        
        .node-item:hover {
            border-color: #667eea;
            background: #f8f9ff;
        }
        
        .node-item.agent {
            background: #e3f2fd;
            border-color: #2196f3;
        }
        
        .node-item.start {
            background: #e8f5e9;
            border-color: #4caf50;
        }
        
        .node-item.end {
            background: #ffebee;
            border-color: #f44336;
        }
        
        .node-item.condition {
            background: #fff3e0;
            border-color: #ff9800;
        }
        
        .canvas {
            flex: 1;
            background: #fafafa;
            position: relative;
        }
        
        .toolbar {
            position: absolute;
            top: 20px;
            right: 20px;
            display: flex;
            gap: 10px;
            z-index: 10;
        }
        
        .btn {
            padding: 10px 20px;
            border: none;
            border-radius: 6px;
            font-size: 14px;
            font-weight: 500;
            cursor: pointer;
            transition: all 0.2s;
        }
        
        .btn-primary {
            background: #667eea;
            color: white;
        }
        
        .btn-primary:hover {
            background: #5568d3;
        }
        
        .btn-secondary {
            background: white;
            color: #667eea;
            border: 2px solid #667eea;
        }
        
        .btn-secondary:hover {
            background: #f8f9ff;
        }
        
        .config-list {
            margin-top: 20px;
        }
        
        .config-item {
            padding: 12px;
            border: 1px solid #e0e0e0;
            border-radius: 6px;
            margin-bottom: 10px;
            cursor: pointer;
            transition: all 0.2s;
        }
        
        .config-item:hover {
            border-color: #667eea;
            background: #f8f9ff;
        }
        
        .config-item.active {
            border-color: #667eea;
            background: #e8eaff;
        }
        
        .config-name {
            font-weight: 600;
            color: #2c3e50;
            margin-bottom: 5px;
        }
        
        .config-meta {
            font-size: 12px;
            color: #7f8c8d;
        }
        
        .empty-state {
            text-align: center;
            padding: 60px 20px;
            color: #95a5a6;
        }
        
        .empty-state-icon {
            font-size: 64px;
            margin-bottom: 20px;
            opacity: 0.3;
        }
        
        /* React Flow 样式 */
        .react-flow {
            width: 100%;
            height: 100%;
        }
        
        .react-flow__node {
            border-radius: 8px;
            padding: 10px;
            box-shadow: 0 2px 8px rgba(0,0,0,0.1);
        }
        
        .react-flow__node-agent {
            background: #e3f2fd;
            border: 2px solid #2196f3;
        }
        
        .react-flow__node-start {
            background: #e8f5e9;
            border: 2px solid #4caf50;
        }
        
        .react-flow__node-end {
            background: #ffebee;
            border: 2px solid #f44336;
        }
        
        .react-flow__node-condition {
            background: #fff3e0;
            border: 2px solid #ff9800;
        }
        
        .react-flow__edge-path {
            stroke: #667eea;
            stroke-width: 2;
        }
    </style>
</head>
<body>
    <div class="header">
        <h1>🎨 AP Visual Editor</h1>
        <p>可视化编排tool - 拖拽式创建Agent工作流</p>
    </div>
    
    <div class="container">
        <div class="sidebar">
            <h2>节点类型</h2>
            <div class="node-palette">
                <div class="node-item start" draggable="true" data-type="start">
                    <div>🟢</div>
                    <div>开始</div>
                </div>
                <div class="node-item agent" draggable="true" data-type="agent">
                    <div>🤖</div>
                    <div>Agent</div>
                </div>
                <div class="node-item condition" draggable="true" data-type="condition">
                    <div>🔀</div>
                    <div>条件</div>
                </div>
                <div class="node-item end" draggable="true" data-type="end">
                    <div>🔴</div>
                    <div>结束</div>
                </div>
            </div>
            
            <h2>工作流配置</h2>
            <div class="config-list" id="config-list">
                <div class="empty-state">
                    <div class="empty-state-icon">📋</div>
                    <p>暂无配置</p>
                    <p style="font-size: 13px; margin-top: 10px;">点击"新建工作流"开始创建</p>
                </div>
            </div>
        </div>
        
        <div class="canvas">
            <div class="toolbar">
                <button class="btn btn-secondary" onclick="newWorkflow()">新建工作流</button>
                <button class="btn btn-primary" onclick="saveWorkflow()">保存</button>
                <button class="btn btn-primary" onclick="executeWorkflow()">执行</button>
            </div>
            
            <div id="react-flow-container" class="react-flow">
                <div class="empty-state">
                    <div class="empty-state-icon">🎨</div>
                    <p>从左侧拖拽节点到画布</p>
                    <p style="font-size: 13px; margin-top: 10px;">连接节点创建数据流</p>
                </div>
            </div>
        </div>
    </div>
    
    <script>
        // 这里应该引入React Flow库
        // 由于是示例，我们使用简化的实现
        
        let currentConfig = null;
        let nodes = [];
        let edges = [];
        
        // 拖拽开始
        document.querySelectorAll('.node-item').forEach(item => {
            item.addEventListener('dragstart', (e) => {
                e.dataTransfer.setData('type', e.target.dataset.type);
            });
        });
        
        // 拖拽放置
        const canvas = document.querySelector('.canvas');
        canvas.addEventListener('dragover', (e) => {
            e.preventDefault();
        });
        
        canvas.addEventListener('drop', (e) => {
            e.preventDefault();
            const type = e.dataTransfer.getData('type');
            const rect = canvas.getBoundingClientRect();
            const x = e.clientX - rect.left;
            const y = e.clientY - rect.top;
            
            addNode(type, x, y);
        });
        
        function addNode(type, x, y) {
            const node = {
                id: 'node_' + Date.now(),
                type: type,
                position: { x, y },
                data: { label: type.charAt(0).toUpperCase() + type.slice(1) }
            };
            nodes.push(node);
            renderNodes();
        }
        
        function renderNodes() {
            const container = document.getElementById('react-flow-container');
            container.innerHTML = nodes.map(node => 
                '<div class="react-flow__node react-flow__node-' + node.type + '" ' +
                'style="position: absolute; left: ' + node.position.x + 'px; top: ' + node.position.y + 'px;">' +
                node.data.label +
                '</div>'
            ).join('');
        }
        
        function newWorkflow() {
            currentConfig = {
                id: 'wf_' + Date.now(),
                name: '新工作流',
                description: '',
                mode: 'dag',
                nodes: [],
                edges: [],
                created_at: new Date().toISOString(),
                updated_at: new Date().toISOString()
            };
            nodes = [];
            edges = [];
            renderNodes();
        }
        
        async function saveWorkflow() {
            if (!currentConfig) {
                alert('请先创建新工作流');
                return;
            }
            
            currentConfig.nodes = nodes;
            currentConfig.edges = edges;
            
            try {
                const response = await fetch('/api/editor/configs', {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify(currentConfig)
                });
                
                if (response.ok) {
                    alert('保存成功');
                    loadConfigs();
                } else {
                    alert('保存失败');
                }
            } catch (error) {
                console.error('保存失败:', error);
                alert('保存失败: ' + error.message);
            }
        }
        
        async function executeWorkflow() {
            if (!currentConfig) {
                alert('请先保存工作流');
                return;
            }
            
            try {
                const response = await fetch('/api/editor/execute/' + currentConfig.id, {
                    method: 'POST'
                });
                
                if (response.ok) {
                    const result = await response.json();
                    alert('执行已启动，ID: ' + result.execution_id);
                } else {
                    alert('执行失败');
                }
            } catch (error) {
                console.error('执行失败:', error);
                alert('执行失败: ' + error.message);
            }
        }
        
        async function loadConfigs() {
            try {
                const response = await fetch('/api/editor/configs');
                const configs = await response.json();
                
                const list = document.getElementById('config-list');
                if (configs.length === 0) {
                    list.innerHTML = '<div class="empty-state">' +
                        '<div class="empty-state-icon">📋</div>' +
                        '<p>暂无配置</p>' +
                        '</div>';
                    return;
                }
                
                list.innerHTML = configs.map(cfg => 
                    '<div class="config-item" onclick="loadConfig(\'' + cfg.id + '\')">' +
                    '<div class="config-name">' + cfg.name + '</div>' +
                    '<div class="config-meta">' + cfg.nodes.length + ' 节点 | ' + 
                    new Date(cfg.updated_at).toLocaleString('zh-CN') + '</div>' +
                    '</div>'
                ).join('');
            } catch (error) {
                console.error('加载配置失败:', error);
            }
        }
        
        async function loadConfig(id) {
            try {
                const response = await fetch('/api/editor/config/' + id);
                if (response.ok) {
                    currentConfig = await response.json();
                    nodes = currentConfig.nodes || [];
                    edges = currentConfig.edges || [];
                    renderNodes();
                }
            } catch (error) {
                console.error('加载配置失败:', error);
            }
        }
        
        // 初始加载
        loadConfigs();
    </script>
</body>
</html>`
