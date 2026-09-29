# Metrics API

> Prometheus 指标收集：Agent 运行、LLM 调用、工具执行、Memory 操作全覆盖。

## 指标清单

### Agent 指标

| 指标 | 类型 | 说明 |
|------|------|------|
| `ap_agent_runs_total` | Counter | Agent 运行总次数 |
| `ap_agent_run_duration_seconds` | Histogram | 单次运行耗时 |
| `ap_agent_turns_per_run` | Histogram | 每次运行的 ReAct 轮次 |
| `ap_agent_errors_total` | Counter | 错误次数（按 error_type 拆分） |

### LLM 指标

| 指标 | 类型 | 说明 |
|------|------|------|
| `ap_llm_requests_total` | Counter | LLM 请求总次数 |
| `ap_llm_tokens_total` | Counter | Token 用量（prompt/completion） |
| `ap_llm_latency_seconds` | Histogram | LLM 响应延迟 |
| `ap_llm_cost_usd` | Counter | 估算费用 |

### 工具指标

| 指标 | 类型 | 说明 |
|------|------|------|
| `ap_tool_invocations_total` | Counter | 工具调用次数 |
| `ap_tool_duration_seconds` | Histogram | 工具执行耗时 |
| `ap_tool_errors_total` | Counter | 工具错误次数 |

### Memory 指标

| 指标 | 类型 | 说明 |
|------|------|------|
| `ap_memory_adds_total` | Counter | 记忆写入次数 |
| `ap_memory_searches_total` | Counter | 搜索次数 |
| `ap_memory_search_latency_seconds` | Histogram | 搜索延迟 |

### Pool 指标

| 指标 | 类型 | 说明 |
|------|------|------|
| `ap_pool_tasks_submitted_total` | Counter | 提交任务总数 |
| `ap_pool_tasks_completed_total` | Counter | 完成任务数 |
| `ap_pool_task_queue_seconds` | Histogram | 任务排队时间 |
| `ap_pool_workers_active` | Gauge | 当前活跃 worker 数 |

## 使用示例

```go
// 注册默认注册表（默认已注入）
import "github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/metrics"

// 读取指标（/metrics 端点由 admin HTTP 服务暴露）
handler := metrics.PrometheusHandler()

// 在自定义代码中暴露
http.Handle("/metrics", handler)
```

## Grafana Dashboard

AgentPrimordia 提供开箱即用的 Grafana Dashboard JSON：`deploy/grafana/dashboard.json`。

导入后可以看到：
- Agent 运行 QPS / P99 延迟
- LLM Token 用量 & 费用趋势
- 工具调用热力图
- 错误率按类型分布
