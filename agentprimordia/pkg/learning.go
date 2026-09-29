// Stability: Experimental — v3.0.0 新增自适应学习与知识蒸馏能力，API 可能随演进而调整。
package ap

import (
	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/agent/learning"
)

// ===== 知识蒸馏 =====

// KnowledgeDistiller 知识蒸馏器，从交互中提取知识→压缩→存入语义记忆
type KnowledgeDistiller = learning.KnowledgeDistiller

// KnowledgeItem 蒸馏出的知识项
type KnowledgeItem = learning.KnowledgeItem

// LearningInteraction 一次 Agent 交互（用于知识蒸馏输入）
type LearningInteraction = learning.Interaction

// DistillerStats 蒸馏统计（内部使用，含 atomic 字段）
type DistillerStats = learning.DistillerStats

// DistillerStatsSnapshot 蒸馏统计快照（值安全，GetStats 返回此类型）
type DistillerStatsSnapshot = learning.DistillerStatsSnapshot

var (
	// NewKnowledgeDistiller 创建知识蒸馏器
	NewKnowledgeDistiller = learning.NewKnowledgeDistiller
)

// ===== 能力进化 =====

// CapabilityEvolver 能力进化器，评估 Agent 能力并识别弱项
type CapabilityEvolver = learning.CapabilityEvolver

// LearningCapability 能力定义（名称、描述、评分、测试次数）
type LearningCapability = learning.Capability

var (
	// NewCapabilityEvolver 创建能力进化器
	NewCapabilityEvolver = learning.NewCapabilityEvolver
)

// ===== 反馈学习 =====

// FeedbackLearner 反馈学习器，从人类反馈中学习偏好模型
type FeedbackLearner = learning.FeedbackLearner

// FeedbackEntry 反馈条目
type FeedbackEntry = learning.FeedbackEntry

// PreferenceModel 偏好模型
type PreferenceModel = learning.PreferenceModel

var (
	// NewFeedbackLearner 创建反馈学习器
	NewFeedbackLearner = learning.NewFeedbackLearner
)
