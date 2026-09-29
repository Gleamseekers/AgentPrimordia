package agent

import (
	"context"

	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/agent/learning"
	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/agent/planning"
	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/agent/reflection"
	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/agent/tool_learning"
	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/audit"
	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/llm"
	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/memory"
	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/observability"
	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/persist"
	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/tools"
)

// OutputGuard 是输出端 Guardrail 检查函数类型（避免 agent → guardrail 反向依赖）
// 返回值：
//   - sanitized: 脱敏后的内容（空字符串表示无变化）
//   - blocked: 是否拒绝（如 reject 动作）
//   - err: 检查过程中的错误
//
// 使用示例（外部 guardrail 包适配）：
//
//	adapter := func(content string) (string, bool, error) {
//	    report, err := engine.CheckOutput(content)
//	    if err != nil { return "", false, err }
//	    if report.Action == ActionReject { return "", true, nil }
//	    if report.Action == ActionSanitize && len(report.Results) > 0 {
//	        return report.Results[len(report.Results)-1].Sanitized, false, nil
//	    }
//	    return "", false, nil
//	}
//	agent.WithOutputGuard(adapter)
type OutputGuard func(content string) (sanitized string, blocked bool, err error)

// InputGuard 是输入端 Guardrail 检查函数类型（user 输入检查，v3.4-4）。
// 返回值语义与 OutputGuard 一致：
//   - sanitized: 脱敏后的输入（空字符串表示无变化）
//   - blocked: 是否拒绝输入（如注入攻击拦截）
//   - err: 检查过程中的错误
type InputGuard func(content string) (sanitized string, blocked bool, err error)

// InputGuardCapable 标识 Agent 具备输入端护栏能力（v3.4-4）。
type InputGuardCapable interface {
	GetInputGuard() InputGuard
}

// AuditEvent 审计事件结构——统一为 internal/audit.Event 的别名
// （修复评估报告 §六.2：此前 agent/observability/audit 三份重复模型；
// audit 包仅依赖标准库，agent→audit 无循环依赖）。
type AuditEvent = audit.Event

// AuditLogger 审计日志接口（agent 内部使用）
// audit.Logger 实现了此接口
type AuditLogger interface {
	Log(ctx context.Context, event AuditEvent) error
}

// 此文件定义 Capable 接口协议，实现协议式微内核架构。
//
// 设计理念：借鉴 Go 标准库的接口发现模式（io.Reader、http.Handler），
// 让能力成为接口而非配置字段。ReAct 引擎通过类型断言自动发现 Agent
// 实现了哪些 Capable 接口，从而启用对应能力。
//
// 使用方式：
//
//	agent := NewReActAgent(ReActConfig{...}).WithMemory(mem).WithRAG(ragCfg)
//	// 引擎自动发现 agent 实现了 MemoryCapable 和 RAGCapable
//	// 无需在 config 中声明"我要用 RAG"

// MemoryCapable 标识 Agent 具备记忆存储能力。
// ReAct 引擎在每轮结束后自动保存对话到 MemoryStore。
type MemoryCapable interface {
	GetMemoryStore() MemoryStore
}

// RAGCapable 标识 Agent 具备 RAG 检索能力。
// ReAct 引擎在每轮推理前自动查询知识库并注入上下文。
type RAGCapable interface {
	GetRAGConfig() *RAGConfig
}

// HITLCapable 标识 Agent 具备人机协作能力。
// tool执行前自动检查是否需要人类确认。
type HITLCapable interface {
	GetHITLConfig() *HITLConfig
}

// HookCapable 标识 Agent 具备 Hook 能力。
// 引擎在关键点自动触发注册的 Hook 函数。
type HookCapable interface {
	GetHooks() Hooks
}

// TraceCapable 标识 Agent 具备分布式追踪能力。
// 引擎在 ReAct Loop 关键点自动创建 Span。
type TraceCapable interface {
	GetTracer() Tracer
}

// ObservabilityCapable 标识 Agent 具备全链路关联能力（v3.5-4）。
// 引擎在请求开始时以 trace_id 登记 RequestTrace，并将 Span/审计事件/指标
// 关联到同一 trace_id，实现"单请求可全链路回溯"。
type ObservabilityCapable interface {
	GetObservability() *observability.CorrelationStore
}

// CostCapable 标识 Agent 具备成本追踪能力。
// 引擎自动记录每轮 LLM Usage 并追踪成本。
type CostCapable interface {
	GetCostTracker() *CostTracker
}

// ContextWindowCapable 标识 Agent 具备上下文窗口裁剪能力。
// 引擎在 LLM 调用前自动裁剪历史消息。
type ContextWindowCapable interface {
	GetContextWindowStrategy() ContextWindowStrategy
}

// EventCapable 标识 Agent 具备事件发布能力。
// 引擎在生命周期关键点自动发布事件。
type EventCapable interface {
	GetEventPublisher() EventPublisher
}

// MetricsCapable 标识 Agent 具备指标记录能力。
// 引擎自动收集 LLM 调用、tool调用等指标。
type MetricsCapable interface {
	GetMetricsRecorder() MetricsRecorder
}

// CheckpointCapable 标识 Agent 具备检查点持久化能力。
// 引擎在每轮结束后自动保存 Agent 状态。
type CheckpointCapable interface {
	GetCheckpointStore() persist.CheckpointStore
}

// FailureCapable 标识 Agent 具备失败记录与重放能力（v3.4-6）。
// 引擎在 Run 失败时自动记录 FailureRecord（含失败阶段、错误与可恢复检查点），
// 并可通过 ReplayFailure 从内嵌检查点一键重放定位。
type FailureCapable interface {
	GetFailureStore() persist.FailureStore
}

// SummarizerCapable 标识 Agent 具备摘要提取能力。
// 引擎在保存记忆后异步提取摘要和标签。
type SummarizerCapable interface {
	GetSummarizer() memory.SummaryExtractor
}

// FileScopeCapable 标识 Agent 具备文件作用域限制能力。
// 引擎在构建系统提示词时自动注入文件权限规则。
type FileScopeCapable interface {
	GetFileScope() []string
}

// CacheCapable 标识 Agent 具备 LLM 缓存能力。
// 引擎自动缓存 Complete 调用结果以减少重复请求。
type CacheCapable interface {
	GetCache() llm.LLMCache
}

// ToolkitCapable 标识 Agent 具备tool注册表能力。
// 引擎通过此接口发现可用的tool定义。
type ToolkitCapable interface {
	GetToolkit() *tools.Registry
}

// PlanningCapable 标识 Agent 具备任务规划能力。
// Agent 可以将复杂任务分解为子任务并生成执行计划。
type PlanningCapable interface {
	GetPlanner() planning.Planner
}

// ReflectionCapable 标识 Agent 具备自我反思能力。
// Agent 可以对输出进行反思、批评和改进。
type ReflectionCapable interface {
	GetReflector() reflection.Reflector
}

// ToolLearningCapable 标识 Agent 具备tool学习能力。
// Agent 可以记录tool使用经验，获取最佳实践，并基于历史经验建议改进。
type ToolLearningCapable interface {
	GetToolLearner() tool_learning.ToolLearner
}

// GuardrailCapable 标识 Agent 具备 Guardrail 防护能力。
// 引擎在 LLM 响应返回后会调用 OutputGuard 函数进行 PII 脱敏、注入拦截等。
type GuardrailCapable interface {
	GetOutputGuard() OutputGuard
}

// AuditLoggerCapable 标识 Agent 具备审计日志能力。
// 引擎在 LLM 调用、tool调用、Agent 启动/停止等关键路径自动写入审计事件。
type AuditLoggerCapable interface {
	GetAuditLogger() AuditLogger
}

// LearningCapable 标识 Agent 具备自适应学习能力。
// 引擎在 Agent 完成推理后自动调用 KnowledgeDistiller 从交互中蒸馏知识，
// 并可通过 FeedbackLearner 记录人类反馈、通过 CapabilityEvolver 评估能力。
type LearningCapable interface {
	GetKnowledgeDistiller() *learning.KnowledgeDistiller
	GetCapabilityEvolver() *learning.CapabilityEvolver
	GetFeedbackLearner() *learning.FeedbackLearner
}
