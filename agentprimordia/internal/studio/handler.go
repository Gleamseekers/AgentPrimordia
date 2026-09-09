// handler.go — Studio /api/v1/* HTTP 端点实现
package studio

import (
	"context"
	"encoding/json"
	"net/http"
	"time"
)

// registerRoutes 注册 Studio 面板端点（Go 1.22+ 方法路由）。
func (h *StudioHandler) registerRoutes() {
	// Learning Dashboard (HTML)
	h.mux.HandleFunc("GET /dashboard/learning", h.learningDashboard)
	// Chaos Lab
	h.mux.HandleFunc("GET /api/v1/chaos/experiments", h.listExperiments)
	h.mux.HandleFunc("POST /api/v1/chaos/experiments", h.createExperiment)
	h.mux.HandleFunc("POST /api/v1/chaos/experiments/abort", h.abortExperiment)
	// Cluster Dashboard
	h.mux.HandleFunc("GET /api/v1/cluster/status", h.clusterStatus)
	// Learning Monitor
	h.mux.HandleFunc("GET /api/v1/learning/stats", h.learningStats)
	h.mux.HandleFunc("GET /api/v1/learning/capabilities", h.learningCapabilities)
	h.mux.HandleFunc("GET /api/v1/learning/capability-history", h.learningCapabilityHistory)
	h.mux.HandleFunc("GET /api/v1/learning/pipeline/stats", h.learningPipelineStats)
	// Marketplace
	h.mux.HandleFunc("GET /api/v1/marketplace/templates", h.marketplaceTemplates)
	h.mux.HandleFunc("POST /api/v1/marketplace/deploy", h.marketplaceDeploy)
	h.mux.HandleFunc("GET /api/v1/marketplace/deployments", h.marketplaceDeployments)
	h.mux.HandleFunc("POST /api/v1/marketplace/deployments/{id}/stop", h.marketplaceStopDeployment)
	h.mux.HandleFunc("POST /api/v1/marketplace/deployments/{id}/start", h.marketplaceStartDeployment)
	// Autonomy Monitor (v3.3)
	h.mux.HandleFunc("GET /api/v1/autonomy/goals", h.autonomyGoals)
	h.mux.HandleFunc("GET /api/v1/autonomy/alerts", h.autonomyAlerts)
	h.mux.HandleFunc("POST /api/v1/autonomy/goals/{id}/resume", h.autonomyResume)
	// Skill Library (v3.4)
	h.mux.HandleFunc("GET /api/v1/skills", h.skillsList)
	h.mux.HandleFunc("POST /api/v1/skills/{id}/verify", h.skillsVerify)
	h.mux.HandleFunc("POST /api/v1/skills/{id}/deprecate", h.skillsDeprecate)
	// A2A Interop (v3.5)
	h.mux.HandleFunc("GET /api/v1/a2a/interop/status", h.a2aInteropStatus)
	// Realtime Console (v3.6)
	h.mux.HandleFunc("GET /api/v1/realtime/sessions", h.realtimeSessions)
	h.mux.HandleFunc("GET /api/v1/realtime/events", h.realtimeEvents)
	h.mux.HandleFunc("POST /api/v1/realtime/sessions/{id}/barge-in", h.realtimeBargeIn)
}

// ===== Chaos Lab =====

func (h *StudioHandler) listExperiments(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	items, err := h.chaos.ListExperiments(ctx)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if items == nil {
		items = []ExperimentResult{}
	}
	writeJSON(w, http.StatusOK, items)
}

func (h *StudioHandler) createExperiment(w http.ResponseWriter, r *http.Request) {
	var req CreateExperimentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "请求体解析失败: " + err.Error()})
		return
	}
	if req.Name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "实验名称不能为空"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	if err := h.chaos.CreateExperiment(ctx, req); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"status": "created"})
}

// AbortRequest POST /api/v1/chaos/experiments/abort 请求体。
type AbortRequest struct {
	Name string `json:"name"`
}

func (h *StudioHandler) abortExperiment(w http.ResponseWriter, r *http.Request) {
	var req AbortRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "请求体解析失败: " + err.Error()})
		return
	}
	if req.Name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "实验名称不能为空"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	if err := h.chaos.AbortExperiment(ctx, req.Name); err != nil {
		writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "aborted", "name": req.Name})
}

// ===== Cluster Dashboard =====

func (h *StudioHandler) clusterStatus(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	status, err := h.cluster.Status(ctx)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, status)
}

// ===== Learning Monitor =====

func (h *StudioHandler) learningStats(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	stats, err := h.learning.Stats(ctx)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, stats)
}

func (h *StudioHandler) learningCapabilities(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	caps, err := h.learning.Capabilities(ctx)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if caps == nil {
		caps = []Capability{}
	}
	writeJSON(w, http.StatusOK, caps)
}

func (h *StudioHandler) learningPipelineStats(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	stats, err := h.learning.PipelineStats(ctx)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, stats)
}

func (h *StudioHandler) learningCapabilityHistory(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	history, err := h.learning.CapabilityHistory(ctx)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if history == nil {
		history = []CapabilityHistory{}
	}
	writeJSON(w, http.StatusOK, history)
}

func (h *StudioHandler) learningDashboard(w http.ResponseWriter, r *http.Request) {
	// 嵌入 HTML 文件
	htmlContent := `<!DOCTYPE html>
<html lang="zh-CN">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>AgentPrimordia Studio - 学习可视化</title>
    <style>
        * { margin: 0; padding: 0; box-sizing: border-box; }
        body { font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif; background: #f5f5f5; color: #333; }
        .header { background: linear-gradient(135deg, #667eea 0%, #764ba2 100%); color: white; padding: 2rem; box-shadow: 0 2px 10px rgba(0,0,0,0.1); }
        .header h1 { font-size: 2rem; margin-bottom: 0.5rem; }
        .header p { opacity: 0.9; font-size: 1rem; }
        .container { max-width: 1400px; margin: 0 auto; padding: 2rem; }
        .stats-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(250px, 1fr)); gap: 1.5rem; margin-bottom: 2rem; }
        .stat-card { background: white; border-radius: 12px; padding: 1.5rem; box-shadow: 0 2px 8px rgba(0,0,0,0.1); transition: transform 0.2s; }
        .stat-card:hover { transform: translateY(-4px); box-shadow: 0 4px 16px rgba(0,0,0,0.15); }
        .stat-card h3 { font-size: 0.9rem; color: #666; margin-bottom: 0.5rem; text-transform: uppercase; letter-spacing: 0.5px; }
        .stat-card .value { font-size: 2.5rem; font-weight: bold; color: #667eea; margin-bottom: 0.5rem; }
        .stat-card .label { font-size: 0.9rem; color: #999; }
        .chart-container { background: white; border-radius: 12px; padding: 2rem; margin-bottom: 2rem; box-shadow: 0 2px 8px rgba(0,0,0,0.1); }
        .chart-container h2 { font-size: 1.5rem; margin-bottom: 1.5rem; color: #333; }
        .radar-chart { width: 100%; max-width: 600px; margin: 0 auto; }
        .growth-chart { width: 100%; height: 400px; position: relative; }
        .capabilities-list { display: grid; grid-template-columns: repeat(auto-fill, minmax(300px, 1fr)); gap: 1rem; }
        .capability-item { background: #f9f9f9; border-radius: 8px; padding: 1rem; border-left: 4px solid #667eea; }
        .capability-item h4 { font-size: 1rem; margin-bottom: 0.5rem; color: #333; }
        .capability-item .progress { background: #e0e0e0; border-radius: 4px; height: 8px; overflow: hidden; margin-bottom: 0.5rem; }
        .capability-item .progress-bar { height: 100%; background: linear-gradient(90deg, #667eea 0%, #764ba2 100%); transition: width 0.3s; }
        .capability-item .stats { display: flex; justify-content: space-between; font-size: 0.85rem; color: #666; }
        .trend-up { color: #10b981; }
        .trend-down { color: #ef4444; }
        .trend-stable { color: #6b7280; }
        .loading { text-align: center; padding: 3rem; color: #999; }
        .error { background: #fee; color: #c33; padding: 1rem; border-radius: 8px; margin-bottom: 1rem; }
        canvas { max-width: 100%; }
    </style>
</head>
<body>
    <div class="header">
        <h1>🚀 AgentPrimordia Studio</h1>
        <p>学习可视化面板 — 越用越强</p>
    </div>
    <div class="container">
        <div id="error" class="error" style="display: none;"></div>
        <div class="stats-grid">
            <div class="stat-card">
                <h3>总体成功率</h3>
                <div class="value" id="success-rate">--</div>
                <div class="label" id="success-label">加载中...</div>
            </div>
            <div class="stat-card">
                <h3>总任务数</h3>
                <div class="value" id="total-tasks">--</div>
                <div class="label">已完成任务</div>
            </div>
            <div class="stat-card">
                <h3>能力域数量</h3>
                <div class="value" id="domain-count">--</div>
                <div class="label">已学习领域</div>
            </div>
            <div class="stat-card">
                <h3>平均轮数</h3>
                <div class="value" id="avg-turns">--</div>
                <div class="label">每任务平均</div>
            </div>
        </div>
        <div class="chart-container">
            <h2>📊 能力雷达图</h2>
            <div class="radar-chart"><canvas id="radarChart"></canvas></div>
        </div>
        <div class="chart-container">
            <h2>📈 成长曲线</h2>
            <div class="growth-chart"><canvas id="growthChart"></canvas></div>
        </div>
        <div class="chart-container">
            <h2>🎯 能力详情</h2>
            <div class="capabilities-list" id="capabilities-list">
                <div class="loading">加载中...</div>
            </div>
        </div>
    </div>
    <script src="https://cdn.jsdelivr.net/npm/chart.js@4.4.0/dist/chart.umd.min.js"></script>
    <script>
        let radarChart = null;
        let growthChart = null;
        async function loadData() {
            try {
                const statsResp = await fetch('/api/v1/learning/stats');
                if (!statsResp.ok) throw new Error('加载统计失败');
                const stats = await statsResp.json();
                document.getElementById('success-rate').textContent = (stats.overall_success_rate * 100).toFixed(1) + '%';
                document.getElementById('success-label').textContent = stats.total_successes + '/' + stats.total_tasks + ' 成功';
                document.getElementById('total-tasks').textContent = stats.total_tasks;
                document.getElementById('domain-count').textContent = stats.domain_count || 0;
                document.getElementById('avg-turns').textContent = stats.avg_turns ? stats.avg_turns.toFixed(1) : '0';
                const capsResp = await fetch('/api/v1/learning/capabilities');
                if (!capsResp.ok) throw new Error('加载能力失败');
                const caps = await capsResp.json();
                updateRadarChart(caps);
                updateCapabilitiesList(caps);
                const historyResp = await fetch('/api/v1/learning/capability-history');
                if (!historyResp.ok) throw new Error('加载历史失败');
                const history = await historyResp.json();
                updateGrowthChart(history);
            } catch (error) {
                showError(error.message);
            }
        }
        function updateRadarChart(capabilities) {
            const ctx = document.getElementById('radarChart').getContext('2d');
            const labels = capabilities.map(c => c.domain);
            const data = capabilities.map(c => c.success_rate * 100);
            if (radarChart) radarChart.destroy();
            radarChart = new Chart(ctx, {
                type: 'radar',
                data: { labels: labels, datasets: [{ label: '成功率 (%)', data: data, backgroundColor: 'rgba(102, 126, 234, 0.2)', borderColor: 'rgba(102, 126, 234, 1)', borderWidth: 2, pointBackgroundColor: 'rgba(102, 126, 234, 1)', pointBorderColor: '#fff', pointHoverBackgroundColor: '#fff', pointHoverBorderColor: 'rgba(102, 126, 234, 1)' }] },
                options: { responsive: true, scales: { r: { beginAtZero: true, max: 100, ticks: { stepSize: 20 } } }, plugins: { legend: { display: false } } }
            });
        }
        function updateGrowthChart(history) {
            const ctx = document.getElementById('growthChart').getContext('2d');
            history.sort((a, b) => new Date(a.timestamp) - new Date(b.timestamp));
            const labels = history.map(h => new Date(h.timestamp).toLocaleDateString('zh-CN'));
            const data = history.map(h => h.success_rate * 100);
            if (growthChart) growthChart.destroy();
            growthChart = new Chart(ctx, {
                type: 'line',
                data: { labels: labels, datasets: [{ label: '成功率 (%)', data: data, borderColor: 'rgba(102, 126, 234, 1)', backgroundColor: 'rgba(102, 126, 234, 0.1)', borderWidth: 3, fill: true, tension: 0.4, pointRadius: 5, pointHoverRadius: 7 }] },
                options: { responsive: true, maintainAspectRatio: false, scales: { y: { beginAtZero: true, max: 100, ticks: { callback: function(value) { return value + '%'; } } } }, plugins: { legend: { display: false }, tooltip: { callbacks: { label: function(context) { return '成功率: ' + context.parsed.y.toFixed(1) + '%'; } } } } }
            });
        }
        function updateCapabilitiesList(capabilities) {
            const container = document.getElementById('capabilities-list');
            if (capabilities.length === 0) { container.innerHTML = '<div class="loading">暂无学习数据</div>'; return; }
            capabilities.sort((a, b) => b.success_rate - a.success_rate);
            container.innerHTML = capabilities.map(cap => {
                const trendClass = cap.trend === 'improving' ? 'trend-up' : cap.trend === 'declining' ? 'trend-down' : 'trend-stable';
                const trendIcon = cap.trend === 'improving' ? '↑' : cap.trend === 'declining' ? '↓' : '→';
                return '<div class="capability-item"><h4>' + cap.domain + '</h4><div class="progress"><div class="progress-bar" style="width: ' + (cap.success_rate * 100) + '%"></div></div><div class="stats"><span>' + (cap.success_rate * 100).toFixed(1) + '% (' + cap.successes + '/' + cap.total + ')</span><span class="' + trendClass + '">' + trendIcon + ' ' + cap.trend + '</span></div></div>';
            }).join('');
        }
        function showError(message) {
            const errorDiv = document.getElementById('error');
            errorDiv.textContent = '错误: ' + message;
            errorDiv.style.display = 'block';
        }
        window.addEventListener('load', loadData);
        setInterval(loadData, 30000);
    </script>
</body>
</html>`
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(htmlContent))
}

// ===== Marketplace =====

func (h *StudioHandler) marketplaceTemplates(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("q")
	category := r.URL.Query().Get("category")
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	templates, err := h.marketplace.SearchTemplates(ctx, query, category)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if templates == nil {
		templates = []AgentTemplate{}
	}
	writeJSON(w, http.StatusOK, templates)
}

func (h *StudioHandler) marketplaceDeploy(w http.ResponseWriter, r *http.Request) {
	var req DeployRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "请求体解析失败: " + err.Error()})
		return
	}
	if req.TemplateID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "template_id 不能为空"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	dep, err := h.marketplace.Deploy(ctx, req.TemplateID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, dep)
}

func (h *StudioHandler) marketplaceDeployments(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	deps, err := h.marketplace.ListDeployments(ctx)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if deps == nil {
		deps = []Deployment{}
	}
	writeJSON(w, http.StatusOK, deps)
}

func (h *StudioHandler) marketplaceStopDeployment(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	if err := h.marketplace.StopDeployment(ctx, id); err != nil {
		writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "stopped", "id": id})
}

func (h *StudioHandler) marketplaceStartDeployment(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	if err := h.marketplace.StartDeployment(ctx, id); err != nil {
		writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "running", "id": id})
}

// ===== 工具函数 =====

// writeJSON 输出 JSON 响应。
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
