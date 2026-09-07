// react_loop.go — ReAct 循环核心类型与入口
// 包含 ReActAgent 结构体定义、构造函数、Run/StreamRun 入口
// runLoop / reactLoopEngine / executeToolCalls / handleHITL 已拆分到独立文件
package agent

import (
	"context"
	"log/slog"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"agentprimordia/internal/agent/learning"
	"agentprimordia/internal/agent/planning"
	"agentprimordia/internal/agent/reflection"
	"agentprimordia/internal/agent/tool_learning"
	"agentprimordia/internal/agent/worldmodel"
	"agentprimordia/internal/llm"
	"agentprimordia/internal/memory"
	"agentprimordia/internal/observability"
	"agentprimordia/internal/persist"
	"agentprimordia/internal/tools"
	"agentprimordia/internal/tools/intelligence"
	"agentprimordia/pkg/logger"
)

// idCounter 用于生成唯一 ID 的原子计数器
type idCounter struct {
	n int64
}

// next 生成唯一的记忆 ID（优化：避免 fmt.Sprintf）
func (c *idCounter) next() string {
	n := atomic.AddInt64(&c.n, 1)
	ts := time.Now().UnixNano()
	return "msg_" + strconv.FormatInt(ts, 10) + "_" + strconv.FormatInt(n, 10)
}

// ===== 集成接口 =====

// MemoryStore 是 Agent 所需的记忆存储接口
type MemoryStore interface {
	Add(ctx context.Context, episode *memory.Episode) error
	// UpdateSummary 更新指定 episode 的摘要和标签（M2 修复：异步摘要存储）。
	UpdateSummary(ctx context.Context, id, summary, topics string) error
}

// EventPublisher 是 Agent 所需的事件发布接口
type EventPublisher interface {
	PublishAsync(eventType string, source string, payload any) error
}

// MetricsRecorder 是 Agent 所需的指标记录接口
type MetricsRecorder interface {
	RecordLLMCall(duration time.Duration, err error)
	RecordToolCall(duration time.Duration, err error)
	RecordTurn(duration time.Duration)
	RecordTokenUsage(model string, promptTokens, completionTokens int)
	IncActiveAgents()
	DecActiveAgents()
}

// LabeledMetricsRecorder 可选接口：带标签维度的指标记录
// 实现此接口的记录器（如 AgentMetrics）可输出 provider/model/tool_name 等维度，
// 供 Grafana PromQL 多维聚合查询使用。
// Agent 运行时通过类型断言发现此能力，未实现则回退到无标签记录。
type LabeledMetricsRecorder interface {
	RecordLLMCallWithLabels(duration time.Duration, err error, provider, model string)
	RecordToolCallWithLabels(duration time.Duration, err error, toolName string)
	RecordTurnWithAgent(duration time.Duration, agentName string)
}

// ReActConfig holds configuration for a ReAct-based agent
type ReActConfig struct {
	// ===== 核心配置（必填） =====
	Name           string
	SystemPrompt   string
	PromptTemplate *PromptTemplate
	Model          llm.Provider
	MaxTurns       int
	Temperature    float64
	SessionID      string

	// Lifecycle 生命周期管理器（默认自动创建）
	Lifecycle *Lifecycle

	// Logger 结构化日志，默认使用 logger.Default()
	Logger *slog.Logger

	// ===== Phase 1 G1 闭环配置 =====

	// ParallelToolExecution 启用同轮tool并行执行（G1-4）
	// 默认 false 保持向后兼容
	ParallelToolExecution bool

	// MaxParallelTools 单批并行tool数上限（G1-4）
	// 0 表示无限制（一次并行所有同轮tool）；建议生产设为 8-16 避免资源争用
	MaxParallelTools int

	// ReflectionSeverityThreshold 触发 Reflection 改进的最低严重度（G1-2）
	// 取值 "low" / "medium" / "high" / "critical"，默认 "high"
	// 低于阈值的 Critique 不触发 Improve，直接返回原始输出
	ReflectionSeverityThreshold string

	// ToolLearningConfidenceThreshold 触发tool参数建议的最低置信度（G1-3）
	// 范围 [0, 1]，默认 0.7
	ToolLearningConfidenceThreshold float64

	// v3.6-1 自愈配置：
	// PlanSubtaskRetries 子任务失败时的额外重试次数（默认 1）。
	PlanSubtaskRetries int
	// PlanRecoveryMode 计划失败时自动换路径（replan / 降级到 runLoop）：
	// 空值或 "on" 表示启用（默认），"off" 表示关闭——默认故障恢复不依赖人工。
	PlanRecoveryMode string

	// ToolIntelligence 工具智能配置（Task 7：IntelligenceHook 桥接）
	// 非 nil 时自动在 ReAct 循环中记录工具使用画像 + 缺口检测 + 自动创建工具
	ToolIntelligence *ToolIntelligenceConfig
}

// ReActAgent implements the ReAct (Reasoning + Acting) pattern
type ReActAgent struct {
	config    ReActConfig
	lifecycle *Lifecycle
	hooks     Hooks
	logger    *slog.Logger
	startTime time.Time
	stats     AgentStats
	statsMu   sync.RWMutex // 锁层级 L1：统计信息（仅保护 ToolsCalled map）
	runMu     sync.Mutex   // 锁层级 L2：运行状态
	mu        sync.Mutex   // 锁层级 L3：通用字段（最内层，最后获取）
	// 锁顺序：statsMu → runMu → mu，禁止反向获取

	// 热路径原子计数器（避免每 turn 加锁，Task 3.5 优化）
	atomicTurn     atomic.Int64
	atomicMessages atomic.Int64
	hitlMgr        *HITLManager
	idGen          idCounter // 实例级 ID 生成器，消除全局可变状态

	// currentRequestID 当前运行的请求 ID，用于可观测性关联
	currentRequestID string

	// memSessionID 本次运行解析出的记忆会话 ID（reactLoopEngine 入口设置，
	// runMu 保护下运行期共享），供 saveMemory 对无 Metadata 的 assistant 消息回退使用。
	memSessionID string

	// hookCtx 用于 fireHook 调用，绑定到当前运行的 context
	// 确保 agent 取消时 hook 也能被取消
	hookCtx context.Context

	// self 自引用，指向最外层的 Agent 包装器
	// 用于协议式微内核的接口发现：引擎通过 a.self.(XxxCapable) 检测能力
	// 默认指向自身；WithXxx 链式调用时更新为 CapabilityAgent
	self Agent

	// ===== Task 1: saveMemory 异步化 =====
	// memoryWriter 封装异步写入队列，从 ReActAgent 剥离独立管理
	memWriter *memoryWriter

	// ===== Task 1.5: Executor 复用 =====
	// 缓存的 *tools.Executor，避免每轮tool调用时重新分配
	toolExecutor     *tools.Executor
	toolExecutorOnce sync.Once

	// ===== Task 2: 能力查找缓存 =====
	// 单次 Run() 期间不变的能力引用，reactLoopEngine 入口处一次性查找
	// runLoop 内通过 capCache 访问，避免每轮重复类型断言
	//
	// 生命周期契约（评估修复）：
	//   - 每次 Run() 在 runMu 锁内通过 resolveCapabilities 重新填充；
	//   - Run 结束后**不置 nil**（旧实现置 nil 会让异步 goroutine 读到
	//     nil 而 panic；每次 Run 重新填充已保证不会误用旧引用）；
	//   - 异步 goroutine（如 distillKnowledge）必须先拷贝所需字段再进入
	//     goroutine，禁止在 goroutine 内直接读取本字段。
	capCache *capabilityCache

	// ===== v3.4-1: 子任务执行器可注入（测试/扩展） =====
	// 为 nil 时 executePlan 走默认 executeSubTask；测试可注入可控执行器。
	subtaskExecutor func(ctx context.Context, task planning.SubTask, history []Message, cfg loopConfig) (*Response, error)
}

// newReActAgent 创建基于 ReAct 循环的 Agent 实例（内部使用）
// 仅接受标量配置，能力通过链式 API 注入。
func newReActAgent(cfg ReActConfig) *ReActAgent {
	if cfg.MaxTurns == 0 {
		cfg.MaxTurns = 50
	}
	// v3.6-1：自愈默认值——子任务重试 1 次、失败自动换路径（replan/降级）
	if cfg.PlanSubtaskRetries == 0 {
		cfg.PlanSubtaskRetries = defaultPlanSubtaskRetries
	}
	if cfg.Lifecycle == nil {
		cfg.Lifecycle = NewLifecycle()
	}
	if cfg.Logger == nil {
		cfg.Logger = logger.Default()
	}

	a := &ReActAgent{
		config:    cfg,
		lifecycle: cfg.Lifecycle,
		hooks:     nil, // 通过链式 API WithHooks 注入
		logger:    cfg.Logger,
		stats: AgentStats{
			Status:      StatusIdle,
			ToolsCalled: make(map[string]int),
		},
		hitlMgr: nil, // 通过链式 API WithHITL 注入
	}
	a.initSelf()
	return a
}

// loopConfig 循环配置
type loopConfig struct {
	stream    bool
	streamCh  chan StreamEvent
	streamCtx context.Context
	requestID string
	// v3.6-1：自愈降级时跳过 plan 分支，防止递归进入 executePlanWithSelfHealing
	skipPlan bool
}

// capabilityCache 缓存单次 Run() 期间不变的能力查找结果。
// 优化（Task 2）：ReAct 循环中每轮调用的 getTracer/getCostTracker/getMemoryStore 等
// 在 Run() 入口一次性查找并缓存到此处，避免每轮重复类型断言。
// 优化（perf-v2）：新增 toolDefinitions 缓存，避免每轮重复转换tool定义。
// R1.2：新增 planner/reflector/toolLearner 字段，连接 G1-1/G1-2/G1-3 闭环。
type capabilityCache struct {
	requestID        string
	traceID          string                          // v3.5-4：本次请求的分布式追踪 ID（关联键）
	observability    *observability.CorrelationStore // v3.5-4：全链路关联存储
	tracer           Tracer
	costTracker      *CostTracker
	memoryStore      MemoryStore
	metricsRecorder  MetricsRecorder
	labeledRecorder  LabeledMetricsRecorder
	eventPublisher   EventPublisher
	checkpointStore  persist.CheckpointStore
	failureStore     persist.FailureStore // v3.4-6：失败记录存储（失败重放）
	contextWindow    ContextWindowStrategy
	summarizer       memory.SummaryExtractor
	fileScope        []string
	toolkit          *tools.Registry
	toolDefinitions  []llm.ToolDefinition // 优化（perf-v2）：缓存转换后的tool定义
	systemInfoCached bool
	provider         string
	model            string
	outputGuard      OutputGuard // p2t1：输出端 Guardrail 检查函数（PII 自动脱敏）
	auditLogger      AuditLogger // p2t4：审计日志器（合规事件记录）

	// R1.2：闭环构建期新能力
	planner     planning.Planner          // G1-1 Planning 接入
	reflector   reflection.Reflector      // G1-2 Reflection 接入
	toolLearner tool_learning.ToolLearner // G1-3 ToolLearning 接入

	// v3.0：自适应学习能力
	distiller       *learning.KnowledgeDistiller // 知识蒸馏器
	evolver         *learning.CapabilityEvolver  // 能力进化器
	feedbackLearner *learning.FeedbackLearner    // 反馈学习器

	// v6.1：世界模型跟踪器（opt-in；nil = 不启用，默认路径零变化）
	worldTracker *worldmodel.WorldModelTracker

	// Task 7：工具智能 Hook（nil = 不启用）
	intelligenceHook *intelligence.IntelligenceHook
}

// resolveCapabilities 一次性查找所有能力并填充到 capabilityCache。
// 必须在 reactLoopEngine 入口处调用，且只在 Run() 期间使用。
func (a *ReActAgent) resolveCapabilities(requestID string) *capabilityCache {
	c := &capabilityCache{
		requestID:       requestID,
		observability:   a.getObservability(),
		tracer:          a.getTracer(),
		costTracker:     a.getCostTracker(),
		memoryStore:     a.getMemoryStore(),
		metricsRecorder: a.getMetricsRecorder(),
		eventPublisher:  a.getEventPublisher(),
		checkpointStore: a.getCheckpointStore(),
		failureStore:    a.getFailureStore(),
		contextWindow:   a.getContextWindowStrategy(),
		summarizer:      a.getSummarizer(),
		fileScope:       a.getFileScope(),
		toolkit:         a.getToolkit(),
		outputGuard:     a.getOutputGuard(),
		auditLogger:     a.getAuditLogger(),
		// R1.2：闭环构建期新能力查找
		planner:     a.getPlanner(),
		reflector:   a.getReflector(),
		toolLearner: a.getToolLearner(),

		// v3.0：自适应学习能力查找
		distiller:       a.getKnowledgeDistiller(),
		evolver:         a.getCapabilityEvolver(),
		feedbackLearner: a.getFeedbackLearner(),

		// v6.1：世界模型跟踪器（接线点①；opt-in，未注入时为 nil）
		worldTracker: a.getWorldModelTracker(),
	}
	// Task 7：工具智能 Hook——从配置构建 IntelligenceHook 实例
	if ti := a.config.ToolIntelligence; ti != nil {
		c.intelligenceHook = intelligence.NewIntelligenceHook(ti.Profiler, ti.Detector, ti.Creator)
	}
	// 缓存 labeled 记录器
	if c.metricsRecorder != nil {
		if lm, ok := c.metricsRecorder.(LabeledMetricsRecorder); ok {
			c.labeledRecorder = lm
		}
	}
	// 缓存模型信息（用于 recordLLM）
	if info := a.config.Model.Info(); info.Name != "" {
		c.systemInfoCached = true
		c.provider = info.Provider
		c.model = info.Name
	}
	// 优化（perf-v2）：预转换tool定义，避免每轮重复转换
	if c.toolkit != nil {
		if defs := c.toolkit.Definitions(); len(defs) > 0 {
			c.toolDefinitions = convertToolDefsToLLMDefinitions(defs)
		}
	}
	return c
}

// emitStream 在流式模式下向通道发送事件
func (a *ReActAgent) emitStream(cfg loopConfig, event StreamEvent) {
	if cfg.stream && cfg.streamCh != nil {
		// 自动填充请求 ID
		if event.RequestID == "" {
			event.RequestID = cfg.requestID
		}
		select {
		case cfg.streamCh <- event:
		case <-cfg.streamCtx.Done():
		}
	}
}

// Run executes the agent with the given input message
func (a *ReActAgent) Run(ctx context.Context, input Message) (*Response, error) {
	// 注入请求 ID：若 context 中已有则复用，否则生成新的
	reqID := RequestIDFromCtx(ctx)
	if reqID == "" {
		reqID = NewRequestID()
		ctx = WithRequestID(ctx, reqID)
	}
	a.mu.Lock()
	a.currentRequestID = reqID
	a.mu.Unlock()

	return a.reactLoopEngine(ctx, input, loopConfig{stream: false, requestID: reqID})
}

// StreamRun 执行 Agent 并以流式方式输出结果
// 返回一个事件 channel，调用者可以逐事件消费
func (a *ReActAgent) StreamRun(ctx context.Context, input Message) (<-chan StreamEvent, error) {
	// 注入请求 ID
	reqID := RequestIDFromCtx(ctx)
	if reqID == "" {
		reqID = NewRequestID()
		ctx = WithRequestID(ctx, reqID)
	}
	a.mu.Lock()
	a.currentRequestID = reqID
	a.mu.Unlock()

	ch := make(chan StreamEvent, 32)
	go func() {
		defer close(ch)
		if _, err := a.reactLoopEngine(ctx, input, loopConfig{stream: true, streamCh: ch, streamCtx: ctx, requestID: reqID}); err != nil {
			select {
			case ch <- StreamEvent{Type: StreamEventError, RequestID: reqID, Content: err.Error()}:
			case <-ctx.Done():
			}
		}
	}()
	return ch, nil
}
