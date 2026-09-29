// Stability: Experimental — v7.1 统一工具智能系统（工具画像/参数调优/历史选择/
// 缺口检测/自动生成）公共导出面，API 可能随子系统演进而调整。
//
// 本文件以类型别名 + 变量 re-export 方式导出 internal/tools/intelligence 家族：
//   - intelligence      — 统一入口 ToolIntelligence、ReAct 桥接 Hook、INV-0 门控注册
//   - create            — 轨迹缺口检测与工具生成（模板/LLM 两种生成器）
//   - optimize          — 性能画像、数据驱动调优、历史选择
//   - reuse             — 工具目录与任务-工具匹配
//
// 命名说明：ap.ToolUsageRecord 已指向 agent/tool_learning.ToolUsageRecord，
// 故 intelligence 的使用记录导前缀名 IntelligenceToolUsageRecord 以免冲突。
package ap

import (
	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/tools/intelligence"
	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/tools/intelligence/create"
	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/tools/intelligence/optimize"
	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/tools/intelligence/reuse"
)

// ===== 统一入口与核心接口 =====

// ToolIntelligence 统一工具智能入口：组装缺口检测/工具生成/画像/调优/选择全部子组件
type ToolIntelligence = intelligence.ToolIntelligence

// NewToolIntelligence 构造统一工具智能入口
var NewToolIntelligence = intelligence.NewToolIntelligence

// GapDetector 缺口检测器接口：从调用轨迹中发现缺失工具
type GapDetector = intelligence.GapDetector

// ToolCreator 工具生成器接口：按缺口候选生成工具工件
type ToolCreator = intelligence.ToolCreator

// ToolProfiler 工具性能画像接口：记录调用并计算成功率/延迟/P95 统计
type ToolProfiler = intelligence.ToolProfiler

// ToolTuner 参数调优器接口：基于画像提出参数调整建议
type ToolTuner = intelligence.ToolTuner

// ToolSelector 工具选择优化接口：按历史成功率从候选中选优
type ToolSelector = intelligence.ToolSelector

// ===== 数据类型 =====

// GapCandidate 缺口候选（错误模式聚类结果）
type GapCandidate = intelligence.GapCandidate

// ToolArtifact 工具产物。Signature/PublicKey 为 ed25519 签名对，
// RegisteringCreator 的验签门依此放行；缺失即拒绝注册（INV-0：签名前置）。
type ToolArtifact = intelligence.ToolArtifact

// ToolCallRecord 工具调用记录（缺口检测的轨迹单元）
type ToolCallRecord = intelligence.ToolCallRecord

// IntelligenceToolUsageRecord 工具使用记录（画像录入）。
// 前缀命名以避免与 ap.ToolUsageRecord（tool_learning 家族）冲突。
type IntelligenceToolUsageRecord = intelligence.ToolUsageRecord

// ToolProfile 工具性能画像（成功率/平均延迟/P95/平均 token 等统计）
type ToolProfile = intelligence.ToolProfile

// TuningSuggestion 调优建议（参数名/建议值/置信度/原因）
type TuningSuggestion = intelligence.TuningSuggestion

// ===== ReAct 循环桥接 Hook =====

// IntelligenceHook 工具智能 Hook：工具调用后记录画像，轮次结束后检测缺口并自动创建工具
type IntelligenceHook = intelligence.IntelligenceHook

// NewIntelligenceHook 创建工具智能 Hook
var NewIntelligenceHook = intelligence.NewIntelligenceHook

// ===== INV-0 合规注册（验签门 + 沙箱执行通道） =====

// ArtifactVerifier 工件验签门接口（INV-0/A6 门控）。
// 未注入或验签失败时 RegisteringCreator 一律拒绝注册（fail-closed）。
type ArtifactVerifier = intelligence.ArtifactVerifier

// ArtifactExecutor 工件执行器注入点。
// 唯一合法实现是 wasm 沙箱适配器（ap.NewWASMToolAdapter 侧通道），
// 由组装根绑定；未注入时注册成功的工具在执行阶段拒绝宿主执行。
type ArtifactExecutor = intelligence.ArtifactExecutor

// RegisteringCreator INV-0 门控注册生成器：验签通过后才以 0644 数据文件
// 落盘并注册；执行通道仅可来自注入的 executor（wasm 沙箱）。
type RegisteringCreator = intelligence.RegisteringCreator

// NewRegisteringCreator 构造 RegisteringCreator。
// 默认不注入验签器/执行器——此时对任何工件一律拒绝注册（安全默认值）。
var NewRegisteringCreator = intelligence.NewRegisteringCreator

// ===== create 子包：缺口检测与工具生成 =====

// TraceGapDetector 轨迹缺口检测器（并发安全）：按错误模式聚类发现缺失工具
type TraceGapDetector = create.TraceGapDetector

// NewTraceGapDetector 创建轨迹缺口检测器
var NewTraceGapDetector = create.NewTraceGapDetector

// LifecycleCreator 生命周期工具生成器：基于硬编码 shell 脚本模板生成工具
type LifecycleCreator = create.LifecycleCreator

// NewLifecycleCreator 创建生命周期工具生成器
var NewLifecycleCreator = create.NewLifecycleCreator

// LLMCompleter 最小化 LLM 补全接口（Complete(ctx, prompt) → 脚本文本）
type LLMCompleter = create.LLMCompleter

// LLMCreator LLM 驱动工具生成器：LLM 失败时自动降级到 LifecycleCreator 模板
type LLMCreator = create.LLMCreator

// NewLLMCreator 构造 LLM 驱动工具生成器
var NewLLMCreator = create.NewLLMCreator

// ===== optimize 子包：画像 / 调优 / 选择 =====

// InMemoryProfiler 内存版工具性能画像器
type InMemoryProfiler = optimize.InMemoryProfiler

// NewInMemoryProfiler 创建内存版工具性能画像器
var NewInMemoryProfiler = optimize.NewInMemoryProfiler

// DataDrivenTuner 数据驱动参数调优器：低成功率→建议重试，高延迟→建议增大超时
type DataDrivenTuner = optimize.DataDrivenTuner

// NewDataDrivenTuner 创建数据驱动参数调优器
var NewDataDrivenTuner = optimize.NewDataDrivenTuner

// HistorySelector 基于历史成功率的工具选择器
type HistorySelector = optimize.HistorySelector

// NewHistorySelector 创建基于历史成功率的工具选择器
var NewHistorySelector = optimize.NewHistorySelector

// ===== reuse 子包：工具目录与任务匹配 =====

// ToolEntry 工具目录条目（ID/名称/描述/领域）
type ToolEntry = reuse.ToolEntry

// ToolCatalog 工具目录注册表（并发安全）
type ToolCatalog = reuse.ToolCatalog

// NewToolCatalog 创建工具目录
var NewToolCatalog = reuse.NewToolCatalog

// TaskMatcher 任务-工具匹配器：按关键词重叠度选择最匹配的工具
type TaskMatcher = reuse.TaskMatcher

// NewTaskMatcher 创建任务-工具匹配器
var NewTaskMatcher = reuse.NewTaskMatcher
