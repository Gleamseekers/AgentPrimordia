// Stability: Experimental — v3.0.0 新增前沿能力，API 可能随使用场景演进而调整。
package ap

import (
	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/agent/reflection"
)

// Reflector 定义自我反思和纠错接口
type Reflector = reflection.Reflector

// Reflection 反思结果
type Reflection = reflection.Reflection

// Critique 批评结果
type Critique = reflection.Critique

// Issue 问题描述
type Issue = reflection.Issue

// Severity 严重程度
type Severity = reflection.Severity

// Correction 纠正建议
type Correction = reflection.Correction

// LLMReflector 使用 LLM 进行自我反思
type LLMReflector = reflection.LLMReflector

const (
	// ReflectSeverityLow 低严重程度
	ReflectSeverityLow = reflection.SeverityLow
	// ReflectSeverityMedium 中等严重程度
	ReflectSeverityMedium = reflection.SeverityMedium
	// ReflectSeverityHigh 高严重程度
	ReflectSeverityHigh = reflection.SeverityHigh
	// ReflectSeverityCritical 严重级别
	ReflectSeverityCritical = reflection.SeverityCritical
)

var (
	// NewLLMReflector 创建 LLMReflector 实例
	NewLLMReflector = reflection.NewLLMReflector
)
