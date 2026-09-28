// options.go — Functional Options for AgentConfig
// v0.7.0 API 稳定化：提供 Option 函数（标量 + 顶层快捷注入 + 分组注入），
// 让用户通过 NewAgent(name, prompt, model, opts...) 一次性注入所有能力。
package agent

import (
	"agentprimordia/internal/agent/planning"
	"agentprimordia/internal/agent/reflection"
	"agentprimordia/internal/agent/tool_learning"
	"agentprimordia/internal/agent/worldmodel"
	"agentprimordia/internal/llm"
	"agentprimordia/internal/memory"
	"agentprimordia/internal/persist"
	"agentprimordia/internal/tools"
)

// Option 是 AgentConfig 的函数式选项类型。
// 每个 Option 函数接收 *AgentConfig 并按需修改字段，
// 由 NewAgent 在构造时依次应用。
type Option func(*AgentConfig)

// ===== 4 个标量 Option =====

// WithMaxTurns 设置 ReAct 循环的最大迭代次数（默认 50）。
func WithMaxTurns(n int) Option {
	return func(c *AgentConfig) { c.MaxTurns = n }
}

// WithTemperature 设置 LLM 温度参数（默认 0，由 Model 决定）。
func WithTemperature(t float64) Option {
	return func(c *AgentConfig) { c.Temperature = t }
}

// WithSessionID 设置会话 ID，用于跨轮记忆关联。
func WithSessionID(id string) Option {
	return func(c *AgentConfig) { c.SessionID = id }
}

// WithPromptTemplate 设置系统提示词模板，支持 {{.Variable}} 变量注入。
func WithPromptTemplate(t *PromptTemplate) Option {
	return func(c *AgentConfig) { c.PromptTemplate = t }
}

// ===== 14 个顶层快捷注入 Option =====

// WithMemory 设置记忆存储（快捷方式：等价于 WithMemoryConfig 时仅设 Store 字段）。
func WithMemory(m MemoryStore) Option {
	return func(c *AgentConfig) { c.Memory.Store = m }
}

// WithToolkit 设置tool注册表（快捷方式：等价于 WithToolsConfig 时仅设 Registry 字段）。
func WithToolkit(r *tools.Registry) Option {
	return func(c *AgentConfig) { c.Tools.Registry = r }
}

// WithHooks 设置 Hook 管理器（快捷方式：等价于 WithObservability 时仅设 Hooks 字段）。
func WithHooks(h Hooks) Option {
	return func(c *AgentConfig) { c.Observability.Hooks = h }
}

// WithRAG 设置 RAG 检索配置，启用后 Agent 在推理前自动查询知识库。
func WithRAG(cfg RAGConfig) Option {
	return func(c *AgentConfig) { c.RAG = cfg }
}

// WithTracer 设置分布式追踪器（快捷方式：等价于 WithObservability 时仅设 Tracer 字段）。
func WithTracer(t Tracer) Option {
	return func(c *AgentConfig) { c.Observability.Tracer = t }
}

// WithInputGuard 设置输入端护栏（v3.4-4）：用户输入进入循环前检查。
func WithInputGuard(g InputGuard) Option {
	return func(c *AgentConfig) { c.Observability.InputGuard = g }
}

// WithCostTracker 设置成本追踪器（快捷方式：等价于 WithObservability 时仅设 CostTracker 字段）。
func WithCostTracker(ct *CostTracker) Option {
	return func(c *AgentConfig) { c.Observability.CostTracker = ct }
}

// WithContextWindow 设置上下文窗口裁剪策略（快捷方式：等价于 WithResilience 时仅设 ContextWindow 字段）。
func WithContextWindow(cw ContextWindowStrategy) Option {
	return func(c *AgentConfig) { c.Resilience.ContextWindow = cw }
}

// WithEvents 设置事件发布器（快捷方式：等价于 WithObservability 时仅设 Events 字段）。
func WithEvents(ep EventPublisher) Option {
	return func(c *AgentConfig) { c.Observability.Events = ep }
}

// WithMetrics 设置指标收集器（快捷方式：等价于 WithObservability 时仅设 Metrics 字段）。
func WithMetrics(m MetricsRecorder) Option {
	return func(c *AgentConfig) { c.Observability.Metrics = m }
}

// WithCheckpointStore 设置状态持久化存储（快捷方式：等价于 WithResilience 时仅设 CheckpointStore 字段）。
func WithCheckpointStore(cs persist.CheckpointStore) Option {
	return func(c *AgentConfig) { c.Resilience.CheckpointStore = cs }
}

// WithFailureStore 设置失败记录存储（快捷方式：等价于 WithResilience 时仅设 FailureStore 字段）。
// 注入后引擎在 Run 失败时自动记录 FailureRecord（含失败阶段与可恢复检查点），
// 并可通过 ReplayFailure 一键重放定位（v3.4-6）。
func WithFailureStore(fs persist.FailureStore) Option {
	return func(c *AgentConfig) { c.Resilience.FailureStore = fs }
}

// WithSummarizer 设置记忆摘要生成器（快捷方式：等价于 WithMemoryConfig 时仅设 Summarizer 字段）。
func WithSummarizer(s memory.SummaryExtractor) Option {
	return func(c *AgentConfig) { c.Memory.Summarizer = s }
}

// WithFileScope 设置文件范围限制（快捷方式：等价于 WithMemoryConfig 时仅设 FileScope 字段）。
func WithFileScope(scopes []string) Option {
	return func(c *AgentConfig) { c.Memory.FileScope = scopes }
}

// WithCache 设置 LLM 响应缓存（快捷方式：等价于 WithResilience 时仅设 Cache 字段）。
func WithCache(cache llm.LLMCache) Option {
	return func(c *AgentConfig) { c.Resilience.Cache = cache }
}

// WithHITL 设置人机协作配置（快捷方式：等价于 WithResilience 时仅设 HITL 字段）。
func WithHITL(cfg *HITLConfig) Option {
	return func(c *AgentConfig) { c.Resilience.HITL = cfg }
}

// WithPlanner 设置任务规划器（快捷方式：等价于 WithCognition 时仅设 Planner 字段）。
// 注入后 ReAct 引擎在首轮对复杂任务自动分解为子任务，按依赖图分层执行。
func WithPlanner(p planning.Planner) Option {
	return func(c *AgentConfig) { c.Cognition.Planner = p }
}

// WithReflector 设置反思器（快捷方式：等价于 WithCognition 时仅设 Reflector 字段）。
// 注入后完成路径上的最终输出会先经过批评，严重度达到阈值时自动改进。
func WithReflector(r reflection.Reflector) Option {
	return func(c *AgentConfig) { c.Cognition.Reflector = r }
}

// WithReflectionThreshold 设置触发 Reflection 改进的最低严重度
// （快捷方式：等价于 WithCognition 时仅设 ReflectionSeverityThreshold 字段）。
// 取值 low/medium/high/critical，空值时引擎默认 high。
func WithReflectionThreshold(severity string) Option {
	return func(c *AgentConfig) { c.Cognition.ReflectionSeverityThreshold = severity }
}

// WithWorldModel 启用世界模型状态图（v6.1 opt-in，默认关闭；提案 §2.1）。
// 注入后 ReAct 循环把任务世界增量维护为结构化状态图：工具调用/观察落图、
// 上下文裁剪消息转为事实节点、预演门与回溯差异以观察模式写失败库+审计。
// 不调用时默认 ReAct 行为零变更（铁律 7）；「评价线默认开」「v7.0 翻默认」
// 分属提案 §2.2/§2.3，均不改变本选项的用户侧缺省语义。
// tracker 允许 nil（等价不启用，便于条件装配）。
func WithWorldModel(t *worldmodel.WorldModelTracker) Option {
	return func(c *AgentConfig) { c.Cognition.WorldModel = t }
}

// WithToolLearner 显式注入工具学习器（v7.4 接线，优先于自动装配）。
// 不注入且记忆存储具备会话列举能力时，buildAgent 会自动装配
// 基于该存储的 MemoryToolLearner；两者都不满足时回注钩子不生效。
// 允许 nil（等价不启用，便于条件装配）。
func WithToolLearner(tl tool_learning.ToolLearner) Option {
	return func(c *AgentConfig) { c.Cognition.ToolLearner = tl }
}

// ===== 4 个分组注入 Option =====

// WithMemoryConfig 整体设置记忆能力分组配置（Store / Summarizer / FileScope）。
func WithMemoryConfig(cfg MemoryConfig) Option {
	return func(c *AgentConfig) { c.Memory = cfg }
}

// WithObservability 整体设置可观测性分组配置（Hooks / Tracer / Metrics / Events / CostTracker）。
func WithObservability(cfg ObservabilityConfig) Option {
	return func(c *AgentConfig) { c.Observability = cfg }
}

// WithResilience 整体设置韧性分组配置（CheckpointStore / HITL / Cache / ContextWindow）。
func WithResilience(cfg ResilienceConfig) Option {
	return func(c *AgentConfig) { c.Resilience = cfg }
}

// WithToolsConfig 整体设置tool系统分组配置（Registry）。
func WithToolsConfig(cfg ToolsConfig) Option {
	return func(c *AgentConfig) { c.Tools = cfg }
}

// WithLearning 整体设置自适应学习能力分组配置（Distiller / Evolver / FeedbackLearner）。
// 注入后引擎在 Agent 完成推理后自动从交互中蒸馏知识。
//
// 使用方式：
//
//	agent, _ := NewAgent("bot", "prompt", provider,
//	    WithLearning(LearningConfig{
//	        Distiller: learning.NewKnowledgeDistiller(),
//	    }),
//	)
func WithLearning(cfg LearningConfig) Option {
	return func(c *AgentConfig) { c.Learning = cfg }
}

// WithCognition 整体设置认知能力分组配置（Planner / Reflector / ReflectionSeverityThreshold）。
//
// 使用方式：
//
//	agent, _ := NewAgent("bot", "prompt", provider,
//	    WithCognition(CognitionConfig{
//	        Planner:   planning.NewLLMPlanner(provider),
//	        Reflector: reflection.NewLLMReflector(provider),
//	    }),
//	)
func WithCognition(cfg CognitionConfig) Option {
	return func(c *AgentConfig) { c.Cognition = cfg }
}
