package agent

import (
	"github.com/Gleamseekers/AgentPrimordia/internal/agent/autonomy"
	"github.com/Gleamseekers/AgentPrimordia/internal/agent/learning"
	"github.com/Gleamseekers/AgentPrimordia/internal/agent/planning"
	"github.com/Gleamseekers/AgentPrimordia/internal/agent/realtime"
	"github.com/Gleamseekers/AgentPrimordia/internal/agent/reflection"
	"github.com/Gleamseekers/AgentPrimordia/internal/agent/skills"
	"github.com/Gleamseekers/AgentPrimordia/internal/agent/tool_learning"
	"github.com/Gleamseekers/AgentPrimordia/internal/agent/worldmodel"
	"github.com/Gleamseekers/AgentPrimordia/internal/llm"
	"github.com/Gleamseekers/AgentPrimordia/internal/memory"
	"github.com/Gleamseekers/AgentPrimordia/internal/observability"
	"github.com/Gleamseekers/AgentPrimordia/internal/persist"
	"github.com/Gleamseekers/AgentPrimordia/internal/tools"
	"context"
)

// CapabilityAgent 是可组合能力的 Agent 包装器。
//
// 通过链式 API 按需注入 Memory、RAG、Hook 等能力，实现协议式微内核架构。
// CapabilityAgent 同时实现 Agent 接口和所有 Capable 接口，
// ReAct 引擎通过类型断言自动发现已注入的能力。
//
// 使用方式：
//
//	agent := NewReActAgent(ReActConfig{
//	    Name: "my-agent", Model: provider, MaxTurns: 10,
//	}).WithMemory(mem).WithRAG(RAGConfig{...}).WithHooks(hooks)
type CapabilityAgent struct {
	inner *ReActAgent

	// 可选能力字段，非 nil 即表示已启用
	memory       MemoryStore
	rag          *RAGConfig
	hitl         *HITLConfig
	hooks        Hooks
	tracer       Tracer
	costTracker  *CostTracker
	ctxWindow    ContextWindowStrategy
	eventPub     EventPublisher
	metrics      MetricsRecorder
	checkpoint   persist.CheckpointStore
	failureStore persist.FailureStore
	summarizer   memory.SummaryExtractor
	fileScope    []string
	cache        llm.LLMCache
	toolkit      *tools.Registry
	planner      planning.Planner
	reflector    reflection.Reflector
	toolLearner  tool_learning.ToolLearner
	outputGuard  OutputGuard
	inputGuard   InputGuard
	auditLogger  AuditLogger
	// v3.5-4：全链路关联存储（trace → 指标 → 审计 闭环）
	observability *observability.CorrelationStore

	// v3.0：自适应学习能力
	distiller       *learning.KnowledgeDistiller
	evolver         *learning.CapabilityEvolver
	feedbackLearner *learning.FeedbackLearner

	// v6.1：世界模型（opt-in，提案 §2.1）
	worldModel *worldmodel.WorldModelTracker

	// v3.3-v3.6：自治 / 技能 / 实时能力
	autonomyRT  *autonomy.AutonomyRuntime
	skillStore  *skills.Store
	skillMatch  *skills.Matcher
	realtimeHub *realtime.RealtimeHub
}

// ===== Agent 接口委托 =====

// Run 执行同步推理，委托给内部 ReActAgent
func (c *CapabilityAgent) Run(ctx context.Context, input Message) (*Response, error) {
	return c.inner.Run(ctx, input)
}

// StreamRun 执行流式推理，委托给内部 ReActAgent
func (c *CapabilityAgent) StreamRun(ctx context.Context, input Message) (<-chan StreamEvent, error) {
	return c.inner.StreamRun(ctx, input)
}

// Stop 停止当前运行，委托给内部 ReActAgent
func (c *CapabilityAgent) Stop() {
	c.inner.Stop()
}

// Stats 返回运行统计，委托给内部 ReActAgent
func (c *CapabilityAgent) Stats() AgentStats {
	return c.inner.Stats()
}

// Name 返回 Agent 名称，委托给内部 ReActAgent
func (c *CapabilityAgent) Name() string {
	return c.inner.Name()
}

// ===== 额外方法委托 =====

// GracefulShutdown 优雅关闭 Agent，委托给内部 ReActAgent
func (c *CapabilityAgent) GracefulShutdown(ctx context.Context) error {
	return c.inner.GracefulShutdown(ctx)
}

// ResumeFromCheckpoint 从检查点恢复，委托给内部 ReActAgent
func (c *CapabilityAgent) ResumeFromCheckpoint(ctx context.Context) (*Response, error) {
	return c.inner.ResumeFromCheckpoint(ctx)
}

// ReplayFailure 从失败记录一键重放，委托给内部 ReActAgent（v3.4-6）
func (c *CapabilityAgent) ReplayFailure(ctx context.Context, failureID string) (*Response, error) {
	return c.inner.ReplayFailure(ctx, failureID)
}

// Pause 暂停 Agent，委托给内部 ReActAgent
func (c *CapabilityAgent) Pause() {
	c.inner.Pause()
}

// Resume 恢复暂停的 Agent，委托给内部 ReActAgent
func (c *CapabilityAgent) Resume() {
	c.inner.Resume()
}

// Inner 返回内部 ReActAgent，用于需要直接访问的场景
func (c *CapabilityAgent) Inner() *ReActAgent {
	return c.inner
}

// ===== Capable 接口实现 =====

// GetMemoryStore 返回记忆存储（MemoryCapable）
func (c *CapabilityAgent) GetMemoryStore() MemoryStore { return c.memory }

// GetRAGConfig 返回 RAG 配置（RAGCapable）
func (c *CapabilityAgent) GetRAGConfig() *RAGConfig { return c.rag }

// GetHITLConfig 返回人机协作配置（HITLCapable）
func (c *CapabilityAgent) GetHITLConfig() *HITLConfig { return c.hitl }

// GetHooks 返回 Hook 管理器（HookCapable）
func (c *CapabilityAgent) GetHooks() Hooks { return c.hooks }

// GetTracer 返回分布式追踪器（TraceCapable）
func (c *CapabilityAgent) GetTracer() Tracer { return c.tracer }

// GetCostTracker 返回成本追踪器（CostCapable）
func (c *CapabilityAgent) GetCostTracker() *CostTracker { return c.costTracker }

// GetContextWindowStrategy 返回上下文窗口裁剪策略（ContextWindowCapable）
func (c *CapabilityAgent) GetContextWindowStrategy() ContextWindowStrategy { return c.ctxWindow }

// GetEventPublisher 返回事件发布器（EventCapable）
func (c *CapabilityAgent) GetEventPublisher() EventPublisher { return c.eventPub }

// GetMetricsRecorder 返回指标记录器（MetricsCapable）
func (c *CapabilityAgent) GetMetricsRecorder() MetricsRecorder { return c.metrics }

// GetCheckpointStore 返回检查点存储（CheckpointCapable）
func (c *CapabilityAgent) GetCheckpointStore() persist.CheckpointStore { return c.checkpoint }

// GetFailureStore 返回失败记录存储（FailureCapable，v3.4-6）
func (c *CapabilityAgent) GetFailureStore() persist.FailureStore { return c.failureStore }

// WithObservability 注入全链路关联存储（v3.5-4）
// 引擎将以 trace_id 聚合 trace/metrics/audit，支持单请求全链路回溯。
func (c *CapabilityAgent) WithObservability(corr *observability.CorrelationStore) *CapabilityAgent {
	c.observability = corr
	return c
}

// GetObservability 返回全链路关联存储（ObservabilityCapable，v3.5-4）
func (c *CapabilityAgent) GetObservability() *observability.CorrelationStore { return c.observability }

// WithPlanSubtaskRetries 设置子任务失败重试次数（v3.6-1 自愈）。
func (c *CapabilityAgent) WithPlanSubtaskRetries(n int) *CapabilityAgent {
	c.inner.config.PlanSubtaskRetries = n
	return c
}

// WithPlanRecoveryMode 设置计划失败自愈模式（v3.6-1）：
// "on"/"" 启用（默认，自动 replan/降级），"off" 关闭。
func (c *CapabilityAgent) WithPlanRecoveryMode(mode string) *CapabilityAgent {
	c.inner.config.PlanRecoveryMode = mode
	return c
}

// GetSummarizer 返回摘要提取器（SummarizerCapable）
func (c *CapabilityAgent) GetSummarizer() memory.SummaryExtractor { return c.summarizer }

// GetFileScope 返回文件作用域（FileScopeCapable）
func (c *CapabilityAgent) GetFileScope() []string { return c.fileScope }

// GetCache 返回 LLM 缓存（CacheCapable）
func (c *CapabilityAgent) GetCache() llm.LLMCache { return c.cache }

// GetToolkit 返回tool注册表（ToolkitCapable）
func (c *CapabilityAgent) GetToolkit() *tools.Registry { return c.toolkit }

// ===== 链式 API =====

// WithMemory 注入记忆存储能力
func (c *CapabilityAgent) WithMemory(m MemoryStore) *CapabilityAgent {
	c.memory = m
	return c
}

// WithRAG 注入 RAG 检索能力
func (c *CapabilityAgent) WithRAG(cfg RAGConfig) *CapabilityAgent {
	c.rag = &cfg
	return c
}

// WithHITL 注入人机协作能力
func (c *CapabilityAgent) WithHITL(cfg HITLConfig) *CapabilityAgent {
	c.hitl = &cfg
	// 重建 HITLManager
	c.inner.hitlMgr = NewHITLManager(cfg)
	return c
}

// WithHooks 注入 Hook 管理器
func (c *CapabilityAgent) WithHooks(h Hooks) *CapabilityAgent {
	c.hooks = h
	c.inner.hooks = h
	return c
}

// WithTracer 注入分布式追踪器
func (c *CapabilityAgent) WithTracer(t Tracer) *CapabilityAgent {
	c.tracer = t
	return c
}

// WithCostTracker 注入成本追踪器
func (c *CapabilityAgent) WithCostTracker(ct *CostTracker) *CapabilityAgent {
	c.costTracker = ct
	return c
}

// WithContextWindow 注入上下文窗口裁剪策略
func (c *CapabilityAgent) WithContextWindow(cw ContextWindowStrategy) *CapabilityAgent {
	c.ctxWindow = cw
	return c
}

// WithEvents 注入事件发布器
func (c *CapabilityAgent) WithEvents(ep EventPublisher) *CapabilityAgent {
	c.eventPub = ep
	return c
}

// WithMetrics 注入指标记录器
func (c *CapabilityAgent) WithMetrics(m MetricsRecorder) *CapabilityAgent {
	c.metrics = m
	return c
}

// WithCheckpointStore 注入检查点存储
func (c *CapabilityAgent) WithCheckpointStore(cs persist.CheckpointStore) *CapabilityAgent {
	c.checkpoint = cs
	return c
}

// WithFailureStore 注入失败记录存储（v3.4-6）
func (c *CapabilityAgent) WithFailureStore(fs persist.FailureStore) *CapabilityAgent {
	c.failureStore = fs
	return c
}

// WithSummarizer 注入摘要提取器
func (c *CapabilityAgent) WithSummarizer(s memory.SummaryExtractor) *CapabilityAgent {
	c.summarizer = s
	return c
}

// WithFileScope 注入文件作用域限制
func (c *CapabilityAgent) WithFileScope(scopes []string) *CapabilityAgent {
	c.fileScope = scopes
	return c
}

// WithCache 注入 LLM 缓存
// 缓存会在 LLM Provider 层生效，设置后自动包装 Model 为 CachedProvider
func (c *CapabilityAgent) WithCache(cache llm.LLMCache) *CapabilityAgent {
	c.cache = cache
	// 包装 Model 为 CachedProvider
	if cache != nil {
		cached, err := llm.NewCachedProvider(c.inner.config.Model, cache, 0.8)
		if err == nil {
			c.inner.config.Model = cached
		}
	}
	return c
}

// WithToolkit 注入tool注册表
func (c *CapabilityAgent) WithToolkit(t *tools.Registry) *CapabilityAgent {
	c.toolkit = t
	return c
}

// WithPlanner 注入任务规划器
func (c *CapabilityAgent) WithPlanner(p planning.Planner) *CapabilityAgent {
	c.planner = p
	return c
}

// GetPlanner 返回任务规划器
func (c *CapabilityAgent) GetPlanner() planning.Planner {
	return c.planner
}

// WithWorldModel 注入世界模型跟踪器（v6.1 opt-in，提案 §2.1）
func (c *CapabilityAgent) WithWorldModel(t *worldmodel.WorldModelTracker) *CapabilityAgent {
	c.worldModel = t
	return c
}

// GetWorldModelTracker 返回世界模型跟踪器（WorldModelCapable 接口实现）
func (c *CapabilityAgent) GetWorldModelTracker() *worldmodel.WorldModelTracker {
	return c.worldModel
}

// WithReflector 注入反思器
func (c *CapabilityAgent) WithReflector(r reflection.Reflector) *CapabilityAgent {
	c.reflector = r
	return c
}

// GetReflector 返回反思器
func (c *CapabilityAgent) GetReflector() reflection.Reflector {
	return c.reflector
}

// WithToolLearner 注入tool学习器
func (c *CapabilityAgent) WithToolLearner(tl tool_learning.ToolLearner) *CapabilityAgent {
	c.toolLearner = tl
	return c
}

// GetToolLearner 返回tool学习器
func (c *CapabilityAgent) GetToolLearner() tool_learning.ToolLearner {
	return c.toolLearner
}

// WithOutputGuard 注入输出端 Guardrail 检查函数
// 在 LLM 响应后会调用该函数进行 PII 脱敏、注入拦截等
func (c *CapabilityAgent) WithOutputGuard(g OutputGuard) *CapabilityAgent {
	c.outputGuard = g
	return c
}

// GetOutputGuard 返回输出端 Guardrail 检查函数
func (c *CapabilityAgent) GetOutputGuard() OutputGuard {
	return c.outputGuard
}

// WithInputGuard 注入输入端 Guardrail 检查函数（v3.4-4）
// 在用户输入进入循环前调用，可脱敏或拒绝输入。
func (c *CapabilityAgent) WithInputGuard(g InputGuard) *CapabilityAgent {
	c.inputGuard = g
	return c
}

// GetInputGuard 返回输入端 Guardrail 检查函数
func (c *CapabilityAgent) GetInputGuard() InputGuard {
	return c.inputGuard
}

// WithAuditLogger 注入审计日志器
// LLM 调用、tool调用、Agent 启动/停止等关键路径会自动写入审计事件
func (c *CapabilityAgent) WithAuditLogger(l AuditLogger) *CapabilityAgent {
	c.auditLogger = l
	return c
}

// GetAuditLogger 返回审计日志器
func (c *CapabilityAgent) GetAuditLogger() AuditLogger {
	return c.auditLogger
}

// ===== v3.0 自适应学习能力 =====

// WithLearning 一次性注入自适应学习相关配置（Distiller / Evolver / FeedbackLearner）。
//
// 使用方式：
//
//	agent.WithLearning(learning.LearningConfig{
//		Distiller: learning.NewKnowledgeDistiller(),
//	})
func (c *CapabilityAgent) WithLearning(cfg LearningConfig) *CapabilityAgent {
	if cfg.Distiller != nil {
		c.distiller = cfg.Distiller
	}
	if cfg.Evolver != nil {
		c.evolver = cfg.Evolver
	}
	if cfg.FeedbackLearner != nil {
		c.feedbackLearner = cfg.FeedbackLearner
	}
	return c
}

// WithKnowledgeDistiller 注入知识蒸馏器（LearningCapable）。
// 引擎在 Agent 完成推理后自动从交互中蒸馏知识并存入知识库。
func (c *CapabilityAgent) WithKnowledgeDistiller(d *learning.KnowledgeDistiller) *CapabilityAgent {
	c.distiller = d
	return c
}

// GetKnowledgeDistiller 返回知识蒸馏器（LearningCapable）
func (c *CapabilityAgent) GetKnowledgeDistiller() *learning.KnowledgeDistiller {
	return c.distiller
}

// WithCapabilityEvolver 注入能力进化器。
// 引擎可据此评估 Agent 能力弱项并自动改进。
func (c *CapabilityAgent) WithCapabilityEvolver(e *learning.CapabilityEvolver) *CapabilityAgent {
	c.evolver = e
	return c
}

// GetCapabilityEvolver 返回能力进化器
func (c *CapabilityAgent) GetCapabilityEvolver() *learning.CapabilityEvolver {
	return c.evolver
}

// WithFeedbackLearner 注入反馈学习器。
// 引擎可据此记录人类反馈并调整 Agent 行为偏好。
func (c *CapabilityAgent) WithFeedbackLearner(f *learning.FeedbackLearner) *CapabilityAgent {
	c.feedbackLearner = f
	return c
}

// GetFeedbackLearner 返回反馈学习器
func (c *CapabilityAgent) GetFeedbackLearner() *learning.FeedbackLearner {
	return c.feedbackLearner
}

// ===== v3.3-v3.6 能力（自治 / 技能 / 实时） =====

// WithAutonomy 注入长期自治运行时（AutonomyCapable）
func (c *CapabilityAgent) WithAutonomy(rt *autonomy.AutonomyRuntime) *CapabilityAgent {
	c.autonomyRT = rt
	return c
}

// GetAutonomyRuntime 返回自治运行时（AutonomyCapable）
func (c *CapabilityAgent) GetAutonomyRuntime() *autonomy.AutonomyRuntime { return c.autonomyRT }

// WithSkills 注入技能库与匹配器（SkillsCapable）
func (c *CapabilityAgent) WithSkills(store *skills.Store, matcher *skills.Matcher) *CapabilityAgent {
	c.skillStore = store
	c.skillMatch = matcher
	return c
}

// GetSkillStore 返回技能库（SkillsCapable）
func (c *CapabilityAgent) GetSkillStore() *skills.Store { return c.skillStore }

// GetSkillMatcher 返回技能匹配器（SkillsCapable）
func (c *CapabilityAgent) GetSkillMatcher() *skills.Matcher { return c.skillMatch }

// WithRealtime 注入实时会话编排器（RealtimeCapable）
func (c *CapabilityAgent) WithRealtime(hub *realtime.RealtimeHub) *CapabilityAgent {
	c.realtimeHub = hub
	return c
}

// GetRealtimeHub 返回实时会话编排器（RealtimeCapable）
func (c *CapabilityAgent) GetRealtimeHub() *realtime.RealtimeHub { return c.realtimeHub }
