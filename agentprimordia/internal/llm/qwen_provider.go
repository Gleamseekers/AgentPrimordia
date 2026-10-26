package llm

import (
	"context"
	"encoding/json"
	"fmt"
)

// QwenProvider 通义千问多模态 Provider（支持 Qwen-VL 系列）。
//
// 实现与 GLMProvider 共享 openAICompatMultimodal（见 openai_compat_multimodal.go），
// 厂商差异集中在 qwenProfile()。历史上此处曾有一份与 glm_provider.go 逐字节
// 相同的 ~470 行实现，导致修复只落在一侧；现已上提。
type QwenProvider struct {
	openAICompatMultimodal
}

// NewQwenProvider 创建通义千问 Provider
func NewQwenProvider(cfg Config) (*QwenProvider, error) {
	base, err := newOpenAICompatMultimodal(qwenProfile(), cfg)
	if err != nil {
		return nil, err
	}
	return &QwenProvider{base}, nil
}

// convertToQwenFormat 转换为通义千问 API 格式（OpenAI 兼容）。
//
// 与 GLM 的 convertToGLMFormat 走同一实现（convertContentPart）——通义千问
// 与智谱的 chat/completions 内容片段格式本就一致。保留本转发方法是为了
// 不破坏既有测试面对该名字的调用。
func (p *QwenProvider) convertToQwenFormat(content *MultimodalContent) map[string]any {
	return p.convertContentPart(content)
}

// CallTools tool调用
func (p *QwenProvider) CallTools(ctx context.Context, req *ToolCallRequest) (*ToolCallResponse, error) {
	model := p.resolveModel(req.Model)

	tools := make([]map[string]any, len(req.Tools))
	for i, t := range req.Tools {
		tools[i] = map[string]any{
			"type": t.Type,
			"function": map[string]any{
				"name":        t.Function.Name,
				"description": t.Function.Description,
				"parameters":  t.Function.Parameters,
			},
		}
	}

	body := map[string]any{
		"model":    model,
		"messages": p.buildMessages(req.Messages),
		"tools":    tools,
	}
	if temp := ResolveTemperature(nil, p.config.Temperature); temp != nil {
		body["temperature"] = *temp
	}
	if p.config.MaxTokens > 0 {
		body["max_tokens"] = p.config.MaxTokens
	}

	raw, err := p.doRequest(ctx, "/chat/completions", body)
	if err != nil {
		return nil, err
	}

	var resp openaiChatResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrResponseParseFailed, err)
	}
	if resp.Error != nil {
		return nil, resp.Error
	}
	if len(resp.Choices) == 0 {
		return nil, ErrEmptyResponse
	}

	choice := resp.Choices[0]
	result := &ToolCallResponse{
		Content: choice.Message.Content,
		Usage: Usage{
			PromptTokens:     resp.Usage.PromptTokens,
			CompletionTokens: resp.Usage.CompletionTokens,
			TotalTokens:      resp.Usage.TotalTokens,
		},
	}

	if len(choice.Message.ToolCalls) > 0 {
		result.ToolCalls = make([]FunctionCall, len(choice.Message.ToolCalls))
		for i, tc := range choice.Message.ToolCalls {
			result.ToolCalls[i] = FunctionCall{
				ID:        tc.ID,
				Name:      tc.Function.Name,
				Arguments: tc.Function.Arguments,
			}
		}
	}

	return result, nil
}
