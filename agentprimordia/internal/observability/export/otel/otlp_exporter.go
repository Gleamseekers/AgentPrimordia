package otel

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/Gleamseekers/AgentPrimordia/internal/agent"
	"github.com/Gleamseekers/AgentPrimordia/internal/metrics"
)

const (
	defaultOTLPMaxRetry    = 3
	defaultOTLPHTTPTimeout = 10 * time.Second
)

// OTLPConfig OTLP 导出器配置
type OTLPConfig struct {
	Endpoint    string
	Headers     map[string]string
	MaxRetry    int
	HTTPTimeout time.Duration
}

// OTLPExporter OTLP HTTP/JSON 导出器（零外部依赖）
type OTLPExporter struct {
	config     OTLPConfig
	httpClient *http.Client
	logger     *slog.Logger
}

// NewOTLPExporter 创建 OTLP 导出器
func NewOTLPExporter(config OTLPConfig) *OTLPExporter {
	if config.MaxRetry <= 0 {
		config.MaxRetry = defaultOTLPMaxRetry
	}
	if config.HTTPTimeout <= 0 {
		config.HTTPTimeout = defaultOTLPHTTPTimeout
	}
	return &OTLPExporter{
		config:     config,
		httpClient: &http.Client{Timeout: config.HTTPTimeout},
		logger:     slog.Default(),
	}
}

// ExportTraces 导出 Trace 数据到 OTLP 端点
func (e *OTLPExporter) ExportTraces(tracer *agent.LoggingTracer) error {
	payload := e.buildTracePayload(tracer)
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal traces: %w", err)
	}
	return e.send("/v1/traces", body)
}

// ExportMetrics 导出 Metrics 数据到 OTLP 端点
func (e *OTLPExporter) ExportMetrics(snapshot metrics.MetricsSnapshot) error {
	payload := e.buildMetricsPayload(snapshot)
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal metrics: %w", err)
	}
	return e.send("/v1/metrics", body)
}

// Close 关闭导出器，释放 HTTP 连接
func (e *OTLPExporter) Close() error {
	e.httpClient.CloseIdleConnections()
	return nil
}

func (e *OTLPExporter) send(path string, body []byte) error {
	url := e.config.Endpoint + path
	var lastErr error
	for attempt := 0; attempt <= e.config.MaxRetry; attempt++ {
		if attempt > 0 {
			backoff := time.Duration(1<<uint(attempt)) * time.Second
			if backoff > 30*time.Second {
				backoff = 30 * time.Second
			}
			time.Sleep(backoff)
		}
		req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
		if err != nil {
			return fmt.Errorf("create request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
		for k, v := range e.config.Headers {
			req.Header.Set(k, v)
		}
		resp, err := e.httpClient.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		if _, copyErr := io.Copy(io.Discard, resp.Body); copyErr != nil {
			e.logger.Warn("读取响应体失败", "error", copyErr)
		}
		resp.Body.Close()
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return nil
		}
		lastErr = fmt.Errorf("OTLP server returned %d", resp.StatusCode)
		if resp.StatusCode < 500 {
			break
		}
	}
	return lastErr
}

func (e *OTLPExporter) buildTracePayload(tracer *agent.LoggingTracer) map[string]any {
	return map[string]any{
		"resourceSpans": []any{
			map[string]any{
				"scopeSpans": []any{
					map[string]any{
						"spans": e.convertSpans(tracer),
					},
				},
			},
		},
	}
}

func (e *OTLPExporter) convertSpans(tracer *agent.LoggingTracer) []any {
	output := tracer.String()
	if output == "" {
		return []any{}
	}
	lines := strings.Split(strings.TrimSpace(output), "\n")
	var spans []any
	for _, line := range lines {
		if line == "" {
			continue
		}
		spanName := extractSpanName(line)
		spans = append(spans, map[string]any{
			"name":   spanName,
			"status": map[string]any{"code": "STATUS_CODE_OK"},
		})
	}
	return spans
}

func extractSpanName(line string) string {
	afterBracket := line
	if idx := strings.Index(line, "] "); idx >= 0 {
		afterBracket = line[idx+2:]
	}
	if idx := strings.Index(afterBracket, " "); idx >= 0 {
		return afterBracket[:idx]
	}
	return afterBracket
}

func (e *OTLPExporter) buildMetricsPayload(snapshot metrics.MetricsSnapshot) map[string]any {
	return map[string]any{
		"resourceMetrics": []any{
			map[string]any{
				"scopeMetrics": []any{
					map[string]any{
						"metrics": e.convertMetrics(snapshot),
					},
				},
			},
		},
	}
}

func (e *OTLPExporter) convertMetrics(s metrics.MetricsSnapshot) []any {
	return []any{
		e.makeCounter("ap_llm_total_calls", s.LLMTotalCalls),
		e.makeCounter("ap_llm_total_errors", s.LLMTotalErrors),
		e.makeCounter("ap_tool_total_calls", s.ToolTotalCalls),
		e.makeCounter("ap_tool_total_errors", s.ToolTotalErrors),
		e.makeCounter("ap_total_turns", s.TotalTurns),
		e.makeGauge("ap_active_agents", s.ActiveAgents),
		e.makeGauge("ap_pool_queue_length", s.PoolQueueLength),
		e.makeGauge("ap_memory_size_bytes", s.MemorySizeBytes),
	}
}

func (e *OTLPExporter) makeCounter(name string, value int64) map[string]any {
	return map[string]any{
		"name": name,
		"data": map[string]any{"asInt": value, "isMonotonic": true},
	}
}

func (e *OTLPExporter) makeGauge(name string, value int64) map[string]any {
	return map[string]any{
		"name": name,
		"data": map[string]any{"asInt": value},
	}
}
