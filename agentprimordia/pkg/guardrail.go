// Stability: Stable — Guardrail 引擎、规则、报告。
package ap

import (
	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/guardrail"
)

type GuardrailEngine = guardrail.Engine
type GuardrailRule = guardrail.Rule
type GuardrailReport = guardrail.Report
type GuardrailResult = guardrail.Result

type GuardrailAction = guardrail.Action
type GuardrailSeverity = guardrail.Severity
type GuardrailCheckPoint = guardrail.CheckPoint

const (
	GuardrailPass     = guardrail.ActionPass
	GuardrailReject   = guardrail.ActionReject
	GuardrailSanitize = guardrail.ActionSanitize
	GuardrailFlag     = guardrail.ActionFlag
)

const (
	SeverityLow      = guardrail.SeverityLow
	SeverityMedium   = guardrail.SeverityMedium
	SeverityHigh     = guardrail.SeverityHigh
	SeverityCritical = guardrail.SeverityCritical
)

const (
	CheckInput  = guardrail.CheckInput
	CheckOutput = guardrail.CheckOutput
)

var NewGuardrailEngine = guardrail.NewEngine

type PIIRule = guardrail.PIIRule
type PIIRuleConfig = guardrail.PIIRuleConfig

var NewPIIRule = guardrail.NewPIIRule
var DefaultPIIRuleConfig = guardrail.DefaultPIIRuleConfig

type SensitiveWordRule = guardrail.SensitiveWordRule
type SensitiveWordConfig = guardrail.SensitiveWordConfig

var NewSensitiveWordRule = guardrail.NewSensitiveWordRule

type PromptInjectionRule = guardrail.PromptInjectionRule
type PromptInjectionConfig = guardrail.PromptInjectionConfig

var NewPromptInjectionRule = guardrail.NewPromptInjectionRule

type TopicConstraintRule = guardrail.TopicConstraintRule
type TopicConstraintConfig = guardrail.TopicConstraintConfig

var NewTopicConstraintRule = guardrail.NewTopicConstraintRule

type OutputSafetyRule = guardrail.OutputSafetyRule
type OutputSafetyConfig = guardrail.OutputSafetyConfig

var NewOutputSafetyRule = guardrail.NewOutputSafetyRule

type Sanitizer = guardrail.Sanitizer
type SanitizerConfig = guardrail.SanitizerConfig
type SanitizeStrategy = guardrail.SanitizeStrategy
type SanitizePosition = guardrail.Position

var NewSanitizer = guardrail.NewSanitizer

const (
	StrategyMask    = guardrail.StrategyMask
	StrategyRedact  = guardrail.StrategyRedact
	StrategyReplace = guardrail.StrategyReplace
	StrategyHash    = guardrail.StrategyHash
)

type GuardrailHook = guardrail.GuardrailHook

var NewGuardrailHook = guardrail.NewGuardrailHook

const (
	TopicModeAllowlist = guardrail.TopicModeAllowlist
	TopicModeDenylist  = guardrail.TopicModeDenylist
)
