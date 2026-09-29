// react_capabilities.go — 协议式微内核接口发现 + Hook/Event 辅助
// 引擎通过 a.self.(XxxCapable) 检测能力，优先使用接口发现，回退到 config 字段
package agent

import (
	"context"
	"time"

	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/agent/learning"
	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/agent/planning"
	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/agent/reflection"
	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/agent/tool_learning"
	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/memory"
	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/observability"
	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/persist"
	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/tools"
)

// initSelf 初始化自引用，必须在构造后调用（因为需要返回值赋值后再设置）
func (a *ReActAgent) initSelf() {
	if a.self == nil {
		a.self = a
	}
}

// ===== 协议式微内核：接口发现辅助方法 =====
// 引擎通过 a.self.(XxxCapable) 检测能力，优先使用接口发现，回退到 config 字段

// getMemoryStore 获取记忆存储，通过 MemoryCapable 接口发现
func (a *ReActAgent) getMemoryStore() MemoryStore {
	if c, ok := a.self.(MemoryCapable); ok {
		return c.GetMemoryStore()
	}
	return nil
}

// getRAGConfig 获取 RAG 配置，通过 RAGCapable 接口发现
func (a *ReActAgent) getRAGConfig() *RAGConfig {
	if c, ok := a.self.(RAGCapable); ok {
		return c.GetRAGConfig()
	}
	return nil
}

// getEventPublisher 获取事件发布器，通过 EventCapable 接口发现
func (a *ReActAgent) getEventPublisher() EventPublisher {
	if c, ok := a.self.(EventCapable); ok {
		return c.GetEventPublisher()
	}
	return nil
}

// getMetricsRecorder 获取指标记录器，通过 MetricsCapable 接口发现
func (a *ReActAgent) getMetricsRecorder() MetricsRecorder {
	if c, ok := a.self.(MetricsCapable); ok {
		return c.GetMetricsRecorder()
	}
	return nil
}

// getLabeledRecorder 尔回带标签维度的 MetricsRecorder（可能为 nil）
func (a *ReActAgent) getLabeledRecorder() LabeledMetricsRecorder {
	if m := a.getMetricsRecorder(); m != nil {
		if lm, ok := m.(LabeledMetricsRecorder); ok {
			return lm
		}
	}
	return nil
}

// recordLLM 记录 LLM 调用，优先使用带标签的记录器（内部已调用 RecordLLMCall）
// 优化（Task 2）：当 capCache 可用时使用缓存的 provider/model，避免重复 Model.Info() 调用。
// v3.5-4：同时将指标关联到当前请求的 trace_id（全链路闭环）。
func (a *ReActAgent) recordLLM(duration time.Duration, err error) {
	if a.capCache != nil && a.capCache.labeledRecorder != nil {
		a.capCache.labeledRecorder.RecordLLMCallWithLabels(duration, err, a.capCache.provider, a.capCache.model)
	} else if lm := a.getLabeledRecorder(); lm != nil {
		provider, model := "", ""
		if info := a.config.Model.Info(); info.Name != "" {
			provider = info.Provider
			model = info.Name
		}
		lm.RecordLLMCallWithLabels(duration, err, provider, model)
	} else if m := a.getMetricsRecorder(); m != nil {
		m.RecordLLMCall(duration, err)
	}
	a.recordLLMObservability(duration, err)
}

// recordLLMObservability 将 LLM 指标关联到当前请求 trace（仅统计成功调用）。
func (a *ReActAgent) recordLLMObservability(duration time.Duration, err error) {
	if a.capCache == nil || a.capCache.observability == nil || a.capCache.traceID == "" || err != nil {
		return
	}
	a.capCache.observability.RecordLLM(a.capCache.traceID, duration, 0, 0, 0)
}

// recordTool 记录tool调用，优先使用带标签的记录器（内部已调用 RecordToolCall）
// 优化（Task 2）：使用缓存的 labeledRecorder。
// v3.5-4：同时将指标关联到当前请求的 trace_id。
func (a *ReActAgent) recordTool(duration time.Duration, err error, toolName string) {
	if a.capCache != nil && a.capCache.labeledRecorder != nil {
		a.capCache.labeledRecorder.RecordToolCallWithLabels(duration, err, toolName)
	} else if lm := a.getLabeledRecorder(); lm != nil {
		lm.RecordToolCallWithLabels(duration, err, toolName)
	} else if m := a.getMetricsRecorder(); m != nil {
		m.RecordToolCall(duration, err)
	}
	if a.capCache != nil && a.capCache.observability != nil && a.capCache.traceID != "" && err == nil {
		a.capCache.observability.RecordTool(a.capCache.traceID, duration)
	}
}

// recordTurn 记录 Turn 耗时，优先使用带标签的记录器（内部已调用 RecordTurn）
// 优化（Task 2）：使用缓存的 labeledRecorder。
// v3.5-4：同时将指标关联到当前请求的 trace_id。
func (a *ReActAgent) recordTurn(duration time.Duration) {
	if a.capCache != nil && a.capCache.labeledRecorder != nil {
		a.capCache.labeledRecorder.RecordTurnWithAgent(duration, a.config.Name)
	} else if lm := a.getLabeledRecorder(); lm != nil {
		lm.RecordTurnWithAgent(duration, a.config.Name)
	} else if m := a.getMetricsRecorder(); m != nil {
		m.RecordTurn(duration)
	}
	if a.capCache != nil && a.capCache.observability != nil && a.capCache.traceID != "" {
		a.capCache.observability.RecordTurn(a.capCache.traceID)
	}
}

// getTracer 获取追踪器，通过 TraceCapable 接口发现
func (a *ReActAgent) getTracer() Tracer {
	if c, ok := a.self.(TraceCapable); ok {
		return c.GetTracer()
	}
	return nil
}

// getObservability 获取全链路关联存储，通过 ObservabilityCapable 接口发现（v3.5-4）
func (a *ReActAgent) getObservability() *observability.CorrelationStore {
	if c, ok := a.self.(ObservabilityCapable); ok {
		return c.GetObservability()
	}
	return nil
}

// getCostTracker 获取成本追踪器，通过 CostCapable 接口发现
func (a *ReActAgent) getCostTracker() *CostTracker {
	if c, ok := a.self.(CostCapable); ok {
		return c.GetCostTracker()
	}
	return nil
}

// getCheckpointStore 获取检查点存储，通过 CheckpointCapable 接口发现
func (a *ReActAgent) getCheckpointStore() persist.CheckpointStore {
	if c, ok := a.self.(CheckpointCapable); ok {
		return c.GetCheckpointStore()
	}
	return nil
}

// getFailureStore 获取失败记录存储，通过 FailureCapable 接口发现（v3.4-6）
func (a *ReActAgent) getFailureStore() persist.FailureStore {
	if c, ok := a.self.(FailureCapable); ok {
		return c.GetFailureStore()
	}
	return nil
}

// getContextWindowStrategy 获取上下文窗口策略，通过 ContextWindowCapable 接口发现
func (a *ReActAgent) getContextWindowStrategy() ContextWindowStrategy {
	if c, ok := a.self.(ContextWindowCapable); ok {
		return c.GetContextWindowStrategy()
	}
	return nil
}

// getSummarizer 获取摘要提取器，通过 SummarizerCapable 接口发现
func (a *ReActAgent) getSummarizer() memory.SummaryExtractor {
	if c, ok := a.self.(SummarizerCapable); ok {
		return c.GetSummarizer()
	}
	return nil
}

// getFileScope 获取文件作用域，通过 FileScopeCapable 接口发现
func (a *ReActAgent) getFileScope() []string {
	if c, ok := a.self.(FileScopeCapable); ok {
		return c.GetFileScope()
	}
	return nil
}

// getToolkit 获取tool注册表，通过 ToolkitCapable 接口发现
func (a *ReActAgent) getToolkit() *tools.Registry {
	if c, ok := a.self.(ToolkitCapable); ok {
		return c.GetToolkit()
	}
	return nil
}

// getOutputGuard 获取输出端 Guardrail 检查函数，通过 GuardrailCapable 接口发现
// 用于在 LLM 响应后调用（PII 脱敏、注入拦截等）
func (a *ReActAgent) getOutputGuard() OutputGuard {
	if c, ok := a.self.(GuardrailCapable); ok {
		return c.GetOutputGuard()
	}
	return nil
}

// getInputGuard 获取输入端 Guardrail 检查函数，通过 InputGuardCapable 接口发现
// 用于在用户输入进入循环前调用（v3.4-4）
func (a *ReActAgent) getInputGuard() InputGuard {
	if c, ok := a.self.(InputGuardCapable); ok {
		return c.GetInputGuard()
	}
	return nil
}

// getAuditLogger 获取审计日志器，通过 AuditLoggerCapable 接口发现
// 用于在 LLM 调用、tool调用、Agent 启动/停止时写入审计事件
func (a *ReActAgent) getAuditLogger() AuditLogger {
	if c, ok := a.self.(AuditLoggerCapable); ok {
		return c.GetAuditLogger()
	}
	return nil
}

// getPlanner 获取任务规划器，通过 PlanningCapable 接口发现
// R1.2：G1-1 Planning 接入的发现入口
func (a *ReActAgent) getPlanner() planning.Planner {
	if c, ok := a.self.(PlanningCapable); ok {
		return c.GetPlanner()
	}
	return nil
}

// getReflector 获取反思器，通过 ReflectionCapable 接口发现
// R1.2：G1-2 Reflection 接入的发现入口
func (a *ReActAgent) getReflector() reflection.Reflector {
	if c, ok := a.self.(ReflectionCapable); ok {
		return c.GetReflector()
	}
	return nil
}

// getToolLearner 获取tool学习器，通过 ToolLearningCapable 接口发现
// R1.2：G1-3 ToolLearning 接入的发现入口
func (a *ReActAgent) getToolLearner() tool_learning.ToolLearner {
	if c, ok := a.self.(ToolLearningCapable); ok {
		return c.GetToolLearner()
	}
	return nil
}

// getKnowledgeDistiller 获取知识蒸馏器，通过 LearningCapable 接口发现
// v3.0：自适应学习接入的发现入口
func (a *ReActAgent) getKnowledgeDistiller() *learning.KnowledgeDistiller {
	if c, ok := a.self.(LearningCapable); ok {
		return c.GetKnowledgeDistiller()
	}
	return nil
}

// getCapabilityEvolver 获取能力进化器，通过 LearningCapable 接口发现
// v3.0：自适应学习接入的发现入口
func (a *ReActAgent) getCapabilityEvolver() *learning.CapabilityEvolver {
	if c, ok := a.self.(LearningCapable); ok {
		return c.GetCapabilityEvolver()
	}
	return nil
}

// getFeedbackLearner 获取反馈学习器，通过 LearningCapable 接口发现
// v3.0：自适应学习接入的发现入口
func (a *ReActAgent) getFeedbackLearner() *learning.FeedbackLearner {
	if c, ok := a.self.(LearningCapable); ok {
		return c.GetFeedbackLearner()
	}
	return nil
}

func (a *ReActAgent) fireHook(point HookPoint, hctx *HookContext) error {
	if a.hooks != nil {
		hctx.Point = point
		// 优化（perf-v3）：优先使用 capCache 中的 requestID，避免每次 fireHook 都获取互斥锁
		if hctx.RequestID == "" {
			if a.capCache != nil {
				hctx.RequestID = a.capCache.requestID
			} else {
				a.mu.Lock()
				hctx.RequestID = a.currentRequestID
				a.mu.Unlock()
			}
		}
		if hctx.AgentID == "" {
			hctx.AgentID = a.config.Name
		}
		// 使用 agent 的运行 context 而非 Background，确保取消能传播到 hook
		// P1-2：经访问器在 mu 保护下读取，避免与入口写入竞争
		ctx := a.getHookCtx()
		if ctx == nil {
			ctx = context.Background()
		}
		if err := a.hooks.Fire(ctx, hctx); err != nil {
			a.logger.Warn("Hook 执行失败", "point", point, "error", err)
			return err
		}
	}
	return nil
}

// fireHookWithPool 是 fireHook 的 sync.Pool 优化版本。
// 仅使用 Turn 字段时优先使用此方法，可避免每次 fireHook 都分配一个 HookContext。
// 内部从 hookContextPool 获取、设置必要字段、用完后归还。
func (a *ReActAgent) fireHookWithPool(point HookPoint, turn int) error {
	if a.hooks == nil {
		return nil
	}
	hctx := AcquireHookContext()
	defer ReleaseHookContext(hctx)
	hctx.Turn = turn
	return a.fireHook(point, hctx)
}

// fireHookWithPoolResp 是 fireHook 的 sync.Pool 优化版本，用于传递 Response 字段。
func (a *ReActAgent) fireHookWithPoolResp(point HookPoint, resp *Response) error {
	if a.hooks == nil {
		return nil
	}
	hctx := AcquireHookContext()
	defer ReleaseHookContext(hctx)
	hctx.Response = resp
	return a.fireHook(point, hctx)
}

// fireHookWithPoolErr 是 fireHook 的 sync.Pool 优化版本，用于传递 Error 字段。
func (a *ReActAgent) fireHookWithPoolErr(point HookPoint, err error) error {
	if a.hooks == nil {
		return nil
	}
	hctx := AcquireHookContext()
	defer ReleaseHookContext(hctx)
	hctx.Error = err
	return a.fireHook(point, hctx)
}

// hasEventSubscriber 快速检查是否有事件订阅者，用于避免在热点路径上构造 payload map。
// 优化（Task 3）：通过 capCache 直接检查，避免接口断言和 map 分配。
func (a *ReActAgent) hasEventSubscriber() bool {
	if a.capCache != nil {
		return a.capCache.eventPublisher != nil
	}
	return a.getEventPublisher() != nil
}

// publishEvent 向 EventPublisher 发布事件，自动注入 request_id
// 优化（Task 3）：在确认订阅者存在前不做任何工作；通过 capCache 缓存 EventPublisher
// 以避免每次重复类型断言。
func (a *ReActAgent) publishEvent(eventType string, payload any) {
	// 优先使用 capCache 中的 EventPublisher（一次 Run 期间不变）
	ep := EventPublisher(nil)
	if a.capCache != nil {
		ep = a.capCache.eventPublisher
	} else {
		ep = a.getEventPublisher()
	}
	if ep == nil {
		return
	}
	// 如果 payload 是 map[string]string，注入 request_id
	// 优化：避免在调用前预先构造大 map；当订阅者存在时才构造
	if m, ok := payload.(map[string]string); ok {
		// 仅当存在订阅者时填充 request_id（已通过 ep != nil 短路）
		if a.capCache != nil {
			m["request_id"] = a.capCache.requestID
		} else {
			a.mu.Lock()
			reqID := a.currentRequestID
			a.mu.Unlock()
			m["request_id"] = reqID
		}
	}
	if err := ep.PublishAsync(eventType, a.config.Name, payload); err != nil {
		a.logger.Warn("发布事件失败", "error", err, "type", eventType)
	}
}
