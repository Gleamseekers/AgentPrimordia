package main

import (
	"context"
	"encoding/base64"
	"fmt"
	"log"
	"os"
	"time"

	ap "github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/pkg"
)

func main() {
	fmt.Println("🎨 AgentPrimordia 多模态视觉示例")
	fmt.Println("=" + string(make([]byte, 60)))
	fmt.Println()

	imagePath := "test_image.png"
	if len(os.Args) > 1 {
		imagePath = os.Args[1]
	}

	imageData, err := os.ReadFile(imagePath)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Println("📸 未找到图片文件，使用模拟数据演示...")
			runDemoWithMockData()
			return
		}
		log.Fatalf("❌ 读取图片失败: %v", err)
	}

	base64Image := base64.StdEncoding.EncodeToString(imageData)
	mimeType := detectMimeType(imagePath)

	fmt.Printf("📷 图片文件: %s\n", imagePath)
	fmt.Printf("📐 图片大小: %d bytes\n", len(imageData))
	fmt.Printf("🏷️  MIME 类型: %s\n", mimeType)
	fmt.Println()

	demonstrateMultimodalCapabilities(base64Image, mimeType)
}

func runDemoWithMockData() {
	fmt.Println()
	fmt.Println("🧪 使用 Mock 数据演示多模态能力...")
	fmt.Println()
	fmt.Println("✅ 支持的多模态 Provider:")
	fmt.Println("   🤖 OpenAI GPT-4o (视觉+音频)")
	fmt.Println("   🤖 Anthropic Claude (视觉)")
	fmt.Println("   🤖 Google Gemini (视觉+音频+视频)")
	fmt.Println("   🤖 通义千问 Qwen-VL (视觉)")
	fmt.Println("   🤖 智谱 GLM-4V (视觉)")
	fmt.Println()
	fmt.Println("💡 核心能力:")
	fmt.Println("   ✅ 多模态消息类型系统 (ContentType)")
	fmt.Println("   ✅ Base64/URL 图片输入")
	fmt.Println("   ✅ 多图对比分析")
	fmt.Println("   ✅ 流式/非流式输出")
	fmt.Println("   ✅ ReActLoop 多模态适配")
	fmt.Println("   ✅ 自动降级（多模态→纯文本）")
	fmt.Println()
	fmt.Println("🔧 使用方式:")
	fmt.Println("   1. 设置环境变量: export OPENAI_API_KEY=your_key")
	fmt.Println("   2. 运行示例: go run main.go your_image.png")
	fmt.Println()
	fmt.Println("📚 示例代码:")
	showExampleCode()
}

func showExampleCode() {
	example := `
// 创建 OpenAI 多模态 Provider
provider, _ := ap.NewOpenAIMultimodalProvider(ap.Config{
    APIKey: "your-api-key",
    Model:  "gpt-4o",
})

// 构建多模态请求（文本 + 图片）
req := &ap.CompletionRequestExt{
    Messages: []*ap.ChatMessageExt{
        ap.NewUserMultimodalMessage(
            ap.NewTextContent("请描述这张图片："),
            ap.NewImageB64Content(base64Image, "image/png"),
        ),
    },
}

// 调用多模态补全
resp, err := provider.CompleteMultimodal(ctx, req)

// 在 Agent 中使用多模态消息
multiMsg := agent.UserImageMessage(
    "分析这张图表",
    base64Image,
    "image/jpeg",
)
resp, err := myAgent.RunMultimodal(ctx, multiMsg)
`
	fmt.Println(example)
}

func demonstrateMultimodalCapabilities(base64Image string, mimeType string) {
	fmt.Println("🔬 测试各 Provider 的多模态能力")
	fmt.Println("-" + string(make([]byte, 50)))
	fmt.Println()

	testProviders := []struct {
		name     string
		provider interface{}
	}{
		{"OpenAI GPT-4o", createOpenAIProvider()},
		{"Anthropic Claude", createAnthropicProvider()},
		{"Google Gemini", createGeminiProvider()},
		{"通义千问 Qwen-VL", createQwenProvider()},
		{"智谱 GLM-4V", createGLMProvider()},
	}

	successCount := 0
	for _, tp := range testProviders {
		if tp.provider == nil {
			fmt.Printf("⚠️  %s: 跳过（未配置 API Key）\n", tp.name)
			continue
		}

		fmt.Printf("\n🤖 测试 %s...\n", tp.name)

		startTime := time.Now()

		req := &ap.CompletionRequestExt{
			Messages: []*ap.ChatMessageExt{
				ap.NewUserMultimodalMessage(
					ap.NewTextContent("请用中文描述这张图片的内容、颜色和主要元素："),
					ap.NewImageB64Content(base64Image, mimeType),
				),
			},
		}

		resp, err := callMultimodalComplete(tp.provider, req)
		duration := time.Since(startTime)

		if err != nil {
			fmt.Printf("   ❌ 失败: %v\n", err)
			continue
		}

		successCount++
		fmt.Printf("   ✅ 成功 (%.2fs)\n", duration.Seconds())
		fmt.Printf("   📝 回复: %.100s...\n", resp.Content)
		fmt.Printf("   📊 Token 使用: %d (输入: %d, 输出: %d)\n",
			resp.Usage.TotalTokens,
			resp.Usage.PromptTokens,
			resp.Usage.CompletionTokens,
		)
	}

	fmt.Println("\n" + "=" + string(make([]byte, 60)))
	fmt.Printf("🎉 多模态视觉测试完成! 成功: %d/%d\n", successCount, len(testProviders))
	fmt.Println()
	fmt.Println("💡 AgentPrimordia v0.3.0 多模态支持:")
	fmt.Println("   🌐 国际模型: OpenAI GPT-4o, Anthropic Claude, Google Gemini")
	fmt.Println("   🇨🇳 国内模型: 通义千问 Qwen-VL, 智谱 GLM-4V")
	fmt.Println("   🔧 核心特性: 零依赖、高性能、完整测试覆盖")
}

func createOpenAIProvider() interface{} {
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		return nil
	}
	provider, _ := ap.NewOpenAIMultimodalProvider(ap.Config{
		APIKey: apiKey,
		Model:  "gpt-4o",
	})
	return provider
}

func createAnthropicProvider() interface{} {
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		return nil
	}
	provider, _ := ap.NewAnthropicVisionProvider(ap.Config{
		APIKey: apiKey,
		Model:  "claude-sonnet-4-20250514",
	})
	return provider
}

func createGeminiProvider() interface{} {
	apiKey := os.Getenv("GEMINI_API_KEY")
	if apiKey == "" {
		return nil
	}
	provider, _ := ap.NewGeminiMultimodalProvider(ap.Config{
		APIKey: apiKey,
		Model:  "gemini-2.0-flash",
	})
	return provider
}

func createQwenProvider() interface{} {
	apiKey := os.Getenv("QWEN_API_KEY")
	if apiKey == "" {
		return nil
	}
	provider, _ := ap.NewQwenProvider(ap.Config{
		APIKey: apiKey,
		Model:  "qwen-vl-max-latest",
	})
	return provider
}

func createGLMProvider() interface{} {
	apiKey := os.Getenv("GLM_API_KEY")
	if apiKey == "" {
		return nil
	}
	provider, _ := ap.NewGLMProvider(ap.Config{
		APIKey: apiKey,
		Model:  "glm-4v-flash",
	})
	return provider
}

func callMultimodalComplete(provider interface{}, req *ap.CompletionRequestExt) (*ap.CompletionResponse, error) {
	if mp, ok := provider.(interface {
		CompleteMultimodal(ctx context.Context, req *ap.CompletionRequestExt) (*ap.CompletionResponse, error)
	}); ok {
		return mp.CompleteMultimodal(context.Background(), req)
	}
	return nil, fmt.Errorf("provider does not support multimodal")
}

func detectMimeType(filename string) string {
	extToMIME := map[string]string{
		".png":  "image/png",
		".jpg":  "image/jpeg",
		".jpeg": "image/jpeg",
		".gif":  "image/gif",
		".webp": "image/webp",
	}

	for ext, mime := range extToMIME {
		if len(filename) >= len(ext) && filename[len(filename)-len(ext):] == ext {
			return mime
		}
	}
	return "image/png"
}
