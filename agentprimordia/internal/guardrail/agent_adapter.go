package guardrail

import "github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/agent"

// NewAgentOutputGuardAdapter 创建 agent.OutputGuard 适配器，
// 将 Guardrail Engine 的 Output 检查包装为 agent 可注入的函数。
//
// 使用方式：
//
//	engine := guardrail.NewEngine()
//	engine.AddRule(guardrail.NewPIIRule(guardrail.DefaultPIIRuleConfig()))
//	guard := guardrail.NewAgentOutputGuardAdapter(engine)
//	agent := agentprimordia.NewReActAgent(...).WithOutputGuard(guard)
//
// 返回的适配器处理 Action：
//   - ActionPass: 返回空字符串，blocked=false
//   - ActionSanitize: 返回最后一条 Sanitize 规则的 Sanitized 结果
//   - ActionReject: 返回 blocked=true
func NewAgentOutputGuardAdapter(engine *Engine) agent.OutputGuard {
	return func(content string) (sanitized string, blocked bool, err error) {
		report, err := engine.CheckOutput(content)
		if err != nil {
			return "", false, err
		}
		if report == nil {
			return "", false, nil
		}
		switch report.Action {
		case ActionReject:
			return "", true, nil
		case ActionSanitize:
			// 取最后一条 Sanitize 规则的 Sanitized 结果（多规则链式处理）
			for i := len(report.Results) - 1; i >= 0; i-- {
				if report.Results[i].Action == ActionSanitize && report.Results[i].Sanitized != "" {
					return report.Results[i].Sanitized, false, nil
				}
			}
			return "", false, nil
		default:
			return "", false, nil
		}
	}
}
