# 自定义 LLM Provider

> 接入 AgentPrimordia 未内置的 LLM（如自部署模型）。

## 代码

```go
package myprovider

import (
    "github.com/Gleamseekers/AgentPrimordia/internal/llm"
)

type Provider struct{ apiKey string }

func New(cfg llm.ProviderConfig) *Provider {
    return {apiKey: cfg.APIKey}
}

func (p *Provider) Name() string { return "my-llm" }

func (p *Provider) Chat(ctx context.Context, req llm.ChatRequest) (*llm.ChatResponse, error) {
    // 1. 构建 HTTP 请求
    // 2. 调用自部署推理服务
    // 3. 解析响应为 llm.ChatResponse
    return &llm.ChatResponse{Content: "..."}, nil
}

func (p *Provider) ChatStream(ctx context.Context, req llm.ChatRequest, ch chan<- llm.StreamChunk) error {
    // 流式版本（可选）
    return nil
}
```

## 使用

```yaml
# .ap.yaml
llm:
  provider: my-llm
  api_key: ${MY_LLM_API_KEY}
  endpoint: http://localhost:8080/v1
```

## 扩展

- **健康检查**：确保上线前 Provider 可达
- **重试**：429 / 503 按退避重试
- **用量统计**：暴露 Prometheus 指标
- **模型路由**：根据 prompt 长度自动选模型
