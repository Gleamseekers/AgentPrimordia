package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/jsonutil" // perf-v6 round 8 Task 1：统一 JSON 序列化
)

var (
	ErrInvalidJSON            = errors.New("llm: LLM response is not valid JSON")
	ErrSchemaValidationFailed = errors.New("llm: structured output validation failed")
)

// ResponseFormatType LLM 响应格式类型
type ResponseFormatType string

const (
	ResponseFormatText       ResponseFormatType = "text"
	ResponseFormatJSONObject ResponseFormatType = "json_object"
	ResponseFormatJSONSchema ResponseFormatType = "json_schema"
)

// ResponseFormat LLM 响应格式控制
type ResponseFormat struct {
	Type       ResponseFormatType `json:"type"`
	JSONSchema *SchemaDef         `json:"json_schema,omitempty"`
}

// SchemaDef JSON Schema 定义，用于约束 LLM 输出结构
type SchemaDef struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Schema      map[string]any `json:"schema"`
	Strict      bool           `json:"strict,omitempty"`
}

// ExtractorConfig 结构化提取器配置
type ExtractorConfig struct {
	MaxRetries int
	Validate   bool
}

// StructuredExtractor 结构化提取器
// 通过 LLM + JSON Schema 约束，从自然语言输入中提取结构化数据
// 支持重试修复：当 LLM 输出不符合 Schema 时，自动将错误反馈给 LLM 重试
type StructuredExtractor struct {
	provider Provider
	model    string
	config   ExtractorConfig
}

// NewStructuredExtractor 创建结构化提取器
func NewStructuredExtractor(provider Provider, model string) (*StructuredExtractor, error) {
	if provider == nil {
		return nil, fmt.Errorf("provider must not be nil")
	}
	return &StructuredExtractor{
		provider: provider,
		model:    model,
		config:   ExtractorConfig{MaxRetries: 0, Validate: false},
	}, nil
}

// NewStructuredExtractorWithConfig 创建带配置的结构化提取器
func NewStructuredExtractorWithConfig(provider Provider, model string, cfg ExtractorConfig) *StructuredExtractor {
	return &StructuredExtractor{
		provider: provider,
		model:    model,
		config:   cfg,
	}
}

// Extract 从自然语言输入中提取结构化数据
// prompt 引导 LLM 输出，schema 约束输出格式
// 返回 JSON RawMessage，调用方自行反序列化
func (e *StructuredExtractor) Extract(ctx context.Context, prompt string, schema *SchemaDef) (json.RawMessage, error) {
	if schema == nil {
		return nil, fmt.Errorf("schema must not be nil")
	}
	messages := []ChatMessage{
		{Role: "system", Content: structuredSystemPrompt(schema)},
		{Role: "user", Content: prompt},
	}

	maxAttempts := 1 + e.config.MaxRetries
	var lastErr error

	for attempt := 0; attempt < maxAttempts; attempt++ {
		req := &CompletionRequest{
			Messages:       messages,
			Model:          e.model,
			ResponseFormat: &ResponseFormat{Type: ResponseFormatJSONSchema, JSONSchema: schema},
		}

		resp, err := e.provider.Complete(ctx, req)
		if err != nil {
			return nil, fmt.Errorf("structured extraction LLM call failed: %w", err)
		}

		raw := json.RawMessage(resp.Content)
		if !json.Valid(raw) {
			lastErr = fmt.Errorf("LLM response is not valid JSON: %w", ErrInvalidJSON)
			messages = append(messages,
				ChatMessage{Role: "assistant", Content: resp.Content},
				ChatMessage{Role: "user", Content: fmt.Sprintf("你输出的内容不是有效的 JSON，请修正。错误: %s\n请严格按照 Schema 重新输出。", resp.Content)},
			)
			continue
		}

		if e.config.Validate {
			if errs := ValidateAgainstSchema(raw, schema); len(errs) > 0 {
				var errStrs []string
				for _, ve := range errs {
					errStrs = append(errStrs, ve.Error())
				}
				lastErr = fmt.Errorf("structured output validation failed: %w", ErrSchemaValidationFailed)
				messages = append(messages,
					ChatMessage{Role: "assistant", Content: resp.Content},
					ChatMessage{Role: "user", Content: fmt.Sprintf("你的输出不符合 Schema 约束，请修正以下错误:\n%s\n请严格按照 Schema 重新输出。", strings.Join(errStrs, "\n"))},
				)
				continue
			}
		}

		return raw, nil
	}

	return nil, fmt.Errorf("structured extraction failed (retried %d times): %w", e.config.MaxRetries, lastErr)
}

// ExtractInto 提取并反序列化到目标类型
func ExtractInto[T any](e *StructuredExtractor, ctx context.Context, prompt string, schema *SchemaDef) (*T, error) {
	raw, err := e.Extract(ctx, prompt, schema)
	if err != nil {
		return nil, err
	}

	var result T
	// perf-v6 round 8 Task 1：使用 pooled reader
	if err := jsonutil.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("failed to deserialize structured output: %w", err)
	}
	return &result, nil
}

// ExtractStruct 从自然语言输入中提取结构化数据，自动从 Go struct 生成 Schema
// v 为 Go struct 实例（零值即可），用于推断 JSON Schema
func (e *StructuredExtractor) ExtractStruct(ctx context.Context, prompt string, v any, opts ...SchemaOption) (json.RawMessage, error) {
	schema := SchemaFromStruct(v, opts...)
	return e.Extract(ctx, prompt, schema)
}

// ExtractStructInto 从自然语言输入中提取结构化数据，自动从 Go struct 生成 Schema 并反序列化
func ExtractStructInto[T any](e *StructuredExtractor, ctx context.Context, prompt string, opts ...SchemaOption) (*T, error) {
	var zero T
	schema := SchemaFromStruct(zero, opts...)
	return ExtractInto[T](e, ctx, prompt, schema)
}

// structuredSystemPrompt 生成结构化提取的系统提示词
func structuredSystemPrompt(schema *SchemaDef) string {
	schemaBytes, _ := jsonutil.Marshal(schema.Schema)
	desc := ""
	if schema.Description != "" {
		desc = "\n描述: " + schema.Description
	}
	return fmt.Sprintf(
		"你是一个结构化数据提取助手。请严格按照以下 JSON Schema 输出结果，不要输出任何其他内容。%s\n\nSchema:\n%s",
		desc,
		string(schemaBytes),
	)
}
