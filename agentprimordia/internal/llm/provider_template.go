//go:build ignore_template

// Package llm 包含 LLM Provider 抽象层。
//
// ⚠️ DO NOT USE — TEMPLATE ONLY ⚠️
//
// 本文件中定义的 TemplateProvider 是新 LLM Provider 的实现模板，
// 用于生态贡献者复制后改写。它本身不是真实可用的 Provider ——
// 任何调用 NewTemplateProvider 都会立即返回错误，Complete/Stream/
// CallTools/Info 全部返回 "TODO: 未实现"。
//
// 使用流程：
//  1. cp provider_template.go {your_provider}_provider.go
//  2. 全局替换 "template" / "Template" 为你的 provider 名称
//  3. 实现 Complete() 方法
//  4. 实现 Stream() 方法
//  5. 实现 CallTools() 方法
//  6. 实现 Info() 方法
//  7. 删除本注释块 + 把文件名改为你的 provider
//  8. 运行测试：go test -run TestTemplate ./internal/llm/
//
// 为什么 NewTemplateProvider 返回错误:
// 防止任何生态代码或示例误把模板当真 Provider 用 —— 运行时才
// 暴露 "TODO: 未实现" 太晚，启动期拒绝能更早发现问题。
//
// 误用防护设计参考 github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/docs/版本规范.md 的兼容性承诺与
// Experimental API 定义。
package llm

// ProviderTemplate 是新 Provider 的模板代码
// 复制此文件并重命名为 {provider}_provider.go，然后替换所有 TODO
//
// 使用步骤：
//  1. cp provider_template.go {provider}_provider.go
//  2. 全局替换 "template" / "Template" 为你的 provider 名称
//  3. 实现 Complete() 方法
//  4. 实现 Stream() 方法
//  5. 实现 CallTools() 方法
//  6. 实现 Info() 方法
//  7. 删除此注释块
//  8. 运行测试：go test -run TestTemplate ./internal/llm/

import (
	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/internal/jsonutil" // perf-v6 round 6 Task 1
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
)

const (
	// templateDefaultBaseURL 模板 Provider 默认 API 地址
	// TODO: 替换为实际的 API 地址
	templateDefaultBaseURL = "https://api.example.com/v1"
	// defaultTemplateMaxContext 默认最大上下文窗口
	// TODO: 替换为实际的上下文窗口大小
	defaultTemplateMaxContext = 8192
	// defaultTemplateMaxTokens 默认最大输出 Token 数
	// TODO: 替换为实际的默认值
	defaultTemplateMaxTokens = 4096
)

// TemplateProvider 模板 Provider 实现
// TODO: 重命名为 XxxProvider（如 DeepSeekProvider）
type TemplateProvider struct {
	config Config
	client *http.Client // 使用包内已定义的 http.Client（来自 net/http）
	// TODO: 新 Provider 必须同时持有流式专用 client（v6.x 评估 §4.2 P1-4）：
	//   streamClient *http.Client // NewDefaultLLMStreamClient()：无整体超时，
	//                              // Stream 路径必须用它，避免长流被 Client.Timeout 掐断
}

// NewTemplateProvider 拒绝创建 — 这是模板不是真 Provider。
//
// 任何调用都会返回 ErrTemplateNotImplemented 错误，避免生态代码
// 误把 TemplateProvider 当真实 Provider 使用。贡献者应复制本文件
// 到新文件并实现所有方法。
//
// 错误信息指引：
//
//	"TemplateProvider is a code template, not a real Provider. "
//	"Copy internal/llm/provider_template.go to a new file and implement it. "
//	"See internal/llm/provider_template.go and ecosystem/contributing/PROVIDER.md."
func NewTemplateProvider(cfg Config) (*TemplateProvider, error) {
	return nil, ErrTemplateNotImplemented
}

// Complete 执行非流式补全
func (p *TemplateProvider) Complete(ctx context.Context, req *CompletionRequest) (*CompletionResponse, error) {
	// TODO: 实现 API 调用
	// 1. 使用 ResolveModel 解析模型名称
	// 2. 使用 ResolveTemperature 解析温度参数
	// 3. 构建请求体（参考 openai_provider.go 的 Complete 方法）
	// 4. 发送 HTTP 请求（使用 p.doRequest 辅助方法）
	// 5. 解析响应
	// 6. 返回 CompletionResponse
	return nil, fmt.Errorf("TemplateProvider.Complete: not implemented")
}

// Stream 执行流式补全
func (p *TemplateProvider) Stream(ctx context.Context, req *CompletionRequest) (<-chan Chunk, error) {
	// TODO: 实现流式 API 调用
	// 1. 使用 ResolveModel 解析模型名称
	// 2. 构建请求体（stream: true）
	// 3. 发送 HTTP 请求（必须用 p.streamClient，参考 openai_provider.go：
	//    流式路径禁用 Client.Timeout，改用 ResponseHeaderTimeout + ctx deadline，
	//    见 v6.x 评估 §4.2 P1-4）
	// 4. 检查响应状态码
	// 5. 创建 buffered channel: ch := make(chan Chunk, 32)
	// 6. 在 goroutine 中：
	//    a. 使用 bufio.Scanner 读取 SSE 流
	//    b. 解析每个 data: 行
	//    c. 发送 Chunk 到 channel
	//    d. 监听 ctx.Done() 支持取消
	//    e. 流结束时发送 Chunk{Done: true}
	//    f. close(ch) 和 resp.Body.Close()
	// 参考 openai_provider.go 的 Stream 方法
	return nil, fmt.Errorf("TemplateProvider.Stream: not implemented")
}

// CallTools 执行tool调用
func (p *TemplateProvider) CallTools(ctx context.Context, req *ToolCallRequest) (*ToolCallResponse, error) {
	// TODO: 实现tool调用
	// 大多数 Provider 的tool调用与 Complete 共享同一 API
	// 区别在于请求中包含 tools 定义，响应中包含 tool_calls
	//
	// 如果 Provider 不支持tool调用，返回 ErrNotSupported：
	//   return nil, ErrNotSupported
	//
	// OpenAI 兼容 API 可复用 BuildOpenAIMessages 构建消息
	// 参考 openai_provider.go 的 CallTools 方法
	return nil, fmt.Errorf("TemplateProvider.CallTools: not implemented")
}

// Info 返回模型信息
func (p *TemplateProvider) Info() ModelInfo {
	// TODO: 返回正确的模型信息
	return ModelInfo{
		Name:              p.config.Model,
		Provider:          "template", // TODO: 替换为 provider 标识（小写英文）
		MaxContext:        defaultTemplateMaxContext,
		SupportsTools:     true, // TODO: 根据实际情况设置
		SupportsStreaming: true, // TODO: 根据实际情况设置
	}
}

// resolveModel 解析模型名称
func (p *TemplateProvider) resolveModel(model string) string {
	return ResolveModel(model, p.config.Model)
}

// doRequest 发送 HTTP 请求
// TODO: 根据目标 API 的认证方式调整请求头
func (p *TemplateProvider) doRequest(ctx context.Context, endpoint string, body any) ([]byte, error) {
	url := fmt.Sprintf("%s%s", p.config.BaseURL, endpoint)

	bodyBytes, err := jsonutil.MarshalBody(body)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request body: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// TODO: 根据目标 API 调整认证方式
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", p.config.APIKey))
	req.Header.Set("User-Agent", userAgent)

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		limitedReader := io.LimitReader(resp.Body, maxResponseSize)
		respBody, _ := io.ReadAll(limitedReader)
		// perf-v6 round 8 Task 3：携带 Retry-After + 错误分类
		return nil, NewHTTPError("api", resp.StatusCode, respBody, resp.Header)
	}

	limitedReader := io.LimitReader(resp.Body, maxResponseSize)
	responseData, err := io.ReadAll(limitedReader)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	return responseData, nil
}
