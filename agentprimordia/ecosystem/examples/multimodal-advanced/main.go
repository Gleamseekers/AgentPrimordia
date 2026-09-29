package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/cmd/example/demo"
	ap "github.com/Gleamseekers/AgentPrimordia/v7/agentprimordia/pkg"
)

func main() {
	fmt.Println("👁️  多模态高级示例")
	fmt.Println("=" + string(make([]byte, 50)))
	fmt.Println()

	demonstrateTextAndImage()
	demonstrateVisionAgent()
	demonstrateMultimodalTools()
}

func demonstrateTextAndImage() {
	fmt.Println("📝 文本+图片多模态输入演示")
	fmt.Println("-" + string(make([]byte, 40)))

	apiKey := os.Getenv("GEMINI_API_KEY")
	if apiKey == "" {
		fmt.Println("⚠️  未设置 GEMINI_API_KEY，使用 Demo 模式")
		demoMultimodal()
		return
	}

	provider, err := ap.NewGeminiMultimodalProvider(ap.Config{
		APIKey: apiKey,
		Model:  "gemini-2.0-flash",
	})
	if err != nil {
		log.Fatalf("❌ 创建 Gemini Provider 失败: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	imageData := loadSampleImage()

	resp, _ := provider.CompleteMultimodal(ctx, &ap.CompletionRequestExt{
		Messages: []*ap.ChatMessageExt{
			{
				Role: "user",
				Contents: []*ap.MultimodalContent{
					ap.NewTextContent("请描述这张图片的内容"),
					ap.NewImageB64Content(string(imageData), "image/jpeg"),
				},
			},
		},
	})

	fmt.Printf("✅ 多模态响应:\n")
	fmt.Printf("   🤖 %s\n", resp.Content)
	fmt.Println()
}

func demoMultimodal() {
	fmt.Println("🔄 Demo 模式模拟多模态输入...")

	mockProvider := demo.NewDemoLLM(
		"这张图片显示了一个现代化的办公环境，有明亮的灯光、整洁的桌面和一台笔记本电脑。整体氛围专业且舒适。",
	)

	ctx := context.Background()
	resp, _ := mockProvider.Complete(ctx, &ap.CompletionRequest{
		Messages: []ap.ChatMessage{
			{Role: "user", Content: "[图片] 请描述这张图片"},
		},
	})

	fmt.Printf("🤖 模拟响应: %s\n", resp.Content)
	fmt.Println()
}

func demonstrateVisionAgent() {
	fmt.Println("🤖 视觉 Agent 演示")
	fmt.Println("-" + string(make([]byte, 40)))

	apiKey := os.Getenv("GEMINI_API_KEY")
	if apiKey == "" {
		fmt.Println("⚠️  未设置 GEMINI_API_KEY，使用 Demo 模式")
		demoVisionAgent()
		return
	}

	visionProvider, _ := ap.NewGeminiMultimodalProvider(ap.Config{
		APIKey: apiKey,
		Model:  "gemini-2.0-flash",
	})

	visionAgent, err := ap.NewAgent("VisionBot", "你是一个具有视觉能力的 AI 助手。你可以分析图片、理解图表、识别文字等。", visionProvider,
		ap.WithMaxTurns(5),
	)
	if err != nil {
		log.Fatalf("❌ 创建 VisionBot 失败: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	tasks := []string{
		"分析这张图片的主要内容和颜色",
		"图片中有哪些文字？请提取出来",
		"这张图片适合用于什么场景？",
	}

	for i, task := range tasks {
		fmt.Printf("📋 任务%d: %s\n", i+1, task)

		startTime := time.Now()
		resp, _ := visionAgent.Run(ctx, ap.UserMessage(task))
		duration := time.Since(startTime)

		fmt.Printf("   ⏱️  耗时: %v\n", duration)
		fmt.Printf("   🤖 回复: %s\n\n", truncate(resp.Content, 200))
	}
	fmt.Println()
}

func demoVisionAgent() {
	demoLLM := demo.NewDemoLLM(
		"作为视觉 Agent，我分析了图片并提取了以下信息：这是一张包含图表的数据可视化图片，显示了2024年各季度的销售趋势。主要颜色是蓝色和绿色。建议用于商业报告或数据分析场景。",
	)

	visionAgent, err := ap.NewAgent("DemoVisionBot", "模拟视觉 Agent 的能力", demoLLM)
	if err != nil {
		log.Fatalf("❌ 创建 DemoVisionBot 失败: %v", err)
	}

	ctx := context.Background()
	resp, _ := visionAgent.Run(ctx, ap.UserMessage("分析这张图片"))

	fmt.Printf("🤖 Demo 视觉 Agent 响应:\n")
	fmt.Printf("   %s\n", resp.Content)
	fmt.Println()
}

func demonstrateMultimodalTools() {
	fmt.Println("🔧 多模态工具集成演示")
	fmt.Println("-" + string(make([]byte, 40)))

	fmt.Println("💡 多模态工具可以:")
	fmt.Println("   1. 📸 图片处理 - 裁剪、缩放、格式转换")
	fmt.Println("   2. 🔍 OCR 文字识别 - 从图片中提取文字")
	fmt.Println("   3. 🎨 图像生成 - 根据描述生成图片")
	fmt.Println("   4. 📊 图表分析 - 理解数据可视化内容")

	fmt.Println("\n示例场景:")

	scenarios := []struct {
		name        string
		description string
	}{
		{
			name:        "文档数字化",
			description: "用户上传发票图片 → OCR 提取信息 → 结构化数据 → 存入数据库",
		},
		{
			name:        "智能客服",
			description: "用户发送产品截图 → 识别产品 → 查询库存 → 返回结果",
		},
		{
			name:        "内容审核",
			description: "用户上传图片 → 分析内容 → 检测违规 → 自动标记/拒绝",
		},
		{
			name:        "数据录入助手",
			description: "拍照表格 → 识别单元格 → 导出 Excel → 自动填充表单",
		},
	}

	for i, scenario := range scenarios {
		fmt.Printf("\n   场景%d: %s\n", i+1, scenario.name)
		fmt.Printf("   流程: %s\n", scenario.description)
	}
	fmt.Println()
}

func loadSampleImage() []byte {
	return []byte("sample-image-data-placeholder")
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
