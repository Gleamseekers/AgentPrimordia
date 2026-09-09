package llm

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// DemoProvider 无需 API key 的演示 Provider。
// 基于关键词匹配产生有意义的响应，用于零配置体验和新用户上手。
type DemoProvider struct{}

// NewDemoProvider 创建演示 Provider。
func NewDemoProvider() *DemoProvider {
	return &DemoProvider{}
}

// Complete 同步补全，根据用户消息关键词匹配响应。
func (d *DemoProvider) Complete(ctx context.Context, req *CompletionRequest) (*CompletionResponse, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	lastUser := lastUserMessage(req.Messages)
	content := d.generate(lastUser)

	return &CompletionResponse{
		ID:      fmt.Sprintf("demo-%d", time.Now().UnixNano()),
		Model:   "demo",
		Content: content,
		Role:    "assistant",
		Usage: Usage{
			PromptTokens:     estimateTokens(req.Messages),
			CompletionTokens: demoTokenEstimate(content),
			TotalTokens:      estimateTokens(req.Messages) + demoTokenEstimate(content),
		},
	}, nil
}

// Stream 流式补全，逐词输出。
func (d *DemoProvider) Stream(ctx context.Context, req *CompletionRequest) (<-chan Chunk, error) {
	lastUser := lastUserMessage(req.Messages)
	content := d.generate(lastUser)

	ch := make(chan Chunk, 16)
	go func() {
		defer close(ch)
		words := splitChunks(content)
		for i, w := range words {
			if err := ctx.Err(); err != nil {
				return
			}
			ch <- Chunk{
				Content: w,
				Done:    i == len(words)-1,
			}
		}
	}()
	return ch, nil
}

// CallTools 工具调用，返回提示信息。
func (d *DemoProvider) CallTools(ctx context.Context, req *ToolCallRequest) (*ToolCallResponse, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	toolNames := make([]string, 0, len(req.Tools))
	for _, t := range req.Tools {
		toolNames = append(toolNames, t.Function.Name)
	}

	content := fmt.Sprintf("Demo 模式下工具调用已模拟。可用工具: %s。\n提示: 设置 AP_LLM_API_KEY 可启用真实工具执行。",
		strings.Join(toolNames, ", "))

	return &ToolCallResponse{
		Content: content,
		Usage: Usage{
			PromptTokens:     estimateTokens(req.Messages),
			CompletionTokens: demoTokenEstimate(content),
			TotalTokens:      estimateTokens(req.Messages) + demoTokenEstimate(content),
		},
	}, nil
}

// Info 返回模型信息。
func (d *DemoProvider) Info() ModelInfo {
	return ModelInfo{
		Name:              "demo",
		Provider:          "demo",
		MaxContext:        4096,
		SupportsTools:     true,
		SupportsStreaming: true,
	}
}

// generate 根据用户输入关键词生成响应。
func (d *DemoProvider) generate(input string) string {
	lower := strings.ToLower(input)

	switch {
	case matchesAny(lower, "代码", "code", "review", "审查", "函数", "func "):
		return "我注意到你提到了代码相关的内容。\n\n" +
			"在 Demo 模式下，我可以提供基本的代码反馈：\n" +
			"1. 函数命名是否清晰表达了意图\n" +
			"2. 参数和返回值类型是否合理\n" +
			"3. 是否考虑了边界情况\n\n" +
			"提示: 连接真实 LLM（设置 AP_LLM_API_KEY）后，我能进行深度代码分析、bug 检测和优化建议。"

	case matchesAny(lower, "配置", "config", "设置", "setup", "api key", "密钥"):
		return "配置 AgentPrimordia 的 LLM 连接：\n\n" +
			"**方式一: 环境变量**\n" +
			"  export AP_LLM_API_KEY=sk-your-key\n" +
			"  export AP_LLM_PROVIDER=openai  # 或 gemini, qwen, ollama 等\n\n" +
			"**方式二: 配置文件**\n" +
			"  ap config set api-key\n" +
			"  ap config set model gpt-4o\n\n" +
			"**方式三: 项目配置 (.ap.yaml)**\n" +
			"  llm:\n" +
			"    provider: openai\n" +
			"    model: gpt-4o\n" +
			"    api_key: sk-your-key\n\n" +
			"支持的 Provider: openai, gemini, qwen, ollama, deepseek, glm, azure, anthropic"

	case matchesAny(lower, "帮助", "help", "能做什么", "what can you do", "功能", "能力"):
		return "以下是我的核心能力：\n\n" +
			"**对话与问答**\n" +
			"- 多轮对话，理解上下文\n" +
			"- 中英文双语支持\n\n" +
			"**工具使用**\n" +
			"- 文件读写和操作\n" +
			"- Shell 命令执行\n" +
			"- Web 搜索和浏览\n" +
			"- 数据库查询\n\n" +
			"**记忆与学习**\n" +
			"- 跨会话记忆持久化\n" +
			"- 工具使用经验积累\n" +
			"- 自我反思和改进\n\n" +
			"**多 Agent 协作**\n" +
			"- 任务分解和委派\n" +
			"- Pipeline/DAG 编排\n" +
			"- A2A 协议通信\n\n" +
			"提示: 运行 `ap profile` 查看 agent 的成长状态。"

	case matchesAny(lower, "谢谢", "感谢", "thanks", "thank you"):
		return "不客气！很高兴能帮到你。\n\n" +
			"在 Demo 模式下，我只能基于关键词匹配提供基础响应。\n" +
			"连接真实 LLM（设置 AP_LLM_API_KEY）后，我可以处理更复杂的问题。\n\n" +
			"你可以继续问我其他问题，或者运行 `ap profile` 查看 agent 的成长状态。"

	case matchesAny(lower, "任务", "task", "分析", "analyze", "总结", "summary"):
		return "收到你的任务请求。在 Demo 模式下，我会模拟任务处理流程：\n\n" +
			"1. **理解阶段**: 分析你的需求关键词\n" +
			"2. **规划阶段**: 确定执行步骤\n" +
			"3. **执行阶段**: 调用相关工具\n" +
			"4. **总结阶段**: 整理结果并反馈\n\n" +
			"当前结果是基于关键词匹配的模拟输出。\n" +
			"连接真实 LLM 后，我能处理复杂的多步骤任务并提供精确结果。"

	case matchesGreeting(lower):
		return "你好！我是 AgentPrimordia 的 Demo 助手。\n\n" +
			"当前运行在演示模式下，无需 API key 即可体验基本功能。\n" +
			"要获得完整的 AI 能力，请设置环境变量 AP_LLM_API_KEY 或使用 `ap config set api-key` 配置。\n\n" +
			"我可以帮你：\n" +
			"- 回答问题和对话\n" +
			"- 代码审查和建议\n" +
			"- 数据分析和总结\n" +
			"- 使用各种工具完成任务\n\n" +
			"有什么我可以帮你的吗？"

	default:
		return fmt.Sprintf("我收到了你的消息: \"%s\"\n\n"+
			"当前运行在 Demo 模式下，响应基于关键词匹配生成。\n"+
			"要获得更智能的回复，请设置 AP_LLM_API_KEY 环境变量连接真实 LLM。\n\n"+
			"你可以尝试：\n"+
			"- 输入 \"help\" 查看我的能力\n"+
			"- 输入 \"配置\" 了解如何连接真实 LLM\n"+
			"- 运行 `ap profile` 查看 agent 成长状态", input)
	}
}

// matchesAny 检查 s 是否包含任一关键词。
func matchesAny(s string, keywords ...string) bool {
	for _, kw := range keywords {
		if strings.Contains(s, kw) {
			return true
		}
	}
	return false
}

// matchesGreeting 检查是否为问候语（避免子串误匹配，如 "hi" 在 "this" 中）。
func matchesGreeting(s string) bool {
	greetings := []string{"你好", "嗨", "早上好", "下午好", "晚上好"}
	for _, g := range greetings {
		if strings.Contains(s, g) {
			return true
		}
	}
	words := strings.Fields(s)
	for _, w := range words {
		clean := strings.Trim(w, ".,!?;:")
		if clean == "hello" || clean == "hi" || clean == "hey" {
			return true
		}
	}
	return false
}

// lastUserMessage 提取最后一条用户消息。
func lastUserMessage(msgs []ChatMessage) string {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == "user" {
			return msgs[i].Content
		}
	}
	return ""
}

// splitChunks 将文本按词/句拆分为流式 chunk。
func splitChunks(text string) []string {
	var chunks []string
	for _, line := range strings.Split(text, "\n") {
		if line == "" {
			chunks = append(chunks, "\n")
			continue
		}
		chunks = append(chunks, line+"\n")
	}
	return chunks
}

// demoTokenEstimate 粗略估算消息序列的 token 数（复用 model_router.go 中的 estimateTokens）。
// demoTokenEstimate 粗略估算字符串的 token 数。
func demoTokenEstimate(s string) int {
	cnCount := 0
	enCount := 0
	for _, r := range s {
		if r > 0x4e00 && r < 0x9fff {
			cnCount++
		} else {
			enCount++
		}
	}
	return cnCount*2/3 + enCount/4 + 1
}
