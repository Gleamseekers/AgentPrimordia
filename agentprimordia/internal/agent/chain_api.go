package agent

import (
	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/agent/worldmodel"
	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/llm"
	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/memory"
	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/observability"
	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/persist"
	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/tools"
	"context"
	"strings"
)

// 此文件提供 ReActAgent 的链式 API 入口方法。
// 每个方法创建一个 CapabilityAgent 包装器并注入对应能力，
// 后续可通过 CapabilityAgent 的链式方法继续注入其他能力。
//
// 使用方式：
//
//	agent := NewReActAgent(ReActConfig{
//	    Name: "my-agent", Model: provider, MaxTurns: 10,
//	}).WithMemory(mem).WithRAG(RAGConfig{...}).WithHooks(hooks)

// AsCapability 将 ReActAgent 包装为 CapabilityAgent，暴露链式 API。
//
// 这是从 ReActAgent 转换为 CapabilityAgent 的标准方式。
// 等价于 WithMemory(nil) 的语义效果，但语义更清晰。
func (a *ReActAgent) AsCapability() *CapabilityAgent {
	return a.wrapSelf(&CapabilityAgent{inner: a})
}

// wrapSelf 创建 CapabilityAgent 并更新 ReActAgent 的自引用
func (a *ReActAgent) wrapSelf(cap *CapabilityAgent) *CapabilityAgent {
	a.self = cap // 更新自引用，使引擎通过接口发现能力
	return cap
}

// WithMemory 注入记忆存储能力，返回可链式调用的 CapabilityAgent
func (a *ReActAgent) WithMemory(m MemoryStore) *CapabilityAgent {
	return a.wrapSelf(&CapabilityAgent{inner: a, memory: m})
}

// WithToolkit 注入tool注册表，返回可链式调用的 CapabilityAgent
func (a *ReActAgent) WithToolkit(t *tools.Registry) *CapabilityAgent {
	return a.wrapSelf(&CapabilityAgent{inner: a, toolkit: t})
}

// WithWorldModel 注入世界模型跟踪器（v6.1 opt-in，提案 §2.1），
// 返回可链式调用的 CapabilityAgent。不注入时默认 ReAct 行为零变更（铁律 7）。
func (a *ReActAgent) WithWorldModel(t *worldmodel.WorldModelTracker) *CapabilityAgent {
	return a.wrapSelf(&CapabilityAgent{inner: a, worldModel: t})
}

// WithHooks 注入 Hook 管理器，返回可链式调用的 CapabilityAgent
func (a *ReActAgent) WithHooks(h Hooks) *CapabilityAgent {
	a.hooks = h
	return a.wrapSelf(&CapabilityAgent{inner: a, hooks: h})
}

// WithRAG 注入 RAG 检索能力，返回可链式调用的 CapabilityAgent
func (a *ReActAgent) WithRAG(cfg RAGConfig) *CapabilityAgent {
	return a.wrapSelf(&CapabilityAgent{inner: a, rag: &cfg})
}

// WithHITL 注入人机协作能力，返回可链式调用的 CapabilityAgent
func (a *ReActAgent) WithHITL(cfg HITLConfig) *CapabilityAgent {
	a.hitlMgr = NewHITLManager(cfg)
	return a.wrapSelf(&CapabilityAgent{inner: a, hitl: &cfg})
}

// WithTracer 注入分布式追踪器，返回可链式调用的 CapabilityAgent
func (a *ReActAgent) WithTracer(t Tracer) *CapabilityAgent {
	return a.wrapSelf(&CapabilityAgent{inner: a, tracer: t})
}

// WithCostTracker 注入成本追踪器，返回可链式调用的 CapabilityAgent
func (a *ReActAgent) WithCostTracker(ct *CostTracker) *CapabilityAgent {
	return a.wrapSelf(&CapabilityAgent{inner: a, costTracker: ct})
}

// WithContextWindow 注入上下文窗口裁剪策略，返回可链式调用的 CapabilityAgent
func (a *ReActAgent) WithContextWindow(cw ContextWindowStrategy) *CapabilityAgent {
	return a.wrapSelf(&CapabilityAgent{inner: a, ctxWindow: cw})
}

// WithEvents 注入事件发布器，返回可链式调用的 CapabilityAgent
func (a *ReActAgent) WithEvents(ep EventPublisher) *CapabilityAgent {
	return a.wrapSelf(&CapabilityAgent{inner: a, eventPub: ep})
}

// WithMetrics 注入指标记录器，返回可链式调用的 CapabilityAgent
func (a *ReActAgent) WithMetrics(m MetricsRecorder) *CapabilityAgent {
	return a.wrapSelf(&CapabilityAgent{inner: a, metrics: m})
}

// WithCheckpointStore 注入检查点存储，返回可链式调用的 CapabilityAgent
func (a *ReActAgent) WithCheckpointStore(cs persist.CheckpointStore) *CapabilityAgent {
	return a.wrapSelf(&CapabilityAgent{inner: a, checkpoint: cs})
}

// WithFailureStore 注入失败记录存储，返回可链式调用的 CapabilityAgent（v3.4-6）
func (a *ReActAgent) WithFailureStore(fs persist.FailureStore) *CapabilityAgent {
	return a.wrapSelf(&CapabilityAgent{inner: a, failureStore: fs})
}

// WithSummarizer 注入摘要提取器，返回可链式调用的 CapabilityAgent
func (a *ReActAgent) WithSummarizer(s memory.SummaryExtractor) *CapabilityAgent {
	return a.wrapSelf(&CapabilityAgent{inner: a, summarizer: s})
}

// WithFileScope 注入文件作用域限制，返回可链式调用的 CapabilityAgent
func (a *ReActAgent) WithFileScope(scopes []string) *CapabilityAgent {
	return a.wrapSelf(&CapabilityAgent{inner: a, fileScope: scopes})
}

// WithCache 注入 LLM 缓存，返回可链式调用的 CapabilityAgent
// 注意：缓存会在 LLM Provider 层生效，设置后自动包装 Model 为 CachedProvider
func (a *ReActAgent) WithCache(cache llm.LLMCache) *CapabilityAgent {
	// 包装 Model 为 CachedProvider
	if cache != nil {
		cached, err := llm.NewCachedProvider(a.config.Model, cache, 0.8)
		if err == nil {
			a.config.Model = cached
		}
	}
	return a.wrapSelf(&CapabilityAgent{inner: a, cache: cache})
}

// WithOutputGuard 注入输出端 Guardrail 检查函数，返回可链式调用的 CapabilityAgent
// 用于在 LLM 响应后自动进行 PII 脱敏、注入拦截、敏感词过滤等防护
func (a *ReActAgent) WithOutputGuard(g OutputGuard) *CapabilityAgent {
	return a.wrapSelf(&CapabilityAgent{inner: a, outputGuard: g})
}

// WithInputGuard 注入输入端 Guardrail 检查函数，返回可链式调用的 CapabilityAgent
// 用于在用户输入进入循环前自动进行注入拦截、脱敏等防护（v3.4-4）
func (a *ReActAgent) WithInputGuard(g InputGuard) *CapabilityAgent {
	return a.wrapSelf(&CapabilityAgent{inner: a, inputGuard: g})
}

// WithAuditLogger 注入审计日志器，返回可链式调用的 CapabilityAgent
// LLM 调用、tool调用、Agent 启动/停止等关键路径会自动写入审计事件
func (a *ReActAgent) WithAuditLogger(l AuditLogger) *CapabilityAgent {
	return a.wrapSelf(&CapabilityAgent{inner: a, auditLogger: l})
}

// WithObservability 注入全链路关联存储（v3.5-4），返回可链式调用的 CapabilityAgent
func (a *ReActAgent) WithObservability(corr *observability.CorrelationStore) *CapabilityAgent {
	return a.wrapSelf(&CapabilityAgent{inner: a, observability: corr})
}

// ===== RAG 简化 API =====

// ragStoreAdapter 将 memory.RAGStore 适配为 agent.RAGProvider，
// 避免用户手动调用 NewRAGProviderAdapter。
type ragStoreAdapter struct {
	store *memory.RAGStore
}

func (a *ragStoreAdapter) Search(ctx context.Context, query string, topK int) ([]*RAGDocument, error) {
	results, err := a.store.HybridSearch(ctx, query, topK)
	if err != nil {
		return nil, err
	}
	docs := make([]*RAGDocument, len(results))
	for i, r := range results {
		source := strings.Join(r.Sources, "+")
		docs[i] = &RAGDocument{
			ID:      r.Episode.ID,
			Content: r.Episode.Content,
			Score:   r.Score,
			Source:  source,
			Role:    string(r.Episode.Role),
		}
	}
	return docs, nil
}

// WithRAGMemory 一步完成 RAG 设置。
//
//	agent.WithRAGMemory(mem, provider) 等价于以下手动 6 步：
//	  NewEmbeddingAdapter → NewRAGStore → NewRAGProviderAdapter → RAGConfig → WithRAG
//
// 内部自动创建 VectorStore + RAGStore + 适配器，使用默认 RAG 参数：
//
//	Mode=RAGModeAuto, TopK=5, MinScore=0.3
func (a *ReActAgent) WithRAGMemory(mem memory.Memory, emb memory.EmbeddingProvider) *CapabilityAgent {
	ragStore := memory.NewRAGStore(mem, emb)
	cfg := RAGConfig{
		Provider: &ragStoreAdapter{store: ragStore},
		Mode:     RAGModeAuto,
		TopK:     5,
		MinScore: defaultRAGMinScore(),
	}
	return a.WithRAG(cfg)
}

func defaultRAGMinScore() float32 { return 0.3 }
