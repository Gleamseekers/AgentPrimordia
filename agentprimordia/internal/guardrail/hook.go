package guardrail

import (
	"context"
	"fmt"

	"github.com/Gleamseekers/AgentPrimordia/internal/agent"
)

const (
	MetaKeyGuardrailFlagged = "guardrail_flagged"
	MetaKeyGuardrailResults = "guardrail_results"
)

type GuardrailHook struct {
	engine *Engine
}

func NewGuardrailHook(engine *Engine) *GuardrailHook {
	return &GuardrailHook{engine: engine}
}

func (h *GuardrailHook) RegisterInputGuard(hooks *agent.HookManager) {
	hooks.RegisterConditionalInPhase(
		agent.PhaseValidation,
		agent.HookBeforeLLM,
		h.inputCheck,
		0,
		agent.Always,
		"guardrail_input",
	)
}

func (h *GuardrailHook) RegisterOutputGuard(hooks *agent.HookManager) {
	hooks.RegisterConditionalInPhase(
		agent.PhaseValidation,
		agent.HookAfterLLM,
		h.outputCheck,
		0,
		agent.Always,
		"guardrail_output",
	)
}

func (h *GuardrailHook) RegisterAll(hooks *agent.HookManager) {
	h.RegisterInputGuard(hooks)
	h.RegisterOutputGuard(hooks)
}

func (h *GuardrailHook) inputCheck(_ context.Context, hctx *agent.HookContext) error {
	var content string
	if hctx.Message != nil {
		content = hctx.Message.Content
	}
	return h.checkContent(content, "input", hctx, func(report *Report) {
		if hctx.Message != nil && len(report.Results) > 0 {
			hctx.Message.Content = report.Results[0].Sanitized
		}
	})
}

func (h *GuardrailHook) outputCheck(_ context.Context, hctx *agent.HookContext) error {
	var content string
	if hctx.Response != nil {
		content = hctx.Response.Content
	}
	return h.checkContent(content, "output", hctx, func(report *Report) {
		if hctx.Response != nil && len(report.Results) > 0 {
			hctx.Response.Content = report.Results[0].Sanitized
		}
	})
}

func (h *GuardrailHook) checkContent(content string, direction string, hctx *agent.HookContext, onSanitize func(*Report)) error {
	if content == "" {
		return nil
	}

	var report *Report
	var err error

	switch direction {
	case "input":
		report, err = h.engine.CheckInput(content)
	default:
		report, err = h.engine.CheckOutput(content)
	}

	if err != nil {
		return fmt.Errorf("guardrail %s check error: %w", direction, err)
	}

	if report.Passed {
		return nil
	}

	switch report.Action {
	case ActionReject:
		return fmt.Errorf("guardrail rejected %s: %s", direction, report.Results[0].Message)
	case ActionSanitize:
		onSanitize(report)
	case ActionFlag:
		if hctx.Metadata == nil {
			hctx.Metadata = make(map[string]any)
		}
		hctx.Metadata[MetaKeyGuardrailFlagged] = true
		hctx.Metadata[MetaKeyGuardrailResults] = report.Results
	}

	return nil
}
