// Stability: Experimental — v3.0.0 新增前沿能力，API 可能随使用场景演进而调整。
package ap

import (
	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/agent/reflection"
)

// Reflector 定义自我反思和纠错接口
// Deprecated: 无外部采用证据的周边能力（自反思 / 批评），v8.0 收敛 pkg 核心承诺面。v8.0 将从 pkg 公共面移除（实现保留在 internal/，CLI 不受影响）。[v8.0-cut]
// Removed in v8.0.
type Reflector = reflection.Reflector

// Reflection 反思结果
// Deprecated: 无外部采用证据的周边能力（自反思 / 批评），v8.0 收敛 pkg 核心承诺面。v8.0 将从 pkg 公共面移除（实现保留在 internal/，CLI 不受影响）。[v8.0-cut]
// Removed in v8.0.
type Reflection = reflection.Reflection

// Critique 批评结果
// Deprecated: 无外部采用证据的周边能力（自反思 / 批评），v8.0 收敛 pkg 核心承诺面。v8.0 将从 pkg 公共面移除（实现保留在 internal/，CLI 不受影响）。[v8.0-cut]
// Removed in v8.0.
type Critique = reflection.Critique

// Issue 问题描述
// Deprecated: 无外部采用证据的周边能力（自反思 / 批评），v8.0 收敛 pkg 核心承诺面。v8.0 将从 pkg 公共面移除（实现保留在 internal/，CLI 不受影响）。[v8.0-cut]
// Removed in v8.0.
type Issue = reflection.Issue

// Severity 严重程度
// Deprecated: 无外部采用证据的周边能力（自反思 / 批评），v8.0 收敛 pkg 核心承诺面。v8.0 将从 pkg 公共面移除（实现保留在 internal/，CLI 不受影响）。[v8.0-cut]
// Removed in v8.0.
type Severity = reflection.Severity

// Correction 纠正建议
// Deprecated: 无外部采用证据的周边能力（自反思 / 批评），v8.0 收敛 pkg 核心承诺面。v8.0 将从 pkg 公共面移除（实现保留在 internal/，CLI 不受影响）。[v8.0-cut]
// Removed in v8.0.
type Correction = reflection.Correction

// LLMReflector 使用 LLM 进行自我反思
// Deprecated: 无外部采用证据的周边能力（自反思 / 批评），v8.0 收敛 pkg 核心承诺面。v8.0 将从 pkg 公共面移除（实现保留在 internal/，CLI 不受影响）。[v8.0-cut]
// Removed in v8.0.
type LLMReflector = reflection.LLMReflector

const (
	// ReflectSeverityLow 低严重程度
	// Deprecated: 无外部采用证据的周边能力（自反思 / 批评），v8.0 收敛 pkg 核心承诺面。v8.0 将从 pkg 公共面移除（实现保留在 internal/，CLI 不受影响）。[v8.0-cut]
	// Removed in v8.0.
	ReflectSeverityLow = reflection.SeverityLow
	// ReflectSeverityMedium 中等严重程度
	// Deprecated: 无外部采用证据的周边能力（自反思 / 批评），v8.0 收敛 pkg 核心承诺面。v8.0 将从 pkg 公共面移除（实现保留在 internal/，CLI 不受影响）。[v8.0-cut]
	// Removed in v8.0.
	ReflectSeverityMedium = reflection.SeverityMedium
	// ReflectSeverityHigh 高严重程度
	// Deprecated: 无外部采用证据的周边能力（自反思 / 批评），v8.0 收敛 pkg 核心承诺面。v8.0 将从 pkg 公共面移除（实现保留在 internal/，CLI 不受影响）。[v8.0-cut]
	// Removed in v8.0.
	ReflectSeverityHigh = reflection.SeverityHigh
	// ReflectSeverityCritical 严重级别
	// Deprecated: 无外部采用证据的周边能力（自反思 / 批评），v8.0 收敛 pkg 核心承诺面。v8.0 将从 pkg 公共面移除（实现保留在 internal/，CLI 不受影响）。[v8.0-cut]
	// Removed in v8.0.
	ReflectSeverityCritical = reflection.SeverityCritical
)

var (
	// NewLLMReflector 创建 LLMReflector 实例
	// Deprecated: 无外部采用证据的周边能力（自反思 / 批评），v8.0 收敛 pkg 核心承诺面。v8.0 将从 pkg 公共面移除（实现保留在 internal/，CLI 不受影响）。[v8.0-cut]
	// Removed in v8.0.
	NewLLMReflector = reflection.NewLLMReflector
)
