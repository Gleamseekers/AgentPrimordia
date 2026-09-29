// react_convert.go — 类型转换辅助函数
// 包含 Message → LLM ChatMessage 转换、多模态内容构建、ToolCall 转换等
package agent

import (
	"encoding/json"

	"github.com/Gleamseekers/AgentPrimordia/agentprimordia/internal/llm"
)

// Helper functions for type conversion

func convertToLLMMessages(history []Message) []llm.ChatMessage {
	msgs := make([]llm.ChatMessage, 0, len(history))
	for _, m := range history {
		msg := llm.ChatMessage{
			Role:    string(m.Role),
			Content: m.Content,
		}

		if m.HasMultimodal() {
			msg.Content = buildMultimodalContent(m.ContentParts)
		}

		if m.Role == RoleAssistant && len(m.ToolCalls) > 0 {
			msg.ToolCalls = make([]llm.FunctionCall, len(m.ToolCalls))
			for j, tc := range m.ToolCalls {
				msg.ToolCalls[j] = llm.FunctionCall{
					ID:        tc.ID,
					Name:      tc.Name,
					Arguments: tc.Args,
				}
			}
		}
		if m.Role == RoleTool && len(m.Metadata.Extra) > 0 {
			if id, ok := m.Metadata.Extra["tool_call_id"]; ok {
				msg.ToolCallID = id
			}
			if isError, ok := m.Metadata.Extra["is_error"]; ok && isError == "true" {
				msg.IsToolError = true
			}
		}
		msgs = append(msgs, msg)
	}
	return msgs
}

// convertToolDefsToLLMDefinitions 将 []map[string]any 形式的tool定义反解为 []llm.ToolDefinition。
// 优化（Task 2.5）：调用点直接持有 []llm.ToolDefinition 避免重复反解。
func convertToolDefsToLLMDefinitions(toolDefs []map[string]any) []llm.ToolDefinition {
	definitions := make([]llm.ToolDefinition, 0, len(toolDefs))
	for _, def := range toolDefs {
		fn, ok := def["function"].(map[string]any)
		if !ok {
			continue
		}
		typ, _ := def["type"].(string)
		name, _ := fn["name"].(string)
		desc, _ := fn["description"].(string)
		params, _ := fn["parameters"].(map[string]any)
		if name == "" {
			continue
		}
		definitions = append(definitions, llm.ToolDefinition{
			Type: typ,
			Function: llm.FunctionDefinition{
				Name:        name,
				Description: desc,
				Parameters:  params,
			},
		})
	}
	return definitions
}

// buildMultimodalContent 将 ContentParts 转换为 OpenAI 兼容的多模态 content JSON 字符串
func buildMultimodalContent(parts []ContentPart) string {
	type contentItem struct {
		Type     string `json:"type"`
		Text     string `json:"text,omitempty"`
		ImageURL *struct {
			URL    string `json:"url"`
			Detail string `json:"detail,omitempty"`
		} `json:"image_url,omitempty"`
	}

	items := make([]contentItem, len(parts))
	for i, p := range parts {
		switch p.Type {
		case "text":
			items[i] = contentItem{Type: "text", Text: p.Text}
		case "image_url":
			items[i] = contentItem{
				Type: "image_url",
				ImageURL: &struct {
					URL    string `json:"url"`
					Detail string `json:"detail,omitempty"`
				}{URL: p.URL, Detail: p.Detail},
			}
		case "image_b64":
			items[i] = contentItem{
				Type: "image_url",
				ImageURL: &struct {
					URL    string `json:"url"`
					Detail string `json:"detail,omitempty"`
				}{URL: "data:" + p.MIME + ";base64," + p.Data, Detail: p.Detail},
			}
		default:
			items[i] = contentItem{Type: "text", Text: p.Text}
		}
	}

	data, err := json.Marshal(items)
	if err != nil {
		type textItem struct {
			Type string `json:"type"`
			Text string `json:"text,omitempty"`
		}
		var fallback []textItem
		for _, p := range parts {
			if p.Text != "" {
				fallback = append(fallback, textItem{Type: "text", Text: p.Text})
			}
		}
		if fb, fbErr := json.Marshal(fallback); fbErr == nil {
			return string(fb)
		}
		return `[{"type":"text","text":""}]`
	}
	return string(data)
}

func convertToToolCalls(calls []llm.FunctionCall) []ToolCall {
	tcs := make([]ToolCall, len(calls))
	for i, c := range calls {
		tcs[i] = ToolCall{
			ID:   c.ID,
			Name: c.Name,
			Args: c.Arguments,
		}
	}
	return tcs
}
