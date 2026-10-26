package llm

import "context"

// GLMProvider 智谱 GLM 多模态 Provider（支持 GLM-4V 系列）。
//
// 实现与 QwenProvider 共享 openAICompatMultimodal（见 openai_compat_multimodal.go），
// 厂商差异集中在 glmProfile()。历史上此处曾有一份与 qwen_provider.go 逐字节
// 相同的 ~410 行实现，导致修复只落在一侧；现已上提。
type GLMProvider struct {
	openAICompatMultimodal
}

// NewGLMProvider 创建智谱 GLM Provider
func NewGLMProvider(cfg Config) (*GLMProvider, error) {
	base, err := newOpenAICompatMultimodal(glmProfile(), cfg)
	if err != nil {
		return nil, err
	}
	return &GLMProvider{base}, nil
}

// convertToGLMFormat 转换为智谱 API 格式（OpenAI 兼容）。
//
// 与 Qwen 的 convertToQwenFormat 走同一实现（convertContentPart）——智谱与
// 通义千问的 chat/completions 内容片段格式本就一致。保留本转发方法是为了
// 不破坏既有测试面对该名字的调用。
func (p *GLMProvider) convertToGLMFormat(content *MultimodalContent) map[string]any {
	return p.convertContentPart(content)
}

// CallTools tool调用（暂不支持）
//
// 智谱 OpenAI 兼容层对 tool_calls 协议支持有限，故显式返回 ErrNotSupported
// 而非静默降级——该不对称是既有契约，pkg/llm.go 的 NewGLMProvider 注释亦作此声明。
func (p *GLMProvider) CallTools(_ context.Context, _ *ToolCallRequest) (*ToolCallResponse, error) {
	return nil, ErrNotSupported
}
